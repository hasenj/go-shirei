# ferry

Two-pane file manager for copying between your machine and servers over SSH/SFTP.

![ferry](ferry.webp)

## What it does

Ferry lists hosts from your SSH config (`~/.ssh/config` by default; `-F` to
override), opens sessions as tabs, and shows a **local** pane beside a
**remote** pane. Multi-select files (click / cmd / shift, drag-select, arrows)
and ferry them either direction. Transfers run in a global queue with progress;
when names collide you pick skip, merge, replace, or overwrite.

Browsing and previews use SFTP. Bulk copies do not: they stream a gzip’d tar
over an SSH exec session into a stage directory next to the destination, then
commit with renames. If anything fails, the stage is deleted and the
destination is left as it was — nothing half-written shows up on the far side.

Deletion on the remote is two-phase on purpose (`rm` has no undo):

1. **Move to bin** — reversible staging; rows stay in the listing with a trash
   stamp so pending deletes stay visible
2. **Delete N permanently…** — red, confirm dialog with the full path list; no
   Enter shortcut

Closing a tab with a non-empty bin warns first. The bin is per session (prod-a’s
staged deletes never appear under prod-b); the local pane is shared across tabs;
the transfer strip is global but tags each job with its server.

Also:

- Sortable listings; show/hide hidden files
- Collapsible text/image preview (and collapsible transfer / bin strips so they
  can share the bottom of the window without fighting)
- Host-key accept and password prompts as modals; host-key *mismatch* is a hard
  error, not “trust anyway”
- New remote folder; reconnect banner if a session drops

CLI for the same transport without the GUI: `ferry hosts`, `ls`, `head`,
`put`, `get`.

## What it shows (shirei)

The largest multi-surface example: screens, tabs, modals, collapsible panels,
heavy background I/O, and snapshot tests. Good reference when your app is more
than one main view.

### Modals as optional overlays

`RootView` always draws the chrome, then opens a modal only when the matching
request pointer is non-nil:

```go
if req := appData.hostKeyReq; req != nil {
    HostKeyModal(req)
}
// password, conflict, delete-confirm, leave-confirm, new-folder …
```

No modal manager type — presence of data *is* the open state.

See `gui.go`: `RootView`.

### Collapsible panels with stable body identity

`CollapsiblePanel` toggles a `*bool` and keys the body so height can animate
without the content identity thrashing. Used for transfers, delete bin, and
preview.

See `panels.go`: `CollapsiblePanel`.

### Network work never on the frame path

Connect, list directory, transfer, and preview load all run in goroutines and
publish into app state under the frame lock. The frame callback only reads
state and draws; directories show explicit loading states instead of blocking.

See `app.go` (reload / preview) and `transfers.go` (worker).

### Snapshot lists before iterating

shirei rebuilds the UI every frame, so a click handler that mutates a list a
widget is currently walking can corrupt that frame (e.g. “Restore” shrinking
the bin while the bin list is drawing). Mutators build fresh slices; views
snapshot the headers they will walk. The change lands cleanly on the next
frame.

See the delete-bin panel in `deletebin.go` / `gui.go`.

### Where state lives

Immediate mode does not invent ownership for you. Ferry’s rule of thumb:

| State | Scope |
|-------|--------|
| Local pane | Shared across tabs |
| Remote pane + delete bin | Per SSH session / tab |
| Transfer queue | Global (each job tagged with its server) |

Match the data to the lifetime you mean, instead of forcing everything into
one “app state” bag for symmetry.

## Run it

```shell
go run .                    # inside examples/ferry; uses ~/.ssh/config
go run . -F path/to/config
go run . --png out.png
go run . hosts              # CLI: hosts, ls, head, put, get
```

Tests and `dev_ssh_config` use throwaway hosts only — never point automated
runs at production machines. For local lima boxes: `-F ./dev_ssh_config`.
