//go:build linux && !android

package gpurender

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"
	"unsafe"

	"go.hasen.dev/shirei"
	"golang.org/x/sys/unix"
)

var (
	drmFD       int
	gbmDev      uintptr
	allowedMods []uint64
)

// SetModifiers is the compositor's advertised dmabuf modifiers for the
// window format, including DRM_FORMAT_MOD_INVALID (implicit).
func SetModifiers(mods []uint64) {
	allowedMods = append([]uint64(nil), mods...)
}

func gpuInit() error {
	if glesReady {
		return nil
	}
	if err := loadGLESLibs(); err != nil {
		return err
	}

	path := strings.TrimSpace(os.Getenv("SHIREI_DRM_RENDER"))
	var err error
	if path != "" {
		drmFD, err = unix.Open(path, unix.O_RDWR|unix.O_CLOEXEC, 0)
		if err != nil {
			return fmt.Errorf("open %s: %w", path, err)
		}
	} else {
		err = fmt.Errorf("no render node")
		for i := 128; i < 144; i++ {
			p := fmt.Sprintf("/dev/dri/renderD%d", i)
			drmFD, err = unix.Open(p, unix.O_RDWR|unix.O_CLOEXEC, 0)
			if err == nil {
				path = p
				break
			}
		}
		if err != nil {
			return fmt.Errorf("open /dev/dri/renderD*: %w", err)
		}
	}

	gbmDev = gbmCreateDevice(int32(drmFD))
	if gbmDev == 0 {
		unix.Close(drmFD)
		drmFD = 0
		return fmt.Errorf("gbm_create_device failed")
	}

	if eglGetPlatformDisplay != nil {
		eglDpy = eglGetPlatformDisplay(eglPlatformGBMKHR, unsafe.Pointer(gbmDev), nil)
	}
	if eglDpy == 0 {
		eglDpy = eglGetDisplay(unsafe.Pointer(gbmDev))
	}
	if eglDpy == 0 {
		return fmt.Errorf("eglGetDisplay failed (%#x)", uint32(eglGetError()))
	}
	var maj, min int32
	if eglInitialize(eglDpy, &maj, &min) == eglFalse {
		return fmt.Errorf("eglInitialize failed (%#x)", uint32(eglGetError()))
	}
	if eglBindAPI(eglOpenGLESAPI) == eglFalse {
		return fmt.Errorf("eglBindAPI GLES failed")
	}

	exts := cstring(eglQueryString(eglDpy, eglExtensions))
	if !strings.Contains(exts, "EGL_EXT_image_dma_buf_import") {
		return fmt.Errorf("EGL_EXT_image_dma_buf_import missing")
	}

	cfgAttribs := []int32{
		eglSurfaceType, eglWindowBit,
		eglRenderableType, eglOpenGLES3Bit,
		eglRedSize, 8,
		eglGreenSize, 8,
		eglBlueSize, 8,
		eglNone,
	}
	var ncfg int32
	if eglChooseConfig(eglDpy, &cfgAttribs[0], &eglCfg, 1, &ncfg) == eglFalse || ncfg < 1 {
		cfgAttribs[3] = eglOpenGLES2Bit
		if eglChooseConfig(eglDpy, &cfgAttribs[0], &eglCfg, 1, &ncfg) == eglFalse || ncfg < 1 {
			return fmt.Errorf("eglChooseConfig failed (%#x)", uint32(eglGetError()))
		}
	}

	ctxAttribs := []int32{
		eglContextMajorVersion, 3,
		eglContextMinorVersion, 0,
		eglNone,
	}
	eglCtx = eglCreateContext(eglDpy, eglCfg, 0, &ctxAttribs[0])
	if eglCtx == 0 {
		ctxAttribs = []int32{eglContextClientVersion, 3, eglNone}
		eglCtx = eglCreateContext(eglDpy, eglCfg, 0, &ctxAttribs[0])
	}
	if eglCtx == 0 {
		return fmt.Errorf("eglCreateContext ES3 failed (%#x)", uint32(eglGetError()))
	}
	if eglMakeCurrent(eglDpy, 0, 0, eglCtx) == eglFalse {
		return fmt.Errorf("eglMakeCurrent surfaceless failed (%#x); need EGL_KHR_surfaceless_context", uint32(eglGetError()))
	}
	return glesFinishInit(path)
}

func gpuBindDest(dest unsafe.Pointer, w, h int) error {
	if dest == nil {
		return fmt.Errorf("bad target")
	}
	t := (*Target)(dest)
	if t.fbo == 0 || t.w != w || t.h != h {
		return fmt.Errorf("target size mismatch")
	}
	eglMakeCurrent(eglDpy, 0, 0, eglCtx)
	glBindFramebuffer(glFramebuffer, t.fbo)
	glViewport(0, 0, int32(w), int32(h))
	glDisable(glScissorTest)
	if shirei.GetHost().TransparentBackground {
		glClearColor(0, 0, 0, 0)
	} else {
		glClearColor(1, 1, 1, 1)
	}
	glClear(glColorBufferBit)
	glEnable(glScissorTest)
	glUseProgram(glProg)
	glUniform2f(uNdcY, 2, -1)
	glUniform2f(uDestOrig, 0, 0)
	glUniform1i(uDestFlip, 0)
	return nil
}

func gpuScissor(x, y, cw, ch, sceneW, sceneH int) {
	glScissor(int32(x), int32(y), int32(cw), int32(ch))
}

func gpuPresent(dest unsafe.Pointer, wait bool) int64 {
	glFlush()
	if !wait {
		return 0
	}
	t1 := time.Now()
	glFinish()
	return time.Since(t1).Nanoseconds()
}

// Target is a GBM/dmabuf render destination. Plane fds are owned by the
// Target; do not close them. Pass Handle() to Render.
type Target struct {
	w, h     int
	nplane   int
	pfd      [4]int
	poff     [4]int
	pstride  [4]int
	modifier uint64
	format   uint32
	bo       uintptr
	image    uintptr
	tex      uint32
	fbo      uint32
}

func (t *Target) Handle() unsafe.Pointer { return unsafe.Pointer(t) }
func (t *Target) Modifier() uint64       { return t.modifier }
func (t *Target) Format() uint32         { return t.format }
func (t *Target) Size() (int, int)       { return t.w, t.h }
func (t *Target) Planes() int            { return t.nplane }
func (t *Target) Plane(i int) (fd, offset, stride int) {
	return t.pfd[i], t.poff[i], t.pstride[i]
}

func AllocTarget(w, h int) (*Target, error) {
	if err := gpuInit(); err != nil {
		return nil, err
	}
	if w <= 0 || h <= 0 {
		return nil, fmt.Errorf("bad target size %dx%d", w, h)
	}
	eglMakeCurrent(eglDpy, 0, 0, eglCtx)

	format := uint32(drmFormatXRGB8888)
	if shirei.GetHost().TransparentBackground {
		format = drmFormatARGB8888
	}
	bo := glesCreateBO(w, h, format)
	if bo == 0 {
		return nil, fmt.Errorf("gbm_bo_create %dx%d failed", w, h)
	}
	nplane := 1
	if havePlaneCount && haveFdForPlane {
		if n := int(gbmBoGetPlaneCount(bo)); n >= 1 && n <= 4 {
			nplane = n
		}
	}
	t := &Target{w: w, h: h, nplane: nplane, format: format, bo: bo}
	for i := 0; i < nplane; i++ {
		t.pfd[i] = -1
	}
	for i := 0; i < nplane; i++ {
		var fd int32
		if nplane > 1 {
			fd = gbmBoGetFdForPlane(bo, int32(i))
		} else {
			fd = gbmBoGetFd(bo)
		}
		if fd < 0 {
			glesClosePlanes(t)
			gbmBoDestroy(bo)
			return nil, fmt.Errorf("gbm_bo_get_fd plane %d failed", i)
		}
		t.pfd[i] = int(fd)
		if haveBoOffset {
			t.poff[i] = int(gbmBoGetOffset(bo, int32(i)))
		}
		if haveStrideForPlane {
			t.pstride[i] = int(gbmBoGetStrideForPlane(bo, int32(i)))
		} else if i == 0 {
			t.pstride[i] = int(gbmBoGetStride(bo))
		}
	}
	mod := uint64(drmModLinear)
	if haveBoModifier {
		mod = gbmBoGetModifier(bo)
	}
	send, ok := sendModifier(mod)
	if !ok {
		glesClosePlanes(t)
		gbmBoDestroy(bo)
		return nil, fmt.Errorf("gbm modifier %#x not advertised", mod)
	}
	t.modifier = send

	attribs := eglImageAttribs(w, h, t)
	image := eglCreateImageKHR(eglDpy, 0, eglLinuxDMABufEXT, 0, &attribs[0])
	if image == 0 && nplane == 1 {
		attribs = []int32{
			eglWidth, int32(w),
			eglHeight, int32(h),
			eglLinuxDRMFourccEXT, int32(t.format),
			eglDMABufPlane0FDEXT, int32(t.pfd[0]),
			eglDMABufPlane0OffsetEXT, int32(t.poff[0]),
			eglDMABufPlane0PitchEXT, int32(t.pstride[0]),
			eglNone,
		}
		image = eglCreateImageKHR(eglDpy, 0, eglLinuxDMABufEXT, 0, &attribs[0])
	}
	if image == 0 {
		glesClosePlanes(t)
		gbmBoDestroy(bo)
		return nil, fmt.Errorf("eglCreateImage dmabuf failed (%#x)", uint32(eglGetError()))
	}

	var tex uint32
	glGenTextures(1, &tex)
	glBindTexture(glTexture2D, tex)
	glTexParameteri(glTexture2D, glTextureMinFilter, glLinear)
	glTexParameteri(glTexture2D, glTextureMagFilter, glLinear)
	glTexParameteri(glTexture2D, glTextureWrapS, glClampToEdge)
	glTexParameteri(glTexture2D, glTextureWrapT, glClampToEdge)
	glEGLImageTargetTexture2DOES(glTexture2D, image)

	var fbo uint32
	glGenFramebuffers(1, &fbo)
	glBindFramebuffer(glFramebuffer, fbo)
	glFramebufferTexture2D(glFramebuffer, glColorAttachment0, glTexture2D, tex, 0)
	if glCheckFramebufferStatus(glFramebuffer) != glFramebufferComplete {
		glDeleteFramebuffers(1, &fbo)
		glDeleteTextures(1, &tex)
		eglDestroyImageKHR(eglDpy, image)
		glesClosePlanes(t)
		gbmBoDestroy(bo)
		return nil, fmt.Errorf("framebuffer incomplete")
	}

	t.image, t.tex, t.fbo = image, tex, fbo
	return t, nil
}

func glesCreateBO(w, h int, format uint32) uintptr {
	explicit, implicit := splitMods(allowedMods)

	// LINEAR first when advertised: window present must send a pair the
	// compositor listed (linux-dmabuf v4+ posts invalid_format otherwise).
	// A full modifier list can make GBM return a CCS/DCC variant that was
	// not advertised.
	if hasMod(explicit, drmModLinear) {
		if bo := keepSendable(gbmCreateExplicit(w, h, format, []uint64{drmModLinear})); bo != 0 {
			return bo
		}
		if bo := keepSendable(gbmBoCreate(gbmDev, uint32(w), uint32(h), format, uint32(gbmBoUseRendering|gbmBoUseLinear))); bo != 0 {
			return bo
		}
	}
	if bo := keepSendable(gbmCreateExplicit(w, h, format, explicit)); bo != 0 {
		return bo
	}
	for _, m := range explicit {
		if m == drmModLinear {
			continue
		}
		if bo := keepSendable(gbmCreateExplicit(w, h, format, []uint64{m})); bo != 0 {
			return bo
		}
	}
	if implicit {
		if bo := keepSendable(gbmBoCreate(gbmDev, uint32(w), uint32(h), format, uint32(gbmBoUseRendering))); bo != 0 {
			return bo
		}
	}
	return 0
}

func gbmCreateExplicit(w, h int, format uint32, mods []uint64) uintptr {
	if len(mods) == 0 || !(haveCreateWithModifiers2 || haveCreateWithModifiers) {
		return 0
	}
	var bo uintptr
	if haveCreateWithModifiers2 {
		bo = gbmBoCreateWithModifiers2(gbmDev, uint32(w), uint32(h), format, &mods[0], uint32(len(mods)), uint32(gbmBoUseRendering))
	}
	if bo == 0 && haveCreateWithModifiers {
		bo = gbmBoCreateWithModifiers(gbmDev, uint32(w), uint32(h), format, &mods[0], uint32(len(mods)))
	}
	runtime.KeepAlive(mods)
	return bo
}

func keepSendable(bo uintptr) uintptr {
	if bo == 0 {
		return 0
	}
	if haveBoModifier {
		if _, ok := sendModifier(gbmBoGetModifier(bo)); !ok {
			gbmBoDestroy(bo)
			return 0
		}
	}
	return bo
}

// splitMods separates explicit modifiers from DRM_FORMAT_MOD_INVALID.
// INVALID is not passed to gbm_bo_create_with_modifiers; it means allocate
// with gbm_bo_create (implicit). No advertised list → implicit.
func splitMods(mods []uint64) (explicit []uint64, implicit bool) {
	if len(mods) == 0 {
		return nil, true
	}
	for _, m := range mods {
		if m == drmModInvalid {
			implicit = true
			continue
		}
		explicit = append(explicit, m)
	}
	return explicit, implicit
}

func hasMod(mods []uint64, want uint64) bool {
	for _, m := range mods {
		if m == want {
			return true
		}
	}
	return false
}

// sendModifier is the modifier to put on params.add. It must be a pair the
// compositor advertised (v4+ treats anything else as a fatal invalid_format).
// Implicit bos often report LINEAR; if LINEAR was not advertised but INVALID
// was, send INVALID.
func sendModifier(got uint64) (uint64, bool) {
	if len(allowedMods) == 0 {
		return got, true
	}
	for _, m := range allowedMods {
		if m == got {
			return got, true
		}
	}
	if got == drmModLinear || got == drmModInvalid {
		for _, m := range allowedMods {
			if m == drmModInvalid {
				return drmModInvalid, true
			}
		}
	}
	return 0, false
}

func eglImageAttribs(w, h int, t *Target) []int32 {
	a := make([]int32, 0, 8+t.nplane*10)
	a = append(a,
		eglWidth, int32(w),
		eglHeight, int32(h),
		eglLinuxDRMFourccEXT, int32(t.format),
	)
	for i := 0; i < t.nplane; i++ {
		base := int32(eglDMABufPlane0FDEXT + i*3)
		a = append(a,
			base, int32(t.pfd[i]),
			base+1, int32(t.poff[i]),
			base+2, int32(t.pstride[i]),
		)
		if t.modifier != drmModInvalid {
			modLo := int32(eglDMABufPlane0ModLoEXT + i*2)
			a = append(a,
				modLo, int32(uint32(t.modifier)),
				modLo+1, int32(uint32(t.modifier>>32)),
			)
		}
	}
	return append(a, eglNone)
}

func glesClosePlanes(t *Target) {
	seen := make(map[int]struct{})
	for i := 0; i < t.nplane; i++ {
		fd := t.pfd[i]
		if fd < 0 {
			continue
		}
		if _, ok := seen[fd]; ok {
			t.pfd[i] = -1
			continue
		}
		seen[fd] = struct{}{}
		unix.Close(fd)
		t.pfd[i] = -1
	}
}

func FreeTarget(t *Target) {
	if t == nil {
		return
	}
	if glesReady {
		eglMakeCurrent(eglDpy, 0, 0, eglCtx)
		if t.fbo != 0 {
			glDeleteFramebuffers(1, &t.fbo)
		}
		if t.tex != 0 {
			glDeleteTextures(1, &t.tex)
		}
		if t.image != 0 && eglDestroyImageKHR != nil {
			eglDestroyImageKHR(eglDpy, t.image)
		}
	}
	glesClosePlanes(t)
	if t.bo != 0 {
		gbmBoDestroy(t.bo)
		t.bo = 0
	}
}
