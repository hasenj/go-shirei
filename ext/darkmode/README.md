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
	app.Run(RootView)
}

func RootView() {
	isDark := darkmode.OSDarkMode()

	bgH, bgS, bgL := float32(220), float32(12), float32(96)
	txtH, txtS, txtL := float32(220), float32(20), float32(18)
	status := "Light Mode"

	if isDark {
		bgH, bgS, bgL = 225, 14, 12
		txtH, txtS, txtL = 0, 0, 96
		status = "Dark Mode"
	}

	Container(Attrs(Viewport, Expand, Background(bgH, bgS, bgL, 1), Pad(24), Gap(16)), func() {
		Label("System Theme: " + status, FontSize(20), FontWeight(WeightBold), TextColor(txtH, txtS, txtL, 1))
	})
}
```

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
