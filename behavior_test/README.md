# behavior_test

Windowed / headless harnesses for multi-frame UI behavior. Not part of
`go test ./...`.

## Run

```bash
# from shirei/
./behavior_test/run.sh                          # all headless
go run ./behavior_test/<name>                   # one headless
go run ./cmd/behavior_runner                    # GUI: build ahead, run with windows
```

## Window flags (`btmode`)

Every harness uses the same three flags:

| Flags | Meaning |
|-------|---------|
| *(default)* | Headless drive; PASS/FAIL on stdout; exit 0/1 |
| `--window --drive --close` | Open window, auto-drive on screen, SUCCESS/FAIL banner, exit |
| `--window --drive` | Auto-drive on screen; keep window open after verdict |
| `--window` | Manual; user operates the window |

`behavior_runner` Run all uses `--window --drive --close`.

When `--window` is set, drive runs inside `app.Run` so each step is visible.
Do not finish the suite via `RunFrameFn` before `SetupWindow` and only show a banner.

## Add a harness

1. Create `behavior_test/<name>/main.go` (directory name = row label).
2. Use `btmode.RegisterFlags` / `AfterParse` / `VerdictBanner` / `TickClose`.
3. Headless default must exit 0/1; support all three window modes.
4. Window+drive uses the same frame path as headless, with longer holds so steps are readable.
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

When `--window` is set, call `SetupWindow` **before** long drive work so
`behavior_runner`’s “running” row always has a visible child window.
