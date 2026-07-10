# icons

Filterable gallery of the icon fonts bundled with shirei.

![icons](icons.webp)

## What it does

Every `Sym*` (Microns) and `Typ*` (Typicons) constant from the `widgets`
package, in a grid you can filter by name. Click an icon for a footer with the
glyph, family, codepoint, and a copy-paste usage snippet
(`Icon(Name)` / `Button(Name, "label")`). Double-click (or the copy control)
puts the constant name on the clipboard.

Use it when you want “what icons do I already have?” without grepping the
widgets sources. The table of icons is generated (`//go:generate`) from those
sources so the gallery stays in sync.

## What it shows (shirei)

A minimal complete app: setup, filter, virtualized grid, selection, clipboard.

### Smallest useful shell

```go
func main() {
    if /* --png ... */ { RenderToPNG(...); return }
    app.SetupWindow("shirei icons", 1080, 720)
    app.Run(RootView)
}
```

`RootView` is header → toolbar → grid → footer. Good template for a first app.

See `main.go`: `main`, `RootView`.

### Multi-column grid on top of `VirtualListView`

The virtual list is still one-dimensional. Each “row” is a horizontal strip of
cells; column count comes from panel width (`GetResolvedSize`).

See `main.go`: `IconGrid`.

### Click vs double-click; key by pointer

`IsClicked` selects; `IsDoubleClicked` copies. Cells use `ContainerWithKey(ic, …)`
so selection survives filter regrouping.

See `main.go`: `IconCell`.

### Clipboard at end of frame

`RequestTextCopy(name)` queues a copy for the backend after the frame. You do
not talk to the OS clipboard mid-layout.

See `main.go`: `copyIconName`.

## Run it

```shell
go run .                 # inside examples/icons
go run . --png out.png
```
