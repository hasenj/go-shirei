# Drive motion promo

A silent 24-second, 1920×1080, 30 fps companion to the furigana and dark-mode promos.

- 0–6 s: a native Shirei app and a real `ping` / `pong` exchange.
- 6–12 s: `query save` exposes the Save button's identity, name, bounds, and role.
- 12–18 s: text input and a click update the live application's state.
- 18–24 s: `show status` confirms the result, followed by the Shirei end card.

The application runs in a native window with `app.SetupDrive()`. The capture
program connects over loopback UDP, queries named controls, enters “Night
garden,” clicks Save, and checks the returned status. All application images
come from Drive's `screenshot` command. `session.json` contains the actual
responses and target rectangles used for the overlays.

The terminal, pointer, outlines, and transitions are editorial motion graphics.
The sequence uses captured states with presentation timing; it is not a
real-time screen recording or a latency benchmark. The Go helper snippets
express the captured gestures and omit error handling for readability. The
final status display selects the path, role, and value from the full response.

## Render

From the repository root, with a graphical desktop session available:

```sh
go run ./shirei/promos/drive
python3 shirei/promos/drive/render.py
```

The first command opens a quiet demonstration window, captures its interaction
sequence, and closes it. `go run ./shirei/promos/drive -app` opens the same
application for manual use. The demo stores its state in memory.

The Python renderer requires Pillow and an `ffmpeg` executable on PATH. It
uses macOS Avenir Next and Menlo by default; `--font` and `--mono` accept
alternative font files. Application screenshots use Shirei's system fonts.

Use `--preview` to produce selected frames and a storyboard without encoding.
Generated files live in the ignored `output/` directory:

- `shirei-drive.mp4`: H.264, yuv420p, streaming metadata at the front.
- `poster.jpg`: opening title card.
- `storyboard.jpg`: eight timestamped frames.
- `assets/`: live screenshots and `session.json`.

The application and capture sequence are in `main.go`. Wording, timing,
typography, and overlays are in `render.py`. See the
[Drive command reference](../../docs/drive.md) for the actual interface.
