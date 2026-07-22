package camera

import "errors"

var (
	errNilDest         = errors.New("camera: Dest is nil")
	errUnsupported     = errors.New("camera: take-photo not implemented on this platform")
	errBadData         = errors.New("camera: invalid capture args")
	errNoPresenter     = errors.New("camera: EscapeHatchBackendContext missing platform presenter")
	errCameraBusy      = errors.New("camera: another capture is already in progress")
	errCameraCancelled = errors.New("camera: cancelled")
	errCameraFailed    = errors.New("camera: capture failed")
)
