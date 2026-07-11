# haystack

Search for text across a directory tree; matches stream into the UI as they are found.

![haystack](haystack.webp)

## Find-in-files with streaming results

Pick a folder, enter a query (literal, case options, whole word, or regex), and
run a search. Results appear in a virtualized list: path + line number, a few
lines of context, and actions to copy the path or open the file at that line in
an editor haystack detects (VS Code, Sublime, Zed, …).

- Past searches stay as tabs so you can flip back without re-running
- Include / exclude filename globs; optional `.gitignore` handling
- Pure Go walk and match — no `rg` / `grep` subprocess
- Status line: matches, files hit/scanned, elapsed time

## Stream under the frame lock

Workers never touch UI structs on the frame path. After scanning a file they
append matches under `WithFrameLock` and request a redraw. Counters that only
need to tick (matches / files scanned) are atomics so workers do not serialize
on the lock for every file.

```go
// search.go — after matching one file
WithFrameLock(func() {
    if s.cancelled.Load() {
        return
    }
    s.filesMatched.Add(1)
    s.matchCount.Add(int64(len(fileMatches)))
    g.Append(&s.matches, fileMatches...)
})
RequestNextFrame()
```

While a search is active, the status line also calls `RequestNextFrame` so the
elapsed timer and growing list keep updating without user input (`StatusLine`
in `gui.go`).

## Virtual list keyed per tab

Each search tab is its own list identity. `VirtualListViewExt` is keyed by the
`*Search`, and `OutScrollOffset` points at that search’s `scrollY` so switching
tabs restores the right offset.

```go
// gui.go — ResultsList
VirtualListViewExt(s, VirtualListAttrs{
    ItemCount:       len(matches),
    ItemKey:         func(i int) any { return matches[i] },
    ItemHeight:      func(i int, width f32) f32 { return rowHeight(matches[i]) },
    ItemView:        func(i int, width f32) { MatchRow(matches[i]) },
    OutScrollOffset: &s.scrollY,
})
```

Row height is computed from layout constants (`headerH`, `lineH`, …) at the top
of `gui.go` — the virtual list needs a height function it can call without
building the full row.

## App state is ordinary Go

```go
type App struct {
    pathInput, query string
    matchCase, wholeWord, regex bool
    searches []*Search
    active   *Search
}
```

Widgets write fields directly (`TextInput(&appData.query)`). A finished search
is another `*Search` on the slice — no binding layer (`gui.go`: `App`, `RootView`).

Tab close is deferred until after the tab loop so the slice is not mutated
mid-iteration (`TabBar` in `gui.go`).

## Run it

```shell
go run .                      # inside examples/haystack; searches cwd
go run . --png out.png [q]    # optional query runs before the frame
```

Concepts in more depth: [tutorial.md](../../docs/tutorial.md).
