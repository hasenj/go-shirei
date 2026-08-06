# shirei_mobilerun — development installs for iOS and Android

Build and launch a Shirei `package main` on a simulator, emulator, or USB
device while iterating. Android uses a **debug** keystore and debuggable APKs;
iOS uses simulator or development signing.

For **release** IPAs and APKs, use **`shirei_bundle`**:
[../shirei_bundle/README.md](../shirei_bundle/README.md).

## Install

```bash
go install go.hasen.dev/shirei/cmd/shirei_mobilerun@latest
```

Put `$(go env GOPATH)/bin` (or `GOBIN`) on your `PATH`, then:

```bash
shirei_mobilerun
```

From a local checkout of this module:

```bash
go install ./cmd/shirei_mobilerun
# or:
go run ./cmd/shirei_mobilerun
```

## Host setup

Follow the full environment guides:

- [Android](../../docs/android.md) — JDK, SDK, NDK, `adb`
- [iOS](../../docs/ios.md) — macOS + Xcode (Simulator and/or device)

## Usage

### GUI

```bash
shirei_mobilerun
```

Pick platform, package, optional id/name/icon, then run.

### CLI

```bash
# Android device / emulator
shirei_mobilerun -platform android ./path/to/package

# Build APK only (no install)
shirei_mobilerun -platform android -build-only ./path/to/package

# iOS Simulator
shirei_mobilerun -platform ios ./path/to/package

# Physical iPhone (Team ID required)
shirei_mobilerun -platform ios -device -team YOURTEAMID ./path/to/package
```

Preferences: `<UserConfigDir>/shirei/mobile-run.json`.

## Related

- [docs/android.md](../../docs/android.md)
- [docs/ios.md](../../docs/ios.md)
- [docs/mobile-extensions.md](../../docs/mobile-extensions.md) — camera and capability tokens
- [shirei_bundle](../shirei_bundle/README.md) — release packaging
