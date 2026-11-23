//go:build ios

package media

/*
#include "codec_ios.h"
#include <stdlib.h>
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
	var settingsBuf [iosDav1dSettingsBufSize / 8]uint64
	settings := unsafe.Slice((*byte)(unsafe.Pointer(&settingsBuf[0])), iosDav1dSettingsBufSize)
	C.shirei_ios_dav1d_default_settings(unsafe.Pointer(&settingsBuf[0]))
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
	if rc := C.shirei_ios_dav1d_open(&ctx, unsafe.Pointer(&settingsBuf[0])); rc < 0 || ctx == nil {
		return nil, fmt.Errorf("media: dav1d_open failed (%d)", int(rc))
	}
	return &iosDav1dDecoder{ctx: ctx}, nil
}

func (d *iosDav1dDecoder) decode(pkt demuxPacket, emit func(decodedVideoFrame)) error {
	if d.ctx == nil || len(pkt.Data) == 0 {
		return nil
	}
	var dataBuf [iosDav1dDataBufSize / 8]uint64
	data := unsafe.Slice((*byte)(unsafe.Pointer(&dataBuf[0])), iosDav1dDataBufSize)
	bufPtr := C.shirei_ios_dav1d_data_create(unsafe.Pointer(&dataBuf[0]), C.size_t(len(pkt.Data)))
	if bufPtr == nil {
		return errors.New("media: dav1d_data_create failed")
	}
	copy(unsafe.Slice((*byte)(unsafe.Pointer(bufPtr)), len(pkt.Data)), pkt.Data)
	tsUs := int64(math.Round(pkt.PTS * 1e6))
	binary.LittleEndian.PutUint64(data[iosDav1dDataOffTimestamp:iosDav1dDataOffTimestamp+8], uint64(tsUs))

	for {
		rc := int(C.shirei_ios_dav1d_send_data(d.ctx, unsafe.Pointer(&dataBuf[0])))
		if rc == 0 {
			break
		}
		if rc == iosDav1dEAGAIN {
			if !d.drainPictures(emit) {
				C.shirei_ios_dav1d_data_unref(unsafe.Pointer(&dataBuf[0]))
				break
			}
			continue
		}
		C.shirei_ios_dav1d_data_unref(unsafe.Pointer(&dataBuf[0]))
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
	var picBuf [iosDav1dPicBufSize / 8]uint64
	pic := unsafe.Slice((*byte)(unsafe.Pointer(&picBuf[0])), iosDav1dPicBufSize)
	for {
		clear(picBuf[:])
		rc := int(C.shirei_ios_dav1d_get_picture(d.ctx, unsafe.Pointer(&picBuf[0])))
		if rc == iosDav1dEAGAIN || rc < 0 {
			break
		}
		got = true
		d.emitPicture(pic, emit)
		C.shirei_ios_dav1d_picture_unref(unsafe.Pointer(&picBuf[0]))
	}
	return got
}

func (d *iosDav1dDecoder) emitPicture(pic []byte, emit func(decodedVideoFrame)) {
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
	iosVPXABIMismatch = 3
)

type iosVPXDecoder struct {
	isVP9 bool
	ctx   unsafe.Pointer // C-allocated vpx_codec_ctx_t
	cfg   unsafe.Pointer // C-allocated vpx_codec_dec_cfg_t
	ready bool
}

func newIOSVPXDecoder(isVP9 bool) (videoDecoder, error) {
	if (isVP9 && C.shirei_ios_has_vpx_vp9() == 0) || (!isVP9 && C.shirei_ios_has_vpx_vp8() == 0) {
		return nil, errors.New("media: libvpx not linked")
	}
	d := &iosVPXDecoder{
		isVP9: isVP9,
		ctx:   C.calloc(1, iosVPXCtxBufSize),
		cfg:   C.calloc(1, 16),
	}
	if d.ctx == nil || d.cfg == nil {
		d.close()
		return nil, errors.New("media: libvpx alloc failed")
	}
	if err := d.initCtx(); err != nil {
		d.close()
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
	cfgBytes := unsafe.Slice((*byte)(d.cfg), 16)
	clear(cfgBytes)
	binary.LittleEndian.PutUint32(cfgBytes[0:4], uint32(nThreads))
	vp9Flag := C.int(0)
	if d.isVP9 {
		vp9Flag = 1
	}
	clear(unsafe.Slice((*byte)(d.ctx), iosVPXCtxBufSize))
	for _, ver := range []C.int{12, 11, 13, 14, 10} {
		rc := int(C.shirei_ios_vpx_dec_init(d.ctx, vp9Flag, d.cfg, ver))
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
	if C.shirei_ios_vpx_decode(d.ctx, (*C.uint8_t)(unsafe.Pointer(&pkt.Data[0])), C.uint(len(pkt.Data))) != 0 {
		return nil
	}
	d.drainFrames(pkt.PTS, emit)
	return nil
}

func (d *iosVPXDecoder) flush(emit func(decodedVideoFrame)) {
	if !d.ready {
		return
	}
	C.shirei_ios_vpx_decode(d.ctx, nil, 0)
	d.drainFrames(0, emit)
}

func (d *iosVPXDecoder) drainFrames(pts float64, emit func(decodedVideoFrame)) {
	var iter unsafe.Pointer
	for {
		imgPtr := C.shirei_ios_vpx_get_frame(d.ctx, &iter)
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
		C.shirei_ios_vpx_destroy(d.ctx)
		d.ready = false
		_ = d.initCtx()
	}
}

func (d *iosVPXDecoder) close() {
	if d.ready {
		C.shirei_ios_vpx_destroy(d.ctx)
		d.ready = false
	}
	if d.ctx != nil {
		C.free(d.ctx)
		d.ctx = nil
	}
	if d.cfg != nil {
		C.free(d.cfg)
		d.cfg = nil
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
	vi     unsafe.Pointer // C-allocated vorbis_info
	vc     unsafe.Pointer // C-allocated vorbis_comment
	vd     unsafe.Pointer // C-allocated vorbis_dsp_state
	vb     unsafe.Pointer // C-allocated vorbis_block
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
	d := &iosVorbisDecoder{
		vi: C.calloc(1, 128),
		vc: C.calloc(1, 64),
		vd: C.calloc(1, 256),
		vb: C.calloc(1, 320),
	}
	if d.vi == nil || d.vc == nil || d.vd == nil || d.vb == nil {
		d.freeStructs()
		return nil, errors.New("media: libvorbis alloc failed")
	}
	C.shirei_ios_vorbis_info_init(d.vi)
	C.shirei_ios_vorbis_comment_init(d.vc)

	for i, hdr := range headers {
		var dataPtr *C.uint8_t
		if len(hdr) > 0 {
			dataPtr = (*C.uint8_t)(unsafe.Pointer(&hdr[0]))
		}
		bos := C.int(0)
		if i == 0 {
			bos = 1
		}
		rc := int(C.shirei_ios_vorbis_headerin_pkt(d.vi, d.vc, dataPtr, C.size_t(len(hdr)), bos, C.int64_t(i)))
		if rc != 0 {
			C.shirei_ios_vorbis_comment_clear(d.vc)
			C.shirei_ios_vorbis_info_clear(d.vi)
			d.freeStructs()
			return nil, fmt.Errorf("media: vorbis_synthesis_headerin[%d] failed (%d)", i, rc)
		}
	}

	viBytes := unsafe.Slice((*byte)(d.vi), 128)
	d.ch = int(int32(binary.LittleEndian.Uint32(viBytes[4:8])))
	d.rate = int(binary.LittleEndian.Uint64(viBytes[8:16]))
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

	if rc := int(C.shirei_ios_vorbis_synthesis_init(d.vd, d.vi)); rc != 0 {
		C.shirei_ios_vorbis_comment_clear(d.vc)
		C.shirei_ios_vorbis_info_clear(d.vi)
		d.freeStructs()
		return nil, fmt.Errorf("media: vorbis_synthesis_init failed (%d)", rc)
	}
	if rc := int(C.shirei_ios_vorbis_block_init(d.vd, d.vb)); rc != 0 {
		C.shirei_ios_vorbis_dsp_clear(d.vd)
		C.shirei_ios_vorbis_comment_clear(d.vc)
		C.shirei_ios_vorbis_info_clear(d.vi)
		d.freeStructs()
		return nil, fmt.Errorf("media: vorbis_block_init failed (%d)", rc)
	}
	d.pktNo = 3
	d.inited = true
	return d, nil
}

func (d *iosVorbisDecoder) freeStructs() {
	if d.vb != nil {
		C.free(d.vb)
		d.vb = nil
	}
	if d.vd != nil {
		C.free(d.vd)
		d.vd = nil
	}
	if d.vc != nil {
		C.free(d.vc)
		d.vc = nil
	}
	if d.vi != nil {
		C.free(d.vi)
		d.vi = nil
	}
}

func (d *iosVorbisDecoder) sampleRate() int { return d.rate }

func (d *iosVorbisDecoder) decode(pkt demuxPacket) ([]float32, error) {
	if !d.inited || len(pkt.Data) == 0 {
		return nil, nil
	}
	pktNo := d.pktNo
	d.pktNo++

	rc := int(C.shirei_ios_vorbis_synthesis_pkt(
		d.vb,
		(*C.uint8_t)(unsafe.Pointer(&pkt.Data[0])),
		C.size_t(len(pkt.Data)),
		C.int64_t(pktNo),
	))
	if rc != 0 {
		return nil, nil
	}
	if C.shirei_ios_vorbis_blockin(d.vd, d.vb) != 0 {
		return nil, nil
	}

	d.pcmOut = d.pcmOut[:0]
	for {
		var pcmPtr **C.float
		n := int(C.shirei_ios_vorbis_pcmout(d.vd, &pcmPtr))
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
		C.shirei_ios_vorbis_read(d.vd, C.int(n))
	}
	return d.pcmOut, nil
}

func (d *iosVorbisDecoder) reset() {
	if d.inited {
		C.shirei_ios_vorbis_restart(d.vd)
	}
}

func (d *iosVorbisDecoder) close() {
	if d.inited {
		C.shirei_ios_vorbis_block_clear(d.vb)
		C.shirei_ios_vorbis_dsp_clear(d.vd)
		C.shirei_ios_vorbis_comment_clear(d.vc)
		C.shirei_ios_vorbis_info_clear(d.vi)
		d.inited = false
	}
	d.freeStructs()
}
