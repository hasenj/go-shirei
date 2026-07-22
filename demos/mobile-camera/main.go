// Camera plugin demo (iOS + Android): button → system camera / gallery →
// show *image.RGBA via shirei.UseImage.
//
//	go run ./shirei/mobilerun -platform ios ./shirei/demos/mobile-camera
//	go run ./shirei/mobilerun -platform android ./shirei/demos/mobile-camera
//
// Desktop: take-photo is unsupported.
package main

import (
	"fmt"
	"image"

	"go.hasen.dev/shirei/app"
	"go.hasen.dev/shirei/ext/camera"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

func main() {
	app.SetupWindow("Camera demo", 400, 640)
	app.Run(root)
}

var (
	photo  *image.RGBA
	status = "Tap the button to take a photo."
	busy   bool
	imgKey = "camera-demo/last"
)

func root() {
	ModAttrs(Viewport, Background(220, 12, 96, 1), Pad(16), Gap(12))

	Label("Camera plugin demo", FontSize(20), FontWeight(WeightBold))
	Label("EscapeHatchBackendContext + ext/camera → *image.RGBA → UseImage", FontSize(13), TextColor(0, 0, 40, 1))

	disabled := busy
	if Button(SymImage, "Take photo") && !disabled {
		busy = true
		status = "Opening camera…"
		dest := &photo
		camera.TakePhoto(dest, func(err error) {
			WithFrameLock(func() {
				busy = false
				if err != nil {
					status = fmt.Sprintf("Error: %v", err)
					RequestNextFrame()
					return
				}
				if photo == nil {
					status = "No photo returned."
					RequestNextFrame()
					return
				}
				UseImage(imgKey, photo)
				status = fmt.Sprintf("Got %d×%d image.", photo.Bounds().Dx(), photo.Bounds().Dy())
				RequestNextFrame()
			})
		})
	}

	if busy {
		Label("Waiting for camera / gallery…", TextColor(30, 60, 40, 1))
	}
	Label(status, FontSize(14))

	if photo == nil {
		Label("No photo yet", TextColor(0, 0, 50, 1))
	} else {
		ImageView(UseImage(imgKey, photo), Vec2{320, 480})
	}
}
