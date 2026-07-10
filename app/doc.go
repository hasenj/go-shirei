// Package app is shirei's GOOS-selected native backend. An application imports
// this one package, calls SetupWindow then Run, and the compiler links the
// right platform shell:
//
//	darwin  -> cocoabackend  (AppKit + IOSurface/CALayer present)
//	windows -> win32backend  (Win32 + CreateDIBSection/StretchDIBits present)
//	linux   -> linuxbackend  (Wayland wl_shm / X11 MIT-SHM; selected at runtime)
//
// All three shells share shirei's core software renderer; they differ only in
// window, input, and present plumbing. Each underlying backend package still
// works standalone — this package is a thin re-export so app code targets a
// single import regardless of OS.
//
// The OS selection is purely compile-time, via build constraints on the
// app_<goos>.go files. Within Linux, the Wayland-vs-X11 choice is made at
// runtime inside linuxbackend.
//
// The package also carries the platform audio-output boundary: StartAudio
// opens the default output device and pulls mono float32 samples from an
// app-supplied fill function (audio_<goos>.go — AudioQueue on darwin, ALSA
// via purego on linux, winmm waveOut on windows). Each OS's audio backend
// uses the same linking mechanism its window backend already relies on, so
// the build story does not change: cgo on darwin, no cgo on linux/windows.
package app
