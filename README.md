# Shirei

A practical **immediate-mode** GUI framework for building native desktop
applications in Go — not web pages — with flexbox-style layout, robust text,
and almost no boilerplate.

- **Immediate mode** — describe the whole UI each frame; no widget objects, no
  bindings, no reactive state. You mutate ordinary Go structs, and the next
  frame shows them.
- **Cross-platform** — the same UI code runs on macOS, Linux, and Windows.
- **Complex text** — shaping and bidirectional layout, with per-rune font
  fallback (CJK and RTL work out of the box).
- **Flexbox layout** — containers arrange children horizontally or vertically,
  with padding, gaps, alignment, wrapping, floating, scrolling, and expansion.

## The model

A shirei program has two layers:

1. **Application state** — your own structs, slices, and maps, mutated however
   you like.
2. **View functions** — shirei code that, each frame, builds a tree of
   containers to render that state.

There is no reactive variable and no UI object graph to keep in sync: the whole
UI re-renders every frame, so whatever your data says, the next frame shows.

## A minimal app

```go
package main

import (
	app "go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

func main() {
	app.SetupWindow("My App", 1000, 700)
	app.Run(RootView)
}

func RootView() {
	Container(Attrs(Viewport, Background(220, 10, 97, 1)), func() {
		Label("Hello")
		if Button(0, "Refresh") {
			// clicked this frame
		}
	})
}
```

`Container(attrs, builder)` opens a container as a child of the current one and
runs `builder` with it as the current container, so the calls inside — here
`Label` and `Button` — become its children. `Attrs(...)` composes the
container's layout and appearance from small setters (`Row`, `Pad(8)`,
`Background(…)`). Widgets are just functions that build containers; there are
only containers.

## Headless rendering

A frame can be rendered without opening a window, via the built-in software
rasterizer — for instant visual checks, snapshot tests, and a fast feedback
loop (especially handy for AI coding agents):

```go
RenderToPNG("out.png", 1000, 700, RootView)
```

## Learn more

- **Full tutorial:** [docs/tutorial.md](docs/tutorial.md) — the model, building
  blocks, application patterns, and a case study.
- **Audio tutorial:** [docs/audio-tutorial.md](docs/audio-tutorial.md) — sound
  output, mixing, and the piano example.
- **Container identity:** [docs/identity.md](docs/identity.md) — how containers
  stay “the same” across frames (keys, positional matching, conditionals).
- **Drag and drop:** [docs/drag-drop.md](docs/drag-drop.md) — moving items
  between drop zones (`demos/balls-buckets`, `demos/kanban`).
- **Docs site:** https://judi.systems/shirei
- **Example programs:** `examples/` — `du`, `see_pprof`, `process_monitor`,
  `piano`, and more.
