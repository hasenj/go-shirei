// Package x11backend is a direct-X11 backend for shirei: it opens a window via
// the X11 core protocol (pure Go, github.com/jezek/xgb), routes input, and
// presents the core software renderer's BGRA buffer with PutImage. It mirrors
// cocoabackend/win32backend minus the rasterizer, and is one half of the Linux
// shell (the other being Wayland); ../notes/backends-plan.md has the roadmap.
//
// Build gating: the implementation is constrained to
//
//	//go:build linux || (darwin && x11darwin)
//
// so it is the real backend on Linux but, on macOS, only compiles under the
// explicit `x11darwin` build tag — used solely to test the X11 protocol code
// against XQuartz during development. A normal macOS build (and the GOOS-selected
// go.hasen.dev/shirei/app wrapper, whose darwin path is cocoabackend) never
// references it, so there is no shippable "X11 on macOS" backend. This file
// carries no build constraint so the package is always a valid (empty) package
// on platforms where the implementation is excluded.
package x11backend
