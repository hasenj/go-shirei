//go:build linux && !android

package gpurender

import (
	"runtime"
	"testing"
	"unsafe"

	"go.hasen.dev/shirei"
)

func TestGLESTransparentCanvas(t *testing.T) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	if err := Init(); err != nil {
		t.Skipf("GLES unavailable: %v", err)
	}
	lib, err := dlopenFirst("libGLESv2.so.2", "libGLESv2.so")
	if err != nil {
		t.Fatal(err)
	}
	var readPixels func(int32, int32, int32, int32, uint32, uint32, unsafe.Pointer)
	if err := bindSym(lib, "glReadPixels", &readPixels); err != nil {
		t.Fatal(err)
	}
	host := shirei.GetHost()
	old, mods := host.TransparentBackground, append([]uint64(nil), allowedMods...)
	defer func() { host.TransparentBackground = old; SetModifiers(mods) }()
	SetModifiers([]uint64{drmModInvalid})
	for _, transparent := range []bool{true, false} {
		host.TransparentBackground = transparent
		target, err := AllocTarget(32, 32)
		if err != nil {
			t.Fatal(err)
		}
		wantFormat := uint32(drmFormatXRGB8888)
		if transparent {
			wantFormat = drmFormatARGB8888
		}
		if target.Format() != wantFormat {
			t.Fatalf("format %#x, want %#x", target.Format(), wantFormat)
		}
		red := shirei.Vec4{0, 100, 50, .5}
		green := shirei.Vec4{120, 100, 50, .5}
		scene := []shirei.Surface{
			{Rect: shirei.Rect{Size: shirei.Vec2{20, 32}}, Color1: red, Color2: red},
			{Rect: shirei.Rect{Origin: shirei.Vec2{12, 0}, Size: shirei.Vec2{12, 32}}, Color1: green, Color2: green},
		}
		for frame := range 4 {
			surfaces := scene
			if frame == 3 {
				surfaces = nil
			}
			if err := Render(target.Handle(), 32, 32, 1, surfaces, nil, nil, nil, true); err != nil {
				t.Fatal(err)
			}
			glBindFramebuffer(glFramebuffer, target.fbo)
			for _, sample := range []struct {
				x    int32
				rgba [4]byte
			}{
				{28, [4]byte{}}, {4, [4]byte{128, 0, 0, 128}}, {16, [4]byte{64, 128, 0, 192}},
			} {
				want := sample.rgba
				if frame == 3 {
					want = [4]byte{}
				}
				if !transparent {
					for c := range 3 {
						want[c] += 255 - want[3]
					}
					want[3] = 255
				}
				var got [4]byte
				readPixels(sample.x, 16, 1, 1, glRGBA, glUnsignedByte, unsafe.Pointer(&got[0]))
				for c := range 4 {
					if delta := int(got[c]) - int(want[c]); delta < -2 || delta > 2 {
						t.Fatalf("transparent=%t frame=%d x=%d: RGBA=%v want %v", transparent, frame, sample.x, got, want)
					}
				}
			}
		}
		FreeTarget(target)
	}
}
