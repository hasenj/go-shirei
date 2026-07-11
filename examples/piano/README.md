# piano

A one-row playable keyboard and a small multi-voice synth.

![piano](piano.webp)

## Playable keyboard and multi-voice synth

Play notes with the mouse or the computer keyboard (home row = white keys; the
row above = black keys, lined up like a real keyboard). Three voices via a
segmented control: plucked string (Karplus–Strong), flute, and plain sine.
Volume slider; held keys light up; Esc releases stuck notes.

Also usable offline without a window:

- `--play [string|flute|sine]` — short demo through the default audio device
- `--wav out.wav [voice]` — render the same demo to a file

## Audio beside the window

```go
app.StartAudio(SampleRate, mixer.Fill)
app.SetupWindow("piano", 720, 430)
app.Run(RootView)
```

Same fill callback on macOS (AudioQueue), Linux (ALSA), and Windows (winmm).
Voices and the mixer live in `synth.go`. Details:
[audio-tutorial.md](../../docs/audio-tutorial.md).

## Black keys with `Float` + `InFront`

White keys participate in a normal row. Black keys do not: they get an absolute
X from the white-key slot and sit on top with `InFront`.

```go
// gui.go — PianoKeyView (simplified)
attrs := Attrs(FixSize(whiteW, whiteH), /* white styling */)
if k.IsBlack {
    x := framePad + f32(k.Slot+1)*(whiteW+keyGap) - keyGap/2 - blackW/2
    attrs = Attrs(Float(x, framePad), InFront, FixSize(blackW, blackH), /* black styling */)
}
ContainerWithKey(k, attrs, func() {
    pressed := keyInteraction(k)
    // label, keycap chip, …
})
```

## Mouse hold vs keyboard edges

Mouse: click starts a note; `PressAction` + `IsActive` keeps it held while the
pointer stays on the key; release ends a mouse-started note.

```go
// gui.go — keyInteraction
if IsClicked() {
    noteOn(k, true)
}
PressAction()
holding := IsActive()
if h := appData.held[k]; h != nil && h.mouse && !holding {
    noteOff(k, true)
}
```

Keyboard: each frame compares `InputState.DownKeys` to the previous set and
fires note on/off on edges. Cmd/Ctrl suppress piano keys so shortcuts stay free
(`handleKeyboard` in `main.go`). Mouse-held and keyboard-held notes are tracked
separately so the two paths do not fight.

## Run it

```shell
go run .                          # inside examples/piano
go run . --png out.png
go run . --play string
go run . --wav out.wav flute
```
