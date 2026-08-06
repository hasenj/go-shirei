# Running Shirei apps on Android

Shirei is a Go GUI framework with its own software renderer, and an Android
app built with it is an ordinary Go program — the same `package main` that
runs on the desktop. There is no Android Studio, no Gradle, no Kotlin, and
you never write Java: the `shirei_mobilerun` tool cross-compiles your package with
the NDK and assembles a ready-to-install APK from it.

**Development and release.** `shirei_mobilerun` iterates on a connected device
or emulator with debug signing, debuggable APKs, and ad-hoc app ids. For release
APKs using your keystore with `debuggable=false`, use **`shirei_bundle`**
([cmd/shirei_bundle/README.md](../cmd/shirei_bundle/README.md)).

Install the tool:

```sh
go install go.hasen.dev/shirei/cmd/shirei_mobilerun@latest
```

Ensure `$(go env GOPATH)/bin` is on your `PATH`.

This document covers setting up the development environment and getting a
first app onto a device. For Shirei itself, start with
[tutorial.md](tutorial.md). iOS: [ios.md](ios.md).

Camera and other OS features (escape hatches, `shirei.capabilities`):
[mobile-extensions.md](mobile-extensions.md).

---

## 1. What you need

| Requirement | Why |
|---|---|
| macOS, Linux, or Windows host | `shirei_mobilerun` has host support for all three |
| Go 1.24+ | building the app (and `shirei_mobilerun` itself); must be on your `PATH` |
| A JDK 17+ (OpenJDK / Temurin) | `sdkmanager`, `apksigner`, `keytool`, `javac`, and `d8` are Java programs — you still never write Java. `javac` and `keytool` must be on your `PATH` |
| Android command-line tools | provides `sdkmanager`, which installs everything below |
| SDK packages (via `sdkmanager`) | `platform-tools` (adb), an `ndk`, a `build-tools`, and one `platforms;android-NN` |
| An Android device (or emulator) | anything `adb` can talk to; Android 5.0+ (the APKs declare minSdk 21) |

Roughly 3 GB of disk for the SDK pieces, most of it the NDK. A network
connection is required for the `sdkmanager --install` step.

`shirei_mobilerun` finds the SDK via `$ANDROID_HOME` / `$ANDROID_SDK_ROOT`, or the
per-platform default locations below — install to those and there is nothing
to configure.

---

## 2. Install: host-specific setup

Do **one** of the platform sections below, then continue with
[§3 Common SDK packages](#3-common-sdk-packages-all-hosts).

### macOS (Homebrew)

```sh
brew install openjdk
# openjdk is keg-only — put it on PATH for this shell (and add to ~/.zshrc if you like):
export PATH="$(brew --prefix)/opt/openjdk/bin:$PATH"

brew install --cask android-commandlinetools android-platform-tools
```

Check:

```sh
java -version
javac -version
which sdkmanager adb
```

The command-line-tools cask lands under
`/opt/homebrew/share/android-commandlinetools` (Apple Silicon) or
`/usr/local/share/android-commandlinetools` (Intel). `shirei_mobilerun` also
searches `~/Library/Android/sdk` (Android Studio’s default).

Continue with [§3](#3-common-sdk-packages-all-hosts). Use the `sdkmanager` that
`which` found (Homebrew links it onto your PATH).

### Linux

**Host CPU must be x86_64 (amd64).** Google’s Linux packages for `adb`,
`aapt2`, and the NDK ship **x86_64 host binaries only** under
`toolchains/llvm/prebuilt/linux-x86_64/`. On **arm64 / aarch64** Linux
(common for Ubuntu VMs under Parallels on Apple Silicon) you get
`cannot execute binary file: Exec format error` for both `adb` and the NDK
clang. There is no supported official NDK host build for Linux arm64.

| Your setup | What to do |
|---|---|
| Linux **x86_64** | Follow this section as written |
| Linux **arm64** (Parallels ARM VM, etc.) | Use a **x86_64** Linux VM, or build/run `shirei_mobilerun` on the **macOS host** (Apple Silicon is fine — the Darwin NDK prebuilts are universal) |
| Check | `uname -m` → should print `x86_64` for this guide |

1. **JDK** from your distro, for example:

   ```sh
   # Debian / Ubuntu
   sudo apt update
   sudo apt install -y openjdk-21-jdk unzip wget

   # Fedora
   # sudo dnf install java-21-openjdk-devel unzip wget

   # Arch
   # sudo pacman -S jdk21-openjdk unzip wget
   ```

2. **Command-line tools only** from
   [developer.android.com/studio](https://developer.android.com/studio)
   (scroll to *Command line tools only* → Linux zip — these are for **x86_64
   hosts**). Unpack into the layout `sdkmanager` expects — note the doubled
   `cmdline-tools` segment and the final `latest` directory name:

   ```sh
   mkdir -p ~/Android/Sdk/cmdline-tools
   # Adjust the zip filename to whatever you downloaded.
   unzip commandlinetools-linux-*_latest.zip -d ~/Android/Sdk/cmdline-tools
   # Zip contains a top-level "cmdline-tools/" folder → rename it to "latest":
   mv ~/Android/Sdk/cmdline-tools/cmdline-tools ~/Android/Sdk/cmdline-tools/latest
   ```

3. Put tools on your PATH (and optionally set `ANDROID_HOME`):

   ```sh
   export ANDROID_HOME="$HOME/Android/Sdk"
   export PATH="$ANDROID_HOME/cmdline-tools/latest/bin:$ANDROID_HOME/platform-tools:$PATH"
   # Persist in ~/.bashrc / ~/.zshrc if you want this permanent.
   ```

4. **USB devices:** without udev rules, `adb devices` often shows
   `no permissions`. On Ubuntu/Debian:

   ```sh
   sudo apt install -y android-sdk-platform-tools-common
   # or: android-udev (some distros) / package "android-udev-rules"
   # Log out and back in (or replug the phone) so group membership applies.
   ```

   Note: the distro `adb` package may be arm64-native (and can talk to a phone),
   but the **NDK** from `sdkmanager` still will not run on arm64 Linux. Prefer
   a x86_64 host for the full `shirei_mobilerun` path.

`shirei_mobilerun` searches `~/Android/Sdk` and `/usr/lib/android-sdk` when
`ANDROID_HOME` is unset.

Continue with [§3](#3-common-sdk-packages-all-hosts). Prefer the full path if
you have not added `cmdline-tools/latest/bin` to PATH:

```sh
~/Android/Sdk/cmdline-tools/latest/bin/sdkmanager --version
```

### Windows

1. **JDK** (PowerShell as a normal user is fine):

   ```powershell
   winget install EclipseAdoptium.Temurin.21.JDK
   ```

   Open a **new** terminal so `java` / `javac` / `keytool` are on PATH. Check:

   ```powershell
   java -version
   javac -version
   keytool -help
   ```

2. **Go** if you do not already have it (`winget install GoLang.Go` or the
   installer from [go.dev](https://go.dev/dl/)). Confirm `go version` prints
   1.24+.

3. **Command-line tools only** [developer.android.com/studio](https://developer.android.com/studio)
   (scroll to *Command line tools only* → Windows zip). Unpack so `sdkmanager.bat` lives at:

   ```%LOCALAPPDATA%\Android\Sdk\cmdline-tools\latest\bin\sdkmanager.bat```

   Example (adjust the zip path):

   ```powershell
   $sdk = "$env:LOCALAPPDATA\Android\Sdk"
   New-Item -ItemType Directory -Force -Path "$sdk\cmdline-tools" | Out-Null
   Expand-Archive -Path .\commandlinetools-win-*_latest.zip -DestinationPath "$sdk\cmdline-tools\tmp"
   # Zip root is "cmdline-tools\..." → become "...\cmdline-tools\latest"
   Move-Item "$sdk\cmdline-tools\tmp\cmdline-tools" "$sdk\cmdline-tools\latest"
   Remove-Item "$sdk\cmdline-tools\tmp" -Recurse -Force
   ```

4. Optional: set env vars for this session (and permanently via System
   Properties → Environment Variables if you like):

   ```powershell
   $env:ANDROID_HOME = "$env:LOCALAPPDATA\Android\Sdk"
   $env:Path = "$env:ANDROID_HOME\cmdline-tools\latest\bin;$env:ANDROID_HOME\platform-tools;$env:Path"
   ```

`shirei_mobilerun` searches `%LOCALAPPDATA%\Android\Sdk` when `ANDROID_HOME` is
unset.

If `adb devices` cannot see a physical phone, after §3 also install the
Google USB driver:

```powershell
sdkmanager --install "extras;google;usb_driver"
```

(or the phone vendor’s driver). Then re-plug the device.

Continue with [§3](#3-common-sdk-packages-all-hosts). Call `sdkmanager.bat` if
it is not on PATH:

```powershell
& "$env:LOCALAPPDATA\Android\Sdk\cmdline-tools\latest\bin\sdkmanager.bat" --version
```

---

## 3. Common SDK packages (all hosts)

Once `sdkmanager` runs, install the pieces `shirei_mobilerun` needs. Exact versions
are **not** load-bearing: `shirei_mobilerun` picks the newest NDK, build-tools, and
platform it finds. The versions below are a known-good set:

```sh
sdkmanager --licenses     # accept (scroll / type y as prompted)
sdkmanager --install "platform-tools" "ndk;27.2.12479018" "build-tools;35.0.0" "platforms;android-35"
```

On Windows PowerShell the same package names work; if `sdkmanager` is not on
PATH, invoke `sdkmanager.bat` with the full path as in §2.

Sanity check:

```sh
adb version
# NDK / build-tools dirs should exist under the SDK root, e.g.:
#   $ANDROID_HOME/ndk/...
#   $ANDROID_HOME/build-tools/...
#   $ANDROID_HOME/platforms/...
#   $ANDROID_HOME/platform-tools/adb
```

---

## 4. Device setup

1. On the phone: Settings → About → tap **Build number** seven times to
   unlock Developer options, then enable **USB debugging**.
2. Plug it in and run `adb devices`. Accept the authorization prompt on the
   phone the first time. You want a line like `XXXXXXXX    device` (not
   `unauthorized` or `no permissions`).
3. If the device list stays empty: pull down the phone's USB notification
   and switch the connection to **File Transfer** — and be suspicious of
   cables and USB adapters; charge-only ones are a classic trap that
   enumerates the phone without exposing adb.
4. On Linux, revisit the udev note in §2 if you see `no permissions`.

An emulator works exactly the same way — if `adb devices` lists it,
`shirei_mobilerun` can target it. (Creating an AVD is outside this doc; Android
Studio or `avdmanager` both work.)

---

## 5. Build and run

Point `shirei_mobilerun` at any `package main` that uses
`go.hasen.dev/shirei/app` (path relative to your cwd, or absolute):

```sh
# CLI: build, adb install, launch
shirei_mobilerun -platform android ./demos/theme

# APK only (no device required)
shirei_mobilerun -platform android -build-only ./demos/theme

# GUI: device picker, package scan, log panel
shirei_mobilerun
```

From a local checkout of the shirei module you can also
`go run ./cmd/shirei_mobilerun` instead of installing.

First build takes a couple of minutes (all of Shirei compiles for
`GOOS=android`); after that it is seconds.

Useful CLI flags:

| Flag | |
|---|---|
| `-platform android` | required for CLI builds (GUI picks platform in the window) |
| `-build-only` | produce the APK without installing |
| `-screencap out.png` | screenshot the device a moment after launch — good for scripted verification |
| `-logcat` | stream the app's filtered log output after launch |
| `-serial S` | pick a device when several are connected |
| `-arch arm` | 32-bit ARM build (default is arm64) |
| `-id`, `-label` | application id (default `dev.shirei.<folder>`) and launcher label |
| `-icon path` | launcher icon image (default `<package>/icon.png` when present) |

Build products land in `bin/android/<app>/` at the workspace root (`go.work`
root if present, else `go.mod` root); the installable artifact is
`<app>.apk`. Signing uses the standard Android **debug** keystore at
`~/.android/debug.keystore` (`%USERPROFILE%\.android\debug.keystore` on
Windows), generated automatically if absent — fine for local install, not
for shipping.

Your app needs no Android-specific code. The usual

```go
app.SetupWindow("My App", 540, 640)
app.Run(RootView)
```

runs full-screen (the requested size is ignored), and `app.StartAudio`
works the same as on desktop.

### Cold checklist (good for a first Linux/Windows pass)

Use this to verify the instructions on a clean host:

1. `go version` → 1.24+
2. `java -version` / `javac -version` / `keytool` work
3. `sdkmanager --version` (or `sdkmanager.bat`)
4. §3 packages installed; `adb version` works
5. `shirei_mobilerun -platform android -build-only ./demos/theme` → APK under `bin/android/theme/`
6. With a device: `adb devices` shows `device`, then
   `shirei_mobilerun -platform android ./demos/theme` (or the GUI with no args)

If step 5 fails, the log line above the error usually names the missing tool
(`NDK not found`, `javac`, `aapt2`, …).

---

## 6. What works

- Rendering via the core software renderer, at the device's native
  resolution and density scale.
- Touch: every finger fills `InputState.Touches` (multi-contact data + hit
  queries such as `IsTouched`); the primary finger also synthesizes mouse and
  scroll with fling so mouse-oriented UIs work unmodified. Built-in multi-finger
  gestures (pinch, etc.) are not part of core — apps read the contact table if
  they need them.
- Soft keyboard: appears when a text input has focus, with committed text,
  backspace, and basic IME composition rendered inline. An accessory bar
  above the keyboard provides arrows / select all / copy / paste / done.
- System clipboard, both directions.
- Audio output (`app.StartAudio`) via AAudio on Android 8.0+; on older
  devices it returns an error and the app runs silent.
- Networking: `net/http` and friends work as on desktop — the generated
  manifest declares the `INTERNET` permission (Android blocks all sockets
  without it), and DNS resolves through the system resolver.
- Hardware keyboards and `adb shell input text`.
- The window area follows the system bars and the keyboard (content is
  never hidden under them); rotation is handled as a resize.
- Launcher icons: pass `-icon` or ship `<package>/icon.png`; the APK gets
  `android:icon` via aapt2.

Not there yet: per-field keyboard types (password fields get the normal
keyboard), and advanced IME features (candidate-window positioning,
selection sync). The 32-bit build has a flag but little mileage.

---

## 7. Debugging

Go's stdout and stderr — including panics — are redirected to logcat under
the `shirei` tag:

```sh
adb logcat -s shirei:V threaded_app:V AndroidRuntime:E DEBUG:I
```

`threaded_app` is the NativeActivity glue's lifecycle chatter,
`AndroidRuntime`/`DEBUG` catch Java exceptions and native crashes.
`shirei_mobilerun -platform android -logcat …` starts exactly this filter for you.

For headless verification, `-screencap` (or `adb exec-out screencap -p`)
captures what is actually on the screen — Shirei's `--png` convention works
on-device too, in spirit: build, launch, screenshot, look.

---

## 8. How it works, briefly

`shirei_mobilerun` builds your package with `-buildmode=c-shared` using the NDK's
clang, producing one `.so` that contains your app, Shirei, and the Go
runtime. The APK wraps that library with an `android.app.NativeActivity`
subclass — the one Java class in the system, embedded in `shirei_mobilerun` and
compiled at build time — which exists to host the soft keyboard's
`InputConnection` and the clipboard. Rendering locks the `ANativeWindow`
buffer and rasterizes into it directly; there is no GL/Vulkan layer. The
manifest, resource table, alignment, and signature are produced with
`aapt2`, `zipalign`, and `apksigner` straight from the build-tools — which
is why Gradle never enters the picture.
