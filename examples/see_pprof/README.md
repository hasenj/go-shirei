# see_pprof

Native viewer for Go `pprof` profiles — a small alternative to
`go tool pprof -http`.

![see_pprof](see_pprof.webp)

## Native pprof profile viewer

Point it at a directory (it starts on the current working directory). The
sidebar lists `*.pprof` files, newest first, and **keeps watching** so new
profiles from other processes appear without a restart.

Select a profile for:

- Sortable top-functions table (flat / cumulative)
- Interactive flame graph: scroll to pan, ctrl+scroll or pinch to zoom, click
  to select, double-click to focus a subtree
- Shared search filter across table and flame
- Caller/callee **Peek** (who calls this, what it calls) without leaving the window

No browser tab, no local HTTP server. With `DEBUG=1`, a floating
`ProfileButton` can capture a CPU profile of this app (or any other shirei app
that embeds the same widget) and drop a `.pprof` next to you to open.

## Flame frames with `Float`

`FlameGraph` does not put frames in a flex row. It walks the tree, computes
each node’s pixel rect, and places a `ContainerWithKey` with `Float(x, y)` and
`FixSize(w, h)`. Off-screen rects are skipped; clicks and hovers use the normal
shirei hit tests on those containers.

```go
// main.go — FlameGraph draw (simplified)
ContainerWithKey(node, Attrs(
    Float(clippedX0, y),
    FixSize(clippedX1-clippedX0, rowH-1),
    Background(node.Hue, sat, lit, 1),
    Pad4(1, 4, 1, 4), CrossMid, Clip,
), func() {
    if IsHovered() {
        hovered = node
        ModAttrs(BorderColor(0, 0, 15, 1), BorderWidth(1))
    }
    if IsDoubleClicked() {
        doubleClicked = node
    } else if IsClicked() {
        clicked = node
    }
    if clippedX1-clippedX0 >= flameMinLabelWidth {
        Label(node.Name, FontSize(10), TextColorVec(labelColor))
    }
})
```

Children get horizontal slices of the parent’s width proportional to `Value`.
Tooltips use `ClickThrough` so they draw on top without eating clicks
(`FlameGraph` in `main.go`).

## State scoped to the tree

Pan / zoom / focus live in a `FlameState` created with `UseWithInit`, inside a
container keyed by `flameRoot`. Selecting another profile changes the key, so
the state is recreated instead of leaking onto a different tree.

```go
// main.go — MainContent
ContainerWithKey(appData.flameRoot, Attrs(Grow(1), Expand, Clip), func() {
    state := UseWithInit[FlameState]("flame-state", func() *FlameState {
        return &FlameState{scale: 1}
    })
    // table above, flame below (shared search / selection via state)
})
```

## Other pieces

- **`UseData`** — sidebar file metadata cached by name/mtime so rescans do not
  re-parse every profile every frame (`cachedProfileFileInfo`).
- **Shared selection** — click selects in table and flame; double-click peeks
  or focuses (`NameCell`, flame click handling).
- **Pointer keys in peek tables** — caller/callee rows key by pointer, not
  function-name string, so recycled names do not collide.

## Run it

```shell
go run .                 # inside examples/see_pprof; watches cwd
go run . --png out.png   # uses newest .pprof if present
DEBUG=1 go run .         # also shows the in-app profiler toggle
```
