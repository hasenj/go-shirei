# Dark-mode motion promo

A silent 24-second, 1920×1080, 30 fps companion to the furigana promo.

- 0–6 s: a circular light-to-dark reveal expands from an appearance switch.
- 6–12 s: a sliding comparison shows the same controls in both modes.
- 12–18 s: cool light, cool dark, warm light, and warm dark presets.
- 18–24 s: follow-system appearance, manual choice, and the Shirei end card.

All application screenshots come from Shirei's renderer and stock widgets.
The exporter selects the preferred scheme pair and calls `SetDarkMode`.
Its surfaces use `UseSurface` and its controls resolve their normal scheme
colors. Palette swatches come from exported `ColorScheme` values.

The circular reveal, comparison wipes, large switch, and system selector are
editorial motion graphics. They illustrate appearance choices rather than
promise built-in transition effects. Following the operating system uses
`ext/darkmode`; the application owns the follow-system or manual preference.

## Render

From the repository root:

```sh
go run ./shirei/promos/darkmode
python3 shirei/promos/darkmode/render.py
```

The Python renderer requires Pillow and an `ffmpeg` executable on PATH.
It uses macOS Avenir Next and Menlo by default. `--font` and `--mono` accept
alternative font files. Shirei uses its normal system-font discovery for
application screenshots.

Use `--preview` to generate selected frames and a storyboard without encoding.
Generated files live in the ignored `output/` directory:

- `shirei-darkmode.mp4`: H.264, yuv420p, streaming metadata at the front.
- `poster.jpg`: dark-mode title card.
- `storyboard.jpg`: eight timestamped frames.
- `assets/`: four-palette application/control captures and palette colors.

Scene timings, wording, typography, and transitions are in `render.py`.
The rendered application and control samples are in `main.go`.
