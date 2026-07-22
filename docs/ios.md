# Running Shirei apps on iOS

Shirei is a Go GUI framework with its own software renderer, and an iOS
app built with it is an ordinary Go program — the same `package main` that
runs on the desktop. You do not write Swift or Objective-C for the UI: the
`mobilerun` tool cross-compiles your package, links a thin UIKit host, and
installs the result on the Simulator or a USB iPhone.

**macOS only.** Building for iOS requires Apple’s toolchain (Xcode). Linux
and Windows hosts cannot produce or run iOS apps with this path.

**Development only.** `mobilerun` is for iterating on the Simulator or a
developer-signed device (ad-hoc ids, free Personal Team or your Apple
Development cert). It is **not** a production or App Store packaging
pipeline — TestFlight, distribution certificates, and store submission are
out of scope for this release.

This document covers setting up the environment and getting a first app
onto the Simulator or a phone. For Shirei itself, start with
[tutorial.md](tutorial.md). Android: [android.md](android.md).

Camera and other OS features (escape hatches, `shirei.capabilities`):
[mobile-extensions.md](mobile-extensions.md).

---

## 1. What you need

| Requirement | Why |
|---|---|
| **macOS** host | Xcode, `xcrun`, `simctl` / `devicectl`, and the iOS SDKs are Apple-only |
| Go 1.24+ | building the app (and `mobilerun` itself); must be on your `PATH` |
| **Xcode** (full app from the Mac App Store, not only Command Line Tools) | iOS SDK, Simulator, `xcodebuild`, signing, `simctl` / `devicectl` |
| An iOS Simulator runtime and/or a physical iPhone/iPad | something to launch onto |
| Apple ID (for a **physical device** only) | free Personal Team + Apple Development certificate via Xcode |

Simulator builds use ad-hoc signing and do **not** need an Apple Developer
Program membership. Device builds need at least a free Apple ID signed into
Xcode (Personal Team). A paid Apple Developer Program account works too, but
is not required for local USB installs.

Rough disk: Xcode itself is large (tens of GB with platforms). Shirei’s own
build scratch is small by comparison.

---

## 2. Install Xcode and Go

1. Install **Xcode** from the Mac App Store and open it once so it finishes
   installing components.
2. Install the **command-line tools** and select the Xcode app as the active
   developer directory:

   ```sh
   xcode-select --install   # if prompted / if missing
   sudo xcode-select -s /Applications/Xcode.app/Contents/Developer
   sudo xcodebuild -license accept   # once, if not already accepted
   ```

3. Confirm:

   ```sh
   xcrun --version
   xcodebuild -version
   go version   # 1.24+
   ```

4. Install an **iOS Simulator** runtime if you do not already have one:
   **Xcode → Settings → Platforms** (or *Components* on older Xcode) →
   download an iOS version. Without a runtime, Simulator launch fails.

---

## 3. Apple ID and signing (physical device only)

Skip this section if you only use the **Simulator**.

### 3.1 Sign in to Xcode

1. **Xcode → Settings → Accounts**
2. **+** → **Apple ID** → sign in with any Apple ID (free is fine)
3. Select the account → your **Team** (often *Your Name (Personal Team)*)
4. Note the **Team ID** (10-character alphanumeric). `mobilerun` can often
   read it automatically from Xcode; otherwise paste it into the GUI’s
   **Apple Team** field or pass `-team`.

### 3.2 Create an Apple Development certificate

Automatic Signing usually creates this on the first successful device
build, but doing it once in Xcode avoids confusing first-run failures:

1. **Xcode → Settings → Accounts** → select your Apple ID  
2. Select the team → **Manage Certificates…**  
3. **+** → **Apple Development**

That installs a certificate and private key in your login keychain. The
first device build may still prompt for **keychain access** — allow it.

Xcode’s Automatic Signing (what `mobilerun` uses) will also create or
refresh a **provisioning profile** that includes your phone’s UDID.

### 3.3 Free Personal Team limits

A free Personal Team can install only a **small number of apps** at once
(historically about three). If install fails with a signing / profile
error, delete an old development app from the phone or **reuse the same
bundle id** for the app you are iterating on.

A paid Apple Developer Program membership raises those limits and is what
you would use for TestFlight / App Store later — not required for this
dev loop.

---

## 4. Device setup (physical iPhone / iPad)

1. Unlock the phone and connect it with a **data-capable** USB cable.
2. Tap **Trust** when asked to trust this computer.
3. On iOS 16+: **Settings → Privacy & Security → Developer Mode** → enable
   (reboot if prompted). Without this, developer installs are blocked.
4. Confirm the Mac sees the device:

   ```sh
   xcrun devicectl list devices
   # or
   xcrun xctrace list devices
   ```

   You want a connected iPhone/iPad listed as available.

---

## 5. Build and run

From a Shirei checkout (directory that contains `mobilerun/` and `go.mod`):

```sh
# GUI: platform = iOS, package picker, team, log
go run ./mobilerun

# Simulator (CLI)
go run ./mobilerun -platform ios demos/theme

# Physical device (CLI) — Team ID required
go run ./mobilerun -platform ios -device -team YOURTEAMID demos/theme
```

In the GUI: set **Platform** to **iOS**, choose **Simulator** or **Device**,
pick a package, set identity (App ID / name / icon) if you want, then
**Run**. Empty App ID uses `{prefix}.<folder>`; empty icon uses
`<package>/icon.png` when present.

First build for a new GOOS/arch takes a while (Shirei + Go compile); later
builds are much faster. First **device** build can take longer while Xcode
creates a cert/profile.

Useful flags:

| Flag | |
|---|---|
| `-platform ios` | required for CLI |
| `-device` / `-sim` | physical device or Simulator (default sim) |
| `-team ID` | `DEVELOPMENT_TEAM` (required for `-device`) |
| `-id`, `-name` | bundle id and display name |
| `-icon path` | home-screen icon image |

There is also a lower-level script (`./ios-run.sh` in the shirei root, or
the copy embedded under `mobilerun/embed/`) with the same behavior and
environment variables (`SHIREI_IOS_TEAM`, `SHIREI_IOS_BUNDLE_ID`, …). Prefer
`mobilerun` for day-to-day use.

---

## 6. What works

- Rendering via the core software renderer at the device’s resolution and
  scale
- Touch: multi-contact `InputState.Touches` plus primary-finger synthesis to
  mouse/scroll with fling (same model as Android; see the main tutorial §6)
- Soft keyboard via `UITextInput`, basic IME composition, accessory bar
  (arrows / select all / copy / paste / done)
- System clipboard
- Audio (`app.StartAudio`) via AudioQueue
- Networking (`net/http` and friends) as on desktop
- Content area shrinks for the keyboard and orientation changes
- Optional home-screen icon from a PNG/JPEG (via actool)

Your app needs no iOS-specific code for the basic path:

```go
app.SetupWindow("My App", 540, 640)
app.Run(RootView)
```

(The requested window size is not a phone window chrome size; the app runs
full screen in the content area.)

Not a production path: App Store / TestFlight packaging, distribution
certs, full adaptive assets, advanced IME polish, etc.

---

## 7. Troubleshooting

| Symptom | What to try |
|---|---|
| `xcrun not found` | Install Xcode; `xcode-select -s /Applications/Xcode.app/Contents/Developer` |
| No Simulator device | Xcode → Settings → Platforms → install an iOS runtime |
| No connected phone | Unlock, Trust, data cable; `xcrun devicectl list devices` |
| Team ID required | Device builds need `-team` or GUI team; Simulator does not |
| `xcodebuild` / signing failure | Manage Certificates → Apple Development; allow keychain access; unlock phone |
| Free team app limit | Delete old dev apps or reuse the same App ID / bundle id |
| Developer Mode | iOS 16+ Settings → Privacy & Security → Developer Mode |
| Install error after icon | Re-run; device re-sign uses the Apple Development identity |

---

## 8. How it works, briefly

`mobilerun` (via the embedded `ios-run.sh`) builds your package with
`-buildmode=c-archive` against the iOS or Simulator SDK, then links a small
UIKit host (the one Objective-C file in the system) into an `.app` bundle.
Simulator builds are ad-hoc signed and installed with `simctl`. Device
builds use **Xcode Automatic Signing** (`CODE_SIGN_STYLE=Automatic`,
`CODE_SIGN_IDENTITY=Apple Development`) with your team id, then
`devicectl` to install and launch. The host template is embedded in the
`mobilerun` binary and extracted to the user cache — no monorepo layout is
required to run a package that imports Shirei.
