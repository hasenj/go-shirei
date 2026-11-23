# Video Player

A lightweight local video player built with [`go.hasen.dev/shirei/media`](../../docs/video.md). Video frames composite directly through Shirei's GPU pipeline with a translucent HUD hovering above playback.

## Run

```bash
go run .
go run . /path/to/movie.mp4
go run . -float /path/to/movie.mp4
```

Pass `-float` to keep the window floating above other applications.

## Controls

| Action | Shortcut / Input |
|---|---|
| Open file dialog | `⌘O` (macOS) / `Ctrl+O`, or folder button |
| Play / Pause | `Space`, `K`, or click the video |
| Seek ±5 seconds | `←` / `→` or `J` / `L` |
| Mute / Unmute | `M` |
| Dismiss focus | `Esc` |

The HUD auto-hides after a few seconds of pointer inactivity while playing and reappears on pointer movement, keyboard input, or pause.

See [docs/video.md](../../docs/video.md) for supported platforms, hardware acceleration paths, and codec coverage.
