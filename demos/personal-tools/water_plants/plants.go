package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// PlantKind picks a botanical icon and color accent for a houseplant.
type PlantKind string

const (
	KindTropical  PlantKind = "tropical"
	KindFern      PlantKind = "fern"
	KindSucculent PlantKind = "succulent"
	KindTrailing  PlantKind = "trailing"
	KindTree      PlantKind = "tree"
	KindFlowering PlantKind = "flowering"
)

var AllPlantKinds = []PlantKind{
	KindTropical,
	KindFern,
	KindTrailing,
	KindTree,
	KindFlowering,
	KindSucculent,
}

func (k PlantKind) Label() string {
	switch k {
	case KindFern:
		return "Fern"
	case KindSucculent:
		return "Succulent"
	case KindTrailing:
		return "Trailing"
	case KindTree:
		return "Tree"
	case KindFlowering:
		return "Flowering"
	default:
		return "Tropical"
	}
}

// LightLevel describes the plant's preferred light exposure.
type LightLevel string

const (
	LightLow    LightLevel = "Low"
	LightMedium LightLevel = "Medium"
	LightBright LightLevel = "Bright"
)

var AllLightLevels = []LightLevel{LightLow, LightMedium, LightBright}

// Plant is one tracked houseplant.
type Plant struct {
	ID              string     `json:"id"`
	Name            string     `json:"name"`
	Location        string     `json:"location"`
	Kind            PlantKind  `json:"kind"`
	Light           LightLevel `json:"light"`
	IntervalDays    int        `json:"interval_days"`
	LastWatered     string     `json:"last_watered"`      // YYYY-MM-DD (local calendar date)
	PrevLastWatered string     `json:"prev_last_watered"` // previous date for quick undo
	WaterCount      int        `json:"water_count"`
	Notes           string     `json:"notes,omitempty"`
}

// Urgency buckets a plant's current watering state relative to today.
type Urgency int

const (
	UrgencyOverdue Urgency = iota
	UrgencyToday
	UrgencySoon
	UrgencyHappy
	UrgencyWateredToday
)

// PlantStatus is the derived schedule state for a plant on a reference calendar day.
type PlantStatus struct {
	DaysSinceWatered int
	DaysUntilDue     int // < 0 overdue, 0 due today, > 0 days remaining
	NextDueDate      time.Time
	LastWateredDate  time.Time
	Urgency          Urgency
	WateredToday     bool
	MoistureFrac     float32 // 1.0 freshly watered -> 0.0 due/overdue
}

// FilterTab selects which subset of plants is shown in the list.
type FilterTab int

const (
	FilterAll FilterTab = iota
	FilterThirsty
	FilterSoon
	FilterHappy
)

func (f FilterTab) Label() string {
	switch f {
	case FilterThirsty:
		return "Thirsty"
	case FilterSoon:
		return "Soon"
	case FilterHappy:
		return "Happy"
	default:
		return "All"
	}
}

func ParseFilterTab(s string) (FilterTab, bool) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "all":
		return FilterAll, true
	case "thirsty", "due", "overdue":
		return FilterThirsty, true
	case "soon", "upcoming":
		return FilterSoon, true
	case "happy", "ok", "watered":
		return FilterHappy, true
	default:
		return FilterAll, false
	}
}

const dateLayout = "2006-01-02"

// CalendarDay strips time-of-day, keeping the local calendar date at midnight UTC
// for clean integer-day subtraction without DST drift.
func CalendarDay(t time.Time) time.Time {
	y, m, d := t.Date()
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

func FormatISODate(day time.Time) string {
	return day.Format(dateLayout)
}

func ParseISODate(s string) (time.Time, bool) {
	t, err := time.Parse(dateLayout, strings.TrimSpace(s))
	if err != nil {
		return time.Time{}, false
	}
	return CalendarDay(t), true
}

// Status computes a plant's watering status for the given calendar day.
func (p *Plant) Status(today time.Time) PlantStatus {
	day := CalendarDay(today)
	interval := p.IntervalDays
	if interval < 1 {
		interval = 7
	}
	last, ok := ParseISODate(p.LastWatered)
	if !ok {
		// Unset date: treat as due today so the user sees it immediately.
		last = day.AddDate(0, 0, -interval)
	}
	daysSince := int(day.Sub(last).Hours() / 24)
	nextDue := last.AddDate(0, 0, interval)
	daysUntil := int(nextDue.Sub(day).Hours() / 24)

	wateredToday := daysSince <= 0

	var urg Urgency
	switch {
	case wateredToday:
		urg = UrgencyWateredToday
	case daysUntil < 0:
		urg = UrgencyOverdue
	case daysUntil == 0:
		urg = UrgencyToday
	case daysUntil <= 3:
		urg = UrgencySoon
	default:
		urg = UrgencyHappy
	}

	var moisture float32
	if wateredToday {
		moisture = 1.0
	} else if daysUntil <= 0 {
		moisture = 0.0
	} else {
		moisture = float32(daysUntil) / float32(interval)
		if moisture < 0 {
			moisture = 0
		}
		if moisture > 1 {
			moisture = 1
		}
	}

	return PlantStatus{
		DaysSinceWatered: daysSince,
		DaysUntilDue:     daysUntil,
		NextDueDate:      nextDue,
		LastWateredDate:  last,
		Urgency:          urg,
		WateredToday:     wateredToday,
		MoistureFrac:     moisture,
	}
}

// DueHeadline returns the crisp badge text describing when the plant needs water next.
func (st PlantStatus) DueHeadline() string {
	if st.WateredToday {
		return "Watered today"
	}
	switch {
	case st.DaysUntilDue < -1:
		return fmt.Sprintf("%dd overdue", -st.DaysUntilDue)
	case st.DaysUntilDue == -1:
		return "1d overdue"
	case st.DaysUntilDue == 0:
		return "Water today"
	case st.DaysUntilDue == 1:
		return "Tomorrow"
	default:
		return fmt.Sprintf("In %d days · %s", st.DaysUntilDue, st.NextDueDate.Format("Mon"))
	}
}

// LastWateredCaption describes how long ago the plant was watered.
func (st PlantStatus) LastWateredCaption() string {
	switch {
	case st.DaysSinceWatered <= 0:
		return "Watered today"
	case st.DaysSinceWatered == 1:
		return "Last: yesterday"
	default:
		return fmt.Sprintf("Last: %dd ago", st.DaysSinceWatered)
	}
}

// NextDateCaption returns a short due or next-watering date string.
func (st PlantStatus) NextDateCaption() string {
	if st.WateredToday {
		return "Next: " + st.NextDueDate.Format("Mon, Jan 2")
	}
	if st.DaysUntilDue < 0 {
		return "Due: " + st.NextDueDate.Format("Mon, Jan 2")
	}
	if st.DaysUntilDue == 0 {
		return "Due: Today"
	}
	return "Next: " + st.NextDueDate.Format("Mon, Jan 2")
}

// MatchesFilter returns whether a plant status belongs in the selected filter tab.
func (st PlantStatus) MatchesFilter(tab FilterTab) bool {
	switch tab {
	case FilterThirsty:
		return st.Urgency == UrgencyOverdue || st.Urgency == UrgencyToday
	case FilterSoon:
		return st.Urgency == UrgencySoon
	case FilterHappy:
		return st.Urgency == UrgencyHappy || st.Urgency == UrgencyWateredToday
	default:
		return true
	}
}

// SummaryCounts aggregates plant counts across urgency buckets.
type SummaryCounts struct {
	Total        int
	Overdue      int
	DueToday     int
	Thirsty      int
	Soon         int
	Happy        int
	WateredToday int
}

func ComputeSummary(plants []*Plant, today time.Time) SummaryCounts {
	var c SummaryCounts
	c.Total = len(plants)
	for _, p := range plants {
		st := p.Status(today)
		switch st.Urgency {
		case UrgencyOverdue:
			c.Overdue++
			c.Thirsty++
		case UrgencyToday:
			c.DueToday++
			c.Thirsty++
		case UrgencySoon:
			c.Soon++
		case UrgencyHappy:
			c.Happy++
		case UrgencyWateredToday:
			c.WateredToday++
			c.Happy++
		}
	}
	return c
}

// SortedPlants returns a new slice ordered by urgency:
// overdue/due-today first (most overdue at top), then upcoming soonest,
// then happy, then freshly watered today at the bottom.
func SortedPlants(plants []*Plant, today time.Time, filter FilterTab) []*Plant {
	type entry struct {
		p  *Plant
		st PlantStatus
	}
	var list []entry
	for _, p := range plants {
		st := p.Status(today)
		if !st.MatchesFilter(filter) {
			continue
		}
		list = append(list, entry{p: p, st: st})
	}
	sort.SliceStable(list, func(i, j int) bool {
		a, b := list[i], list[j]
		// Keep plants watered today neatly tucked after active upcoming items when viewing All.
		if a.st.WateredToday != b.st.WateredToday {
			return !a.st.WateredToday
		}
		if a.st.DaysUntilDue != b.st.DaysUntilDue {
			return a.st.DaysUntilDue < b.st.DaysUntilDue
		}
		return strings.ToLower(a.p.Name) < strings.ToLower(b.p.Name)
	})
	out := make([]*Plant, len(list))
	for i, e := range list {
		out[i] = e.p
	}
	return out
}

// SamplePlants returns a realistic starter houseplant collection anchored to today.
func SamplePlants(today time.Time) []*Plant {
	day := CalendarDay(today)
	daysAgo := func(n int) string {
		return FormatISODate(day.AddDate(0, 0, -n))
	}
	return []*Plant{
		{
			ID:           "boston-fern",
			Name:         "Boston Fern",
			Location:     "Bathroom",
			Kind:         KindFern,
			Light:        LightMedium,
			IntervalDays: 3,
			LastWatered:  daysAgo(5), // 2d overdue
			WaterCount:   19,
			Notes:        "Keep soil evenly damp; mist fronds when air feels dry.",
		},
		{
			ID:           "calathea-orbifolia",
			Name:         "Calathea Orbifolia",
			Location:     "Bedroom",
			Kind:         KindTropical,
			Light:        LightMedium,
			IntervalDays: 5,
			LastWatered:  daysAgo(6), // 1d overdue
			WaterCount:   12,
			Notes:        "Prefers filtered water; avoid harsh midday sun.",
		},
		{
			ID:           "monstera-deliciosa",
			Name:         "Monstera Deliciosa",
			Location:     "Living Room",
			Kind:         KindTropical,
			Light:        LightBright,
			IntervalDays: 7,
			LastWatered:  daysAgo(7), // due today
			WaterCount:   15,
			Notes:        "Water thoroughly until it drains; wipe dusty leaves monthly.",
		},
		{
			ID:           "golden-pothos",
			Name:         "Golden Pothos",
			Location:     "Desk Shelf",
			Kind:         KindTrailing,
			Light:        LightLow,
			IntervalDays: 7,
			LastWatered:  daysAgo(6), // tomorrow
			WaterCount:   22,
			Notes:        "Let top inch dry out between waterings.",
		},
		{
			ID:           "fiddle-leaf-fig",
			Name:         "Fiddle Leaf Fig",
			Location:     "Window Bay",
			Kind:         KindTree,
			Light:        LightBright,
			IntervalDays: 8,
			LastWatered:  daysAgo(5), // in 3 days
			WaterCount:   9,
			Notes:        "Rotate pot a quarter turn each week for even growth.",
		},
		{
			ID:              "peace-lily",
			Name:            "Peace Lily",
			Location:        "Hallway",
			Kind:            KindFlowering,
			Light:           LightLow,
			IntervalDays:    5,
			LastWatered:     daysAgo(0), // watered today
			PrevLastWatered: daysAgo(5),
			WaterCount:      14,
			Notes:           "Leaves droop slightly as an early thirst cue.",
		},
		{
			ID:           "zz-plant",
			Name:         "ZZ Plant",
			Location:     "Study",
			Kind:         KindSucculent,
			Light:        LightLow,
			IntervalDays: 14,
			LastWatered:  daysAgo(5), // in 9 days
			WaterCount:   6,
			Notes:        "Drought tolerant; err on the side of underwatering.",
		},
		{
			ID:           "snake-plant",
			Name:         "Snake Plant",
			Location:     "Entryway",
			Kind:         KindSucculent,
			Light:        LightMedium,
			IntervalDays: 14,
			LastWatered:  daysAgo(3), // in 11 days
			WaterCount:   7,
			Notes:        "Wait until soil is completely dry through the pot.",
		},
	}
}

type persistedFile struct {
	Version int      `json:"version"`
	Plants  []*Plant `json:"plants"`
}

// DefaultDataPath returns the default per-user JSON storage location.
func DefaultDataPath() string {
	if env := strings.TrimSpace(os.Getenv("WATER_PLANTS_DATA")); env != "" {
		return env
	}
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "water_plants", "plants.json")
}

// LoadPlants loads persisted plants from path. Returns (nil, os.ErrNotExist) when absent.
func LoadPlants(path string) ([]*Plant, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var pf persistedFile
	if err := json.Unmarshal(raw, &pf); err != nil {
		return nil, err
	}
	if pf.Plants == nil {
		return []*Plant{}, nil
	}
	for _, p := range pf.Plants {
		if p.IntervalDays < 1 {
			p.IntervalDays = 7
		}
		if p.Kind == "" {
			p.Kind = KindTropical
		}
		if p.Light == "" {
			p.Light = LightMedium
		}
	}
	return pf.Plants, nil
}

// SavePlants writes plants atomically to path.
func SavePlants(path string, plants []*Plant) error {
	if path == "" {
		return errors.New("empty data path")
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	raw, err := json.MarshalIndent(persistedFile{
		Version: 1,
		Plants:  plants,
	}, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// slugifyID turns a plant name into a clean access-name-safe lowercase id.
func slugifyID(name string, existing []*Plant) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevDash = false
		} else if !prevDash && b.Len() > 0 {
			b.WriteByte('-')
			prevDash = true
		}
	}
	base := strings.Trim(b.String(), "-")
	if base == "" {
		base = "plant"
	}
	taken := func(cand string) bool {
		for _, p := range existing {
			if p.ID == cand {
				return true
			}
		}
		return false
	}
	if !taken(base) {
		return base
	}
	for i := 2; ; i++ {
		cand := fmt.Sprintf("%s-%d", base, i)
		if !taken(cand) {
			return cand
		}
	}
}
