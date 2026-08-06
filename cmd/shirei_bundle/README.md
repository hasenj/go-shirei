# shirei_bundle — release packaging for Shirei apps

Build **release** artifacts for a Shirei `package main`: signed IPA, release
APK, macOS app/zip/pkg, Linux tarballs, Windows zips.

This is the release counterpart of **`shirei_mobilerun`**, which is for
development installs only (debug signing, fast device/simulator iteration).
See [docs/android.md](../../docs/android.md) and
[docs/ios.md](../../docs/ios.md).

`shirei_bundle` writes packages **on disk**. Store listing metadata, TestFlight /
Play Console upload, and Android App Bundles (AAB) are outside this tool —
upload IPAs/pkgs with Apple’s Transporter (or equivalent), and APKs via
sideload or Play Console when you use APK tracks.

## Install

```bash
go install go.hasen.dev/shirei/cmd/shirei_bundle@latest
```

Ensure `$(go env GOPATH)/bin` (or `GOBIN`) is on your `PATH`, then:

```bash
shirei_bundle
```

From a local checkout of this module:

```bash
go install ./cmd/shirei_bundle
# or without installing:
go run ./cmd/shirei_bundle
```

With no package path argument, the **GUI** opens. Pass a package path and
`-platform` for a one-shot **CLI** build (iOS, Android, macOS).

---

## Artifacts

| Platform | Output |
|----------|--------|
| **iOS** | Signed **`.ipa`** |
| **Android** | **Release `.apk`** (`debuggable=false`, target SDK 34, your keystore) |
| **macOS** | Self-dist **`.app` + zip** (Developer ID; optional notarize) and/or App Store **`.pkg`** |
| **Linux** | **`.tar.gz`** per arch (binary + `.desktop` with `Terminal=false` + icon) |
| **Windows** | **`.zip`** per arch (GUI-subsystem `.exe` + icon; no console window) |

Default release layout is under each app’s configured **Release directory**
(often `releases/`), namespaced by product / version / platform.

---

## App resources (`Resources/`)

Shirei’s resource convention is a **runtime** feature: put assets in
`<package>/Resources/` next to `package main`, and load them with
`app.ResourcePath(...)` / `app.ResourcesDir()`. The same calls work under
`go run` and inside a release package. Full details:
[App resources](../../docs/resources.md).

On **desktop** builds, when that directory exists, `shirei_bundle` copies its
contents into the platform resource root (no config field — presence is enough):

| Platform | Destination |
|----------|-------------|
| macOS | `App.app/Contents/Resources/` |
| Linux | `<exeDir>/Resources/` in the tarball |
| Windows | `<exeDir>/Resources/` in the zip |

App code should not open those packaged paths directly; always go through
`app.ResourcePath`. Mobile resource packaging is not covered here yet.

---

## GUI workflow

1. **Add application** — path to the Go package, display name, reverse-DNS App
   ID prefix (or full App ID), icon path (defaults to `Resources/icon.png` when
   present, else `icon.png` beside the package).
2. **Enable platforms** and fill identity / signing for each:
   - **iOS:** Team ID, export method, optional codesign identity pin, release dir.
   - **Android:** keystore path, key alias, arch (`arm64` / `arm`), release dir.
     Passwords are entered when you Bundle; they are **never** stored.
   - **macOS:** self-dist and/or App Store, archs (arm64 / amd64 → universal when
     both), identities, category (App Store), provisioning profile, notary profile.
   - **Linux / Windows:** arch multi-select, release dir; optional identity overrides.
3. Open a **marketing version** (e.g. `1.0.0`). **Bundle** each platform; build
   numbers increment per platform. **New version** freezes the previous one (no
   further builds on frozen versions).
4. After a successful build: **Reveal** artifacts, **Re-bundle** if needed.
   Android can **Install** over adb. macOS self-dist can **Notarize** when a
   notarytool profile name is set.

Per-platform fields can override shared name / App ID / icon when a store
requires a different identity on one OS.

### Config location

| File | Contents |
|------|----------|
| `…/shirei/bundle.json` | Saved apps and platform settings (`UserConfigDir`) |
| `…/shirei/bundle-releases.json` | Version history and artifact paths |

---

## Host requirements

| Target | Host |
|--------|------|
| iOS | **macOS** + full **Xcode**; team and certificates for the export method |
| Android | **JDK** + Android **SDK** (NDK, build-tools, platform) — same base as [android.md](../../docs/android.md) — plus a **release keystore** you create and keep safe |
| macOS app | **macOS** + codesign identities; notarize needs a **notarytool** keychain profile |
| Linux / Windows packages | Go that can cross-compile; the target OS is not required as the host |

iOS and macOS packaging run only on Darwin hosts.

---

## CLI

CLI supports **ios**, **android**, and **macos**. Linux and Windows use the GUI.

Common flags: `-platform`, `-id`, `-name`, `-version`, `-build`, `-o` (output),
`-icon`, `-identity` (codesign pin where applicable).

### iOS

```bash
shirei_bundle -platform ios \
  -team YOURTEAMID \
  -id com.example.myapp \
  -name "My App" \
  -version 0.1.0 -build 1 \
  -method app-store-connect \
  ./path/to/package
```

| Flag | Role |
|------|------|
| `-team` | Apple Team ID (required) |
| `-method` | Export method: `debugging`, `development`, `app-store-connect`, `ad-hoc`, `enterprise` (default `debugging`) |
| `-identity` | Optional codesign identity; empty = auto (Distribution / Development by method) |

Prints the IPA path on success.

### Android

```bash
SHIREI_ANDROID_KS_PASS='…' \
SHIREI_ANDROID_KEY_PASS='…' \   # optional; defaults to keystore password
shirei_bundle -platform android \
  -keystore ~/keys/release.jks \
  -key-alias myalias \
  -id com.example.myapp \
  -version 0.1.0 -build 1 \
  -arch arm64 \
  ./path/to/package
```

| Flag / env | Role |
|------------|------|
| `-keystore`, `-key-alias` | Signing keystore |
| `SHIREI_ANDROID_KS_PASS` | Keystore password (**required** on CLI) |
| `SHIREI_ANDROID_KEY_PASS` | Key password if different from the store |
| `-arch` | `arm64` (default) or `arm` |
| `-build` | Integer **versionCode** (≥ 1) |

Prints the APK path on success.

### macOS

CLI builds **self-distribution** (Developer ID `.app` + zip). App Store `.pkg`
and multi-mode options are in the GUI.

```bash
shirei_bundle -platform macos \
  -id com.example.myapp \
  -version 0.1.0 -build 1 \
  -arch universal \
  -notary-profile YOUR_NOTARY_PROFILE \
  ./path/to/package
```

| Flag | Role |
|------|------|
| `-arch` | `arm64`, `amd64`, or `universal` / `both` / `all` |
| `-identity` | Developer ID Application pin (empty = auto) |
| `-notary-profile` | After build, notarize with this notarytool keychain profile |

Prints the primary artifact path (zip after notarize when profile is set).

---

## After the build

| Goal | What to do |
|------|------------|
| iOS TestFlight / App Store | Upload the IPA with **Transporter** / App Store Connect |
| Android sideload / internal | Install the APK (`adb` from the GUI, or manually) |
| Google Play (new apps) | Console usually expects **AAB**; this tool ships **APK** today |
| macOS direct download | Ship the notarized zip |
| Mac App Store | Upload the **`.pkg`** with Transporter |
| Linux / Windows | Ship the archives; no store automation |

---

## Related

- [App resources](../../docs/resources.md) (`Resources/` + `app.ResourcePath`)
- Parent overview: [shirei README](../../README.md)
- Dev mobile installs: [android.md](../../docs/android.md), [ios.md](../../docs/ios.md)
- Capability tokens (privacy / sensors) still apply to release graphs:
  [mobile-extensions.md](../../docs/mobile-extensions.md)
