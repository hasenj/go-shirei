package media

import (
	"errors"
	"fmt"
	"io"
	"os"
)

type containerKind int

const (
	containerUnknown containerKind = iota
	containerWebM                  // WebM or Matroska (.webm, .mkv)
	containerMP4                   // ISO BMFF (.mp4, .m4v, .mov)
)

type videoCodec int

const (
	videoCodecNone videoCodec = iota
	videoCodecAV1
	videoCodecVP9
	videoCodecVP8
	videoCodecH264
	videoCodecHEVC
	videoCodecOther
)

func (c videoCodec) String() string {
	switch c {
	case videoCodecAV1:
		return "av1"
	case videoCodecVP9:
		return "vp9"
	case videoCodecVP8:
		return "vp8"
	case videoCodecH264:
		return "h264"
	case videoCodecHEVC:
		return "hevc"
	case videoCodecOther:
		return "other"
	default:
		return "none"
	}
}

type audioCodec int

const (
	audioCodecNone audioCodec = iota
	audioCodecOpus
	audioCodecVorbis
	audioCodecAAC
	audioCodecMP3
	audioCodecFLAC
	audioCodecOther
)

func (c audioCodec) String() string {
	switch c {
	case audioCodecOpus:
		return "opus"
	case audioCodecVorbis:
		return "vorbis"
	case audioCodecAAC:
		return "aac"
	case audioCodecMP3:
		return "mp3"
	case audioCodecFLAC:
		return "flac"
	case audioCodecOther:
		return "other"
	default:
		return "none"
	}
}

type mediaProbe struct {
	Container    containerKind
	VideoCodec   videoCodec
	AudioCodec   audioCodec
	Width        int
	Height       int
	FPS          float64
	Duration     float64
	SampleRate   int
	Channels     int
	VideoPrivate []byte
	AudioPrivate []byte
	AudioPreSkip int
}

type demuxPacket struct {
	IsVideo  bool
	Keyframe bool
	PTS      float64 // presentation timestamp in seconds
	Duration float64 // optional packet duration in seconds
	Data     []byte
}

type demuxer interface {
	probe() mediaProbe
	nextPacket() (demuxPacket, error)
	seek(sec float64) error
	close() error
}

var errUnsupportedContainer = errors.New("media: unsupported container")

// openDemuxer opens path with a pure-Go WebM/Matroska or MP4 demuxer.
func openDemuxer(path string) (demuxer, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	var magic [12]byte
	n, err := io.ReadFull(f, magic[:])
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		f.Close()
		return nil, err
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		f.Close()
		return nil, err
	}
	if n >= 4 && magic[0] == 0x1A && magic[1] == 0x45 && magic[2] == 0xDF && magic[3] == 0xA3 {
		d, err := newWebMDemuxer(f)
		if err != nil {
			f.Close()
			return nil, err
		}
		return d, nil
	}
	if n >= 8 && looksLikeMP4(magic[:n]) {
		d, err := newMP4Demuxer(f)
		if err != nil {
			f.Close()
			return nil, err
		}
		return d, nil
	}
	f.Close()
	return nil, fmt.Errorf("%w: %s", errUnsupportedContainer, path)
}

// sniffFile inspects path's container and track codecs without decoding frames.
func sniffFile(path string) (mediaProbe, bool) {
	d, err := openDemuxer(path)
	if err != nil {
		return mediaProbe{}, false
	}
	p := d.probe()
	_ = d.close()
	return p, true
}

func looksLikeMP4(hdr []byte) bool {
	if len(hdr) < 8 {
		return false
	}
	switch string(hdr[4:8]) {
	case "ftyp", "moov", "mdat", "free", "skip", "wide", "pnot":
		return true
	default:
		return false
	}
}
