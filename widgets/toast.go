package widgets

import (
	"sync"
	"time"

	. "go.hasen.dev/shirei"
)

// ToastDuration is the default auto-dismiss lifetime when ToastAttrs.Duration is 0.
var ToastDuration = 5 * time.Second

// DefaultToastWidth is the card width when ToastAttrs.Width is 0.
var DefaultToastWidth f32 = 360

// ToastCorner places a toast stack against a window corner.
type ToastCorner int

const (
	ToastBottomRight ToastCorner = iota
	ToastBottomLeft
	ToastTopRight
	ToastTopLeft
)

// ToastId identifies one active toast for DismissToast.
type ToastId int64

// ToastAttrs configures a notification card. The general entry point is
// ToastExt; Toast / ToastMessage / ToastWithAccent are thin wrappers.
//
// When Content is set, it replaces the default icon/title/body block. The
// card chrome (background, dismiss, countdown bar) still applies unless
// disabled via flags.
type ToastAttrs struct {
	Title string
	Body  string

	// Icon is a font glyph drawn at the leading edge. Ignored when Image != 0
	// or when Content is set.
	Icon IconGlyph
	// Image is an optional leading bitmap (e.g. from UseImage). Takes
	// precedence over Icon. Ignored when Content is set.
	Image ImageId

	// Content, when non-nil, replaces the icon/title/body area with arbitrary
	// UI. The card still provides dismiss and the countdown bar.
	Content func()

	Background Vec4 // card fill; zero → dark translucent default
	TitleColor Vec4 // zero → near-white
	BodyColor  Vec4 // zero → light gray
	Accent     Vec4 // countdown bar; zero → DefaultAccent

	// Duration until auto-dismiss. 0 → ToastDuration; negative → sticky
	// (no timer, no auto-dismiss).
	Duration time.Duration

	Corner ToastCorner // zero value is ToastBottomRight
	Width  f32         // zero → DefaultToastWidth

	NoDismiss bool // hide the × control
	NoTimer   bool // hide the countdown bar (auto-dismiss still runs if Duration ≥ 0)
}

// Common card backgrounds (HSLA). Assign to ToastAttrs.Background.
var (
	ToastBackgroundDefault = Vec4{0, 0, 18, 0.92}
	ToastBackgroundSuccess = Vec4{140, 45, 22, 0.94}
	ToastBackgroundWarning = Vec4{40, 70, 28, 0.94}
	ToastBackgroundDanger  = Vec4{5, 65, 28, 0.94}
	ToastBackgroundInfo    = Vec4{210, 55, 28, 0.94}
)

type toastEntry struct {
	id    ToastId
	attrs ToastAttrs
	born  time.Time
	until time.Time // zero when sticky
}

var (
	toastMu     sync.Mutex
	toasts      []toastEntry
	nextToastId ToastId

	// toastLayouts is rebuilt every toastFrame while cards paint. Readable
	// after the popup pass; geometry via GetResolvedRectOf is previous-frame.
	toastLayouts []ToastLayout
)

// ToastLayout holds identity handles for one painted toast card. Use with
// GetResolvedRectOf / GetScreenRectOf (previous-frame geometry).
type ToastLayout struct {
	Id        ToastId
	CardId    ContainerId
	DismissId ContainerId // nil when NoDismiss
}

// ToastCount returns how many toasts are currently queued (including those
// that have not painted a frame yet).
func ToastCount() int {
	toastMu.Lock()
	n := len(toasts)
	toastMu.Unlock()
	return n
}

// ActiveToastLayouts returns the cards painted in the latest toastFrame pass.
func ActiveToastLayouts() []ToastLayout {
	return toastLayouts
}

func init() {
	RegisterFramePopup(toastFrame)
}

// Toast pushes a toast with icon, title, and body.
func Toast(icon IconGlyph, title, msg string) ToastId {
	return ToastExt(ToastAttrs{Icon: icon, Title: title, Body: msg})
}

// ToastMessage pushes a body-only toast with package defaults.
func ToastMessage(msg string) ToastId {
	return ToastExt(ToastAttrs{Body: msg})
}

// ToastWithAccent is Toast plus a countdown-bar accent color.
func ToastWithAccent(icon IconGlyph, title, msg string, accent Vec4) ToastId {
	return ToastExt(ToastAttrs{Icon: icon, Title: title, Body: msg, Accent: accent})
}

// ToastExt adds a toast to the stack and returns its id. Toasts with the same
// Corner stack together; the app should avoid flooding the stack.
func ToastExt(attrs ToastAttrs) ToastId {
	if attrs.Width <= 0 {
		attrs.Width = DefaultToastWidth
	}
	if attrs.Background == (Vec4{}) {
		attrs.Background = ToastBackgroundDefault
	}
	if attrs.TitleColor == (Vec4{}) {
		attrs.TitleColor = Vec4{0, 0, 98, 1}
	}
	if attrs.BodyColor == (Vec4{}) {
		attrs.BodyColor = Vec4{0, 0, 85, 1}
	}
	if attrs.Accent == (Vec4{}) {
		attrs.Accent = DefaultAccent
	}

	now := time.Now()
	var until time.Time
	dur := attrs.Duration
	if dur == 0 {
		dur = ToastDuration
		attrs.Duration = dur
	}
	if dur > 0 {
		until = now.Add(dur)
	}

	toastMu.Lock()
	nextToastId++
	id := nextToastId
	toasts = append(toasts, toastEntry{id: id, attrs: attrs, born: now, until: until})
	toastMu.Unlock()

	RequestNextFrame()
	return id
}

// DismissToast removes one toast by id. Unknown ids are ignored.
func DismissToast(id ToastId) {
	toastMu.Lock()
	for i, t := range toasts {
		if t.id == id {
			toasts = append(toasts[:i], toasts[i+1:]...)
			break
		}
	}
	toastMu.Unlock()
}

// DismissAllToasts clears the entire stack.
func DismissAllToasts() {
	toastMu.Lock()
	toasts = toasts[:0]
	toastMu.Unlock()
}

func toastFrame() {
	toastLayouts = toastLayouts[:0]

	toastMu.Lock()
	now := time.Now()
	alive := toasts[:0]
	for _, t := range toasts {
		if !t.until.IsZero() && now.After(t.until) {
			continue
		}
		alive = append(alive, t)
	}
	toasts = alive
	if len(toasts) == 0 {
		toastMu.Unlock()
		return
	}
	snapshot := append([]toastEntry(nil), toasts...)
	toastMu.Unlock()

	RequestNextFrame()

	byCorner := [4][]toastEntry{}
	for _, t := range snapshot {
		c := t.attrs.Corner
		if c < ToastBottomRight || c > ToastTopLeft {
			c = ToastBottomRight
		}
		byCorner[c] = append(byCorner[c], t)
	}

	Popup(func() {
		ws := GetHost().WindowSize
		const pad f32 = 16
		const stackGap f32 = 8
		for corner, list := range byCorner {
			if len(list) == 0 {
				continue
			}
			bottom := corner == int(ToastBottomRight) || corner == int(ToastBottomLeft)
			right := corner == int(ToastBottomRight) || corner == int(ToastTopRight)
			main := AlignStart
			cross := AlignStart
			if bottom {
				main = AlignEnd
			}
			if right {
				cross = AlignEnd
			}
			// Bottom: oldest first so newest sits on the corner.
			// Top: newest first so newest sits on the corner.
			ordered := list
			if !bottom {
				ordered = append([]toastEntry(nil), list...)
				for i, j := 0, len(ordered)-1; i < j; i, j = i+1, j-1 {
					ordered[i], ordered[j] = ordered[j], ordered[i]
				}
			}
			// ClickThrough on the full-window host so empty areas do not steal
			// hits; each card opts back in with NoClickThrough.
			Container(Attrs(Float(0, 0), FixSizeVec(ws), ClickThrough, Pad(pad),
				MainAlign(main), CrossAlign(cross), Gap(stackGap)), func() {
				for _, t := range ordered {
					toastCard(t, now)
				}
			})
		}
	})
}

func toastCard(t toastEntry, now time.Time) {
	a := t.attrs
	w := a.Width
	const pad f32 = 14
	const dismissBox f32 = 28
	const iconBox f32 = 28
	const gap f32 = 10

	var remaining f32
	showTimer := !a.NoTimer && !t.until.IsZero() && a.Duration > 0
	if showTimer {
		total := a.Duration.Seconds()
		left := t.until.Sub(now).Seconds()
		if total > 0 {
			remaining = f32(left / total)
			if remaining < 0 {
				remaining = 0
			}
			if remaining > 1 {
				remaining = 1
			}
		}
	}

	var dismissId ContainerId
	// NoClickThrough: sit inside the ClickThrough corner host but accept hits.
	cardId := ContainerWithKey(t.id, Attrs(NoClickThrough, FixWidth(w), Clip, Corners(8),
		BackgroundVec(a.Background), BoxShadow(12), NoAnimate), func() {
		// Expand so this column takes the card width; without it the row is
		// content-sized and Filler/Grow have no leftover to push × right.
		Container(Attrs(Expand, Pad(pad), Gap(8)), func() {
			textMax := w - pad*2
			if !a.NoDismiss {
				textMax -= dismissBox + gap
			}

			if a.Content != nil {
				Container(Attrs(Row, Expand, CrossAlign(AlignStart), Gap(gap)), func() {
					Container(Attrs(MaxWidth(textMax), Gap(4)), func() {
						a.Content()
					})
					if !a.NoDismiss {
						Filler(1)
						dismissId = toastDismissButton(t.id, a.TitleColor)
					}
				})
				return
			}

			hasLead := a.Image != 0 || a.Icon.Rune != 0
			if hasLead {
				textMax -= iconBox + gap
			}
			if textMax < 40 {
				textMax = 40
			}

			Container(Attrs(Row, Expand, CrossAlign(AlignStart), Gap(gap)), func() {
				if a.Image != 0 {
					ImageView(a.Image, Vec2{iconBox, iconBox})
				} else if a.Icon.Rune != 0 {
					Container(Attrs(FixSize(iconBox, iconBox), Center), func() {
						Icon(a.Icon, FontSize(18), TextColorVec(a.TitleColor))
					})
				}

				Container(Attrs(MaxWidth(textMax), Gap(4)), func() {
					if a.Title != "" {
						Label(a.Title, FontSize(13), FontWeight(WeightBold), TextColorVec(a.TitleColor))
					}
					if a.Body != "" {
						Label(a.Body, FontSize(12), TextColorVec(a.BodyColor))
					}
				})

				if !a.NoDismiss {
					Filler(1)
					dismissId = toastDismissButton(t.id, a.TitleColor)
				}
			})
		})

		if showTimer {
			// Countdown: full → empty (remaining fraction of lifetime).
			barH := comfort(3)
			Container(Attrs(Expand, FixHeight(barH), Background(0, 0, 100, 0.12), NoAnimate, Clip), func() {
				Element(Attrs(FixWidth(w*remaining), FixHeight(barH), BackgroundVec(a.Accent), NoAnimate))
			})
		}
	})
	toastLayouts = append(toastLayouts, ToastLayout{Id: t.id, CardId: cardId, DismissId: dismissId})
}

func toastDismissButton(id ToastId, fg Vec4) ContainerId {
	return ContainerWithKey(toastDismissKey(id), Attrs(FixSize(28, 28), Center, Corners(4)), func() {
		if IsHovered() {
			ModAttrs(Background(0, 0, 100, 0.15))
		}
		if PressAction() {
			DismissToast(id)
		}
		Icon(TypTimes, FontSize(12), TextColorVec(fg))
	})
}

type toastDismissKey ToastId
