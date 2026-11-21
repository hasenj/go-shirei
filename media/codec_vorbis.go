//go:build (darwin && !ios) || (linux && !android) || windows

package media

import (
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"unsafe"
)

// Opaque buffer sizes generously exceeding libvorbis struct sizes on all 64-bit OSes.
const (
	vorbisInfoBufSize    = 128
	vorbisCommentBufSize = 64
	vorbisDSPBufSize     = 256
	vorbisBlockBufSize   = 320
	oggPacketBufSize     = 64
)

var (
	vorbisOnce sync.Once
	vorbisErr  error

	vorbisInfoInit         func(vi unsafe.Pointer)
	vorbisInfoClear        func(vi unsafe.Pointer)
	vorbisCommentInit      func(vc unsafe.Pointer)
	vorbisCommentClear     func(vc unsafe.Pointer)
	vorbisSynthesisHdrIn   func(vi, vc, op unsafe.Pointer) int32
	vorbisSynthesisInit    func(vd, vi unsafe.Pointer) int32
	vorbisBlockInit        func(vd, vb unsafe.Pointer) int32
	vorbisSynthesis        func(vb, op unsafe.Pointer) int32
	vorbisSynthesisBlockIn func(vd, vb unsafe.Pointer) int32
	vorbisSynthesisPCMOut  func(vd unsafe.Pointer, pcm *uintptr) int32
	vorbisSynthesisRead    func(vd unsafe.Pointer, samples int32) int32
	vorbisSynthesisRestart func(vd unsafe.Pointer) int32
	vorbisBlockClear       func(vb unsafe.Pointer) int32
	vorbisDSPClear         func(vd unsafe.Pointer)
)

func init() {
	registerAudioDecoder(audioCodecVorbis, vorbisAvailable, newVorbisDecoder)
}

func loadVorbis() error {
	vorbisOnce.Do(func() {
		if unsafe.Sizeof(uintptr(0)) != 8 {
			vorbisErr = errors.New("media: libvorbis purego bindings require 64-bit platform")
			return
		}
		var names []string
		switch runtime.GOOS {
		case "darwin":
			names = []string{"libvorbis.0.dylib", "libvorbis.dylib"}
		case "windows":
			names = []string{"vorbis.dll", "libvorbis-0.dll", "libvorbis.dll"}
		default:
			names = []string{"libvorbis.so.0", "libvorbis.so"}
		}
		h, err := openCodecLib(names)
		if err != nil {
			vorbisErr = err
			return
		}
		syms := []struct {
			fn   any
			name string
		}{
			{&vorbisInfoInit, "vorbis_info_init"},
			{&vorbisInfoClear, "vorbis_info_clear"},
			{&vorbisCommentInit, "vorbis_comment_init"},
			{&vorbisCommentClear, "vorbis_comment_clear"},
			{&vorbisSynthesisHdrIn, "vorbis_synthesis_headerin"},
			{&vorbisSynthesisInit, "vorbis_synthesis_init"},
			{&vorbisBlockInit, "vorbis_block_init"},
			{&vorbisSynthesis, "vorbis_synthesis"},
			{&vorbisSynthesisBlockIn, "vorbis_synthesis_blockin"},
			{&vorbisSynthesisPCMOut, "vorbis_synthesis_pcmout"},
			{&vorbisSynthesisRead, "vorbis_synthesis_read"},
			{&vorbisSynthesisRestart, "vorbis_synthesis_restart"},
			{&vorbisBlockClear, "vorbis_block_clear"},
			{&vorbisDSPClear, "vorbis_dsp_clear"},
		}
		for _, s := range syms {
			if err := registerCodecFunc(s.fn, h, s.name); err != nil {
				vorbisErr = fmt.Errorf("media: libvorbis symbol %s: %w", s.name, err)
				return
			}
		}
	})
	return vorbisErr
}

func vorbisAvailable() bool {
	return loadVorbis() == nil
}

type vorbisDecoder struct {
	vi     [vorbisInfoBufSize]byte
	vc     [vorbisCommentBufSize]byte
	vd     [vorbisDSPBufSize]byte
	vb     [vorbisBlockBufSize]byte
	rate   int
	ch     int
	pktNo  int64
	inited bool
	pcmOut []float32
}

func newVorbisDecoder(p mediaProbe) (audioDecoder, error) {
	if err := loadVorbis(); err != nil {
		return nil, err
	}
	headers, err := splitVorbisCodecPrivate(p.AudioPrivate)
	if err != nil {
		return nil, err
	}

	d := &vorbisDecoder{}
	vorbisInfoInit(unsafe.Pointer(&d.vi[0]))
	vorbisCommentInit(unsafe.Pointer(&d.vc[0]))

	var op [oggPacketBufSize]byte
	for i, hdr := range headers {
		fillOggPacket(&op, hdr, i == 0, int64(i))
		rc := vorbisSynthesisHdrIn(unsafe.Pointer(&d.vi[0]), unsafe.Pointer(&d.vc[0]), unsafe.Pointer(&op[0]))
		runtime.KeepAlive(hdr)
		if rc != 0 {
			vorbisCommentClear(unsafe.Pointer(&d.vc[0]))
			vorbisInfoClear(unsafe.Pointer(&d.vi[0]))
			return nil, fmt.Errorf("media: vorbis_synthesis_headerin[%d] failed (%d)", i, rc)
		}
	}

	d.ch = int(int32(binary.LittleEndian.Uint32(d.vi[4:8])))
	if runtime.GOOS == "windows" {
		d.rate = int(int32(binary.LittleEndian.Uint32(d.vi[8:12])))
	} else {
		d.rate = int(binary.LittleEndian.Uint64(d.vi[8:16]))
	}
	if d.ch <= 0 {
		d.ch = p.Channels
	}
	if d.ch <= 0 {
		d.ch = 2
	}
	if d.rate <= 0 {
		d.rate = p.SampleRate
	}
	if d.rate <= 0 {
		d.rate = 44100
	}

	if rc := vorbisSynthesisInit(unsafe.Pointer(&d.vd[0]), unsafe.Pointer(&d.vi[0])); rc != 0 {
		vorbisCommentClear(unsafe.Pointer(&d.vc[0]))
		vorbisInfoClear(unsafe.Pointer(&d.vi[0]))
		return nil, fmt.Errorf("media: vorbis_synthesis_init failed (%d)", rc)
	}
	if rc := vorbisBlockInit(unsafe.Pointer(&d.vd[0]), unsafe.Pointer(&d.vb[0])); rc != 0 {
		vorbisDSPClear(unsafe.Pointer(&d.vd[0]))
		vorbisCommentClear(unsafe.Pointer(&d.vc[0]))
		vorbisInfoClear(unsafe.Pointer(&d.vi[0]))
		return nil, fmt.Errorf("media: vorbis_block_init failed (%d)", rc)
	}
	d.pktNo = 3
	d.inited = true
	return d, nil
}

func (d *vorbisDecoder) sampleRate() int { return d.rate }

func (d *vorbisDecoder) decode(pkt demuxPacket) ([]float32, error) {
	if !d.inited || len(pkt.Data) == 0 {
		return nil, nil
	}
	var op [oggPacketBufSize]byte
	fillOggPacket(&op, pkt.Data, false, d.pktNo)
	d.pktNo++

	rc := vorbisSynthesis(unsafe.Pointer(&d.vb[0]), unsafe.Pointer(&op[0]))
	runtime.KeepAlive(pkt.Data)
	if rc != 0 {
		return nil, nil
	}
	if vorbisSynthesisBlockIn(unsafe.Pointer(&d.vd[0]), unsafe.Pointer(&d.vb[0])) != 0 {
		return nil, nil
	}

	d.pcmOut = d.pcmOut[:0]
	for {
		var pcmPtr uintptr
		n := int(vorbisSynthesisPCMOut(unsafe.Pointer(&d.vd[0]), &pcmPtr))
		if n <= 0 || pcmPtr == 0 {
			break
		}
		chanPtrs := unsafe.Slice((*uintptr)(unsafe.Pointer(pcmPtr)), d.ch)
		if d.ch == 1 {
			ch0 := unsafe.Slice((*float32)(unsafe.Pointer(chanPtrs[0])), n)
			for i := 0; i < n; i++ {
				s := ch0[i]
				d.pcmOut = append(d.pcmOut, s, s)
			}
		} else {
			ch0 := unsafe.Slice((*float32)(unsafe.Pointer(chanPtrs[0])), n)
			ch1 := unsafe.Slice((*float32)(unsafe.Pointer(chanPtrs[1])), n)
			for i := 0; i < n; i++ {
				d.pcmOut = append(d.pcmOut, ch0[i], ch1[i])
			}
		}
		vorbisSynthesisRead(unsafe.Pointer(&d.vd[0]), int32(n))
	}
	return d.pcmOut, nil
}

func (d *vorbisDecoder) reset() {
	if d.inited {
		vorbisSynthesisRestart(unsafe.Pointer(&d.vd[0]))
	}
}

func (d *vorbisDecoder) close() {
	if d.inited {
		vorbisBlockClear(unsafe.Pointer(&d.vb[0]))
		vorbisDSPClear(unsafe.Pointer(&d.vd[0]))
		vorbisCommentClear(unsafe.Pointer(&d.vc[0]))
		vorbisInfoClear(unsafe.Pointer(&d.vi[0]))
		d.inited = false
	}
}

// fillOggPacket populates ogg_packet respecting Unix LP64 (8-byte long) vs
// Windows LLP64 (4-byte long).
func fillOggPacket(op *[oggPacketBufSize]byte, data []byte, bos bool, pktNo int64) {
	clear(op[:])
	var ptr uintptr
	if len(data) > 0 {
		ptr = uintptr(unsafe.Pointer(&data[0]))
	}
	var bosVal uint64
	if bos {
		bosVal = 1
	}
	binary.LittleEndian.PutUint64(op[0:8], uint64(ptr))
	if runtime.GOOS == "windows" {
		binary.LittleEndian.PutUint32(op[8:12], uint32(len(data)))
		binary.LittleEndian.PutUint32(op[12:16], uint32(bosVal))
		binary.LittleEndian.PutUint32(op[16:20], 0) // e_o_s
		// 4 bytes alignment padding before int64 granulepos
		binary.LittleEndian.PutUint64(op[24:32], 0)
		binary.LittleEndian.PutUint64(op[32:40], uint64(pktNo))
	} else {
		binary.LittleEndian.PutUint64(op[8:16], uint64(len(data)))
		binary.LittleEndian.PutUint64(op[16:24], bosVal)
		binary.LittleEndian.PutUint64(op[24:32], 0) // e_o_s
		binary.LittleEndian.PutUint64(op[32:40], 0) // granulepos
		binary.LittleEndian.PutUint64(op[40:48], uint64(pktNo))
	}
}
