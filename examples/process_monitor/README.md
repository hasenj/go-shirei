# process_monitor

A small task manager: live process list, filter, tree or flat table, host
CPU/memory, and per-process CPU/memory/energy history. The compact layout follows the system
light or dark appearance.

![Process Monitor table and resource-history inspector split diagonally between light and dark modes](process_monitor.webp)

## Live process list and history charts

Shows processes for the current machine with columns for PID, CPU, RSS, memory
share, uptime, power (when the OS can measure it), user, state, threads, and
name (with the program icon when the OS provides one). Click a column header to sort
(again to reverse). Select a row for a compact bottom inspector with command, executable, parent,
start time, and ~60s CPU/memory/energy charts. The Details menu includes the working
directory. Click the row again or × in the inspector to close it.

- The filter stays visible: search name, command line, user, or PID. ⌘F / Ctrl+F
  focuses it; Esc or × clears it
- Scope the list to all processes, running processes, or pinned processes
- Pin a process (icon on the name, or Pin in the details panel) to keep it
  at the top of the sort — including among tree siblings — and after it exits
- End the selected process (confirm the second click); permission errors
  surface as a toast
- Flat list or parent/child tree (collapse with ▸/▾; filter keeps ancestors)
- Refresh every 1s / 2s / 5s / 10s
- Pause/resume sampling with the toolbar button or Space when no control has focus.
  Filtering, sorting, and inspecting remain available while paused
- Metrics that could not be read show `--`, not a fake zero (power is
  best-effort: macOS task energy, Linux RAPL share when readable)
- Exited processes stay for ~10s (and while selected) and render faded as "exited"
- Same UI on macOS, Linux, and Windows (`procinfo` collectors underneath)

There is also a headless terminal mode (`-once`) that prints a sorted report
and exits — useful for quick checks without opening a window.

## Chart from containers

A small “chart” built only from ordinary containers — no canvas widget, no
chart library. Related: [tutorial.md](../../docs/tutorial.md) Part IV.

`UsageChart` plots a minute of history as a thin step line with a faint area fill.
`historyPlot` places horizontal segments and vertical connectors using `Float`
and `FixSize` inside a clipped container. The grid uses the same primitives.
Metric labels and adaptive scale limits sit above each chart; time labels sit
below. Empty leading slots remain blank.

History is irregularly sampled; `resampleHistory` folds it into fixed 1s
buckets (average in a slot; linear fill between real slots) so the x-axis is
always “last N seconds,” not “last N samples.” Full code: `UsageChart` /
`resampleHistory` in `main.go`.

## Background samples, UI only reads

The sampler runs off the frame path. It takes the OS snapshot, then publishes
under `WithFrameLock` so the next frame sees a consistent `appData` / store.

```go
// main.go — startSamplerLoop
snap, err := sam.Sample()
WithFrameLock(func() {
    appData.snapshot = snap
    appData.err = err
    appData.store.Update(snap, appData.selected)
})
RequestNextFrame()
```

## Stable processes + external table order

`ProcessStore` keys rows by identity that survives PID reuse (PID + start
time) and keeps short history for the charts. The table binds to `*Process`
pointers from the store, not to one-shot snapshot rows (`process_store.go`).

Filter / tree / sort build an ordered `[]*Process`. `TableStyled` owns header
chrome and `SortState`; the app owns which rows appear and in what order
(`ProcessTable`, `visibleRows`).

## Run it

```shell
go run .                              # inside examples/process_monitor
go run . -png out.png
go run . -once -limit 20 -sort cpu    # terminal report
```
