//go:build (darwin && !ios) || (linux && !android) || windows

package media

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"runtime"
	"sync"
	"unsafe"
)

// dav1d 1.x (ABI 7) struct offsets on 64-bit platforms.
const (
	dav1dSettingsBufSize = 256
	dav1dDataBufSize     = 128
	dav1dPicBufSize      = 512

	dav1dDataOffTimestamp = 24

	dav1dPicOffSeqHdr    = 0
	dav1dPicOffData0     = 16
	dav1dPicOffData1     = 24
	dav1dPicOffData2     = 32
	dav1dPicOffStride0   = 40
	dav1dPicOffStride1   = 48
	dav1dPicOffWidth     = 56
	dav1dPicOffHeight    = 60
	dav1dPicOffLayout    = 64
	dav1dPicOffBPC       = 68
	dav1dPicOffTimestamp = 72

	dav1dSeqHdrOffMatrix     = 24
	dav1dSeqHdrOffColorRange = 33

	dav1dLayoutI400 = 0
	dav1dLayoutI420 = 1
	dav1dLayoutI422 = 2
	dav1dLayoutI444 = 3

	dav1dMatrixBT709 = 1
)

var (
	dav1dOnce sync.Once
	dav1dErr  error

	dav1dDefaultSettings func(s unsafe.Pointer)
	dav1dOpen            func(ctxOut *uintptr, s unsafe.Pointer) int32
	dav1dClose           func(ctxOut *uintptr)
	dav1dFlush           func(ctx uintptr)
	dav1dDataCreate      func(data unsafe.Pointer, sz uintptr) unsafe.Pointer
	dav1dDataUnref       func(data unsafe.Pointer)
	dav1dSendData        func(ctx uintptr, data unsafe.Pointer) int32
	dav1dGetPicture      func(ctx uintptr, pic unsafe.Pointer) int32
	dav1dPictureUnref    func(pic unsafe.Pointer)
)

func init() {
	registerVideoDecoder(videoCodecAV1, dav1dAvailable, newDav1dDecoder)
}

func dav1dEAGAIN() int32 {
	if runtime.GOOS == "darwin" {
		return -35
	}
	return -11
}

func loadDav1d() error {
	dav1dOnce.Do(func() {
		if unsafe.Sizeof(uintptr(0)) != 8 {
			dav1dErr = errors.New("media: dav1d purego bindings require 64-bit platform")
			return
		}
		var names []string
		switch runtime.GOOS {
		case "darwin":
			names = []string{"libdav1d.7.dylib", "libdav1d.dylib"}
		case "windows":
			names = []string{"dav1d.dll", "libdav1d.dll", "libdav1d-7.dll"}
		default:
			names = []string{"libdav1d.so.7", "libdav1d.so"}
		}
		h, err := openCodecLib(names)
		if err != nil {
			dav1dErr = err
			return
		}
		syms := []struct {
			fn   any
			name string
		}{
			{&dav1dDefaultSettings, "dav1d_default_settings"},
			{&dav1dOpen, "dav1d_open"},
			{&dav1dClose, "dav1d_close"},
			{&dav1dFlush, "dav1d_flush"},
			{&dav1dDataCreate, "dav1d_data_create"},
			{&dav1dDataUnref, "dav1d_data_unref"},
			{&dav1dSendData, "dav1d_send_data"},
			{&dav1dGetPicture, "dav1d_get_picture"},
			{&dav1dPictureUnref, "dav1d_picture_unref"},
		}
		for _, s := range syms {
			if err := registerCodecFunc(s.fn, h, s.name); err != nil {
				dav1dErr = fmt.Errorf("media: dav1d symbol %s: %w", s.name, err)
				return
			}
		}
	})
	return dav1dErr
}

func dav1dAvailable() bool {
	return loadDav1d() == nil
}

type dav1dDecoder struct {
	ctx uintptr
}

func newDav1dDecoder(p mediaProbe) (videoDecoder, error) {
	if err := loadDav1d(); err != nil {
		return nil, err
	}
	var settings [dav1dSettingsBufSize]byte
	dav1dDefaultSettings(unsafe.Pointer(&settings[0]))
	nThreads := runtime.NumCPU()
	if nThreads > 8 {
		nThreads = 8
	}
	if nThreads < 1 {
		nThreads = 1
	}
	binary.LittleEndian.PutUint32(settings[0:4], uint32(nThreads))
	binary.LittleEndian.PutUint32(settings[4:8], 2) // low-latency frame delay

	var ctx uintptr
	if rc := dav1dOpen(&ctx, unsafe.Pointer(&settings[0])); rc < 0 || ctx == 0 {
		return nil, fmt.Errorf("media: dav1d_open failed (%d)", rc)
	}
	return &dav1dDecoder{ctx: ctx}, nil
}

func (d *dav1dDecoder) decode(pkt demuxPacket, emit func(decodedVideoFrame)) error {
	if d.ctx == 0 || len(pkt.Data) == 0 {
		return nil
	}
	var data [dav1dDataBufSize]byte
	bufPtr := dav1dDataCreate(unsafe.Pointer(&data[0]), uintptr(len(pkt.Data)))
	if bufPtr == nil {
		return errors.New("media: dav1d_data_create failed")
	}
	copy(unsafe.Slice((*byte)(bufPtr), len(pkt.Data)), pkt.Data)
	tsUs := int64(math.Round(pkt.PTS * 1e6))
	binary.LittleEndian.PutUint64(data[dav1dDataOffTimestamp:dav1dDataOffTimestamp+8], uint64(tsUs))

	eagain := dav1dEAGAIN()
	for {
		rc := dav1dSendData(d.ctx, unsafe.Pointer(&data[0]))
		if rc == 0 {
			break
		}
		if rc == eagain {
			if !d.drainPictures(emit) {
				break
			}
			continue
		}
		dav1dDataUnref(unsafe.Pointer(&data[0]))
		return nil
	}
	d.drainPictures(emit)
	return nil
}

func (d *dav1dDecoder) flush(emit func(decodedVideoFrame)) {
	if d.ctx == 0 {
		return
	}
	d.drainPictures(emit)
}

func (d *dav1dDecoder) drainPictures(emit func(decodedVideoFrame)) bool {
	got := false
	eagain := dav1dEAGAIN()
	var pic [dav1dPicBufSize]byte
	for {
		clear(pic[:])
		rc := dav1dGetPicture(d.ctx, unsafe.Pointer(&pic[0]))
		if rc == eagain || rc < 0 {
			break
		}
		got = true
		d.emitPicture(&pic, emit)
		dav1dPictureUnref(unsafe.Pointer(&pic[0]))
	}
	return got
}

func (d *dav1dDecoder) emitPicture(pic *[dav1dPicBufSize]byte, emit func(decodedVideoFrame)) {
	w := int(int32(binary.LittleEndian.Uint32(pic[dav1dPicOffWidth : dav1dPicOffWidth+4])))
	h := int(int32(binary.LittleEndian.Uint32(pic[dav1dPicOffHeight : dav1dPicOffHeight+4])))
	layout := int(int32(binary.LittleEndian.Uint32(pic[dav1dPicOffLayout : dav1dPicOffLayout+4])))
	bpc := int(int32(binary.LittleEndian.Uint32(pic[dav1dPicOffBPC : dav1dPicOffBPC+4])))
	tsUs := int64(binary.LittleEndian.Uint64(pic[dav1dPicOffTimestamp : dav1dPicOffTimestamp+8]))

	p0 := uintptr(binary.LittleEndian.Uint64(pic[dav1dPicOffData0 : dav1dPicOffData0+8]))
	p1 := uintptr(binary.LittleEndian.Uint64(pic[dav1dPicOffData1 : dav1dPicOffData1+8]))
	p2 := uintptr(binary.LittleEndian.Uint64(pic[dav1dPicOffData2 : dav1dPicOffData2+8]))
	s0 := int(int64(binary.LittleEndian.Uint64(pic[dav1dPicOffStride0 : dav1dPicOffStride0+8])))
	s1 := int(int64(binary.LittleEndian.Uint64(pic[dav1dPicOffStride1 : dav1dPicOffStride1+8])))
	if w <= 0 || h <= 0 || p0 == 0 || s0 <= 0 {
		return
	}

	var xShift, yShift int
	switch layout {
	case dav1dLayoutI420:
		xShift, yShift = 1, 1
	case dav1dLayoutI422:
		xShift, yShift = 1, 0
	case dav1dLayoutI444:
		xShift, yShift = 0, 0
	default:
		xShift, yShift = 0, 0
	}

	bt709 := h >= 720
	fullRange := false
	seqPtr := uintptr(binary.LittleEndian.Uint64(pic[dav1dPicOffSeqHdr : dav1dPicOffSeqHdr+8]))
	if seqPtr != 0 {
		seq := unsafe.Slice((*byte)(unsafe.Pointer(seqPtr)), 40)
		mtrx := int32(binary.LittleEndian.Uint32(seq[dav1dSeqHdrOffMatrix : dav1dSeqHdrOffMatrix+4]))
		if mtrx == dav1dMatrixBT709 {
			bt709 = true
		} else if mtrx == 6 || mtrx == 5 {
			bt709 = false
		}
		fullRange = seq[dav1dSeqHdrOffColorRange] != 0
	}

	ySlice := unsafe.Slice((*byte)(unsafe.Pointer(p0)), h*s0)
	var uSlice, vSlice []byte
	if layout != dav1dLayoutI400 && p1 != 0 && p2 != 0 && s1 > 0 {
		uvH := (h + (1 << yShift) - 1) >> yShift
		uSlice = unsafe.Slice((*byte)(unsafe.Pointer(p1)), uvH*s1)
		vSlice = unsafe.Slice((*byte)(unsafe.Pointer(p2)), uvH*s1)
	}

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
		bitDepth:  bpc,
		bt709:     bt709,
		fullRange: fullRange,
		pts:       float64(tsUs) / 1e6,
	})
}

func (d *dav1dDecoder) reset() {
	if d.ctx != 0 {
		dav1dFlush(d.ctx)
	}
}

func (d *dav1dDecoder) close() {
	if d.ctx != 0 {
		dav1dClose(&d.ctx)
		d.ctx = 0
	}
}
