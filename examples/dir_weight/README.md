# dir_weight

Disk-usage explorer: scan a directory tree and find what is taking the space.
The interface follows the system light or dark appearance.

![Dir Weight size tree, folder bars, and scan status split diagonally between light and dark modes](dir_weight.webp)

## Disk usage tree

Pick a folder or pass one on the command line. Scanning runs in the background.
Each node keeps its size directly above its name, with indentation and guides
showing the folder hierarchy. The background fill and nearby percentage show
that entry's share of its parent.

- Use the chevron to expand or collapse a folder.
- Select an entry to see its full path and use **Browse** or **Reveal** below the tree.
- Use **Minimum size** to hide small entries. The name filter searches descendants
  even inside collapsed folders and presents matches in descending size order.
  In filtered results, fills represent each entry's share of the summed result
  sizes; matching parents and children can overlap.
- Open several scans in tabs. Each keeps its filter, expansion, selection, and
  scroll position. Closing a tab cancels its scan.
- Read scan progress, folder counts, elapsed time, and the visible entry count
  in the bottom status bar. Inaccessible subfolders appear as a neutral skipped
  count; select it to inspect or copy the paths and reasons.

## Shared tabs

`TabStrip`, `TabItem`, and `TabCloseButton` in `shirei/widgets` provide the same
flat tabs used by Haystack. The application owns tab order and selection and
applies close requests after rendering the strip. Their `Styled` variants accept
literal colors through `TabStyle`; the ordinary functions resolve the current
color scheme.

## Tree rendering

Visible entries are collected into a flat slice (`ListupViewableEntries`) and
rendered by `VirtualListViewExt` at a fixed row height. Expanding a folder changes
that slice; it does not create nested scroll views. Only visible rows are built.

`SizeTreeRow` draws the size fill as a floating `Element` with `Behind`, using a
fraction of the row's resolved width. Labels retain the full row width, so small
entries remain readable. Indentation is capped to leave room for names in deep
trees. Fills, guides, text, and selection use the current color scheme.

## Background scan under the frame lock

Workers read the filesystem outside the UI thread (`buildDirDraft`) into a
private list of children. They attach that list, update counters, and roll sizes
under `WithFrameLock` (`publishDirDraft` → `updateSizeAndStateAndSorting`).
Closing a tab sets an atomic cancel flag so in-flight jobs never promote the
scanner to `Done`. Hard links use atomic `LoadOrStore`; directory cycles are
blocked via `seenDirs` and skipping symlink-like modes.

## Run it

From the repository root:

```shell
go run ./shirei/examples/dir_weight
go run ./shirei/examples/dir_weight ~/Library ~/Downloads
go run ./shirei/examples/dir_weight -png out.png ~/Downloads
```
