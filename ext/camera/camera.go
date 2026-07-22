// Package camera is the reference Shirei mobile extension: take a still photo
// via the platform camera UI and write *image.RGBA for the app (e.g.
// shirei.UseImage).
//
// Uses Host.EscapeHatchBackendContext for the platform host object. See
// README.md and docs/mobile-extensions.md.
package camera

import (
	"image"

	"go.hasen.dev/shirei"
)

// TakePhotoArgs is the in-flight capture state for platform handlers.
type TakePhotoArgs struct {
	// Dest receives the photo when capture succeeds (RGBA, shirei-friendly).
	// Must be non-nil; the pointed-to *image.RGBA is replaced on success.
	Dest **image.RGBA
	// Done is optional; called once with nil on success or an error.
	// May run on a non-frame goroutine — use WithFrameLock if touching UI.
	Done func(error)
}

// TakePhoto starts a camera capture using the escape-hatch backend context.
// Does not block; result is async via done and Dest.
// Call from the UI/app thread (e.g. a button handler during a frame).
func TakePhoto(dest **image.RGBA, done func(error)) {
	if dest == nil {
		if done != nil {
			done(errNilDest)
		}
		return
	}
	args := &TakePhotoArgs{Dest: dest, Done: done}
	takePhoto(shirei.GetHost().EscapeHatchBackendContext, args)
}

// takePhoto is set per platform (build tags).
var takePhoto func(ctx shirei.BackendContext, args *TakePhotoArgs)
