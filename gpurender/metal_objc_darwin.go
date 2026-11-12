//go:build darwin && !ios

package gpurender

import (
	"fmt"
	"structs"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/cstrings"
	"github.com/ebitengine/purego/objc"
)

// Typed objc_msgSend bindings for the encode hot path. Do not use
// objc.ID.Send / objc.Send[T] inside metalEncode — those pack ...any.

const (
	mtlPixelFormatBGRA8Unorm = 80
	mtlPixelFormatRGBA8Unorm = 70
	mtlPixelFormatR8Unorm    = 10

	mtlTextureUsageShaderRead   = 1
	mtlTextureUsageRenderTarget = 4

	mtlStorageModeShared  = 0
	mtlStorageModeManaged = 1

	mtlResourceStorageModeShared  = 0
	mtlResourceStorageModeManaged = 16

	mtlBlendFactorOne                 = 1
	mtlBlendFactorOneMinusSourceAlpha = 5

	mtlSamplerMinMagFilterLinear     = 1
	mtlSamplerAddressModeClampToEdge = 0

	mtlLoadActionDontCare = 0
	mtlLoadActionClear    = 2
	mtlStoreActionStore   = 1

	mtlPrimitiveTypeTriangle    = 3
	mtlCommandBufferStatusError = 5
	kIOSurfaceLockReadOnly      = 1
	pixelFormatBGRA             = 0x42475241
)

type mtlOrigin struct {
	_       structs.HostLayout
	X, Y, Z uint
}

type mtlSize struct {
	_                    structs.HostLayout
	Width, Height, Depth uint
}

type mtlRegion struct {
	_      structs.HostLayout
	Origin mtlOrigin
	Size   mtlSize
}

type mtlViewport struct {
	_                               structs.HostLayout
	OriginX, OriginY, Width, Height float64
	ZNear, ZFar                     float64
}

type mtlScissorRect struct {
	_                   structs.HostLayout
	X, Y, Width, Height uint
}

type mtlClearColor struct {
	_                       structs.HostLayout
	Red, Green, Blue, Alpha float64
}

type nsRange struct {
	_                structs.HostLayout
	Location, Length uint
}

func mtlRegion2D(x, y, w, h uint) mtlRegion {
	return mtlRegion{Origin: mtlOrigin{X: x, Y: y}, Size: mtlSize{Width: w, Height: h, Depth: 1}}
}

var (
	metalOnce sync.Once
	metalErr  error
	msgSend   uintptr

	mtlCopyAllDevices            func() objc.ID
	mtlCreateSystemDefaultDevice func() objc.ID
	ioSurfaceCreate              func(props uintptr) uintptr
	ioSurfaceLock                func(s uintptr, opts uint32, seed *uint32) int32
	ioSurfaceUnlock              func(s uintptr, opts uint32, seed *uint32) int32
	ioSurfaceGetBytesPerRow      func(s uintptr) uintptr
	ioSurfaceGetBaseAddress      func(s uintptr) uintptr
	ioSurfaceGetWidth            func(s uintptr) uintptr
	ioSurfaceGetHeight           func(s uintptr) uintptr
	ioSurfaceIsInUse             func(s uintptr) uint8
	cfRelease                    func(s uintptr)
	dispatchAsyncF               func(queue, context, work uintptr)
	dispatchMainQ                uintptr
	dispatchWorkPC               uintptr

	kIOSurfaceWidth           objc.ID
	kIOSurfaceHeight          objc.ID
	kIOSurfaceBytesPerElement objc.ID
	kIOSurfaceBytesPerRow     objc.ID
	kIOSurfacePixelFormat     objc.ID

	sel_retain, sel_release, sel_count, sel_objectAtIndex          objc.SEL
	sel_lowPower, sel_removable, sel_hasUnifiedMemory, sel_name    objc.SEL
	sel_newCommandQueue, sel_newLibrary, sel_newFunction           objc.SEL
	sel_newPipeline, sel_newSampler, sel_newTexture, sel_newTexIOS objc.SEL
	sel_newBuffer, sel_tex2DDesc, sel_setUsage, sel_setStorageMode objc.SEL
	sel_alloc, sel_init, sel_setVertexFn, sel_setFragmentFn        objc.SEL
	sel_colorAttachments, sel_at, sel_setPixelFormat               objc.SEL
	sel_setBlending, sel_setSrcRGB, sel_setDstRGB                  objc.SEL
	sel_setSrcA, sel_setDstA, sel_setMinFilter, sel_setMagFilter   objc.SEL
	sel_setSAddr, sel_setTAddr, sel_replaceRegion                  objc.SEL
	sel_commandBuffer, sel_commit, sel_wait, sel_status, sel_error objc.SEL
	sel_addCompleted, sel_contents, sel_length, sel_didModify      objc.SEL
	sel_blitEnc, sel_endEncoding, sel_copyFromBuffer               objc.SEL
	sel_renderPass, sel_setTexture, sel_setStore, sel_setLoad      objc.SEL
	sel_setClear, sel_renderEnc, sel_setPipeline, sel_setViewport  objc.SEL
	sel_setVertexBytes, sel_setVertexBuf, sel_setScissor           objc.SEL
	sel_setFragTex, sel_setFragSamp, sel_setFragBytes, sel_draw    objc.SEL
	sel_width, sel_height, sel_localizedDescription                objc.SEL
	sel_stringUTF8, sel_dictObjsKeys, sel_numberInt, sel_numberU32 objc.SEL
	sel_array, sel_addObject                                       objc.SEL

	msgRetain         func(objc.ID, objc.SEL) objc.ID
	msgRelease        func(objc.ID, objc.SEL)
	msgCount          func(objc.ID, objc.SEL) uint
	msgObjectAtIndex  func(objc.ID, objc.SEL, uint) objc.ID
	msgBool           func(objc.ID, objc.SEL) bool
	msgID             func(objc.ID, objc.SEL) objc.ID
	msgNewQueue       func(objc.ID, objc.SEL) objc.ID
	msgNewLibrary     func(objc.ID, objc.SEL, objc.ID, objc.ID, *objc.ID) objc.ID
	msgNewFunction    func(objc.ID, objc.SEL, objc.ID) objc.ID
	msgNewPipeline    func(objc.ID, objc.SEL, objc.ID, *objc.ID) objc.ID
	msgNewSampler     func(objc.ID, objc.SEL, objc.ID) objc.ID
	msgNewTexture     func(objc.ID, objc.SEL, objc.ID) objc.ID
	msgNewTexIOS      func(objc.ID, objc.SEL, objc.ID, objc.ID, uint) objc.ID
	msgNewBuffer      func(objc.ID, objc.SEL, uint, uint) objc.ID
	msgTex2DDesc      func(objc.ID, objc.SEL, uint, uint, uint, bool) objc.ID
	msgSetU           func(objc.ID, objc.SEL, uint)
	msgSetBool        func(objc.ID, objc.SEL, bool)
	msgSetID          func(objc.ID, objc.SEL, objc.ID)
	msgAlloc          func(objc.ID, objc.SEL) objc.ID
	msgInit           func(objc.ID, objc.SEL) objc.ID
	msgAt             func(objc.ID, objc.SEL, uint) objc.ID
	msgReplaceRegion  func(objc.ID, objc.SEL, mtlRegion, uint, unsafe.Pointer, uint)
	msgCommandBuffer  func(objc.ID, objc.SEL) objc.ID
	msgCommit         func(objc.ID, objc.SEL)
	msgWait           func(objc.ID, objc.SEL)
	msgStatus         func(objc.ID, objc.SEL) uint
	msgError          func(objc.ID, objc.SEL) objc.ID
	msgAddCompleted   func(objc.ID, objc.SEL, objc.Block)
	msgContents       func(objc.ID, objc.SEL) uintptr
	msgLength         func(objc.ID, objc.SEL) uint
	msgDidModify      func(objc.ID, objc.SEL, nsRange)
	msgBlitEnc        func(objc.ID, objc.SEL) objc.ID
	msgEndEncoding    func(objc.ID, objc.SEL)
	msgCopyFromBuffer func(objc.ID, objc.SEL, objc.ID, uint, uint, uint, mtlSize, objc.ID, uint, uint, mtlOrigin)
	msgRenderPass     func(objc.ID, objc.SEL) objc.ID
	msgSetClear       func(objc.ID, objc.SEL, mtlClearColor)
	msgRenderEnc      func(objc.ID, objc.SEL, objc.ID) objc.ID
	msgSetViewport    func(objc.ID, objc.SEL, mtlViewport)
	msgSetVertexBytes func(objc.ID, objc.SEL, unsafe.Pointer, uint, uint)
	msgSetVertexBuf   func(objc.ID, objc.SEL, objc.ID, uint, uint)
	msgSetScissor     func(objc.ID, objc.SEL, mtlScissorRect)
	msgSetFragTex     func(objc.ID, objc.SEL, objc.ID, uint)
	msgSetFragSamp    func(objc.ID, objc.SEL, objc.ID, uint)
	msgSetFragBytes   func(objc.ID, objc.SEL, unsafe.Pointer, uint, uint)
	msgDraw           func(objc.ID, objc.SEL, uint, uint, uint, uint, uint)
	msgWH             func(objc.ID, objc.SEL) uint
	msgStringUTF8     func(objc.ID, objc.SEL, string) objc.ID
	msgDict           func(objc.ID, objc.SEL, objc.ID, objc.ID) objc.ID
	msgNumberInt      func(objc.ID, objc.SEL, int) objc.ID
	msgNumberU32      func(objc.ID, objc.SEL, uint32) objc.ID
	msgArray          func(objc.ID, objc.SEL) objc.ID
	msgAddObject      func(objc.ID, objc.SEL, objc.ID)

	class_NSString, class_NSMutableArray, class_NSDictionary, class_NSNumber objc.Class
	class_MTLTexDesc, class_MTLPipeDesc, class_MTLSampDesc, class_MTLPass    objc.Class
)

func bindMsg(fn any) { purego.RegisterFunc(fn, msgSend) }

// Integer-only objc_msgSend calls avoid reflection while keeping native code
// on the system stack.
func objc0(id objc.ID, sel objc.SEL) uintptr {
	return objcCall(uintptr(id), uintptr(sel), 0, 0, 0, 0, 0)
}
func objc1(id objc.ID, sel objc.SEL, a uintptr) uintptr {
	return objcCall(uintptr(id), uintptr(sel), a, 0, 0, 0, 0)
}
func objc2(id objc.ID, sel objc.SEL, a, b uintptr) uintptr {
	return objcCall(uintptr(id), uintptr(sel), a, b, 0, 0, 0)
}
func objc3(id objc.ID, sel objc.SEL, a, b, c uintptr) uintptr {
	return objcCall(uintptr(id), uintptr(sel), a, b, c, 0, 0)
}
func objc4(id objc.ID, sel objc.SEL, a, b, c, d uintptr) uintptr {
	return objcCall(uintptr(id), uintptr(sel), a, b, c, d, 0)
}
func objc5(id objc.ID, sel objc.SEL, a, b, c, d, e uintptr) uintptr {
	return objcCall(uintptr(id), uintptr(sel), a, b, c, d, e)
}

func ptr[T any](p uintptr) *T { return *(**T)(unsafe.Pointer(&p)) }

func loadConst(lib uintptr, name string) (objc.ID, error) {
	addr, err := purego.Dlsym(lib, name)
	if err != nil || addr == 0 {
		return 0, fmt.Errorf("missing %s", name)
	}
	return *ptr[objc.ID](addr), nil
}

func bindMetal() error {
	metalOnce.Do(func() {
		for _, path := range []string{
			"/System/Library/Frameworks/Foundation.framework/Foundation",
			"/System/Library/Frameworks/Metal.framework/Metal",
			"/System/Library/Frameworks/IOSurface.framework/IOSurface",
		} {
			if _, err := purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_GLOBAL); err != nil {
				metalErr = err
				return
			}
		}
		objcLib, err := purego.Dlopen("/usr/lib/libobjc.A.dylib", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			metalErr = err
			return
		}
		msgSend, err = purego.Dlsym(objcLib, "objc_msgSend")
		if err != nil {
			metalErr = err
			return
		}
		objcMsgSend = msgSend
		mtl, err := purego.Dlopen("/System/Library/Frameworks/Metal.framework/Metal", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			metalErr = err
			return
		}
		ioLib, err := purego.Dlopen("/System/Library/Frameworks/IOSurface.framework/IOSurface", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			metalErr = err
			return
		}
		purego.RegisterLibFunc(&mtlCopyAllDevices, mtl, "MTLCopyAllDevices")
		purego.RegisterLibFunc(&mtlCreateSystemDefaultDevice, mtl, "MTLCreateSystemDefaultDevice")
		purego.RegisterLibFunc(&ioSurfaceCreate, ioLib, "IOSurfaceCreate")
		purego.RegisterLibFunc(&ioSurfaceLock, ioLib, "IOSurfaceLock")
		purego.RegisterLibFunc(&ioSurfaceUnlock, ioLib, "IOSurfaceUnlock")
		purego.RegisterLibFunc(&ioSurfaceGetBytesPerRow, ioLib, "IOSurfaceGetBytesPerRow")
		purego.RegisterLibFunc(&ioSurfaceGetBaseAddress, ioLib, "IOSurfaceGetBaseAddress")
		purego.RegisterLibFunc(&ioSurfaceGetWidth, ioLib, "IOSurfaceGetWidth")
		purego.RegisterLibFunc(&ioSurfaceGetHeight, ioLib, "IOSurfaceGetHeight")
		purego.RegisterLibFunc(&ioSurfaceIsInUse, ioLib, "IOSurfaceIsInUse")
		purego.RegisterLibFunc(&cfRelease, purego.RTLD_DEFAULT, "CFRelease")
		purego.RegisterLibFunc(&dispatchAsyncF, purego.RTLD_DEFAULT, "dispatch_async_f")
		dispatchMainQ, err = purego.Dlsym(purego.RTLD_DEFAULT, "_dispatch_main_q")
		if err != nil {
			metalErr = err
			return
		}
		dispatchWorkPC = purego.NewCallback(dispatchWork)

		must := func(id objc.ID, e error) objc.ID {
			if e != nil {
				metalErr = e
			}
			return id
		}
		kIOSurfaceWidth = must(loadConst(ioLib, "kIOSurfaceWidth"))
		kIOSurfaceHeight = must(loadConst(ioLib, "kIOSurfaceHeight"))
		kIOSurfaceBytesPerElement = must(loadConst(ioLib, "kIOSurfaceBytesPerElement"))
		kIOSurfaceBytesPerRow = must(loadConst(ioLib, "kIOSurfaceBytesPerRow"))
		kIOSurfacePixelFormat = must(loadConst(ioLib, "kIOSurfacePixelFormat"))
		if metalErr != nil {
			return
		}

		sel_retain = objc.RegisterName("retain")
		sel_release = objc.RegisterName("release")
		sel_count = objc.RegisterName("count")
		sel_objectAtIndex = objc.RegisterName("objectAtIndex:")
		sel_lowPower = objc.RegisterName("isLowPower")
		sel_removable = objc.RegisterName("isRemovable")
		sel_hasUnifiedMemory = objc.RegisterName("hasUnifiedMemory")
		sel_name = objc.RegisterName("name")
		sel_newCommandQueue = objc.RegisterName("newCommandQueue")
		sel_newLibrary = objc.RegisterName("newLibraryWithSource:options:error:")
		sel_newFunction = objc.RegisterName("newFunctionWithName:")
		sel_newPipeline = objc.RegisterName("newRenderPipelineStateWithDescriptor:error:")
		sel_newSampler = objc.RegisterName("newSamplerStateWithDescriptor:")
		sel_newTexture = objc.RegisterName("newTextureWithDescriptor:")
		sel_newTexIOS = objc.RegisterName("newTextureWithDescriptor:iosurface:plane:")
		sel_newBuffer = objc.RegisterName("newBufferWithLength:options:")
		sel_tex2DDesc = objc.RegisterName("texture2DDescriptorWithPixelFormat:width:height:mipmapped:")
		sel_setUsage = objc.RegisterName("setUsage:")
		sel_setStorageMode = objc.RegisterName("setStorageMode:")
		sel_alloc = objc.RegisterName("alloc")
		sel_init = objc.RegisterName("init")
		sel_setVertexFn = objc.RegisterName("setVertexFunction:")
		sel_setFragmentFn = objc.RegisterName("setFragmentFunction:")
		sel_colorAttachments = objc.RegisterName("colorAttachments")
		sel_at = objc.RegisterName("objectAtIndexedSubscript:")
		sel_setPixelFormat = objc.RegisterName("setPixelFormat:")
		sel_setBlending = objc.RegisterName("setBlendingEnabled:")
		sel_setSrcRGB = objc.RegisterName("setSourceRGBBlendFactor:")
		sel_setDstRGB = objc.RegisterName("setDestinationRGBBlendFactor:")
		sel_setSrcA = objc.RegisterName("setSourceAlphaBlendFactor:")
		sel_setDstA = objc.RegisterName("setDestinationAlphaBlendFactor:")
		sel_setMinFilter = objc.RegisterName("setMinFilter:")
		sel_setMagFilter = objc.RegisterName("setMagFilter:")
		sel_setSAddr = objc.RegisterName("setSAddressMode:")
		sel_setTAddr = objc.RegisterName("setTAddressMode:")
		sel_replaceRegion = objc.RegisterName("replaceRegion:mipmapLevel:withBytes:bytesPerRow:")
		sel_commandBuffer = objc.RegisterName("commandBuffer")
		sel_commit = objc.RegisterName("commit")
		sel_wait = objc.RegisterName("waitUntilCompleted")
		sel_status = objc.RegisterName("status")
		sel_error = objc.RegisterName("error")
		sel_addCompleted = objc.RegisterName("addCompletedHandler:")
		sel_contents = objc.RegisterName("contents")
		sel_length = objc.RegisterName("length")
		sel_didModify = objc.RegisterName("didModifyRange:")
		sel_blitEnc = objc.RegisterName("blitCommandEncoder")
		sel_endEncoding = objc.RegisterName("endEncoding")
		sel_copyFromBuffer = objc.RegisterName("copyFromBuffer:sourceOffset:sourceBytesPerRow:sourceBytesPerImage:sourceSize:toTexture:destinationSlice:destinationLevel:destinationOrigin:")
		sel_renderPass = objc.RegisterName("renderPassDescriptor")
		sel_setTexture = objc.RegisterName("setTexture:")
		sel_setStore = objc.RegisterName("setStoreAction:")
		sel_setLoad = objc.RegisterName("setLoadAction:")
		sel_setClear = objc.RegisterName("setClearColor:")
		sel_renderEnc = objc.RegisterName("renderCommandEncoderWithDescriptor:")
		sel_setPipeline = objc.RegisterName("setRenderPipelineState:")
		sel_setViewport = objc.RegisterName("setViewport:")
		sel_setVertexBytes = objc.RegisterName("setVertexBytes:length:atIndex:")
		sel_setVertexBuf = objc.RegisterName("setVertexBuffer:offset:atIndex:")
		sel_setScissor = objc.RegisterName("setScissorRect:")
		sel_setFragTex = objc.RegisterName("setFragmentTexture:atIndex:")
		sel_setFragSamp = objc.RegisterName("setFragmentSamplerState:atIndex:")
		sel_setFragBytes = objc.RegisterName("setFragmentBytes:length:atIndex:")
		sel_draw = objc.RegisterName("drawPrimitives:vertexStart:vertexCount:instanceCount:baseInstance:")
		sel_width = objc.RegisterName("width")
		sel_height = objc.RegisterName("height")
		sel_localizedDescription = objc.RegisterName("localizedDescription")
		sel_stringUTF8 = objc.RegisterName("stringWithUTF8String:")
		sel_dictObjsKeys = objc.RegisterName("dictionaryWithObjects:forKeys:")
		sel_numberInt = objc.RegisterName("numberWithInt:")
		sel_numberU32 = objc.RegisterName("numberWithUnsignedInt:")
		sel_array = objc.RegisterName("array")
		sel_addObject = objc.RegisterName("addObject:")

		bindMsg(&msgRetain)
		bindMsg(&msgRelease)
		bindMsg(&msgCount)
		bindMsg(&msgObjectAtIndex)
		bindMsg(&msgBool)
		bindMsg(&msgID)
		bindMsg(&msgNewQueue)
		bindMsg(&msgNewLibrary)
		bindMsg(&msgNewFunction)
		bindMsg(&msgNewPipeline)
		bindMsg(&msgNewSampler)
		bindMsg(&msgNewTexture)
		bindMsg(&msgNewTexIOS)
		bindMsg(&msgNewBuffer)
		bindMsg(&msgTex2DDesc)
		bindMsg(&msgSetU)
		bindMsg(&msgSetBool)
		bindMsg(&msgSetID)
		bindMsg(&msgAlloc)
		bindMsg(&msgInit)
		bindMsg(&msgAt)
		bindMsg(&msgReplaceRegion)
		bindMsg(&msgCommandBuffer)
		bindMsg(&msgCommit)
		bindMsg(&msgWait)
		bindMsg(&msgStatus)
		bindMsg(&msgError)
		bindMsg(&msgAddCompleted)
		bindMsg(&msgContents)
		bindMsg(&msgLength)
		bindMsg(&msgDidModify)
		bindMsg(&msgBlitEnc)
		bindMsg(&msgEndEncoding)
		bindMsg(&msgCopyFromBuffer)
		bindMsg(&msgRenderPass)
		bindMsg(&msgSetClear)
		bindMsg(&msgRenderEnc)
		bindMsg(&msgSetViewport)
		bindMsg(&msgSetVertexBytes)
		bindMsg(&msgSetVertexBuf)
		bindMsg(&msgSetScissor)
		bindMsg(&msgSetFragTex)
		bindMsg(&msgSetFragSamp)
		bindMsg(&msgSetFragBytes)
		bindMsg(&msgDraw)
		bindMsg(&msgWH)
		bindMsg(&msgStringUTF8)
		bindMsg(&msgDict)
		bindMsg(&msgNumberInt)
		bindMsg(&msgNumberU32)
		bindMsg(&msgArray)
		bindMsg(&msgAddObject)

		class_NSString = objc.GetClass("NSString")
		class_NSMutableArray = objc.GetClass("NSMutableArray")
		class_NSDictionary = objc.GetClass("NSDictionary")
		class_NSNumber = objc.GetClass("NSNumber")
		class_MTLTexDesc = objc.GetClass("MTLTextureDescriptor")
		class_MTLPipeDesc = objc.GetClass("MTLRenderPipelineDescriptor")
		class_MTLSampDesc = objc.GetClass("MTLSamplerDescriptor")
		class_MTLPass = objc.GetClass("MTLRenderPassDescriptor")
		if class_MTLPipeDesc == 0 || class_MTLTexDesc == 0 {
			metalErr = fmt.Errorf("Metal classes missing")
		}
	})
	return metalErr
}

func nsString(s string) objc.ID {
	return msgStringUTF8(objc.ID(class_NSString), sel_stringUTF8, s)
}

func nsArray(objs ...objc.ID) objc.ID {
	a := msgArray(objc.ID(class_NSMutableArray), sel_array)
	for _, o := range objs {
		msgAddObject(a, sel_addObject, o)
	}
	return a
}

func nsNumber(n int) objc.ID {
	return msgNumberInt(objc.ID(class_NSNumber), sel_numberInt, n)
}

func nsNumberU32(n uint32) objc.ID {
	return msgNumberU32(objc.ID(class_NSNumber), sel_numberU32, n)
}

func nsErr(id objc.ID) string {
	if id == 0 {
		return ""
	}
	return cstrings.NSStringToString(msgID(id, sel_localizedDescription))
}

func colorAtt(obj objc.ID) objc.ID {
	return msgAt(msgID(obj, sel_colorAttachments), sel_at, 0)
}

var dispatchSeq atomic.Uintptr
var dispatchJobs sync.Map

func dispatchWork(ctx uintptr) {
	if v, ok := dispatchJobs.LoadAndDelete(ctx); ok {
		v.(func())()
	}
}

func onMain(fn func()) {
	id := dispatchSeq.Add(1)
	dispatchJobs.Store(id, fn)
	dispatchAsyncF(dispatchMainQ, id, dispatchWorkPC)
}
