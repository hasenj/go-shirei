//go:build darwin && !ios

package main

import (
	"fmt"
	"os"
	"structs"
	"sync"
	"time"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/cstrings"
	"github.com/ebitengine/purego/objc"

	"go.hasen.dev/shirei"
)

const plasmaShader = `#include <metal_stdlib>
using namespace metal;

kernel void plasma(texture2d<float, access::write> out [[texture(0)]],
                   constant float &t [[buffer(0)]],
                   uint2 gid [[thread_position_in_grid]]) {
    float2 size = float2(out.get_width(), out.get_height());
    float2 uv = (float2(gid) + 0.5) / size;
    float2 p = (uv - 0.5) * float2(1.7, 1.0);
    float v = sin(p.x * 9.0 + t + sin(p.y * 7.0 - t * 0.8));
    v += sin(p.y * 8.0 - t * 0.6 + sin(p.x * 6.0 + t * 0.5));
    v = v * 0.25 + 0.5;
    float3 c = float3(
        0.5 + 0.5 * sin(v * 6.28318 + 0.0),
        0.5 + 0.5 * sin(v * 6.28318 + 2.0944),
        0.5 + 0.5 * sin(v * 6.28318 + 4.1888));
    out.write(float4(c, 1.0), gid);
}
`

type mtlSize struct {
	_                    structs.HostLayout
	Width, Height, Depth uint
}

type gpuSurface struct {
	ref uintptr
	tex objc.ID
}

type gpuProducer struct {
	w, h int
	key  string

	mu       sync.Mutex
	id       shirei.ImageId
	next     int
	surfaces []*gpuSurface

	start time.Time
}

func startGPUProducer(w, h int, key string) *gpuProducer {
	if err := bindMetal(); err != nil {
		fmt.Fprintf(os.Stderr, "gputexture: %v\n", err)
		os.Exit(1)
	}
	device := mtlCreateSystemDefaultDevice()
	if device == 0 {
		fmt.Fprintln(os.Stderr, "gputexture: no Metal device")
		os.Exit(1)
	}
	queue = objc.Send[objc.ID](device, sel("newCommandQueue"))
	if queue == 0 {
		fmt.Fprintln(os.Stderr, "gputexture: no command queue")
		os.Exit(1)
	}

	pool := objc.Send[objc.ID](objc.ID(objc.GetClass("NSAutoreleasePool")), sel("new"))
	defer objc.Send[objc.ID](pool, sel("drain"))

	src := objc.Send[objc.ID](objc.ID(objc.GetClass("NSString")), sel("stringWithUTF8String:"), plasmaShader)
	var err objc.ID
	lib := objc.Send[objc.ID](device, sel("newLibraryWithSource:options:error:"), src, uint(0), unsafe.Pointer(&err))
	if lib == 0 {
		fmt.Fprintf(os.Stderr, "gputexture: shader: %s\n", errorText(err))
		os.Exit(1)
	}
	fn := objc.Send[objc.ID](lib, sel("newFunctionWithName:"), nsstring("plasma"))
	if fn == 0 {
		fmt.Fprintln(os.Stderr, "gputexture: shader function missing")
		os.Exit(1)
	}
	pipeline = objc.Send[objc.ID](device, sel("newComputePipelineStateWithFunction:error:"), fn, unsafe.Pointer(&err))
	if pipeline == 0 {
		fmt.Fprintf(os.Stderr, "gputexture: pipeline: %s\n", errorText(err))
		os.Exit(1)
	}

	p := &gpuProducer{w: w, h: h, key: key, start: time.Now()}
	for i := 0; i < 3; i++ {
		s, err := newSurface(device, w, h)
		if err != nil {
			fmt.Fprintf(os.Stderr, "gputexture: %v\n", err)
			os.Exit(1)
		}
		p.surfaces = append(p.surfaces, s)
	}
	go p.loop()
	return p
}

func (p *gpuProducer) ImageId() shirei.ImageId {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.id
}

func (p *gpuProducer) loop() {
	ticker := time.NewTicker(16 * time.Millisecond)
	defer ticker.Stop()
	for range ticker.C {
		p.frame()
	}
}

func (p *gpuProducer) frame() {
	p.mu.Lock()
	start := p.next
	p.mu.Unlock()

	var surface *gpuSurface
	chosen := -1
	for i := 0; i < len(p.surfaces); i++ {
		idx := (start + i) % len(p.surfaces)
		if ioSurfaceIsInUse(p.surfaces[idx].ref) == 0 {
			surface = p.surfaces[idx]
			chosen = idx
			break
		}
	}
	if surface == nil {
		return
	}
	p.mu.Lock()
	p.next = (chosen + 1) % len(p.surfaces)
	p.mu.Unlock()

	t := float32(time.Since(p.start).Seconds())
	cmd := objc.Send[objc.ID](queue, sel("commandBuffer"))
	enc := objc.Send[objc.ID](cmd, sel("computeCommandEncoder"))
	objc.Send[objc.ID](enc, sel("setComputePipelineState:"), pipeline)
	objc.Send[objc.ID](enc, sel("setTexture:atIndex:"), surface.tex, uint(0))
	objc.Send[objc.ID](enc, sel("setBytes:length:atIndex:"), unsafe.Pointer(&t), uint(4), uint(0))
	objc.Send[objc.ID](enc, sel("dispatchThreads:threadsPerThreadgroup:"),
		mtlSize{Width: uint(p.w), Height: uint(p.h), Depth: 1},
		mtlSize{Width: 16, Height: 16, Depth: 1})
	objc.Send[objc.ID](enc, sel("endEncoding"))
	objc.Send[objc.ID](cmd, sel("commit"))
	objc.Send[objc.ID](cmd, sel("waitUntilCompleted"))
	if status := objc.Send[uint](cmd, sel("status")); status == 5 {
		fmt.Fprintf(os.Stderr, "gputexture: command buffer: %s\n", errorText(objc.Send[objc.ID](cmd, sel("error"))))
		return
	}

	cfRetain(surface.ref)
	frame := shirei.ExternalFrame{
		Kind:   shirei.ExternalIOSurfaceBGRA,
		Handle: surface.ref,
		Width:  p.w,
		Height: p.h,
		Release: func() {
			cfRelease(surface.ref)
		},
	}
	var id shirei.ImageId
	shirei.WithFrameLock(func() {
		id = shirei.UseExternalImage(p.key, frame)
	})
	if id == 0 {
		frame.Release()
		return
	}
	p.mu.Lock()
	p.id = id
	p.mu.Unlock()
	shirei.RequestNextFrame()
}

func newSurface(device objc.ID, w, h int) (*gpuSurface, error) {
	bpr := (w*4 + 255) &^ 255
	dict := objc.Send[objc.ID](objc.ID(objc.GetClass("NSMutableDictionary")), sel("dictionary"))
	setInt := func(key objc.ID, v int) {
		objc.Send[objc.ID](dict, sel("setObject:forKey:"),
			objc.Send[objc.ID](objc.ID(objc.GetClass("NSNumber")), sel("numberWithInt:"), v), key)
	}
	setInt(kIOSurfaceWidth, w)
	setInt(kIOSurfaceHeight, h)
	setInt(kIOSurfaceBytesPerElement, 4)
	setInt(kIOSurfaceBytesPerRow, bpr)
	objc.Send[objc.ID](dict, sel("setObject:forKey:"),
		objc.Send[objc.ID](objc.ID(objc.GetClass("NSNumber")), sel("numberWithUnsignedInt:"), uint32(0x42475241)),
		kIOSurfacePixelFormat)

	ref := ioSurfaceCreate(uintptr(dict))
	if ref == 0 {
		return nil, fmt.Errorf("IOSurfaceCreate failed (%dx%d)", w, h)
	}
	desc := objc.Send[objc.ID](objc.ID(objc.GetClass("MTLTextureDescriptor")),
		sel("texture2DDescriptorWithPixelFormat:width:height:mipmapped:"),
		uint(80), uint(w), uint(h), false)
	objc.Send[objc.ID](desc, sel("setUsage:"), uint(3)) // shader read | write
	tex := objc.Send[objc.ID](device, sel("newTextureWithDescriptor:iosurface:plane:"), desc, objc.ID(ref), uint(0))
	if tex == 0 {
		cfRelease(ref)
		return nil, fmt.Errorf("newTextureWithDescriptor:iosurface failed")
	}
	return &gpuSurface{ref: ref, tex: tex}, nil
}

func errorText(err objc.ID) string {
	if err == 0 {
		return "unknown error"
	}
	desc := objc.Send[objc.ID](err, sel("localizedDescription"))
	if desc == 0 {
		return "unknown error"
	}
	return cstrings.NSStringToString(desc)
}

func nsstring(s string) objc.ID {
	return objc.Send[objc.ID](objc.ID(objc.GetClass("NSString")), sel("stringWithUTF8String:"), s)
}

// -----------------------------------------------------------------------------
// Minimal Metal / IOSurface bindings

var (
	bindOnce sync.Once
	bindErr  error

	queue    objc.ID
	pipeline objc.ID

	mtlCreateSystemDefaultDevice func() objc.ID
	ioSurfaceCreate              func(props uintptr) uintptr
	ioSurfaceIsInUse             func(s uintptr) uint8
	cfRetain                     func(s uintptr) uintptr
	cfRelease                    func(s uintptr)

	kIOSurfaceWidth, kIOSurfaceHeight, kIOSurfaceBytesPerElement, kIOSurfaceBytesPerRow, kIOSurfacePixelFormat objc.ID

	selectors = map[string]objc.SEL{}
)

func sel(name string) objc.SEL {
	if s, ok := selectors[name]; ok {
		return s
	}
	s := objc.RegisterName(name)
	selectors[name] = s
	return s
}

func bindMetal() error {
	bindOnce.Do(func() {
		for _, path := range []string{
			"/System/Library/Frameworks/Foundation.framework/Foundation",
			"/System/Library/Frameworks/Metal.framework/Metal",
			"/System/Library/Frameworks/IOSurface.framework/IOSurface",
		} {
			if _, err := purego.Dlopen(path, purego.RTLD_NOW|purego.RTLD_GLOBAL); err != nil {
				bindErr = err
				return
			}
		}
		metal, err := purego.Dlopen("/System/Library/Frameworks/Metal.framework/Metal", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			bindErr = err
			return
		}
		ioLib, err := purego.Dlopen("/System/Library/Frameworks/IOSurface.framework/IOSurface", purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err != nil {
			bindErr = err
			return
		}
		purego.RegisterLibFunc(&mtlCreateSystemDefaultDevice, metal, "MTLCreateSystemDefaultDevice")
		purego.RegisterLibFunc(&ioSurfaceCreate, ioLib, "IOSurfaceCreate")
		purego.RegisterLibFunc(&ioSurfaceIsInUse, ioLib, "IOSurfaceIsInUse")
		purego.RegisterLibFunc(&cfRetain, purego.RTLD_DEFAULT, "CFRetain")
		purego.RegisterLibFunc(&cfRelease, purego.RTLD_DEFAULT, "CFRelease")
		kIOSurfaceWidth = mustConst(ioLib, "kIOSurfaceWidth")
		kIOSurfaceHeight = mustConst(ioLib, "kIOSurfaceHeight")
		kIOSurfaceBytesPerElement = mustConst(ioLib, "kIOSurfaceBytesPerElement")
		kIOSurfaceBytesPerRow = mustConst(ioLib, "kIOSurfaceBytesPerRow")
		kIOSurfacePixelFormat = mustConst(ioLib, "kIOSurfacePixelFormat")
		for _, name := range []string{
			"newCommandQueue",
			"newLibraryWithSource:options:error:",
			"newFunctionWithName:",
			"newComputePipelineStateWithFunction:error:",
			"commandBuffer",
			"computeCommandEncoder",
			"setComputePipelineState:",
			"setTexture:atIndex:",
			"setBytes:length:atIndex:",
			"dispatchThreads:threadsPerThreadgroup:",
			"endEncoding",
			"commit",
			"waitUntilCompleted",
			"status",
			"error",
			"texture2DDescriptorWithPixelFormat:width:height:mipmapped:",
			"setUsage:",
			"newTextureWithDescriptor:iosurface:plane:",
			"dictionary",
			"setObject:forKey:",
			"numberWithInt:",
			"numberWithUnsignedInt:",
			"stringWithUTF8String:",
			"localizedDescription",
			"new",
			"drain",
		} {
			sel(name)
		}
	})
	return bindErr
}

func ptr[T any](p uintptr) *T { return *(**T)(unsafe.Pointer(&p)) }

func mustConst(lib uintptr, name string) objc.ID {
	addr, err := purego.Dlsym(lib, name)
	if err != nil || addr == 0 {
		if bindErr == nil {
			bindErr = fmt.Errorf("missing %s", name)
		}
		return 0
	}
	return *ptr[objc.ID](addr)
}
