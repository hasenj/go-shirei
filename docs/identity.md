# Container Identity

Shirei redraws your whole UI every frame. That raises a practical question:
when a row scrolls, stays hovered, or keeps a text field’s caret, how does
shirei know it’s looking at the *same* container as last frame?

This document aims to answer these questions, and explain when you can ignore it,
and when you must give items an explicit key.

---

## 1. The short version

Most of the time you write `Container(...)` and nothing special is needed.
Shirei matches containers by **where they sit** in the tree: “the first
button under this panel,” “the third row in this loop,” and so on.

That works for fixed layouts and for lists that don’t change order or
membership.

When items can appear, disappear, reorder, or move between parents — and
you care that *each item* keeps its own scroll position, focus, or widget
state — give that item an explicit key with `ContainerWithKey`.

```go
// Good for a list that can sort, filter, or reorder:
for _, item := range items {
    ContainerWithKey(item.Id, Attrs(...), func() {
        // ...
    })
}
```

Use something stable that names the *data* (an id field, a pointer to your
struct) — not the loop index `i`, which changes meaning when the list
shuffles.

---

## 2. What goes wrong if identity is wrong

If shirei thinks two different items are “the same” container (or loses
track of one), you may see:

- hover or focus jumping to another row after a sort
- a scrolled panel jumping back to the top
- a text field forgetting its caret or selection
- a brief animation glitch as if the row were brand new

Those symptoms usually mean a dynamic list needed `ContainerWithKey`, or
used a key that wasn’t unique / wasn’t stable.

---

## 3. Explicit ids (`ContainerWithKey`)

```go
ContainerWithKey(item.Id, attrs, func() { ... })
```

The first argument is a **key**: any comparable Go value you own — a
string, an int, a pointer to your object. Fresh strings each frame are
fine (`fmt.Sprintf("row-%d", id)`).

**Rules that matter in practice:**

- **Unique among siblings.** Under one parent, don’t use the
  same key twice in the same frame. (The same key under *different*
  parents is fine — two panels can each have a `"header"`.)
- **Stable for the item.** Prefer `item.Id` or `&item` over the position
  in the slice.
- **Follows the item.** If a card moves from one column to another
  (drag-and-drop), keep the same key so its identity travels with it.

You only need this for containers whose *per-item* UI state should stick
to the data. Static chrome (title bars, fixed toolbars) can stay plain
`Container`.

---

## 4. Things that appear and disappear

A common worry: “If I show a banner above my list, do all the rows get new
ids and lose their state?”

**Usually no.** Optional UI that is a *different* piece of code than the
list rows — a banner function, a divider, an empty-state message — does
not scramble the rows below it. Turning that banner on or off is safe
without keys on the rows.

**Where you do need care:** a list of similar rows that can grow, shrink,
or reorder. Then positional matching (“third row”) is the wrong idea —
the third *visible* row isn’t always the same *item*. Key those rows.

| What you do | What happens to the other items |
|-------------|----------------------------------|
| Show/hide a banner or toolbar above a list | List rows keep their identity |
| Insert/remove/reorder similar list rows without keys | Later rows can “inherit” the wrong state |
| Filter a keyed list (some items not drawn this frame) | Other keyed items are unaffected |
| Bring a keyed item back | It is recognized as the same item again |

**Practical rule:** if the children are “many of the same kind of thing”
and the set can change, use `ContainerWithKey`. If you’re toggling
unrelated chrome around a stable list, you’re fine.

---

## 5. Two different “ids” (don’t mix them up)

| What | What it’s for |
|------|----------------|
| **Key** — the value you pass to `ContainerWithKey` | Tells shirei “this is item X” across frames |
| **`ContainerId`** — returned by `Container` / `ContainerWithKey`, or from `CurrentId()` | A handle you pass to helpers like “is this focused?” or “where is this on screen?” |

You pick the key from your app data. Shirei gives you the `ContainerId`
when you need to ask questions about that container later.

---

## 6. Checklist

- Fixed layout, nothing reordering → plain `Container` is enough.
- List that sorts, filters, or virtualizes → `ContainerWithKey` per item.
- Key by the item’s real identity, not by `i`.
- Don’t reuse the same key twice under one parent in one frame.
- Optional banners/dividers above a list usually don’t require re-keying
  the list.
- If hover/focus/scroll “follows the wrong row” after a data change, add
  or fix keys first.
