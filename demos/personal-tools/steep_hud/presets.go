package main

import (
	"fmt"
	"strings"
	"time"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

// Category groups quick timer presets by ritual.
type Category int

const (
	CatTea Category = iota
	CatCoffee
	CatEyes
)

var allCategories = []Category{CatTea, CatCoffee, CatEyes}

func (c Category) Label() string {
	switch c {
	case CatTea:
		return "Tea"
	case CatCoffee:
		return "Coffee"
	case CatEyes:
		return "Eyes"
	default:
		return "Timer"
	}
}

func (c Category) Icon() IconGlyph {
	switch c {
	case CatTea:
		return TypLeaf
	case CatCoffee:
		return TypCoffee
	case CatEyes:
		return TypEye
	default:
		return TypStopwatch
	}
}

// AccentHSLA returns the primary luminous accent color for each category.
func (c Category) AccentHSLA() Vec4 {
	switch c {
	case CatTea:
		return Vec4{146, 52, 56, 1} // warm matcha jade
	case CatCoffee:
		return Vec4{28, 82, 58, 1} // roasted copper amber
	case CatEyes:
		return Vec4{195, 74, 60, 1} // restful mist teal
	default:
		return Vec4{210, 60, 60, 1}
	}
}

// SoftTintHSLA returns a subtle translucent pill tint for the active category.
func (c Category) SoftTintHSLA(alpha float32) Vec4 {
	a := c.AccentHSLA()
	return Vec4{a[0], a[1] * 0.72, 26, alpha}
}

// Preset is one quick-launch timer configuration.
type Preset struct {
	ID       string
	Category Category
	Name     string
	Duration time.Duration
	Note     string
}

// DefaultPresets returns the curated presets for tea, coffee, and eye breaks.
func DefaultPresets() []Preset {
	return []Preset{
		// Tea steeping
		{ID: "green", Category: CatTea, Name: "Green", Duration: 2 * time.Minute, Note: "75-80°C · delicate leaves"},
		{ID: "oolong", Category: CatTea, Name: "Oolong", Duration: 3 * time.Minute, Note: "85-90°C · rolled leaves"},
		{ID: "black", Category: CatTea, Name: "Black", Duration: 4 * time.Minute, Note: "95-100°C · full steep"},
		{ID: "herbal", Category: CatTea, Name: "Herbal", Duration: 5 * time.Minute, Note: "100°C · floral & tisane"},

		// Coffee brewing
		{ID: "bloom", Category: CatCoffee, Name: "Bloom", Duration: 45 * time.Second, Note: "Wet grounds · degas"},
		{ID: "pourover", Category: CatCoffee, Name: "Pour-over", Duration: 2*time.Minute + 45*time.Second, Note: "V60 · steady drawdown"},
		{ID: "aeropress", Category: CatCoffee, Name: "AeroPress", Duration: 2 * time.Minute, Note: "Steep · gentle press"},
		{ID: "frenchpress", Category: CatCoffee, Name: "French Press", Duration: 4 * time.Minute, Note: "Full immersion · plunge"},

		// Short eye breaks
		{ID: "20-20-20", Category: CatEyes, Name: "20-20-20", Duration: 20 * time.Second, Note: "Focus 20 ft away"},
		{ID: "palm", Category: CatEyes, Name: "Palm Rest", Duration: 1 * time.Minute, Note: "Close eyes · soft breath"},
		{ID: "micro", Category: CatEyes, Name: "Micro Break", Duration: 2 * time.Minute, Note: "Blink · relax shoulders"},
		{ID: "reset", Category: CatEyes, Name: "Screen Reset", Duration: 5 * time.Minute, Note: "Step away · daylight"},
	}
}

// TimerStatus is the lifecycle state of the active countdown.
type TimerStatus int

const (
	StatusIdle TimerStatus = iota
	StatusRunning
	StatusPaused
	StatusDone
)

func (s TimerStatus) Label() string {
	switch s {
	case StatusRunning:
		return "Steeping"
	case StatusPaused:
		return "Paused"
	case StatusDone:
		return "Ready"
	default:
		return "Standby"
	}
}

func (s TimerStatus) StatusText(cat Category) string {
	if s == StatusRunning {
		switch cat {
		case CatTea:
			return "Steeping"
		case CatCoffee:
			return "Brewing"
		case CatEyes:
			return "Resting"
		}
	}
	if s == StatusDone {
		switch cat {
		case CatTea:
			return "Tea ready"
		case CatCoffee:
			return "Brew ready"
		case CatEyes:
			return "Eyes fresh"
		}
	}
	return s.Label()
}

// AppState holds the timer model and HUD preferences.
type AppState struct {
	Presets       []Preset
	ActiveCat     Category
	ActivePreset  int // index into Presets
	Total         time.Duration
	Remaining     time.Duration
	Status        TimerStatus
	LastTick      time.Time
	Muted         bool
	Opacity       float32 // card background opacity (0.55..0.98)
	ChimeFlash    float32 // 0..1 gentle glow pulse when chime fires
	Backdrop      bool    // draw a simulated desktop backdrop (useful for -png inspection)
	AudioDisabled bool    // true if OS audio device failed to open
}

func newAppState() *AppState {
	presets := DefaultPresets()
	st := &AppState{
		Presets:      presets,
		ActiveCat:    presets[0].Category,
		ActivePreset: 0,
		Total:        presets[0].Duration,
		Remaining:    presets[0].Duration,
		Status:       StatusIdle,
		Opacity:      0.90,
	}
	return st
}

func (st *AppState) currentPreset() Preset {
	if st.ActivePreset < 0 || st.ActivePreset >= len(st.Presets) {
		return st.Presets[0]
	}
	return st.Presets[st.ActivePreset]
}

// presetsForCategory returns indices into st.Presets for the given category.
func (st *AppState) presetsForCategory(cat Category) []int {
	var out []int
	for i, p := range st.Presets {
		if p.Category == cat {
			out = append(out, i)
		}
	}
	return out
}

// SelectPresetByID finds a preset by case-insensitive ID or name.
func (st *AppState) SelectPresetByID(id string) bool {
	want := strings.ToLower(strings.TrimSpace(id))
	for i, p := range st.Presets {
		if strings.ToLower(p.ID) == want || strings.ToLower(p.Name) == want {
			st.ActivePreset = i
			st.ActiveCat = p.Category
			st.Total = p.Duration
			st.Remaining = p.Duration
			st.Status = StatusIdle
			st.ChimeFlash = 0
			return true
		}
	}
	return false
}

// SelectCategory switches the visible preset tab. If the timer is idle,
// it also loads the first preset of that category without auto-starting.
func (st *AppState) SelectCategory(cat Category) {
	st.ActiveCat = cat
	if st.Status == StatusIdle || st.Status == StatusDone {
		indices := st.presetsForCategory(cat)
		if len(indices) > 0 {
			p := st.Presets[indices[0]]
			st.ActivePreset = indices[0]
			st.Total = p.Duration
			st.Remaining = p.Duration
			st.Status = StatusIdle
			st.ChimeFlash = 0
		}
	}
}

// StartPreset loads preset idx and immediately begins counting down.
func (st *AppState) StartPreset(idx int, now time.Time) {
	if idx < 0 || idx >= len(st.Presets) {
		return
	}
	p := st.Presets[idx]
	st.ActivePreset = idx
	st.ActiveCat = p.Category
	st.Total = p.Duration
	st.Remaining = p.Duration
	st.Status = StatusRunning
	st.LastTick = now
	st.ChimeFlash = 0
}

// ToggleStartPause starts, pauses, resumes, or restarts from Done.
func (st *AppState) ToggleStartPause(now time.Time) {
	switch st.Status {
	case StatusRunning:
		st.Status = StatusPaused
	case StatusPaused:
		st.Status = StatusRunning
		st.LastTick = now
	case StatusDone:
		st.Remaining = st.Total
		st.Status = StatusRunning
		st.LastTick = now
		st.ChimeFlash = 0
	default: // StatusIdle
		if st.Remaining <= 0 {
			st.Remaining = st.Total
		}
		st.Status = StatusRunning
		st.LastTick = now
		st.ChimeFlash = 0
	}
}

// Reset restores the current preset duration and puts the timer in Standby.
func (st *AppState) Reset() {
	p := st.currentPreset()
	st.Total = p.Duration
	st.Remaining = p.Duration
	st.Status = StatusIdle
	st.ChimeFlash = 0
}

// AdjustDuration nudges the timer by delta (clamped to [10s, 60m]).
func (st *AppState) AdjustDuration(delta time.Duration) {
	const minDur = 10 * time.Second
	const maxDur = 60 * time.Minute

	if st.Status == StatusIdle || st.Status == StatusDone {
		next := st.Total + delta
		if next < minDur {
			next = minDur
		}
		if next > maxDur {
			next = maxDur
		}
		st.Total = next
		st.Remaining = next
		if st.Status == StatusDone {
			st.Status = StatusIdle
			st.ChimeFlash = 0
		}
		return
	}

	nextRem := st.Remaining + delta
	if nextRem < 5*time.Second {
		nextRem = 5 * time.Second
	}
	if nextRem > maxDur {
		nextRem = maxDur
	}
	st.Remaining = nextRem
	if st.Remaining > st.Total {
		st.Total = st.Remaining
	}
}

// CycleOpacity steps through three pleasant translucent glass levels.
func (st *AppState) CycleOpacity() {
	switch {
	case st.Opacity > 0.84:
		st.Opacity = 0.78
	case st.Opacity > 0.70:
		st.Opacity = 0.64
	default:
		st.Opacity = 0.90
	}
}

// Tick advances the countdown and gentle chime flash. Returns true on the
// exact transition into StatusDone so the caller can ring the chime once.
func (st *AppState) Tick(now time.Time) (completed bool, needsFrame bool) {
	if st.ChimeFlash > 0 {
		dt := float32(now.Sub(st.LastTick).Seconds())
		if dt <= 0 || dt > 0.5 {
			dt = 0.1
		}
		st.ChimeFlash -= dt * 0.45
		if st.ChimeFlash < 0 {
			st.ChimeFlash = 0
		}
		needsFrame = true
	}

	if st.Status != StatusRunning {
		st.LastTick = now
		return false, needsFrame
	}

	elapsed := now.Sub(st.LastTick)
	st.LastTick = now
	if elapsed <= 0 {
		return false, true
	}

	st.Remaining -= elapsed
	if st.Remaining <= 0 {
		st.Remaining = 0
		st.Status = StatusDone
		st.ChimeFlash = 1.0
		return true, true
	}
	return false, true
}

// ElapsedFraction returns progress from 0 (fresh) to 1 (finished).
func (st *AppState) ElapsedFraction() float32 {
	if st.Status == StatusDone {
		return 1
	}
	if st.Total <= 0 {
		return 0
	}
	f := 1 - float32(st.Remaining)/float32(st.Total)
	if f < 0 {
		return 0
	}
	if f > 1 {
		return 1
	}
	return f
}

// FormatClock formats d as MM:SS, rounding up partial seconds while running
// so 00:01 does not prematurely show 00:00.
func FormatClock(d time.Duration) string {
	if d <= 0 {
		return "00:00"
	}
	secs := int((d + 999*time.Millisecond) / time.Second)
	m := secs / 60
	s := secs % 60
	return fmt.Sprintf("%02d:%02d", m, s)
}

// FormatShortDuration formats a preset duration concisely (e.g. "2:00", "0:45", "0:20").
func FormatShortDuration(d time.Duration) string {
	secs := int(d / time.Second)
	m := secs / 60
	s := secs % 60
	return fmt.Sprintf("%d:%02d", m, s)
}
