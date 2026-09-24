# Custom widgets: process, paint, and a chat compose bar

This is a **follow-up** to the [layout tutorial](layout-tutorial.md). Layout
taught multi-panel structure (shell, Extrinsic, Viewport, VirtualList). This
tutorial teaches a different skill: **how Shirei expects you to customize
interactive controls**.

For application palettes and stock widget styles, see the
[appearance tutorial](appearance-tutorial.md). This tutorial builds custom
control geometry using Shirei's input-processing helpers.

Pair custom controls with the metadata and actions in the
[accessibility tutorial](accessibility-tutorial.md) so screen readers can
identify and operate them.

We build on layout **step 14**:

1. **Philosophy** — process vs presentation  
2. **A flat button** — easiest way into the model  
3. **A text field** — process plus plain text/caret draw  
4. **Compose** — put the button next to the field in one chrome box  
5. **Light and dark mode** — switch the finished chat between built-in schemes

Runnable samples (each is a full window you can open while reading):

| Step | Code | What you learn |
|------|------|----------------|
| 14 | [layout step 14](../demos/layout-shell/step14/main.go) | Starting shell (default compose) |
| 15a | [`step15a/main.go`](../demos/layout-shell/step15a/main.go) | Custom send circle, **default** text field |
| 15 | [`step15/main.go`](../demos/layout-shell/step15/main.go) | Full custom compose (pill + field + send) |
| 16 | [`step16/main.go`](../demos/layout-shell/step16/main.go) | Live light/dark switch |

```bash
cd shirei
go run ./demos/layout-shell/step14    # default field + default Send
go run ./demos/layout-shell/step15a   # custom circle, default field
go run ./demos/layout-shell/step15    # full custom compose
go run ./demos/layout-shell/step16    # live light/dark switch
```

Screenshots of the shell appear **with each section** below (and again in
the recap). The progression is easier to see in order than only at the top.

---

## Prerequisites

Finish [layout-tutorial.md](layout-tutorial.md) through **step 14**, or at
least run and skim
[`step14/main.go`](../demos/layout-shell/step14/main.go). You should know
containers, `Attrs`, rows/columns, and that compose is already a layout slot.

Optional deeper references (not required to follow along):

- Gallery demos: [custom-buttons](../demos/custom-buttons/),
  [custom-textinputs](../demos/custom-textinputs/)

---

## 1. Philosophy: process vs presentation

Default widgets (`Button`, `TextInput`, …) are **convenience packages**: they
wire up interaction **and** draw a default look. That is fine until the
default look is wrong for your app.

Shirei's custom controls combine input processing with application-owned
containers and paint:

> **You own the container.**  
> **We provide functions that process input for the current container.**  
> **You paint whatever you want from the snapshot they return.**

There is no plug-in skin object. There is no inverted “framework calls your
draw.” You build the tree; when you need “is this box hovered / clicked /
being edited?”, you call a **process** helper on that box.

```text
  ┌─────────────────────────────────────────┐
  │  your Container (you set size, pad, bg) │
  │                                         │
  │   st := Process…Events(...)             │  ← interaction (and for text, edits)
  │   // use st.Hovered, st.Clicked, …      │
  │   Label / Icon / Draw…                  │  ← presentation (yours, or plain helpers)
  └─────────────────────────────────────────┘
```

### Why this shape?

- **State depends on the box.** Hover, press, and focus are about *this*
  container’s id and geometry. Handing state into a nested “view callback”
  the shell owns is circular — the shell would need the view to exist before
  it could compute the state the view needs.
- **Data-centric.** Process returns a plain snapshot (`Hovered`, `Clicked`,
  caret position, …). You branch and paint; you do not implement an interface.
- **Defaults are thin.** `Button` is process + a default face. `TextInput` is
  process + plain text/caret draw + a little default chrome. You can drop to the
  same building blocks anytime.

### The one rule that bites

`ModAttrs` must run **before** any child is added (labels, icons, draw
helpers). Process helpers are written so they **do not create children**, so
this stays legal:

```go
st := ProcessButtonEvents(false)
if st.Hovered {
    ModAttrs(Background(...)) // OK — no children yet
}
Icon(...) // children only after attrs are settled
```

---

## 2. Warm-up: a flat custom button

Buttons are the easiest place to learn the model. Interaction is pointer
press/release plus keyboard (the box is Focusable; Space/Enter are Active
while held and Clicked on release). You draw everything yourself.

### What the library provides

```go
st := ProcessButtonEvents(disabled bool) ButtonState
// st.Hovered, st.Active, st.Clicked, st.Disabled, st.HasFocus, st.FocusVisible, st.Local
```

That is the whole interaction contract for a clickable box. Default
`Button` / `CtrlButton` call the same idea under the hood and then paint an
accent face. You can skip the default face entirely.

### A circular “send” control

In the chat shell we will want a circle with an arrow, not a labeled
rectangle. That is still just process + paint:

```go
func sendCircle(disabled bool) bool {
	const size float32 = 36
	var clicked bool
	Container(Attrs(FixSize(size, size), Corners(size/2), BorderWidth(2), Center), func() {
		st := ProcessButtonEvents(disabled)
		NextAccessRole("button")
		NextAccessLabel("Send message")
		NextAccessDisabled(disabled)
		AssignAccess()
		clicked = st.Clicked

		scheme := CurrentColorScheme
		style := scheme.Buttons.Primary
		paint := style.Normal
		switch {
		case st.Disabled:
			paint = style.Disabled
		case st.Active:
			paint = style.Pressed
		case st.Hovered:
			paint = style.Hovered
		}
		ModAttrs(BackgroundVec(paint.Background), BorderColorVec(paint.Border))
		if st.FocusVisible && !disabled {
			ModAttrs(BorderColorVec(scheme.FocusRing))
		}
		Icon(TypArrowUp, FontSize(18), TextColorVec(paint.Text))
	})
	return clicked
}
```

**What to notice:**

- The **circle is your container** — size, corners, fill are presentation.
- **Process** answers pointer and keyboard interaction for *this* box
  (`Clicked` covers press-release, Space, and Enter; `FocusVisible` controls
  the ordinary focus outline, while `HasFocus` controls keyboard behavior).
- Read button-state colors from `CurrentColorScheme.Buttons.Primary` during
  each build. The same geometry works in either mode.
- Give the icon-only button a role, label, and disabled state for screen readers.
  `ProcessButtonEvents` also handles the accessibility press action.
- Disable by passing `true` when there is nothing to send; process will not
  report a click.

Optional gallery of other faces (Material flat, XP Luna, Win98 bevel):
[`demos/custom-buttons/`](../demos/custom-buttons/).

### On the chat shell (still a default field)

Drop that circle next to the default `TextInputExt` from layout step 14 — you
do **not** need a custom field yet. Intermediate sample:

[`demos/layout-shell/step15a/main.go`](../demos/layout-shell/step15a/main.go)

![Step 15a — custom send, default field](layout-tutorial/images/step15a.png)

Compare to default send on the same shell:

![Step 14 — default field and Send button](layout-tutorial/images/step14.png)

**What to notice:** Only the control you care about changed. The list, rails,
and text field are still default. That is the process/paint model working at
call-site scale.

---

## 3. Text fields: process *and* plain paint

Text is harder than buttons: keys, selection, scroll, IME, caret blink. We
still separate **process** from **chrome**, but we also ship **plain paint**
helpers so you do not reimplement a text editor.

### Two layers

| Function | Responsibility |
|----------|----------------|
| `ProcessTextInput(buf, cfg)` | Focus, hooks, edit model, clipboard, IME rules. **No children.** Returns a snapshot. |
| `DrawTextInputPlain(st, cfg)` | Scrollable text, selection, composition marks, blinking caret. |

You own the **field box** (border, background, padding of the control as a
product). We own **editing** and a **default way to draw the text and caret**
— but you choose **where** to call the draw helpers (inside your box, after
your chrome attrs).

```go
Container(Attrs(Focusable, Clip, PadVec(cfg.Padding), /* your chrome */), func() {
    st := ProcessTextInput(buf, cfg)
    if st.HasFocus {
        ModAttrs(/* focus chrome — still before children */)
    }
    DrawTextInputPlain(st, cfg) // text + cursor, wherever you placed this call
})
```

### Why paint helpers for text but not for buttons?

A button face is a few rectangles and a label — easy to reinvent. A correct
field is not. Requiring every app to redraw caret affinity, composition
underlines, and scroll-to-caret would fight the “customize chrome” goal. So:

- **Buttons:** process only; presentation is entirely yours.  
- **Text:** process + optional plain draw; presentation of the **chrome** is
  yours; presentation of **glyphs and caret** can be the library’s.

You can still style text/caret colors via `TextInputConfig` when needed. You
are free to call `DrawTextInputContent` / `DrawTextInputCaret` separately if
you want them in different places (advanced).

### Contract worth remembering

- Call `ProcessTextInput` **inside** the focusable field container.  
- That container’s **padding** is the text geometry padding (caret and
  hit-testing).  
- Process creates **no children** (so focus `ModAttrs` stays legal).  

Gallery of field skins only: [`demos/custom-textinputs/`](../demos/custom-textinputs/).

You will see a borderless field **in context** in the next section (inside
the compose pill). Until then, the gallery demos are the best place to try
field chrome alone without the whole chat shell.

---

## 4. Put them together: the compose bar

Layout step 14 ends with a practical but plain strip:

```go
Container(Attrs(Expand, Pad(10), Gap(8), Row, CrossMid, UseSurface(SurfacePanel)), func() {
    a := DefaultTextInputAttrs()
    a.NoAutoFocus = true
    TextInputExt(&draft, a)
    if Button(NoIcon, "Send") && draft != "" {
        draft = ""
    }
})
```

A default field next to a default button works. Modern chat-style UIs usually
want **one chrome surface** that houses both: generous outer padding, a
rounded “pill,” a quiet multi-line field, and a compact send control.

```text
[  pad  ]
[  rounded pill:  [ multi-line field .............. ]  (↑)  ]
[  pad  ]
```

That is just **§2 + §3 in one row** — not a new API.

### Step A — Helper on the shell

Keep the layout shell from step 14; only replace the compose strip:

```diff
-				Container(Attrs(Expand, Pad(10), Gap(8), Row, CrossMid, UseSurface(SurfacePanel)), func() {
-					TextInputExt(&draft, ...)
-					if Button(NoIcon, "Send") && draft != "" { draft = "" }
-				})
+				chatCompose(&draft, &messages)
```

### Step B — Outer pad + pill (chrome only)

```go
func chatCompose(draft *string, messages *[]msg) {
    Container(Attrs(Expand, Pad(12), UseSurface(SurfaceCanvas)), func() {
        Container(Attrs(Expand, Row, CrossMid, Gap(8),
            Pad2(6, 8), Corners(12),
            UseSurface(SurfacePanel), BorderWidth(1),
        ), func() {
            // field (step C) + send circle (step D)
        })
    })
}
```

The **pill** is product chrome. The field inside will stay visually quiet.

### Step C — Field inside the pill

```go
scheme := CurrentColorScheme
cfg := TextInputConfigWithStyle(TextInputConfig{
    FontSize: DefaultTextSize,
    Padding: N4(10),
    Wrap: true, MaxLines: 0, Rows: 2,
    NoAutoFocus: true,
}, scheme.TextInput)
boxH := float32(cfg.Rows)*cfg.FontSize + PadSize(cfg.Padding)[1]

Container(Attrs(
    Focusable, Clip, Grow(1), PadVec(cfg.Padding),
    MinSize(80, boxH), MaxSizeVec(Vec2{0, boxH}),
    Corners(6), BorderWidth(1),
), func() {
    st := ProcessTextInput(draft, cfg)
    NextAccessRole("text")
    NextAccessLabel("Message")
    NextAccessEditable(true, true)
    NextAccessValue(*draft)
    AssignAccess()
    if st.HasFocus {
        ModAttrs(BorderColorVec(scheme.FocusRing))
    }
    DrawTextInputPlain(st, cfg)
})
```

**What to notice:** Default `TextInputExt` draws its own field surface and
border. Here the pill already frames the control, so the field is
transparent with a focus outline. `TextInputConfigWithStyle` supplies text,
caret, selection, and placeholder colors from the active scheme; rebuild
this config each frame so it follows mode changes.

The metadata names the field and exposes its value and editable state.
Native selection and text-editing accessibility APIs have additional
requirements; see the [accessibility tutorial](accessibility-tutorial.md).

### Step D — Send circle in the same pill

Reuse the §2 idea next to the field (`Grow(1)` on the field leaves a fixed
circle on the right):

```go
if sendCircle(*draft == "") {
    text := *draft
    *draft = ""
    *messages = append(*messages, msg{
        id: len(*messages) + 1, author: "you",
        body: text, time: time.Now().Format("15:04"),
    })
    RequestNextFrame()
}
```

Messages already use `VirtualListView` with stable ids from layout step 14 —
append is enough; no layout rewrite.

### Full source

[`demos/layout-shell/step15/main.go`](../demos/layout-shell/step15/main.go)

```bash
cd shirei
go run ./demos/layout-shell/step15
```

![Step 15 — full custom compose](layout-tutorial/images/step15.png)

**What to notice:** One pill holds both process helpers. The shell above the
strip uses the same surfaces as step 14. The field, focus outline, and send
button all resolve their colors from the active scheme.

---

## 5. Switch between light and dark mode

Step 16 adds a mode checkbox to the finished shell. Its custom controls use
scheme colors just like the stock widgets, so the same drawing code serves
both modes.

**Full source:** [`step16/main.go`](../demos/layout-shell/step16/main.go)

```bash
cd shirei
go run ./demos/layout-shell/step16               # start in dark mode
go run ./demos/layout-shell/step16 --dark=false  # start in light mode
```

![Step 16 — dark mode with a live switch](layout-tutorial/images/step16.png)

### Select the scheme at the start of the frame

Keep the user's choice in application state:

```go
var darkMode = true

func frame() {
    SetDarkMode(darkMode)
    scheme := CurrentColorScheme
    ModAttrs(UseSurface(SurfaceCanvas))
    // Build the shell with this scheme.
}
```

The sample also binds `darkMode` to its `--dark` flag. `SetDarkMode` selects
the preferred light or dark scheme and requests a redraw when it changes.
Call it before building any widgets so each frame uses one scheme throughout.

### Put the choice in the top bar

```go
Container(Attrs(Row, Expand, FixHeight(48), UseSurface(SurfacePanel),
    Pad2(0, 14), Gap(12), CrossMid), func() {
    Label("Layout shell", FontSize(15), FontWeight(WeightSemibold))
    Filler(1)
    NextAccessName("dark_mode")
    CheckBox(&darkMode, "Dark mode")
})
```

The checkbox updates `darkMode`; the next frame applies that choice. Draft
text, messages, and list identities stay in the same application state.
There is no separate dark version of `chatCompose` or `sendCircle`.

### Let the scheme provide the paint

| Part of the shell | Color source |
|-------------------|--------------|
| Main area and compose outer pad | `UseSurface(SurfaceCanvas)` |
| Sidebars, top bar, compose pill | `UseSurface(SurfacePanel)` |
| Ordinary labels | Inherited surface text color |
| Custom send button states | `CurrentColorScheme.Buttons.Primary` |
| Custom field text, caret, selection | `TextInputConfigWithStyle(cfg, CurrentColorScheme.TextInput)` |
| Focus outline | `CurrentColorScheme.FocusRing` |
| Default scrollbars | `CurrentColorScheme.ScrollBar` |

`VirtualListView` and `ScrollBars()` already draw their default scrollbars
with the active scheme. Switching modes needs no custom scrollbar callback.

Resolve surfaces and custom-widget colors while building each frame.
Caching a resolved style at startup prevents it from following later mode
changes. See the [appearance tutorial](appearance-tutorial.md) for custom
palettes, widget style overrides, and following the operating system's mode.

---

## Recap

| Stage | You learn |
|-------|-----------|
| Philosophy | You own containers; process returns data; you present |
| Flat button | `ProcessButtonEvents` + your paint |
| Text field | `ProcessTextInput` + `DrawTextInputPlain` (chrome yours; glyphs/caret optional helpers) |
| Compose | Same two pieces in one chrome box on the chat shell |
| Light/dark mode | Select a scheme; resolve custom-control paint each frame |

| Default convenience | Building blocks |
|-------------------|-----------------|
| `Button` | `ProcessButtonEvents` + paint |
| `TextInputExt` | field container + `ProcessTextInput` + `DrawTextInputPlain` (+ optional chrome) |
| `ScrollBars()` (modern overlay) | `ScrollBarExt` + optional `SetDefaultScrollBar` |

Layout (Extrinsic / Viewport / VirtualList) determines structure and sizing.
The active color scheme supplies paint for the shell and its controls.

### Final results (same images as above)

Light mode, full custom compose:

![Step 15](layout-tutorial/images/step15.png)

Dark mode, the same custom controls:

![Step 16](layout-tutorial/images/step16.png)

```bash
cd shirei
go run ./demos/layout-shell/step15
go run ./demos/layout-shell/step16
```

---

## Common mistakes

- `ModAttrs` **after** `Icon` / `DrawTextInputPlain` (panic).  
- Default field chrome **and** pill chrome (double borders).  
- Calling `Button(NoIcon, "Send")` and considering the control “custom.”  
- Changing Extrinsic/Viewport while redesigning compose — usually unnecessary.  
- Calling `ProcessTextInput` outside the focusable field container (hooks and
  focus attach to the wrong node).  
- Expecting `CrossMid` alone to vertically center a header label in a column.  
- Reimplementing thumb drag — use `ScrollBarExt` / the package default instead.  
- Caching resolved scheme colors or text-input configs at startup.
- Selecting the mode after building part of the UI.
- Changing a selected background without its matching text color.

---

## Related

| | |
|--|--|
| Palettes and system appearance | [appearance-tutorial.md](appearance-tutorial.md) |
| Layout shell (01–14) | [layout-tutorial.md](layout-tutorial.md) |
| Custom send only (step 15a) | [`demos/layout-shell/step15a/`](../demos/layout-shell/step15a/) |
| Compose sample (step 15) | [`demos/layout-shell/step15/`](../demos/layout-shell/step15/) |
| Live mode switch (step 16) | [`demos/layout-shell/step16/`](../demos/layout-shell/step16/) |
| Scrollbar skins gallery | [`demos/custom-scrollbars/`](../demos/custom-scrollbars/) |
| Button skins gallery | [`demos/custom-buttons/`](../demos/custom-buttons/) |
| Field skins gallery | [`demos/custom-textinputs/`](../demos/custom-textinputs/) |
