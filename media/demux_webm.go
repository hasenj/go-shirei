package media

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
)

// Matroska / WebM EBML element IDs.
const (
	ebmlIDHeader       = 0x1A45DFA3
	ebmlIDSegment      = 0x18538067
	ebmlIDSeekHead     = 0x114D9B74
	ebmlIDSeek         = 0x4DBB
	ebmlIDSeekID       = 0x53AB
	ebmlIDSeekPosition = 0x53AC
	ebmlIDInfo         = 0x1549A966
	ebmlIDTimeScale    = 0x2AD7B1
	ebmlIDDuration     = 0x4489
	ebmlIDTracks       = 0x1654AE6B
	ebmlIDTrackEntry   = 0xAE
	ebmlIDTrackNumber  = 0xD7
	ebmlIDTrackType    = 0x83
	ebmlIDDefaultDur   = 0x23E383
	ebmlIDCodecDelay   = 0x56AA
	ebmlIDCodecID      = 0x86
	ebmlIDCodecPrivate = 0x63A2
	ebmlIDVideo        = 0xE0
	ebmlIDPixelWidth   = 0xB0
	ebmlIDPixelHeight  = 0xBA
	ebmlIDAudio        = 0xE1
	ebmlIDSampleFreq   = 0xB5
	ebmlIDChannels     = 0x9F
	ebmlIDCues         = 0x1C53BB6B
	ebmlIDCuePoint     = 0xBB
	ebmlIDCueTime      = 0xB3
	ebmlIDCueTrackPos  = 0xB7
	ebmlIDCueTrack     = 0xF7
	ebmlIDCueCluster   = 0xF1
	ebmlIDCluster      = 0x1F43B675
	ebmlIDClusterTime  = 0xE7
	ebmlIDSimpleBlock  = 0xA3
	ebmlIDBlockGroup   = 0xA0
	ebmlIDBlock        = 0xA1
	ebmlIDRefBlock     = 0xFB
	ebmlIDBlockDur     = 0x9B
)

type webmCue struct {
	timeSec    float64
	clusterPos int64 // byte offset relative to segmentDataOff
}

type webmDemuxer struct {
	f        *os.File
	fileSize int64

	info            mediaProbe
	timeScale       uint64 // nanoseconds per tick (default 1_000_000)
	segmentDataOff  int64
	segmentEnd      int64
	firstClusterOff int64

	videoTrack uint64
	audioTrack uint64
	videoDur   float64 // default frame duration in seconds
	audioDur   float64

	cues []webmCue

	// Active cluster reading state.
	clusterEnd  int64
	clusterTime int64
	inCluster   bool
	pending     []demuxPacket
}

func newWebMDemuxer(f *os.File) (*webmDemuxer, error) {
	st, err := f.Stat()
	if err != nil {
		return nil, err
	}
	d := &webmDemuxer{
		f:         f,
		fileSize:  st.Size(),
		timeScale: 1_000_000,
		info: mediaProbe{
			Container: containerWebM,
		},
	}
	if err := d.parseHeader(); err != nil {
		return nil, err
	}
	return d, nil
}

func (d *webmDemuxer) probe() mediaProbe { return d.info }
func (d *webmDemuxer) close() error      { return d.f.Close() }

func (d *webmDemuxer) parseHeader() error {
	if _, err := d.f.Seek(0, io.SeekStart); err != nil {
		return err
	}
	// Verify EBML header and skip it.
	id, sz, unknown, err := readEBMLElemHeader(d.f)
	if err != nil {
		return err
	}
	if id != ebmlIDHeader || unknown {
		return errors.New("media: invalid EBML header")
	}
	if _, err := d.f.Seek(sz, io.SeekCurrent); err != nil {
		return err
	}

	// Find Segment.
	for {
		id, sz, unknown, err = readEBMLElemHeader(d.f)
		if err != nil {
			return fmt.Errorf("media: missing EBML Segment: %w", err)
		}
		pos, _ := d.f.Seek(0, io.SeekCurrent)
		if id == ebmlIDSegment {
			d.segmentDataOff = pos
			if unknown || pos+sz > d.fileSize {
				d.segmentEnd = d.fileSize
			} else {
				d.segmentEnd = pos + sz
			}
			break
		}
		if unknown {
			return errors.New("media: unbounded element before Segment")
		}
		if _, err := d.f.Seek(pos+sz, io.SeekStart); err != nil {
			return err
		}
	}

	var cuesPos int64 = -1
	var parsedInfo, parsedTracks, parsedCues bool

	// Scan top-level Segment children until the first Cluster.
	for {
		elemStart, _ := d.f.Seek(0, io.SeekCurrent)
		if elemStart >= d.segmentEnd {
			break
		}
		id, sz, unknown, err := readEBMLElemHeader(d.f)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				break
			}
			return err
		}
		dataStart, _ := d.f.Seek(0, io.SeekCurrent)
		elemEnd := d.segmentEnd
		if !unknown && dataStart+sz < elemEnd {
			elemEnd = dataStart + sz
		}

		switch id {
		case ebmlIDSeekHead:
			if !unknown && sz > 0 && sz <= 16<<20 && sz <= d.segmentEnd-dataStart {
				buf := make([]byte, sz)
				if _, err := io.ReadFull(d.f, buf); err == nil {
					if cp := parseSeekHeadCues(buf); cp >= 0 {
						cuesPos = cp
					}
				}
			}
		case ebmlIDInfo:
			if !unknown && sz > 0 && sz <= 16<<20 && sz <= d.segmentEnd-dataStart {
				buf := make([]byte, sz)
				if _, err := io.ReadFull(d.f, buf); err != nil {
					return err
				}
				d.parseInfo(buf)
				parsedInfo = true
			}
		case ebmlIDTracks:
			if !unknown && sz > 0 && sz <= 16<<20 && sz <= d.segmentEnd-dataStart {
				buf := make([]byte, sz)
				if _, err := io.ReadFull(d.f, buf); err != nil {
					return err
				}
				d.parseTracks(buf)
				parsedTracks = true
			}
		case ebmlIDCues:
			if !unknown && sz > 0 && sz <= 16<<20 && sz <= d.segmentEnd-dataStart {
				buf := make([]byte, sz)
				if _, err := io.ReadFull(d.f, buf); err == nil {
					d.parseCues(buf)
					parsedCues = true
				}
			}
		case ebmlIDCluster:
			d.firstClusterOff = elemStart
			goto doneScan
		}
		if unknown {
			break
		}
		if _, err := d.f.Seek(elemEnd, io.SeekStart); err != nil {
			return err
		}
	}

doneScan:
	if !parsedTracks || (d.videoTrack == 0 && d.audioTrack == 0) {
		return errors.New("media: webm/mkv contains no video or audio tracks")
	}

	// If Cues lives after the clusters (standard ffmpeg output), jump via SeekHead.
	if !parsedCues && cuesPos >= 0 {
		absOff := d.segmentDataOff + cuesPos
		if absOff > 0 && absOff < d.segmentEnd {
			if _, err := d.f.Seek(absOff, io.SeekStart); err == nil {
				id, sz, unknown, err := readEBMLElemHeader(d.f)
				if err == nil && id == ebmlIDCues && !unknown && sz > 0 && sz <= 16<<20 {
					buf := make([]byte, sz)
					if _, err := io.ReadFull(d.f, buf); err == nil {
						d.parseCues(buf)
					}
				}
			}
		}
	}

	// Recompute cue timestamps using final timeScale and filter/sort.
	if len(d.cues) > 0 {
		scale := float64(d.timeScale) / 1e9
		for i := range d.cues {
			d.cues[i].timeSec *= scale
		}
		sort.Slice(d.cues, func(i, j int) bool {
			return d.cues[i].timeSec < d.cues[j].timeSec
		})
	}
	if parsedInfo && d.info.Duration > 0 {
		d.info.Duration *= float64(d.timeScale) / 1e9
	}
	if d.info.FPS <= 0 && d.videoDur > 0 {
		d.info.FPS = 1.0 / d.videoDur
	}
	if d.info.FPS <= 0 && d.videoTrack != 0 {
		d.info.FPS = 30
	}

	// Position file at the first cluster.
	if d.firstClusterOff == 0 {
		d.firstClusterOff = d.segmentDataOff
	}
	if _, err := d.f.Seek(d.firstClusterOff, io.SeekStart); err != nil {
		return err
	}
	d.inCluster = false
	return nil
}

func parseSeekHeadCues(data []byte) int64 {
	for len(data) > 0 {
		id, val, rest, ok := nextEBMLBuf(data)
		if !ok {
			break
		}
		data = rest
		if id != ebmlIDSeek {
			continue
		}
		var targetID uint64
		var pos int64 = -1
		for len(val) > 0 {
			cid, cval, crest, cok := nextEBMLBuf(val)
			if !cok {
				break
			}
			val = crest
			switch cid {
			case ebmlIDSeekID:
				targetID = readUintBE(cval)
			case ebmlIDSeekPosition:
				pos = int64(readUintBE(cval))
			}
		}
		if targetID == ebmlIDCues && pos >= 0 {
			return pos
		}
	}
	return -1
}

func (d *webmDemuxer) parseInfo(data []byte) {
	for len(data) > 0 {
		id, val, rest, ok := nextEBMLBuf(data)
		if !ok {
			break
		}
		data = rest
		switch id {
		case ebmlIDTimeScale:
			if v := readUintBE(val); v > 0 {
				d.timeScale = v
			}
		case ebmlIDDuration:
			d.info.Duration = readFloatBE(val)
		}
	}
}

func (d *webmDemuxer) parseTracks(data []byte) {
	for len(data) > 0 {
		id, val, rest, ok := nextEBMLBuf(data)
		if !ok {
			break
		}
		data = rest
		if id != ebmlIDTrackEntry {
			continue
		}
		var (
			trackNum   uint64
			trackType  uint64
			codecID    string
			priv       []byte
			defDur     uint64
			codecDelay uint64
			w, h       int
			sr, ch     int
		)
		for len(val) > 0 {
			cid, cval, crest, cok := nextEBMLBuf(val)
			if !cok {
				break
			}
			val = crest
			switch cid {
			case ebmlIDTrackNumber:
				trackNum = readUintBE(cval)
			case ebmlIDTrackType:
				trackType = readUintBE(cval)
			case ebmlIDCodecID:
				codecID = string(cval)
			case ebmlIDCodecPrivate:
				priv = append([]byte(nil), cval...)
			case ebmlIDDefaultDur:
				defDur = readUintBE(cval)
			case ebmlIDCodecDelay:
				codecDelay = readUintBE(cval)
			case ebmlIDVideo:
				for len(cval) > 0 {
					vid, vval, vrest, vok := nextEBMLBuf(cval)
					if !vok {
						break
					}
					cval = vrest
					switch vid {
					case ebmlIDPixelWidth:
						w = int(readUintBE(vval))
					case ebmlIDPixelHeight:
						h = int(readUintBE(vval))
					}
				}
			case ebmlIDAudio:
				for len(cval) > 0 {
					aid, aval, arest, aok := nextEBMLBuf(cval)
					if !aok {
						break
					}
					cval = arest
					switch aid {
					case ebmlIDSampleFreq:
						sr = int(math.Round(readFloatBE(aval)))
					case ebmlIDChannels:
						ch = int(readUintBE(aval))
					}
				}
			}
		}

		if trackType == 1 && d.videoTrack == 0 && trackNum != 0 {
			d.videoTrack = trackNum
			d.info.VideoCodec = mapWebMVideoCodec(codecID)
			d.info.Width = w
			d.info.Height = h
			d.info.VideoPrivate = priv
			if defDur > 0 {
				d.videoDur = float64(defDur) / 1e9
				d.info.FPS = 1e9 / float64(defDur)
			}
		} else if trackType == 2 && d.audioTrack == 0 && trackNum != 0 {
			d.audioTrack = trackNum
			d.info.AudioCodec = mapWebMAudioCodec(codecID)
			d.info.SampleRate = sr
			d.info.Channels = ch
			d.info.AudioPrivate = priv
			if defDur > 0 {
				d.audioDur = float64(defDur) / 1e9
			}
			if d.info.AudioCodec == audioCodecOpus {
				if len(priv) >= 12 && string(priv[:8]) == "OpusHead" {
					d.info.AudioPreSkip = int(binary.LittleEndian.Uint16(priv[10:12]))
					if d.info.Channels == 0 && priv[9] > 0 {
						d.info.Channels = int(priv[9])
					}
				} else if codecDelay > 0 {
					d.info.AudioPreSkip = int(math.Round(float64(codecDelay) * 48000.0 / 1e9))
				}
				if d.info.SampleRate == 0 {
					d.info.SampleRate = 48000
				}
			}
			if d.info.Channels == 0 {
				d.info.Channels = 2
			}
		}
	}
}

func (d *webmDemuxer) parseCues(data []byte) {
	wantTrack := d.videoTrack
	if wantTrack == 0 {
		wantTrack = d.audioTrack
	}
	for len(data) > 0 {
		id, val, rest, ok := nextEBMLBuf(data)
		if !ok {
			break
		}
		data = rest
		if id != ebmlIDCuePoint {
			continue
		}
		var cueTicks uint64
		var clusterPos int64 = -1
		var matched bool
		for len(val) > 0 {
			cid, cval, crest, cok := nextEBMLBuf(val)
			if !cok {
				break
			}
			val = crest
			switch cid {
			case ebmlIDCueTime:
				cueTicks = readUintBE(cval)
			case ebmlIDCueTrackPos:
				var trk uint64
				var cpos int64 = -1
				for len(cval) > 0 {
					pid, pval, prest, pok := nextEBMLBuf(cval)
					if !pok {
						break
					}
					cval = prest
					switch pid {
					case ebmlIDCueTrack:
						trk = readUintBE(pval)
					case ebmlIDCueCluster:
						cpos = int64(readUintBE(pval))
					}
				}
				if (trk == wantTrack || wantTrack == 0) && cpos >= 0 {
					clusterPos = cpos
					matched = true
				}
			}
		}
		if matched {
			d.cues = append(d.cues, webmCue{
				timeSec:    float64(cueTicks), // scaled in parseHeader
				clusterPos: clusterPos,
			})
		}
	}
}

func (d *webmDemuxer) seek(sec float64) error {
	if sec < 0 {
		sec = 0
	}
	d.pending = d.pending[:0]
	d.inCluster = false

	if sec <= 0.001 {
		_, err := d.f.Seek(d.firstClusterOff, io.SeekStart)
		return err
	}

	if len(d.cues) > 0 {
		idx := sort.Search(len(d.cues), func(i int) bool {
			return d.cues[i].timeSec > sec
		})
		if idx > 0 {
			idx--
		}
		off := d.segmentDataOff + d.cues[idx].clusterPos
		if off >= d.segmentDataOff && off < d.segmentEnd {
			_, err := d.f.Seek(off, io.SeekStart)
			return err
		}
	}

	// Fallback without Cues: scan cluster headers linearly to find last cluster <= sec.
	if _, err := d.f.Seek(d.firstClusterOff, io.SeekStart); err != nil {
		return err
	}
	scale := float64(d.timeScale) / 1e9
	bestOff := d.firstClusterOff
	for {
		elemStart, _ := d.f.Seek(0, io.SeekCurrent)
		if elemStart >= d.segmentEnd {
			break
		}
		id, sz, unknown, err := readEBMLElemHeader(d.f)
		if err != nil {
			break
		}
		dataStart, _ := d.f.Seek(0, io.SeekCurrent)
		if id == ebmlIDCluster {
			// Scan cluster children until Cluster Timestamp (Position/PrevSize may precede it).
			clusterEnd := d.segmentEnd
			if !unknown && sz >= 0 && sz <= d.segmentEnd-dataStart {
				clusterEnd = dataStart + sz
			}
			passedTarget := false
			for {
				cPos, _ := d.f.Seek(0, io.SeekCurrent)
				if cPos >= clusterEnd {
					break
				}
				cid, csz, cunk, cerr := readEBMLElemHeader(d.f)
				if cerr != nil {
					break
				}
				cDataStart, _ := d.f.Seek(0, io.SeekCurrent)
				if cid == ebmlIDClusterTime && !cunk && csz >= 0 && csz <= 8 {
					var tb [8]byte
					if _, err := io.ReadFull(d.f, tb[:csz]); err == nil {
						tSec := float64(readUintBE(tb[:csz])) * scale
						if tSec <= sec {
							bestOff = elemStart
						} else {
							passedTarget = true
						}
					}
					break
				}
				if cid == ebmlIDSimpleBlock || cid == ebmlIDBlockGroup || cunk || csz < 0 || csz > clusterEnd-cDataStart {
					break
				}
				if _, err := d.f.Seek(cDataStart+csz, io.SeekStart); err != nil {
					break
				}
			}
			if passedTarget || unknown {
				break
			}
			if _, err := d.f.Seek(clusterEnd, io.SeekStart); err != nil {
				break
			}
			continue
		}
		if unknown {
			break
		}
		if _, err := d.f.Seek(dataStart+sz, io.SeekStart); err != nil {
			break
		}
	}
	_, err := d.f.Seek(bestOff, io.SeekStart)
	return err
}

func (d *webmDemuxer) popPending() demuxPacket {
	pkt := d.pending[0]
	d.pending[0] = demuxPacket{}
	d.pending = d.pending[1:]
	if len(d.pending) == 0 {
		d.pending = d.pending[:0]
	}
	return pkt
}

func (d *webmDemuxer) nextPacket() (demuxPacket, error) {
	if len(d.pending) > 0 {
		return d.popPending(), nil
	}

	scale := float64(d.timeScale) / 1e9

	for {
		pos, err := d.f.Seek(0, io.SeekCurrent)
		if err != nil {
			return demuxPacket{}, err
		}
		if pos >= d.segmentEnd {
			return demuxPacket{}, io.EOF
		}
		if d.inCluster && pos >= d.clusterEnd {
			d.inCluster = false
		}

		id, sz, unknown, err := readEBMLElemHeader(d.f)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				return demuxPacket{}, io.EOF
			}
			return demuxPacket{}, err
		}
		dataStart, _ := d.f.Seek(0, io.SeekCurrent)
		elemEnd := d.segmentEnd
		if !unknown && dataStart+sz < elemEnd {
			elemEnd = dataStart + sz
		}

		if id == ebmlIDCluster {
			d.inCluster = true
			d.clusterTime = 0
			d.clusterEnd = elemEnd
			continue
		}

		if !d.inCluster {
			if unknown {
				return demuxPacket{}, io.EOF
			}
			if _, err := d.f.Seek(elemEnd, io.SeekStart); err != nil {
				return demuxPacket{}, err
			}
			continue
		}

		switch id {
		case ebmlIDClusterTime:
			if !unknown && sz >= 0 && sz <= 8 {
				var tb [8]byte
				if _, err := io.ReadFull(d.f, tb[:sz]); err != nil {
					if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
						return demuxPacket{}, io.EOF
					}
					return demuxPacket{}, err
				}
				d.clusterTime = int64(readUintBE(tb[:sz]))
				continue
			}
		case ebmlIDSimpleBlock:
			if !unknown && sz > 0 && sz <= 64<<20 {
				buf := make([]byte, sz)
				if _, err := io.ReadFull(d.f, buf); err != nil {
					if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
						return demuxPacket{}, io.EOF
					}
					return demuxPacket{}, err
				}
				if d.decodeBlock(buf, true, false, 0, scale) {
					return d.popPending(), nil
				}
				continue
			}
		case ebmlIDBlockGroup:
			if !unknown && sz > 0 && sz <= 64<<20 {
				buf := make([]byte, sz)
				if _, err := io.ReadFull(d.f, buf); err != nil {
					if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
						return demuxPacket{}, io.EOF
					}
					return demuxPacket{}, err
				}
				var blockData []byte
				hasRef := false
				var blockDur float64
				rem := buf
				for len(rem) > 0 {
					cid, cval, crest, cok := nextEBMLBuf(rem)
					if !cok {
						break
					}
					rem = crest
					switch cid {
					case ebmlIDBlock:
						blockData = cval
					case ebmlIDRefBlock:
						hasRef = true
					case ebmlIDBlockDur:
						blockDur = float64(readUintBE(cval)) * scale
					}
				}
				if len(blockData) > 0 && d.decodeBlock(blockData, false, !hasRef, blockDur, scale) {
					return d.popPending(), nil
				}
				continue
			}
		default:
			// If we hit a top-level element inside an unknown-sized Cluster, exit cluster.
			if isTopLevelSegmentChild(id) {
				d.inCluster = false
			}
		}

		if unknown {
			return demuxPacket{}, io.EOF
		}
		if _, err := d.f.Seek(elemEnd, io.SeekStart); err != nil {
			return demuxPacket{}, err
		}
	}
}

func (d *webmDemuxer) decodeBlock(buf []byte, isSimple bool, bgKey bool, blockDur, scale float64) bool {
	trackNum, n, ok := readVINTValueBuf(buf)
	if !ok || len(buf) < n+3 {
		return false
	}
	isVideo := trackNum == d.videoTrack && d.videoTrack != 0
	isAudio := trackNum == d.audioTrack && d.audioTrack != 0
	if !isVideo && !isAudio {
		return false
	}

	relTime := int16(binary.BigEndian.Uint16(buf[n : n+2]))
	flags := buf[n+2]
	payload := buf[n+3:]

	keyframe := bgKey
	if isSimple {
		keyframe = (flags & 0x80) != 0
	}
	pts := float64(d.clusterTime+int64(relTime)) * scale
	if pts < 0 {
		pts = 0
	}
	dur := blockDur
	if dur <= 0 {
		if isVideo {
			dur = d.videoDur
		} else {
			dur = d.audioDur
		}
	}

	lacing := (flags >> 1) & 0x03
	if lacing == 0 {
		d.pending = append(d.pending, demuxPacket{
			IsVideo:  isVideo,
			Keyframe: keyframe,
			PTS:      pts,
			Duration: dur,
			Data:     payload,
		})
		return true
	}

	frames := splitLacedFrames(payload, lacing)
	if len(frames) == 0 {
		return false
	}
	step := dur
	if blockDur > 0 && len(frames) > 1 {
		step = blockDur / float64(len(frames))
	}
	for i, fr := range frames {
		d.pending = append(d.pending, demuxPacket{
			IsVideo:  isVideo,
			Keyframe: keyframe && i == 0,
			PTS:      pts + float64(i)*step,
			Duration: step,
			Data:     fr,
		})
	}
	return true
}

func splitLacedFrames(payload []byte, lacing byte) [][]byte {
	if len(payload) < 1 {
		return nil
	}
	numFrames := int(payload[0]) + 1
	p := payload[1:]
	if numFrames <= 1 {
		return [][]byte{p}
	}

	switch lacing {
	case 1: // Xiph lacing
		sizes := make([]int, numFrames)
		var total int64
		for i := 0; i < numFrames-1; i++ {
			var sz int64
			for {
				if len(p) == 0 {
					return nil
				}
				b := int64(p[0])
				p = p[1:]
				sz += b
				if sz > int64(len(p)) {
					return nil
				}
				if b < 255 {
					break
				}
			}
			sizes[i] = int(sz)
			total += sz
			if total > int64(len(p)) {
				return nil
			}
		}
		sizes[numFrames-1] = len(p) - int(total)
		return sliceBySizes(p, sizes)

	case 2: // Fixed-size lacing
		if len(p)%numFrames != 0 {
			return nil
		}
		frameSize := len(p) / numFrames
		out := make([][]byte, numFrames)
		for i := 0; i < numFrames; i++ {
			out[i] = p[i*frameSize : (i+1)*frameSize]
		}
		return out

	case 3: // EBML lacing
		sizes := make([]int, numFrames)
		first, n, ok := readVINTValueBuf(p)
		if !ok {
			return nil
		}
		p = p[n:]
		if first > uint64(len(p)) {
			return nil
		}
		sizes[0] = int(first)
		prev := int64(first)
		total := int64(first)
		for i := 1; i < numFrames-1; i++ {
			uval, vn, vok := readVINTValueBuf(p)
			if !vok {
				return nil
			}
			p = p[vn:]
			bias := int64((uint64(1) << (7*vn - 1)) - 1)
			delta := int64(uval) - bias
			prev += delta
			if prev < 0 || prev > int64(len(p)) {
				return nil
			}
			sizes[i] = int(prev)
			total += prev
			if total > int64(len(p)) {
				return nil
			}
		}
		sizes[numFrames-1] = len(p) - int(total)
		return sliceBySizes(p, sizes)
	}
	return nil
}

func sliceBySizes(p []byte, sizes []int) [][]byte {
	out := make([][]byte, len(sizes))
	off := 0
	for i, sz := range sizes {
		if sz < 0 || sz > len(p)-off {
			return nil
		}
		out[i] = p[off : off+sz]
		off += sz
	}
	return out
}

func isTopLevelSegmentChild(id uint64) bool {
	switch id {
	case ebmlIDSeekHead, ebmlIDInfo, ebmlIDTracks, ebmlIDCues, ebmlIDCluster,
		0x1254C367, // Tags
		0x1043A770, // Chapters
		0x1941A469: // Attachments
		return true
	}
	return false
}

func mapWebMVideoCodec(id string) videoCodec {
	switch id {
	case "V_AV1":
		return videoCodecAV1
	case "V_VP9":
		return videoCodecVP9
	case "V_VP8":
		return videoCodecVP8
	case "V_MPEG4/ISO/AVC":
		return videoCodecH264
	case "V_MPEGH/ISO/HEVC":
		return videoCodecHEVC
	default:
		return videoCodecOther
	}
}

func mapWebMAudioCodec(id string) audioCodec {
	switch id {
	case "A_OPUS":
		return audioCodecOpus
	case "A_VORBIS":
		return audioCodecVorbis
	case "A_AAC", "A_AAC/MPEG4/LC", "A_AAC/MPEG4/SBR", "A_AAC/MPEG2/LC":
		return audioCodecAAC
	case "A_MPEG/L3":
		return audioCodecMP3
	case "A_FLAC":
		return audioCodecFLAC
	default:
		return audioCodecOther
	}
}

// -----------------------------------------------------------------------------
// EBML VINT & primitive readers

func readEBMLElemHeader(r io.Reader) (id uint64, size int64, unknown bool, err error) {
	id, err = readVINTReader(r, true)
	if err != nil {
		return 0, 0, false, err
	}
	var sz uint64
	sz, unknown, err = readVINTSizeReader(r)
	if err != nil {
		return 0, 0, false, err
	}
	return id, int64(sz), unknown, nil
}

func readVINTReader(r io.Reader, keepMarker bool) (uint64, error) {
	var b [8]byte
	if _, err := io.ReadFull(r, b[:1]); err != nil {
		return 0, err
	}
	first := b[0]
	if first == 0 {
		return 0, errors.New("media: invalid EBML VINT")
	}
	length := 1
	mask := byte(0x80)
	for (first & mask) == 0 {
		mask >>= 1
		length++
		if length > 8 {
			return 0, errors.New("media: EBML VINT overflow")
		}
	}
	if length > 1 {
		if _, err := io.ReadFull(r, b[1:length]); err != nil {
			return 0, err
		}
	}
	var val uint64
	if keepMarker {
		val = uint64(first)
	} else {
		val = uint64(first & (mask - 1))
	}
	for i := 1; i < length; i++ {
		val = (val << 8) | uint64(b[i])
	}
	return val, nil
}

func readVINTSizeReader(r io.Reader) (uint64, bool, error) {
	var b [8]byte
	if _, err := io.ReadFull(r, b[:1]); err != nil {
		return 0, false, err
	}
	first := b[0]
	if first == 0 {
		return 0, false, errors.New("media: invalid EBML size")
	}
	length := 1
	mask := byte(0x80)
	for (first & mask) == 0 {
		mask >>= 1
		length++
		if length > 8 {
			return 0, false, errors.New("media: EBML size overflow")
		}
	}
	if length > 1 {
		if _, err := io.ReadFull(r, b[1:length]); err != nil {
			return 0, false, err
		}
	}
	val := uint64(first & (mask - 1))
	for i := 1; i < length; i++ {
		val = (val << 8) | uint64(b[i])
	}
	allOnes := (uint64(1) << (7 * length)) - 1
	return val, val == allOnes, nil
}

func readVINTValueBuf(buf []byte) (uint64, int, bool) {
	if len(buf) == 0 || buf[0] == 0 {
		return 0, 0, false
	}
	first := buf[0]
	length := 1
	mask := byte(0x80)
	for (first & mask) == 0 {
		mask >>= 1
		length++
		if length > 8 || length > len(buf) {
			return 0, 0, false
		}
	}
	val := uint64(first & (mask - 1))
	for i := 1; i < length; i++ {
		val = (val << 8) | uint64(buf[i])
	}
	return val, length, true
}

func readVINTIDBuf(buf []byte) (uint64, int, bool) {
	if len(buf) == 0 || buf[0] == 0 {
		return 0, 0, false
	}
	first := buf[0]
	length := 1
	mask := byte(0x80)
	for (first & mask) == 0 {
		mask >>= 1
		length++
		if length > 4 || length > len(buf) {
			return 0, 0, false
		}
	}
	val := uint64(first)
	for i := 1; i < length; i++ {
		val = (val << 8) | uint64(buf[i])
	}
	return val, length, true
}

func nextEBMLBuf(buf []byte) (id uint64, val, rest []byte, ok bool) {
	id, n1, ok := readVINTIDBuf(buf)
	if !ok {
		return 0, nil, nil, false
	}
	sz, n2, ok := readVINTValueBuf(buf[n1:])
	if !ok {
		return 0, nil, nil, false
	}
	start := n1 + n2
	if sz > uint64(len(buf)-start) {
		return 0, nil, nil, false
	}
	end := start + int(sz)
	return id, buf[start:end], buf[end:], true
}

func readUintBE(b []byte) uint64 {
	var v uint64
	for _, x := range b {
		v = (v << 8) | uint64(x)
	}
	return v
}

func readFloatBE(b []byte) float64 {
	switch len(b) {
	case 4:
		return float64(math.Float32frombits(binary.BigEndian.Uint32(b)))
	case 8:
		return math.Float64frombits(binary.BigEndian.Uint64(b))
	default:
		return 0
	}
}
