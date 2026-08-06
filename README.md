# Shirei

Shirei is a cross-platform GUI framework for Go. Application code describes
what the interface should look like right now using ordinary Go data and
control flow—not HTML and JavaScript.

The same codebase targets macOS, Windows, Linux, iOS, and Android with
Shirei-rendered controls. Mobile support is currently aimed at utility-style
apps; `shirei_mobilerun` handles fast development installs and
`shirei_bundle` produces signed IPAs and release APKs. See
[docs/android.md](docs/android.md), [docs/ios.md](docs/ios.md), and the
[bundler documentation](cmd/shirei_bundle/README.md). On Linux it is one of the
easiest ways to produce a self-contained GUI program that does *not* require
shared library dependencies.

※ "Shirei" is derived from the Japanese pronunciation of "Simple Layout":
シンプル・レイアウト → シレイ

![haystack](examples/haystack/haystack.webp)

## Motivation

Many GUI toolkits ask application code to create persistent widget objects and
keep them synchronized with application data. Shirei takes a plain-data,
declarative approach: view functions describe the current interface directly
from your structs, slices, strings, and booleans.

You do not create a button object and later update or remove it. You write the
button where it belongs:

```go
if Button(SymIPlus, "Increment") {
    count++
}
```

Ordinary `if` statements and loops control which interface elements exist.
Mutate the application data and the next requested update reflects it, without
a binding layer or an application-owned widget graph.

Shirei retains the framework state needed for identity, focus, scrolling, and
text editing. Application view functions build the current container tree when
an update is requested; Shirei then sizes, lays out, and renders it in deferred
passes. Nothing continuously rebuilds while the application is idle.

![process monitor](examples/process_monitor/process_monitor.webp)

## Features:

* Native: real executable programs, not web pages. Typical binary size ≈10MB.

* Plain-data, declarative UI: describe what the interface should look like
right now using ordinary Go values, loops, and conditionals. No widget
synchronization or reactive state system.

* Integrated framework: Shirei owns the window and update lifecycle and
provides layout, controls, robust text editing, virtual lists and tables, modal
and focus helpers, software rendering, and headless snapshots.

* Robust international text support: complex shaping, bidirectional layout,
access to system fonts, and IME support (input method editor) for East Asian
languages.

* Flexible layout and styling: one of the good things about the web is that you
have a lot of flexibility in how you arrange the UI; you're not limited to just a
standard set of widgets and containers. You can make your own. Default controls
split process vs paint so you can skin buttons, toggles, text fields, and
scrollbars without reimplementing hit-testing (see
[custom widgets](docs/custom-widgets-tutorial.md)).

* Deterministic full-interface testing: render the normal application frame to
an image without opening a native window or requiring a GPU.

* Ordinary Go background work: publish state from goroutines with
`WithFrameLock` and wake the interface with `RequestNextFrame`.

* App resources: put icons and data files in a `Resources/` directory next to
`package main`; `app.ResourcePath` finds them under `go run` and in desktop
release packages alike (see [App resources](docs/resources.md)).

* Easy to learn API, for both humans and AI agents. If you have ideas for small
programs you want to make but don't have the time for, try asking the latest AI
engines to use shirei to build it. You'll be surprised how well they can use it.

Several example programs under [`examples/`](examples/) — start with `haystack`
if you only look at one.

Shirei is young and especially effective for self-contained developer tools.
Its widget catalog, accessibility support, and native platform integration are
narrower than those of mature desktop toolkits.

## Getting started

Copy this into `main.go` in a new folder:

```go
package main

import (
	"fmt"

	app "go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

func main() {
	app.SetupWindow("My App", 300, 100)
	app.Run(RootView)
}

var count int

func RootView() {
	Container(Attrs(Viewport, Background(220, 10, 97, 1)), func() {
		Container(Attrs(Row, CrossMid, Pad(20), Gap(10)), func() {
			Label(fmt.Sprintf("Counter: %d", count))
			if Button(SymIPlus, "Increment") {
				count++
			}
		})
	})
}

```

Then type:

```
$ go mod init main
$ go mod tidy
$ go run .
```

## Tools

Install the companion CLIs (puts binaries on `$(go env GOPATH)/bin` — keep that
on your `PATH`):

```
go install go.hasen.dev/shirei/cmd/shirei_mobilerun@latest
go install go.hasen.dev/shirei/cmd/shirei_bundle@latest
```

| Command | Role |
|---------|------|
| **`shirei_mobilerun`** | Dev installs on iOS / Android (debug signing, fast iteration) |
| **`shirei_bundle`** | Release packaging (IPA, release APK, macOS zip/pkg, desktop archives) |

- Dev mobile setup: [docs/android.md](docs/android.md), [docs/ios.md](docs/ios.md)
- Bundle details: [cmd/shirei_bundle/README.md](cmd/shirei_bundle/README.md)
- Mobilerun details: [cmd/shirei_mobilerun/README.md](cmd/shirei_mobilerun/README.md)
- App resources (`Resources/` + `app.ResourcePath`): [docs/resources.md](docs/resources.md)

## Learn

- [Tutorial](docs/tutorial.md)
- [Layout shell (step-by-step)](docs/layout-tutorial.md)
- [Audio](docs/audio-tutorial.md)
- [App resources](docs/resources.md)
- [Virtual lists and Measure](docs/virtual-list.md)
- [Container identity](docs/identity.md)
- [Drag and drop](docs/drag-drop.md)
- [Running on Android](docs/android.md) (`shirei_mobilerun`)
- [Running on iOS](docs/ios.md) (`shirei_mobilerun`)
- [Mobile extensions / escape hatches](docs/mobile-extensions.md)
- [Release packaging (`shirei_bundle`)](cmd/shirei_bundle/README.md)
