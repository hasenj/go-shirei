package main

import (
	"fmt"
	"math"
	"strings"
	"time"

	. "go.hasen.dev/shirei"
)

// City represents a selectable world city and its IANA timezone.
type City struct {
	ID      string
	Name    string
	Region  string
	TZName  string
	Default bool

	Loc *time.Location
}

// Phase describes the rough human schedule phase for a local time of day.
type Phase uint8

const (
	PhaseNight Phase = iota
	PhaseMorning
	PhaseWork
	PhaseEvening
)

func (p Phase) Label() string {
	switch p {
	case PhaseWork:
		return "Work"
	case PhaseMorning:
		return "Morning"
	case PhaseEvening:
		return "Evening"
	default:
		return "Night"
	}
}

// OverlapWindow describes a contiguous span of minutes in the reference day
// where the active cities share a strong overlap score.
type OverlapWindow struct {
	StartMin int // inclusive, 0..1440
	EndMin   int // exclusive, 0..1440
	Working  int
	Awake    int
	Total    int
	Score    int
}

func (w OverlapWindow) MidMin() float32 {
	return float32(w.StartMin+w.EndMin) / 2.0
}

// CityCatalog returns all built-in cities with loaded time.Location values.
func CityCatalog() []*City {
	raw := []City{
		{ID: "sf", Name: "San Francisco", Region: "US · Pacific", TZName: "America/Los_Angeles", Default: true},
		{ID: "nyc", Name: "New York", Region: "US · Eastern", TZName: "America/New_York", Default: true},
		{ID: "lon", Name: "London", Region: "United Kingdom", TZName: "Europe/London", Default: true},
		{ID: "ber", Name: "Berlin", Region: "Germany", TZName: "Europe/Berlin", Default: true},
		{ID: "blr", Name: "Bengaluru", Region: "India", TZName: "Asia/Kolkata", Default: true},
		{ID: "tyo", Name: "Tokyo", Region: "Japan", TZName: "Asia/Tokyo", Default: true},

		{ID: "utc", Name: "UTC", Region: "Coordinated Universal", TZName: "UTC"},
		{ID: "hnl", Name: "Honolulu", Region: "US · Hawaii", TZName: "Pacific/Honolulu"},
		{ID: "anc", Name: "Anchorage", Region: "US · Alaska", TZName: "America/Anchorage"},
		{ID: "van", Name: "Vancouver", Region: "Canada", TZName: "America/Vancouver"},
		{ID: "sea", Name: "Seattle", Region: "US · Pacific", TZName: "America/Los_Angeles"},
		{ID: "den", Name: "Denver", Region: "US · Mountain", TZName: "America/Denver"},
		{ID: "chi", Name: "Chicago", Region: "US · Central", TZName: "America/Chicago"},
		{ID: "mex", Name: "Mexico City", Region: "Mexico", TZName: "America/Mexico_City"},
		{ID: "tor", Name: "Toronto", Region: "Canada", TZName: "America/Toronto"},
		{ID: "bog", Name: "Bogota", Region: "Colombia", TZName: "America/Bogota"},
		{ID: "sao", Name: "Sao Paulo", Region: "Brazil", TZName: "America/Sao_Paulo"},
		{ID: "bue", Name: "Buenos Aires", Region: "Argentina", TZName: "America/Argentina/Buenos_Aires"},
		{ID: "rey", Name: "Reykjavik", Region: "Iceland", TZName: "Atlantic/Reykjavik"},
		{ID: "lis", Name: "Lisbon", Region: "Portugal", TZName: "Europe/Lisbon"},
		{ID: "dub", Name: "Dublin", Region: "Ireland", TZName: "Europe/Dublin"},
		{ID: "par", Name: "Paris", Region: "France", TZName: "Europe/Paris"},
		{ID: "ams", Name: "Amsterdam", Region: "Netherlands", TZName: "Europe/Amsterdam"},
		{ID: "zur", Name: "Zurich", Region: "Switzerland", TZName: "Europe/Zurich"},
		{ID: "sto", Name: "Stockholm", Region: "Sweden", TZName: "Europe/Stockholm"},
		{ID: "war", Name: "Warsaw", Region: "Poland", TZName: "Europe/Warsaw"},
		{ID: "cai", Name: "Cairo", Region: "Egypt", TZName: "Africa/Cairo"},
		{ID: "jnb", Name: "Johannesburg", Region: "South Africa", TZName: "Africa/Johannesburg"},
		{ID: "ist", Name: "Istanbul", Region: "Turkey", TZName: "Europe/Istanbul"},
		{ID: "riy", Name: "Riyadh", Region: "Saudi Arabia", TZName: "Asia/Riyadh"},
		{ID: "dxb", Name: "Dubai", Region: "UAE", TZName: "Asia/Dubai"},
		{ID: "khi", Name: "Karachi", Region: "Pakistan", TZName: "Asia/Karachi"},
		{ID: "ktm", Name: "Kathmandu", Region: "Nepal", TZName: "Asia/Kathmandu"},
		{ID: "bkk", Name: "Bangkok", Region: "Thailand", TZName: "Asia/Bangkok"},
		{ID: "sin", Name: "Singapore", Region: "Singapore", TZName: "Asia/Singapore"},
		{ID: "hkg", Name: "Hong Kong", Region: "China", TZName: "Asia/Hong_Kong"},
		{ID: "tpe", Name: "Taipei", Region: "Taiwan", TZName: "Asia/Taipei"},
		{ID: "sel", Name: "Seoul", Region: "South Korea", TZName: "Asia/Seoul"},
		{ID: "syd", Name: "Sydney", Region: "Australia", TZName: "Australia/Sydney"},
		{ID: "mel", Name: "Melbourne", Region: "Australia", TZName: "Australia/Melbourne"},
		{ID: "akl", Name: "Auckland", Region: "New Zealand", TZName: "Pacific/Auckland"},
	}

	out := make([]*City, 0, len(raw))
	for i := range raw {
		c := raw[i]
		loc, err := time.LoadLocation(c.TZName)
		if err != nil {
			loc = time.UTC
		}
		c.Loc = loc
		out = append(out, &c)
	}
	return out
}

// LocalPhase classifies a local time into Night, Morning, Work (9-17), or Evening.
func LocalPhase(t time.Time) Phase {
	h := float64(t.Hour()) + float64(t.Minute())/60.0
	switch {
	case h >= 9.0 && h < 17.0:
		return PhaseWork
	case h >= 7.0 && h < 9.0:
		return PhaseMorning
	case h >= 17.0 && h < 22.0:
		return PhaseEvening
	default:
		return PhaseNight
	}
}

// IsWorkHour reports whether local time t is inside core 09:00-17:00 work hours.
func IsWorkHour(t time.Time) bool {
	return LocalPhase(t) == PhaseWork
}

// DayOffset returns the calendar day difference of local relative to base.
func DayOffset(local, base time.Time) int {
	ly, lm, ld := local.Date()
	by, bm, bd := base.Date()
	lDay := time.Date(ly, lm, ld, 0, 0, 0, 0, time.UTC)
	bDay := time.Date(by, bm, bd, 0, 0, 0, 0, time.UTC)
	return int(math.Round(lDay.Sub(bDay).Hours() / 24.0))
}

// FormatTime formats a clock time in 24h ("09:30") or 12h ("9:30 AM") format.
func FormatTime(t time.Time, use24h bool) string {
	if use24h {
		return t.Format("15:04")
	}
	return t.Format("3:04 PM")
}

// FormatMinutes formats minutes-of-day [0..1440] in 24h or 12h format.
func FormatMinutes(totalMin int, use24h bool) string {
	if totalMin < 0 {
		totalMin = 0
	}
	if totalMin >= 1440 {
		if use24h {
			return "24:00"
		}
		return "12:00 AM"
	}
	h := totalMin / 60
	m := totalMin % 60
	if use24h {
		return fmt.Sprintf("%02d:%02d", h, m)
	}
	suffix := "AM"
	h12 := h
	if h >= 12 {
		suffix = "PM"
	}
	if h12 == 0 {
		h12 = 12
	} else if h12 > 12 {
		h12 -= 12
	}
	return fmt.Sprintf("%d:%02d %s", h12, m, suffix)
}

// FormatHourTick formats a compact hour label for rulers and bars.
func FormatHourTick(hour int, use24h bool) string {
	h := ((hour % 24) + 24) % 24
	if use24h {
		if hour == 24 {
			return "24"
		}
		return fmt.Sprintf("%02d", h)
	}
	if h == 0 {
		return "12a"
	}
	if h == 12 {
		return "12p"
	}
	if h < 12 {
		return fmt.Sprintf("%da", h)
	}
	return fmt.Sprintf("%dp", h-12)
}

// FormatUTCOffset returns strings like "UTC-8", "UTC+5:30", or "UTC".
func FormatUTCOffset(t time.Time) string {
	_, sec := t.Zone()
	if sec == 0 {
		return "UTC"
	}
	sign := "+"
	if sec < 0 {
		sign = "-"
		sec = -sec
	}
	h := sec / 3600
	m := (sec % 3600) / 60
	if m == 0 {
		return fmt.Sprintf("UTC%s%d", sign, h)
	}
	return fmt.Sprintf("UTC%s%d:%02d", sign, h, m)
}

// FormatRelOffset returns the hour difference of city relative to base, e.g. "+9h", "-3h", "+13.5h".
func FormatRelOffset(local, base time.Time) string {
	_, lSec := local.Zone()
	_, bSec := base.Zone()
	diffMin := (lSec - bSec) / 60
	if diffMin == 0 {
		return "Home"
	}
	sign := "+"
	if diffMin < 0 {
		sign = "-"
		diffMin = -diffMin
	}
	h := diffMin / 60
	m := diffMin % 60
	if m == 0 {
		return fmt.Sprintf("%s%dh", sign, h)
	}
	if m == 30 {
		return fmt.Sprintf("%s%d.5h", sign, h)
	}
	return fmt.Sprintf("%s%dh%02dm", sign, h, m)
}

// SolarColor maps a local hour [0..24) to an HSLA color representing
// night, dawn, daylight/workday, golden hour, and dusk.
func SolarColor(localHour float64) Vec4 {
	h := math.Mod(localHour, 24.0)
	if h < 0 {
		h += 24.0
	}

	type stop struct {
		hr      float64
		H, S, L float64
	}
	// Carefully chosen solar gradient stops that avoid muddy green transitions
	// between amber dawn/dusk and clear daytime sky blue.
	stops := []stop{
		{0.0, 232, 46, 16},  // midnight deep indigo
		{4.5, 230, 44, 19},  // late night navy
		{5.5, 236, 38, 28},  // pre-dawn twilight
		{6.25, 268, 34, 42}, // first light violet
		{6.8, 16, 72, 62},   // sunrise coral
		{7.5, 36, 82, 68},   // golden morning
		{8.3, 44, 62, 79},   // warm bright horizon
		{9.0, 202, 68, 75},  // start of workday sky
		{12.5, 199, 76, 68}, // bright solar noon azure
		{16.5, 204, 66, 74}, // clear afternoon sky
		{17.3, 40, 74, 73},  // late afternoon honey
		{18.2, 24, 78, 61},  // golden hour amber
		{19.0, 8, 64, 52},   // sunset vermilion-coral
		{19.8, 276, 36, 37}, // dusk plum-violet
		{20.8, 238, 42, 25}, // evening twilight
		{22.0, 232, 46, 18}, // night indigo
		{24.0, 232, 46, 16}, // wrap to midnight
	}

	// Find bracketing stops.
	for i := 0; i < len(stops)-1; i++ {
		a, b := stops[i], stops[i+1]
		if h >= a.hr && h <= b.hr {
			t := (h - a.hr) / (b.hr - a.hr)
			// At the warm-horizon <-> sky-blue boundary, keep chroma clean by
			// blending via lightness/saturation smoothly rather than sweeping hue
			// across green.
			hue := lerpHueClean(a.H, b.H, t)
			sat := a.S + (b.S-a.S)*t
			light := a.L + (b.L-a.L)*t
			// Desaturate slightly at the warm-to-blue horizon seam.
			if (a.H < 60 && b.H > 180) || (a.H > 180 && b.H < 60) {
				seam := 1.0 - math.Abs(2*t-1) // peaks at 1.0 when t=0.5
				sat *= (1.0 - 0.45*seam)
				light += 5.0 * seam
				if t < 0.5 {
					hue = a.H
				} else {
					hue = b.H
				}
			}
			return Vec4{float32(hue), float32(sat), float32(light), 1}
		}
	}
	return Vec4{232, 46, 16, 1}
}

func lerpHueClean(a, b, t float64) float64 {
	diff := b - a
	if diff > 180 {
		diff -= 360
	} else if diff < -180 {
		diff += 360
	}
	res := a + diff*t
	if res < 0 {
		res += 360
	} else if res >= 360 {
		res -= 360
	}
	return res
}

// SlotStats summarizes how many cities are working, awake, or sleeping at an instant.
type SlotStats struct {
	Working  int
	Awake    int // includes Working
	Sleeping int
	Total    int
	Score    int
}

// EvaluateInstant counts working/awake/sleeping cities at a specific instant.
func EvaluateInstant(cities []*City, instant time.Time) SlotStats {
	var st SlotStats
	st.Total = len(cities)
	for _, c := range cities {
		lt := instant.In(c.Loc)
		switch LocalPhase(lt) {
		case PhaseWork:
			st.Working++
			st.Awake++
			st.Score += 3
		case PhaseMorning, PhaseEvening:
			st.Awake++
			st.Score += 1
		default:
			st.Sleeping++
		}
	}
	return st
}

// ComputeOverlapScores returns per-slot (48 half-hour slots) stats across the base day,
// plus the best contiguous overlap windows (where score is near the daily maximum).
func ComputeOverlapScores(cities []*City, baseMidnight time.Time) ([]SlotStats, []OverlapWindow) {
	const slots = 48
	stats := make([]SlotStats, slots)
	if len(cities) == 0 {
		return stats, nil
	}

	maxScore := -1
	for i := 0; i < slots; i++ {
		// Sample middle of each 30-minute slot.
		inst := baseMidnight.Add(time.Duration(i*30+15) * time.Minute)
		s := EvaluateInstant(cities, inst)
		stats[i] = s
		if s.Score > maxScore {
			maxScore = s.Score
		}
	}

	// Collect contiguous windows achieving the daily peak score (or within 1 point if peak >= 4).
	threshold := maxScore
	if threshold <= 0 {
		return stats, nil
	}

	var windows []OverlapWindow
	for i := 0; i < slots; {
		if stats[i].Score < threshold {
			i++
			continue
		}
		j := i + 1
		bestW, bestA, bestS := stats[i].Working, stats[i].Awake, stats[i].Score
		for j < slots && stats[j].Score >= threshold {
			if stats[j].Working > bestW {
				bestW = stats[j].Working
			}
			if stats[j].Awake > bestA {
				bestA = stats[j].Awake
			}
			if stats[j].Score > bestS {
				bestS = stats[j].Score
			}
			j++
		}
		windows = append(windows, OverlapWindow{
			StartMin: i * 30,
			EndMin:   j * 30,
			Working:  bestW,
			Awake:    bestA,
			Total:    len(cities),
			Score:    bestS,
		})
		i = j
	}
	return stats, windows
}

// MatchCity checks if a city matches a case-insensitive search query.
func MatchCity(c *City, query string) bool {
	q := strings.TrimSpace(strings.ToLower(query))
	if q == "" {
		return true
	}
	if strings.Contains(strings.ToLower(c.Name), q) {
		return true
	}
	if strings.Contains(strings.ToLower(c.Region), q) {
		return true
	}
	if strings.Contains(strings.ToLower(c.TZName), q) {
		return true
	}
	return false
}
