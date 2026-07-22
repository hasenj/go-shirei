# Virtual lists and Measure

How to scroll large collections in Shirei without building every row every
frame, and how row heights relate to `Measure`. Companion to
[tutorial.md](tutorial.md) (general overview); this document is only about
virtualization and measuring.

Reference programs:

- `demos/measure-list` — title + free-form body cards; **auto-height**
  (`ItemHeight` nil)
- `examples/hacker-news-reader` — feed + threaded comments; auto-height
- `demos/vlist-pin` — pin-to-top / pin-to-bottom while the list mutates
- `widgets.LogView` — streaming lines with pin-to-bottom policy on top of
  VirtualList

---

## 1. Why virtualize

A normal column of widgets builds **every** child each frame:

```go
for i := range items {
    row(items[i])
}
```

That is fine for dozens of rows. For hundreds or thousands it becomes work
proportional to list size: layout, text shaping, surfaces — even for rows that
are scrolled far off screen.

A **virtual list** only builds the rows that intersect the viewport (plus a
little slack for scrolling). Total content height is estimated or measured so
the scrollbar still represents the whole corpus.

In Shirei that widget is `widgets.VirtualListView` /
`VirtualListViewExt`.

---

## 2. The API

```go
import (
    . "go.hasen.dev/shirei"
    . "go.hasen.dev/shirei/widgets"
)

var listKey = new(int) // stable address for commands / Use state

VirtualListView(
    listKey,
    len(items),
    func(i int) any { return items[i].id }, // ItemKey
    nil,                                    // ItemHeight (optional)
    func(i int, width f32) {                // ItemView
        row(items[i], width)
    },
)
```

Or with full options:

```go
var scrollY, maxScroll f32

VirtualListViewExt(listKey, VirtualListAttrs{
    ItemCount:          len(items),
    ItemKey:            func(i int) any { return items[i].id },
    ItemHeight:         nil, // or a custom fn
    ItemView:           func(i int, width f32) { row(items[i], width) },
    OutScrollOffset:    &scrollY,
    OutMaxScrollOffset: &maxScroll,
})
```

| Field | Role |
|-------|------|
| **key** (`listKey`) | Identity for the list instance (`ContainerWithKey` + command target). Use a stable pointer into app-owned data, unique among live widgets. |
| **ItemCount** | How many logical rows. |
| **ItemKey** | Stable unique key per row (id, not index). Used for row identity and scroll bookkeeping. |
| **ItemHeight** | Height of row `i` at content `width`. **Optional** — see §4. |
| **ItemView** | Builds row `i` at content `width`. Only called for rows that need to be laid out this frame (visible / probes). |
| **OutScrollOffset** / **OutMaxScrollOffset** | Optional readouts after the list settles this frame (for pin policies, chrome, etc.). |

Put the list inside a **Viewport** (or a parent that already is one —
`VirtualListView` installs a viewport on its root). Give it remaining space
with `Grow(1)` / `Expand` as usual.

---

## 3. What the list does each frame

Roughly:

1. Learn content **width** (viewport minus scrollbar gutter).
2. Sample heights (top of the list, walk from an **anchor**, sometimes a
   **tail**) to estimate total content height and the first visible row.
3. Emit a spacer above the first visible row (`spaceBefore`).
4. For each visible row: fixed-size slot → call **ItemView**.
5. Emit a spacer below (`spaceAfter`) so the scrollbar range matches the
   estimated remainder.

Scrolling stays smooth by re-anchoring to a known (index, offset) pair and
walking with real heights, not by rebuilding the whole list.

You do **not** need to manage which indices are visible. You only supply count,
keys, optional heights, and how to draw one row.

---

## 4. Row heights: fixed, custom, or auto-Measure

Virtualization needs a height for rows that are **not** currently built as full
widgets in the live tree (off-screen rows still contribute to total scroll
range). Three patterns:

### 4a. Fixed height

```go
ItemHeight: func(i int, width f32) f32 { return 48 },
```

Cheapest. Use when every row is the same chrome (icon + single line, etc.).

### 4b. Custom / heuristic height

```go
ItemHeight: func(i int, width f32) f32 {
    // e.g. ShapeTextMax + padding math, or a table of known sizes
    return measureSomehow(items[i], width)
},
```

Historically this is what apps did for wrapping text: shape offline and add
padding. It is easy to drift from the real `ItemView` layout (pad, gap, icons,
nested cards) and produce **gaps or clipping** between rows.

### 4c. Auto-height (`ItemHeight` nil) — preferred for variable rows

```go
VirtualListView(listKey, len(items), itemKey, nil, itemView)
```

When `ItemHeight` is nil, the list calls:

```text
Measure(Vec2{width, 0}, func() { ItemView(i, width) })
```

for each height probe. The **same** builder paints the row and defines its
height, so pad/gap/wrap stay consistent.

There is **no height cache** inside VirtualList. Each probe measures again.
That matches the usual cost of re-shaping text for height heuristics, and
avoids stale heights when content or collapse state changes.

If measuring is too expensive for your corpus, pass an explicit `ItemHeight`
(fixed or cheap custom). Auto-height is the correctness default, not a
requirement.

---

## 5. What `Measure` is

```go
func Measure(maxSize Vec2, fn FrameFn) Vec2
```

`Measure` runs `fn` on a **fresh** `*UI` (new identity root, empty frame
buffers), with:

- root **MaxSize** = `maxSize` (zero component ⇒ unconstrained on that axis)
- `Host.WindowSize` set to the same budget (widgets that read window size see it)
- layout only (no surface present, no process-wide image/content sweeps)

Then it restores the previous active UI and returns the root’s resolved size.

Typical use for a row:

```go
h := Measure(Vec2{rowWidth, 0}, func() {
    // same tree as ItemView — no FixHeight from the list slot
    cardBody(item)
})[1]
```

(VirtualList does this for you when `ItemHeight` is nil.)

### Nested vs outside a frame

| Call site | Locking |
|-----------|---------|
| Inside `RunFrameFn` (including inside VirtualList) | Reuses the frame lock; swaps `ui` temporarily |
| Outside a frame (tests, tools) | Takes the frame mutex like `RunFrameFn` |

Always restores the previous active UI, including if `fn` panics.

### Resources

Fonts, shape caches, glyph bitmaps, images, etc. are **process-shared**
(`SharedResources`). Measure does **not** free them. A disposable measure
`FrameNumber` must not drive process reclaim — that is intentional.

---

## 6. Caveats (read these)

### 6.1 Ephemeral `Use` / identity state is invisible to Measure

Measure uses a **new identity tree**. Hooks like `Use("…")` on live nodes are
not present. Defaults apply unless the state lives on **app-owned data** that
`ItemView` reads.

**Do:**

```go
// collapse is app state, keyed by item id
if collapsed[item.ID] { /* shorter body */ }
```

**Don’t expect Measure to see:**

```go
// open is only on the live identity node
open := Use[bool]("open")
if *open { /* tall body */ }
```

The live row may expand after click while auto-height still measures the
default (collapsed) tree → wrong slot height. Put expansion, selection chrome
that changes size, “show more”, etc. on **app data** (maps, fields on the item,
etc.), same as the HN reader’s `collapsed` map.

### 6.2 Measure and paint must agree on the tree

Auto-height measures **ItemView**. Anything that only appears in paint (or only
in a separate height fn) will desync.

- Prefer **one** function for the row body.
- Avoid `FixHeight` inside the row that fights the list’s slot size; the list
  already sizes the slot to the measured height.
- Horizontal pad on the **same** container as wrapping `Label`/`Text` is a
  common trap: text wrap uses `MaxSize[0]`, not content box after pad. Nest a
  zero-H-pad column for text, or match measure carefully (see HN comment cards).

### 6.3 Interactions inside Measure

`ItemView` may call `PressAction`, `IsHovered`, `WantKeyboard`, etc. During
Measure there is no real pointer/focus world for that tree: hovers are false,
clicks do not stick. That is fine for pure layout. Side effects that mutate
app state inside `ItemView` can still run if the code path is taken without a
gesture guard — keep mutations behind `PressAction()` / explicit buttons.

### 6.4 Cost

Auto-height may call `Measure` many times per frame (average sample, anchor
walk, visible rows, tail). Each Measure builds a short-lived UI and runs layout.
For huge lists or heavy rows, either:

- keep rows cheap to build, or
- supply a cheap explicit `ItemHeight` for the common case.

There is no built-in LRU of heights; caching is **your** policy if you need it
(and you must invalidate on width, content, and expand/collapse).

### 6.5 Scroll commands and pin policy

The list is policy-free for pin-to-bottom:

- `VirtualListView_ScrollTo(listKey, offset)`
- `VirtualListView_ScrollToEnd(listKey, margin)`
- `VirtualListScrollIntoView(listKey, itemKey)`

Callers that pin (e.g. LogView) re-post commands each frame from
`OutScrollOffset` / `OutMaxScrollOffset`. See `demos/vlist-pin`.

### 6.6 ItemKey stability

Use a **stable id**, not the slice index, when the list can reorder, insert, or
filter. Index keys make scroll/identity jump when the data moves.

---

## 7. Minimal auto-height example

```go
type card struct {
    id    int
    title string
    body  string
}

var listKey = new(int)
var cards []card

func frame() {
    Container(Attrs(Viewport, Expand), func() {
        VirtualListView(listKey, len(cards),
            func(i int) any { return cards[i].id },
            nil,
            func(i int, width f32) {
                c := cards[i]
                Container(Attrs(Expand, Gap(6), Pad(12)), func() {
                    Label(c.title, FontWeight(WeightSemibold))
                    Label(c.body) // wraps from parent MaxWidth cascade
                })
            },
        )
    })
}
```

Run `go run ./demos/measure-list` for a longer variant (hundreds of
variable-length cards).

---

## 8. When to use what

| Situation | Approach |
|-----------|----------|
| Uniform row chrome | Fixed `ItemHeight` |
| Variable text / nested cards, correctness first | `ItemHeight` **nil** (auto Measure) |
| Variable rows but Measure too hot | Custom `ItemHeight` that stays in sync with `ItemView` (harder) |
| Expand/collapse changes height | App-owned state read by `ItemView` (not bare `Use` for size) |
| Pin to bottom while appending | App policy + `ScrollToEnd` / readouts (`LogView` pattern) |

---

## 9. Related APIs

| API | Package | Role |
|-----|---------|------|
| `VirtualListView` / `VirtualListViewExt` | `widgets` | Virtualized column |
| `Measure` | `shirei` | Fresh-UI layout-only size |
| `ShapeTextMax` | `shirei` | Offline text metrics (optional height helper, not required for auto-height) |
| `ActiveUI` / `GetHost` | `shirei` | Live UI / Host I/O |
| `SharedResources` | `shirei` | Process caches (shared with Measure) |
