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

// libvpx ABI constants (vpx_image_t has been frozen at VPX_IMAGE_ABI_VERSION=5).
const (
	vpxCodecCtxBufSize = 128

	vpxImgOffFmt      = 0
	vpxImgOffCS       = 4
	vpxImgOffRange    = 8
	vpxImgOffBitDepth = 20
	vpxImgOffDW       = 24
	vpxImgOffDH       = 28
	vpxImgOffXShift   = 40
	vpxImgOffYShift   = 44
	vpxImgOffPlanes   = 48 // 4 * uintptr (32 bytes on 64-bit)
	vpxImgOffStride   = 80 // 4 * int32 (16 bytes)

	vpxImgFmtPlanar       = 0x100
	vpxImgFmtUVFlip       = 0x200
	vpxImgFmtHighBitDepth = 0x800
	vpxImgFmtNV12         = vpxImgFmtPlanar | 9
	vpxCSBT709            = 2
	vpxCRFullRange        = 1
	vpxCodecOK            = 0
	vpxCodecABIMismatch   = 3
)

var (
	vpxOnce sync.Once
	vpxErr  error

	vpxCodecVP8Dx      func() uintptr
	vpxCodecVP9Dx      func() uintptr
	vpxCodecDecInitVer func(ctx unsafe.Pointer, iface uintptr, cfg unsafe.Pointer, flags int64, ver int32) int32
	vpxCodecDecode     func(ctx unsafe.Pointer, data *byte, dataSz uint32, userPriv uintptr, deadline int64) int32
	vpxCodecGetFrame   func(ctx unsafe.Pointer, iter *uintptr) uintptr
	vpxCodecDestroy    func(ctx unsafe.Pointer) int32
)

func init() {
	registerVideoDecoder(videoCodecVP9, vpxVP9Available, func(p mediaProbe) (videoDecoder, error) {
		return newVPXDecoder(true)
	})
	registerVideoDecoder(videoCodecVP8, vpxVP8Available, func(p mediaProbe) (videoDecoder, error) {
		return newVPXDecoder(false)
	})
}

func loadVPX() error {
	vpxOnce.Do(func() {
		if unsafe.Sizeof(uintptr(0)) != 8 {
			vpxErr = errors.New("media: libvpx purego bindings require 64-bit platform")
			return
		}
		var names []string
		switch runtime.GOOS {
		case "darwin":
			names = []string{"libvpx.9.dylib", "libvpx.8.dylib", "libvpx.7.dylib", "libvpx.dylib"}
		case "windows":
			names = []string{"vpx.dll", "libvpx.dll", "libvpx-9.dll", "libvpx-8.dll", "libvpx-7.dll"}
		default:
			names = []string{"libvpx.so.9", "libvpx.so.8", "libvpx.so.7", "libvpx.so.6", "libvpx.so"}
		}
		h, err := openCodecLib(names)
		if err != nil {
			vpxErr = err
			return
		}
		syms := []struct {
			fn   any
			name string
		}{
			{&vpxCodecDecInitVer, "vpx_codec_dec_init_ver"},
			{&vpxCodecDecode, "vpx_codec_decode"},
			{&vpxCodecGetFrame, "vpx_codec_get_frame"},
			{&vpxCodecDestroy, "vpx_codec_destroy"},
		}
		for _, s := range syms {
			if err := registerCodecFunc(s.fn, h, s.name); err != nil {
				vpxErr = fmt.Errorf("media: libvpx symbol %s: %w", s.name, err)
				return
			}
		}
		_ = registerCodecFunc(&vpxCodecVP9Dx, h, "vpx_codec_vp9_dx")
		_ = registerCodecFunc(&vpxCodecVP8Dx, h, "vpx_codec_vp8_dx")
		if vpxCodecVP9Dx == nil && vpxCodecVP8Dx == nil {
			vpxErr = errors.New("media: libvpx exposes neither vp8 nor vp9 decoder")
		}
	})
	return vpxErr
}

func vpxVP9Available() bool {
	return loadVPX() == nil && vpxCodecVP9Dx != nil
}

func vpxVP8Available() bool {
	return loadVPX() == nil && vpxCodecVP8Dx != nil
}

type vpxDecoder struct {
	isVP9 bool
	ctx   [vpxCodecCtxBufSize / 8]uint64
	cfg   [3]uint32
	ready bool
}

func newVPXDecoder(isVP9 bool) (videoDecoder, error) {
	if err := loadVPX(); err != nil {
		return nil, err
	}
	d := &vpxDecoder{isVP9: isVP9}
	if err := d.initCtx(); err != nil {
		return nil, err
	}
	return d, nil
}

func (d *vpxDecoder) initCtx() error {
	var iface uintptr
	if d.isVP9 {
		if vpxCodecVP9Dx == nil {
			return errors.New("media: libvpx built without VP9 decoder")
		}
		iface = vpxCodecVP9Dx()
	} else {
		if vpxCodecVP8Dx == nil {
			return errors.New("media: libvpx built without VP8 decoder")
		}
		iface = vpxCodecVP8Dx()
	}
	if iface == 0 {
		return errors.New("media: libvpx null codec interface")
	}

	nThreads := runtime.NumCPU()
	if nThreads > 8 {
		nThreads = 8
	}
	if nThreads < 1 {
		nThreads = 1
	}
	d.cfg = [3]uint32{uint32(nThreads), 0, 0}

	clear(d.ctx[:])
	// Probe ABI version across libvpx 1.8–1.15 releases (VPX_DECODER_ABI_VERSION 11..14).
	for _, ver := range []int32{12, 11, 13, 14, 10} {
		rc := vpxCodecDecInitVer(unsafe.Pointer(&d.ctx[0]), iface, unsafe.Pointer(&d.cfg[0]), 0, ver)
		if rc == vpxCodecOK {
			d.ready = true
			return nil
		}
		if rc != vpxCodecABIMismatch {
			return fmt.Errorf("media: vpx_codec_dec_init_ver failed (%d)", rc)
		}
	}
	return errors.New("media: libvpx ABI version mismatch")
}

func (d *vpxDecoder) decode(pkt demuxPacket, emit func(decodedVideoFrame)) error {
	if !d.ready || len(pkt.Data) == 0 {
		return nil
	}
	rc := vpxCodecDecode(unsafe.Pointer(&d.ctx[0]), &pkt.Data[0], uint32(len(pkt.Data)), 0, 0)
	if rc != vpxCodecOK {
		return nil
	}
	d.drainFrames(pkt.PTS, emit)
	return nil
}

func (d *vpxDecoder) flush(emit func(decodedVideoFrame)) {
	if !d.ready {
		return
	}
	_ = vpxCodecDecode(unsafe.Pointer(&d.ctx[0]), nil, 0, 0, 0)
	d.drainFrames(0, emit)
}

func (d *vpxDecoder) drainFrames(pts float64, emit func(decodedVideoFrame)) {
	var iter uintptr
	for {
		imgPtr := vpxCodecGetFrame(unsafe.Pointer(&d.ctx[0]), &iter)
		if imgPtr == 0 {
			break
		}
		img := unsafe.Slice((*byte)(unsafe.Pointer(imgPtr)), 128)
		fmtVal := binary.LittleEndian.Uint32(img[vpxImgOffFmt : vpxImgOffFmt+4])
		cs := binary.LittleEndian.Uint32(img[vpxImgOffCS : vpxImgOffCS+4])
		rng := binary.LittleEndian.Uint32(img[vpxImgOffRange : vpxImgOffRange+4])
		bitDepth := int(binary.LittleEndian.Uint32(img[vpxImgOffBitDepth : vpxImgOffBitDepth+4]))
		w := int(binary.LittleEndian.Uint32(img[vpxImgOffDW : vpxImgOffDW+4]))
		h := int(binary.LittleEndian.Uint32(img[vpxImgOffDH : vpxImgOffDH+4]))
		xShift := int(binary.LittleEndian.Uint32(img[vpxImgOffXShift : vpxImgOffXShift+4]))
		yShift := int(binary.LittleEndian.Uint32(img[vpxImgOffYShift : vpxImgOffYShift+4]))

		p0 := uintptr(binary.LittleEndian.Uint64(img[vpxImgOffPlanes : vpxImgOffPlanes+8]))
		p1 := uintptr(binary.LittleEndian.Uint64(img[vpxImgOffPlanes+8 : vpxImgOffPlanes+16]))
		p2 := uintptr(binary.LittleEndian.Uint64(img[vpxImgOffPlanes+16 : vpxImgOffPlanes+24]))
		s0 := int(int32(binary.LittleEndian.Uint32(img[vpxImgOffStride : vpxImgOffStride+4])))
		s1 := int(int32(binary.LittleEndian.Uint32(img[vpxImgOffStride+4 : vpxImgOffStride+8])))

		if w <= 0 || h <= 0 || p0 == 0 || s0 <= 0 {
			continue
		}
		if bitDepth == 0 {
			if (fmtVal & vpxImgFmtHighBitDepth) != 0 {
				bitDepth = 10
			} else {
				bitDepth = 8
			}
		}
		if (fmtVal & vpxImgFmtUVFlip) != 0 {
			p1, p2 = p2, p1
		}

		ySlice := unsafe.Slice((*byte)(unsafe.Pointer(p0)), h*s0)
		var uSlice, vSlice []byte
		if p1 != 0 && p2 != 0 && s1 > 0 && fmtVal != vpxImgFmtNV12 {
			uvH := (h + (1 << yShift) - 1) >> yShift
			uSlice = unsafe.Slice((*byte)(unsafe.Pointer(p1)), uvH*s1)
			vSlice = unsafe.Slice((*byte)(unsafe.Pointer(p2)), uvH*s1)
		}

		bt709 := cs == vpxCSBT709 || (cs == 0 && h >= 720)
		emit(decodedVideoFrame{
			y:         ySlice,
			u:         uSlice,
			v:         vSlice,
			yStride:   s0,
			uvStride:  s1,
			w:         w,
			h:         h,
			xShift:    xShift,
			yShift:    yShift,
			bitDepth:  bitDepth,
			bt709:     bt709,
			fullRange: rng == vpxCRFullRange,
			pts:       pts,
		})
	}
}

func (d *vpxDecoder) reset() {
	if d.ready {
		vpxCodecDestroy(unsafe.Pointer(&d.ctx[0]))
		d.ready = false
		_ = d.initCtx()
	}
}

func (d *vpxDecoder) close() {
	if d.ready {
		vpxCodecDestroy(unsafe.Pointer(&d.ctx[0]))
		d.ready = false
	}
}
