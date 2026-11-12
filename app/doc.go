// Package app is shirei's GOOS-selected native backend. An application imports
// this one package, calls SetupWindow then Run, and the compiler links the
// right platform shell. Quit is generic.ExitWithCleanup so AddExitCleanup
// handlers run.
// On macOS, Windows, and Wayland, SetupTransparent preserves per-pixel alpha,
// and SetupUndecorated omits the title bar and window buttons.
// These options are configured before Run; other platforms ignore them.
// Wayland undecorated windows use centered top-layer surfaces when layer-shell
// v4 is available, with on-demand keyboard focus and app-provided window controls.
//
//	darwin  -> cocoabackend  (AppKit; Metal compositor into IOSurface/CALayer)
//	ios     -> iosbackend    (UIKit; Metal into CAMetalLayer; Simulator spike)
//	windows -> win32backend  (Win32; D3D11 compositor, DIB software fallback)
//	linux   -> linuxbackend  (Wayland GLES/dmabuf or X11 software; runtime pick)
//	android -> androidbackend (GLES compositor, software fallback)
//	js      -> jsbackend     (WebGL2 compositor or 2d canvas; wasm)
//
// Windowed paint uses the GPU compositor by default on macOS (Metal), iOS
// (Metal), Wayland (GLES), Android (GLES), Windows (D3D11), and web
// (WebGL2). X11 stays on SoftRenderer. SHIREI_GPU=0 or init failure falls
// back to SoftRenderer on the other shells. Snapshots and headless tests
// always use SoftRenderer. Shells still differ in window, input, and present
// plumbing. Each underlying backend package still works standalone — this
// package is a thin re-export so app code targets a single import regardless
// of OS.
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
// pointer/key input. Audio links without cgo on macOS (purego AudioQueue),
// linux (purego ALSA), windows (winmm), and js (Web Audio). iOS audio is
// still cgo. AppKit and Metal on macOS are purego.
package app
