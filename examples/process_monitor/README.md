# process_monitor

A small task manager: live process list, filter, tree or flat table, and
per-process CPU/RAM history.

![process_monitor](process_monitor.webp)

## Live process list and history charts

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

## Chart from containers

A small “chart” built only from ordinary containers — no canvas widget, no
chart library. Related: [tutorial.md](../../docs/tutorial.md) Part IV.

`UsageChart` turns a ~60s history into a row of bars. Each time bucket is a
`Grow(1)` column; the bar itself is an `Element` with a fixed height at the
bottom of that column (`Filler` pushes it down). Empty slots get a 1px baseline
instead of inventing data.

```go
// main.go — UsageChart (simplified)
Container(Attrs(Row, Expand, FixHeight(chartHeight), Gap(1)), func() {
    for _, b := range buckets {
        Container(Attrs(Grow(1), FixHeight(chartHeight), NoAnimate), func() {
            if !b.HasData {
                Filler(1)
                Element(Attrs(FixHeight(1), Expand, Background(0, 0, 82, 1)))
                return
            }
            ratio := f32(b.Value / scale) // 0..1
            barHeight := max(f32(1), ratio*chartHeight)
            Filler(1)
            Element(Attrs(FixHeight(barHeight), Expand, Background(hue, 75, 52, 1)))
        })
    }
})
```

The metric name floats over the bars so it does not steal layout space:

```go
Container(Attrs(Float(6, 6), InFront), func() {
    Label(title, FontSize(12), FontWeight(WeightBold), TextColor(0, 0, 0, 0.5))
})
```

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

Filter / tree / sort build an ordered `[]*Process`. `TableExt` owns header
chrome and `SortState`; the app owns which rows appear and in what order
(`ProcessTable`, `visibleRows`).

## Run it

```shell
go run .                              # inside examples/process_monitor
go run . -png out.png
go run . -once -limit 20 -sort cpu    # terminal report
```
