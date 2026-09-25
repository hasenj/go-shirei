//go:build darwin && !ios

package gpurender

import (
	"fmt"
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego/cstrings"
	"github.com/ebitengine/purego/objc"
)

const (
	ring       = 3
	maxTargets = 8
)

const metalShaderSrc = `#include <metal_stdlib>
using namespace metal;
struct Quad { float4 dst; float4 uv; float4 color; float4 color2; };
struct Uni { float2 viewport; };
struct VOut {
    float4 position [[position]];
    float4 color;
    float4 color2;
    float2 uv;
    float2 corner;
};
vertex VOut vs_main(uint vid [[vertex_id]], uint iid [[instance_id]],
                    constant Quad *quads [[buffer(0)]],
                    constant Uni &uni [[buffer(1)]]) {
    float2 c = float2(float((vid == 1 || vid == 3 || vid == 4) ? 1 : 0),
                      float((vid == 2 || vid == 4 || vid == 5) ? 1 : 0));
    Quad q = quads[iid];
    float2 pos = q.dst.xy + c * q.dst.zw;
    float2 ndc;
    ndc.x = pos.x / uni.viewport.x * 2.0 - 1.0;
    ndc.y = 1.0 - pos.y / uni.viewport.y * 2.0;
    VOut o;
    o.position = float4(ndc, 0.0, 1.0);
    o.color = q.color;
    o.color2 = q.color2;
    o.uv = q.uv.xy + c * q.uv.zw;
    o.corner = c;
    return o;
}
struct ClipUni { uint n; uint p0; uint p1; uint p2; float4 rects[4]; float4 radii[4]; };
float sdRoundBox(float2 p, float2 b, float4 r) {
    r.xy = (p.x > 0.0) ? r.xy : r.zw;
    r.x  = (p.y > 0.0) ? r.x  : r.y;
    float2 q = abs(p) - b + r.x;
    return min(max(q.x, q.y), 0.0) + length(max(q, 0.0)) - r.x;
}
float clipCov(float2 pos, constant ClipUni &clip) {
    float cov = 1.0;
    for (uint i = 0; i < clip.n; i++) {
        float4 rc = clip.rects[i];
        float2 halfv = rc.zw * 0.5;
        float2 p = pos - (rc.xy + halfv);
        p.y = -p.y;
        float4 rad = clip.radii[i];
        float4 r = float4(rad.y, rad.z, rad.x, rad.w);
        cov *= saturate(0.5 - sdRoundBox(p, halfv, r));
    }
    return cov;
}
fragment float4 fs_main(VOut in [[stage_in]],
                        texture2d<float> tex [[texture(0)]],
                        sampler samp [[sampler(0)]],
                        constant uint &mode [[buffer(2)]],
                        constant ClipUni &clip [[buffer(3)]]) {
    float4 outc;
    if (mode == 0) {
        float4 c = mix(in.color, in.color2, in.corner.y);
        outc = float4(c.rgb * c.a, c.a);
    } else if (mode == 1) {
        float cov = tex.sample(samp, in.uv).r;
        float4 c = mix(in.color, in.color2, in.corner.y);
        float a = c.a * cov;
        outc = float4(c.rgb * a, a);
    } else {
        float4 t = tex.sample(samp, in.uv);
        outc = t * in.color.a;
    }
    return outc * clipCov(in.position.xy, clip);
}
`

type clipUni struct {
	N          uint32
	P0, P1, P2 uint32
	Rects      [4][4]float32
	Radii      [4][4]float32
}

type ioTarget struct {
	surf objc.ID
	tex  objc.ID
	w, h int
}

var (
	mtlDevice   objc.ID
	mtlQueue    objc.ID
	mtlPipeline objc.ID
	mtlSampler  objc.ID
	mtlWhite    objc.ID
	mtlGlyph    objc.ID
	mtlColor    objc.ID
	mtlImages   = map[uint32]objc.ID{}
	mtlInstBuf  [ring]objc.ID
	mtlInstTurn int
	mtlTargets  [maxTargets]ioTarget
	mtlDevName  string
	mtlLastErr  string
	mtlUnified  bool

	// Scratch for by-pointer struct args so SyscallN's uintptrescapes
	// does not heap-allocate per batch (encode is main-thread only).
	scratchScissor  mtlScissorRect
	scratchViewport mtlViewport
	scratchRegion   mtlRegion
	scratchClip     clipUni
	scratchMode     uint32
	scratchUni      [2]float32
)

var (
	completeOnce  sync.Once
	completeBlock objc.Block
	inflightCmds  sync.Map // command buffer → dest
)

func ensureCompleteBlock() {
	completeOnce.Do(func() {
		completeBlock = objc.NewBlock(func(_ objc.Block, cmd objc.ID) {
			v, ok := inflightCmds.LoadAndDelete(cmd)
			if !ok {
				return
			}
			surf := v.(unsafe.Pointer)
			onMain(func() {
				if completeFn != nil {
					completeFn(surf)
				}
			})
		})
	})
}

func setErr(format string, args ...any) {
	mtlLastErr = fmt.Sprintf(format, args...)
}

func gpuInit() error {
	if err := bindMetal(); err != nil {
		return err
	}
	if mtlDevice != 0 {
		return nil
	}
	dev := pickDevice()
	if dev == 0 {
		return fmt.Errorf("no Metal device")
	}
	if err := metalCommonInit(dev); err != nil {
		return err
	}
	return nil
}

func DeviceName() string { return mtlDevName }

func pickDevice() objc.ID {
	devs := mtlCopyAllDevices()
	if devs != 0 {
		n := msgCount(devs, sel_count)
		for i := uint(0); i < n; i++ {
			d := msgObjectAtIndex(devs, sel_objectAtIndex, i)
			if msgBool(d, sel_lowPower) && !msgBool(d, sel_removable) {
				d = msgRetain(d, sel_retain)
				msgRelease(devs, sel_release)
				return d
			}
		}
		msgRelease(devs, sel_release)
	}
	return mtlCreateSystemDefaultDevice()
}

func metalCommonInit(device objc.ID) error {
	mtlDevice = device
	if name := msgID(device, sel_name); name != 0 {
		mtlDevName = cstrings.NSStringToString(name)
	} else {
		mtlDevName = "unknown"
	}
	mtlUnified = msgBool(device, sel_hasUnifiedMemory)
	mtlQueue = msgNewQueue(device, sel_newCommandQueue)
	if mtlQueue == 0 {
		return fmt.Errorf("newCommandQueue failed")
	}

	src := nsString(metalShaderSrc)
	var nerr objc.ID
	lib := msgNewLibrary(device, sel_newLibrary, src, 0, &nerr)
	if lib == 0 {
		msg := "compile failed"
		if nerr != 0 {
			msg = nsErr(nerr)
		}
		return fmt.Errorf("shader: %s", msg)
	}
	vs := msgNewFunction(lib, sel_newFunction, nsString("vs_main"))
	fs := msgNewFunction(lib, sel_newFunction, nsString("fs_main"))
	msgRelease(lib, sel_release)
	if vs == 0 || fs == 0 {
		if vs != 0 {
			msgRelease(vs, sel_release)
		}
		if fs != 0 {
			msgRelease(fs, sel_release)
		}
		return fmt.Errorf("shader missing vs_main/fs_main")
	}

	pd := msgInit(msgAlloc(objc.ID(class_MTLPipeDesc), sel_alloc), sel_init)
	msgSetID(pd, sel_setVertexFn, vs)
	msgSetID(pd, sel_setFragmentFn, fs)
	att := colorAtt(pd)
	msgSetU(att, sel_setPixelFormat, mtlPixelFormatBGRA8Unorm)
	msgSetBool(att, sel_setBlending, true)
	msgSetU(att, sel_setSrcRGB, mtlBlendFactorOne)
	msgSetU(att, sel_setDstRGB, mtlBlendFactorOneMinusSourceAlpha)
	msgSetU(att, sel_setSrcA, mtlBlendFactorOne)
	msgSetU(att, sel_setDstA, mtlBlendFactorOneMinusSourceAlpha)
	msgRelease(vs, sel_release)
	msgRelease(fs, sel_release)

	nerr = 0
	mtlPipeline = msgNewPipeline(device, sel_newPipeline, pd, &nerr)
	msgRelease(pd, sel_release)
	if mtlPipeline == 0 {
		msg := "failed"
		if nerr != 0 {
			msg = nsErr(nerr)
		}
		return fmt.Errorf("pipeline: %s", msg)
	}

	sd := msgInit(msgAlloc(objc.ID(class_MTLSampDesc), sel_alloc), sel_init)
	msgSetU(sd, sel_setMinFilter, mtlSamplerMinMagFilterLinear)
	msgSetU(sd, sel_setMagFilter, mtlSamplerMinMagFilterLinear)
	msgSetU(sd, sel_setSAddr, mtlSamplerAddressModeClampToEdge)
	msgSetU(sd, sel_setTAddr, mtlSamplerAddressModeClampToEdge)
	mtlSampler = msgNewSampler(device, sel_newSampler, sd)
	msgRelease(sd, sel_release)

	mtlWhite = makeSharedTex(mtlPixelFormatRGBA8Unorm, 1, 1)
	white := [4]byte{255, 255, 255, 255}
	msgReplaceRegion(mtlWhite, sel_replaceRegion, mtlRegion2D(0, 0, 1, 1), 0, unsafe.Pointer(&white[0]), 4)
	mtlGlyph = makeSharedTex(mtlPixelFormatR8Unorm, glyphAtlasW, glyphAtlasH)
	mtlColor = makeSharedTex(mtlPixelFormatRGBA8Unorm, colorAtlasW, colorAtlasH)
	zeroTex(mtlGlyph, 1)
	zeroTex(mtlColor, 4)
	if mtlWhite == 0 || mtlGlyph == 0 || mtlColor == 0 || mtlSampler == 0 {
		return fmt.Errorf("atlas/sampler alloc failed")
	}
	return nil
}

func storageMode() uint {
	if mtlUnified {
		return mtlStorageModeShared
	}
	return mtlStorageModeManaged
}

func resourceOpts() uint {
	if mtlUnified {
		return mtlResourceStorageModeShared
	}
	return mtlResourceStorageModeManaged
}

func makeSharedTex(fmt uint, w, h int) objc.ID {
	d := msgTex2DDesc(objc.ID(class_MTLTexDesc), sel_tex2DDesc, fmt, uint(w), uint(h), false)
	msgSetU(d, sel_setUsage, mtlTextureUsageShaderRead)
	msgSetU(d, sel_setStorageMode, storageMode())
	tex := msgNewTexture(mtlDevice, sel_newTexture, d)
	return tex
}

func zeroTex(tex objc.ID, bpp int) {
	if tex == 0 {
		return
	}
	w, h := int(msgWH(tex, sel_width)), int(msgWH(tex, sel_height))
	z := make([]byte, w*h*bpp)
	msgReplaceRegion(tex, sel_replaceRegion, mtlRegion2D(0, 0, uint(w), uint(h)), 0, unsafe.Pointer(&z[0]), uint(w*bpp))
}

func bufferDidModify(buf objc.ID, n uint) {
	if !mtlUnified && buf != 0 && n > 0 {
		msgDidModify(buf, sel_didModify, nsRange{Location: 0, Length: n})
	}
}

func growBuf(old objc.ID, bytes uint) objc.ID {
	if old != 0 && msgLength(old, sel_length) >= bytes {
		return old
	}
	if old != 0 {
		msgRelease(old, sel_release)
	}
	cap := bytes * 2
	if cap < 4096 {
		cap = 4096
	}
	return msgNewBuffer(mtlDevice, sel_newBuffer, cap, resourceOpts())
}

func ensureInstanceBuf(bytes uint) error {
	if bytes == 0 {
		return nil
	}
	i := mtlInstTurn % ring
	b := growBuf(mtlInstBuf[i], bytes)
	if b == 0 {
		return fmt.Errorf("instance buffer alloc failed")
	}
	mtlInstBuf[i] = b
	return nil
}

func WaitIdle() {
	if mtlQueue == 0 {
		return
	}
	cmd := msgCommandBuffer(mtlQueue, sel_commandBuffer)
	msgCommit(cmd, sel_commit)
	msgWait(cmd, sel_wait)
}

func Forget(dest unsafe.Pointer) {
	if dest == nil {
		return
	}
	surf := *(*objc.ID)(unsafe.Pointer(&dest))
	for i := range mtlTargets {
		if mtlTargets[i].surf == surf {
			if mtlTargets[i].tex != 0 {
				msgRelease(mtlTargets[i].tex, sel_release)
			}
			mtlTargets[i] = ioTarget{}
			return
		}
	}
}

func gpuResetAtlases() {
	WaitIdle()
	zeroTex(mtlGlyph, 1)
	zeroTex(mtlColor, 4)
}

func gpuImageEnsure(id uint32, w, h int) bool {
	if mtlDevice == 0 || w <= 0 || h <= 0 {
		setErr("image ensure: bad args")
		return false
	}
	if tex, ok := mtlImages[id]; ok && int(msgWH(tex, sel_width)) == w && int(msgWH(tex, sel_height)) == h {
		return true
	}
	tex := makeSharedTex(mtlPixelFormatRGBA8Unorm, w, h)
	if tex == 0 {
		setErr("image texture alloc failed")
		return false
	}
	if old, ok := mtlImages[id]; ok && old != 0 {
		msgRelease(old, sel_release)
	}
	mtlImages[id] = tex
	return true
}

func gpuImageForget(id uint32) {
	if tex, ok := mtlImages[id]; ok {
		msgRelease(tex, sel_release)
		delete(mtlImages, id)
	}
}

func texFor(surf unsafe.Pointer, w, h int) objc.ID {
	id := *(*objc.ID)(unsafe.Pointer(&surf))
	for i := range mtlTargets {
		if mtlTargets[i].surf == id {
			if mtlTargets[i].w == w && mtlTargets[i].h == h && mtlTargets[i].tex != 0 {
				return mtlTargets[i].tex
			}
			if mtlTargets[i].tex != 0 {
				msgRelease(mtlTargets[i].tex, sel_release)
			}
			mtlTargets[i] = ioTarget{}
			break
		}
	}
	d := msgTex2DDesc(objc.ID(class_MTLTexDesc), sel_tex2DDesc, mtlPixelFormatBGRA8Unorm, uint(w), uint(h), false)
	msgSetU(d, sel_setUsage, mtlTextureUsageRenderTarget|mtlTextureUsageShaderRead)
	tex := msgNewTexIOS(mtlDevice, sel_newTexIOS, d, id, 0)
	if tex == 0 {
		setErr("newTextureWithDescriptor:iosurface failed (%dx%d)", w, h)
		return 0
	}
	slot := -1
	for i := range mtlTargets {
		if mtlTargets[i].surf == 0 {
			slot = i
			break
		}
	}
	if slot < 0 {
		if mtlTargets[0].tex != 0 {
			msgRelease(mtlTargets[0].tex, sel_release)
		}
		slot = 0
	}
	mtlTargets[slot] = ioTarget{surf: id, tex: tex, w: w, h: h}
	return tex
}

func metalEncode(cmd, tex objc.ID, w, h int, quads []Quad, batches []Batch, uploads []gpuUpload, doClear bool, clearR, clearG, clearB, clearA float64) error {
	if mtlDevice == 0 || mtlQueue == 0 || mtlPipeline == 0 {
		return fmt.Errorf("not initialized")
	}
	if cmd == 0 || tex == 0 || w <= 0 || h <= 0 {
		return fmt.Errorf("bad target")
	}
	qbytes := uint(len(quads) * int(unsafe.Sizeof(Quad{})))
	if err := ensureInstanceBuf(qbytes); err != nil {
		return err
	}
	instI := mtlInstTurn % ring
	mtlInstTurn++
	inst := mtlInstBuf[instI]
	if len(quads) > 0 {
		dst := objc0(inst, sel_contents)
		copy(unsafe.Slice((*byte)(unsafe.Pointer(dst)), qbytes), unsafe.Slice((*byte)(unsafe.Pointer(&quads[0])), qbytes))
		bufferDidModify(inst, qbytes)
	}

	for _, u := range uploads {
		var dst objc.ID
		switch u.kind {
		case 1:
			dst = mtlGlyph
		case 2:
			dst = mtlColor
		default:
			dst = mtlImages[u.imageID]
		}
		if dst == 0 || len(u.pix) == 0 || u.w <= 0 || u.h <= 0 {
			continue
		}
		scratchRegion = mtlRegion2D(uint(u.x), uint(u.y), uint(u.w), uint(u.h))
		msgReplaceRegion(dst, sel_replaceRegion, scratchRegion, 0, unsafe.Pointer(&u.pix[0]), uint(u.stride))
	}

	pass := objc.ID(objc0(objc.ID(class_MTLPass), sel_renderPass))
	att := objc.ID(objc1(objc.ID(objc0(pass, sel_colorAttachments)), sel_at, 0))
	objc1(att, sel_setTexture, uintptr(tex))
	objc1(att, sel_setStore, mtlStoreActionStore)
	if doClear {
		objc1(att, sel_setLoad, mtlLoadActionClear)
		msgSetClear(att, sel_setClear, mtlClearColor{Red: clearR, Green: clearG, Blue: clearB, Alpha: clearA})
	} else {
		objc1(att, sel_setLoad, mtlLoadActionDontCare)
	}

	enc := objc.ID(objc1(cmd, sel_renderEnc, uintptr(pass)))
	objc1(enc, sel_setPipeline, uintptr(mtlPipeline))
	scratchViewport = mtlViewport{Width: float64(w), Height: float64(h), ZFar: 1}
	msgSetViewport(enc, sel_setViewport, scratchViewport)
	scratchUni = [2]float32{float32(w), float32(h)}
	objc3(enc, sel_setVertexBytes, uintptr(unsafe.Pointer(&scratchUni[0])), uintptr(unsafe.Sizeof(scratchUni)), 1)
	if len(quads) > 0 {
		objc3(enc, sel_setVertexBuf, uintptr(inst), 0, 0)
	}

	for i := range batches {
		b := &batches[i]
		if b.Count <= 0 || b.ClipW <= 0 || b.ClipH <= 0 {
			continue
		}
		x, y, cw, ch := int(b.ClipX), int(b.ClipY), int(b.ClipW), int(b.ClipH)
		if x < 0 {
			cw += x
			x = 0
		}
		if y < 0 {
			ch += y
			y = 0
		}
		if x+cw > w {
			cw = w - x
		}
		if y+ch > h {
			ch = h - y
		}
		if cw <= 0 || ch <= 0 {
			continue
		}
		scratchScissor = mtlScissorRect{X: uint(x), Y: uint(y), Width: uint(cw), Height: uint(ch)}
		msgSetScissor(enc, sel_setScissor, scratchScissor)
		t := mtlWhite
		scratchMode = 0
		switch b.TexKind {
		case texGlyph:
			t = mtlGlyph
			scratchMode = 1
		case texColorGlyph:
			t = mtlColor
			scratchMode = 2
		case texImage:
			if im := mtlImages[uint32(b.TexKey)]; im != 0 {
				t = im
			}
			scratchMode = 2
		}
		objc2(enc, sel_setFragTex, uintptr(t), 0)
		objc2(enc, sel_setFragSamp, uintptr(mtlSampler), 0)
		objc3(enc, sel_setFragBytes, uintptr(unsafe.Pointer(&scratchMode)), 4, 2)
		scratchClip = clipUni{N: uint32(b.ClipN), Rects: b.ClipRect, Radii: b.ClipRad}
		if scratchClip.N > 4 {
			scratchClip.N = 4
		}
		objc3(enc, sel_setFragBytes, uintptr(unsafe.Pointer(&scratchClip)), uintptr(unsafe.Sizeof(scratchClip)), 3)
		objc5(enc, sel_draw, mtlPrimitiveTypeTriangle, 0, 6, uintptr(b.Count), uintptr(b.First))
	}
	objc0(enc, sel_endEncoding)
	return nil
}

func gpuSubmit(dest unsafe.Pointer, w, h int, quads []Quad, batches []Batch, uploads []gpuUpload, wait bool) (encodeNs, waitNs int64, err error) {
	if mtlDevice == 0 || mtlQueue == 0 {
		return 0, 0, fmt.Errorf("not initialized")
	}
	if dest == nil || w <= 0 || h <= 0 {
		return 0, 0, fmt.Errorf("bad target")
	}
	t0 := time.Now()
	tex := texFor(dest, w, h)
	if tex == 0 {
		return 0, 0, fmt.Errorf("%s", mtlLastErr)
	}
	cmd := msgCommandBuffer(mtlQueue, sel_commandBuffer)
	if err := metalEncode(cmd, tex, w, h, quads, batches, uploads, true, 1, 1, 1, 1); err != nil {
		return 0, 0, err
	}
	if !wait {
		ensureCompleteBlock()
		inflightCmds.Store(cmd, dest)
		objc1(cmd, sel_addCompleted, uintptr(completeBlock))
	}
	msgCommit(cmd, sel_commit)
	encodeNs = time.Since(t0).Nanoseconds()
	if wait {
		t1 := time.Now()
		msgWait(cmd, sel_wait)
		waitNs = time.Since(t1).Nanoseconds()
		if msgStatus(cmd, sel_status) == mtlCommandBufferStatusError {
			e := msgError(cmd, sel_error)
			msg := "error"
			if e != 0 {
				msg = nsErr(e)
			}
			return encodeNs, waitNs, fmt.Errorf("command buffer: %s", msg)
		}
	}
	return encodeNs, waitNs, nil
}

func testSurface(w, h int) unsafe.Pointer {
	if err := bindMetal(); err != nil {
		return nil
	}
	bpr := (w*4 + 255) &^ 255
	keys := nsArray(kIOSurfaceWidth, kIOSurfaceHeight, kIOSurfaceBytesPerElement, kIOSurfaceBytesPerRow, kIOSurfacePixelFormat)
	vals := nsArray(nsNumber(w), nsNumber(h), nsNumber(4), nsNumber(bpr), nsNumberU32(pixelFormatBGRA))
	dict := msgDict(objc.ID(class_NSDictionary), sel_dictObjsKeys, vals, keys)
	s := ioSurfaceCreate(uintptr(dict))
	if s == 0 {
		return nil
	}
	return *(*unsafe.Pointer)(unsafe.Pointer(&s))
}

func testRelease(s unsafe.Pointer) {
	if s != nil {
		cfRelease(uintptr(s))
	}
}

func testPixel(s unsafe.Pointer, x, y int) [4]byte {
	if s == nil {
		return [4]byte{}
	}
	p := uintptr(s)
	w, h := int(ioSurfaceGetWidth(p)), int(ioSurfaceGetHeight(p))
	if x < 0 || y < 0 || x >= w || y >= h {
		return [4]byte{}
	}
	ioSurfaceLock(p, kIOSurfaceLockReadOnly, nil)
	base := ioSurfaceGetBaseAddress(p)
	bpr := ioSurfaceGetBytesPerRow(p)
	off := uintptr(y)*bpr + uintptr(x)*4
	out := *(*[4]byte)(unsafe.Pointer(base + off))
	ioSurfaceUnlock(p, kIOSurfaceLockReadOnly, nil)
	return out
}
