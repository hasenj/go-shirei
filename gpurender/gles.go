//go:build linux || android

package gpurender

import (
	"fmt"
	"runtime"
	"strings"
	"time"
	"unsafe"

	"go.hasen.dev/shirei"
)

type glImgTex struct {
	tex      uint32
	texUV    uint32
	eglImg0  uintptr
	eglImg1  uintptr
	w, h     int
	nv12     bool
	fullRng  bool
	oes      bool
	stMat    [16]float32
	external bool
}

var (
	glesDevName string
	glesReady   bool
	glesHasOES  bool

	eglDpy uintptr
	eglCtx uintptr
	eglCfg uintptr

	glProg     uint32
	glVAO      uint32
	glVBO      uint32
	glTexWhite uint32
	glTexGlyph uint32
	glTexColor uint32
	texImages  map[uint32]glImgTex

	uViewport int32
	uNdcY     int32
	uDestOrig int32
	uDestFlip int32
	uMode     int32
	uClipN    int32
	uClipRect int32
	uClipRad  int32
	uTex      int32
	uTexUV    int32
	uTexOES   int32
	uSTMat    int32

	// Optional platform fragment shader override (Android GL_OES_EGL_image_external_essl3).
	glesFragOverride string

	// Set by gles_linux.go to import Linux DRM prime dmabufs via EGLImage.
	glesDMABufImporter func(id uint32, db *shirei.ExternalDMABufFrame, w, h int) bool

	// Set by gles_android.go to update Android SurfaceTexture GL_TEXTURE_EXTERNAL_OES.
	glesAndroidOESImporter func(id uint32, st *shirei.ExternalSurfaceTextureFrame, w, h int) bool
)

func DeviceName() string { return glesDevName }

func WaitIdle() {
	if glesReady {
		glFinish()
	}
}

func gpuResetAtlases() {
	if !glesReady {
		return
	}
	glesZeroTex(glTexGlyph, glRed, 1, glyphAtlasW, glyphAtlasH)
	glesZeroTex(glTexColor, glRGBA, 4, colorAtlasW, colorAtlasH)
}

func glesFreeImgTex(t glImgTex) {
	if t.tex != 0 {
		glDeleteTextures(1, &t.tex)
	}
	if t.texUV != 0 {
		glDeleteTextures(1, &t.texUV)
	}
	if t.eglImg0 != 0 && eglDpy != 0 && eglDestroyImageKHR != nil {
		eglDestroyImageKHR(eglDpy, t.eglImg0)
	}
	if t.eglImg1 != 0 && eglDpy != 0 && eglDestroyImageKHR != nil {
		eglDestroyImageKHR(eglDpy, t.eglImg1)
	}
}

func gpuImageEnsure(id uint32, w, h int) bool {
	if !glesReady || w <= 0 || h <= 0 {
		return false
	}
	if cur, ok := texImages[id]; ok && cur.tex != 0 && !cur.nv12 && !cur.external && cur.w == w && cur.h == h {
		return true
	}
	tex := glesMakeTex(glRGBA8, glRGBA, w, h)
	if tex == 0 {
		return false
	}
	if old, ok := texImages[id]; ok {
		glesFreeImgTex(old)
	}
	texImages[id] = glImgTex{tex: tex, w: w, h: h}
	return true
}

func gpuImageForget(id uint32) {
	if !glesReady {
		return
	}
	t, ok := texImages[id]
	if !ok {
		return
	}
	glesFreeImgTex(t)
	delete(texImages, id)
}

func glesExternalImage(id uint32, frame *shirei.ExternalFrame, w, h int) bool {
	if !glesReady || frame == nil || w <= 0 || h <= 0 {
		return false
	}
	switch frame.Kind {
	case shirei.ExternalNV12:
		nv, ok := frame.Data.(*shirei.ExternalNV12Frame)
		if !ok || nv == nil {
			return false
		}
		return glesImportNV12(id, nv, w, h)
	case shirei.ExternalDMABuf:
		if glesDMABufImporter == nil {
			return false
		}
		db, ok := frame.Data.(*shirei.ExternalDMABufFrame)
		if !ok || db == nil {
			return false
		}
		return glesDMABufImporter(id, db, w, h)
	case shirei.ExternalAndroidSurfaceTexture:
		if glesAndroidOESImporter == nil || !glesHasOES {
			return false
		}
		st, ok := frame.Data.(*shirei.ExternalSurfaceTextureFrame)
		if !ok || st == nil || st.Update == nil {
			return false
		}
		return glesAndroidOESImporter(id, st, w, h)
	default:
		return false
	}
}

func glesImportNV12(id uint32, nv *shirei.ExternalNV12Frame, w, h int) bool {
	uvW, uvH := w/2, h/2
	if uvW <= 0 || uvH <= 0 || nv.YStride < w || nv.UVStride < uvW*2 {
		return false
	}
	if len(nv.Y) < h*nv.YStride || len(nv.UV) < uvH*nv.UVStride {
		return false
	}
	cur, ok := texImages[id]
	if !ok || !cur.nv12 || cur.external || cur.tex == 0 || cur.texUV == 0 || cur.w != w || cur.h != h {
		texY := glesMakeTex(glR8, glRed, w, h)
		if texY == 0 {
			return false
		}
		texUV := glesMakeTex(glRG8, glRG, uvW, uvH)
		if texUV == 0 {
			glDeleteTextures(1, &texY)
			return false
		}
		if ok {
			glesFreeImgTex(cur)
		}
		cur = glImgTex{tex: texY, texUV: texUV, w: w, h: h, nv12: true}
	}
	cur.fullRng = nv.FullRange
	texImages[id] = cur
	if !queueUpload(4, 0, 0, w, h, nv.YStride, id, nv.Y) {
		return false
	}
	if !queueUpload(5, 0, 0, uvW, uvH, nv.UVStride, id, nv.UV) {
		return false
	}
	gpu.upload += h*nv.YStride + uvH*nv.UVStride
	return true
}

func Forget(dest unsafe.Pointer) {}

// glesFinishInit builds the program, VAO, and atlases. The EGL context must
// already be current. Idempotent after glesReady.
func glesFinishInit(devName string) error {
	if glesReady {
		return nil
	}
	if err := glesBuildProgram(); err != nil {
		return err
	}
	glGenVertexArrays(1, &glVAO)
	glBindVertexArray(glVAO)
	glGenBuffers(1, &glVBO)
	glBindBuffer(glArrayBuffer, glVBO)
	for i := uint32(0); i < 4; i++ {
		glEnableVertexAttribArray(i)
		glVertexAttribDivisor(i, 1)
	}
	glTexWhite = glesMakeTex(glRGBA8, glRGBA, 1, 1)
	white := [4]byte{255, 255, 255, 255}
	glBindTexture(glTexture2D, glTexWhite)
	glTexSubImage2D(glTexture2D, 0, 0, 0, 1, 1, glRGBA, glUnsignedByte, unsafe.Pointer(&white[0]))
	glTexGlyph = glesMakeTex(glR8, glRed, glyphAtlasW, glyphAtlasH)
	glTexColor = glesMakeTex(glRGBA8, glRGBA, colorAtlasW, colorAtlasH)
	glesZeroTex(glTexGlyph, glRed, 1, glyphAtlasW, glyphAtlasH)
	glesZeroTex(glTexColor, glRGBA, 4, colorAtlasW, colorAtlasH)
	texImages = make(map[uint32]glImgTex)
	glEnable(glBlend)
	glBlendFunc(glOne, glOneMinusSrcAlpha)
	glEnable(glScissorTest)
	if r := cstring(glGetString(glRenderer)); r != "" {
		v := cstring(glGetString(glVendor))
		glesDevName = strings.TrimSpace(v + " " + r)
	}
	if glesDevName == "" {
		glesDevName = devName
	}
	glesReady = true
	return nil
}

func gpuSubmit(dest unsafe.Pointer, w, h int, quads []Quad, batches []Batch, uploads []gpuUpload, wait bool) (encodeNs, waitNs int64, err error) {
	if !glesReady {
		return 0, 0, fmt.Errorf("gles not initialized")
	}
	if w <= 0 || h <= 0 {
		return 0, 0, fmt.Errorf("bad target")
	}
	t0 := time.Now()
	if err := gpuBindDest(dest, w, h); err != nil {
		return 0, 0, err
	}

	glesApplyUploads(uploads)

	glBindVertexArray(glVAO)
	glBindBuffer(glArrayBuffer, glVBO)
	qbytes := len(quads) * int(unsafe.Sizeof(Quad{}))
	if len(quads) > 0 {
		glBufferData(glArrayBuffer, qbytes, unsafe.Pointer(&quads[0]), glDynamicDraw)
	}

	glUseProgram(glProg)
	glUniform2f(uViewport, float32(w), float32(h))
	glUniform1i(uTex, 0)
	if uTexUV >= 0 {
		glUniform1i(uTexUV, 1)
	}
	if uTexOES >= 0 {
		glUniform1i(uTexOES, 2)
	}
	glActiveTexture(glTexture0)

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
		gpuScissor(x, y, cw, ch, w, h)

		tex := glTexWhite
		mode := uint32(0)
		switch b.TexKind {
		case texGlyph:
			tex = glTexGlyph
			mode = 1
		case texColorGlyph:
			tex = glTexColor
			mode = 2
		case texImage:
			mode = 2
			if im, ok := texImages[uint32(b.TexKey)]; ok && im.tex != 0 {
				if im.oes && glesHasOES {
					glActiveTexture(glTexture2)
					glBindTexture(glTextureExternalOES, im.tex)
					glActiveTexture(glTexture0)
					if uSTMat >= 0 {
						glUniformMatrix4fv(uSTMat, 1, 0, &im.stMat[0])
					}
					mode = 5
				} else {
					tex = im.tex
					if im.nv12 && im.texUV != 0 {
						glActiveTexture(glTexture1)
						glBindTexture(glTexture2D, im.texUV)
						glActiveTexture(glTexture0)
						if im.fullRng {
							mode = 4
						} else {
							mode = 3
						}
					}
				}
			}
		}
		glBindTexture(glTexture2D, tex)
		glUniform1ui(uMode, mode)
		n := uint32(b.ClipN)
		if n > 4 {
			n = 4
		}
		glUniform1ui(uClipN, n)
		glUniform4fv(uClipRect, 4, &b.ClipRect[0][0])
		glUniform4fv(uClipRad, 4, &b.ClipRad[0][0])

		off := uintptr(b.First) * unsafe.Sizeof(Quad{})
		stride := int32(unsafe.Sizeof(Quad{}))
		glVertexAttribPointer(0, 4, glFloat, 0, stride, unsafe.Pointer(off))
		glVertexAttribPointer(1, 4, glFloat, 0, stride, unsafe.Pointer(off+16))
		glVertexAttribPointer(2, 4, glFloat, 0, stride, unsafe.Pointer(off+32))
		glVertexAttribPointer(3, 4, glFloat, 0, stride, unsafe.Pointer(off+48))
		glDrawArraysInstanced(glTriangles, 0, 6, b.Count)
	}

	encodeNs = time.Since(t0).Nanoseconds()
	waitNs = gpuPresent(dest, wait)
	if !wait && completeFn != nil {
		completeFn(dest)
	}
	runtime.KeepAlive(quads)
	runtime.KeepAlive(batches)
	runtime.KeepAlive(uploads)
	return encodeNs, waitNs, nil
}

func glesApplyUploads(uploads []gpuUpload) {
	if len(uploads) == 0 {
		return
	}
	glPixelStorei(glUnpackAlignment, 1)
	for i := range uploads {
		u := &uploads[i]
		if len(u.pix) == 0 || u.w <= 0 || u.h <= 0 {
			continue
		}
		var tex uint32
		var format uint32
		var bpp int
		switch u.kind {
		case 1:
			tex = glTexGlyph
			format = glRed
			bpp = 1
		case 2:
			tex = glTexColor
			format = glRGBA
			bpp = 4
		case 3:
			tex = texImages[u.imageID].tex
			format = glRGBA
			bpp = 4
		case 4:
			tex = texImages[u.imageID].tex
			format = glRed
			bpp = 1
		case 5:
			tex = texImages[u.imageID].texUV
			format = glRG
			bpp = 2
		}
		if tex == 0 {
			continue
		}
		rowPix := u.stride / bpp
		if rowPix != u.w {
			glPixelStorei(glUnpackRowLength, int32(rowPix))
		} else {
			glPixelStorei(glUnpackRowLength, 0)
		}
		glBindTexture(glTexture2D, tex)
		glTexSubImage2D(glTexture2D, 0, int32(u.x), int32(u.y), int32(u.w), int32(u.h),
			format, glUnsignedByte, unsafe.Pointer(&u.pix[0]))
	}
	glPixelStorei(glUnpackRowLength, 0)
}

func glesMakeTex(internal uint32, format uint32, w, h int) uint32 {
	var id uint32
	glGenTextures(1, &id)
	glBindTexture(glTexture2D, id)
	glTexParameteri(glTexture2D, glTextureMinFilter, glLinear)
	glTexParameteri(glTexture2D, glTextureMagFilter, glLinear)
	glTexParameteri(glTexture2D, glTextureWrapS, glClampToEdge)
	glTexParameteri(glTexture2D, glTextureWrapT, glClampToEdge)
	glPixelStorei(glUnpackAlignment, 1)
	glTexImage2D(glTexture2D, 0, int32(internal), int32(w), int32(h), 0, format, glUnsignedByte, nil)
	return id
}

func glesZeroTex(id uint32, format uint32, bpp, w, h int) {
	z := make([]byte, w*h*bpp)
	glBindTexture(glTexture2D, id)
	glPixelStorei(glUnpackAlignment, 1)
	glPixelStorei(glUnpackRowLength, 0)
	glTexSubImage2D(glTexture2D, 0, 0, 0, int32(w), int32(h), format, glUnsignedByte, unsafe.Pointer(&z[0]))
}

func glesBuildProgram() error {
	vs, err := glesCompile(glVertexShader, glesVertSrc)
	if err != nil {
		return err
	}
	glesHasOES = false
	var fs uint32
	if glesFragOverride != "" {
		if f, oerr := glesCompile(glFragmentShader, glesFragOverride); oerr == nil {
			fs = f
			glesHasOES = true
		}
	}
	if fs == 0 {
		fs, err = glesCompile(glFragmentShader, glesFragSrc)
		if err != nil {
			glDeleteShader(vs)
			return err
		}
	}
	prog := glCreateProgram()
	glAttachShader(prog, vs)
	glAttachShader(prog, fs)
	glLinkProgram(prog)
	glDeleteShader(vs)
	glDeleteShader(fs)
	var ok int32
	glGetProgramiv(prog, glLinkStatus, &ok)
	if ok == 0 {
		log := glesLog(true, prog)
		return fmt.Errorf("shader link: %s", log)
	}
	glProg = prog
	glUseProgram(prog)
	uViewport = glGetUniformLocation(prog, "viewport")
	uNdcY = glGetUniformLocation(prog, "ndcY")
	uDestOrig = glGetUniformLocation(prog, "destOrig")
	uDestFlip = glGetUniformLocation(prog, "destFlip")
	uMode = glGetUniformLocation(prog, "mode")
	uClipN = glGetUniformLocation(prog, "clipN")
	uClipRect = glGetUniformLocation(prog, "clipRects")
	uClipRad = glGetUniformLocation(prog, "clipRads")
	uTex = glGetUniformLocation(prog, "tex")
	uTexUV = glGetUniformLocation(prog, "texUV")
	uTexOES = glGetUniformLocation(prog, "texOES")
	uSTMat = glGetUniformLocation(prog, "stMat")
	return nil
}

func glesCompile(kind uint32, src string) (uint32, error) {
	sh := glCreateShader(kind)
	csrc := append([]byte(src), 0)
	p := uintptr(unsafe.Pointer(&csrc[0]))
	glShaderSource(sh, 1, &p, nil)
	glCompileShader(sh)
	runtime.KeepAlive(csrc)
	var ok int32
	glGetShaderiv(sh, glCompileStatus, &ok)
	if ok == 0 {
		log := glesLog(false, sh)
		glDeleteShader(sh)
		return 0, fmt.Errorf("shader compile: %s", log)
	}
	return sh, nil
}

func glesLog(program bool, id uint32) string {
	var n int32
	if program {
		glGetProgramiv(id, glInfoLogLength, &n)
	} else {
		glGetShaderiv(id, glInfoLogLength, &n)
	}
	if n <= 1 {
		return ""
	}
	buf := make([]byte, n)
	if program {
		glGetProgramInfoLog(id, n, nil, &buf[0])
	} else {
		glGetShaderInfoLog(id, n, nil, &buf[0])
	}
	return strings.TrimSpace(string(buf[:n-1]))
}
