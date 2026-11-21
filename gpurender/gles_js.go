//go:build js

package gpurender

import (
	"fmt"
	"runtime"
	"strings"
	"syscall/js"
	"time"
	"unsafe"
)

// WebGL2 constants (GLES 3.0 values).
const (
	glColorBufferBit   = 0x00004000
	glBlend            = 0x0BE2
	glOne              = 1
	glOneMinusSrcAlpha = 0x0303
	glScissorTest      = 0x0C11
	glTexture2D        = 0x0DE1
	glRGBA             = 0x1908
	glRGBA8            = 0x8058
	glUnsignedByte     = 0x1401
	glR8               = 0x8229
	glRed              = 0x1903
	glLinear           = 0x2601
	glClampToEdge      = 0x812F
	glTextureMinFilter = 0x2801
	glTextureMagFilter = 0x2800
	glTextureWrapS     = 0x2802
	glTextureWrapT     = 0x2803
	glUnpackAlignment  = 0x0CF5
	glUnpackRowLength  = 0x0CF2
	glTriangles        = 0x0004
	glVertexShader     = 0x8B31
	glFragmentShader   = 0x8B30
	glCompileStatus    = 0x8B81
	glLinkStatus       = 0x8B82
	glArrayBuffer      = 0x8892
	glDynamicDraw      = 0x88E8
	glFloat            = 0x1406
	glTexture0         = 0x84C0
	glRenderer         = 0x1F01
	glVendor           = 0x1F00
	glMaxTextureSize   = 0x0D33
)

var (
	gl          js.Value
	glesDevName string
	glesReady   bool

	glProg     js.Value
	glVAO      js.Value
	glVBO      js.Value
	glTexWhite js.Value
	glTexGlyph js.Value
	glTexColor js.Value
	texImages  map[uint32]glImgTexJS

	uViewport js.Value
	uNdcY     js.Value
	uDestOrig js.Value
	uDestFlip js.Value
	uMode     js.Value
	uClipN    js.Value
	uClipRect js.Value
	uClipRad  js.Value
	uTex      js.Value

	jsPixU8   js.Value
	jsZeroU8  js.Value
	jsClipF32 js.Value
	jsWhite   = []byte{255, 255, 255, 255}
)

func DeviceName() string { return glesDevName }

func WaitIdle() {
	if glesReady && gl.Truthy() {
		gl.Call("finish")
	}
}

func Forget(dest unsafe.Pointer) {}

func gpuInit() error {
	if !gl.Truthy() {
		return fmt.Errorf("webgl2: BindCanvas first")
	}
	return nil
}

// BindCanvas attaches a WebGL2 context. The canvas is the default
// framebuffer. Call once after canvas.getContext("webgl2").
func BindCanvas(ctx js.Value) error {
	if !ctx.Truthy() {
		return fmt.Errorf("nil webgl2 context")
	}
	gl = ctx
	return glesFinishInit("webgl2")
}

func gpuResetAtlases() {
	if !glesReady {
		return
	}
	glesZeroTex(glTexGlyph, glRed, 1, glyphAtlasW, glyphAtlasH)
	glesZeroTex(glTexColor, glRGBA, 4, colorAtlasW, colorAtlasH)
}

type glImgTexJS struct {
	tex  js.Value
	w, h int
}

func gpuImageEnsure(id uint32, w, h int) bool {
	if !glesReady || w <= 0 || h <= 0 {
		return false
	}
	if cur, ok := texImages[id]; ok && cur.tex.Truthy() && cur.w == w && cur.h == h {
		return true
	}
	tex := glesMakeTex(glRGBA8, glRGBA, w, h)
	if !tex.Truthy() {
		return false
	}
	if old, ok := texImages[id]; ok && old.tex.Truthy() {
		gl.Call("deleteTexture", old.tex)
	}
	texImages[id] = glImgTexJS{tex: tex, w: w, h: h}
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
	gl.Call("deleteTexture", t.tex)
	delete(texImages, id)
}

func glesFinishInit(devName string) error {
	if glesReady {
		return nil
	}
	max := gl.Call("getParameter", glMaxTextureSize).Int()
	if max < glyphAtlasW || max < colorAtlasW {
		return fmt.Errorf("webgl2: MAX_TEXTURE_SIZE %d too small", max)
	}
	if err := glesBuildProgram(); err != nil {
		return err
	}
	glVAO = gl.Call("createVertexArray")
	gl.Call("bindVertexArray", glVAO)
	glVBO = gl.Call("createBuffer")
	gl.Call("bindBuffer", glArrayBuffer, glVBO)
	for i := 0; i < 4; i++ {
		gl.Call("enableVertexAttribArray", i)
		gl.Call("vertexAttribDivisor", i, 1)
	}
	glTexWhite = glesMakeTex(glRGBA8, glRGBA, 1, 1)
	gl.Call("bindTexture", glTexture2D, glTexWhite)
	gl.Call("texSubImage2D", glTexture2D, 0, 0, 0, 1, 1, glRGBA, glUnsignedByte, bytesToU8(jsWhite))
	glTexGlyph = glesMakeTex(glR8, glRed, glyphAtlasW, glyphAtlasH)
	glTexColor = glesMakeTex(glRGBA8, glRGBA, colorAtlasW, colorAtlasH)
	glesZeroTex(glTexGlyph, glRed, 1, glyphAtlasW, glyphAtlasH)
	glesZeroTex(glTexColor, glRGBA, 4, colorAtlasW, colorAtlasH)
	texImages = make(map[uint32]glImgTexJS)
	gl.Call("enable", glBlend)
	gl.Call("blendFunc", glOne, glOneMinusSrcAlpha)
	gl.Call("enable", glScissorTest)
	gl.Call("pixelStorei", 0x9240 /* UNPACK_FLIP_Y_WEBGL */, 0)
	gl.Call("pixelStorei", 0x9241 /* UNPACK_PREMULTIPLY_ALPHA_WEBGL */, 0)

	if ext := gl.Call("getExtension", "WEBGL_debug_renderer_info"); ext.Truthy() {
		r := gl.Call("getParameter", ext.Get("UNMASKED_RENDERER_WEBGL"))
		v := gl.Call("getParameter", ext.Get("UNMASKED_VENDOR_WEBGL"))
		glesDevName = strings.TrimSpace(v.String() + " " + r.String())
	}
	if glesDevName == "" {
		r := gl.Call("getParameter", glRenderer)
		v := gl.Call("getParameter", glVendor)
		glesDevName = strings.TrimSpace(v.String() + " " + r.String())
	}
	if glesDevName == "" {
		glesDevName = devName
	}
	jsClipF32 = js.Global().Get("Float32Array").New(16)
	glesReady = true
	return nil
}

func gpuBindDest(dest unsafe.Pointer, w, h int) error {
	if !glesReady {
		return fmt.Errorf("gles not initialized")
	}
	if w <= 0 || h <= 0 {
		return fmt.Errorf("bad target")
	}
	gl.Call("bindFramebuffer", 0x8D40 /* FRAMEBUFFER */, js.Null())
	gl.Call("disable", glScissorTest)
	gl.Call("viewport", 0, 0, w, h)
	gl.Call("clearColor", 1, 1, 1, 1)
	gl.Call("clear", glColorBufferBit)
	gl.Call("enable", glScissorTest)
	gl.Call("useProgram", glProg)
	// Canvas default framebuffer: GL y=0 is bottom, same as Android EGL window.
	gl.Call("uniform2f", uNdcY, -2, 1)
	gl.Call("uniform2f", uDestOrig, 0, 0)
	gl.Call("uniform1i", uDestFlip, 1)
	return nil
}

func gpuScissor(x, y, cw, ch, sceneW, sceneH int) {
	sy := sceneH - y - ch
	gl.Call("scissor", x, sy, cw, ch)
}

func gpuPresent(dest unsafe.Pointer, wait bool) int64 {
	if wait {
		t1 := time.Now()
		gl.Call("finish")
		return time.Since(t1).Nanoseconds()
	}
	gl.Call("flush")
	return 0
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

	gl.Call("bindVertexArray", glVAO)
	gl.Call("bindBuffer", glArrayBuffer, glVBO)
	if len(quads) > 0 {
		raw := quadBytes(quads)
		gl.Call("bufferData", glArrayBuffer, bytesToU8(raw), glDynamicDraw)
		runtime.KeepAlive(quads)
	}

	gl.Call("useProgram", glProg)
	gl.Call("uniform2f", uViewport, float32(w), float32(h))
	gl.Call("uniform1i", uTex, 0)
	gl.Call("activeTexture", glTexture0)

	stride := int(unsafe.Sizeof(Quad{}))
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
			if im, ok := texImages[uint32(b.TexKey)]; ok && im.tex.Truthy() {
				tex = im.tex
			}
			mode = 2
		}
		gl.Call("bindTexture", glTexture2D, tex)
		gl.Call("uniform1ui", uMode, mode)
		n := int(b.ClipN)
		if n > 4 {
			n = 4
		}
		gl.Call("uniform1ui", uClipN, n)
		setClipUniform(uClipRect, b.ClipRect)
		setClipUniform(uClipRad, b.ClipRad)

		off := int(b.First) * stride
		gl.Call("vertexAttribPointer", 0, 4, glFloat, false, stride, off)
		gl.Call("vertexAttribPointer", 1, 4, glFloat, false, stride, off+16)
		gl.Call("vertexAttribPointer", 2, 4, glFloat, false, stride, off+32)
		gl.Call("vertexAttribPointer", 3, 4, glFloat, false, stride, off+48)
		gl.Call("drawArraysInstanced", glTriangles, 0, 6, b.Count)
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
	gl.Call("pixelStorei", glUnpackAlignment, 1)
	for i := range uploads {
		u := &uploads[i]
		if len(u.pix) == 0 || u.w <= 0 || u.h <= 0 {
			continue
		}
		var tex js.Value
		var format int
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
		}
		if !tex.Truthy() {
			continue
		}
		rowPix := u.stride / bpp
		if rowPix != u.w {
			gl.Call("pixelStorei", glUnpackRowLength, rowPix)
		} else {
			gl.Call("pixelStorei", glUnpackRowLength, 0)
		}
		gl.Call("bindTexture", glTexture2D, tex)
		gl.Call("texSubImage2D", glTexture2D, 0, u.x, u.y, u.w, u.h,
			format, glUnsignedByte, bytesToU8(u.pix))
	}
	gl.Call("pixelStorei", glUnpackRowLength, 0)
}

func glesMakeTex(internal, format, w, h int) js.Value {
	id := gl.Call("createTexture")
	gl.Call("bindTexture", glTexture2D, id)
	gl.Call("texParameteri", glTexture2D, glTextureMinFilter, glLinear)
	gl.Call("texParameteri", glTexture2D, glTextureMagFilter, glLinear)
	gl.Call("texParameteri", glTexture2D, glTextureWrapS, glClampToEdge)
	gl.Call("texParameteri", glTexture2D, glTextureWrapT, glClampToEdge)
	gl.Call("pixelStorei", glUnpackAlignment, 1)
	gl.Call("texImage2D", glTexture2D, 0, internal, w, h, 0, format, glUnsignedByte, js.Null())
	return id
}

func glesZeroTex(id js.Value, format, bpp, w, h int) {
	n := w * h * bpp
	z := ensureZero(n)
	gl.Call("bindTexture", glTexture2D, id)
	gl.Call("pixelStorei", glUnpackAlignment, 1)
	gl.Call("pixelStorei", glUnpackRowLength, 0)
	gl.Call("texSubImage2D", glTexture2D, 0, 0, 0, w, h, format, glUnsignedByte, z)
}

func glesBuildProgram() error {
	vs, err := glesCompile(glVertexShader, glesVertSrc)
	if err != nil {
		return err
	}
	fs, err := glesCompile(glFragmentShader, glesFragSrc)
	if err != nil {
		gl.Call("deleteShader", vs)
		return err
	}
	prog := gl.Call("createProgram")
	gl.Call("attachShader", prog, vs)
	gl.Call("attachShader", prog, fs)
	gl.Call("linkProgram", prog)
	gl.Call("deleteShader", vs)
	gl.Call("deleteShader", fs)
	if !progParam(prog, glLinkStatus) {
		return fmt.Errorf("shader link: %s", gl.Call("getProgramInfoLog", prog).String())
	}
	glProg = prog
	gl.Call("useProgram", prog)
	uViewport = gl.Call("getUniformLocation", prog, "viewport")
	uNdcY = gl.Call("getUniformLocation", prog, "ndcY")
	uDestOrig = gl.Call("getUniformLocation", prog, "destOrig")
	uDestFlip = gl.Call("getUniformLocation", prog, "destFlip")
	uMode = gl.Call("getUniformLocation", prog, "mode")
	uClipN = gl.Call("getUniformLocation", prog, "clipN")
	uClipRect = gl.Call("getUniformLocation", prog, "clipRects")
	uClipRad = gl.Call("getUniformLocation", prog, "clipRads")
	uTex = gl.Call("getUniformLocation", prog, "tex")
	if uTexUV := gl.Call("getUniformLocation", prog, "texUV"); uTexUV.Truthy() {
		gl.Call("uniform1i", uTexUV, 1)
	}
	return nil
}

func glesCompile(kind int, src string) (js.Value, error) {
	sh := gl.Call("createShader", kind)
	gl.Call("shaderSource", sh, src)
	gl.Call("compileShader", sh)
	ok := gl.Call("getShaderParameter", sh, glCompileStatus)
	if !ok.Bool() {
		log := gl.Call("getShaderInfoLog", sh).String()
		gl.Call("deleteShader", sh)
		return js.Value{}, fmt.Errorf("shader compile: %s", log)
	}
	return sh, nil
}

func progParam(prog js.Value, pname int) bool {
	v := gl.Call("getProgramParameter", prog, pname)
	return v.Bool()
}

func quadBytes(quads []Quad) []byte {
	n := len(quads) * int(unsafe.Sizeof(Quad{}))
	return unsafe.Slice((*byte)(unsafe.Pointer(&quads[0])), n)
}

func bytesToU8(b []byte) js.Value {
	if len(b) == 0 {
		return js.Null()
	}
	if !jsPixU8.Truthy() || jsPixU8.Get("length").Int() < len(b) {
		jsPixU8 = js.Global().Get("Uint8Array").New(len(b))
	}
	view := jsPixU8
	if view.Get("length").Int() != len(b) {
		view = jsPixU8.Call("subarray", 0, len(b))
	}
	js.CopyBytesToJS(view, b)
	return view
}

func ensureZero(n int) js.Value {
	if !jsZeroU8.Truthy() || jsZeroU8.Get("length").Int() < n {
		jsZeroU8 = js.Global().Get("Uint8Array").New(n)
	} else {
		jsZeroU8.Call("fill", 0, 0, n)
	}
	if jsZeroU8.Get("length").Int() == n {
		return jsZeroU8
	}
	return jsZeroU8.Call("subarray", 0, n)
}

func setClipUniform(loc js.Value, src [4][4]float32) {
	u8 := js.Global().Get("Uint8Array").New(jsClipF32.Get("buffer"))
	raw := unsafe.Slice((*byte)(unsafe.Pointer(&src[0][0])), 64)
	js.CopyBytesToJS(u8, raw)
	gl.Call("uniform4fv", loc, jsClipF32)
}
