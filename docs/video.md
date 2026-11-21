# Video Playback

`go.hasen.dev/shirei/media` decodes video and audio with the platform media
stack and presents frames directly through Shirei's GPU compositor. Because
video frames are ordinary Shirei images in the same draw list as every other
surface, translucent controls, labels, tooltips, and popups hover cleanly above
playing video without native child-window z-order hacks.

Reference example: [`examples/video_player`](../examples/video_player).

## Quick start

Open the player before `app.Run`, close it on exit, and call `player.View` each
frame inside an `Extrinsic` container sized to the available area:

```go
package main

import (
	"log"

	g "go.hasen.dev/generic"
	app "go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
	"go.hasen.dev/shirei/media"
	. "go.hasen.dev/shirei/widgets"
)

var (
	player  *media.Player
	started bool
)

func main() {
	p, err := media.Open("movie.mp4")
	if err != nil {
		log.Fatal(err)
	}
	p.SetLoop(true)
	player = p
	g.AddExitCleanup(p.Close)

	app.SetupWindow("Video", 960, 540)
	app.Run(RootView)
}

func RootView() {
	if !started {
		started = true
		player.Play()
	}

	Container(Attrs(Viewport, Background(0, 0, 3, 1), Clip), func() {
		box := GetAvailableSize()

		Container(Attrs(Expand, Grow(1), Extrinsic, Center), func() {
			player.View(box)
		})

		// Overlay controls rendered above the video in the same pass.
		Container(Attrs(Float(0, 0), FixSize(box[0], box[1]), Pad(14), ClickThrough), func() {
			Container(Attrs(Grow(1), ClickThrough), func() {})
			Container(Attrs(Row, CrossMid, Gap(8), Pad2(8, 12), Background(220, 12, 6, 0.68), Corners(10), NoClickThrough), func() {
				if Button(SymPlay, "Play / Pause") {
					if player.Playing() {
						player.Pause()
					} else {
						player.Play()
					}
				}
			})
		})
	})
}
```

### Layout tip

Wrap `player.View(box)` in an `Extrinsic` container (`Attrs(Expand, Grow(1), Extrinsic, Center)`)
so the video fits the parent's available budget and scales down cleanly when the
window shrinks. Use `Float(0, 0)` + `ClickThrough` on the overlay wrapper and
`NoClickThrough` on the control bar itself so controls stick to the viewport
edges and hover above the video.

## API (`go.hasen.dev/shirei/media`)

| Function / Method | Behavior |
|---|---|
| `Open(path) (*Player, error)` | Opens a local file path (all platforms) or `http://` / `https://` / `file://` URL (macOS, iOS, Android; Android also accepts `content://` URIs). Decoding begins on first `View`, `ImageId`, `Play`, or `Seek`. |
| `Play()` / `Pause()` | Starts, resumes, or pauses playback. `Play` restarts from `0` if playback has ended. |
| `Seek(sec float64)` | Jumps to `sec` seconds from the start (works while playing or paused). |
| `SetLoop(bool)` | Enables or disables looping at end-of-stream. |
| `SetMuted(bool)` / `Muted() bool` | Mutes or unmutes audio output. |
| `SetVolume(float32)` / `Volume() float32` | Gets or sets playback volume in `[0, 1]`. |
| `Playing() bool` / `Ended() bool` | Current playback state. |
| `Position() time.Duration` | Last sampled playback position. |
| `Duration() time.Duration` | Media duration, or `0` until metadata loads. |
| `Size() (int, int)` | Video pixel dimensions, or `(0, 0)` until ready. |
| `ImageId() shirei.ImageId` | Current frame's Shirei image handle (`0` before the first frame) for custom rendering with `ImageViewAt`. |
| `View(maxSize shirei.Vec2)` | Renders the current frame fitted to `maxSize` preserving aspect ratio (draws a dark placeholder until the first frame arrives). |
| `Err() error` | Asynchronous playback or decode error, if any. |
| `Close()` | Stops playback, releases native resources, and frees the image entry. Safe to call multiple times. |

All control methods are safe to call from the frame function. Call `Close`
outside the frame function (for example via `g.AddExitCleanup`).

## Codec and Container Support

Default builds require **zero LGPL/GPL dependencies** and **zero cgo on desktop**.

### Containers

| Container | macOS | Windows | Linux | iOS | Android |
|---|---|---|---|---|---|
| **MP4 / MOV** | ✅ OS + BSD | ✅ OS + BSD | ✅ GSt / BSD / ffmpeg | ✅ OS + BSD | ✅ OS |
| **WebM** | ✅ BSD | ✅ BSD | ✅ GSt / BSD / ffmpeg | ✅ BSD | ✅ OS |
| **MKV** | ✅ BSD *(AV1/VPx)* | ✅ BSD *(AV1/VPx)* | ✅ GSt / BSD / ffmpeg | ✅ BSD *(AV1/VPx)* | ⚠️ Partial *(OS)* |
| **HLS (`.m3u8`)** | ✅ OS | ❌ | ❌ | ✅ OS | ✅ OS |

### Video Codecs

| Video Codec | macOS | Windows | Linux | iOS | Android |
|---|---|---|---|---|---|
| **H.264 (AVC)** | ✅ OS | ✅ OS | ✅ GSt / ffmpeg *(1)* | ✅ OS | ✅ OS |
| **AV1** | ✅ BSD *(+ OS on M3+)* | ✅ BSD *(+ Store ext)* | ✅ GSt / BSD / ffmpeg | ✅ BSD *(+ OS on A17 Pro+)* | ✅ OS *(Android 10+)* |
| **VP9** | ✅ BSD | ✅ BSD *(+ Store ext)* | ✅ GSt / BSD / ffmpeg | ✅ BSD | ✅ OS |
| **VP8** | ✅ BSD | ✅ BSD | ✅ GSt / BSD / ffmpeg | ✅ BSD | ✅ OS |
| **HEVC (H.265)** | ✅ OS | ⚠️ Paid Store ext | ⚠️ GSt / ffmpeg *(1)* | ✅ OS | ✅ OS *(API 21+)* |

### Audio Codecs

| Audio Codec | macOS | Windows | Linux | iOS | Android |
|---|---|---|---|---|---|
| **AAC** | ✅ OS | ✅ OS | ✅ GSt / ffmpeg *(1)* | ✅ OS | ✅ OS |
| **Opus** | ✅ BSD | ✅ BSD | ✅ GSt / BSD / ffmpeg | ✅ BSD *(+ OS in MP4)* | ✅ OS |
| **Vorbis** | ✅ BSD | ✅ BSD | ✅ GSt / BSD / ffmpeg | ✅ BSD | ✅ OS |
| **MP3** | ✅ OS | ✅ OS | ✅ GSt / ffmpeg | ✅ OS | ✅ OS |
| **FLAC** | ✅ OS *(in MP4/MOV)* | ✅ OS | ✅ GSt / ffmpeg | ✅ OS *(in MP4)* | ✅ OS |

### Notes

- **OS** — Native platform decoder: AVFoundation (macOS/iOS), Media Foundation (Windows), `MediaPlayer` (Android).
- **BSD** — Pure-Go WebM/MKV & MP4 demuxers paired with BSD-2/3 codecs (`libdav1d`, `libvpx`, `libopus`, `libvorbis`). Loaded dynamically via `purego` on desktop (searching `SHIREI_CODEC_DIR`, next to the executable, macOS `../Frameworks`, Homebrew, and system paths), and linked statically on iOS when present.
- **GSt** — Linux system GStreamer 1.0 loaded via `purego` (automatically uses VA-API, OpenH264, and installed distro plugins).
- **(1)** On Linux distributions that strip patented codecs by default (such as Fedora or openSUSE without RPM Fusion/Packman), H.264/HEVC/AAC depend on distro codec packages (or Fedora's `openh264`), while **WebM/MP4 + AV1/VP9/VP8 + Opus/Vorbis** work everywhere with the BSD libraries.
- **Universal 5-platform safe formats:** **MP4 (H.264 + AAC)** and **WebM or MP4 (VP9 or AV1 + Opus)**.
- **Web (Wasm):** Returns `media.ErrUnsupported`.

## Platform Backends & Configuration

- **macOS:** AVFoundation (`IOSurface` → zero-copy Metal `MTLTexture`, CPU RGBA fallback when `SHIREI_GPU=0`), routing WebM/MKV and AV1/VP9/VP8/Opus/Vorbis to the BSD fallback automatically.
- **iOS:** AVFoundation (`IOSurface` → zero-copy Metal) with automatic pause on backgrounding, audio interruptions, and headphone unplug. To enable WebM/AV1/VP9/VP8/Opus/Vorbis on iOS, run once to cross-compile and cache the static BSD archives (~2.4 MB total):
  ```sh
  ./shirei/media/scripts/build_ios_codecs.sh
  ```
  `shirei_mobilerun` automatically links any `.a` files in `~/Library/Caches/shirei/ios-codecs/<sdk>` (or `SHIREI_IOS_CODECS_DIR`) with no Go build tags required.
- **Android:** `android.media.MediaPlayer` + `SurfaceTexture` sampled directly via `GL_TEXTURE_EXTERNAL_OES`, with `AudioManager` focus management and automatic pause on `APP_CMD_PAUSE`/`APP_CMD_STOP`.
- **Windows:** Media Foundation (`IMFMediaEngine` → shared D3D11 texture, zero cgo) + BSD fallback.
- **Linux:** Automatic selection order:
  1. In-process `libav*` + VA-API dmabuf (only when built with `-tags libav`)
  2. System GStreamer 1.0 (`purego`, zero cgo, in-process seeking)
  3. Permissive BSD web codecs (`purego`, zero cgo)
  4. `ffmpeg` / `ffprobe` on `PATH`

### Environment variables & build tags

| Variable / Tag | Effect |
|---|---|
| `SHIREI_MEDIA` | Force a specific backend: `avfoundation` or `permissive` (macOS/iOS); `mf`, `libav`, or `permissive` (Windows); `libav`, `gstreamer`, `permissive`, or `ffmpeg` (Linux). |
| `SHIREI_CODEC_DIR` | Directory searched first when dynamically loading `libdav1d`, `libvpx`, `libopus`, and `libvorbis` on desktop. |
| `SHIREI_IOS_CODECS_DIR` | Override directory containing iOS static `.a` codec archives for `shirei_mobilerun`. |
| `-tags libav` | Opt-in LGPL cgo backend on Linux and Windows using `libavformat`/`libavcodec`/`libswscale`/`libswresample` (with VA-API dmabuf zero-copy on Linux Wayland). |

## Low-Level External GPU Textures

Apps producing their own GPU textures (camera feeds, custom decoders, offscreen
renderers) can register frames directly with `shirei.UseExternalImage(key, frame)`
using `ExternalIOSurfaceBGRA` (macOS/iOS Metal), `ExternalD3D11Texture` (Windows
D3D11), `ExternalNV12` / `ExternalDMABuf` (Linux GLES / Windows D3D11), or
`ExternalAndroidSurfaceTexture` (Android GLES). Query backend capability with
`shirei.SupportsExternalKind(kind)` and free entries with
`shirei.ReleaseImage(key)`. See [`demos/gputexture`](../demos/gputexture).
