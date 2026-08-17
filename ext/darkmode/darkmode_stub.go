//go:build (!darwin && !windows && !linux && !js && !android) || (darwin && !ios && x11darwin) || (darwin && !cgo) || (ios && !cgo) || (android && !cgo)

package darkmode

func initPlatform() {
	// Fallback stub for unsupported platforms or headless / non-cgo builds
}
