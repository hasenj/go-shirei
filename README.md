# Shirei

Shirei is a Cross-Platform GUI framework for Go. You get to write the UI using
Go, not HTML and Javascript.

Truely cross-platform: the same code base produces identical looking programs
for MacOS, Windows, and Linux. Also happens to be the easiest way to produce a
self-contained GUI program for Linux that does *not* require any dependencies.

※ "Shirei" is derived from the Japanese pronunciation of "Simple Layout":
シンプル・レイアウト → シレイ

![haystack](examples/haystack/haystack.webp)

## Motivation

Experience has shown us that an immediate mode API is the only sane way to
program GUI applications. Unfrotunately, there is not good library or framework
that just works. Some of them require you to implement your own backend, some of
them do not have a decent cross-platform story, some of them do not have proper
support for non-latin text.

What is it that matters for "immediate mode"? Is it that the UI renders everything
every frame? No. It's that you build the UI by describing what it should look
like everyframe, based only (or mostly) on the data.

This is why React won: no need to maintain UI widgets yourself, no need to keep
track of their states in order to update them. You just say "at this point in
time, I want a button here, and I want the label on it to say so and so, and when
it's clicked, I want to do this and that".

Did this button exist before? What happens to it when you no longer need it?

You *never* have to answer these questions. This is the best part about React,
and this is what "immediate mode" is all about.

It is no good if you have an API that just lets you "draw" things but you are
also responsible for maintaining the state of all the "things" that you "draw".

![process monitor](examples/process_monitor/process_monitor.webp)

## Features:

* Native: real executable programs, not web pages. Typical binary size ≈10MB.

* Immediate mode API in the true sense: you never need to maintain UI widgets
or sync your data with widget state.

* Works out of the box: not just a layout engine, but a full fledged framework
that you can start using right away without any boilerplate.

* Full support for international text: complex shaping, bidirectional layout,
access to system fonts, IME support (input method editor) for East Asian
langauges.

* Flexible layout and styling: one of the good things about the web is that you
have alot of flexibilty in how you arrange the UI; you're not limited to just a
standard set of widgets and containers. You can make your own.

* Easy to learn API, for both humans and AI agents. If you have ideas for small
programs you want to make but don't have the time for, try asking the latest AI
engines to use shirei to build it. You'll be surprised how well they can use it.

Several example programs under [`examples/`](examples/) — start with `haystack`
if you only look at one.

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

## Learn

- [Tutorial](docs/tutorial.md)
- [Audio](docs/audio-tutorial.md)
- [Container identity](docs/identity.md)
- [Drag and drop](docs/drag-drop.md)
