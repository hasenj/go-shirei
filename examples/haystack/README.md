# haystack

Search for text across a directory tree; matches stream into the UI as they are found.

![haystack](haystack.webp)

## What it does

Pick a folder, enter a query (literal, case options, whole word, or regex), and
run a search. Results appear in a virtualized list: path + line number, a few
lines of context, and actions to copy the path or open the file at that line in
an editor haystack detects (VS Code, Sublime, Zed, …).

- Past searches stay as tabs so you can flip back without re-running
- Include / exclude filename globs; optional `.gitignore` handling
- Pure Go walk and match — no `rg` / `grep` subprocess
- Status line: matches, files hit/scanned, elapsed time

## What it shows (shirei)

This is the example that best matches day-to-day “utility app” structure: plain
data, a form, tabs, a large list, and background work.

### App state is ordinary Go

```go
type App struct {
    pathInput, query string
    matchCase, wholeWord, regex bool
    // ...
    searches []*Search
    active   *Search
}
```

Widgets write into fields (`TextInput(&appData.query)`). A finished search is
another `*Search` on the slice. There is no binding layer.

See `gui.go`: `App`, `RootView`.

### Streaming results under the frame lock

The walker runs off the UI thread, appends hits under `WithFrameLock`, and uses
atomics for counters the status line can read without locking everything.
While a search is running, the status path calls `RequestNextFrame` so the UI
keeps ticking without user input.

See `search.go` (search worker) and `gui.go` (`StatusLine`, `ResultsList`).

### Virtual list with per-tab scroll

`VirtualListViewExt` takes a stable key (the `*Search`) and an
`OutScrollOffset` so each tab restores its scroll position. Row height is fixed
and spelled out as constants at the top of `gui.go` — the virtual list needs
that contract.

See `gui.go`: `ResultsList`, the `headerH` / `lineH` constants.

### Deferred tab close

Closing a tab while ranging the tab strip is done by recording the close and
applying it after the loop (same idea as ferry’s session tabs). Avoids mutating
the slice mid-iteration.

See `gui.go`: `TabBar`.

## Run it

```shell
go run .                      # inside examples/haystack; searches cwd
go run . --png out.png [q]    # optional query runs before the frame
```

Concepts in more depth: [tutorial.md](../../docs/tutorial.md).
