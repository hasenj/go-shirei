# Furigana motion promo

A silent 24-second, 1920×1080, 30 fps promo in Shirei's paper and burgundy palette.

- 0–6 s: Japanese readings appear above the base text.
- 6–12 s: sentence spacing, per-character annotations, and neighboring readings.
- 12–18 s: a marked string carries its reading from a label to a text field.
- 18–24 s: Shirei end card and project URL.

The Japanese glyphs and spacing come from Shirei's renderer. The cards,
selection, caret, and transitions are motion-graphics illustrations.

## Render

From the repository root:

```sh
go run ./shirei/promos/furigana
python3 shirei/promos/furigana/render.py
```

The Python renderer requires Pillow and an `ffmpeg` executable on PATH.
It uses the macOS Avenir Next and Menlo fonts by default. Pass `--font` and
`--mono` with alternative font paths on other systems. Shirei uses its normal
system-font discovery for Japanese text.

Use `--preview` to render a storyboard and selected frames without encoding.
Generated files live in the ignored `output/` directory:

- `shirei-furigana.mp4`: H.264, yuv420p, with streaming metadata at the front.
- `poster.jpg`: opening title card.
- `storyboard.jpg`: eight timestamped frames.
- `assets/`: base and reading layers exported by the Go program.

The scene timings, wording, colors, and animation curves are in `render.py`.
