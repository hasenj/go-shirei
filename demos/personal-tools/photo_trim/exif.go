package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/draw"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
)

// PhotoMeta holds parsed EXIF and GPS metadata for a photo.
type PhotoMeta struct {
	HasEXIF     bool
	HasGPS      bool
	GPSLat      float64
	GPSLon      float64
	Camera      string
	DateTaken   string
	Orientation int // EXIF orientation 1..8 (0 or 1 means normal)
	ExifBytes   int // size of the raw EXIF payload in bytes
}

// FormatGPS formats decimal coordinates as a human-readable degree string.
func FormatGPS(lat, lon float64) string {
	latHem := "N"
	if lat < 0 {
		latHem = "S"
		lat = -lat
	}
	lonHem := "E"
	if lon < 0 {
		lonHem = "W"
		lon = -lon
	}
	return fmt.Sprintf("%.4f°%s, %.4f°%s", lat, latHem, lon, lonHem)
}

// InspectPhotoMeta reads metadata headers from a JPEG or PNG file on disk.
func InspectPhotoMeta(path string) (PhotoMeta, error) {
	f, err := os.Open(path)
	if err != nil {
		return PhotoMeta{}, err
	}
	defer f.Close()

	// Read up to 256 KiB of header data; EXIF segments live near the start.
	buf := make([]byte, 256*1024)
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return PhotoMeta{}, err
	}
	data := buf[:n]

	ext := strings.ToLower(filepath.Ext(path))
	if ext == ".png" || (len(data) >= 8 && bytes.Equal(data[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})) {
		return InspectPNGEXIF(data), nil
	}
	return InspectJPEGEXIF(data), nil
}

// InspectPNGEXIF checks a PNG byte stream for an eXIf chunk and parses its TIFF payload.
func InspectPNGEXIF(data []byte) PhotoMeta {
	if len(data) < 8 || !bytes.Equal(data[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}) {
		return PhotoMeta{}
	}
	pos := 8
	for pos+12 <= len(data) {
		length := int(binary.BigEndian.Uint32(data[pos : pos+4]))
		chunkType := string(data[pos+4 : pos+8])
		bodyStart := pos + 8
		bodyEnd := bodyStart + length
		if length < 0 || bodyEnd+4 > len(data) {
			break
		}
		if chunkType == "eXIf" {
			return parseTIFFEXIF(data[bodyStart:bodyEnd])
		}
		if chunkType == "IDAT" || chunkType == "IEND" {
			break
		}
		pos = bodyEnd + 4
	}
	return PhotoMeta{}
}

// InspectJPEGEXIF scans JPEG APP1 segments for an Exif TIFF payload.
func InspectJPEGEXIF(data []byte) PhotoMeta {
	app1 := extractJPEGExifPayload(data)
	if len(app1) == 0 {
		return PhotoMeta{}
	}
	return parseTIFFEXIF(app1)
}

func extractJPEGExifPayload(data []byte) []byte {
	if len(data) < 4 || data[0] != 0xFF || data[1] != 0xD8 {
		return nil
	}
	pos := 2
	for pos+4 <= len(data) {
		if data[pos] != 0xFF {
			break
		}
		for pos < len(data) && data[pos] == 0xFF {
			pos++
		}
		if pos >= len(data) {
			break
		}
		marker := data[pos]
		pos++
		if marker == 0xD9 || marker == 0xDA { // EOI or SOS
			break
		}
		if marker == 0x01 || (marker >= 0xD0 && marker <= 0xD7) {
			continue
		}
		if pos+2 > len(data) {
			break
		}
		segLen := int(binary.BigEndian.Uint16(data[pos : pos+2]))
		if segLen < 2 || pos+segLen > len(data) {
			break
		}
		payload := data[pos+2 : pos+segLen]
		if marker == 0xE1 && len(payload) > 6 && bytes.Equal(payload[:6], []byte("Exif\x00\x00")) {
			return payload[6:]
		}
		pos += segLen
	}
	return nil
}

func parseTIFFEXIF(tiff []byte) PhotoMeta {
	if len(tiff) < 8 {
		return PhotoMeta{}
	}
	var bo binary.ByteOrder
	switch {
	case tiff[0] == 'I' && tiff[1] == 'I':
		bo = binary.LittleEndian
	case tiff[0] == 'M' && tiff[1] == 'M':
		bo = binary.BigEndian
	default:
		return PhotoMeta{}
	}
	if bo.Uint16(tiff[2:4]) != 42 {
		return PhotoMeta{}
	}
	ifd0Off := int(bo.Uint32(tiff[4:8]))
	if ifd0Off < 8 || ifd0Off+2 > len(tiff) {
		return PhotoMeta{}
	}

	meta := PhotoMeta{
		HasEXIF:     true,
		Orientation: 1,
		ExifBytes:   len(tiff),
	}

	var makeStr, modelStr string
	var exifIFDOff, gpsIFDOff int

	walkIFD(tiff, bo, ifd0Off, func(tag, typ uint16, count uint32, valOff int) {
		switch tag {
		case 0x010F: // Make
			makeStr = readTIFFASCII(tiff, typ, count, valOff)
		case 0x0110: // Model
			modelStr = readTIFFASCII(tiff, typ, count, valOff)
		case 0x0112: // Orientation
			if v, ok := readTIFFUint(tiff, bo, typ, valOff); ok && v >= 1 && v <= 8 {
				meta.Orientation = int(v)
			}
		case 0x0132: // DateTime
			if meta.DateTaken == "" {
				meta.DateTaken = normalizeExifDate(readTIFFASCII(tiff, typ, count, valOff))
			}
		case 0x8769: // ExifIFDPointer
			if v, ok := readTIFFUint(tiff, bo, typ, valOff); ok {
				exifIFDOff = int(v)
			}
		case 0x8825: // GPSInfoIFDPointer
			if v, ok := readTIFFUint(tiff, bo, typ, valOff); ok {
				gpsIFDOff = int(v)
			}
		}
	})

	if exifIFDOff > 0 {
		walkIFD(tiff, bo, exifIFDOff, func(tag, typ uint16, count uint32, valOff int) {
			if tag == 0x9003 { // DateTimeOriginal
				if s := normalizeExifDate(readTIFFASCII(tiff, typ, count, valOff)); s != "" {
					meta.DateTaken = s
				}
			}
		})
	}

	if gpsIFDOff > 0 {
		var latRef, lonRef string
		var latVals, lonVals []float64
		walkIFD(tiff, bo, gpsIFDOff, func(tag, typ uint16, count uint32, valOff int) {
			switch tag {
			case 0x0001:
				latRef = strings.ToUpper(strings.TrimSpace(readTIFFASCII(tiff, typ, count, valOff)))
			case 0x0002:
				latVals = readTIFFRationals(tiff, bo, typ, count, valOff)
			case 0x0003:
				lonRef = strings.ToUpper(strings.TrimSpace(readTIFFASCII(tiff, typ, count, valOff)))
			case 0x0004:
				lonVals = readTIFFRationals(tiff, bo, typ, count, valOff)
			}
		})
		if len(latVals) == 3 && len(lonVals) == 3 {
			lat := latVals[0] + latVals[1]/60.0 + latVals[2]/3600.0
			lon := lonVals[0] + lonVals[1]/60.0 + lonVals[2]/3600.0
			if strings.HasPrefix(latRef, "S") {
				lat = -lat
			}
			if strings.HasPrefix(lonRef, "W") {
				lon = -lon
			}
			if !math.IsNaN(lat) && !math.IsNaN(lon) && math.Abs(lat) <= 90 && math.Abs(lon) <= 180 {
				meta.HasGPS = true
				meta.GPSLat = lat
				meta.GPSLon = lon
			}
		}
	}

	makeStr = strings.TrimSpace(makeStr)
	modelStr = strings.TrimSpace(modelStr)
	if modelStr != "" && makeStr != "" && !strings.HasPrefix(strings.ToLower(modelStr), strings.ToLower(makeStr)) {
		meta.Camera = makeStr + " " + modelStr
	} else if modelStr != "" {
		meta.Camera = modelStr
	} else {
		meta.Camera = makeStr
	}

	return meta
}

func walkIFD(tiff []byte, bo binary.ByteOrder, offset int, fn func(tag, typ uint16, count uint32, valOff int)) {
	if offset < 0 || offset+2 > len(tiff) {
		return
	}
	numEntries := int(bo.Uint16(tiff[offset : offset+2]))
	base := offset + 2
	if numEntries <= 0 || numEntries > 256 || base+numEntries*12 > len(tiff) {
		return
	}
	for i := 0; i < numEntries; i++ {
		e := base + i*12
		tag := bo.Uint16(tiff[e : e+2])
		typ := bo.Uint16(tiff[e+2 : e+4])
		count := bo.Uint32(tiff[e+4 : e+8])
		byteLen := tiffTypeSize(typ) * int(count)
		valOff := e + 8
		if byteLen > 4 {
			valOff = int(bo.Uint32(tiff[e+8 : e+12]))
		}
		if valOff < 0 || valOff+byteLen > len(tiff) {
			continue
		}
		fn(tag, typ, count, valOff)
	}
}

func tiffTypeSize(typ uint16) int {
	switch typ {
	case 1, 2, 6, 7: // BYTE, ASCII, SBYTE, UNDEFINED
		return 1
	case 3, 8: // SHORT, SSHORT
		return 2
	case 4, 9, 11: // LONG, SLONG, FLOAT
		return 4
	case 5, 10, 12: // RATIONAL, SRATIONAL, DOUBLE
		return 8
	default:
		return 0
	}
}

func readTIFFASCII(tiff []byte, typ uint16, count uint32, valOff int) string {
	if typ != 2 || count == 0 {
		return ""
	}
	end := valOff + int(count)
	if valOff < 0 || end > len(tiff) {
		return ""
	}
	s := string(tiff[valOff:end])
	return strings.TrimRight(s, "\x00 ")
}

func readTIFFUint(tiff []byte, bo binary.ByteOrder, typ uint16, valOff int) (uint32, bool) {
	switch typ {
	case 3: // SHORT
		if valOff+2 > len(tiff) {
			return 0, false
		}
		return uint32(bo.Uint16(tiff[valOff : valOff+2])), true
	case 4: // LONG
		if valOff+4 > len(tiff) {
			return 0, false
		}
		return bo.Uint32(tiff[valOff : valOff+4]), true
	default:
		return 0, false
	}
}

func readTIFFRationals(tiff []byte, bo binary.ByteOrder, typ uint16, count uint32, valOff int) []float64 {
	if typ != 5 || count == 0 || count > 8 {
		return nil
	}
	if valOff < 0 || valOff+int(count)*8 > len(tiff) {
		return nil
	}
	out := make([]float64, 0, count)
	for i := 0; i < int(count); i++ {
		p := valOff + i*8
		num := bo.Uint32(tiff[p : p+4])
		den := bo.Uint32(tiff[p+4 : p+8])
		if den == 0 {
			return nil
		}
		out = append(out, float64(num)/float64(den))
	}
	return out
}

func normalizeExifDate(s string) string {
	s = strings.TrimSpace(s)
	// Standard EXIF date format: "2026:04:18 17:42:10" -> "2026-04-18 17:42"
	if len(s) >= 16 && s[4] == ':' && s[7] == ':' {
		return s[0:4] + "-" + s[5:7] + "-" + s[8:10] + " " + s[11:16]
	}
	return s
}

// BuildCleanEXIF builds a minimal valid TIFF/EXIF APP1 payload (including "Exif\x00\x00")
// from parsed camera/date metadata, with optional GPS coordinates omitted when includeGPS is false.
func BuildCleanEXIF(makeStr, modelStr, dateStr string, includeGPS bool, lat, lon float64) []byte {
	type entry struct {
		tag   uint16
		typ   uint16
		count uint32
		raw   []byte // inline if len <= 4, else written to data section
	}

	bo := binary.BigEndian
	var ifd0 []entry

	addASCII := func(list *[]entry, tag uint16, s string) {
		if s == "" {
			return
		}
		b := append([]byte(s), 0)
		*list = append(*list, entry{tag: tag, typ: 2, count: uint32(len(b)), raw: b})
	}

	addASCII(&ifd0, 0x010F, makeStr)
	addASCII(&ifd0, 0x0110, modelStr)
	// Orientation = 1 (normal)
	orient := make([]byte, 2)
	bo.PutUint16(orient, 1)
	ifd0 = append(ifd0, entry{tag: 0x0112, typ: 3, count: 1, raw: orient})
	if dateStr != "" {
		addASCII(&ifd0, 0x0132, dateStr)
	}

	var gpsEntries []entry
	if includeGPS {
		latRef := "N"
		if lat < 0 {
			latRef = "S"
			lat = -lat
		}
		lonRef := "E"
		if lon < 0 {
			lonRef = "W"
			lon = -lon
		}
		addASCII(&gpsEntries, 0x0001, latRef)
		gpsEntries = append(gpsEntries, entry{tag: 0x0002, typ: 5, count: 3, raw: encodeDMS(bo, lat)})
		addASCII(&gpsEntries, 0x0003, lonRef)
		gpsEntries = append(gpsEntries, entry{tag: 0x0004, typ: 5, count: 3, raw: encodeDMS(bo, lon)})

		// Placeholder GPSInfoIFDPointer (tag 0x8825), patched below.
		ifd0 = append(ifd0, entry{tag: 0x8825, typ: 4, count: 1, raw: make([]byte, 4)})
	}

	var buf bytes.Buffer
	// TIFF header (8 bytes): MM, 42, IFD0 offset = 8
	buf.Write([]byte{'M', 'M', 0x00, 0x2A, 0x00, 0x00, 0x00, 0x08})

	ifd0Size := 2 + len(ifd0)*12 + 4
	dataOffset := 8 + ifd0Size

	// Reserve space for IFD0 and write out-of-line data after it.
	var dataBuf bytes.Buffer
	var ifd0Buf bytes.Buffer
	_ = binary.Write(&ifd0Buf, bo, uint16(len(ifd0)))

	gpsPtrPos := -1
	for _, e := range ifd0 {
		_ = binary.Write(&ifd0Buf, bo, e.tag)
		_ = binary.Write(&ifd0Buf, bo, e.typ)
		_ = binary.Write(&ifd0Buf, bo, e.count)
		if e.tag == 0x8825 {
			gpsPtrPos = 8 + ifd0Buf.Len()
			ifd0Buf.Write([]byte{0, 0, 0, 0})
			continue
		}
		if len(e.raw) <= 4 {
			var pad [4]byte
			copy(pad[:], e.raw)
			ifd0Buf.Write(pad[:])
		} else {
			off := uint32(dataOffset + dataBuf.Len())
			_ = binary.Write(&ifd0Buf, bo, off)
			dataBuf.Write(e.raw)
			if len(e.raw)%2 == 1 {
				dataBuf.WriteByte(0)
			}
		}
	}
	// Next IFD offset = 0
	ifd0Buf.Write([]byte{0, 0, 0, 0})

	buf.Write(ifd0Buf.Bytes())
	buf.Write(dataBuf.Bytes())

	if includeGPS && gpsPtrPos >= 0 {
		gpsIFDOff := buf.Len()
		gpsIFDSize := 2 + len(gpsEntries)*12 + 4
		gpsDataOff := gpsIFDOff + gpsIFDSize

		var gpsBuf bytes.Buffer
		var gpsData bytes.Buffer
		_ = binary.Write(&gpsBuf, bo, uint16(len(gpsEntries)))
		for _, e := range gpsEntries {
			_ = binary.Write(&gpsBuf, bo, e.tag)
			_ = binary.Write(&gpsBuf, bo, e.typ)
			_ = binary.Write(&gpsBuf, bo, e.count)
			if len(e.raw) <= 4 {
				var pad [4]byte
				copy(pad[:], e.raw)
				gpsBuf.Write(pad[:])
			} else {
				off := uint32(gpsDataOff + gpsData.Len())
				_ = binary.Write(&gpsBuf, bo, off)
				gpsData.Write(e.raw)
				if len(e.raw)%2 == 1 {
					gpsData.WriteByte(0)
				}
			}
		}
		gpsBuf.Write([]byte{0, 0, 0, 0})
		buf.Write(gpsBuf.Bytes())
		buf.Write(gpsData.Bytes())

		raw := buf.Bytes()
		bo.PutUint32(raw[gpsPtrPos:gpsPtrPos+4], uint32(gpsIFDOff))
	}

	out := make([]byte, 0, 6+buf.Len())
	out = append(out, []byte("Exif\x00\x00")...)
	out = append(out, buf.Bytes()...)
	return out
}

func encodeDMS(bo binary.ByteOrder, deg float64) []byte {
	d := math.Floor(deg)
	rem := (deg - d) * 60.0
	m := math.Floor(rem)
	s := (rem - m) * 60.0

	out := make([]byte, 24)
	bo.PutUint32(out[0:4], uint32(d))
	bo.PutUint32(out[4:8], 1)
	bo.PutUint32(out[8:12], uint32(m))
	bo.PutUint32(out[12:16], 1)
	bo.PutUint32(out[16:20], uint32(math.Round(s*1000)))
	bo.PutUint32(out[20:24], 1000)
	return out
}

// InjectJPEGAPP1 inserts an APP1 EXIF segment right after the SOI marker of a JPEG byte slice.
func InjectJPEGAPP1(jpegData, app1Payload []byte) []byte {
	if len(jpegData) < 2 || jpegData[0] != 0xFF || jpegData[1] != 0xD8 || len(app1Payload) == 0 || len(app1Payload)+2 > 0xFFFF {
		return jpegData
	}
	segLen := uint16(len(app1Payload) + 2)
	out := make([]byte, 0, len(jpegData)+4+len(app1Payload))
	out = append(out, 0xFF, 0xD8)
	out = append(out, 0xFF, 0xE1, byte(segLen>>8), byte(segLen))
	out = append(out, app1Payload...)
	out = append(out, jpegData[2:]...)
	return out
}

// ApplyOrientation transforms img to normal upright orientation (EXIF orientation 1).
func ApplyOrientation(src image.Image, orient int) *image.RGBA {
	b := src.Bounds()
	w, h := b.Dx(), b.Dy()
	rgba := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(rgba, rgba.Bounds(), src, b.Min, draw.Src)

	switch orient {
	case 2: // Flip horizontal
		dst := image.NewRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				dst.SetRGBA(w-1-x, y, rgba.RGBAAt(x, y))
			}
		}
		return dst
	case 3: // Rotate 180
		dst := image.NewRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				dst.SetRGBA(w-1-x, h-1-y, rgba.RGBAAt(x, y))
			}
		}
		return dst
	case 4: // Flip vertical
		dst := image.NewRGBA(image.Rect(0, 0, w, h))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				dst.SetRGBA(x, h-1-y, rgba.RGBAAt(x, y))
			}
		}
		return dst
	case 5: // Transpose
		dst := image.NewRGBA(image.Rect(0, 0, h, w))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				dst.SetRGBA(y, x, rgba.RGBAAt(x, y))
			}
		}
		return dst
	case 6: // Rotate 90 CW
		dst := image.NewRGBA(image.Rect(0, 0, h, w))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				dst.SetRGBA(h-1-y, x, rgba.RGBAAt(x, y))
			}
		}
		return dst
	case 7: // Transverse
		dst := image.NewRGBA(image.Rect(0, 0, h, w))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				dst.SetRGBA(h-1-y, w-1-x, rgba.RGBAAt(x, y))
			}
		}
		return dst
	case 8: // Rotate 270 CW
		dst := image.NewRGBA(image.Rect(0, 0, h, w))
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				dst.SetRGBA(y, w-1-x, rgba.RGBAAt(x, y))
			}
		}
		return dst
	default:
		return rgba
	}
}
