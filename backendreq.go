package shirei

// BackendContext is the platform host object for the escape hatch
// (Host.EscapeHatchBackendContext). Concrete types live with each backend
// (e.g. iosbackend.Context, androidbackend.Context, cocoabackend.Context).
// Extensions type-assert when they need native handles.
// See docs/mobile-extensions.md.
type BackendContext interface {
	// Platform is a stable label: "ios", "android", "darwin", "windows",
	// "x11", "wayland", …
	Platform() string
}
