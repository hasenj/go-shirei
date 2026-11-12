# Layout motion promo

A silent 24-second, 1920×1080, 30 fps promo for Shirei's flexbox-style layout.

- 0–6 s: a workspace assembles from its header, sidebar, and main panel.
- 6–12 s: three containers move between a centered row and a centered column.
- 12–18 s: a growing row shares extra width with weights of 1 : 2 : 1.
- 18–24 s: a real interface resizes with a fixed sidebar and a growing main panel.

`main.go` exports row, column, and growth geometry from Shirei's layout engine.
The growth example gives every child an 80-point minimum width. Its labels
show the extra width beyond that minimum; the short rules mark the minimum.
The app captures cover 61 viewport widths from 580 to 820 logical points at
a constant height. The compositor displays these at a constant scale, so
text and the fixed-width sidebar retain their size during resizing.

The assembly effect, row-to-column interpolation, diagrams, measurement lines,
and labels are editorial motion graphics. Shirei computes the diagram's
layout endpoints and each growth sample. Application screenshots come from
Shirei's renderer and stock widgets.

Shirei uses a flexbox-style model with its own sizing rules. Containers size
from content by default; `Grow` distributes leftover main-axis space and
`Expand` fills the cross axis. A column is the default direction; `Row`
selects horizontal layout. See the [layout tutorial](../../docs/layout-tutorial.md)
for complete examples.

## Render

From the repository root:

```sh
go run ./shirei/promos/layout
python3 shirei/promos/layout/render.py
```

The Python renderer requires Pillow and an `ffmpeg` executable on PATH. It
uses macOS Avenir Next and Menlo by default; `--font` and `--mono` accept
alternative font files. Application screenshots use Shirei's system fonts.

Use `--preview` to produce selected frames and a storyboard without encoding.
Generated files live in the ignored `output/` directory:

- `shirei-layout.mp4`: H.264, yuv420p, streaming metadata at the front.
- `poster.jpg`: opening title card.
- `storyboard.jpg`: eight timestamped frames.
- `assets/`: application screenshots and engine-computed geometry.

The layout samples are in `main.go`. Wording, timing, typography, and motion
are in `render.py`.
