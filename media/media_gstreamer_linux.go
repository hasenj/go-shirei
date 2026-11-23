//go:build linux && !android

package media

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
)

// GStreamer 1.0 ABI constants (stable across all GStreamer 1.x releases).
const (
	gstSecond        = uint64(1_000_000_000)
	gstClockTimeNone = ^uint64(0)

	gstStateNull    = 1
	gstStateReady   = 2
	gstStatePaused  = 3
	gstStatePlaying = 4

	gstStateChangeFailure = 0

	gstFormatTime = 3

	gstSeekFlagFlush       = 1 << 0
	gstSeekFlagAccurate    = 1 << 1
	gstSeekFlagKeyUnit     = 1 << 2
	gstSeekFlagSnapNearest = 1 << 5

	gstMapRead = 1 << 0

	// GstBuffer.pts offset on 64-bit Linux (64-byte GstMiniObject + 8-byte pool).
	gstBufOffPTS = 72

	// GstMapInfo layout on 64-bit Linux.
	gstMapInfoBufSize = 160
	gstMapOffData     = 16
	gstMapOffSize     = 24

	// GstVideoMeta layout on 64-bit Linux.
	gstVMetaOffWidth   = 36
	gstVMetaOffHeight  = 40
	gstVMetaOffNPlanes = 44
	gstVMetaOffOffsets = 48 // 4 * uint64
	gstVMetaOffStrides = 80 // 4 * int32

	// GValue size on 64-bit Linux (GType + 2*8-byte union).
	gValueBufSize = 32
)

var (
	gstOnce sync.Once
	gstErr  error

	gstInitCheck               func(argc, argv unsafe.Pointer) int32
	gstElementFactoryMake      func(factoryName, name *byte) uintptr
	gstParseBinFromDescription func(desc *byte, ghostUnlinked int32, errOut *uintptr) uintptr
	gstUtilSetObjectArg        func(obj uintptr, name, val *byte)
	gstBinGetByName            func(bin uintptr, name *byte) uintptr
	gstElementSetState         func(elem uintptr, state int32) int32
	gstElementGetState         func(elem uintptr, state, pending *int32, timeout uint64) int32
	gstElementQueryDuration    func(elem uintptr, format int32, dur *int64) int32
	gstElementSeekSimple       func(elem uintptr, format, flags int32, seekPos int64) int32
	gstElementGetStaticPad     func(elem uintptr, name *byte) uintptr
	gstPadGetCurrentCaps       func(pad uintptr) uintptr
	gstCapsGetStructure        func(caps uintptr, index uint32) uintptr
	gstStructureGetInt         func(s uintptr, field *byte, val *int32) int32
	gstStructureGetFraction    func(s uintptr, field *byte, num, den *int32) int32
	gstSampleGetBuffer         func(sample uintptr) uintptr
	gstSampleGetCaps           func(sample uintptr) uintptr
	gstBufferMap               func(buf uintptr, info unsafe.Pointer, flags int32) int32
	gstBufferUnmap             func(buf uintptr, info unsafe.Pointer)
	gstBufferGetMeta           func(buf uintptr, api uintptr) uintptr
	gstMiniObjectUnref         func(obj uintptr)
	gstObjectUnref             func(obj uintptr)
	gstElementGetType          func() uintptr

	gstAppSinkTryPullSample  func(sink uintptr, timeout uint64) uintptr
	gstAppSinkTryPullPreroll func(sink uintptr, timeout uint64) uintptr
	gstAppSinkIsEOS          func(sink uintptr) int32

	gstVideoMetaAPIGetType func() uintptr

	gValueInit         func(val unsafe.Pointer, gtype uintptr) unsafe.Pointer
	gValueSetObject    func(val unsafe.Pointer, obj uintptr)
	gValueUnset        func(val unsafe.Pointer)
	gObjectSetProperty func(obj uintptr, name *byte, val unsafe.Pointer)

	gFilenameToURI func(filename *byte, hostname unsafe.Pointer, errOut *uintptr) uintptr
	gFree          func(ptr uintptr)
	gErrorFree     func(errPtr uintptr)
)

func loadGStreamer() error {
	gstOnce.Do(func() {
		if unsafe.Sizeof(uintptr(0)) != 8 {
			gstErr = errors.New("media: GStreamer purego backend requires 64-bit Linux")
			return
		}
		hGst, err := purego.Dlopen("libgstreamer-1.0.so.0", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			gstErr = err
			return
		}
		hApp, err := purego.Dlopen("libgstapp-1.0.so.0", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			gstErr = err
			return
		}
		hGObj, err := purego.Dlopen("libgobject-2.0.so.0", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			gstErr = err
			return
		}
		hGLib, err := purego.Dlopen("libglib-2.0.so.0", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			gstErr = err
			return
		}
		hVid, _ := purego.Dlopen("libgstvideo-1.0.so.0", purego.RTLD_NOW|purego.RTLD_GLOBAL)

		type symSpec struct {
			h    uintptr
			fn   any
			name string
		}
		syms := []symSpec{
			{hGst, &gstInitCheck, "gst_init_check"},
			{hGst, &gstElementFactoryMake, "gst_element_factory_make"},
			{hGst, &gstParseBinFromDescription, "gst_parse_bin_from_description"},
			{hGst, &gstUtilSetObjectArg, "gst_util_set_object_arg"},
			{hGst, &gstBinGetByName, "gst_bin_get_by_name"},
			{hGst, &gstElementSetState, "gst_element_set_state"},
			{hGst, &gstElementGetState, "gst_element_get_state"},
			{hGst, &gstElementQueryDuration, "gst_element_query_duration"},
			{hGst, &gstElementSeekSimple, "gst_element_seek_simple"},
			{hGst, &gstElementGetStaticPad, "gst_element_get_static_pad"},
			{hGst, &gstPadGetCurrentCaps, "gst_pad_get_current_caps"},
			{hGst, &gstCapsGetStructure, "gst_caps_get_structure"},
			{hGst, &gstStructureGetInt, "gst_structure_get_int"},
			{hGst, &gstStructureGetFraction, "gst_structure_get_fraction"},
			{hGst, &gstSampleGetBuffer, "gst_sample_get_buffer"},
			{hGst, &gstSampleGetCaps, "gst_sample_get_caps"},
			{hGst, &gstBufferMap, "gst_buffer_map"},
			{hGst, &gstBufferUnmap, "gst_buffer_unmap"},
			{hGst, &gstBufferGetMeta, "gst_buffer_get_meta"},
			{hGst, &gstMiniObjectUnref, "gst_mini_object_unref"},
			{hGst, &gstObjectUnref, "gst_object_unref"},
			{hGst, &gstElementGetType, "gst_element_get_type"},
			{hApp, &gstAppSinkTryPullSample, "gst_app_sink_try_pull_sample"},
			{hApp, &gstAppSinkTryPullPreroll, "gst_app_sink_try_pull_preroll"},
			{hApp, &gstAppSinkIsEOS, "gst_app_sink_is_eos"},
			{hGObj, &gValueInit, "g_value_init"},
			{hGObj, &gValueSetObject, "g_value_set_object"},
			{hGObj, &gValueUnset, "g_value_unset"},
			{hGObj, &gObjectSetProperty, "g_object_set_property"},
			{hGLib, &gFilenameToURI, "g_filename_to_uri"},
			{hGLib, &gFree, "g_free"},
			{hGLib, &gErrorFree, "g_error_free"},
		}
		for _, s := range syms {
			if err := registerCodecFunc(s.fn, s.h, s.name); err != nil {
				gstErr = fmt.Errorf("media: gstreamer symbol %s: %w", s.name, err)
				return
			}
		}
		if hVid != 0 {
			_ = registerCodecFunc(&gstVideoMetaAPIGetType, hVid, "gst_video_meta_api_get_type")
		}
		if gstInitCheck(nil, nil) == 0 {
			gstErr = errors.New("media: gst_init_check failed")
			return
		}
	})
	return gstErr
}

func cstrBytes(s string) []byte {
	b := make([]byte, len(s)+1)
	copy(b, s)
	return b
}

func setObjElementProp(obj uintptr, prop string, elem uintptr) {
	var gv [gValueBufSize / 8]uint64
	gValueInit(unsafe.Pointer(&gv[0]), gstElementGetType())
	gValueSetObject(unsafe.Pointer(&gv[0]), elem)
	p := cstrBytes(prop)
	gObjectSetProperty(obj, &p[0], unsafe.Pointer(&gv[0]))
	runtime.KeepAlive(p)
	gValueUnset(unsafe.Pointer(&gv[0]))
	runtime.KeepAlive(&gv)
}

type gstreamerEngine struct {
	mu       sync.Mutex
	pipeline uintptr
	vsink    uintptr
	asink    uintptr
	meta     probeInfo
	closed   bool
}

func openGStreamerEngine(abs string) (streamEngine, error) {
	if err := loadGStreamer(); err != nil {
		return nil, err
	}

	cpath := cstrBytes(abs)
	var gerr uintptr
	uriPtr := gFilenameToURI(&cpath[0], nil, &gerr)
	runtime.KeepAlive(cpath)
	if gerr != 0 {
		gErrorFree(gerr)
	}
	if uriPtr == 0 {
		return nil, errors.New("media: gstreamer g_filename_to_uri failed")
	}
	defer gFree(uriPtr)

	pf := cstrBytes("playbin")
	pn := cstrBytes("shirei-playbin")
	pipe := gstElementFactoryMake(&pf[0], &pn[0])
	runtime.KeepAlive(pf)
	runtime.KeepAlive(pn)
	if pipe == 0 {
		return nil, errors.New("media: gstreamer playbin plugin unavailable")
	}

	// Always negotiate NV12: matches hardware/VA-API and modern software decoders
	// natively without extra videoconvert passes; publishNV12 uploads directly to
	// GPU GLES when supported and falls back to CPU RGBA on software renderers.
	vdesc := cstrBytes("videoconvert ! video/x-raw,format=NV12 ! appsink name=vsink sync=false max-buffers=4 drop=false")
	gerr = 0
	vbin := gstParseBinFromDescription(&vdesc[0], 1, &gerr)
	runtime.KeepAlive(vdesc)
	if gerr != 0 {
		gErrorFree(gerr)
	}
	if vbin == 0 {
		gstObjectUnref(pipe)
		return nil, errors.New("media: gstreamer video appsink bin unavailable")
	}

	adesc := cstrBytes("audioconvert ! audioresample ! audio/x-raw,format=F32LE,rate=44100,channels=2,layout=interleaved ! appsink name=asink sync=false max-buffers=16 drop=false")
	gerr = 0
	abin := gstParseBinFromDescription(&adesc[0], 1, &gerr)
	runtime.KeepAlive(adesc)
	if gerr != 0 {
		gErrorFree(gerr)
	}
	if abin == 0 {
		gstObjectUnref(vbin)
		gstObjectUnref(pipe)
		return nil, errors.New("media: gstreamer audio appsink bin unavailable")
	}

	vsName := cstrBytes("vsink")
	vsink := gstBinGetByName(vbin, &vsName[0])
	runtime.KeepAlive(vsName)
	asName := cstrBytes("asink")
	asink := gstBinGetByName(abin, &asName[0])
	runtime.KeepAlive(asName)
	if vsink == 0 || asink == 0 {
		if vsink != 0 {
			gstObjectUnref(vsink)
		}
		if asink != 0 {
			gstObjectUnref(asink)
		}
		gstObjectUnref(vbin)
		gstObjectUnref(abin)
		gstObjectUnref(pipe)
		return nil, errors.New("media: gstreamer could not locate appsinks")
	}

	// Configure playbin URI, video+audio flags (no subtitle overlay), and custom sinks.
	uProp := cstrBytes("uri")
	gstUtilSetObjectArg(pipe, &uProp[0], (*byte)(unsafe.Pointer(uriPtr)))
	runtime.KeepAlive(uProp)
	fProp := cstrBytes("flags")
	fVal := cstrBytes("video+audio")
	gstUtilSetObjectArg(pipe, &fProp[0], &fVal[0])
	runtime.KeepAlive(fProp)
	runtime.KeepAlive(fVal)

	// playbin sinks the floating references on vbin and abin via video-sink/audio-sink.
	setObjElementProp(pipe, "video-sink", vbin)
	setObjElementProp(pipe, "audio-sink", abin)

	cleanup := func() {
		gstElementSetState(pipe, gstStateNull)
		gstObjectUnref(vsink)
		gstObjectUnref(asink)
		gstObjectUnref(pipe)
	}

	if gstElementSetState(pipe, gstStatePaused) == gstStateChangeFailure {
		cleanup()
		return nil, errors.New("media: gstreamer failed to start preroll")
	}
	var curState, pendState int32
	rc := gstElementGetState(pipe, &curState, &pendState, 3*gstSecond)
	if rc == gstStateChangeFailure || curState < gstStatePaused {
		cleanup()
		return nil, errors.New("media: gstreamer preroll failed (missing codec plugin?)")
	}

	// Inspect negotiated video caps from vsink's sink pad (or preroll sample).
	w, h, fps := gstProbeVideoSink(vsink)
	if w <= 0 || h <= 0 {
		cleanup()
		return nil, errors.New("media: gstreamer found no decodable video stream")
	}

	var durNs int64
	var durSec float64
	if gstElementQueryDuration(pipe, gstFormatTime, &durNs) != 0 && durNs > 0 {
		durSec = float64(durNs) / 1e9
	}

	hasAudio := gstSinkHasCaps(asink)
	if fps <= 0 || fps > 240 {
		fps = 30
	}

	return &gstreamerEngine{
		pipeline: pipe,
		vsink:    vsink,
		asink:    asink,
		meta: probeInfo{
			width:    w,
			height:   h,
			fps:      fps,
			duration: durSec,
			hasAudio: hasAudio,
		},
	}, nil
}

func gstProbeVideoSink(vsink uintptr) (w, h int, fps float64) {
	padName := cstrBytes("sink")
	pad := gstElementGetStaticPad(vsink, &padName[0])
	runtime.KeepAlive(padName)
	if pad != 0 {
		caps := gstPadGetCurrentCaps(pad)
		gstObjectUnref(pad)
		if caps != 0 {
			w, h, fps = gstCapsVideoInfo(caps)
			gstMiniObjectUnref(caps)
			if w > 0 && h > 0 {
				return w, h, fps
			}
		}
	}
	sample := gstAppSinkTryPullPreroll(vsink, 200*1_000_000)
	if sample != 0 {
		if caps := gstSampleGetCaps(sample); caps != 0 {
			w, h, fps = gstCapsVideoInfo(caps)
		}
		gstMiniObjectUnref(sample)
	}
	return w, h, fps
}

func gstSinkHasCaps(sink uintptr) bool {
	padName := cstrBytes("sink")
	pad := gstElementGetStaticPad(sink, &padName[0])
	runtime.KeepAlive(padName)
	if pad == 0 {
		return false
	}
	caps := gstPadGetCurrentCaps(pad)
	gstObjectUnref(pad)
	if caps == 0 {
		return false
	}
	gstMiniObjectUnref(caps)
	return true
}

func gstCapsVideoInfo(caps uintptr) (w, h int, fps float64) {
	st := gstCapsGetStructure(caps, 0)
	if st == 0 {
		return 0, 0, 0
	}
	var wi, hi, num, den int32
	fw := cstrBytes("width")
	fh := cstrBytes("height")
	ff := cstrBytes("framerate")
	gstStructureGetInt(st, &fw[0], &wi)
	gstStructureGetInt(st, &fh[0], &hi)
	if gstStructureGetFraction(st, &ff[0], &num, &den) != 0 && num > 0 && den > 0 {
		fps = float64(num) / float64(den)
	}
	runtime.KeepAlive(fw)
	runtime.KeepAlive(fh)
	runtime.KeepAlive(ff)
	return int(wi), int(hi), fps
}

func (e *gstreamerEngine) info() probeInfo { return e.meta }

func (e *gstreamerEngine) close() {
	e.mu.Lock()
	if e.closed {
		e.mu.Unlock()
		return
	}
	e.closed = true
	pipe, vsink, asink := e.pipeline, e.vsink, e.asink
	e.pipeline, e.vsink, e.asink = 0, 0, 0
	e.mu.Unlock()

	if pipe != 0 {
		gstElementSetState(pipe, gstStateNull)
	}
	if vsink != 0 {
		gstObjectUnref(vsink)
	}
	if asink != 0 {
		gstObjectUnref(asink)
	}
	if pipe != 0 {
		gstObjectUnref(pipe)
	}
}

func (e *gstreamerEngine) run(p *streamPlayer) {
	var (
		done   <-chan struct{}
		cancel context.CancelFunc
	)
	stopSession := func() {
		if cancel == nil {
			return
		}
		cancel()
		<-done
		cancel = nil
		done = nil
	}
	defer stopSession()

	startSession := func(startAt float64, first bool) {
		stopSession()
		p.resetAudioClock(startAt)
		ctx, c := context.WithCancel(context.Background())
		ch := make(chan struct{})
		cancel = c
		done = ch
		go func() {
			defer close(ch)
			e.runSession(ctx, p, startAt, first)
		}()
	}

	select {
	case <-p.stop:
		return
	default:
	}
	if target, ok := p.takeSeek(); ok {
		startSession(target, false)
	} else {
		startSession(0, true)
	}

	for {
		select {
		case <-p.stop:
			return
		case <-p.wake:
			if target, ok := p.takeSeek(); ok {
				startSession(target, false)
			}
		case <-done:
			cancel = nil
			done = nil
			p.mu.Lock()
			closed := p.closed
			p.mu.Unlock()
			if closed {
				return
			}
			if target, ok := p.takeSeek(); ok {
				startSession(target, false)
				continue
			}
			if p.armLoop() {
				startSession(0, false)
			}
		}
	}
}

func (e *gstreamerEngine) runSession(ctx context.Context, p *streamPlayer, startAt float64, first bool) {
	e.mu.Lock()
	pipe, vsink, asink := e.pipeline, e.vsink, e.asink
	e.mu.Unlock()
	if pipe == 0 || vsink == 0 {
		return
	}

	if !first || startAt > 0.001 {
		posNs := int64(startAt * 1e9)
		if posNs < 0 {
			posNs = 0
		}
		var ok int32
		if !p.Playing() {
			// Scrubbing while paused: snap to nearest keyframe so dragging
			// updates promptly without decoding a full GOP per scrub tick.
			ok = gstElementSeekSimple(pipe, gstFormatTime, gstSeekFlagFlush|gstSeekFlagKeyUnit|gstSeekFlagSnapNearest, posNs)
			if ok == 0 {
				ok = gstElementSeekSimple(pipe, gstFormatTime, gstSeekFlagFlush|gstSeekFlagKeyUnit, posNs)
			}
		}
		if ok == 0 {
			ok = gstElementSeekSimple(pipe, gstFormatTime, gstSeekFlagFlush|gstSeekFlagAccurate, posNs)
		}
		if ok == 0 && startAt <= 0.001 {
			// Some demuxers reject flushing seeks to 0 while paused; fall back to READY->PLAYING.
			gstElementSetState(pipe, gstStateReady)
		}
	}
	gstElementSetState(pipe, gstStatePlaying)

	var wg sync.WaitGroup
	if e.meta.hasAudio && asink != 0 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e.audioLoop(ctx, p, asink)
			if ctx.Err() == nil {
				p.dropAudioClock()
			}
		}()
	}

	e.videoLoop(ctx, p, vsink, startAt)
	wg.Wait()
}

func (e *gstreamerEngine) videoLoop(ctx context.Context, p *streamPlayer, vsink uintptr, startAt float64) {
	firstFrame := true
	frameIdx := 0
	emptyPolls := 0
	var lastPublish time.Time
	var vmetaAPI uintptr
	if gstVideoMetaAPIGetType != nil {
		vmetaAPI = gstVideoMetaAPIGetType()
	}

	for ctx.Err() == nil {
		sample := gstAppSinkTryPullSample(vsink, 10*1_000_000)
		if sample == 0 {
			emptyPolls++
			if gstAppSinkIsEOS(vsink) != 0 && (frameIdx > 0 || emptyPolls >= 50) {
				return
			}
			continue
		}
		emptyPolls = 0

		buf := gstSampleGetBuffer(sample)
		caps := gstSampleGetCaps(sample)
		if buf == 0 {
			gstMiniObjectUnref(sample)
			continue
		}

		ptsNs := binary.LittleEndian.Uint64(unsafe.Slice((*byte)(unsafe.Pointer(buf)), gstBufOffPTS+8)[gstBufOffPTS:])
		var pts float64
		if ptsNs != gstClockTimeNone {
			pts = float64(ptsNs) / 1e9
		} else if p.fps > 0 {
			pts = startAt + float64(frameIdx)/p.fps
		}
		frameIdx++

		isFirst := firstFrame
		if firstFrame {
			firstFrame = false
			if ptsNs != gstClockTimeNone {
				p.alignClock(pts)
			}
		} else if !p.waitUntil(ctx, pts) {
			gstMiniObjectUnref(sample)
			return
		}

		// Drop late frames when behind so video stays locked to audio on slower GPUs/VMs.
		if !isFirst && p.fps > 0 {
			late := p.positionSeconds() - pts
			if late > 1.5/p.fps && time.Since(lastPublish) < 100*time.Millisecond {
				gstMiniObjectUnref(sample)
				continue
			}
		}

		w, h := e.meta.width, e.meta.height
		if caps != 0 {
			if cw, ch, _ := gstCapsVideoInfo(caps); cw > 0 && ch > 0 {
				w, h = cw, ch
			}
		}

		var mapBuf [gstMapInfoBufSize / 8]uint64
		mapInfo := unsafe.Slice((*byte)(unsafe.Pointer(&mapBuf[0])), gstMapInfoBufSize)
		if gstBufferMap(buf, unsafe.Pointer(&mapBuf[0]), gstMapRead) != 0 {
			dataPtr := uintptr(binary.LittleEndian.Uint64(mapInfo[gstMapOffData : gstMapOffData+8]))
			dataSz := int(binary.LittleEndian.Uint64(mapInfo[gstMapOffSize : gstMapOffSize+8]))
			if dataPtr != 0 && dataSz > 0 && w > 0 && h > 0 {
				raw := unsafe.Slice((*byte)(unsafe.Pointer(dataPtr)), dataSz)
				yOff, uvOff, yStride, uvStride := gstNV12Layout(buf, vmetaAPI, w, h)
				if yOff >= 0 && uvOff >= 0 &&
					yOff+h*yStride <= len(raw) &&
					uvOff+(h/2)*uvStride <= len(raw) {
					p.publishNV12(raw[yOff:], raw[uvOff:], yStride, uvStride, w, h, false)
					lastPublish = time.Now()
				}
			}
			gstBufferUnmap(buf, unsafe.Pointer(&mapBuf[0]))
		}
		runtime.KeepAlive(&mapBuf)
		gstMiniObjectUnref(sample)
	}
}

func gstNV12Layout(buf, vmetaAPI uintptr, w, h int) (yOff, uvOff, yStride, uvStride int) {
	if vmetaAPI != 0 {
		if vm := gstBufferGetMeta(buf, vmetaAPI); vm != 0 {
			b := unsafe.Slice((*byte)(unsafe.Pointer(vm)), 100)
			nPlanes := binary.LittleEndian.Uint32(b[gstVMetaOffNPlanes : gstVMetaOffNPlanes+4])
			if nPlanes >= 2 {
				yOff = int(binary.LittleEndian.Uint64(b[gstVMetaOffOffsets : gstVMetaOffOffsets+8]))
				uvOff = int(binary.LittleEndian.Uint64(b[gstVMetaOffOffsets+8 : gstVMetaOffOffsets+16]))
				yStride = int(int32(binary.LittleEndian.Uint32(b[gstVMetaOffStrides : gstVMetaOffStrides+4])))
				uvStride = int(int32(binary.LittleEndian.Uint32(b[gstVMetaOffStrides+4 : gstVMetaOffStrides+8])))
				if yStride >= w && uvStride >= (w&^1) {
					return yOff, uvOff, yStride, uvStride
				}
			}
		}
	}
	yStride = (w + 3) &^ 3
	uvStride = yStride
	rh := (h + 1) &^ 1
	yOff = 0
	uvOff = yStride * rh
	return yOff, uvOff, yStride, uvStride
}

func (e *gstreamerEngine) audioLoop(ctx context.Context, p *streamPlayer, asink uintptr) {
	gotSample := false
	emptyPolls := 0
	for ctx.Err() == nil {
		sample := gstAppSinkTryPullSample(asink, 10*1_000_000)
		if sample == 0 {
			emptyPolls++
			if gstAppSinkIsEOS(asink) != 0 && (gotSample || emptyPolls >= 50) {
				return
			}
			continue
		}
		gotSample = true
		emptyPolls = 0
		if !p.audioEnabled() {
			gstMiniObjectUnref(sample)
			continue
		}
		buf := gstSampleGetBuffer(sample)
		if buf == 0 {
			gstMiniObjectUnref(sample)
			continue
		}
		var mapBuf [gstMapInfoBufSize / 8]uint64
		mapInfo := unsafe.Slice((*byte)(unsafe.Pointer(&mapBuf[0])), gstMapInfoBufSize)
		if gstBufferMap(buf, unsafe.Pointer(&mapBuf[0]), gstMapRead) != 0 {
			dataPtr := uintptr(binary.LittleEndian.Uint64(mapInfo[gstMapOffData : gstMapOffData+8]))
			dataSz := int(binary.LittleEndian.Uint64(mapInfo[gstMapOffSize : gstMapOffSize+8]))
			nFloats := (dataSz / 4) &^ 1
			if dataPtr != 0 && nFloats > 0 {
				samples := unsafe.Slice((*float32)(unsafe.Pointer(dataPtr)), nFloats)
				ok := p.writeAudio(ctx, samples)
				gstBufferUnmap(buf, unsafe.Pointer(&mapBuf[0]))
				runtime.KeepAlive(&mapBuf)
				gstMiniObjectUnref(sample)
				if !ok {
					return
				}
				continue
			}
			gstBufferUnmap(buf, unsafe.Pointer(&mapBuf[0]))
		}
		runtime.KeepAlive(&mapBuf)
		gstMiniObjectUnref(sample)
	}
}
