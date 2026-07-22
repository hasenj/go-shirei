# Building Applications with Shirei

This tutorial explains how to build a small utility-style GUI application with
Shirei. It is meant to be readable by humans, while still being practical enough
for AI coding sessions to use as a guide.

Good example programs to study, referenced throughout:

- `examples/du` — disk usage scanner with background work, tabs, filtering,
  progress, and a large virtual list.
- `examples/see_pprof` — pprof visualizer with a sidebar, dense sortable
  tables, file watching, a custom flame-graph canvas, and snapshot tests.
- `examples/process_monitor` — live process monitor with platform data
  collectors, stable app-owned objects, flat/table mode, tree mode, selection,
  and CPU/RAM history charts.

For sound (mixing, voices, streaming, headless audio verification), see the
companion [audio-tutorial.md](audio-tutorial.md); `examples/piano` is its
reference program.

Specialized feature write-ups live next to this file (for example
[drag-drop.md](drag-drop.md) for item drag-and-drop,
[virtual-list.md](virtual-list.md) for VirtualList and `Measure`,
[layout-tutorial.md](layout-tutorial.md) for a progressive multi-panel shell,
and [android.md](android.md) / [ios.md](ios.md) for device runners). Touch and
multi-touch contacts are covered under Interaction (§6). This file stays a
general overview.

This tutorial was written by Claude Fable 5, and edited by the framework's
original human author and technical director.

An initial version was written with Fugu (SakanaAI).

---

# Part I — The model

## 1. UI as a function of state

Shirei is an **immediate-mode** GUI framework. Your program describes the
whole UI every frame:

```go
func RootView() {
    Header()
    Toolbar()
    MainPanel()
    DetailPanel()
}
```

You do not create a button object once and attach callbacks to it. Instead, you
say each frame:

```go
if Button(SymRefresh, "Refresh") {
    refreshData()
}
```

If the button was clicked this frame, `Button` returns `true`. If not, it
returns `false`. The UI is just a function of the current application state.

A typical Shirei program therefore has two layers:

1. **Application state** — your own structs, slices, maps, selected item,
   filters, sort mode, etc.
2. **View functions** — Shirei layout code that, each frame, builds a tree of
   containers to render that state (§4).

```go
type AppState struct {
    files    []*File
    selected *File
    showGrid bool
    useGrouping bool
}

var appData = &AppState{showGrid: true}
```

This is the central idea: **keep durable data in your own structs; use Shirei
to render and interact with it each frame.**

### Data is just data

Most GUI frameworks have a special kind of variable for anything that
influences the UI — React's "state", observables, reactive properties. You
cannot simply mutate an ordinary variable; you have to shape your data the
way the framework approves of, so it can notice changes and schedule
updates.

Shirei deliberately has none of that. Application state is ordinary Go data
— structs, slices, maps — mutated however you like, because plain data is
much easier to manage. The whole UI re-renders every frame, so whatever the
data says, the next frame shows. Any variable can drive the UI.

The trade is explicitness about *when frames run*. Shirei does not render in
a continuous loop: a frame runs when input arrives, when the previous
frame's output changed (so built-in animations keep flowing), or when one is
explicitly requested. An idle app renders nothing and costs nothing. And
because your data is just data, Shirei cannot see you change it — when a
change happens outside the input loop, you must say so:

```go
RequestNextFrame()
```

You need it exactly when change originates outside user input:

- a background goroutine published new data (§11) — without the request, the
  UI would update only when the user next moves the mouse;
- you are driving a continuous effect yourself — a blinking caret, an
  elapsed-time clock — where each frame requests the next.

You do not need it for ordinary interactions: input produces a frame by
itself, and Shirei requests follow-ups for its own animations automatically.
Geometry that depends on last frame's layout settles inside the same
presented frame for the common case (§7) — you do not call
`RequestNextFrame` just because a size query started at zero.

That is the deal: the framework never tells you how to shape your data; in
exchange, you tell it when the data changed behind its back.

## 2. A minimal app

Most Shirei applications start with this shape:

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
        Header()
        MainContent()
    })
}
```

Optional placement hints go between `SetupWindow` and `Run`:

```go
app.SetupWindow("My App", 1000, 700)
app.CenterWindow()          // or: app.PositionWindow(120, 80)
app.Run(RootView)
```

Placement is best-effort: macOS, Windows, and X11 honor it; Wayland leaves
top-level placement to the compositor; mobile is always full-screen. On macOS
the window is centered by default even without `CenterWindow`.

The dot imports are the house style; they make UI code much easier to read:

```go
Container(Attrs(Row, Expand, CrossMid, Gap(10), Pad(12)), func() {
    Label("Hello")
    Button(0, "Run")
})
```

## 3. Verify as you go: headless rendering

Shirei renders with its own software rasterizer, so a frame can be rendered
**without opening a window**. For AI agents especially, it's recommended to
build this into the app from the start — it gives a very fast feedback loop for
everything else in this tutorial:

```go
func main() {
    // `myapp --png out.png` renders one settled frame and exits
    if len(os.Args) >= 3 && os.Args[1] == "--png" {
        if err := RenderToPNG(os.Args[2], 1000, 700, RootView); err != nil {
            fmt.Println("render failed:", err)
        }
        return
    }

    app.SetupWindow("My App", 1000, 700)
    app.Run(RootView)
}
```

`RenderToPNG` (and `RenderToImage`, which returns the `*image.RGBA` instead)
runs several frames until the layout settles, from a neutral input state,
with animations disabled — so the output is deterministic. This gives you:

- **Instant visual checks** during development without clicking around.
- **Snapshot tests**: render into an image and compare against a committed
  golden PNG. A mismatch writes an `.actual.png` you can eyeball; an
  environment flag regenerates goldens. See `examples/see_pprof/main_test.go`
  for a complete worked example (including how to make the app's data
  deterministic for the test).
- **AI-friendly verification**: a coding session can render your app and
  *look at it* without a human driving a window.

For testing interactions (clicks, typing, scrolling) headlessly, drive frames
directly: set `WindowSize`, fill `InputState`/`FrameInput`, and call
`RunFrameFn`. The tests in `widgets/semantic_test.go` are the reference
pattern.

One habit worth forming: when a screen reaches a state you like, snapshot it.
The test then guards every future refactor for free.

---

# Part II — Building blocks

## 4. Containers

Every frame, your view functions build a **tree of containers**, and `Container`
is how you add to it. A call like this:

```go
Container(Attrs(Gap(8)), func() {
    Label("First")
    Label("Second")
})
```

does three things:

- it opens a new container as a child of the *current* container;
- `Attrs(...)` builds that container's **attribute set** — its layout and
  appearance (here, an 8px gap between children; more attributes below);
- the builder `func()` runs with the new container as the current one, so the
  `Label` calls inside it become *its* children. When the builder returns,
  "current" reverts to the parent.

Nesting `Container` calls therefore nests containers — that is how the whole tree
takes shape, one builder inside another. At the root, `app.Run(RootView)` calls
your top-level view with the window as the current container, and everything
grows from there.

Note that `Label` is not a special node type. Widgets — `Label`, `Button`,
`TextInput`, and the rest — are ordinary functions that build containers with
`Container`/`Element` themselves, so a `Label` inside a builder adds container
children just as a nested `Container` would. Coming from React, this is the thing
to unlearn: there is no separate "component" or "element" kind, and no virtual
DOM — **there are only containers**, built by plain function calls each frame.

Containers stack their children vertically by default; `Row` makes them
horizontal:

```go
Container(Attrs(Row, Gap(8)), func() {
    Label("Left")
    Label("Right")
})
```

`Element` is a container without a builder (a colored box, a spacer).

```go
Element(Attrs(Expand, FixHeight(1), Background(0, 0, 0, 1))) // 1px separator
```

`ModAttrs(...)` modifies the *current* container's attributes from inside its
builder — useful for conditional styling (hover highlights) and for *clearing*
cascaded values (see cascade timing below). Call it before adding children.

```go
for _, item := range list {
    Container(Attrs(Background(200, 50, 50, 1), Pad(10)), func() {
        // highlight the current row if it's selected
        if selectedItemId == item.Id {
            ModAttrs(Background(200, 50, 70, 1)) // sets a different background color
        }

        // add children *after* ModAttrs
        Label(item.Name, TextColor(0, 0, 100, 0.7))
    })
}
```


### How sizing works (the two-pass mental model)

1. **Bottom-up**: children are laid out, then the parent sizes itself to
   contain them. By default, a container is exactly as big as its content,
   unless MinWidth or MinHeight are set, then a container could be bigger
   than its content.
2. **Top-down**: leftover space is distributed. `Grow(n)` takes remaining
   space along the parent's main axis (proportionally, if several children
   grow); `Expand` stretches across the cross axis; `MinSize`/`MaxSize`
   constrain the result.

The one attribute that changes the rules: **`Extrinsic`** (`ExtrinsicSize`,
also bundled into `Viewport`) makes a container ignore its content entirely
and take only what **parent constraints** give it. That is what makes
budgeted panels and scroll regions possible: without it, content can
**inflate** the measured size of a `Grow`/`Expand` panel instead of
overflowing or clipping inside a fixed budget.

`Extrinsic` is **all-or-nothing across axes**. The container no longer
sizes itself from content on *any* axis. So it needs a real external size
on each axis that matters:

- If a flex parent assigns width and height (e.g. `Grow(1)` in a full-height
  row next to a sidebar), Extrinsic is safe for that **pane**.
- If a container is an ordinary content row in a column (header, find bar,
  status strip) and you set Extrinsic **without** also giving it height
  (`FixHeight`, `MinHeight` with a non-extrinsic height path, or a parent
  height slot), its **height collapses** — there is nothing content can
  contribute and nothing external assigned.

**Pick the tool for the job:**

| Intent | Prefer |
|--------|--------|
| Pane / scroll body: size from outside on both axes | `Viewport`, or `Grow`+`Expand`+`Extrinsic`+`Clip` |
| Content row (toolbar, find bar, title strip): height from content; do not widen the pane | `Expand`+`Clip` (rely on an **extrinsic parent** for width); optional `MinHeight` |
| Cap only one axis | `MaxWidth` / `MaxHeight` / `FixHeight` — not full Extrinsic |
| Clip painting without changing sizing rules | `Clip` alone (does **not** stop content from measuring larger) |

A common full-window layout (and a nested “detail” column that must not
grow from long labels or text fields):

```go
func RootView() {
    Container(Attrs(Viewport, Background(220, 10, 97, 1)), func() {
        Header()                                // content-sized height
        Toolbar()                               // content-sized height
        Container(Attrs(Grow(1), Expand, Clip), func() {
            MainList()                          // takes the remaining space
        })
        StatusBar()                             // content-sized height
    })
}

// Sidebar | detail: detail width from flex, not from the longest line.
Container(Attrs(Row, Grow(1), Expand, Clip), func() {
    Sidebar() // FixWidth(...)
    Container(Attrs(Grow(1), Expand, Extrinsic, Clip), func() {
        DetailHeader()  // Expand, Clip — height from content
        FindBar()       // Expand, Clip, MinHeight(...) — not Extrinsic alone
        Container(Attrs(Viewport), func() {
            // scroll body: Extrinsic + Grow from Viewport
            DiffOrList()
        })
    })
})
```

Inside the extrinsic detail column, tool strips stay **content-height**;
only the fill region uses `Viewport`. Do not put bare `Extrinsic` on every
child “to prevent inflation” — that is how zero-height rows appear.

### Cascade: MaxSize (and friends)

Some attributes **cascade** from parent to child when the child leaves them
unset: cross-axis `MaxSize`, text style (§5), and a few behavior flags
(`NoAnimate`, `ClickThrough`). Cascade is not CSS-style deep inheritance of
every property — it is a one-step copy from the *immediate parent* at container
open. The rules matter for layout, so read them carefully.

#### Cross-axis MaxSize only

A parent only cascades max on its **cross** axis (the axis perpendicular to
how it lays out children):

| Parent layout | Cascades | Does **not** cascade |
|---|---|---|
| Column (default) | `MaxWidth` → children's max width | `MaxHeight` |
| Row | `MaxHeight` → children's max height | `MaxWidth` |

Parent padding is peeled off first, so the child receives the content-box
budget (parent max − pad on that axis).

Main-axis max stays on the parent only. That is deliberate: a wrapping row
with `MaxWidth(400)` should wrap its own children, not force every button
inside to a 400px width.

#### One step at a time — orientation can flip

Cascade always looks at the *immediate* parent. Nested layouts therefore
change which axis continues:

```go
// Column with MaxWidth: children that leave width unset inherit that max.
Container(Attrs(MaxWidth(300), Pad(12), Gap(8)), func() {
    Label("This paragraph soft-wraps at ~276px") // 300 − 12 − 12 pad
    // (Text reads current.MaxSize[0] for wrap width; see §5.)

    // This row is a *child of the column*, so it inherits MaxWidth.
    // But a row's cross axis is height, not width — so its own children
    // do NOT inherit that width. They only inherit MaxHeight if this row
    // sets one.
    Container(Attrs(Row, Gap(8)), func() {
        Button(0, "A") // no cascaded MaxWidth from the column above
        Button(0, "B")
        // If the row itself set MaxHeight(40), A and B would get max height 40
        // (minus the row's vertical pad). They still would not get max width
        // from the outer column.
    })
})
```

Picture the tree:

```text
Column  MaxWidth=300
├── Label          → max width cascaded (column's cross axis)
└── Row            → max width cascaded onto the row itself
    ├── Button A   → row's cross axis is height → no max width from above
    └── Button B   → same
```

So a max width set high in a column tree reaches every nested *column* child
along that path, but the moment you open a row, further descendants stop
receiving width cascade and may start receiving height cascade instead.

#### Opting out of cascade (`UnsetMaxCross`, `YesAnimate`)

Cascade runs when a container opens: if the child's field is still zero (and
not explicitly opted out), the parent's value is copied in.

**`UnsetMaxCross`** and **`YesAnimate` / `NoAnimate` / `AnimateOnly`** set a
flag so open-time cascade does not overwrite them. Both work in `Attrs(...)`:

```go
// Cross-axis max: do not inherit the parent's MaxWidth (column) / MaxHeight (row).
Container(Attrs(UnsetMaxCross), func() {
    // wide content may overflow this box; children do not see the grandparent max
})

// Animation: re-enable easing under a Viewport / NoAnimate parent.
Container(Attrs(Viewport), func() {
    Container(Attrs(YesAnimate, /* ... */), func() { /* eases again */ })
})
```

`ModAttrs(UnsetMaxCross)` after open still works if you prefer to clear in the
builder. Clearing stops further propagation **below that node** — cascade is
only parent → child.

### The attribute cheat sheet

`Attrs(...)` composes attribute functions into an `AttrSet` value; `AttrsWith(base, ...)`
extends an existing one. The ones you will use constantly:

Structure and sizing:

| attr | meaning |
|---|---|
| `Row` | horizontal main axis |
| `Gap(n)` | space between children |
| `Pad(n)` / `Pad2(v,h)` / `Pad4(t,r,b,l)` | padding |
| `Spacing(n)` | shorthand: `Gap(n)` + `Pad(n)` |
| `Grow(n)` | take remaining main-axis space |
| `Expand` | fill the cross axis |
| `FixWidth/FixHeight/FixSize` | exact size |
| `MinWidth/MinHeight/MinSize`, `MaxWidth/MaxHeight` | constraints (cross-axis max cascades; see above) |
| `UnsetMaxCross` | clear cascaded cross-axis max (`Attrs` or `ModAttrs`) |
| `Clip` | clip overflowing content (paint only; does not by itself stop measuring larger) |
| `Wrap` | wrap children into lines |
| `Extrinsic` | size from constraints only — content neither inflates nor defines size (all axes); see sizing above |
| `Viewport` | bundle: `Extrinsic`+`Grow(1)`+`Expand`+`Clip`+`NoAnimate` — fill/scroll panel, not every chrome row |
| `Center` / `CrossMid` | center children (both axes / cross axis) |
| `CrossAlign(...)`/`MainAlign(...)`/`SelfAlign(...)` | cross/main/self alignment |
| `Float(x, y)` | position by hand within the parent (skips flow layout) |
| `InFront` / `Behind` / `Z(n)` | z-ordering |

Appearance:

| attr | meaning |
|---|---|
| `Background(h, s, l, a)` | background color, HSLA |
| `Grad(dh, ds, dl, da)` | vertical gradient as a *delta* from the background color |
| `Corners(r)` / `Corners4(...)` | corner rounding |
| `BorderWidth(w)`, `BorderColor(h, s, l, a)` | border width and color |
| `BoxShadow(blur)` | drop shadow |
| `Trans(a)` | transparency applied to the whole subtree |

Text style (on containers; full story in §5):

| attr | meaning |
|---|---|
| `AmendTextStyle(mods...)` | parent text style + mods → this container's style |
| `SetTextStyle(base, mods...)` | fresh base + mods (does not inherit parent style) |

Behavior:

| attr | meaning |
|---|---|
| `NoAnimate` / `YesAnimate` | opt out of / back into implicit animation (`NoAnimate` cascades; re-enable with `ModAttrs(YesAnimate)`) |
| `ClickThrough` | exempt from hit-testing (tooltips, overlays); cascades |
| `Focusable` | participates in tab-cycling focus |

From `widgets`, two layout helpers you will use in every toolbar:

```go
Filler(1) // flexible empty space (an Element with Grow)
Spacer(8) // fixed space along the current layout direction
```

```go
Container(Attrs(Row, CrossMid, Gap(10), Pad(12)), func() {
    Label("My App")
    Filler(1)                    // pushes the button to the right edge
    Button(0, "Refresh")
})
```

## 5. Colors and text styling

Colors are HSLA `Vec4`s: hue 0–360, saturation 0–100, lightness 0–100,
alpha 0–1.

```go
Background(220, 10, 97, 1) // very light cool gray
Background(220, 25, 18, 1) // dark blue-gray
TextColor(0, 0, 100, 1)  // white text
TextColor(0, 0, 15, 1)   // almost black text
```

### Text style lives on containers

Every container carries a fully resolved **text style** (font size, weight,
families, color, and related fields). The frame root starts each frame at
`DefaultTextStyle()`. When a child leaves its style unset, it **inherits the
parent's whole style** (cloned) — wholesale, not field-by-field. Same cascade
timing as MaxSize (§4): parent → child at open; only the immediate parent.

Set style on a container so a whole panel shares defaults:

```go
// Inherit whatever the parent uses, then bump size and color for this subtree.
Container(Attrs(AmendTextStyle(FontSize(14), TextColor(0, 0, 20, 1))), func() {
    Label("Body copy in the panel default")
    Label("Also body — no need to repeat FontSize on every label")

    // Nested amend: starts from *this* panel's style, not the root default.
    Container(Attrs(AmendTextStyle(Fonts(Monospace...))), func() {
        Label("mono body at 14px, same color")
    })
})

// Replace rather than amend: ignore parent style entirely.
Container(Attrs(SetTextStyle(DefaultTextStyle(), FontSize(11), TextColor(0, 0, 45, 1))), func() {
    Label("Caption chrome with an explicit base")
})
```

- **`AmendTextStyle(mods...)`** — copy current parent style, apply mods, store
  the full result on this container (so its children cascade from here).
- **`SetTextStyle(base, mods...)`** — same, but start from an explicit `base`
  (often `DefaultTextStyle()`) instead of the parent. Use when a subtree must
  not pick up a themed ancestor.

There is no separate "unset text style" flag: to break out of a themed parent,
`SetTextStyle` with the base you want.

### Labels and call-local style

`Label` is the everyday API. Optional mods amend the **current container's**
text style for that leaf only — siblings and the container itself are
unchanged:

```go
Label("Heading", FontSize(18), FontWeight(WeightBold), TextColor(220, 40, 25, 1))
Label("caption", FontSize(10), FontStyle(StyleItalic), TextColor(0, 0, 45, 1))
```

Under the hood, `Label(s, mods...)` is `Text(s, TextStyle(mods...))`.
`TextStyle(mods...)` returns current style + mods as a fully resolved value
for `Text`'s second argument; it does not write back onto the container.

### Soft wrap width

Text soft-wraps to the **current container's max width** (`MaxSize[0]`),
including a value cascaded from an ancestor (§4). Zero means unconstrained
(no soft wrap). Put `MaxWidth` on a panel (or rely on cascade into a column
child); do not expect a separate per-text max-width attribute.

```go
Container(Attrs(MaxWidth(280), Pad(10)), func() {
    Label("Long copy wraps inside this panel without extra width args on Label.")
})
```

### Inline spans

For mixed styles inside one string, use `Text` with `Span` ranges (rune
indices, half-open). Spans resolve against that call's paragraph base:

```go
Text("The word orange is colored.", TextStyle(),
    Span(9, 15, TextColor(30, 90, 50, 1)), // "orange"
)
```

`demos/style-spans` is the worked gallery: panel `AmendTextStyle`, then one
`Text` + `Span`s per card.

`FontSize` sets the size; `Fonts(...)` selects font families (system fonts are
discovered automatically, with per-rune fallback — CJK and bidi text work out
of the box).

Shirei also bundles two icon fonts — **Typicons** and **Microns** — with
rune constants in `widgets` (`TypArrowUpThick`, `TypFolderOpen`,
`SymRefresh`, …; see `widgets/typicons.go` and `widgets/microns.go`).
Render one with `Icon(TypFolderOpen)` — the two rune ranges don't overlap,
so `Icon` serves both — or pass it to widgets that take an icon rune:
`Button(SymRefresh, "Refresh")`, with 0 meaning no icon. The icon fonts
register themselves when the `widgets` package is imported, so they work
everywhere — windowed and headless (`--png`, snapshot tests) alike; there
is nothing to call. To find an icon, run `examples/icons`: a filterable
gallery of the full set, each glyph next to its constant name.

A typical utility-app palette: dark header, light toolbar, medium-light table
header, alternating light rows, light detail panel.

## 6. Interaction

Shirei input is **data, not events**: each frame the backend publishes the
current input state, and your code queries it inline. `InputState` holds
cumulative state (mouse position, held keys, modifiers); `FrameInput` holds
what happened *this frame* (a click, a key press, typed text, scroll).

### Hover, click, press

```go
Container(Attrs(Row, Pad(8), Background(0, 0, 95, 1)), func() {
    if IsHovered() {
        ModAttrs(Background(0, 0, 90, 1))    // hover highlight
    }
    if PressAction() {
        appData.selected = item      // acts on full press (down then up inside)
    }
    Label(item.Name)
})
```

Three related queries, with different meanings:

- `IsClicked()` — the mouse button went *down* over this container this
  frame. Right for selection and other non-destructive acts.
- `PressAction()` — a full press *gesture* completed: down and up both inside
  this container. Right for buttons and anything destructive (the user can
  still bail by dragging off before releasing).
- `IsDoubleClicked()` — this frame's click is the second (or later) of a
  click streak (`FrameInput.ClickCount`, detected by core from click timing
  and position). Note the first click of the pair fires `IsClicked` on its
  own frame — so the natural pattern "click selects, double-click acts"
  needs no special handling.

`IsHoveredDirectly()` is `IsHovered` minus children — true only when the
cursor is on this container itself, e.g. to treat a click on a panel's empty
background differently from a click on its content.

### Dragging

`PressAction()` marks the container *active* between mouse-down and mouse-up;
`IsActive()` queries it. With `FrameInput.Motion` (per-frame mouse movement),
that is the whole dragging story. A pane splitter, complete:

```go
Container(Attrs(FixHeight(6), Expand, Background(0, 0, 80, 1)), func() {
    if IsHovered() {
        ModAttrs(Background(210, 60, 60, 1))
    }
    PressAction()
    if IsActive() {
        splitRatio += FrameInput.Motion[1] / totalHeight
    }
})
```

For **moving items between drop zones** (cards between columns, balls into
buckets), use the drag-and-drop helpers in `widgets` instead — see
[drag-drop.md](drag-drop.md). That API carries typed payloads;
`PressAction` does not.

### Touch and multi-touch

On mobile (and any backend with a finger), **one finger is also mouse**:
backends fill the usual pointer path so taps, drags, and fling scroll work
with the same `PressAction` / `ScrollOnInput` code as on desktop. In parallel,
every active contact is published as multi-touch **data** —
`InputState.Touches` (up to `MaxTouches`), with began/ended ids on
`FrameInput`, and hit queries `IsTouched` / `IsTouchedDirectly` /
`TouchingIds` / `TouchById` (same timing idea as hover). Prefer
`IsTouched` when several fingers matter; while `InputState.MouseFromTouch` is
set, ignore the synthetic mouse for hold-style logic so a delayed mouse-up
cannot re-press after lift (see `examples/piano`). Shirei does **not** yet
ship built-in multi-finger *gestures* (pinch, two-finger pan, rotate) —
those are ordinary app code over the contact table if you need them.

### Scrolling

Inside any clipped container, `ScrollOnInput()` applies wheel/trackpad input
to the container's scroll offset, and `ScrollBars()` draws a floating modern
overlay scrollbar (transparent track, thin thumb; override with
`SetDefaultScrollBar` / `ScrollBarExt`):

```go
Container(Attrs(Viewport), func() {
    ScrollOnInput()
    ScrollBars()
    for _, item := range items {
        ItemRow(item)
    }
})
```

(For long lists, prefer the virtualized containers in Part III — they
scroll without building offscreen rows at all.)

### Focus and keyboard

Text inputs handle their own focus (`FocusOnClick`, `AutoFocus`) and support
tab-cycling between `Focusable` containers (`CycleFocusOnTab`). For your own
focusable widgets, the same primitives are available: `HasFocus()`,
`Focus()`, `Blur()`. Keyboard state is queried like everything else:
`FrameInput.Key` (pressed this frame), `FrameInput.Text` (text typed this
frame, IME-aware), `InputState.Modifiers`, `InputState.DownKeys`.

```go
TextInput(&appData.filter)       // edits the string in place
PasswordInput(&appData.secret)
```

That pointer-passing style is the immediate-mode payoff: no binding, no
change events — if anything else modifies the string, the input shows it.

### Clipboard

`RequestTextCopy(text)` queues text for the clipboard; the backend writes
it at the end of the frame. So "copy" is just another thing a widget does
inline (see `LogView`'s hover copy button, or `examples/icons`):

```go
if CtrlButton(SymCopy, "Copy name", true) {
    RequestTextCopy(selected.Name)
}
```

`RequestPaste()` is the read side: the backend fetches the clipboard and
delivers it on a later frame as `FrameInput.Text`, exactly as if typed —
which is how `TextInput` gets paste without special cases.

### Popups, menus, tooltips

`MenuButton`/`MenuItem` and `PopupPanel(&open, anchorId, attrs, fn)` build
floating panels anchored to an element; they render at the root, above
everything, automatically (§2). Any value you used as a container key — or the
`ContainerId` handle returned by `Container`/`ContainerWithKey`, or `CurrentId()` —
works as an anchor.

Widgets to reach for: `Button`/`CtrlButton`, `CheckBox`, `OptionButton`,
`ToggleSwitch`, `Slider`, `TextInput`, `DirectoryBrowse`, `Table`,
`VirtualListView`, `LogView`, `LargeText`, `Link`, `DebugPanel`/`DebugVar`,
and drag-and-drop (`DragAndDrop` / `CanDropHere` — [drag-drop.md](drag-drop.md)).

### Widget commands

Input answers "what did the user do"; sometimes the *app* needs to tell a
widget something imperative — "scroll this row into view" — where passing
state every frame is the wrong shape: the request is an event with a
cause, not a fact that stays true. For this there is a small command
queue:

```go
// the list names itself: its first argument is its key — a typed pointer to
// app-owned data, globally unique among live widgets
VirtualListView(pane, count, itemKey, itemHeight, itemView)

// the app posts at the event site:
VirtualListScrollIntoView(pane, row)
```

`VirtualListScrollIntoView` is a one-line wrapper over the plumbing:
`PostCommand(widget, key, name, arg)` stores one slot per (widget kind,
instance key, command name) — a second post before consumption overwrites
the first, latest intent wins, exactly like sampled input — and the
widget consumes on its next render the way it consumes input, by checking
for data (`TakeCommand[T]`). An unconsumed command expires at the start
of the second frame after the post: long enough to survive any build
order (consumer above or below the poster in the tree), short enough that
a request aimed at a hidden view dies quietly instead of firing when the
view returns.

Conventions: widgets ship named wrappers so call sites read as verbs, and
keep the consuming side package-private; the widget-kind string scopes
commands, so two widget types keyed by the same app object don't collide;
and post on *events*, not conditions — a file pane reveals on arrow keys
but never on mouse clicks, because a click on a half-visible row must not
yank the list under the cursor.

## 7. State across frames: identity, hooks, settling, animation

This section is the heart of writing *correct* Shirei code. Everything in it
follows from one fact: the UI is rebuilt from scratch every frame, so
anything that persists — hover, focus, scroll offsets, widget state,
animation — is keyed by **container identity**, maintained in a persistent
identity tree. Full write-up for app authors: [identity.md](identity.md).

### Identity

Anonymous containers (`Container`, `Element`) are matched **positionally** by
(component type, per-type ordinal), where the component type is the builder's
func literal. Fixed structure and loops with stable membership need nothing
special, and a conditionally inserted sibling of a *different* type does not
shift the others' identity. Only a same-type insertion or reorder shifts
positional identity — which is what explicit keys are for.

For dynamic collections — rows that come and go, reorder, or carry state that
must follow the item — pass an explicit **key**:

```go
ContainerWithKey(itemPtr, Attrs(...), func() { ... })       // pointer to an app object
ContainerWithKey(name, Attrs(...), func() { ... })          // any string — dynamic is fine
ContainerWithKey(fmt.Sprintf("%d", pid), Attrs(...), ...)   // also fine
```

A key is any comparable value you own; it's matched by Go value equality
(pointers by pointer, strings by content) and is **scoped to the parent**: the
same key under two different parents is two distinct containers. The one rule:
keys must be **unique among siblings** within a frame — a duplicate is a program
bug, and shirei reports it loudly on stderr rather than behaving erratically.

A key reconciles the container across frames; to *address* the container after
the fact — focus, hover, geometry, popup anchors — you use its **`ContainerId`**,
an opaque handle that `ContainerWithKey` (and `Container`/`Element`/`CurrentId`/
`GetLastId`) return. Hold that handle and pass it to `IdHasFocus`,
`GetScreenRectOf`, and the like.

For lists of real entities, it's best to use the application's intrinsic way of
reliably identifying entities, whether it be pointers or handles.

This is especially important when the list view can re-arrange (sort) items.
Otherwise, the state would be associated with the position of the item in the
list, and resorting items would mix up their associated states.

```go
for _, item := range sortedItems {
    ContainerWithKey(item.Id, ......)
}
```

### Hooks: widget-local state

`Use[T](key)` attaches a piece of state to the *current container*:

```go
var state = Use[MenuState]("menu-state")
```

It returns a pointer; the first use allocates. The state lives exactly as
long as it keeps being used: **a hook not touched for one full frame is
dropped** — a hidden view forgets its widget-local state (scroll position,
sort order) and reinitializes when shown again. This is deliberate: hooks are
UI state, not application state. Anything that should survive belongs in your
own structs (see Part III).

`UseData[T](ptr, key)` is the other flavor: state attached to an *application
object* rather than a container, surviving regardless of rendering, with
manual cleanup (`DeleteHookedData`). Good for caching derived/parsed data
alongside the object it derives from.

Rule of thumb: app data in your structs; widget-local state in `Use`;
object-derived caches in `UseData`.

### Layout queries and multipass settle

Builders run *before* this frame's layout is fully committed, so every
geometry query — `GetResolvedSize`, `GetContentRect`, `GetScreenRect`, and
the `...Of(id)` variants like `GetScreenRectOf(id)` — answers from the
**previous layout pass**. For a brand-new container that has never been
laid out, that answer is zero until a pass has produced real sizes.

That sounds like a problem for split panes and other “size from parent /
sibling geometry” layouts. In practice the runtime handles the common case:

1. Your builder runs and may query geometry that is not known yet.
2. Layout runs and records sizes for this pass.
3. If any geometry query was unanswered, Shirei **re-runs the same frame**
   (a settle pass) without presenting the incomplete intermediate result.
4. The second pass reads the first pass's sizes, so the frame that reaches
   the screen is already settled for direct dependencies.

```go
// Split by ratio of the current container's resolved height.
// On the first pass GetResolvedSize may be zero; the settle pass re-runs
// the builder once sizes exist — no RequestNextFrame required for that.
totalHeight := GetResolvedSize()[1]
topAttrs := Attrs(Expand, Clip)
if totalHeight > 0 {
    topAttrs = Attrs(FixHeight(totalHeight*splitRatio), Expand, Clip)
}
```

A few nuances:

- **One settle pass** covers the usual “query parent, size child” pattern.
  Longer chains of interdependent geometry may still converge across
  successive *presented* frames when content keeps changing.
- **`RequestNextFrame` is not the geometry fix.** Use it for background
  data, animation you drive yourself, and other out-of-band updates — not
  to “wake” a zero size query.
- **`RenderToPNG` / headless snapshots** run until the frame settles, which
  is why goldens are stable without manual multipass in app code.

See `examples/see_pprof` (`MainContent`) for a real split pane that reads
`GetResolvedSize` while building.

### Animation: on by default

When a container's geometry changes between frames (position, size, padding,
corners, border width, transparency), Shirei **animates** it toward the new
values — this is why UI changes feel fluid with zero effort. A container
appearing for the first time does not animate (there is nothing to animate
from).

Sometimes you don't want it:

- **Hand-positioned content** (`Float` canvases, custom scrollbars, cursors):
  interpolation turns precise placement into drift. Use `NoAnimate`.
- `NoAnimate` **cascades** to children; `YesAnimate` opts an inner container
  back in. A scrollbar does exactly this: `NoAnimate` on the track (because it's
  floating) with `YesAnimate` on the thumb (so it glides).
- `Viewport` includes `NoAnimate` already — panels snap, content animates.

---

# Part III — Application patterns

## 8. Application state: prefer your own structs

For application wide state, define a central state struct:

```go
type AppState struct {
    showGrid bool
    // ...
}

var appData = &AppState{}
```

The UI reads and mutates `appData` directly:

```go
func Toolbar() {
    Container(Attrs(Row, CrossMid, Gap(10)), func() {
        ToggleSwitch(&appata.showGrid)
        Label("Grid")
    })
}

func MainContent() {
    Container(......, func() {
        if appState.showGrid {
            // ... show a grid ...
        }
    })
}
```

## 9. Tables and virtual lists

For the common case — sortable columns over a slice of rows — use the
built-in generic `Table`:

```go
columns := []TableColumn[*Item]{
    {
        Label:  "Name",
        Render: func(it *Item) { Label(it.Name) },
        Less:   func(a, b *Item) bool { return a.Name < b.Name },
    },
    {
        Label: "Size", Width: 90, DefaultDesc: true,
        Render: func(it *Item) { Label(formatBytes(it.Size)) },
        Less:   func(a, b *Item) bool { return a.Size < b.Size },
    },
}

Table(nil, 30, columns, appData.items, func(it *Item) any { return it }, 1)
```

You get clickable sortable headers, aligned columns (one flexible column,
the rest fixed-width), and virtualization for free. The `rowKey` function
must return a stable unique identity per row — the row pointer, per §7. The
last argument picks the initial sort column.

For fully custom lists (non-uniform rows, custom cells), drop down to
`VirtualListView`:

```go
itemKey := func(i int) any { return rows[i] }
itemHeight := func(i int, width f32) f32 { return 30 }
itemView := func(i int, width f32) { RowView(rows[i], i) }

// first argument is the list's own key (nil = anonymous; give it a stable
// value to command it — see below)
VirtualListView(nil, len(rows), itemKey, itemHeight, itemView)
```

Virtualization only builds visible rows, so lists scale to hundreds of
thousands of items. It is the right default for process lists, file lists,
logs, and search results. (For huge text blobs specifically, use the
`LargeText` widget.)

A list with an identity can also be *commanded*: give it its key as the first
argument (`VirtualListView(pane, …)`) and `VirtualListScrollIntoView(pane,
rowPtr)` brings a row into view on the next render, minimally — keyboard
selection's best friend (see §6, Widget commands).

Rows in the list do need to have uniform height, so the list view is *not*
restricted to rendering uniform rows. For example, it can be used to render
a document structure where some elements are headers, some are pargraphs, some
are images, etc.

## 10. Selection

Selection is just a pointer in your app state:

```go
type AppState struct {
    selected *Item
}
```

Row:

```go
func ItemRow(item *Item, idx int) {
    bg := f32(100)
    if idx%2 == 1 {
        bg = 97
    }
    if appData.selected == item {
        bg = 87
    }

    Container(Attrs(Row, Expand, FixHeight(30), Background(220, 8, bg, 1)), func() {
        if IsHovered() {
            ModAttrs(Background(220, 12, 92, 1))
        }
        if IsClicked() {
            appData.selected = item
        }
        Label(item.Name)
    })
}
```

Detail panel:

```go
func DetailPanel() {
    item := appData.selected
    if item == nil {
        Label("Select an item")
        return
    }
    Label(item.Name, FontWeight(WeightBold))
    Label(item.Description)
}
```

An interaction convention that has worked well (see `see_pprof`): **single
click selects, double click acts** (opens, drills in, expands). Because the
first click of a double-click pair fires normally, selection should not be a
click-toggle — clear it with an explicit button or a click on empty
background instead.

## 11. Background work and live data

If a goroutine updates UI state, publish the result under Shirei's frame lock
and then request a new frame:

```go
go func() {
    for {
        data, err := collectData() // expensive work outside the lock

        WithFrameLock(func() {
            appData.data = data
            appData.err = err
        })
        RequestNextFrame()

        time.Sleep(time.Second)
    }
}()
```

Rules:

- Do expensive work outside `WithFrameLock`.
- Mutate shared UI state inside `WithFrameLock`.
- Call `RequestNextFrame()` after publishing.
- App code should not call backend-specific wake functions — backends poll
  Shirei's requested-frame state, keeping the dependency direction clean.

For live monitors, avoid burst sampling: keep the previous sample and compute
deltas from the next one, one collection per refresh interval. (Burst
sampling suits one-shot terminal tools; a continuously-running GUI should
sample smoothly.)

Another use of the same machinery: watching external state. `see_pprof`
watches its directory with fsnotify; the watcher goroutine updates the file
list under `WithFrameLock` and requests a frame — new files appear in the
sidebar the moment they land on disk.

## 12. Stable stores for live entities

The example program `process_monitor` queries the operating system for the list
of running processes and their states at a fixed interval.

When live data arrives as a fresh list each sample, we do not render that list
directly, because we need stable selection, history, expansion state, and
recently-departed items. So we create a stable store:

```go
type ProcessKey struct {
    PID       int
    StartTime time.Time
}

type Process struct {
    ProcInfo
    Key       ProcessKey
    LastSeen  time.Time
    StoppedAt time.Time
    History   []ProcessPoint
    Collapsed bool
}

type ProcessStore struct {
    ByKey map[ProcessKey]*Process
}
```

Each sample:

1. Compute a stable key for each incoming entity.
2. Look up the existing object; update in place, or allocate.
3. Append a history point.
4. Mark missing entities as stopped.
5. Prune old stopped entries unless selected.

This gives us stable pointers for row ids and selection (§7, §10), and a
natural home for history charts and tree expansion state.

---

# Part IV — Case study: patterns from process_monitor

These sections are more specific — they document patterns from building a
live system monitor. Skim them if your app is not monitor-shaped; the
techniques (derived display orders, time-bucketed charts, platform layering)
transfer to other domains.

## 13. Tree views

A tree view is a derived display order over stable objects:

```go
byPID := map[int]*Process{}
children := map[int][]*Process{}
roots := []*Process{}

for _, p := range procs {
    byPID[p.PID] = p
}
for _, p := range procs {
    parent := byPID[p.PPID]
    if parent == nil || parent == p {
        roots = append(roots, p)
    } else {
        children[parent.PID] = append(children[parent.PID], p)
    }
}
```

Flatten depth-first into the visible row list:

```go
func walk(p *Process, depth int) {
    p.TreeDepth = depth
    p.TreeChildCount = len(children[p.PID])
    rows = append(rows, p)

    if p.Collapsed {
        return
    }
    for _, child := range children[p.PID] {
        walk(child, depth+1)
    }
}
```

Render indentation inside the name column so numeric columns stay aligned:

```go
Container(Attrs(Row, Grow(1), CrossMid, Clip), func() {
    Element(Attrs(FixWidth(f32(depth) * 14)))

    if p.TreeChildCount > 0 {
        if PressAction() {
            p.Collapsed = !p.Collapsed
        }
        if p.Collapsed {
            Label("▸")
        } else {
            Label("▾")
        }
    }

    Label(p.Name)
})
```

When filtering a tree: show matches plus their ancestors, and ignore
collapsed state while a filter is active so matches are always visible.

## 14. Simple charts

Charts are just layouts and elements. A bottom-aligned bar chart is a row of
fixed-height columns, each containing a flexible spacer above the bar:

```go
Container(Attrs(Row, FixHeight(chartHeight), Gap(1)), func() {
    for _, b := range buckets {
        Container(Attrs(Grow(1), FixHeight(chartHeight), NoAnimate), func() {
            Filler(1)
            Element(Attrs(FixHeight(barHeight), Expand, Background(hue, sat, light, 1)))
        })
    }
})
```

(Note the `NoAnimate` — per §7, hand-shaped data visualizations should not
interpolate.)

## 15. Platform layers

Keep platform-specific collection separate from UI code, behind one
build-selected function:

```go
func Collect() (RawSnapshot, error)
```

```text
collect_darwin.go      //go:build darwin
collect_linux.go       //go:build linux
collect_windows.go     //go:build windows
collect_unsupported.go //go:build !darwin && !linux && !windows
```

The rest of the app does not know about `libproc`, `/proc`, or Win32 APIs:

```text
platform Collect()
    -> common Sampler computes rates/deltas
        -> stable store updates app-owned objects
            -> UI renders tables, tree views, and charts
```

This separation is what let `process_monitor` support macOS, Linux, and
Windows without touching the GUI.

---

# Part V — Reference

## 16. Common mistakes

### Dynamic lists without keys

Symptoms: duplicate-key reports on stderr; or row state (scroll, hover,
focus, animation) jumping between items when a list sorts, filters, or
reorders.

Fix: for collections of the *same kind of thing* that can change membership
or order, give each item a **key** with `ContainerWithKey` — usually a
pointer or stable id from your app data, never the loop index `i`. Keys must
be unique among siblings under one parent. Anonymous containers are matched
positionally by (component type, per-type ordinal): fixed chrome and
mixed-type siblings are fine without keys; same-type insert/reorder without
keys is what scrambles state. Full rules: [identity.md](identity.md).

### Animation artifacts on hand-positioned content

Symptoms: custom-drawn content (charts, canvases, cursors) drifts, smears,
or lags one step behind interactions.

Fix: `NoAnimate` on hand-positioned containers (§7). `Viewport` already
includes it.

### Extrinsic on a content row collapses height

Symptoms: a toolbar, find bar, or title strip has almost no vertical size
(or disappears), while siblings look fine.

Fix: `Extrinsic` ignores content on **all** axes. A row that only needs a
width budget should not use bare Extrinsic unless the parent also assigns
height (or you set `FixHeight` / a real height constraint). Prefer
content-height chrome (`Expand`, `Clip`, optional `MinHeight`) inside an
**extrinsic pane**; use `Viewport` only for the fill/scroll region. See
§4 (sizing).

### Content inflates a flex pane (scrollbar shoved off)

Symptoms: a long title, path, or text field makes the whole main column
wider than the remaining space; scrollbars or neighbors shift.

Fix: the pane that owns that budget needs `Extrinsic` (often with `Clip`),
not only `Grow`/`Expand`/`Clip`. `Clip` alone does not stop content-driven
measurement. See §4 (sizing).

### Too much work under `WithFrameLock`

Symptoms: UI stutters while background work runs.

Fix: compute outside the lock; publish only final results inside it.

### Forgetting `RequestNextFrame()`

Symptoms: background data changes, but the UI updates only after mouse
movement or another input event.

Fix: after publishing background data, call `RequestNextFrame()`.

### Rendering fresh structs directly

Symptoms: selection/history/expansion state is impossible to maintain.

Fix: a stable store; update objects in place (§12).

### Sample-count charts

Symptoms: chart density changes when the sampling rate changes.

Fix: bucket timestamped samples into fixed time windows (§14).

## 17. A good workflow for building a Shirei app

1. Define your durable app model.
2. If the data source is uncertain, build a terminal prototype first.
3. Create a simple Shirei shell: header, toolbar, main panel, detail panel.
4. Add the `--png` flag on day one (§3); render as you go.
5. Render static or fake data first if needed.
6. Add live background data with `WithFrameLock` and `RequestNextFrame`.
7. Use stable app-owned objects for rows; pointer ids.
8. Add selection (click selects; double-click acts).
9. Add filtering and sorting.
10. Add derived views such as tree mode.
11. Add charts or visual summaries.
12. Snapshot-test the screens you care about; polish layout and styling last.

The most successful Shirei apps share this shape:

```text
platform/data acquisition
    -> common processing
        -> stable app-owned model
            -> derived visible rows/charts
                -> immediate-mode rendering
```
