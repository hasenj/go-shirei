// Package app is shirei's GOOS-selected native backend. An application imports
// this one package, calls SetupWindow then Run, and the compiler links the
// right platform shell:
//
//	darwin  -> cocoabackend  (AppKit + IOSurface/CALayer present)
//	ios     -> iosbackend    (UIKit + CALayer present; Simulator spike)
//	windows -> win32backend  (Win32 + CreateDIBSection/StretchDIBits present)
//	linux   -> linuxbackend  (Wayland wl_shm / X11 MIT-SHM; selected at runtime)
//	js      -> jsbackend     (canvas + requestAnimationFrame present; wasm)
//
// All shells share shirei's core software renderer; they differ only in
// window, input, and present plumbing. Each underlying backend package still
// works standalone — this package is a thin re-export so app code targets a
// single import regardless of OS. On iOS, use ./ios-run.sh to c-archive +
// launch a main package in the Simulator (UIApplicationMain owns the process).
//
// The OS selection is purely compile-time, via build constraints on the
// app_<goos>.go files. Within Linux, the Wayland-vs-X11 choice is made at
// runtime inside linuxbackend.
//
// The package also carries the platform audio-output boundary: StartAudio
// opens the default output device and pulls mono float32 samples from an
// app-supplied fill function (audio_<goos>.go — AudioQueue on macOS and iOS,
// ALSA via purego on linux, winmm waveOut on windows, Web Audio on js).
// On js, audio prefers AudioWorklet + SharedArrayBuffer (requires COOP+COEP
// isolation from shirei_web .headers) and falls back to ScriptProcessor.
// iOS uses Playback + MixWithOthers (audible with silent switch on, mixes with
// other apps; no background audio) and reports interruptions via
// shirei.GetInputState().AudioInterrupted. On the web, the AudioContext often
// stays suspended until a user gesture; the js backend resumes on first
// pointer/key input. Each OS's audio backend uses the same linking mechanism
// its window backend already relies on (cgo on Apple platforms, no cgo on
// linux/windows/js).
package app
