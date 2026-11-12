//go:build linux || android

package gpurender

import (
	"fmt"
	"unsafe"

	"github.com/ebitengine/purego"
)

// EGL / GBM / GLES constants we use. Values are from the Khronos/Mesa headers.
const (
	eglDontCare              = -1
	eglFalse                 = 0
	eglTrue                  = 1
	eglSuccess               = 0x3000
	eglNone                  = 0x3038
	eglPbufferBit            = 0x0001
	eglWindowBit             = 0x0004
	eglOpenGLESAPI           = 0x30A0
	eglSurfaceType           = 0x3033
	eglRenderableType        = 0x3040
	eglOpenGLES2Bit          = 0x0004
	eglOpenGLES3Bit          = 0x00000040
	eglRedSize               = 0x3024
	eglGreenSize             = 0x3023
	eglBlueSize              = 0x3022
	eglAlphaSize             = 0x3021
	eglContextMajorVersion   = 0x3098
	eglContextMinorVersion   = 0x30FB
	eglContextClientVersion  = 0x3098
	eglWidth                 = 0x3057
	eglHeight                = 0x3056
	eglExtensions            = 0x3055
	eglVendor                = 0x3053
	eglPlatformGBMKHR        = 0x31D7
	eglLinuxDMABufEXT        = 0x3270
	eglLinuxDRMFourccEXT     = 0x3271
	eglDMABufPlane0FDEXT     = 0x3272
	eglDMABufPlane0OffsetEXT = 0x3273
	eglDMABufPlane0PitchEXT  = 0x3274
	eglDMABufPlane0ModLoEXT  = 0x3443
	eglDMABufPlane0ModHiEXT  = 0x3444

	glColorBufferBit       = 0x00004000
	glBlend                = 0x0BE2
	glOne                  = 1
	glOneMinusSrcAlpha     = 0x0303
	glScissorTest          = 0x0C11
	glTexture2D            = 0x0DE1
	glFramebuffer          = 0x8D40
	glColorAttachment0     = 0x8CE0
	glRGBA                 = 0x1908
	glRGBA8                = 0x8058
	glUnsignedByte         = 0x1401
	glR8                   = 0x8229
	glRed                  = 0x1903
	glLinear               = 0x2601
	glClampToEdge          = 0x812F
	glTextureMinFilter     = 0x2801
	glTextureMagFilter     = 0x2800
	glTextureWrapS         = 0x2802
	glTextureWrapT         = 0x2803
	glUnpackAlignment      = 0x0CF5
	glUnpackRowLength      = 0x0CF2
	glTriangles            = 0x0004
	glVertexShader         = 0x8B31
	glFragmentShader       = 0x8B30
	glCompileStatus        = 0x8B81
	glLinkStatus           = 0x8B82
	glInfoLogLength        = 0x8B84
	glArrayBuffer          = 0x8892
	glDynamicDraw          = 0x88E8
	glFloat                = 0x1406
	glFalse                = 0
	glTrue                 = 1
	glFramebufferComplete  = 0x8CD5
	glNoError              = 0
	glTexture0             = 0x84C0
	glRenderer             = 0x1F01
	glVendor               = 0x1F00
	glExts                 = 0x1F03
	glFramebufferFlipYMESA = 0x8BBB

	gbmBoUseScanout   = 1 << 0
	gbmBoUseRendering = 1 << 2
	gbmBoUseLinear    = 1 << 4

	drmFormatARGB8888 = 0x34325241 // 'AR24'
	drmFormatXRGB8888 = 0x34325258 // 'XR24'
	drmModLinear      = 0
	drmModInvalid     = uint64(0x00ffffffffffffff) // implicit modifier
)

var (
	eglGetPlatformDisplay  func(platform int32, native unsafe.Pointer, attribs *int64) uintptr
	eglGetDisplay          func(native unsafe.Pointer) uintptr
	eglInitialize          func(dpy uintptr, major, minor *int32) uint32
	eglBindAPI             func(api uint32) uint32
	eglChooseConfig        func(dpy uintptr, attribs *int32, configs *uintptr, n int32, num *int32) uint32
	eglCreateContext       func(dpy, config, share uintptr, attribs *int32) uintptr
	eglMakeCurrent         func(dpy, draw, read, ctx uintptr) uint32
	eglDestroyContext      func(dpy, ctx uintptr) uint32
	eglTerminate           func(dpy uintptr) uint32
	eglGetError            func() int32
	eglQueryString         func(dpy uintptr, name int32) uintptr
	eglCreateImageKHR      func(dpy, ctx uintptr, target uint32, buffer uintptr, attribs *int32) uintptr
	eglDestroyImageKHR     func(dpy, image uintptr) uint32
	eglCreateWindowSurface func(dpy, config, win uintptr, attribs *int32) uintptr
	eglDestroySurface      func(dpy, surf uintptr) uint32
	eglSwapBuffers         func(dpy, surf uintptr) uint32
	eglQuerySurface        func(dpy, surf uintptr, attr int32, value *int32) uint32
	eglGetProcAddress      func(name string) uintptr

	gbmCreateDevice           func(fd int32) uintptr
	gbmDeviceDestroy          func(dev uintptr)
	gbmBoCreate               func(dev uintptr, w, h, format, flags uint32) uintptr
	gbmBoDestroy              func(bo uintptr)
	gbmBoGetFd                func(bo uintptr) int32
	gbmBoGetStride            func(bo uintptr) uint32
	gbmBoGetOffset            func(bo uintptr, plane int32) uint32
	gbmBoGetModifier          func(bo uintptr) uint64
	gbmBoCreateWithModifiers  func(dev uintptr, w, h, format uint32, mods *uint64, count uint32) uintptr
	gbmBoCreateWithModifiers2 func(dev uintptr, w, h, format uint32, mods *uint64, count, flags uint32) uintptr
	gbmBoGetPlaneCount        func(bo uintptr) int32
	gbmBoGetFdForPlane        func(bo uintptr, plane int32) int32
	gbmBoGetStrideForPlane    func(bo uintptr, plane int32) uint32
	haveBoOffset              bool
	haveBoModifier            bool
	haveCreateWithModifiers   bool
	haveCreateWithModifiers2  bool
	havePlaneCount            bool
	haveFdForPlane            bool
	haveStrideForPlane        bool

	glGetString                  func(name uint32) uintptr
	glGetError                   func() uint32
	glGetShaderiv                func(shader, pname uint32, out *int32)
	glGetProgramiv               func(prog, pname uint32, out *int32)
	glGetShaderInfoLog           func(shader uint32, max int32, n *int32, buf *byte)
	glGetProgramInfoLog          func(prog uint32, max int32, n *int32, buf *byte)
	glCreateShader               func(kind uint32) uint32
	glShaderSource               func(shader uint32, count int32, src *uintptr, length *int32)
	glCompileShader              func(shader uint32)
	glCreateProgram              func() uint32
	glAttachShader               func(prog, shader uint32)
	glLinkProgram                func(prog uint32)
	glUseProgram                 func(prog uint32)
	glDeleteShader               func(shader uint32)
	glGetUniformLocation         func(prog uint32, name string) int32
	glUniform1i                  func(loc, v int32)
	glUniform1ui                 func(loc int32, v uint32)
	glUniform2f                  func(loc int32, x, y float32)
	glUniform4fv                 func(loc, n int32, v *float32)
	glGenTextures                func(n int32, ids *uint32)
	glBindTexture                func(target, id uint32)
	glTexParameteri              func(target, pname uint32, param int32)
	glTexImage2D                 func(target uint32, level, internal int32, w, h, border int32, format, typ uint32, pix unsafe.Pointer)
	glTexSubImage2D              func(target uint32, level, x, y, w, h int32, format, typ uint32, pix unsafe.Pointer)
	glDeleteTextures             func(n int32, ids *uint32)
	glGenFramebuffers            func(n int32, ids *uint32)
	glBindFramebuffer            func(target, id uint32)
	glFramebufferTexture2D       func(target, attachment, textarget, tex uint32, level int32)
	glFramebufferParameteri      func(target uint32, pname uint32, param int32)
	glCheckFramebufferStatus     func(target uint32) uint32
	glDeleteFramebuffers         func(n int32, ids *uint32)
	glViewport                   func(x, y, w, h int32)
	glScissor                    func(x, y, w, h int32)
	glEnable                     func(cap uint32)
	glDisable                    func(cap uint32)
	glBlendFunc                  func(sfactor, dfactor uint32)
	glClearColor                 func(r, g, b, a float32)
	glClear                      func(mask uint32)
	glPixelStorei                func(pname uint32, param int32)
	glGenBuffers                 func(n int32, ids *uint32)
	glBindBuffer                 func(target, id uint32)
	glBufferData                 func(target uint32, size int, data unsafe.Pointer, usage uint32)
	glDeleteBuffers              func(n int32, ids *uint32)
	glGenVertexArrays            func(n int32, ids *uint32)
	glBindVertexArray            func(id uint32)
	glDeleteVertexArrays         func(n int32, ids *uint32)
	glVertexAttribPointer        func(index uint32, size int32, typ uint32, normalized uint8, stride int32, pointer unsafe.Pointer)
	glVertexAttribDivisor        func(index, divisor uint32)
	glEnableVertexAttribArray    func(index uint32)
	glDrawArraysInstanced        func(mode uint32, first, count, instanceCount int32)
	glFinish                     func()
	glFlush                      func()
	glActiveTexture              func(unit uint32)
	glEGLImageTargetTexture2DOES func(target uint32, image uintptr)
	haveFramebufferParameteri    bool
)

func cstring(p uintptr) string {
	if p == 0 {
		return ""
	}
	n := 0
	for *(*byte)(unsafe.Pointer(p + uintptr(n))) != 0 {
		n++
	}
	return unsafe.String((*byte)(unsafe.Pointer(p)), n)
}

func dlopenFirst(names ...string) (uintptr, error) {
	var last error
	for _, n := range names {
		h, err := purego.Dlopen(n, purego.RTLD_NOW|purego.RTLD_GLOBAL)
		if err == nil && h != 0 {
			return h, nil
		}
		last = err
	}
	if last == nil {
		last = fmt.Errorf("dlopen %v", names)
	}
	return 0, last
}

func bindSym(lib uintptr, name string, fptr interface{}) error {
	addr, err := purego.Dlsym(lib, name)
	if (err != nil || addr == 0) && eglGetProcAddress != nil {
		addr = eglGetProcAddress(name)
		err = nil
	}
	if addr == 0 {
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		return fmt.Errorf("missing %s", name)
	}
	purego.RegisterFunc(fptr, addr)
	return nil
}

func bindOpt(lib uintptr, name string, fptr interface{}) bool {
	addr, err := purego.Dlsym(lib, name)
	if (err != nil || addr == 0) && eglGetProcAddress != nil {
		addr = eglGetProcAddress(name)
	}
	if addr == 0 {
		return false
	}
	purego.RegisterFunc(fptr, addr)
	return true
}

func bindCommonGL(libEGL, libGLES uintptr) error {
	if err := bindSym(libEGL, "eglGetProcAddress", &eglGetProcAddress); err != nil {
		return err
	}
	need := []struct {
		lib  uintptr
		name string
		fn   interface{}
	}{
		{libEGL, "eglGetDisplay", &eglGetDisplay},
		{libEGL, "eglInitialize", &eglInitialize},
		{libEGL, "eglBindAPI", &eglBindAPI},
		{libEGL, "eglChooseConfig", &eglChooseConfig},
		{libEGL, "eglCreateContext", &eglCreateContext},
		{libEGL, "eglMakeCurrent", &eglMakeCurrent},
		{libEGL, "eglDestroyContext", &eglDestroyContext},
		{libEGL, "eglTerminate", &eglTerminate},
		{libEGL, "eglGetError", &eglGetError},
		{libEGL, "eglQueryString", &eglQueryString},
		{libGLES, "glGetString", &glGetString},
		{libGLES, "glGetError", &glGetError},
		{libGLES, "glGetShaderiv", &glGetShaderiv},
		{libGLES, "glGetProgramiv", &glGetProgramiv},
		{libGLES, "glGetShaderInfoLog", &glGetShaderInfoLog},
		{libGLES, "glGetProgramInfoLog", &glGetProgramInfoLog},
		{libGLES, "glCreateShader", &glCreateShader},
		{libGLES, "glShaderSource", &glShaderSource},
		{libGLES, "glCompileShader", &glCompileShader},
		{libGLES, "glCreateProgram", &glCreateProgram},
		{libGLES, "glAttachShader", &glAttachShader},
		{libGLES, "glLinkProgram", &glLinkProgram},
		{libGLES, "glUseProgram", &glUseProgram},
		{libGLES, "glDeleteShader", &glDeleteShader},
		{libGLES, "glGetUniformLocation", &glGetUniformLocation},
		{libGLES, "glUniform1i", &glUniform1i},
		{libGLES, "glUniform1ui", &glUniform1ui},
		{libGLES, "glUniform2f", &glUniform2f},
		{libGLES, "glUniform4fv", &glUniform4fv},
		{libGLES, "glGenTextures", &glGenTextures},
		{libGLES, "glBindTexture", &glBindTexture},
		{libGLES, "glTexParameteri", &glTexParameteri},
		{libGLES, "glTexImage2D", &glTexImage2D},
		{libGLES, "glTexSubImage2D", &glTexSubImage2D},
		{libGLES, "glDeleteTextures", &glDeleteTextures},
		{libGLES, "glGenFramebuffers", &glGenFramebuffers},
		{libGLES, "glBindFramebuffer", &glBindFramebuffer},
		{libGLES, "glFramebufferTexture2D", &glFramebufferTexture2D},
		{libGLES, "glCheckFramebufferStatus", &glCheckFramebufferStatus},
		{libGLES, "glDeleteFramebuffers", &glDeleteFramebuffers},
		{libGLES, "glViewport", &glViewport},
		{libGLES, "glScissor", &glScissor},
		{libGLES, "glEnable", &glEnable},
		{libGLES, "glDisable", &glDisable},
		{libGLES, "glBlendFunc", &glBlendFunc},
		{libGLES, "glClearColor", &glClearColor},
		{libGLES, "glClear", &glClear},
		{libGLES, "glPixelStorei", &glPixelStorei},
		{libGLES, "glGenBuffers", &glGenBuffers},
		{libGLES, "glBindBuffer", &glBindBuffer},
		{libGLES, "glBufferData", &glBufferData},
		{libGLES, "glDeleteBuffers", &glDeleteBuffers},
		{libGLES, "glGenVertexArrays", &glGenVertexArrays},
		{libGLES, "glBindVertexArray", &glBindVertexArray},
		{libGLES, "glDeleteVertexArrays", &glDeleteVertexArrays},
		{libGLES, "glVertexAttribPointer", &glVertexAttribPointer},
		{libGLES, "glVertexAttribDivisor", &glVertexAttribDivisor},
		{libGLES, "glEnableVertexAttribArray", &glEnableVertexAttribArray},
		{libGLES, "glDrawArraysInstanced", &glDrawArraysInstanced},
		{libGLES, "glFinish", &glFinish},
		{libGLES, "glFlush", &glFlush},
		{libGLES, "glActiveTexture", &glActiveTexture},
	}
	for _, s := range need {
		if err := bindSym(s.lib, s.name, s.fn); err != nil {
			return err
		}
	}
	return nil
}
