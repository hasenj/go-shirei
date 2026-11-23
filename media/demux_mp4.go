package media

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
)

type mp4Sample struct {
	offset   int64
	size     uint32
	dts      float64
	pts      float64
	duration float64
	keyframe bool
}

type mp4Track struct {
	isVideo   bool
	timescale uint32
	duration  uint64
	samples   []mp4Sample
	cursor    int

	// Raw tables collected during stbl parse.
	stts      [][2]uint32 // (count, delta)
	ctts      [][2]int32  // (count, offset)
	stsc      [][3]uint32 // (firstChunk, samplesPerChunk, descIdx)
	stszConst uint32
	stszCount uint32
	stsz      []uint32
	stco      []int64
	stss      []uint32
	editShift float64 // seconds subtracted from PTS (from elst)
}

type mp4Demuxer struct {
	f          *os.File
	fileSize   int64
	movieScale uint32
	movieDur   uint64

	info  mediaProbe
	video *mp4Track
	audio *mp4Track
}

func newMP4Demuxer(f *os.File) (*mp4Demuxer, error) {
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	d := &mp4Demuxer{
		f:        f,
		fileSize: st.Size(),
		info: mediaProbe{
			Container: containerMP4,
		},
	}
	if err := d.parseTopLevel(); err != nil {
		return nil, err
	}
	return d, nil
}

func (d *mp4Demuxer) probe() mediaProbe { return d.info }
func (d *mp4Demuxer) close() error      { return d.f.Close() }

func (d *mp4Demuxer) parseTopLevel() error {
	var off int64
	foundMoov := false
	for off < d.fileSize {
		fourcc, dataOff, boxEnd, err := readMP4BoxHeader(d.f, off, d.fileSize)
		if err != nil {
			break
		}
		if fourcc == "moov" {
			size := boxEnd - dataOff
			if size <= 0 || size > 128<<20 {
				return errors.New("media: invalid mp4 moov size")
			}
			buf := make([]byte, size)
			if _, err := d.f.ReadAt(buf, dataOff); err != nil {
				return err
			}
			if err := d.parseMoov(buf); err != nil {
				return err
			}
			foundMoov = true
			break
		}
		if boxEnd <= off {
			break
		}
		off = boxEnd
	}
	if !foundMoov {
		return errors.New("media: mp4 missing moov box")
	}
	if d.video == nil && d.audio == nil {
		return errors.New("media: mp4 contains no usable video or audio tracks")
	}
	if d.video != nil && len(d.video.samples) == 0 {
		return errors.New("media: fragmented or empty mp4 not supported")
	}
	if d.info.Duration <= 0 && d.movieScale > 0 && d.movieDur > 0 {
		d.info.Duration = float64(d.movieDur) / float64(d.movieScale)
	}
	if d.video != nil && len(d.video.samples) > 0 {
		last := d.video.samples[len(d.video.samples)-1]
		end := last.pts + last.duration
		if end > d.info.Duration {
			d.info.Duration = end
		}
		if d.info.Duration > 0 {
			d.info.FPS = float64(len(d.video.samples)) / d.info.Duration
		}
	}
	if d.info.FPS <= 0 && d.video != nil {
		d.info.FPS = 30
	}
	return nil
}

func (d *mp4Demuxer) parseMoov(buf []byte) error {
	for len(buf) >= 8 {
		fourcc, body, rest, ok := nextMP4Box(buf)
		if !ok {
			break
		}
		buf = rest
		switch fourcc {
		case "mvhd":
			d.parseMvhd(body)
		case "trak":
			d.parseTrak(body)
		}
	}
	return nil
}

func (d *mp4Demuxer) parseMvhd(b []byte) {
	if len(b) < 20 {
		return
	}
	ver := b[0]
	if ver == 0 && len(b) >= 20 {
		d.movieScale = binary.BigEndian.Uint32(b[12:16])
		d.movieDur = uint64(binary.BigEndian.Uint32(b[16:20]))
	} else if ver == 1 && len(b) >= 32 {
		d.movieScale = binary.BigEndian.Uint32(b[20:24])
		d.movieDur = binary.BigEndian.Uint64(b[24:32])
	}
	if d.movieScale > 0 && d.movieDur > 0 {
		d.info.Duration = float64(d.movieDur) / float64(d.movieScale)
	}
}

func (d *mp4Demuxer) parseTrak(buf []byte) {
	var (
		handler    string
		tkW, tkH   int
		trk        = &mp4Track{}
		vCodec     videoCodec
		aCodec     audioCodec
		vW, vH     int
		aSR, aCh   int
		vPriv      []byte
		aPriv      []byte
		aPreSkip   int
		hasElst    bool
		elstMediaT int64
		elstDelay  float64
	)

	for len(buf) >= 8 {
		fourcc, body, rest, ok := nextMP4Box(buf)
		if !ok {
			break
		}
		buf = rest
		switch fourcc {
		case "tkhd":
			tkW, tkH = parseTkhdSize(body)
		case "edts":
			for len(body) >= 8 {
				ef, eb, er, eok := nextMP4Box(body)
				if !eok {
					break
				}
				body = er
				if ef == "elst" {
					hasElst, elstMediaT, elstDelay = parseElst(eb, d.movieScale)
				}
			}
		case "mdia":
			for len(body) >= 8 {
				mf, mb, mr, mok := nextMP4Box(body)
				if !mok {
					break
				}
				body = mr
				switch mf {
				case "mdhd":
					trk.timescale, trk.duration = parseMdhd(mb)
				case "hdlr":
					if len(mb) >= 12 {
						handler = string(mb[8:12])
					}
				case "minf":
					for len(mb) >= 8 {
						nf, nb, nr, nok := nextMP4Box(mb)
						if !nok {
							break
						}
						mb = nr
						if nf == "stbl" {
							for len(nb) >= 8 {
								sf, sb, sr, sok := nextMP4Box(nb)
								if !sok {
									break
								}
								nb = sr
								switch sf {
								case "stsd":
									if handler == "vide" {
										vCodec, vW, vH, vPriv = parseStsdVideo(sb)
									} else if handler == "soun" {
										aCodec, aSR, aCh, aPreSkip, aPriv = parseStsdAudio(sb)
									}
								case "stts":
									trk.stts = parseStts(sb)
								case "ctts":
									trk.ctts = parseCtts(sb)
								case "stsc":
									trk.stsc = parseStsc(sb)
								case "stsz":
									trk.stszConst, trk.stszCount, trk.stsz = parseStsz(sb)
								case "stco":
									trk.stco = parseStco(sb)
								case "co64":
									trk.stco = parseCo64(sb)
								case "stss":
									trk.stss = parseStss(sb)
								}
							}
						}
					}
				}
			}
		}
	}

	if trk.timescale == 0 {
		return
	}
	if hasElst {
		trk.editShift = float64(elstMediaT)/float64(trk.timescale) - elstDelay
	}

	switch handler {
	case "vide":
		if d.video != nil {
			return
		}
		trk.isVideo = true
		if err := trk.buildSamples(); err != nil {
			return
		}
		d.video = trk
		d.info.VideoCodec = vCodec
		if vW > 0 && vH > 0 {
			d.info.Width, d.info.Height = vW, vH
		} else {
			d.info.Width, d.info.Height = tkW, tkH
		}
		d.info.VideoPrivate = vPriv

	case "soun":
		if d.audio != nil {
			return
		}
		trk.isVideo = false
		if err := trk.buildSamples(); err != nil {
			return
		}
		d.audio = trk
		d.info.AudioCodec = aCodec
		d.info.SampleRate = aSR
		d.info.Channels = aCh
		d.info.AudioPrivate = aPriv
		d.info.AudioPreSkip = aPreSkip
		if aCodec == audioCodecOpus && d.info.SampleRate == 0 {
			d.info.SampleRate = 48000
		}
		if d.info.Channels == 0 {
			d.info.Channels = 2
		}
	}
}

const mp4MaxSamples = 10_000_000

func (t *mp4Track) buildSamples() error {
	if len(t.stsc) == 0 || len(t.stco) == 0 {
		return errors.New("missing chunk tables")
	}
	countU64 := uint64(t.stszCount)
	if countU64 == 0 {
		return errors.New("no samples")
	}
	if t.stszConst == 0 && uint64(len(t.stsz)) < countU64 {
		countU64 = uint64(len(t.stsz))
	}
	// Cap count against samples reachable through stsc + stco and hard limit
	// before allocating, so a crafted stsz with constant sample size cannot OOM.
	var chunkSamples uint64
	for cIdx := 0; cIdx < len(t.stsc); cIdx++ {
		firstChunk := int(t.stsc[cIdx][0]) // 1-based
		perChunk := uint64(t.stsc[cIdx][1])
		if firstChunk < 1 || firstChunk > len(t.stco) || perChunk == 0 {
			continue
		}
		lastChunk := len(t.stco)
		if cIdx+1 < len(t.stsc) {
			nextFirst := int(t.stsc[cIdx+1][0]) - 1
			if nextFirst < lastChunk {
				lastChunk = nextFirst
			}
		}
		if lastChunk < firstChunk {
			continue
		}
		nChunks := uint64(lastChunk - firstChunk + 1)
		if perChunk > mp4MaxSamples/nChunks || chunkSamples > mp4MaxSamples-nChunks*perChunk {
			chunkSamples = mp4MaxSamples
			break
		}
		chunkSamples += nChunks * perChunk
	}
	if countU64 > chunkSamples {
		countU64 = chunkSamples
	}
	if countU64 > mp4MaxSamples {
		countU64 = mp4MaxSamples
	}
	count := int(countU64)
	if count <= 0 {
		return errors.New("no samples")
	}
	samples := make([]mp4Sample, count)

	// 1. Sample sizes.
	for i := 0; i < count; i++ {
		if t.stszConst != 0 {
			samples[i].size = t.stszConst
		} else {
			samples[i].size = t.stsz[i]
		}
	}

	// 2. Sample file offsets from stsc + stco/co64.
	sIdx := 0
	for cIdx := 0; cIdx < len(t.stsc) && sIdx < count; cIdx++ {
		firstChunk := int(t.stsc[cIdx][0]) // 1-based
		perChunk := int(t.stsc[cIdx][1])
		if firstChunk < 1 || perChunk <= 0 {
			continue
		}
		lastChunk := len(t.stco)
		if cIdx+1 < len(t.stsc) {
			nextFirst := int(t.stsc[cIdx+1][0]) - 1
			if nextFirst < lastChunk {
				lastChunk = nextFirst
			}
		}
		for chunk := firstChunk; chunk <= lastChunk && sIdx < count; chunk++ {
			off := t.stco[chunk-1]
			for j := 0; j < perChunk && sIdx < count; j++ {
				samples[sIdx].offset = off
				off += int64(samples[sIdx].size)
				sIdx++
			}
		}
	}
	if sIdx < count {
		samples = samples[:sIdx]
		count = sIdx
	}

	// 3. Decoding timestamps from stts.
	invScale := 1.0 / float64(t.timescale)
	var dts int64
	si := 0
	for _, entry := range t.stts {
		cnt := int(entry[0])
		delta := int64(entry[1])
		durSec := float64(delta) * invScale
		for j := 0; j < cnt && si < count; j++ {
			samples[si].dts = float64(dts) * invScale
			samples[si].pts = samples[si].dts - t.editShift
			samples[si].duration = durSec
			dts += delta
			si++
		}
	}

	// 4. Composition offsets from ctts.
	if len(t.ctts) > 0 {
		si = 0
		for _, entry := range t.ctts {
			cnt := int(entry[0])
			off := float64(entry[1]) * invScale
			for j := 0; j < cnt && si < count; j++ {
				samples[si].pts = samples[si].dts + off - t.editShift
				if samples[si].pts < 0 {
					samples[si].pts = 0
				}
				si++
			}
		}
	} else {
		for i := range samples {
			if samples[i].pts < 0 {
				samples[i].pts = 0
			}
		}
	}

	// 5. Keyframes from stss (absent => all samples are sync samples).
	if len(t.stss) == 0 {
		for i := range samples {
			samples[i].keyframe = true
		}
	} else {
		for _, idx1 := range t.stss {
			if idx1 >= 1 && int(idx1) <= count {
				samples[idx1-1].keyframe = true
			}
		}
		if count > 0 {
			samples[0].keyframe = true
		}
	}

	t.samples = samples
	return nil
}

func (d *mp4Demuxer) seek(sec float64) error {
	if sec < 0 {
		sec = 0
	}
	targetPTS := sec
	if d.video != nil && len(d.video.samples) > 0 {
		s := d.video.samples
		// Find the latest sample with PTS <= sec, then back up to its preceding keyframe.
		best := 0
		for i := range s {
			if s[i].pts <= sec+1e-6 {
				best = i
			} else if s[i].dts > sec+2.0 {
				break
			}
		}
		for best > 0 && !s[best].keyframe {
			best--
		}
		d.video.cursor = best
		targetPTS = s[best].pts
	}
	if d.audio != nil && len(d.audio.samples) > 0 {
		s := d.audio.samples
		idx := sort.Search(len(s), func(i int) bool {
			return s[i].pts >= targetPTS
		})
		if idx > 0 && s[idx-1].pts <= targetPTS {
			idx--
		}
		if idx >= len(s) {
			idx = len(s)
		}
		d.audio.cursor = idx
	}
	return nil
}

func (d *mp4Demuxer) nextPacket() (demuxPacket, error) {
	var vReady, aReady bool
	var vS, aS mp4Sample
	if d.video != nil && d.video.cursor < len(d.video.samples) {
		vReady = true
		vS = d.video.samples[d.video.cursor]
	}
	if d.audio != nil && d.audio.cursor < len(d.audio.samples) {
		aReady = true
		aS = d.audio.samples[d.audio.cursor]
	}
	if !vReady && !aReady {
		return demuxPacket{}, io.EOF
	}

	var pickVideo bool
	switch {
	case vReady && !aReady:
		pickVideo = true
	case !vReady && aReady:
		pickVideo = false
	default:
		pickVideo = vS.dts <= aS.dts
	}

	var s mp4Sample
	if pickVideo {
		s = vS
		d.video.cursor++
	} else {
		s = aS
		d.audio.cursor++
	}
	if s.size == 0 || s.size > 64<<20 {
		return demuxPacket{
			IsVideo:  pickVideo,
			Keyframe: s.keyframe,
			PTS:      s.pts,
			Duration: s.duration,
		}, nil
	}
	data := make([]byte, s.size)
	if _, err := d.f.ReadAt(data, s.offset); err != nil {
		return demuxPacket{}, fmt.Errorf("media: mp4 read sample: %w", err)
	}
	return demuxPacket{
		IsVideo:  pickVideo,
		Keyframe: s.keyframe,
		PTS:      s.pts,
		Duration: s.duration,
		Data:     data,
	}, nil
}

// -----------------------------------------------------------------------------
// MP4 box helpers

func readMP4BoxHeader(f *os.File, off, limit int64) (fourcc string, dataOff, boxEnd int64, err error) {
	if off+8 > limit {
		return "", 0, 0, io.EOF
	}
	var hdr [16]byte
	n, err := f.ReadAt(hdr[:8], off)
	if err != nil || n < 8 {
		return "", 0, 0, io.EOF
	}
	size := uint64(binary.BigEndian.Uint32(hdr[0:4]))
	fourcc = string(hdr[4:8])
	hdrSize := int64(8)
	if size == 1 {
		if off+16 > limit {
			return "", 0, 0, io.ErrUnexpectedEOF
		}
		if _, err := f.ReadAt(hdr[8:16], off+8); err != nil {
			return "", 0, 0, err
		}
		size = binary.BigEndian.Uint64(hdr[8:16])
		hdrSize = 16
	} else if size == 0 {
		size = uint64(limit - off)
	}
	if size < uint64(hdrSize) {
		return "", 0, 0, errors.New("media: corrupt mp4 box size")
	}
	if size > uint64(limit-off) {
		boxEnd = limit
	} else {
		boxEnd = off + int64(size)
	}
	return fourcc, off + hdrSize, boxEnd, nil
}

func nextMP4Box(buf []byte) (fourcc string, body, rest []byte, ok bool) {
	if len(buf) < 8 {
		return "", nil, nil, false
	}
	size := uint64(binary.BigEndian.Uint32(buf[0:4]))
	fourcc = string(buf[4:8])
	hdrSize := uint64(8)
	if size == 1 {
		if len(buf) < 16 {
			return "", nil, nil, false
		}
		size = binary.BigEndian.Uint64(buf[8:16])
		hdrSize = 16
	} else if size == 0 {
		size = uint64(len(buf))
	}
	if size < hdrSize || size > uint64(len(buf)) {
		return "", nil, nil, false
	}
	return fourcc, buf[hdrSize:size], buf[size:], true
}

func parseTkhdSize(b []byte) (int, int) {
	if len(b) < 84 {
		return 0, 0
	}
	ver := b[0]
	off := 76
	if ver == 1 {
		off = 88
	}
	if len(b) < off+8 {
		return 0, 0
	}
	w := int(binary.BigEndian.Uint32(b[off:off+4]) >> 16)
	h := int(binary.BigEndian.Uint32(b[off+4:off+8]) >> 16)
	return w, h
}

func parseMdhd(b []byte) (uint32, uint64) {
	if len(b) < 20 {
		return 0, 0
	}
	ver := b[0]
	if ver == 0 && len(b) >= 20 {
		return binary.BigEndian.Uint32(b[12:16]), uint64(binary.BigEndian.Uint32(b[16:20]))
	}
	if ver == 1 && len(b) >= 32 {
		return binary.BigEndian.Uint32(b[20:24]), binary.BigEndian.Uint64(b[24:32])
	}
	return 0, 0
}

func parseElst(b []byte, movieScale uint32) (ok bool, mediaTime int64, delaySec float64) {
	if len(b) < 8 {
		return false, 0, 0
	}
	ver := b[0]
	count := int(binary.BigEndian.Uint32(b[4:8]))
	p := b[8:]
	for i := 0; i < count; i++ {
		var segDur uint64
		var mt int64
		if ver == 0 {
			if len(p) < 12 {
				break
			}
			segDur = uint64(binary.BigEndian.Uint32(p[0:4]))
			mt = int64(int32(binary.BigEndian.Uint32(p[4:8])))
			p = p[12:]
		} else {
			if len(p) < 20 {
				break
			}
			segDur = binary.BigEndian.Uint64(p[0:8])
			mt = int64(binary.BigEndian.Uint64(p[8:16]))
			p = p[20:]
		}
		if mt == -1 {
			if movieScale > 0 {
				delaySec += float64(segDur) / float64(movieScale)
			}
			continue
		}
		return true, mt, delaySec
	}
	return false, 0, 0
}

func parseStsdVideo(b []byte) (codec videoCodec, w, h int, priv []byte) {
	if len(b) < 16 {
		return videoCodecNone, 0, 0, nil
	}
	fourcc, entry, _, ok := nextMP4Box(b[8:])
	if !ok {
		return videoCodecNone, 0, 0, nil
	}
	switch fourcc {
	case "av01":
		codec = videoCodecAV1
	case "vp09":
		codec = videoCodecVP9
	case "vp08":
		codec = videoCodecVP8
	case "avc1", "avc3":
		codec = videoCodecH264
	case "hvc1", "hev1":
		codec = videoCodecHEVC
	default:
		codec = videoCodecOther
	}
	if len(entry) >= 28 {
		w = int(binary.BigEndian.Uint16(entry[24:26]))
		h = int(binary.BigEndian.Uint16(entry[26:28]))
	}
	if len(entry) > 78 {
		sub := entry[78:]
		for len(sub) >= 8 {
			cf, cb, cr, cok := nextMP4Box(sub)
			if !cok {
				break
			}
			sub = cr
			if cf == "av1C" || cf == "vpcC" || cf == "avcC" || cf == "hvcC" {
				priv = append([]byte(nil), cb...)
			}
		}
	}
	return codec, w, h, priv
}

func parseStsdAudio(b []byte) (codec audioCodec, sampleRate, channels, preSkip int, priv []byte) {
	if len(b) < 16 {
		return audioCodecNone, 0, 0, 0, nil
	}
	fourcc, entry, _, ok := nextMP4Box(b[8:])
	if !ok {
		return audioCodecNone, 0, 0, 0, nil
	}
	switch fourcc {
	case "Opus":
		codec = audioCodecOpus
	case "mp4a":
		codec = audioCodecAAC
	case ".mp3":
		codec = audioCodecMP3
	case "fLaC":
		codec = audioCodecFLAC
	default:
		codec = audioCodecOther
	}
	hdrLen := 28
	if len(entry) >= 28 {
		ver := binary.BigEndian.Uint16(entry[8:10])
		channels = int(binary.BigEndian.Uint16(entry[16:18]))
		sampleRate = int(binary.BigEndian.Uint32(entry[24:28]) >> 16)
		switch ver {
		case 1:
			hdrLen = 44
		case 2:
			hdrLen = 64
		}
	}
	if len(entry) > hdrLen {
		sub := entry[hdrLen:]
		for len(sub) >= 8 {
			cf, cb, cr, cok := nextMP4Box(sub)
			if !cok {
				break
			}
			sub = cr
			if cf == "dOps" && len(cb) >= 11 {
				priv = append([]byte(nil), cb...)
				if cb[1] > 0 {
					channels = int(cb[1])
				}
				preSkip = int(binary.BigEndian.Uint16(cb[2:4]))
				if sr := int(binary.BigEndian.Uint32(cb[4:8])); sr > 0 && sampleRate == 0 {
					sampleRate = sr
				}
			} else if cf == "esds" {
				priv = append([]byte(nil), cb...)
			}
		}
	}
	return codec, sampleRate, channels, preSkip, priv
}

func clampMP4TableCount(raw uint32, bodyLen, hdrLen, entrySize int) int {
	if bodyLen <= hdrLen || entrySize <= 0 {
		return 0
	}
	maxEntries := uint64((bodyLen - hdrLen) / entrySize)
	n := uint64(raw)
	if n > maxEntries {
		n = maxEntries
	}
	return int(n)
}

func parseStts(b []byte) [][2]uint32 {
	if len(b) < 8 {
		return nil
	}
	n := clampMP4TableCount(binary.BigEndian.Uint32(b[4:8]), len(b), 8, 8)
	out := make([][2]uint32, n)
	for i := 0; i < n; i++ {
		off := 8 + i*8
		out[i][0] = binary.BigEndian.Uint32(b[off : off+4])
		out[i][1] = binary.BigEndian.Uint32(b[off+4 : off+8])
	}
	return out
}

func parseCtts(b []byte) [][2]int32 {
	if len(b) < 8 {
		return nil
	}
	n := clampMP4TableCount(binary.BigEndian.Uint32(b[4:8]), len(b), 8, 8)
	out := make([][2]int32, n)
	for i := 0; i < n; i++ {
		off := 8 + i*8
		out[i][0] = int32(binary.BigEndian.Uint32(b[off : off+4]))
		out[i][1] = int32(binary.BigEndian.Uint32(b[off+4 : off+8]))
	}
	return out
}

func parseStsc(b []byte) [][3]uint32 {
	if len(b) < 8 {
		return nil
	}
	n := clampMP4TableCount(binary.BigEndian.Uint32(b[4:8]), len(b), 8, 12)
	out := make([][3]uint32, n)
	for i := 0; i < n; i++ {
		off := 8 + i*12
		out[i][0] = binary.BigEndian.Uint32(b[off : off+4])
		out[i][1] = binary.BigEndian.Uint32(b[off+4 : off+8])
		out[i][2] = binary.BigEndian.Uint32(b[off+8 : off+12])
	}
	return out
}

func parseStsz(b []byte) (constSize, count uint32, sizes []uint32) {
	if len(b) < 12 {
		return 0, 0, nil
	}
	constSize = binary.BigEndian.Uint32(b[4:8])
	count = binary.BigEndian.Uint32(b[8:12])
	if constSize != 0 {
		return constSize, count, nil
	}
	n := clampMP4TableCount(count, len(b), 12, 4)
	sizes = make([]uint32, n)
	for i := 0; i < n; i++ {
		off := 12 + i*4
		sizes[i] = binary.BigEndian.Uint32(b[off : off+4])
	}
	return 0, uint32(n), sizes
}

func parseStco(b []byte) []int64 {
	if len(b) < 8 {
		return nil
	}
	n := clampMP4TableCount(binary.BigEndian.Uint32(b[4:8]), len(b), 8, 4)
	out := make([]int64, n)
	for i := 0; i < n; i++ {
		out[i] = int64(binary.BigEndian.Uint32(b[8+i*4 : 12+i*4]))
	}
	return out
}

func parseCo64(b []byte) []int64 {
	if len(b) < 8 {
		return nil
	}
	n := clampMP4TableCount(binary.BigEndian.Uint32(b[4:8]), len(b), 8, 8)
	out := make([]int64, n)
	for i := 0; i < n; i++ {
		out[i] = int64(binary.BigEndian.Uint64(b[8+i*8 : 16+i*8]))
	}
	return out
}

func parseStss(b []byte) []uint32 {
	if len(b) < 8 {
		return nil
	}
	n := clampMP4TableCount(binary.BigEndian.Uint32(b[4:8]), len(b), 8, 4)
	out := make([]uint32, n)
	for i := 0; i < n; i++ {
		out[i] = binary.BigEndian.Uint32(b[8+i*4 : 12+i*4])
	}
	return out
}
