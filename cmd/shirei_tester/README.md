# shirei_tester

IDE-style snapshot test runner (usually for Shirei UI tests; any Go module
that uses the same patterns works).

```bash
# from the shirei module root:
go run ./cmd/shirei_tester
go run ./cmd/shirei_tester ~/code/maqam-gui

# install:
go install go.hasen.dev/shirei/cmd/shirei_tester@latest
```

## What it does

1. **Discovers** packages under the scan root (`go.mod` / `go.work`) by
   walking the filesystem for `*_test.go` that mention `TestSnapshot`,
   `ReportSnap`, or `layoutSnapshot` (no `go list`, no goldens on
   disk). Discovery runs in the background so the window opens immediately.
2. **Lists** every `Test*` by parsing `*_test.go` sources (no compile).
3. **Runs** all / package / single test with `go test -json` and
   `SHIREI_SNAP_REPORT` when the harness supports it.
4. **Shows** a wipe compare (actual vs golden, with diff highlight) and
   **Accept** when the report includes paths; otherwise you still get
   pass/fail and log output.

## Harness report (optional)

| Env | Meaning |
|-----|---------|
| `SHIREI_SNAP_REPORT` | Append-only JSONL path (`shirei.SnapEvent`) |
| `UPDATE_SNAPSHOTS=1` | Rewrite goldens (still reported as `updated`) |

Event shape (one JSON object per line):

```json
{"pkg":"/abs/pkg","test":"TestSnapshotFoo","name":"id","status":"mismatch","golden":"/abs/a.png","actual":"/abs/a.actual.png"}
```

`status`: `match` | `mismatch` | `created` | `updated` | `skip`

Emitted by `shirei.ReportSnap` / `shirei.Snapshot`. Other projects can emit
the same lines from an inlined helper.
