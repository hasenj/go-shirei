# haystack

Search for text across a directory tree; matches stream into a compact native UI
that follows the system light or dark appearance.

![Haystack grouped search results split diagonally between light and dark modes](haystack.webp)

## Find in files

Choose a folder, enter a query, and press Enter or Search. Literal search supports
case matching and whole words; Regex enables regular expressions. Include and
exclude globs narrow the files, with optional `.gitignore` handling.

- Each search stays in its own tab. Selecting a tab restores its query, filters,
  selection, collapsed files, and scroll position.
- Results share one header per file. Click a header to collapse or expand its
  snippets; matching text is highlighted in amber.
- Click a code line to select it. The file's **Open in…** menu opens that line in
  an installed editor (VS Code, Sublime, or Zed). With no line selected in that
  file, it opens the first match. The copy button copies the file path.
- Cmd+F / Ctrl+F focuses the search field. Enter in the query or glob fields runs
  a new search.
- The footer reports matches, files hit/scanned, and elapsed time.

Matching uses [`go.hasen.dev/textsearch`](../../../textsearch), a pure-Go engine
with no `rg` or `grep` subprocess.

## Streaming and virtualization

The worker pool scans files in the background. Each complete file's matches are
published under `WithFrameLock`, followed by a redraw request. Counters are
atomic. The frame path reads a consistent snapshot and keeps the live status
updated while a search runs (`search.go`, `StatusLine`).

`appendResultRows` transforms newly published files into a flat sequence of file
headers, code lines, and snippet separators. It references the engine's `Match`
and `ContextLine` data without copying source text. Collapsing a file rebuilds
this display sequence with that file's code omitted.

`VirtualListViewExt` builds only visible rows, even when one file has a large
contiguous match block. Fixed row heights keep scrolling independent of source
line length. The list is keyed by `*Search`; each tab saves its first visible row
and restores it on activation (`gui.go`).

## Run it

```shell
go run .                              # inside examples/haystack
go run . -query RequestNextFrame ../../widgets
go run . -query hello -png out.png testdata/sample
```

More Shirei concepts: [tutorial.md](../../docs/tutorial.md).
