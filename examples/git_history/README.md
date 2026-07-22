# git_history

Read-only git history and unified-diff viewer — a small native alternative to
flipping between `git log` and `git show`.

## What it does

- **Tabs:** Open several repos via **+ New** (folder browser) or the recents
  chevron menu (filterable). Session restores open tabs on startup (history
  loads lazily when you select a tab). Close with ×. Errors toast bottom-right.
- **Sidebar:** Commit history (short hash + subject; optional author, timestamp,
  and lazy `+n −m · k files` via the ⋮ menu), loaded in pages
  (`historyPageSize`) and extended as you scroll — unbounded. Display toggles
  persist in the session. **Working tree** / **Staging** rows appear when dirty
  (pure-Go status; hide when clean).
- **Header:** Commit subject/body/author plus `+n −m · k files`.
- **Diff:** All files stacked in one continuous virtualized list with colored
  add/del lines. Optional find bar (**⌘/Ctrl+F**) searches the whole stream
  (style-span highlights, prev/next; × clears; Esc dismisses). **Next file**
  (floating ↑/↓ bottom-right, or **P**/**N**): prev pins the last file header
  above the first visible row (start of current file, or previous file if
  already there); next jumps past the last file in view (end of stream when
  none remain).
  Drag to select text; Cmd/Ctrl+C to copy.
- **History filter:** Optional bar (**⌘/Ctrl+L**) narrows the commit list by
  hash, subject, or author (substring highlights in the list and commit header;
  × clears; Esc dismisses the bar, filter stays until the query is cleared).
  Keeps loading older pages while few matches.
- **Status bar:** Bottom strip with shortcut hints (and live match counts when
  filtering or diff-find is active).

Point it at a repo (cwd by default; walks up for `.git`):

```bash
go run .                 # GUI
go run . /path/to/repo
go run . --png out.png   # headless smoke frame
```

Refresh reloads the commit log. The work tree is also watched with
**fsnotify** (debounced): dirty slots update via pure-Go status when files or
the index change; the commit list reloads only when HEAD moves. Watches skip
`.git/objects`, ignored directories, and chmod-only noise. No stage/commit/push
— viewer only.

Commit history and commit diffs use **[go-git](https://github.com/go-git/go-git)**
(pure Go object cache — stepping commits stays fast). Dirty-slot **status** is
pure Go (`computeRepoStatusPure`: index + `lstat`, cache-tree for staging,
lazy gitignore walk for untracked) — stop criterion is wall time ≤ native
`git status --porcelain=v1` on the same repo (`go test -run TestStatusPerfVsNative`
and `go test -bench BenchmarkStatus`). go-git’s `Worktree.Status` stays ~10×
slower here and is not used. Working-tree/staging **diff text** still shells
out to `git diff` for now. Diff docs are cached on visit; one row ahead is
prefetched (`prefetchAhead`). While a load is in flight the previous diff
stays painted (no blank flash).
