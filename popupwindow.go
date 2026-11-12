package shirei

// PopupWindow identifies a native auxiliary window. Zero is not a window.
type PopupWindow uint64

// PopupWindowOptions configures a native auxiliary window independently of its
// owner. Width and Height are positive content dimensions in logical points.
// Windows placement, activation, and stacking are best-effort and have not been
// verified on physical Windows hardware; the shell may adjust them.
type PopupWindowOptions struct {
	Title         string
	Width, Height int
	Transparent   bool
	Undecorated   bool
	// Position places the popup's top-left corner relative to the owner's
	// content top-left. Nil centers it over the owner. Placement stays inside
	// the owner's display's visible area when the popup fits that area.
	// On Wayland this applies only to a layer popup with an explicitly
	// positioned layer owner; other windows use compositor placement.
	Position *Vec2

	// ScreenPosition places the popup's top-left corner in screen points, with
	// the origin at the top-left of the main display. It overrides Position.
	// Supported on macOS, Windows, and Wayland layer windows. Wayland uses
	// the compositor logical desktop origin; without xdg-output geometry the
	// coordinates are local to the selected output. Layer windows select the
	// containing or nearest output and clamp to its bounds, including reserved
	// areas. Cross-output migration during a primary-button drag waits for release.
	// Ordinary Wayland toplevels and other backends ignore it.
	// MovePopupWindow updates a live popup with the same coordinates.
	ScreenPosition *Vec2

	// ExactScreenPosition keeps ScreenPosition exactly as given instead of
	// clamping it to the visible screen frame. Use for full-display overlays
	// whose local coordinates must match screen coordinates.
	// Supported on macOS, Windows, and Wayland layer windows. Wayland uses
	// the full output bounds and permits negative margins; the compositor
	// can clip parts outside that output.
	ExactScreenPosition bool

	// Floating keeps the popup above normal windows when its owner app is
	// inactive. The app is not activated and the popup does not become key.
	// Supported on macOS, Windows, and Wayland with layer-shell v4. Wayland
	// uses a top-layer surface, with client-drawn chrome when decorated.
	// Without layer-shell the ordinary toplevel fallback follows compositor
	// stacking policy.
	Floating bool

	// Focus makes the popup key when it opens, even if the owner was set up as
	// a quiet background window. Use for interactive overlays.
	// Supported on macOS, Windows, and Wayland. Wayland requests activation
	// with xdg-activation when available; the compositor can deny focus.
	Focus bool

	// Nonactivating keeps a click on the popup from activating the owner app.
	// The popup still receives pointer input and runs its UI, but the app that
	// was frontmost stays frontmost. This is separate from Focus, which only
	// controls activation when the popup opens.
	// Nonactivating takes precedence over Focus. Supported on macOS and Windows.
	// Wayland layer popups with Focus false already avoid requesting activation;
	// other backends and ordinary Wayland toplevels follow compositor policy.
	Nonactivating bool

	// TopLevel makes the popup a normal-level top-level window instead of a
	// child of the tiny background owner. It stays visible when the app is
	// inactive, but is not always-on-top and does not force activation. Use
	// for boards and other persistent utility windows.
	// Supported on macOS, Windows, and Wayland. On Wayland this selects an
	// ordinary toplevel even when Undecorated is true; decorations remain
	// subject to compositor policy. Floating takes precedence.
	TopLevel bool

	// OnClose runs once on the UI thread after the window closes, including
	// when its owner closes. Process exit (including app.Quit) does not call it.
	OnClose func()
}
