// Package waylandbackend is shirei's native Wayland shell: it owns the window and
// input and presents the shared core software renderer's BGRA buffer via a
// wl_shm shared-memory pool (no per-frame pixels over the socket), mirroring what
// cocoabackend/win32backend/x11backend do on their platforms.
//
// The Wayland protocol is spoken in pure Go via github.com/neurlang/wayland
// (the maintained successor to rajveermalviya/go-wayland), so the backend
// cross-compiles from any host with CGO disabled — except keyboard text, which
// needs libxkbcommon (added later). The real implementation is in the
// build-tagged *_linux.go files; on other platforms this is an empty package so
// `go build ./...` stays green.
package waylandbackend
