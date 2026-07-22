package camera

// Shared capture slot (one in-flight photo at a time). Used by ios/android.

var pending *TakePhotoArgs

func beginCapture(args *TakePhotoArgs) bool {
	if pending != nil {
		return false
	}
	pending = args
	return true
}

func finish(args *TakePhotoArgs, err error) {
	if args != nil && args.Done != nil {
		args.Done(err)
	}
}

func finishPending(err error) {
	args := pending
	pending = nil
	finish(args, err)
}
