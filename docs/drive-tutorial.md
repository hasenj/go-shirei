# Access attributes and drive tests

This tutorial is about testing a Shirei application the way a person
uses it: look at the window, find a button, click it, and check that
the screen changed.

Two pieces have to work together. First, the app marks certain
on-screen controls with stable names so a test can find them. Second,
a Go test starts the real app, talks to that running window, and
clicks those names.

The rest of this file teaches both. The main GUI tutorial is
[tutorial.md](tutorial.md). The raw list of network commands is
[drive.md](drive.md).

Once the ideas below make sense, these tests in the repo are worth
reading:

- `demos/theme/main_test.go` — Tab, Space, and a dialog
- `examples/process_monitor/drive_test.go` — find, select, pin, and kill a process
- `examples/git_history/drive_close_test.go` — passing extra arguments to the app

---

# Part I — Naming the UI

## 1. Why a unit test is not enough

Most Go tests call a function with some input and check the return
value. That is a good way to test a parser, a sort, or any piece of
logic that does not care about a window.

A graphical application is different. The user does not call your
functions. They look at a window, move the mouse, click a button, type
in a field, and watch what happens. The quality of the app is whether
that whole loop works: the right control is on screen, the click
reaches it, and the screen updates the way a person would expect.

A unit test never opens that window. It never moves the mouse. It can
prove that flipping a "pinned" flag in memory works, and still miss
that the Pin button is off-screen, hidden behind another panel, or
never drawn at all. It can prove that a filter function returns two
rows, and still miss that typing in the search box does not update the
table the user is looking at.

So you need a kind of test that treats the running program as a user
would. The test starts the app, finds a control in the live window,
acts on it (click, type, press Tab), and then reads the screen to see
whether the app did the job.

That is what **drive tests** are. The rest of Part I is how the app
makes its controls findable. Part II is how a Go test talks to the
window.

## 2. Identify a control, then read its state

Suppose you want a test that says: click Save, then check that the
window shows Saved.

To click Save, the test has to know *which* box on screen is the Save
button. To check the result, it has to find whatever shows "Saved" and
read it. In other words: **identify** the controls that matter, then
**query their state** (is it on screen? is the checkbox on? what text
is in the field?).

Two tempting shortcuts fail.

The wording on the button is a poor ID. Tomorrow you might change
"Save" to "Save draft", or ship a German build that says "Speichern".
The test should not break when the label changes.

Pixel coordinates are a poor ID too. "Click at (18, 20)" breaks the
moment you add a toolbar, change the font size, or resize the window.
A person does not click coordinates. They click the button.

What you need is a **name you choose**, attached to that control, that
stays the same even when the label and the layout change. The test
looks up that name. It can also ask, right now: is that named control
on screen? Is it checked? What value does it hold?

Each time the screen updates, your view function runs and describes
what should appear. Calling `Button` draws a button for that update
and tells you whether it was clicked. It does not leave behind an
object the test can keep in a variable and call methods on. So the
name is not a pointer to a widget. It is a handle you put on the
button when you draw it, which a test can look up afterwards.

A name by itself is not enough, though. The next section is the other
half of that problem: what happens when two controls want the same
name.

## 3. Names can repeat; the screen is a hierarchy

A name is a short word you pick: `save`, `pin`, `filter`. It is not
the text the user sees. It is the ID a test uses to find that box.

The hard part is that a real window often has more than one thing that
deserves the same word. A toolbar at the top can have a Save button.
An inline edit panel can have another Save button. Both are "the save
button" in their own neighborhood. If every name on the whole screen
had to be unique, you would invent `topbar_save` and `edit_panel_save`
and keep renaming them as the layout grows. That is painful, and it
throws away the fact that the screen is already grouped into areas.

So names are allowed to repeat. The two buttons can both be named
`save`. What keeps them apart is **where they sit**. You also name the
regions around them — the top bar, the edit panel — and a test asks
for a `save` *inside* a region.

Think of a folder path on disk. Two files can both be called
`readme.txt` as long as they live in different folders. A query is
that kind of path: the region first, then the control, separated by
spaces.

```text
query topbar save         the Save button on the top bar
query edit_panel save     the Save button in the open edit panel
query save                both of them
```

If you have named the screen areas, `topbar save` finds one button and
`edit_panel save` finds the other, even though both controls are
named `save`. You only make a name unique when you actually need a
shortcut that ignores location (`ClickOne("save")` requires exactly
one match).

A single name is one word. Do not put spaces inside it — a space means
"this region, then that child." The hierarchy is several names in
order, not one long unique string.

The next sections are how you stamp a name onto a box, and what else
you can attach (what kind of control it is, a value, whether it is
checked). How those named boxes form a tree, and the exact query
language, come a little later.

## 4. Stamp a name on the box you care about

The handle is a small set of **access attributes** on a container
(a box in the layout). The one you almost always set is the name.

`NextAccessName("save")` does not attach the name yet. It sets the name
aside. `AssignAccess()` copies whatever is set aside onto the
**current** container, then clears it.

Default widgets already call `AssignAccess`. For a button, you only set
the name, immediately before the widget:

```go
NextAccessName("save")
if Button(NoIcon, "Save") {
    saved = true
}
```

The name is a **query handle** for tests and tools. It is not the
visible label: `"save"` stays `"save"` even if the button reads "Save"
or "Speichern". It is also not container **identity** — that is
`Container` / `ContainerWithKey`, which is how hover and scroll survive
the next frame. Access names are optional; skip them and the UI still
works, tests just cannot look the box up.

Names need not be unique, not even among siblings. Twenty process rows
can all be named `proc`. Do not put spaces in a name — looking one up
later splits on spaces. A name must not start with `#` (`#12` is an
identity serial).

Why two steps, instead of `Container` assigning by itself? Layout is
full of wrapper boxes you never want to look up: padding rows, gaps,
alignment. Automatic assign would fill the tree with noise. The widget
— or your own chrome — chooses which box is the named one.

If you call `NextAccessName` and nothing calls `AssignAccess` before
the frame ends, Shirei prints a warning to stderr and drops the leftover.

## 5. A Label is not a control — wrap it

`Label` draws text. It does not assign. A status line that a test needs
to find has to live in a container *you* assign:

```go
if saved {
    NextAccessName("saved")
    Container(Attrs(), func() {
        AssignAccess()
        Label("Saved")
    })
}
```

`AssignAccess()` here stamps `"saved"` onto that wrapper. The label
inside is just drawing.

That is the whole pattern for anything that is not a default widget:
open the box, assign, put the contents inside.

## 6. Role, value, and checked

Name is enough to click a button. Three more fields describe what the
control *is* this frame. They write into the same "set aside" slot that
`NextAccessName` uses. `AssignAccess` copies all of them at once.

| Call | Field | Meaning |
|------|--------|---------|
| `NextAccessName("save")` | `Name` | Query name. Lowercase. No spaces. Not the visible label, not identity. |
| `NextAccessRole("button")` | `Role` | Kind: `button`, `text`, `checkbox`, `radio`, … |
| `NextAccessValue("42")` | `Value` | Snapshot this frame (field text, PID, …). |
| `NextAccessChecked(on)` | `Checked` | Snapshot this frame (toggle, radio, pin). |

If you write the same field twice before assign, the last write wins.

You rarely set role, value, or checked yourself on a default widget —
the widget already does. `CheckBox` stamps `checked` from the bool.
`TextInput` stamps `value` from the buffer and `role: text`. You still
set the name:

```go
NextAccessName("filter")
TextInput(&filter)

NextAccessName("show_ts")
CheckBox(&showTS, "Timestamps")
```

A button does **not** grow a `checked` field just because it toggles
something in your app. If a test needs to read "is this row pinned?",
you stamp it before `Button` assigns:

```go
NextAccessName("btn_pin")
NextAccessChecked(proc.pinned)
if Button(NoIcon, pinLabel) {
    proc.pinned = !proc.pinned
}
```

The pending `checked` is still there when `Button` assigns, so the
record carries both `role: button` and the pin state.

## 7. Default widgets already assign

These call `AssignAccess` and set `Role` (and `Value` / `Checked` where
it applies):

`Button`, `CtrlButton`, `CheckBox`, `OptionButton`, `ToggleSwitch`,
`TextInput`, `Slider`, `ProgressBar`, `MenuButton` / `MenuItem`,
`SegmentedControl` / `SegmentedCell`, `FileSelector`, `DirectoryBrowse`,
and `Toast`.

You still set the **name** on the line above.

A table **header** is named from `TableColumn.AccessName` (not the
visible `Label`). Empty `AccessName` means the header is not in the
access tree. Use a `header-` prefix (`header-pid`, `header-cpu`). Body
rows and cells do not get a name for free. Stamp a row in `OnRow`
(`proc` plus the PID in `value`). Stamp a cell inside `TableColumn.Cell`.
Put the row key in the cell name when tests need `ClickOne` on one cell
(`p4372-pid`, `p4372-cpu`, …) so it does not collide with the header or
the other rows.

For a custom control, call `ProcessButtonEvents` (or the matching
process) on the container, then `NextAccessRole` + `AssignAccess` on
that same container — the same shape the default widgets use.

## 8. The named boxes become a tree

After layout, Shirei walks every container and keeps only the ones that
assigned. That list, with parent links, is the **access tree** for this
frame.

Unnamed wrappers are skipped. The parent of a named leaf is the nearest
**assigned** ancestor, not every `Container` in between.

Picture a toolbar inside a padding column:

```text
Container          ← unnamed, skipped
  Container        ← name: toolbar   (you assigned)
    Button         ← name: save      (Button assigned)
    Button         ← name: open
  Container        ← unnamed, skipped
    Container      ← name: saved     (you assigned; Label inside)
```

The access tree is just the named boxes:

```text
toolbar
  save
  open
saved
```

`save` and `open` sit under `toolbar`. `saved` is a root of its own,
because nothing named sits above it.

If a list is virtual, only the rows that were actually built this frame
appear — the ones that intersect the viewport.

## 9. Finding a node: path and query

Each node has a **path**: its named ancestors, then its own name,
space-separated, starting at the root. In the tree above, the Save
button is `path: toolbar save`. The status line is just `path: saved`.
An assigned node with an empty name shows as `-` in the path.

A **query** is the same kind of string. The last token is the leaf you
want. Earlier tokens are ancestors they must have — not necessarily
the immediate parent.

```text
query save                 every node named save
query toolbar save         a save that has a toolbar ancestor
query detail_pid btn_pin   a pin button under a detail_pid ancestor
```

`query save` matches the button in the diagram. `query toolbar save`
matches it too, because `toolbar` is above it. A second Save button
elsewhere on screen would show up in the first query and not the
second.

`count` is the same lookup but replies with the integer only — whether
the control is on screen, not its rect.

`show` is the same lookup but returns only the first match.

`#N` is an identity serial, not a name. `show #12` / `query #12` /
`count #12` hit that one container or none.

Focus and hover are their own commands, not `show` arguments. They
reply with identity serials, leaf first (then ancestors). Empty if
nothing is focused or hovered.

```text
focused     #78 #3 #1
hovered     #12 #3 #1
```

`show` / `query` records set `focused: true` when that node is on the
focus chain (the leaf or an ancestor). Same for `hovered`.

Several nodes may share a name; uniqueness is not required, even under
the same parent. Twenty process rows can all be named `proc`;
`query proc` returns every one of them that is in the tree. Tell them
apart with `value` — a PID, a path — rather than inventing a distinct
name per row, unless you specifically want `Click("proc_1234")`.

## 10. Reading one record

A `show` or `query` hit looks like this:

```
id: #12
path: save
rect: 18 20 72 28
role: button
focused: true
```

| Field | Meaning |
|-------|---------|
| `id` | Identity serial (`#N`) |
| `path` | Named ancestor chain plus this name |
| `rect` | On-screen box this frame (`Origin`, `Size`), already clipped |
| `role` | Kind, if stamped |
| `value` | Snapshot, if stamped |
| `checked` | Present only when true |
| `focused` / `hovered` | True on the leaf and every ancestor on the chain |
| `z` | Stacking, if non-zero |
| `scroll` | Scrollport offset and max, if this node scrolls |

You do not stamp `focused` or `hovered`. Shirei fills those from the
same stacks the UI already uses.

Caret position (`caret` / `caret_h`) is printed only for a focused
text field. A focused button has no caret.

---

# Part II — Driving a window

## 11. The test is a second process

The access tree lives inside the GUI process, for that frame. A test
that wants to act like a user needs the *window*: mapping, focus, keys,
the OS event loop.

`shirei/drive` is a small client for that. The test launches the app as
its own process. The two talk over loopback UDP: the test sends pointer
and key input, then asks for the access tree.

```text
  go test                          the app
  ─────────                        ─────────
  drive.Start(t, ".")   SHIREI_DRIVE_PORT=N -->  AcceptInputCommands(N)
  drive.Click(port, "save")        UDP  show / move / down / up
  drive.Query(port, "saved")       UDP  query saved  <-- access tree
  t.Cleanup                        UDP  quit
```

You do not write those UDP lines yourself. `Click`, `Query`, `Key`,
`Type` are package functions; the first argument is the port `Start`
returned. Two tests can drive two programs at once by keeping both
ports.

`Start` builds the **main package** (the app, not the test binary),
launches it with `SHIREI_DRIVE_PORT` set, waits until a `ping` comes back, waits
500ms for the window, and registers cleanup that sends `quit`. Extra
arguments after the package path are forwarded to the app:

```go
port := drive.Start(t, ".", "--session", path)
```

Put the test in the same directory as `main.go`, with `package main`.

## 12. The app listens on a port

The running app has to open a listener on that port. `app.SetupDrive`
reads `SHIREI_DRIVE_PORT` (set by `Start`, or by a human:
`SHIREI_DRIVE_PORT=4372 go run .`) and calls `AcceptInputCommands`.
No port is a no-op.

```go
app.SetupWindow("Counter", 400, 240)
app.SetupDrive()
app.Run(RootView)
```

`SetupQuiet` maps the window without taking keyboard focus, so `go test`
does not steal the editor. To watch the window while the test runs:

```bash
SHIREI_DRIVE_FOREGROUND=1 SHIREI_CMD_LOG=1 go test -count=1 -v -run TestDriveClickSave .
```

That env is inherited by the child; the app skips `SetupQuiet`.

## 13. A first test

Same directory as `main.go`. The view names `save` and `saved` as in
§4–5.

```go
package main

import (
	"testing"

	"go.hasen.dev/shirei/drive"
)

func TestDriveClickSave(t *testing.T) {
	port := drive.Start(t, ".")

	if _, err := drive.Click(port, "save"); err != nil {
		t.Fatal(err)
	}
	if err := drive.WaitCount(port, "saved", 1); err != nil {
		t.Fatal(err)
	}
}
```

Two beats:

1. **`Start`** — the process is up, `ping` answers, then 500ms for the
   window.
2. **`Click` then `WaitCount`** — look up `save`, click it, then poll
   `count saved` every 100ms until it is 1 (up to 3s).

The next section is the rule for step 2.

## 14. Waiting for the tree

After a click or key, poll `count` every 100ms until it matches, up to
3s (`WaitCount`). Do not query every frame.

`ClickOne` does the same wait for a unique control before it clicks.
A detail panel after a row click, a modal after Space, a tab close:
same interval.

A stall past 3s fails the test.

`Tick` / `Frames` sleep ~1/60s per frame; they do not send a command. A frame that
saw input already requests one follow-up produce. Held keys stay in
`DownKeys` until `key_up`. `Key(port, "space")` is a tap: `key_down`
(`Pause`), `Tick`, `key_up` (`Pause`).

## 15. The rest of the driver

The UDP lines themselves are listed in [drive.md](drive.md). The
functions below are what `shirei/drive` sends. All of them take `port`
first except `Tick` / `Frames`.

| Function | Sends | Effect |
|--------|--------|--------|
| `Ping(port)` | `ping` | `pong`; `Start` uses this |
| `Click(port, q)` | `show` / `move` / `mouse_down` / `mouse_up` | first match |
| `ClickOne(port, q)` | wait until count is 1, then `query` and click | unique control |
| `Type(port, q, s)` | `ClickOne` then `text` per rune | into a text field |
| `Key(port, name, mods…)` | `key_down` (`Pause`), `Tick`, `key_up` (`Pause`) | a tap |
| `TabUntil(port, q)` | wait until `q` is unique; Tab + `Frames(2)` + `focused` until the leaf is that record's id | wrap-around fails |
| `Focused` / `Hovered` | `focused` / `hovered` | id lists, leaf first |
| `Tick` / `Frames(n)` | sleep n×~1/60s | no kernel command |
| `Count(port, q)` | `count q` | number of matches |
| `WaitCount(port, q, n)` | `count q` every 100ms | until n, up to 3s |
| `Query(port, q)` | `query q` | `QueryResult{Count, Nodes}`; Count is every match |
| `Show(port, q)` | `show q` | first match (name path or `#N`) |
| `Quit(port)` | `quit` | `Start` cleanup already calls this |
| `Windows(port)` | `windows` | `WindowList{Count, Windows}`; main first |
| `SelectWindow(port, id)` | `window id` | later commands target that window |
| `CurrentWindow(port)` | `window` | selected handle (`main` by default) |
| `WaitWindow(port, match)` | `windows` every 100ms | until a window matches, up to 3s |

`go test -v` with `SHIREI_CMD_LOG=1` prints each UDP line with a
timestamp (`>` command, `<` reply), except `ping`. Empty inject replies
are omitted. Tester uses `SHIREI_DRIVE_TRACE` instead and does not set
this.

## 16. Waiting on the OS

Two empty frames do not cover "the operating system listed the
processes" or "the child we spawned shows up in the table." Those are
facts the UI cannot know until the app reads them.

`WaitCount` is for the access tree (100ms). OS facts wait on the
collector, not on widget lag: first sample (`waitSample`, 100ms), a
spawned PID (`waitProc`, 1s then 250ms), `role: exited` after the next
sample — not after the kill click.

Name that helper after the OS fact (`waitSample`, `waitProc`), not after
the widget you happen to query while you wait.

`Start` already waits 500ms after ping for the window.

## 17. Where the click lands

`Click(port, q)` uses the **center** of the named rect. That is wrong when
the rect is a full-width table row: the center sits in a middle column,
and the first body row sits against the header, so the click can hit a
sortable header instead of the row.

Name a small child (the PID cell) and `ClickOne` that. process_monitor
stamps `p4372-pid` on the cell so `ClickOne("p4372-pid")` is the row
select without hitting `header-pid`.

Several widgets can share a name. `Query` returns them in tree order.
Use `value` to pick one, then click that node's rect — look it up
**immediately** before the click if the table can reorder (CPU sort).

## 18. Running the tests

From the app directory:

```bash
SHIREI_CMD_LOG=1 go test -count=1 -v -run TestDriveClickSave .
```

`-count=1` disables Go's successful-test cache. Drive tests launch a
GUI; a cached `ok` does not open a window. Same-package tests still
**compile** the app, so editing `main.go` busts the cache on the next
run. Re-running an unchanged test does not — that is why the flag is
on the command line.

`SHIREI_CMD_LOG=1` plus `-v` prints the UDP trace on stderr.

`SHIREI_DRIVE_FOREGROUND=1` brings the window forward.

`Timeout` (UDP read deadline) is 2s; `Pause` after each pointer/key
inject is 150ms. Both are package constants.

`screenshot /tmp/foo.png` (or `drive.Screenshot(port, path)`) writes the
last completed frame as a PNG for a visual check. Use an absolute path:
the file is created by the app process. It is not a pixel-golden
assertion — `Snapshot` / `RenderToPNG` cover that.

`drive.Shot(port, "after pin")` is the beat for `shirei_tester`: a no-op
unless `SHIREI_DRIVE_TRACE` is a directory (tester sets that). Then it
writes a numbered PNG and a JSONL line. `drive.Comment("Open the filter text field")`
is a heading in that log, also a no-op without the env. Every UDP `Cmd`
except `ping` is logged too. Tester shows comments in bold, then the
commands, then the shots.

---

## 19. Auxiliary windows

Auxiliary windows are separate UIs. UI-scoped commands target the main window
until the listener selects another one:

```go
popup, err := drive.WaitWindow(port, func(w drive.Window) bool {
    return w.Kind == "popup"
})
if err != nil {
    t.Fatal(err)
}
if err := drive.SelectWindow(port, popup.ID); err != nil {
    t.Fatal(err)
}
// Click, Query, Show, Screenshot, … now read and inject the popup.
```

`drive.Windows(port)` lists each window's handle (`id`), `kind`, `title`,
content `size`, screen `pos` (omitted where the platform has no global
position), and whether it is the listener's `target`. An app names a window it
opens with `PopupWindowOptions.Handle`; unnamed windows get `popup-1`,
`popup-2`, … in creation order. `CurrentWindow` reports the current selection.
Selecting a handle that does not exist fails; a selected window that closes
later reverts the listener to `main`.

Coordinates and rects are the selected window's content space, and
`WaitCount` / `Show` read its last completed frame — wait for a popup's tree
the same way as the main window's.

## Putting it together

1. Stamp access: `NextAccessName` on every control the test will
   `Click` / `Query`; `AssignAccess` if the widget does not.
2. `app.SetupDrive()` after `SetupWindow` (listens on `SHIREI_DRIVE_PORT`,
   `SetupQuiet` unless `SHIREI_DRIVE_FOREGROUND`).
3. `drive.Start(t, ".")` from a `package main` test in that directory.
4. After a local click: `WaitCount` (100ms). Fail if it does not match
   within 3s.
5. Wait on the collector for OS-backed facts (`Collect`, spawned PIDs),
   not on widget lag.
6. `go test -count=1` when you want the window driven for real.
