# jsbackend

Browser/wasm shell for shirei. Same contract as every other software backend:
sample input, `RunFrameFn`, `SoftRenderer.RenderInto`, present a bitmap.

## Build a static site

From the shirei module:

```bash
go run ./cmd/shirei_web -o dist ./demos/todo
```

Writes `index.html`, `wasm_exec.js`, `main.wasm`, and sprouts `.headers`
(COOP+COEP for SharedArrayBuffer audio) into `dist/`. Copy that folder to any
static host that respects `.headers` (or set the same response headers itself).
The landing-page interactive sample is `demos/small`.

## Local preview

```bash
go run ./cmd/shirei_web -run ./demos/todo
```

Build into a temp dir, serve on `http://127.0.0.1:8787/`, open a browser.

```bash
go run ./cmd/shirei_web -serve -o dist -n ./demos/theme
```

Build into `dist/`, serve, do not open a browser (`-n`).

## Rebuild published web demos

The landing sample and two static galleries live under
`static-sites/.../shirei/`:

| URL path | Gallery set | Contents |
|----------|-------------|----------|
| `/shirei/try` | standalone build | Complete small demo from the landing page |
| `/shirei/demos` | `-gallery=demos` | Cards in `demos/index.scgo` (not every `demos/*` package) |
| `/shirei/custom-widgets` | `-gallery=custom-widgets` | Cards in `custom-widgets/index.scgo` |

Gallery membership lives in `cmd/shirei_web/gallery.go` and should stay aligned
with those scgo index pages. Each gallery app page shows
`Source: demos/<slug>` linking to `github.com/hasenj/go-shirei`.

From the Shirei module, rebuild all three:

```bash
./rebuild-web-demos.sh
```

The script accepts an alternate site root as its first argument. Rebuild removes
stale demo dirs under `…/apps/` that are no longer in the gallery set.

Screenshots (optional refresh; demos that support `--png`):

```bash
go run ./demos/balls-buckets --png ../static-sites/judi.systems/shirei/demos/shots/balls-buckets.png
go run ./demos/custom-buttons --png ../static-sites/judi.systems/shirei/custom-widgets/shots/custom-buttons.png
```

## Fonts

System font directory scan is empty on `js`. This package embeds **Noto Sans
Regular** and registers it via `UseFontBytes` at startup so UI text works without
a permission prompt. Apps can still call `UseFontBytes` or the page can inject
bytes through the global:

```js
// ArrayBuffer of a TTF/OTF
window.shireiUseFontBytes(arrayBuffer)
```

Chromium **Local Font Access** (`queryLocalFonts` → `blob`) can feed the same
hook for full system-face demos; it is progressive enhancement, not required.

## Layout of the page

Default HTML (from `shirei_web`) is a shell + canvas:

```html
<div id="shirei-root">
  <canvas id="shirei-canvas" tabindex="0"></canvas>
</div>
```

`SetupWindow(title, w, h)` is the **content** size (same contract as
macOS/Win32). **Top-level** pages float a shell of height `h + 34` on the page
background with **client-side decorations** (same idea as Wayland CSD): a
soft-rendered title bar (drag to move, close button) and an edge hit zone to
resize. The app body keeps `w×h`, and after each frame `GetHost().WindowSize`
reports that content size (not the shell).

**Inside an iframe** there is no CSD: the document shrink-wraps to exactly
`w×h` and posts:

```js
{ source: "shirei", type: "resize", width: w, height: h }
```

so the parent can size the iframe. Include `embed.js` from a `shirei_web`
build on the gallery page:

```html
<script src="./embed.js"></script>
<iframe src="./small/" loading="lazy"></iframe>
```

Pass `0, 0` to fill the viewport (no chrome). The backend creates the shell if
missing. A hidden `<input id="shirei-text">` captures IME composition and typed
text.
