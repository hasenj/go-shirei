# Mobile extensions

Shirei’s core handles layout, text, images, input, and drawing. Features that
need a **platform host** — camera, share sheet, sensors, and similar — live
in **extension packages**: Go modules that call into the OS and return results
to the app (for example as `*image.RGBA` for `UseImage`).

This guide covers:

1. How extensions reach the native window or activity at runtime
   (`Host.EscapeHatchBackendContext`)
2. How **mobilerun** applies privacy and packaging declarations
   (`shirei.capabilities`)
3. How to use and write extensions, with
   [`camera`](../ext/camera) (`go.hasen.dev/shirei/ext/camera`) as the reference

Related: [Running on iOS](ios.md), [Running on Android](android.md).

---

## 1. Runtime vs packaging

| Concern | How it works |
|---------|----------------|
| **Runtime** — open system UI or call a native API that needs the Activity or root view controller | Your extension reads `Host.EscapeHatchBackendContext` and uses the platform context |
| **Packaging** — Info.plist purpose strings, Android permissions, related manifest queries | A `shirei.capabilities` file on the extension (or app) module; **mobilerun** injects the matching declarations when you build |

Apps import the extension and call its portable API. mobilerun merges capability
tokens from the dependency graph into the build’s Info.plist / AndroidManifest.

---

## 2. Runtime: escape hatch

When an extension needs the live host (window, activity, root view controller),
it reads:

```go
ctx := shirei.GetHost().EscapeHatchBackendContext
```

The active backend sets this when the window or activity is ready. It is `nil`
in headless mode and before attach.

Use portable Shirei APIs and the extension’s public surface in ordinary app UI.
Type-assert the concrete backend context only inside extension platform code
(or equivalent host glue).

### Flow

```
Backend Run
    Host.EscapeHatchBackendContext = platformContext
         │
App frame (button, …)
    extension portable API (e.g. camera.TakePhoto)
         │
         ▼
Extension reads EscapeHatchBackendContext
    type-assert → start OS UI; return quickly
         │
         … async OS work …
         ▼
Result (extension-defined): e.g. *image.RGBA + done(err)
App: UseImage / RequestNextFrame
```

### Types

```go
type BackendContext interface {
    Platform() string // "ios", "android", "darwin", "windows", "x11", "wayland", …
}

type Host struct {
    // …
    EscapeHatchBackendContext BackendContext
}
```

Start OS UI and return; deliver results later (`Done`, channel, dest pointer).
Do not block the frame waiting for a system sheet. Prefer the UI or app thread
(for example a button handler during a frame).

---

## 3. Backend context types

`EscapeHatchBackendContext` is only `shirei.BackendContext`. Each backend
provides a **concrete** type with the handles that platform needs. Extensions
type-assert or switch on `Platform()`.

### iOS — `iosbackend.Context`

| | |
|--|--|
| **Package** | `go.hasen.dev/shirei/iosbackend` |
| **Type** | `iosbackend.Context` |
| **`Platform()`** | `"ios"` |
| **Extra method** | `RootViewController() unsafe.Pointer` |
| **Meaning** | Opaque `UIViewController *` for the Shirei window’s root view controller (`nil` before attach). Present UIKit controllers from it. |
| **Thread** | Prefer the main / frame path. Hop to the main queue if you leave it. |

```go
//go:build ios

type iosPresenter interface {
    shirei.BackendContext
    RootViewController() unsafe.Pointer
}

func present(ctx shirei.BackendContext) {
    p, ok := ctx.(iosPresenter)
    if !ok || p.RootViewController() == nil {
        return
    }
    vc := p.RootViewController()
    // present UIImagePickerController (etc.) from vc
    _ = vc
}
```

In ObjC/cgo: `(__bridge UIViewController *)vc`.

### Android — `androidbackend.Context`

| | |
|--|--|
| **Package** | `go.hasen.dev/shirei/androidbackend` |
| **Type** | `androidbackend.Context` |
| **`Platform()`** | `"android"` |
| **Extra method** | `Activity() unsafe.Pointer` — live `ShireiActivity` `jobject` (glue-owned) |
| **Host API** (same package) | `OnActivityResult`, `OnPermissionResult`, `StartActivityForResult`, `RequestPermissions`, `CheckPermission`, `JNIEnv` |
| **Thread** | Frame work runs on the native **app thread**. Activity-result callbacks run on the **UI thread**; the JNIEnv passed into the handler is valid only for that call. |

The host forwards `startActivityForResult` / `requestPermissions` and their
results. Feature-specific Intents and decoding stay in the extension.

```go
//go:build android

import (
    "unsafe"
    "go.hasen.dev/shirei/androidbackend"
)

const myReq = 0xBEEF

func init() {
    androidbackend.OnActivityResult(myReq, func(env unsafe.Pointer, resultCode int, intent unsafe.Pointer) {
        // decode intent with env (UI thread); valid only until return
    })
}

func start(ctx shirei.BackendContext) {
    if ctx == nil || ctx.Platform() != "android" {
        return
    }
    // Build an Intent in C/JNI, then:
    // androidbackend.StartActivityForResult(intent, myReq)
}
```

See `ext/camera` (`camera_android.go` + `camera_jni_android.c`) for a full
example: permissions, capture/gallery Intents, and RGBA extraction.

### Desktop backends

| Backend | Package | `Platform()` | Extra |
|---------|---------|--------------|--------|
| macOS | `cocoabackend` | `"darwin"` | `NSWindow() unsafe.Pointer` |
| Windows | `win32backend` | `"windows"` | `HWND() unsafe.Pointer` |
| X11 | `x11backend` | `"x11"` | `Conn() *xgb.Conn`, `Window() xproto.Window`, `Connected() bool` |
| Wayland | `waylandbackend` | `"wayland"` | `XdgToplevel() *xdg.Toplevel` |

#### X11 — connection and window

On X11 the top-level **window XID** is both the desktop window and the
drawable. Together with the **display connection**, that pair is the host
handle for extensions that need the live window.

| | |
|--|--|
| **Package** | `go.hasen.dev/shirei/x11backend` |
| **Type** | `x11backend.Context` |
| **`Platform()`** | `"x11"` |
| **Extra methods** | `Conn() *xgb.Conn`, `Window() xproto.Window`, `Connected() bool` |
| **Meaning** | Live XGB connection and main window XID (`nil` / `0` before the window exists) |
| **Ownership** | Backend-owned. Do not `Close` the connection or destroy the window; do not take over the event loop or present path. |

```go
//go:build linux

import (
    "go.hasen.dev/shirei"
    "go.hasen.dev/shirei/x11backend"
)

func useX11(ctx shirei.BackendContext) {
    c, ok := ctx.(x11backend.Context)
    if !ok || !c.Connected() {
        return
    }
    conn, win := c.Conn(), c.Window()
    _ = conn
    _ = win
}
```

#### Wayland — `xdg_toplevel`

On Wayland, drawing (`wl_surface`) and “being a desktop window” are separate.
**`xdg_toplevel`** is the shell role for the main app window: title, `app_id`
(taskbar / `.desktop` matching), maximize, fullscreen, close.

| | |
|--|--|
| **Package** | `go.hasen.dev/shirei/waylandbackend` |
| **Type** | `waylandbackend.Context` |
| **`Platform()`** | `"wayland"` |
| **Extra method** | `XdgToplevel() *xdg.Toplevel` (`go.hasen.dev/shirei/internal/wayland/xdg`) |
| **Meaning** | Live `xdg_toplevel` for the Shirei window (`nil` before the window exists) |
| **Ownership** | Backend-owned. Do not `Destroy` it or take over configure / commit / present of the main surface. |

Many Linux features (notifications, file chooser portals, V4L2 camera) use
D-Bus or device nodes and do not need this handle. It is available when an
extension needs the main window as the compositor sees it.

```go
//go:build linux

import (
    "go.hasen.dev/shirei"
    "go.hasen.dev/shirei/waylandbackend"
)

func useToplevel(ctx shirei.BackendContext) {
    c, ok := ctx.(waylandbackend.Context)
    if !ok {
        return
    }
    tl := c.XdgToplevel()
    if tl == nil {
        return
    }
    _ = tl
}
```

---

## 4. Using an extension from an app (camera)

Apps call a **portable** API. They do not use `EscapeHatchBackendContext`
unless they implement an extension themselves.

```go
import "go.hasen.dev/shirei/ext/camera"

var photo *image.RGBA

if Button(SymImage, "Take photo") {
    camera.TakePhoto(&photo, func(err error) {
        shirei.WithFrameLock(func() {
            if err == nil && photo != nil {
                shirei.UseImage("camera/last", photo)
            }
            shirei.RequestNextFrame()
        })
    })
}
```

Run the sample with mobilerun:

```sh
go run ./shirei/mobilerun -platform ios ./shirei/demos/mobile-camera
go run ./shirei/mobilerun -platform ios -device ./shirei/demos/mobile-camera
go run ./shirei/mobilerun -platform android ./shirei/demos/mobile-camera
```

---

## 5. Packaging: `shirei.capabilities`

Extensions declare **coarse** capability tokens. When you build with mobilerun,
those tokens become the platform declarations for that build.

### File format

Place next to the extension’s `go.mod` (module root):

```
shirei.capabilities
```

Example (`go.hasen.dev/shirei/ext/camera`):

```
# one token per line; # starts a comment
camera
photos
```

Rules:

1. **Known tokens** — prefer tokens listed in [§6](#6-capability-token-catalogue). Unknown tokens log a **warning** and are **ignored**; the build continues.
2. **One token per line** — no platform key names, no XML or JSON.
3. **Intent, not OS API names** — write `camera`, not `NSCameraUsageDescription`.
4. The file ships with the module like any other file (`go get` includes it).

The **app** module may also ship a `shirei.capabilities` file for features
implemented without a separate extension package.

### How mobilerun applies them

When building a `package main`:

1. Resolve module directories of that package’s dependency graph (`go list -deps`)
2. Read each module’s `shirei.capabilities` if present
3. Union tokens and inject into **this build’s** Info.plist / AndroidManifest

Only modules depended on by that main package are considered.

| Platform | What mobilerun injects |
|----------|-------------------------|
| **iOS** | Privacy purpose strings on the build’s Info.plist (default English text for development); optional supported orientations |
| **Android** | Extra `<uses-permission>` entries, related `<queries>` when needed (for example `IMAGE_CAPTURE` for `camera`), optional `screenOrientation` |

The Android host always includes baseline `INTERNET` so networked apps work
without a capabilities file. Development usage strings are generic; store
builds can replace them with product-specific copy in your release process.

---

## 6. Capability token catalogue

Meaning and platform mapping. Source of truth in the runner:
`mobilerun/capabilities.go`.

| Token | Meaning | iOS (Info.plist) | Android permission(s) |
|-------|---------|------------------|------------------------|
| `camera` | Capture with the camera | `NSCameraUsageDescription` | `CAMERA` |
| `microphone` | Microphone audio | `NSMicrophoneUsageDescription` | `RECORD_AUDIO` |
| `photos` | Read / pick photos | `NSPhotoLibraryUsageDescription` | `READ_MEDIA_IMAGES` |
| `photos-add` | Save into the photo library only | `NSPhotoLibraryAddUsageDescription` | *(usually none)* |
| `videos` | Read videos from storage | `NSPhotoLibraryUsageDescription` | `READ_MEDIA_VIDEO` |
| `location` | Location while in use | `NSLocationWhenInUseUsageDescription` | `ACCESS_COARSE_LOCATION`, `ACCESS_FINE_LOCATION` |
| `location-always` | Background location | when-in-use + `NSLocationAlwaysAndWhenInUseUsageDescription` | fine/coarse + `ACCESS_BACKGROUND_LOCATION` |
| `contacts` | Contacts | `NSContactsUsageDescription` | `READ_CONTACTS` |
| `calendar` | Calendar | `NSCalendarsUsageDescription` | `READ_CALENDAR` |
| `bluetooth` | Bluetooth | `NSBluetoothAlwaysUsageDescription` | `BLUETOOTH_CONNECT`, `BLUETOOTH_SCAN` |
| `local-network` | LAN discovery | `NSLocalNetworkUsageDescription` | *(none mapped)* |
| `notifications` | Post notifications | *(runtime permission on modern iOS)* | `POST_NOTIFICATIONS` |
| `internet` | Outbound network | *(none)* | `INTERNET` |
| `network-state` | Connectivity query | *(none)* | `ACCESS_NETWORK_STATE` |
| `vibrate` | Vibration | *(none)* | `VIBRATE` |
| `nfc` | NFC | `NFCReaderUsageDescription` | `NFC` |
| `motion` | Motion / activity | `NSMotionUsageDescription` | `ACTIVITY_RECOGNITION` |
| `biometrics` | Face ID prompt copy | `NSFaceIDUsageDescription` | *(none mapped)* |
| `speech` | Speech recognition | `NSSpeechRecognitionUsageDescription` | *(pair with `microphone` as needed)* |
| `tracking` | App Tracking Transparency | `NSUserTrackingUsageDescription` | *(none mapped)* |
| `orientation-landscape` | Launch / support landscape only | `UISupportedInterfaceOrientations` = landscape L+R (phone + iPad) | `android:screenOrientation="sensorLandscape"` |
| `orientation-portrait` | Launch / support portrait only | portrait (+ iPad upside-down) | `android:screenOrientation="sensorPortrait"` |

`orientation-landscape` and `orientation-portrait` should not both appear
in the resolved token set. If both are present (for example via different
modules), mobilerun logs a **warning** and continues; **which orientation
is applied is not guaranteed**. Prefer declaring only one. Apps that also
set `Host.PreferredOrientation` at runtime control in-session orientation
policy. If neither token is present, the default host template allows
portrait and landscape.

Some tokens only affect one OS; the other side is a no-op.

---

## 7. Writing a new extension

### Runtime package

1. Ship a Go module with a **portable** public API (no `BackendContext` in
   signatures apps call).
2. Inside the extension, read `shirei.GetHost().EscapeHatchBackendContext`,
   type-assert the platform context, and start OS work.
3. Deliver results asynchronously — do not block the frame on a system sheet.
4. Feed data into Shirei (for example `UseImage`) and wake the UI with
   `RequestNextFrame` (use `WithFrameLock` if off the frame thread).

### Packaging

1. Add `shirei.capabilities` at the module root with the minimal tokens.
2. Build under mobilerun and confirm the log shows your tokens
   (`— capabilities […]`).

### Host helpers

- **iOS:** present from `RootViewController()` on `iosbackend.Context` (UIKit
  in the extension’s own `.m` / cgo files).
- **Android:** use `androidbackend`’s bridges
  (`StartActivityForResult`, `RequestPermissions`, result registrations).
  Keep feature-specific Intent construction and result decoding in the
  extension.

### Documentation

- README in the extension module
- Link here for the shared mechanism

---

## 8. Reference: camera

Module: **`go.hasen.dev/shirei/ext/camera`**.

| Piece | Role |
|-------|------|
| Public API | `TakePhoto(dest **image.RGBA, done func(error))` |
| Host | `EscapeHatchBackendContext` (iOS root VC / Android host bridges) |
| iOS | Root VC → `UIImagePickerController` (all in the camera package) |
| Android | Generic host activity-result bridge + camera-owned Intents/JNI |
| Capabilities | `camera`, `photos` |
| Demo | `demos/mobile-camera` |

```sh
go run ./shirei/mobilerun -platform ios ./shirei/demos/mobile-camera
go run ./shirei/mobilerun -platform ios -device ./shirei/demos/mobile-camera
go run ./shirei/mobilerun -platform android ./shirei/demos/mobile-camera
```

**iOS:** Simulator without a camera falls back to the photo library. On a
device, the system asks for camera permission once.

**Android:** Runtime `CAMERA` permission, then the system capture activity.
Gallery is a fallback when no camera app is available. With the `camera`
token, mobilerun declares the `IMAGE_CAPTURE` package-visibility query.

---

## 9. Practical notes

- Use `EscapeHatchBackendContext` only in extension (or host) platform code,
  not in ordinary app UI.
- Prefer capability tokens from the catalogue; unknown tokens are warned and ignored.
- Only tokens from modules in the main package’s dependency graph are applied.
- Do not block a frame waiting for a system sheet to finish.
- Keep `BackendContext`, JNI, and view-controller pointers out of ordinary
  app UI code.

---

## 10. Development builds vs store releases

**Caveat:** mobilerun today only produces **development** builds (debug
signing, generic usage strings, debuggable packages). Support for release /
store bundles is not there yet.

When that lands, the **capability policy stays the same**: tokens from
`shirei.capabilities` on modules in the main package’s dependency graph are
unioned into the build, unknown tokens are warned and ignored, and only
those packaging declarations are injected. Review privacy labels and usage
copy for store listings, and avoid depending on extension modules you never
use (their tokens still apply if they are in the graph).

---

## Related

- [Running on iOS](ios.md)
- [Running on Android](android.md)
- [Camera extension](../ext/camera/README.md)
- Capability mapping: `mobilerun/capabilities.go`
