//go:build ios

package media

/*
#include "codec_ios.h"
*/
import "C"

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"runtime"
	"unsafe"
)

func init() {
	registerVideoDecoder(videoCodecAV1, func() bool { return C.shirei_ios_has_dav1d() != 0 }, newIOSDav1dDecoder)
	registerVideoDecoder(videoCodecVP9, func() bool { return C.shirei_ios_has_vpx_vp9() != 0 }, func(p mediaProbe) (videoDecoder, error) {
		return newIOSVPXDecoder(true)
	})
	registerVideoDecoder(videoCodecVP8, func() bool { return C.shirei_ios_has_vpx_vp8() != 0 }, func(p mediaProbe) (videoDecoder, error) {
		return newIOSVPXDecoder(false)
	})
	registerAudioDecoder(audioCodecOpus, func() bool { return C.shirei_ios_has_opus() != 0 }, newIOSOpusDecoder)
	registerAudioDecoder(audioCodecVorbis, func() bool { return C.shirei_ios_has_vorbis() != 0 }, newIOSVorbisDecoder)
}

// -----------------------------------------------------------------------------
// dav1d (AV1)

const (
	iosDav1dSettingsBufSize = 256
	iosDav1dDataBufSize     = 128
	iosDav1dPicBufSize      = 512

	iosDav1dDataOffTimestamp = 24
	iosDav1dPicOffSeqHdr     = 0
	iosDav1dPicOffData0      = 16
	iosDav1dPicOffData1      = 24
	iosDav1dPicOffData2      = 32
	iosDav1dPicOffStride0    = 40
	iosDav1dPicOffStride1    = 48
	iosDav1dPicOffWidth      = 56
	iosDav1dPicOffHeight     = 60
	iosDav1dPicOffLayout     = 64
	iosDav1dPicOffBPC        = 68
	iosDav1dPicOffTimestamp  = 72

	iosDav1dSeqHdrOffMatrix     = 24
	iosDav1dSeqHdrOffColorRange = 33
	iosDav1dEAGAIN              = -35
)

type iosDav1dDecoder struct {
	ctx unsafe.Pointer
}

func newIOSDav1dDecoder(p mediaProbe) (videoDecoder, error) {
	if C.shirei_ios_has_dav1d() == 0 {
		return nil, errors.New("media: libdav1d not linked")
	}
	var settings [iosDav1dSettingsBufSize]byte
	C.shirei_ios_dav1d_default_settings(unsafe.Pointer(&settings[0]))
	nThreads := runtime.NumCPU()
	if nThreads > 6 {
		nThreads = 6
	}
	if nThreads < 1 {
		nThreads = 1
	}
	binary.LittleEndian.PutUint32(settings[0:4], uint32(nThreads))
	binary.LittleEndian.PutUint32(settings[4:8], 2)

	var ctx unsafe.Pointer
	if rc := C.shirei_ios_dav1d_open(&ctx, unsafe.Pointer(&settings[0])); rc < 0 || ctx == nil {
		return nil, fmt.Errorf("media: dav1d_open failed (%d)", int(rc))
	}
	return &iosDav1dDecoder{ctx: ctx}, nil
}

func (d *iosDav1dDecoder) decode(pkt demuxPacket, emit func(decodedVideoFrame)) error {
	if d.ctx == nil || len(pkt.Data) == 0 {
		return nil
	}
	var data [iosDav1dDataBufSize]byte
	bufPtr := C.shirei_ios_dav1d_data_create(unsafe.Pointer(&data[0]), C.size_t(len(pkt.Data)))
	if bufPtr == nil {
		return errors.New("media: dav1d_data_create failed")
	}
	copy(unsafe.Slice((*byte)(unsafe.Pointer(bufPtr)), len(pkt.Data)), pkt.Data)
	tsUs := int64(math.Round(pkt.PTS * 1e6))
	binary.LittleEndian.PutUint64(data[iosDav1dDataOffTimestamp:iosDav1dDataOffTimestamp+8], uint64(tsUs))

	for {
		rc := int(C.shirei_ios_dav1d_send_data(d.ctx, unsafe.Pointer(&data[0])))
		if rc == 0 {
			break
		}
		if rc == iosDav1dEAGAIN {
			if !d.drainPictures(emit) {
				break
			}
			continue
		}
		C.shirei_ios_dav1d_data_unref(unsafe.Pointer(&data[0]))
		return nil
	}
	d.drainPictures(emit)
	return nil
}

func (d *iosDav1dDecoder) flush(emit func(decodedVideoFrame)) {
	if d.ctx != nil {
		d.drainPictures(emit)
	}
}

func (d *iosDav1dDecoder) drainPictures(emit func(decodedVideoFrame)) bool {
	got := false
	var pic [iosDav1dPicBufSize]byte
	for {
		clear(pic[:])
		rc := int(C.shirei_ios_dav1d_get_picture(d.ctx, unsafe.Pointer(&pic[0])))
		if rc == iosDav1dEAGAIN || rc < 0 {
			break
		}
		got = true
		d.emitPicture(&pic, emit)
		C.shirei_ios_dav1d_picture_unref(unsafe.Pointer(&pic[0]))
	}
	return got
}

func (d *iosDav1dDecoder) emitPicture(pic *[iosDav1dPicBufSize]byte, emit func(decodedVideoFrame)) {
	w := int(int32(binary.LittleEndian.Uint32(pic[iosDav1dPicOffWidth : iosDav1dPicOffWidth+4])))
	h := int(int32(binary.LittleEndian.Uint32(pic[iosDav1dPicOffHeight : iosDav1dPicOffHeight+4])))
	layout := int(int32(binary.LittleEndian.Uint32(pic[iosDav1dPicOffLayout : iosDav1dPicOffLayout+4])))
	bpc := int(int32(binary.LittleEndian.Uint32(pic[iosDav1dPicOffBPC : iosDav1dPicOffBPC+4])))
	tsUs := int64(binary.LittleEndian.Uint64(pic[iosDav1dPicOffTimestamp : iosDav1dPicOffTimestamp+8]))

	p0 := uintptr(binary.LittleEndian.Uint64(pic[iosDav1dPicOffData0 : iosDav1dPicOffData0+8]))
	p1 := uintptr(binary.LittleEndian.Uint64(pic[iosDav1dPicOffData1 : iosDav1dPicOffData1+8]))
	p2 := uintptr(binary.LittleEndian.Uint64(pic[iosDav1dPicOffData2 : iosDav1dPicOffData2+8]))
	s0 := int(int64(binary.LittleEndian.Uint64(pic[iosDav1dPicOffStride0 : iosDav1dPicOffStride0+8])))
	s1 := int(int64(binary.LittleEndian.Uint64(pic[iosDav1dPicOffStride1 : iosDav1dPicOffStride1+8])))
	if w <= 0 || h <= 0 || p0 == 0 || s0 <= 0 {
		return
	}

	var xShift, yShift int
	switch layout {
	case 1:
		xShift, yShift = 1, 1
	case 2:
		xShift, yShift = 1, 0
	default:
		xShift, yShift = 0, 0
	}

	bt709 := h >= 720
	fullRange := false
	seqPtr := uintptr(binary.LittleEndian.Uint64(pic[iosDav1dPicOffSeqHdr : iosDav1dPicOffSeqHdr+8]))
	if seqPtr != 0 {
		seq := unsafe.Slice((*byte)(unsafe.Pointer(seqPtr)), 40)
		mtrx := int32(binary.LittleEndian.Uint32(seq[iosDav1dSeqHdrOffMatrix : iosDav1dSeqHdrOffMatrix+4]))
		if mtrx == 1 {
			bt709 = true
		} else if mtrx == 6 || mtrx == 5 {
			bt709 = false
		}
		fullRange = seq[iosDav1dSeqHdrOffColorRange] != 0
	}

	ySlice := unsafe.Slice((*byte)(unsafe.Pointer(p0)), h*s0)
	var uSlice, vSlice []byte
	if layout != 0 && p1 != 0 && p2 != 0 && s1 > 0 {
		uvH := (h + (1 << yShift) - 1) >> yShift
		uSlice = unsafe.Slice((*byte)(unsafe.Pointer(p1)), uvH*s1)
		vSlice = unsafe.Slice((*byte)(unsafe.Pointer(p2)), uvH*s1)
	}

	emit(decodedVideoFrame{
		y: ySlice, u: uSlice, v: vSlice,
		yStride: s0, uvStride: s1,
		w: w, h: h, xShift: xShift, yShift: yShift,
		bitDepth: bpc, bt709: bt709, fullRange: fullRange,
		pts: float64(tsUs) / 1e6,
	})
}

func (d *iosDav1dDecoder) reset() {
	if d.ctx != nil {
		C.shirei_ios_dav1d_flush(d.ctx)
	}
}

func (d *iosDav1dDecoder) close() {
	if d.ctx != nil {
		C.shirei_ios_dav1d_close(&d.ctx)
		d.ctx = nil
	}
}

// -----------------------------------------------------------------------------
// libvpx (VP8 / VP9)

const (
	iosVPXCtxBufSize  = 128
	iosVPXOffFmt      = 0
	iosVPXOffCS       = 4
	iosVPXOffRange    = 8
	iosVPXOffBitDepth = 20
	iosVPXOffDW       = 24
	iosVPXOffDH       = 28
	iosVPXOffXShift   = 40
	iosVPXOffYShift   = 44
	iosVPXOffPlanes   = 48
	iosVPXOffStride   = 80
	iosVPXFmtUVFlip   = 0x200
	iosVPXFmtHBD      = 0x800
	iosVPXFmtNV12     = 0x100 | 9
	iosVPXABIMismatch = 4
)

type iosVPXDecoder struct {
	isVP9 bool
	ctx   [iosVPXCtxBufSize]byte
	ready bool
}

func newIOSVPXDecoder(isVP9 bool) (videoDecoder, error) {
	if (isVP9 && C.shirei_ios_has_vpx_vp9() == 0) || (!isVP9 && C.shirei_ios_has_vpx_vp8() == 0) {
		return nil, errors.New("media: libvpx not linked")
	}
	d := &iosVPXDecoder{isVP9: isVP9}
	if err := d.initCtx(); err != nil {
		return nil, err
	}
	return d, nil
}

func (d *iosVPXDecoder) initCtx() error {
	nThreads := runtime.NumCPU()
	if nThreads > 6 {
		nThreads = 6
	}
	if nThreads < 1 {
		nThreads = 1
	}
	var cfg [12]byte
	binary.LittleEndian.PutUint32(cfg[0:4], uint32(nThreads))
	vp9Flag := C.int(0)
	if d.isVP9 {
		vp9Flag = 1
	}
	clear(d.ctx[:])
	for _, ver := range []C.int{12, 11, 13, 14, 10} {
		rc := int(C.shirei_ios_vpx_dec_init(unsafe.Pointer(&d.ctx[0]), vp9Flag, unsafe.Pointer(&cfg[0]), ver))
		if rc == 0 {
			d.ready = true
			return nil
		}
		if rc != iosVPXABIMismatch {
			return fmt.Errorf("media: vpx_codec_dec_init_ver failed (%d)", rc)
		}
	}
	return errors.New("media: libvpx ABI mismatch")
}

func (d *iosVPXDecoder) decode(pkt demuxPacket, emit func(decodedVideoFrame)) error {
	if !d.ready || len(pkt.Data) == 0 {
		return nil
	}
	if C.shirei_ios_vpx_decode(unsafe.Pointer(&d.ctx[0]), (*C.uint8_t)(unsafe.Pointer(&pkt.Data[0])), C.uint(len(pkt.Data))) != 0 {
		return nil
	}
	d.drainFrames(pkt.PTS, emit)
	return nil
}

func (d *iosVPXDecoder) flush(emit func(decodedVideoFrame)) {
	if !d.ready {
		return
	}
	C.shirei_ios_vpx_decode(unsafe.Pointer(&d.ctx[0]), nil, 0)
	d.drainFrames(0, emit)
}

func (d *iosVPXDecoder) drainFrames(pts float64, emit func(decodedVideoFrame)) {
	var iter unsafe.Pointer
	for {
		imgPtr := C.shirei_ios_vpx_get_frame(unsafe.Pointer(&d.ctx[0]), &iter)
		if imgPtr == nil {
			break
		}
		img := unsafe.Slice((*byte)(imgPtr), 128)
		fmtVal := binary.LittleEndian.Uint32(img[iosVPXOffFmt : iosVPXOffFmt+4])
		cs := binary.LittleEndian.Uint32(img[iosVPXOffCS : iosVPXOffCS+4])
		rng := binary.LittleEndian.Uint32(img[iosVPXOffRange : iosVPXOffRange+4])
		bitDepth := int(binary.LittleEndian.Uint32(img[iosVPXOffBitDepth : iosVPXOffBitDepth+4]))
		w := int(binary.LittleEndian.Uint32(img[iosVPXOffDW : iosVPXOffDW+4]))
		h := int(binary.LittleEndian.Uint32(img[iosVPXOffDH : iosVPXOffDH+4]))
		xShift := int(binary.LittleEndian.Uint32(img[iosVPXOffXShift : iosVPXOffXShift+4]))
		yShift := int(binary.LittleEndian.Uint32(img[iosVPXOffYShift : iosVPXOffYShift+4]))

		p0 := uintptr(binary.LittleEndian.Uint64(img[iosVPXOffPlanes : iosVPXOffPlanes+8]))
		p1 := uintptr(binary.LittleEndian.Uint64(img[iosVPXOffPlanes+8 : iosVPXOffPlanes+16]))
		p2 := uintptr(binary.LittleEndian.Uint64(img[iosVPXOffPlanes+16 : iosVPXOffPlanes+24]))
		s0 := int(int32(binary.LittleEndian.Uint32(img[iosVPXOffStride : iosVPXOffStride+4])))
		s1 := int(int32(binary.LittleEndian.Uint32(img[iosVPXOffStride+4 : iosVPXOffStride+8])))

		if w <= 0 || h <= 0 || p0 == 0 || s0 <= 0 {
			continue
		}
		if bitDepth == 0 {
			if (fmtVal & iosVPXFmtHBD) != 0 {
				bitDepth = 10
			} else {
				bitDepth = 8
			}
		}
		if (fmtVal & iosVPXFmtUVFlip) != 0 {
			p1, p2 = p2, p1
		}

		ySlice := unsafe.Slice((*byte)(unsafe.Pointer(p0)), h*s0)
		var uSlice, vSlice []byte
		if p1 != 0 && p2 != 0 && s1 > 0 && fmtVal != iosVPXFmtNV12 {
			uvH := (h + (1 << yShift) - 1) >> yShift
			uSlice = unsafe.Slice((*byte)(unsafe.Pointer(p1)), uvH*s1)
			vSlice = unsafe.Slice((*byte)(unsafe.Pointer(p2)), uvH*s1)
		}

		emit(decodedVideoFrame{
			y: ySlice, u: uSlice, v: vSlice,
			yStride: s0, uvStride: s1,
			w: w, h: h, xShift: xShift, yShift: yShift,
			bitDepth:  bitDepth,
			bt709:     cs == 2 || (cs == 0 && h >= 720),
			fullRange: rng == 1,
			pts:       pts,
		})
	}
}

func (d *iosVPXDecoder) reset() {
	if d.ready {
		C.shirei_ios_vpx_destroy(unsafe.Pointer(&d.ctx[0]))
		d.ready = false
		_ = d.initCtx()
	}
}

func (d *iosVPXDecoder) close() {
	if d.ready {
		C.shirei_ios_vpx_destroy(unsafe.Pointer(&d.ctx[0]))
		d.ready = false
	}
}

// -----------------------------------------------------------------------------
// libopus

const (
	iosOpusRate     = 48000
	iosOpusMaxFrame = 5760
	iosOpusChannels = 2
)

type iosOpusDecoder struct {
	st  unsafe.Pointer
	pcm []float32
}

func newIOSOpusDecoder(p mediaProbe) (audioDecoder, error) {
	if C.shirei_ios_has_opus() == 0 {
		return nil, errors.New("media: libopus not linked")
	}
	var errCode C.int
	st := C.shirei_ios_opus_create(iosOpusRate, iosOpusChannels, &errCode)
	if st == nil || errCode != 0 {
		return nil, fmt.Errorf("media: opus_decoder_create failed (%d)", int(errCode))
	}
	return &iosOpusDecoder{
		st:  st,
		pcm: make([]float32, iosOpusMaxFrame*iosOpusChannels),
	}, nil
}

func (d *iosOpusDecoder) sampleRate() int { return iosOpusRate }

func (d *iosOpusDecoder) decode(pkt demuxPacket) ([]float32, error) {
	if d.st == nil || len(pkt.Data) == 0 {
		return nil, nil
	}
	n := int(C.shirei_ios_opus_decode_float(
		d.st,
		(*C.uchar)(unsafe.Pointer(&pkt.Data[0])),
		C.int(len(pkt.Data)),
		(*C.float)(unsafe.Pointer(&d.pcm[0])),
		iosOpusMaxFrame,
	))
	if n <= 0 {
		return nil, nil
	}
	return d.pcm[:n*iosOpusChannels], nil
}

func (d *iosOpusDecoder) reset() {
	if d.st != nil {
		C.shirei_ios_opus_destroy(d.st)
		var errCode C.int
		d.st = C.shirei_ios_opus_create(iosOpusRate, iosOpusChannels, &errCode)
		if errCode != 0 {
			d.st = nil
		}
	}
}

func (d *iosOpusDecoder) close() {
	if d.st != nil {
		C.shirei_ios_opus_destroy(d.st)
		d.st = nil
	}
}

// -----------------------------------------------------------------------------
// libvorbis

type iosVorbisDecoder struct {
	vi     [128]byte
	vc     [64]byte
	vd     [256]byte
	vb     [320]byte
	rate   int
	ch     int
	pktNo  int64
	inited bool
	pcmOut []float32
}

func newIOSVorbisDecoder(p mediaProbe) (audioDecoder, error) {
	if C.shirei_ios_has_vorbis() == 0 {
		return nil, errors.New("media: libvorbis not linked")
	}
	headers, err := splitVorbisCodecPrivate(p.AudioPrivate)
	if err != nil {
		return nil, err
	}
	d := &iosVorbisDecoder{}
	C.shirei_ios_vorbis_info_init(unsafe.Pointer(&d.vi[0]))
	C.shirei_ios_vorbis_comment_init(unsafe.Pointer(&d.vc[0]))

	var op [64]byte
	for i, hdr := range headers {
		fillIOSOggPacket(&op, hdr, i == 0, int64(i))
		rc := int(C.shirei_ios_vorbis_headerin(unsafe.Pointer(&d.vi[0]), unsafe.Pointer(&d.vc[0]), unsafe.Pointer(&op[0])))
		runtime.KeepAlive(hdr)
		if rc != 0 {
			C.shirei_ios_vorbis_comment_clear(unsafe.Pointer(&d.vc[0]))
			C.shirei_ios_vorbis_info_clear(unsafe.Pointer(&d.vi[0]))
			return nil, fmt.Errorf("media: vorbis_synthesis_headerin[%d] failed (%d)", i, rc)
		}
	}

	d.ch = int(int32(binary.LittleEndian.Uint32(d.vi[4:8])))
	d.rate = int(binary.LittleEndian.Uint64(d.vi[8:16]))
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

	if rc := int(C.shirei_ios_vorbis_synthesis_init(unsafe.Pointer(&d.vd[0]), unsafe.Pointer(&d.vi[0]))); rc != 0 {
		C.shirei_ios_vorbis_comment_clear(unsafe.Pointer(&d.vc[0]))
		C.shirei_ios_vorbis_info_clear(unsafe.Pointer(&d.vi[0]))
		return nil, fmt.Errorf("media: vorbis_synthesis_init failed (%d)", rc)
	}
	if rc := int(C.shirei_ios_vorbis_block_init(unsafe.Pointer(&d.vd[0]), unsafe.Pointer(&d.vb[0]))); rc != 0 {
		C.shirei_ios_vorbis_dsp_clear(unsafe.Pointer(&d.vd[0]))
		C.shirei_ios_vorbis_comment_clear(unsafe.Pointer(&d.vc[0]))
		C.shirei_ios_vorbis_info_clear(unsafe.Pointer(&d.vi[0]))
		return nil, fmt.Errorf("media: vorbis_block_init failed (%d)", rc)
	}
	d.pktNo = 3
	d.inited = true
	return d, nil
}

func (d *iosVorbisDecoder) sampleRate() int { return d.rate }

func (d *iosVorbisDecoder) decode(pkt demuxPacket) ([]float32, error) {
	if !d.inited || len(pkt.Data) == 0 {
		return nil, nil
	}
	var op [64]byte
	fillIOSOggPacket(&op, pkt.Data, false, d.pktNo)
	d.pktNo++

	rc := int(C.shirei_ios_vorbis_synthesis(unsafe.Pointer(&d.vb[0]), unsafe.Pointer(&op[0])))
	runtime.KeepAlive(pkt.Data)
	if rc != 0 {
		return nil, nil
	}
	if C.shirei_ios_vorbis_blockin(unsafe.Pointer(&d.vd[0]), unsafe.Pointer(&d.vb[0])) != 0 {
		return nil, nil
	}

	d.pcmOut = d.pcmOut[:0]
	for {
		var pcmPtr **C.float
		n := int(C.shirei_ios_vorbis_pcmout(unsafe.Pointer(&d.vd[0]), &pcmPtr))
		if n <= 0 || pcmPtr == nil {
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
		C.shirei_ios_vorbis_read(unsafe.Pointer(&d.vd[0]), C.int(n))
	}
	return d.pcmOut, nil
}

func (d *iosVorbisDecoder) reset() {
	if d.inited {
		C.shirei_ios_vorbis_restart(unsafe.Pointer(&d.vd[0]))
	}
}

func (d *iosVorbisDecoder) close() {
	if d.inited {
		C.shirei_ios_vorbis_block_clear(unsafe.Pointer(&d.vb[0]))
		C.shirei_ios_vorbis_dsp_clear(unsafe.Pointer(&d.vd[0]))
		C.shirei_ios_vorbis_comment_clear(unsafe.Pointer(&d.vc[0]))
		C.shirei_ios_vorbis_info_clear(unsafe.Pointer(&d.vi[0]))
		d.inited = false
	}
}

func fillIOSOggPacket(op *[64]byte, data []byte, bos bool, pktNo int64) {
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
	binary.LittleEndian.PutUint64(op[8:16], uint64(len(data)))
	binary.LittleEndian.PutUint64(op[16:24], bosVal)
	binary.LittleEndian.PutUint64(op[40:48], uint64(pktNo))
}
