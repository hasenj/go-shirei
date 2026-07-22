# Layout shell (tutorial samples)

Self-contained programs for [docs/layout-tutorial.md](../../docs/layout-tutorial.md).

```bash
go run ./demos/layout-shell/step05   # compose slot first appears
go run ./demos/layout-shell/step09   # intentional compose bug
go run ./demos/layout-shell/step10   # Extrinsic fix
go run ./demos/layout-shell/step11   # Viewport helper
go run ./demos/layout-shell/step14   # VirtualList at scale
./demos/layout-shell/gen-pngs.sh

# Follow-up (separate tutorial — custom widgets, not layout):
go run ./demos/layout-shell/step15a  # custom send circle, default field
go run ./demos/layout-shell/step15   # full custom compose
go run ./demos/layout-shell/step16   # dark shell + SetDefaultScrollBar
# docs/custom-widgets-tutorial.md
```

Each `stepNN` is a full `main` package. Window / PNG size is **1100×720**.

| Steps | Theme |
|-------|--------|
| 01–04 | Outer shell |
| **05** | Main: header · messages · **compose** |
| 06–08 | Labels, servers, channels |
| **09** | Wrong: `Grow`+`Clip` → compose half-cut |
| **10** | **`Extrinsic`** fix |
| **11** | **`Viewport`** helper |
| 12–13 | Members + light polish |
| 14 | VirtualList for long messages/members |
| 15a | Custom send + default field — [custom-widgets-tutorial.md](../../docs/custom-widgets-tutorial.md) |
| 15 | Full custom compose (same tutorial) |
| 16 | Dark theme + modern default scrollbar (same tutorial) |
