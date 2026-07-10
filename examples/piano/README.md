# piano

A one-row playable keyboard and a small multi-voice synth.

![piano](piano.webp)

## What it does

Play notes with the mouse or the computer keyboard (home row = white keys; the
row above = black keys, lined up like a real keyboard). Three voices via a
segmented control: plucked string (Karplus–Strong, ported from
[awtar](https://awtar.app)), flute, and plain sine. Volume slider; held keys
light up; Esc releases stuck notes.

Also usable offline without a window:

- `--play [string|flute|sine]` — short demo through the default audio device
- `--wav out.wav [voice]` — render the same demo to a file

## What it shows (shirei)

This is the reference app for **audio output** next to a GUI. UI patterns are
small but dense: hit-testing keys, floating black keys, per-frame keyboard
edges.

### Audio beside the window

```go
app.StartAudio(SampleRate, mixer.Fill)
app.SetupWindow("piano", 720, 430)
app.Run(RootView)
```

Same fill callback on macOS (AudioQueue), Linux (ALSA), and Windows (winmm).
Details: [audio-tutorial.md](../../docs/audio-tutorial.md).

See `main.go`: `main`, `synth.go` for voices/mixer.

### Mouse hold with `PressAction` / `IsActive`

A key starts a note on click and stays “held” while `IsActive()` (drag stays on
the key). Releasing the mouse ends a mouse-started note. Keyboard-held notes
are tracked separately so the two input paths do not fight.

See `gui.go`: `keyInteraction`; `main.go`: `handleKeyboard`.

### Black keys with `Float` + `InFront`

White keys flow in a row. Black keys are positioned with `Float(x, y)` and
`InFront` at computed X offsets, so they sit in the gaps without participating
in flex order.

See `gui.go`: `PianoKeyView`.

### Per-frame key edge detection

Keyboard input is not “key events attached to widgets.” Each frame compares
`InputState.DownKeys` to the previous frame’s set and calls note on/off for
edges. Modifier keys (Cmd/Ctrl) suppress piano keys so shortcuts stay free.

See `main.go`: `handleKeyboard`.

## Run it

```shell
go run .                          # inside examples/piano
go run . --png out.png
go run . --play string
go run . --wav out.wav flute
```
