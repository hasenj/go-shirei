# Drag and Drop in Shirei

How to move items between drop zones with the `widgets` drag-and-drop API.
Companion to [tutorial.md](tutorial.md) (the general overview); this
document covers only item DnD.

Reference demos:

- `demos/balls-buckets` — balls into lettered buckets (and back out)
- `demos/kanban` — cards between lane columns

---

## 1. Two different “drags”

The main tutorial’s **Dragging** section (`PressAction` / `IsActive` +
`FrameInput.Motion`) is for *pointer capture*: splitters, panning a custom
canvas, scrubbing a slider thumb. That path does not transfer application
data between widgets.

**Item drag-and-drop** is a separate mechanism in `shirei/widgets`:

| Concern | Tool |
|---------|------|
| Which widget is being dragged | `CurrentId()` (opaque `ContainerId`) |
| What data the drag carries | **payload** argument to `DragAndDrop` |
| What drop zone was hit | **target** argument to `CanDropHere` |

Payloads and targets are ordinary Go values you pass in each frame — typically
small typed ids (`BallPayload("red")`, `BucketTarget("A")`) that your drop
handler uses to update app state.
---

## 2. The pieces

```go
import . "go.hasen.dev/shirei/widgets"

// On the draggable item (inside its container):
if DragAndDrop(payload) {
    // mouse released over a zone that accepted this payload type
    target := GetDropTarget[TargetType]()
    // mutate your app state
}

// On each drop zone:
if CanDropHere[PayloadType](target) {
    // true while this zone is the active drop target — use for highlight
}

// Optional feedback on the item itself:
if IsDragging() { /* dim / restyle the source */ }

// Optional floating ghost (usually at the end of the frame’s UI):
if item, ok := GetDraggingItem[PayloadType](); ok {
    rect := GetDraggingItemRect()
    // Float is parent-relative; rect is surface-absolute (MousePoint space):
    //   origin := Vec2Sub(rect.Origin, GetRenderData().ResolvedOrigin)
    //   FloatVec(origin), FixSizeVec(rect.Size), ClickThrough, …
}
```

- `DragAndDrop(payload)` — call every frame on the item. Arms on mouse-down
  and only begins a real drag after a small movement threshold (so clicks
  and double-clicks are not swallowed). Returns `true` once on successful
  drop. Double-click presses (`ClickCount >= 2`) do not arm a drag.
- `CanDropHere[Accept](target)` — call every frame on the zone. Registers
  `target` while hovered **only if** the active drag’s payload is type
  `Accept`. Returns whether this zone is currently the drop target.
- `GetDropTarget[T]()` — read the zone’s `target` in the drop handler.
- `GetDraggingItem[T]()` / `GetDraggingItemRect()` — for ghosts and status.

Define distinct named types for payload and target so the type parameter on
`CanDropHere` filters correctly (e.g. only balls drop on buckets):

```go
type BallPayload string
type BucketTarget string
```

---

## 3. Minimal pattern (from balls-buckets)

App state stays yours — e.g. each ball has a `Bucket` field (`""` =
unassigned). The view only renders and wires DnD:

```go
// Drop zone
target := BucketTarget("A")
ContainerWithKey(target, bucketAttrs, func() {
    if CanDropHere[BallPayload](target) {
        ModAttrs(/* highlight */)
    }
    // … children, including balls currently in this bucket …
})

// Draggable item
payload := BallPayload(ball.Id)
ContainerWithKey(payload, ballAttrs, func() {
    if IsDragging() {
        ModAttrs(/* faded source */)
    }
    if DragAndDrop(payload) {
        ball.Bucket = string(GetDropTarget[BucketTarget]())
    }
    Label(ball.Name)
})
```

On drop, mutate durable state (`ball.Bucket = …`). The next frame redraws
the ball under the new parent. No widget object graph to update.

A tray that “unassigns” is just another zone whose target is
`BucketTarget("")` (or any sentinel your state understands).

---

## 4. Ghost preview

While a drag is active, draw a floating copy that follows the pointer.
Do this **after** the main layout so it stacks above content; mark it
`ClickThrough` so it does not steal hover from drop zones:

```go
if payload, ok := GetDraggingItem[BallPayload](); ok {
    rect := GetDraggingItemRect()
    // rect.Origin is surface-absolute; Float is parent-relative.
    origin := Vec2Sub(rect.Origin, GetRenderData().ResolvedOrigin)
    ContainerWithKey("dnd-ghost", ballAttrsFor(payload), func() {
        ModAttrs(NoAnimate, FloatVec(origin), FixSizeVec(rect.Size),
            ClickThrough, Trans(0.55))
        Label(nameFor(payload))
    })
}
```

`GetDraggingItemRect()` tracks the origin captured at mouse-down plus
accumulated `FrameInput.Motion` (surface space, same as `MousePoint`).

---

## 5. Rules of thumb

**Hover includes ancestors.** `CanDropHere` uses `IsHovered()`, which is
true for a zone when the pointer is over it *or* a descendant (e.g. a card
inside a lane). Put `CanDropHere` on the zone container that should accept
the drop, not only on empty padding.

**Ordered lists: prefer one zone + geometry.** Gaps between items are not
part of either child, so per-item `CanDropHere` misses “drop between.”
Use a single stable target on the list/lane, compute the insertion index
from pointer Y vs item midpoints (previous-frame rects via
`GetRenderDataOf`), and draw markers in layout gaps. See `demos/kanban`.

**Targets should be comparable.** Clearing the active zone compares
`DropTarget == target` with Go interface equality. Typed strings/ints work;
avoid non-comparable payloads as targets.

**One drag at a time.** State is process-global in the widgets package —
fine for a single window’s UI.

**Stable item identity.** When the same logical item can appear under
different parents across frames (a ball moving from tray to bucket), give
its container an explicit key via `ContainerWithKey` so hover/drag identity
follows the item. That key is separate from the DnD payload; demos often
pass the same value to both for convenience.

**Not for OS file drops.** This API is in-app item transfer. Platform file
open/drag from Finder/Explorer is a different path (if/when exposed by the
backend).
---

## 6. API checklist

| Function | Where | Role |
|----------|-------|------|
| `DragAndDrop(payload)` | item | start/continue drag; `true` on drop |
| `CanDropHere[Accept](target)` | zone | accept + highlight |
| `IsDropTarget(target)` | anywhere | active target? (markers in gaps) |
| `IsDragging()` | item | source is the active drag |
| `GetDropTarget[T]()` | on drop | zone payload |
| `GetDraggingItem[T]()` | anywhere | active item payload |
| `GetDraggingItemRect()` | ghost | floating rect |

Full examples: `go run ./demos/balls-buckets` and `go run ./demos/kanban`.
