//go:build !ios && !android

package camera

import "go.hasen.dev/shirei"

func init() {
	takePhoto = func(ctx shirei.BackendContext, args *TakePhotoArgs) {
		if args != nil && args.Done != nil {
			args.Done(errUnsupported)
		}
	}
}
