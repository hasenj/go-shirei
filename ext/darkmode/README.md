# darkmode

`go.hasen.dev/shirei/ext/darkmode` provides real-time detection of the host operating system's dark mode preference across desktop, mobile, and web platforms.

## Installation

```bash
go get go.hasen.dev/shirei/ext/darkmode
```

## API

```go
package darkmode

// OSDarkMode reports whether the host operating system is currently in dark mode.
//
// Fast and safe to call every frame (sub-nanosecond atomic read). On first call,
// it inspects the system theme and registers an OS-level notification observer
// so that changes made by the user or scheduled by the OS update automatically
// and request a new frame.
func OSDarkMode() bool
```

## Usage

```go
package main

import (
	"go.hasen.dev/shirei/app"
	"go.hasen.dev/shirei/ext/darkmode"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

func main() {
    app.SetupWindow("Dark Mode Demo", 600, 400)
    SetLightColorScheme(WarmColorScheme())
    SetDarkColorScheme(WarmDarkColorScheme())
    app.Run(RootView)
}

func RootView() {
    SetDarkMode(darkmode.OSDarkMode())
    ModAttrs(UseSurface(SurfaceCanvas), Pad(24), Gap(16))
    Label("System appearance", FontSize(20), FontWeight(WeightBold))
    Button(NoIcon, "Themed button")
}

```

Applications own manual overrides and their follow-system preference. For a
manual choice, pass that choice to `widgets.SetDarkMode` instead of the OS
query. The widget package only stores the preferred pair and active mode.
The [appearance tutorial](../../docs/appearance-tutorial.md) covers both
behaviors, application surfaces, and widget styling. The
[dark-mode probe](../../demos/darkmode-probe/main.go) demonstrates live changes.

## Supported Platforms

| Platform | Detection Mechanism | Reactive Updates |
|---|---|---|
| **macOS** | `NSApp.effectiveAppearance` / `NSUserDefaults` | `NSDistributedNotificationCenter` (`AppleInterfaceThemeChangedNotification`) |
| **Windows** | Registry `AppsUseLightTheme` | `RegNotifyChangeKeyValue` background watcher |
| **Linux** | FreeDesktop XDG Settings Portal (`org.freedesktop.appearance color-scheme`) | D-Bus signal (`SettingChanged`) via `godbus` |
| **Web / WASM** | `window.matchMedia('(prefers-color-scheme: dark)')` | `MediaQueryList` change event listener |
| **iOS** | `UITraitCollection.userInterfaceStyle` | Trait collection observer |
| **Android** | `AConfiguration_getUiModeNight` | NDK configuration query |
| **Other / Stub** | Fallback `false` | Safe no-op |
