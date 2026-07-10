# process_monitor

A small task manager: live process list, filter, tree or flat table, and
per-process CPU/RAM history.

![process_monitor](process_monitor.webp)

## What it does

Shows processes for the current machine with columns for PID, CPU, RSS, memory
share, user, state, threads, and name. Click a column header to sort (again to
reverse). Select a row for detail and ~60s rolling CPU/RAM charts.

- Filter by name, command line, user, or PID
- Flat list or parent/child tree (collapse with ▸/▾; filter keeps ancestors)
- Sample interval 0.5s / 1s / 2s / 5s
- Metrics that could not be read show `--`, not a fake zero
- Same UI on macOS, Linux, and Windows (`procinfo` collectors underneath)

There is also a headless terminal mode (`-once`) that prints a sorted report
and exits — useful for quick checks without opening a window.

## What it shows (shirei)

How to keep a live “entity” set (processes) stable across samples, drive a
generic table, and draw simple charts from layout primitives. Walkthrough of
the same ideas: [tutorial.md](../../docs/tutorial.md) Part IV.

### Sampler goroutine + frame lock

A loop samples the OS, then under `WithFrameLock` updates `appData` and the
store, then `RequestNextFrame()`. The UI never collects processes on the frame
path.

See `main.go`: `startSamplerLoop`.

### Stable store for live entities

`ProcessStore` keys processes by identity that survives PID reuse
(PID + start time), keeps short history for charts, and retains a selected
process for a bit after it exits. The table binds to `*Process` pointers from
that store, not to ephemeral snapshot rows.

See `process_store.go`.

### `TableExt` with external row order

Filter/tree/sort build an ordered `[]*Process`; the table widget owns header
chrome and `SortState`, but the app owns the ordering. That split is what you
want when “visible rows” is more than “sort the raw slice.”

See `main.go`: `ProcessTable`, `visibleRows`.

### Charts from bars, not a chart library

`UsageChart` resamples history into columns and draws each sample as a short
bar container (plus a floated title). Same immediate-mode style as the rest of
the UI.

See `main.go`: `UsageChart` / `resampleHistory`.

## Run it

```shell
go run .                              # inside examples/process_monitor
go run . -png out.png
go run . -once -limit 20 -sort cpu    # terminal report
```
