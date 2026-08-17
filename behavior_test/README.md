# behavior_test

Windowed harnesses for multi-frame UI behavior. Not part of `go test ./...`.
Every test opens a window so interaction and rendering run together.

## Run

```bash
# from shirei/
./behavior_test/run.sh                          # all tests, --close
go run ./behavior_test/<name>                   # one test; stay open after verdict
go run ./behavior_test/<name> --close           # one test; exit after verdict
go run ./behavior_test/<name> --manual          # playground; no auto-drive
go run ./cmd/behavior_runner                    # GUI: build ahead, run with windows
```

## Window flags (`btmode`)

Every harness uses the same flags:

| Flags | Meaning |
|-------|---------|
| *(default)* | Open window, auto-drive, stay open after verdict |
| `--close` | Auto-drive, SUCCESS/FAIL banner, then exit |
| `--manual` | Playground; user operates the window |

`--drive` is on by default. `--window` is accepted and ignored (a window always opens).

`behavior_runner` Run all uses `--close`.

Drive runs inside `app.Run` so each step is visible. Call `SetupWindow` before long drive work so the runner’s “running” row always has a visible child window.

## Add a harness

1. Create `behavior_test/<name>/main.go` (directory name = row label).
2. Use `btmode.RegisterFlags` / `AfterParse` / `VerdictBanner` / `TickClose`.
3. Always open a window; default auto-drive stays open; support `--close` and `--manual`.
4. Drive inside `app.Run` with holds long enough that steps are readable.
5. No registration step — `run.sh` and the runner discover `*/main.go`
   (except `btmode/`).

Prefer one clear user-visible property (scroll reaches true end, large file
tip paints without hang, stream stays pinned, …). Drive with synthetic
`FrameInput` / `InputState`, not real OS events.

## Fixtures

Large corpora live under the monorepo’s gitignored `resources/data/`
(e.g. `large200mb.txt`, `large10mb.txt`, `large100mb.txt`). Do **not**
generate multi‑hundred‑MB files in the harness. If a file is missing, fail
with a clear path message. `text-view-large` cycles 200→10→100 by default.
