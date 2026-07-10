// Window icon: decodes the image recorded by SetupIcon and hands it to the
// window as a 32bpp alpha HICON via WM_SETICON, which Windows shows in the
// title bar, taskbar, and Alt-Tab switcher.
package win32backend

import (
	"image"
	"syscall"
	"unsafe"

	"go.hasen.dev/shirei/internal/iconimg"
)

var (
	winIconPath string
	winIconImg  *image.NRGBA
)

// SetupIcon records the path of the image (PNG etc.) used as the window's
// title-bar/taskbar icon. Call it before Run; empty keeps the default icon.
func SetupIcon(imagePath string) {
	winIconPath = imagePath
}

// SetupIconImage is SetupIcon from an in-memory image (e.g. decoded from
// go:embed-ed bytes) instead of a file. It takes precedence over SetupIcon.
func SetupIconImage(img image.Image) {
	winIconImg = iconimg.FromImage(img)
}

// iconImage resolves whichever icon source was recorded (nil if none/broken).
func iconImage() *image.NRGBA {
	if winIconImg != nil {
		return winIconImg
	}
	if winIconPath != "" {
		return iconimg.LoadNRGBA(winIconPath)
	}
	return nil
}

// applyWindowIcon builds the HICON and assigns it to the window. Best-effort:
// on any failure the window keeps the default application icon. The HICON must
// stay valid while the window uses it, so it is never destroyed (it lives for
// the process).
func applyWindowIcon() {
	img := iconImage()
	if img == nil {
		return
	}
	w, h := img.Bounds().Dx(), img.Bounds().Dy()

	// Color bitmap: a 32bpp top-down DIB holding straight-alpha BGRA, the same
	// pixel layout as a 32bpp .ico image.
	bmi := bitmapInfo{Header: bitmapInfoHeader{
		Width:       int32(w),
		Height:      -int32(h), // negative => top-down rows
		Planes:      1,
		BitCount:    32,
		Compression: biRGB,
	}}
	bmi.Header.Size = uint32(unsafe.Sizeof(bmi.Header))
	var bits unsafe.Pointer
	bm, _, _ := procCreateDIBSection.Call(0, uintptr(unsafe.Pointer(&bmi)),
		dibRGBColors, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if bm == 0 || bits == nil {
		return
	}
	dst := unsafe.Slice((*byte)(bits), w*h*4)
	for y := 0; y < h; y++ {
		srow := img.Pix[y*img.Stride : y*img.Stride+w*4]
		drow := dst[y*w*4:]
		for x := 0; x < w; x++ {
			drow[x*4+0] = srow[x*4+2] // B
			drow[x*4+1] = srow[x*4+1] // G
			drow[x*4+2] = srow[x*4+0] // R
			drow[x*4+3] = srow[x*4+3] // A
		}
	}

	// CreateIconIndirect requires a monochrome AND-mask even though it is
	// ignored once any pixel has nonzero alpha; a zeroed one suffices. Rows of
	// a CreateBitmap-supplied buffer are word-aligned.
	maskBits := make([]byte, ((w+15)/16)*2*h)
	mask, _, _ := procCreateBitmap.Call(uintptr(w), uintptr(h), 1, 1,
		uintptr(unsafe.Pointer(&maskBits[0])))

	ii := iconInfo{FIcon: 1, HbmMask: syscall.Handle(mask), HbmColor: syscall.Handle(bm)}
	hicon, _, _ := procCreateIconIndirect.Call(uintptr(unsafe.Pointer(&ii)))
	procDeleteObject.Call(bm) // the icon owns copies of both bitmaps
	procDeleteObject.Call(mask)
	if hicon == 0 {
		return
	}
	procSendMessageW.Call(uintptr(hwnd), wmSeticon, iconSmall, hicon)
	procSendMessageW.Call(uintptr(hwnd), wmSeticon, iconBig, hicon)
}
