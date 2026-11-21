# UDP drive commands

The windowed app can listen on a loopback UDP port and apply **one command
per datagram**. This file is that command list.

How to stamp names on containers and write `go test` clients is
[drive-tutorial.md](drive-tutorial.md). The Go helper is `shirei/drive`.

---

## Wire

Call `app.SetupDrive()` after `SetupWindow` and before `Run`. It reads
`SHIREI_DRIVE_PORT` (set by `drive.Start`, or by a human:
`SHIREI_DRIVE_PORT=4372 go run .`) and, if the value is a positive
port, binds a loopback listener. No port is a no-op. It also calls
`SetupQuiet` unless `SHIREI_DRIVE_FOREGROUND` is on, so `go test` does
not steal the editor.

`AcceptInputCommands(port)` is the lower layer: bind this port, no
quiet window, no env. Use it when you want to pick the port or the
conditions yourself. Non-loopback senders are dropped. The app does
not listen unless something calls one of these.

Each datagram is one UTF-8 line. The reply fits in **1024 bytes** (the
`udplib` buffer). A `query` with many hits includes only whole records
that fit; it never sends a torn last record. Inject commands reply an
empty datagram (delivery is the success flag). Unknown commands and
bad arguments reply `error: …`.

The handler runs under the frame lock and does **not** wait for another
produce. Pointer and key commands write `Host.Input` / `FrameInput` and
request a frame; `query` / `show` / `screenshot` read the last completed
frame.

Each listener selects the window that UI-scoped commands apply to with the
`window` command; the default is the main window. When no window is
registered (headless tests, and backends that do not register a native
window), commands apply to the active UI. See [Windows](#windows).

Coordinates are the same space as `rect` on a record (logical points, origin
top-left of the selected window's content).

---

## Commands

| Command | Reply | Effect |
|---------|--------|--------|
| `ping` | `pong` | Liveness. `drive.Start` waits on this, then 500ms for the window. |
| `windows` | `count: N` then records | List the process's windows: `id`, `kind`, `title`, `size`, `pos`, `target`. |
| `window` | handle or empty | Current target handle (`main` by default). |
| `window handle` | `ok` or `error: no such window: …` | Select the window that UI-scoped commands apply to on this listener. |
| `move x y` | (empty) | Move the pointer; `FrameInput.Motion` is the delta from the previous point. |
| `mouse_down [button]` | (empty) | Mouse press this frame. Button: `primary` (default), `secondary`, `tertiary` (aliases `1`/`left`, `2`/`right`, `3`/`middle`). |
| `mouse_up [button]` | (empty) | Mouse release this frame. Same button names. |
| `wheel dy` | (empty) | Vertical scroll this frame. |
| `key_down Name [mods…]` | (empty) | Key down this frame: `FrameInput.Key` and add to `DownKeys`. |
| `key_up Name [mods…]` | (empty) | Key up: remove from `DownKeys`. Does not set `FrameInput.Key`. |
| `text s` | (empty) | Text this frame (as if typed / IME). `s` may be a Go-quoted string. |
| `query [q]` | `count: N` then records | N is every match; following records are those that fit. |
| `count [q]` | `N` | Number of matches this frame. Same lookup as `query`. |
| `show q` | one record or `error: no match` | First match. `q` is a name path or `#N`. |
| `focused` | `#N #M …` or empty | Identity serials: leaf, then ancestors to root. |
| `hovered` | `#N #M …` or empty | Hover stack: direct hit, then ancestors. |
| `screenshot path` | `ok` | Write a PNG of the last completed frame (`SoftRenderer`, same as `--png`). Path is the rest of the line (app filesystem). `drive.Shot` uses this when `SHIREI_DRIVE_TRACE` is set. |
| `profile_start path` | `ok` | Start a process-wide CPU profile. Path is the rest of the line, relative to the app working directory or absolute. Fails if any CPU profiler is already running. |
| `profile_stop` | `ok` | Stop the drive-owned recording, flush it, and publish it at the requested path. Fails if no drive recording is active. |
| `quit` / `exit` | (empty) | `generic.ExitWithCleanup(0)` on a new goroutine. |

`query` with no matches is `count: 0`. `count` with no matches is `0`.
`show` with no match is `error: no match`. `focused` / `hovered` with
none is empty.

---

## Windows

Every native window has a stable **handle**. The main window is always `main`.
Auxiliary windows use the handle declared in `app.PopupWindowOptions.Handle`,
or a generated `popup-1`, `popup-2`, … when none is declared. Handles are unique
among live windows; a closed window's handle can be reused.

`windows` lists them:

```text
windows
→ count: 2

id: main
kind: main
title: Shirei / Drive
size: 640 400
pos: 120 160
target: true

id: busy_alarm_popup
kind: popup
title: High CPU
size: 360 120
```

One block per window, in registration order (main first, then auxiliary
windows in creation order), with the same packing rules as `query`: `count` is
every window, and the blocks are the complete records that fit in the datagram.
Fields:

| Field | Meaning |
|-------|---------|
| `id` | Window handle |
| `kind` | `main` or `popup` |
| `title` | Native title; omitted when empty |
| `size` | Content size in logical points |
| `pos` | Window top-left in screen logical points, origin at the top-left of the primary display (same space as `PopupWindowOptions.ScreenPosition` and `ext/window.Position`). Omitted where the platform cannot report a global position, such as ordinary Wayland toplevels. |
| `target` | `true` on the listener's current selection |

`window` reads or selects the target:

```text
window                 → main              (current target; empty before any window exists)
window busy_alarm_popup → ok
window main            → ok
window nope            → error: no such window: nope
```

Selection is sticky per listener: `move`, `mouse_down` / `mouse_up`, `wheel`,
`key_down` / `key_up`, `text`, `query`, `count`, `show`, `focused`, `hovered`,
and `screenshot` then operate on the selected window — its content coordinates,
its last completed frame. `ping`, `windows`, `window`, `profile_start`,
`profile_stop`, and `quit` are process-wide. The default target is `main`; if
the selected window closes, the target reverts to `main`. When no window is
registered at all (headless `AcceptInputCommands` tests), UI-scoped commands
apply to the active UI as before.

`drive.Windows(port)`, `drive.SelectWindow(port, handle)`,
`drive.CurrentWindow(port)`, and `drive.WaitWindow(port, match)` wrap these
commands. Every other `drive` helper keeps its signature and uses the
listener's selected window.

---

## CPU profiles

```text
profile_start /tmp/haystack-scroll.pprof
move 500 500
wheel 100
profile_stop
```

Recordings write to a temporary file in the destination directory. A successful
stop atomically renames it to the requested path, replacing an existing file.
The directory must exist. Spaces in the path are literal; no quoting is needed.
Normal app exit through `generic.ExitWithCleanup` also finishes an active drive
recording. A forced kill cannot finish it. If publishing fails, the error names
the temporary file so it can be recovered.

These commands do not enable the profiler panel or request continuous frames.
Drive the interaction being measured between start and stop. CPU profiling
includes all Go goroutines, including background search and rendering work.
The commands cannot stop a recording started by the profiler panel or app code.

---

## Keys

Named keys (case-insensitive):

`tab`, `space`, `enter` / `return`, `escape` / `esc`, `left`, `right`,
`up`, `down`, `home`, `end`, `backspace`, `delete`, `pageup`, `pagedown`,
`f1`…`f12`.

A single letter or digit is that key (`key_down a`, `key_down A`, `key_down 3`).

Modifiers after the name, any subset:

`shift`, `alt`, `ctrl` / `control`, `cmd` / `command`, `primary`
(platform primary: Cmd on macOS, Ctrl elsewhere).

`key_down` is the down edge and holds the key in `DownKeys` until
`key_up`. A frame that saw input always requests one follow-up produce
(empty `FrameInput`, keys still held). `drive.Key` is a tap: `key_down`
(`Pause`), `Tick`, `key_up` (`Pause`). `Pause` is 150ms. `Tick` /
`Frames` sleep ~1/60s; they are not kernel commands.

---

## Query strings

`q` is space-separated names. The last token is the leaf; earlier tokens
are assigned ancestors (not necessarily immediate). Exact match.

A `q` of the form `#N` (no spaces) is an identity serial (one node or
none), not a name.

```text
query save
query toolbar save
show #12
focused
hovered
```

`query` with no argument matches every assigned node this frame, in tree
order. A virtual list only includes rows that were built.

The reply always starts with `count: N` — **N is every match**, not the
number of records that follow. Records after the blank line are as many
whole nodes as fit in the datagram. `Count > len(Nodes)` means the rest
did not fit.

```
count: 40

path: proc
rect: 1 2 3 4
value: 1
```

`drive.Query` returns `QueryResult{Count, Nodes}`. Several nodes may
share a name. Distinguish them with `value` on a record.

---

## Record format

One node per block. After `query`'s `count` line, blocks are separated
by a blank line. Fields that are false, empty, or zero are omitted
(`checked` only when true). `show` is a single record with no `count`.

```
id: #12
path: toolbar save
rect: 18 20 72 28
role: button
focused: true
```

| Field | Meaning |
|-------|---------|
| `id` | Identity serial (`#N`); stable while the container identity lives |
| `path` | Named-ancestor chain plus this name (empty name is `-`) |
| `rect` | `originX originY width height` (clipped, on-screen) |
| `role` | Kind, if stamped |
| `value` | Snapshot, if stamped (newlines flattened to spaces) |
| `checked` | `true` when stamped true |
| `focused` / `hovered` | This pass (`focused` is true on the leaf and every ancestor) |
| `z` | Stacking, if non-zero |
| `scroll` | `ox oy max: mx my` if this node is a scrollport |
| `caret` / `caret_h` | Only when the focused node wants keyboard this frame |
| `copy` | Clipboard text queued last frame, if this node is focused |

`focused` lists identity serials (leaf first). `hovered` lists the hover
stack. `show` / `query` records set `focused: true` when that node is on
the focus chain.

---

## Composed gestures

The kernel does not have `click`. A click is `show` (a tree read), then
three injects:

```text
show save
move <rect center>
mouse_down
mouse_up
```

`shirei/drive` sleeps `Pause` (150ms) after each inject so the
app can produce. Helpers take the UDP port as the first argument
(`Start` returns that port). `Timeout` (2s) is the UDP read deadline.

| Function | Sends |
|--------|--------|
| `Click(port, q)` | `show` / `move` / `mouse_down` / `mouse_up` (first match) |
| `ClickOne(port, q)` | wait until count is 1; `query`; click that record |
| `Count(port, q)` | `count` |
| `WaitCount(port, q, n)` | `count` every 100ms until n (3s) |
| `Hover(port, q)` | `show` / `move` |
| `Drag(port, from, to)` | hover `from`, `mouse_down`, hover `to`, `mouse_up` |
| `Type(port, q, s)` | `ClickOne(port, q)` then `text` per rune |
| `Key(port, name, mods…)` | `key_down` (`Pause`), `Tick`, `key_up` (`Pause`) |
| `KeyDown` / `KeyUp` | `key_down` / `key_up` |
| `Focused` / `Hovered` | `focused` / `hovered` |
| `TabUntil(port, q)` | wait until `q` is unique; Tab + `Frames(2)` + `focused` until the leaf is that record's id; wrap-around fails |
| `Tick` / `Frames(n)` | sleep n×~1/60s (no kernel command; no port) |

Raw lines: `Cmd(port, "key_down Tab")`.
