package shirei

import (
	"runtime"
	"sync/atomic"
	"time"
)

// InputStateData is persistent input (mouse, keys, touches, composition).
// Backends write; widgets read. Stored on the active UI as ui.Host.Input.
type InputStateData struct {
	MousePoint  Vec2
	MouseButton MouseButton

	DownKeys []KeyCode

	// control keys state
	Modifiers Modifiers

	Composition    string // text being input via IME
	CompositionSel [2]int // selected clause/caret as rune offsets into Composition

	// Touches is the current contact set (multi-touch). Slots with Active
	// false are free; Id is meaningful only when Active. Filled by backends
	// that have touch; empty on mouse-only platforms.
	Touches [MaxTouches]TouchInfo

	// MouseFromTouch is true while the backend is driving the mouse from a
	// finger (including the short hold after a synthetic tap). Prefer
	// IsTouched for hit-testing while this is set so a delayed mouse-up
	// cannot re-engage a control after the finger has lifted.
	MouseFromTouch bool

	// AudioInterrupted is set by the platform audio backend when the OS
	// suspends output (phone call, Siri, another app taking the session on
	// iOS, etc.) and cleared when the interruption ends. Apps and media
	// widgets can pause/resume synthesis; the backend still owns restarting
	// the device stream. False on platforms that never report interruptions.
	AudioInterrupted bool
}

// FrameInputData is transient per-frame input (click, scroll, text, touch edges).
// Stored on the active UI as ui.Host.FrameInput; cleared at end of each pass.
type FrameInputData struct {
	Mouse  MouseAction
	Motion Vec2 // mouse movement
	Scroll Vec2

	// ClickCount is the click-streak position of this frame's MouseClick:
	// 1 for a single click, 2 for the second click of a double-click, and
	// so on (macOS style). Computed by core at frame start from click
	// timing and position — backends only deliver the clicks. Valid only
	// when Mouse == MouseClick.
	ClickCount int

	Key KeyCode

	Text string // text inputted this frame (could come from IME completion)

	// Touch edges this frame (ids only). Counts are the number of valid
	// leading entries; remaining slots are unspecified.
	TouchesBegan      [MaxTouches]uint32
	TouchesBeganCount int
	TouchesEnded      [MaxTouches]uint32
	TouchesEndedCount int
}

// PreferredOrientation is an app → backend policy for how the window should
// sit relative to the device. Mobile backends lock the OS interface
// orientation so WindowSize, safe area, and the soft keyboard follow that
// policy. Desktop backends ignore it. Default is OrientationAny.
type PreferredOrientation int

const (
	// OrientationAny follows the device (no lock).
	OrientationAny PreferredOrientation = iota
	// OrientationPortrait locks to portrait (sensor may flip upright).
	OrientationPortrait
	// OrientationLandscape locks to landscape (sensor may pick left/right).
	OrientationLandscape
)

// Host is the backend ↔ app I/O channel for one UI (nested on UI as ui.Host):
// window geometry, input, clipboard requests, IME anchors, next-frame, etc.
// All Host fields live here — there are no parallel package-level scalar vars.
type Host struct {
	Input      InputStateData
	FrameInput FrameInputData

	// Window (backend → core)
	WindowSize    Vec2
	WindowScale   float32 // device pixels per logical point; default 1
	WindowFocused bool

	// HardwareKeyboard is backend → app: true when a physical keyboard is
	// available (built-in laptop keys, USB/Bluetooth keyboard, etc.). False
	// for soft/IME-only devices. May change at runtime when a keyboard is
	// attached or detached (phones/tablets). Desktop backends set true;
	// headless defaults true so snapshots match desktop chrome.
	HardwareKeyboard bool

	// ComfortScale multiplies default control geometry (button/input text size
	// and padding, segment height, slider handle, checkbox/toggle size, …) so
	// touch-first devices get larger hit targets without changing desktop
	// density. Design units are authored at scale 1; widgets do
	// `size * ComfortScale` with no zero-sentinel branch. Backends set this
	// once at startup (desktop/headless: 1; phone: typically ~1.25). Apps may
	// override. Always initialize to a positive value — never leave at 0.
	ComfortScale float32

	// PrimaryMod is the platform shortcut modifier for editing and similar
	// chords (select-all, copy, undo, …): ModCmd on Apple hosts, ModCtrl
	// elsewhere. Zero means PrimaryMod() derives it from GOOS (darwin → Cmd).
	// Web backends set this from the browser platform because GOOS is always
	// "js" and would otherwise always pick Ctrl.
	PrimaryMod Modifiers

	// PixelOrder maps each destination byte slot to a source channel of
	// (R,G,B,A): slot k stores source channel PixelOrder[k]. Backends set this
	// once at window setup so SoftRenderer writes presentable memory directly.
	// Zero (all zeros) means PixelOrderBGRA. Do not change after the first
	// frame — image and region caches hold bytes in the current order.
	//
	//	PixelOrderBGRA = {2,1,0,3}  // default; win32/cocoa/wayland/x11
	//	PixelOrderRGBA = {0,1,2,3}  // canvas ImageData, Android ANativeWindow
	PixelOrder [4]uint8

	// PreferredOrientation is app → backend: sticky orientation policy
	// (unlike WantsKeyboard, it is not cleared each frame). Set once at
	// startup, e.g. GetHost().PreferredOrientation = OrientationLandscape.
	// Mobile backends apply it via the OS; 0 (OrientationAny) is the default.
	PreferredOrientation PreferredOrientation

	// IME / soft keyboard (core → backend)
	CaretPos       Vec2
	CaretHeight    float32
	CompositionPos Vec2
	WantsKeyboard  bool

	// Clipboard / open-URL requests (core → backend via FrameOutputData)
	Copy    string
	Paste   bool
	OpenURL string // non-empty: open this URL after the frame (last write wins)

	// NextFrame is set when the UI wants another frame without input
	// (animations, settle). Backends also read FrameOutputData.NextFrameRequested.
	NextFrame atomic.Bool

	// Diagnostics: LayoutTime is produce (RunFrameFn); PaintTime is the last
	// SoftRenderer pass (set at the end of Render / RenderInto). TotalFrameTime
	// is reserved for backends that want produce+present wall time.
	TotalFrameTime time.Duration
	LayoutTime     time.Duration
	PaintTime      time.Duration
	ImageScaleTime time.Duration

	// HeadlessRender is set for RenderToPNG / snapshot paths.
	HeadlessRender bool

	// GlyphCacheBudgetBytes is the soft cap for the shared glyph cache; 0 disables.
	// Backend sets this once at startup (process-wide effect via Resources).
	GlyphCacheBudgetBytes int

	// EscapeHatchBackendContext is the live platform host object for
	// extensions that must call OS APIs outside Shirei's normal surface
	// (camera, share sheet, system pickers, …). The active backend sets it
	// when the window/activity is ready; nil when headless or before attach.
	//
	// This is intentionally not a casual Host field: prefer portable Shirei
	// APIs and extension packages. Type-assert to the concrete backend
	// context (iosbackend.Context, androidbackend.Context, …) only inside
	// those packages — not in ordinary app UI code.
	// See docs/mobile-extensions.md.
	EscapeHatchBackendContext BackendContext
}

// GetHost returns a pointer to the active UI's Host I/O channel.
// Always follows the current ui after bindUI / Measure swaps.
func GetHost() *Host {
	return &ui.Host
}

// GetInputState returns a pointer to the active UI's persistent input.
// Always follows the current ui after bindUI / Measure swaps — no rebind.
func GetInputState() *InputStateData {
	return &ui.Host.Input
}

// GetFrameInput returns a pointer to the active UI's per-frame input edges.
func GetFrameInput() *FrameInputData {
	return &ui.Host.FrameInput
}

func defaultHost() Host {
	return Host{
		WindowScale:      1,
		WindowFocused:    true,
		HardwareKeyboard: true, // desktop / headless default; mobile overwrites
		ComfortScale:     1,    // desktop density; mobile backends raise this
		PixelOrder:       PixelOrderBGRA,
	}
}

// Soft-renderer framebuffer channel layouts (see Host.PixelOrder).
var (
	PixelOrderBGRA = [4]uint8{2, 1, 0, 3}
	PixelOrderRGBA = [4]uint8{0, 1, 2, 3}
)

// ComfortScale returns Host.ComfortScale for the active UI. Widget defaults
// multiply design-unit sizes by this (see Host.ComfortScale). Always a real
// multiplier — backends and defaultHost set it to 1; do not treat 0 as 1.
func ComfortScale() float32 {
	return ui.Host.ComfortScale
}

// PrimaryMod returns the shortcut modifier for the active host: Host.PrimaryMod
// when set by the backend, otherwise Cmd on darwin and Ctrl on every other
// GOOS (including js unless the backend overrides).
func PrimaryMod() Modifiers {
	if m := ui.Host.PrimaryMod; m != 0 {
		return m
	}
	if runtime.GOOS == "darwin" {
		return ModCmd
	}
	return ModCtrl
}
