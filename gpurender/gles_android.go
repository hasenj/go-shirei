//go:build android

package gpurender

import (
	"fmt"
	"unsafe"

	"go.hasen.dev/shirei"
)

const glesFragSrcAndroid = `#version 300 es
#extension GL_OES_EGL_image_external_essl3 : require
precision highp float;
precision highp int;
in vec4 vColor;
in vec4 vColor2;
in vec2 vUV;
in vec2 vCorner;
uniform uint mode;
uniform uint clipN;
uniform vec4 clipRects[4];
uniform vec4 clipRads[4];
uniform vec2 viewport;
uniform vec2 destOrig;
uniform int destFlip;
uniform sampler2D tex;
uniform sampler2D texUV;
uniform samplerExternalOES texOES;
uniform mat4 stMat;
out vec4 fragColor;

float sdRoundBox(vec2 p, vec2 b, vec4 r) {
    r.xy = (p.x > 0.0) ? r.xy : r.zw;
    r.x  = (p.y > 0.0) ? r.x  : r.y;
    vec2 q = abs(p) - b + r.x;
    return min(max(q.x, q.y), 0.0) + length(max(q, 0.0)) - r.x;
}

float clipCov(vec2 pos) {
    float cov = 1.0;
    for (uint i = 0u; i < clipN; i++) {
        vec4 rc = clipRects[i];
        vec2 halfv = rc.zw * 0.5;
        vec2 p = pos - (rc.xy + halfv);
        p.y = -p.y;
        vec4 rad = clipRads[i];
        vec4 r = vec4(rad.y, rad.z, rad.x, rad.w);
        cov *= clamp(0.5 - sdRoundBox(p, halfv, r), 0.0, 1.0);
    }
    return cov;
}

void main() {
    vec2 local = gl_FragCoord.xy - destOrig;
    float sceneY = destFlip == 0 ? local.y : (viewport.y - local.y);
    vec2 dev = vec2(local.x, sceneY);
    vec4 outc;
    if (mode == 0u) {
        vec4 c = mix(vColor, vColor2, vCorner.y);
        outc = vec4(c.rgb * c.a, c.a);
    } else if (mode == 1u) {
        float cov = texture(tex, vUV).r;
        vec4 c = mix(vColor, vColor2, vCorner.y);
        float a = c.a * cov;
        outc = vec4(c.rgb * a, a);
    } else if (mode == 3u || mode == 4u) {
        float y = texture(tex, vUV).r;
        vec2 uv = texture(texUV, vUV).rg - vec2(0.5);
        vec3 rgb;
        if (mode == 3u) {
            float y1 = (y - 0.0627451) * 1.1643836;
            rgb = vec3(
                y1 + 1.7927411 * uv.y,
                y1 - 0.2132486 * uv.x - 0.5329093 * uv.y,
                y1 + 2.1124018 * uv.x
            );
        } else {
            rgb = vec3(
                y + 1.5748 * uv.y,
                y - 0.187324 * uv.x - 0.468124 * uv.y,
                y + 1.8556 * uv.x
            );
        }
        rgb = clamp(rgb, 0.0, 1.0);
        outc = vec4(rgb * vColor.a, vColor.a);
    } else if (mode == 5u) {
        vec4 stUV = stMat * vec4(vUV.x, 1.0 - vUV.y, 0.0, 1.0);
        vec4 t = texture(texOES, stUV.xy);
        outc = vec4(t.rgb * vColor.a, vColor.a);
    } else {
        vec4 t = texture(tex, vUV);
        outc = t * vColor.a;
    }
    fragColor = outc * clipCov(dev);
}
`

var (
	eglSurf   uintptr
	nativeWin uintptr
	winW      int
	winH      int
	conL      int
	conT      int
	conW      int
	conH      int
	origX     int // GL window x of content
	origY     int // GL window y of content (bottom-left origin)
)

func gpuInit() (retErr error) {
	if eglDpy != 0 {
		return nil
	}
	if err := loadGLESLibs(); err != nil {
		return err
	}
	var eglInited bool
	defer func() {
		if retErr == nil {
			return
		}
		if eglDpy != 0 {
			if eglCtx != 0 {
				eglDestroyContext(eglDpy, eglCtx)
				eglCtx = 0
			}
			if eglInited {
				eglTerminate(eglDpy)
			}
			eglDpy = 0
		}
	}()
	eglDpy = eglGetDisplay(nil)
	if eglDpy == 0 {
		return fmt.Errorf("eglGetDisplay failed (%#x)", uint32(eglGetError()))
	}
	var maj, min int32
	if eglInitialize(eglDpy, &maj, &min) == eglFalse {
		return fmt.Errorf("eglInitialize failed (%#x)", uint32(eglGetError()))
	}
	eglInited = true
	if eglBindAPI(eglOpenGLESAPI) == eglFalse {
		return fmt.Errorf("eglBindAPI GLES failed")
	}
	cfgAttribs := []int32{
		eglSurfaceType, eglWindowBit,
		eglRenderableType, eglOpenGLES3Bit,
		eglRedSize, 8,
		eglGreenSize, 8,
		eglBlueSize, 8,
		eglAlphaSize, 8,
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
	glesFragOverride = glesFragSrcAndroid
	glesAndroidOESImporter = glesImportAndroidOES
	gpuExternalImage = glesExternalImage
	gpuExternalKindSupported = func(kind shirei.ExternalKind) bool {
		if kind == shirei.ExternalAndroidSurfaceTexture {
			return !glesReady.Load() || glesHasOES.Load()
		}
		return kind == shirei.ExternalNV12
	}
	return nil
}

func glesImportAndroidOES(id uint32, st *shirei.ExternalSurfaceTextureFrame, w, h int) bool {
	if !glesReady.Load() || !glesHasOES.Load() || eglSurf == 0 || st == nil || st.Update == nil {
		return false
	}
	if eglMakeCurrent(eglDpy, eglSurf, eglSurf, eglCtx) == eglFalse {
		return false
	}
	cur, ok := texImages[id]
	if !ok || !cur.oes || cur.tex == 0 {
		var tex uint32
		glGenTextures(1, &tex)
		if tex == 0 {
			return false
		}
		glBindTexture(glTextureExternalOES, tex)
		glTexParameteri(glTextureExternalOES, glTextureMinFilter, glLinear)
		glTexParameteri(glTextureExternalOES, glTextureMagFilter, glLinear)
		glTexParameteri(glTextureExternalOES, glTextureWrapS, glClampToEdge)
		glTexParameteri(glTextureExternalOES, glTextureWrapT, glClampToEdge)
		if ok {
			glesFreeImgTex(cur)
		}
		cur = glImgTex{tex: tex, w: w, h: h, oes: true, external: true}
	}
	mat, updated := st.Update(cur.tex)
	if !updated {
		texImages[id] = cur
		return false
	}
	cur.w = w
	cur.h = h
	cur.stMat = mat
	texImages[id] = cur
	return true
}

// BindWindow creates (or recreates) the EGL window surface for nativeWin
// (ANativeWindow*). winW/winH are the full surface size in device pixels.
func BindWindow(win unsafe.Pointer, w, h int) error {
	if err := gpuInit(); err != nil {
		return err
	}
	nw := uintptr(win)
	if nw == 0 || w <= 0 || h <= 0 {
		return fmt.Errorf("bad native window")
	}
	if eglSurf != 0 && nativeWin == nw && winW == w && winH == h {
		return nil
	}
	if eglSurf != 0 {
		eglMakeCurrent(eglDpy, 0, 0, 0)
		eglDestroySurface(eglDpy, eglSurf)
		eglSurf = 0
	}
	eglSurf = eglCreateWindowSurface(eglDpy, eglCfg, nw, nil)
	if eglSurf == 0 {
		return fmt.Errorf("eglCreateWindowSurface failed (%#x)", uint32(eglGetError()))
	}
	if eglMakeCurrent(eglDpy, eglSurf, eglSurf, eglCtx) == eglFalse {
		eglDestroySurface(eglDpy, eglSurf)
		eglSurf = 0
		return fmt.Errorf("eglMakeCurrent failed (%#x)", uint32(eglGetError()))
	}
	nativeWin, winW, winH = nw, w, h
	if err := glesFinishInit("android"); err != nil {
		return err
	}
	return nil
}

// UnbindWindow drops the EGL surface (TERM_WINDOW). The context stays.
func UnbindWindow() {
	if eglDpy == 0 {
		return
	}
	eglMakeCurrent(eglDpy, 0, 0, 0)
	if eglSurf != 0 {
		eglDestroySurface(eglDpy, eglSurf)
		eglSurf = 0
	}
	nativeWin = 0
}

// WindowDest is the GPU present target: full window size plus the content
// rect in Android top-left device pixels (same as the CPU lock path).
type WindowDest struct {
	WinW, WinH int
	Left, Top  int
	W, H       int
}

func (d *WindowDest) Handle() unsafe.Pointer { return unsafe.Pointer(d) }

func gpuBindDest(dest unsafe.Pointer, w, h int) error {
	if dest == nil || eglSurf == 0 {
		return fmt.Errorf("no egl surface")
	}
	d := (*WindowDest)(dest)
	if d.W != w || d.H != h {
		return fmt.Errorf("target size mismatch")
	}
	if eglMakeCurrent(eglDpy, eglSurf, eglSurf, eglCtx) == eglFalse {
		return fmt.Errorf("eglMakeCurrent failed (%#x)", uint32(eglGetError()))
	}
	conL, conT, conW, conH = d.Left, d.Top, d.W, d.H
	winW, winH = d.WinW, d.WinH
	origX = conL
	origY = winH - conT - conH

	glBindFramebuffer(glFramebuffer, 0)
	glDisable(glScissorTest)
	glViewport(0, 0, int32(winW), int32(winH))
	glClearColor(1, 1, 1, 1)
	glClear(glColorBufferBit)
	glViewport(int32(origX), int32(origY), int32(conW), int32(conH))
	glEnable(glScissorTest)
	glUseProgram(glProg)
	glUniform2f(uNdcY, -2, 1)
	glUniform2f(uDestOrig, float32(origX), float32(origY))
	glUniform1i(uDestFlip, 1)
	return nil
}

func gpuScissor(x, y, cw, ch, sceneW, sceneH int) {
	sy := origY + (sceneH - y - ch)
	glScissor(int32(origX+x), int32(sy), int32(cw), int32(ch))
}

func gpuPresent(dest unsafe.Pointer, wait bool) int64 {
	if eglSwapBuffers(eglDpy, eglSurf) == eglFalse {
		return 0
	}
	if wait {
		glFinish()
	}
	return 0
}
