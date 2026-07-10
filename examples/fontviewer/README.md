# fontviewer

Preview sample text in every font family shirei can find on the system.

![fontviewer](fontviewer.webp)

## What it does

Type (or shuffle) sample text; every discovered family renders it on a card.
A filter box narrows by name, a slider sets preview size (12–72px), and
clicking a card copies the family name for use in `Fonts(...)` elsewhere.

Font files are prewarmed in the background so the grid stays scrollable while
hundreds of families load. Cards that are not ready yet show a skeleton instead
of blocking the frame on a synchronous parse.

## What it shows (shirei)

A small but complete “gallery” app: toolbar + virtualized multi-column grid +
pointer identity + system font selection.

### Responsive columns from resolved size

Column count is derived from `GetResolvedSize()` on the grid panel. On the first
frame the width is often still zero; the code requests another frame and waits.

See `main.go`: `FontGrid`.

### Identity with `ContainerWithKey`

Each card is keyed by the `*FontFamily` pointer so hover and “Copied” feedback
follow the family when filtering reshuffles the grid.

See `main.go`: `FontCard`.

### Per-label font selection

Sample text uses the family’s name via `Fonts(...)` (and a fixed text width for
wrapping), which is exactly how app code picks a face:

```go
Label(appData.sample, Fonts(fam.Name), FontSize(appData.fontSize), TextWidth(...), ...)
```

### Headless vs interactive paths

`--png` skips background prewarm so snapshot tests stay deterministic; the
windowed path starts `startPrewarm`. Same `RootView`, two data-loading modes.

## Run it

```shell
go run .                 # inside examples/fontviewer
go run . --png out.png
```
