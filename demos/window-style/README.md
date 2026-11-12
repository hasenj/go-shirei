# Window style demo

On macOS, Windows, and Wayland, `app.SetupTransparent()` preserves per-pixel alpha.
Wayland supports both GPU and software rendering; transparent pixels remain
inside the normal window input region.
`app.SetupUndecorated()` omits the title bar and standard window buttons.
Call either or both before `app.Run`.
Unsupported platforms ignore each option. On macOS, transparency also
disables the native window shadow.

From the repository root:

```sh
go run ./shirei/demos/window-style
```

For a Windows smoke test from macOS with the Wine helper:

```sh
~/bin/go_run_wine.sh ./shirei/demos/window-style --wine
```

Pass demo flags after `--`, for example `-- -undecorated=false`. The renderer
label indicates whether GPU rendering or the software fallback is active.

The demo enables both options. Put it over another window: the gaps and rounded
corners reveal that window, and the colored swatches blend with it. Adjust panel
opacity, type in the field, hide the swatches, and use Small/Large to resize.
The opaque-background checkbox fills the whole content area. Close exits the app.

Other launch modes:

```sh
# Software rendering
SHIREI_GPU=0 go run ./shirei/demos/window-style

# Transparency with standard window decorations
go run ./shirei/demos/window-style -undecorated=false

# An opaque window without decorations
go run ./shirei/demos/window-style -transparent=false

# An ordinary opaque, decorated window
go run ./shirei/demos/window-style -transparent=false -undecorated=false

# Open without taking focus
go run ./shirei/demos/window-style -quiet
```

These are startup options. Transparent pixels have the platform's native mouse
hit-testing behavior; the demo does not define a custom click-through region.
On Windows, pixels with zero alpha pass mouse input through. Resizing from an
undecorated edge requires visible pixels there; the Small/Large buttons work
with fully transparent margins. Windows transparency uses layered-window
presentation; the title bar and borders are painted by Windows into the same
image as the client content.
`-close-after=5s` closes automatically for launch checks.

On Wayland with layer-shell v4 support, undecorated main and popup windows use
centered top-layer surfaces. They have no native title bar or border, sit above
normal application windows across workspaces, and accept keyboard focus on
demand (click the window to focus it). They reserve no desktop space.
Use Close and Small/Large inside the demo; compositor move/resize shortcuts do
not manage layer surfaces. Layer windows do not appear in the normal task switcher.

Other Wayland compositors use undecorated toplevels with compositor-dependent
chrome. `SHIREI_LAYER_SHELL=0` selects that toplevel path explicitly.
