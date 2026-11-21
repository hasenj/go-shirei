package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"go.hasen.dev/generic"
	"go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

const (
	winWidth  = 1120
	winHeight = 700
)

// AppState is the durable application model for Sunbelt.
type AppState struct {
	Catalog []*City
	Cities  []*City
	BaseID  string

	// Reference calendar date (year, month, day) interpreted in the base city's location.
	Year  int
	Month time.Month
	Day   int

	// ScrubMin is the scrubbed offset in minutes from 00:00 of the reference date
	// in the base city (range 0..1440).
	ScrubMin float32

	// Now is the real (or pinned) current instant used for the subtle NOW marker.
	Now time.Time

	Use24h   bool
	SnapStep int // 15, 30, or 60 minutes
	DarkMode bool

	ShowAddCity bool
	CitySearch  string
}

var state *AppState

func newAppState(now time.Time) *AppState {
	catalog := CityCatalog()
	var active []*City
	for _, c := range catalog {
		if c.Default {
			active = append(active, c)
		}
	}
	baseID := ""
	if len(active) > 0 {
		baseID = active[0].ID
	}
	st := &AppState{
		Catalog:  catalog,
		Cities:   active,
		BaseID:   baseID,
		Now:      now,
		Use24h:   true,
		SnapStep: 15,
	}
	st.jumpToInstant(now)
	return st
}

func (st *AppState) baseCity() *City {
	for _, c := range st.Cities {
		if c.ID == st.BaseID {
			return c
		}
	}
	if len(st.Cities) > 0 {
		st.BaseID = st.Cities[0].ID
		return st.Cities[0]
	}
	return st.Catalog[0]
}

func (st *AppState) baseMidnight() time.Time {
	base := st.baseCity()
	return time.Date(st.Year, st.Month, st.Day, 0, 0, 0, 0, base.Loc)
}

func (st *AppState) scrubbedInstant() time.Time {
	min := int(st.ScrubMin + 0.5)
	if min < 0 {
		min = 0
	}
	if min > 1440 {
		min = 1440
	}
	return st.baseMidnight().Add(time.Duration(min) * time.Minute)
}

func (st *AppState) jumpToInstant(inst time.Time) {
	base := st.baseCity()
	local := inst.In(base.Loc)
	st.Year, st.Month, st.Day = local.Date()
	mins := local.Hour()*60 + local.Minute()
	// Snap cleanly to current snap step.
	step := st.SnapStep
	if step <= 0 {
		step = 15
	}
	snapped := ((mins + step/2) / step) * step
	if snapped > 1440 {
		snapped = 1440
	}
	st.ScrubMin = float32(snapped)
}

func (st *AppState) setBaseCity(id string) {
	if st.BaseID == id {
		return
	}
	// Preserve the exact UTC instant the user is currently inspecting.
	inst := st.scrubbedInstant()
	st.BaseID = id
	base := st.baseCity()
	local := inst.In(base.Loc)
	st.Year, st.Month, st.Day = local.Date()
	st.ScrubMin = float32(local.Hour()*60 + local.Minute())
}

func (st *AppState) shiftDay(delta int) {
	base := st.baseCity()
	d := time.Date(st.Year, st.Month, st.Day, 12, 0, 0, 0, base.Loc).AddDate(0, 0, delta)
	st.Year, st.Month, st.Day = d.Date()
}

func (st *AppState) hasCity(id string) bool {
	for _, c := range st.Cities {
		if c.ID == id {
			return true
		}
	}
	return false
}

func (st *AppState) addCity(c *City) {
	if st.hasCity(c.ID) {
		return
	}
	st.Cities = append(st.Cities, c)
	if st.BaseID == "" {
		st.BaseID = c.ID
	}
}

func (st *AppState) removeCity(id string) {
	if len(st.Cities) <= 1 {
		return
	}
	for i, c := range st.Cities {
		if c.ID == id {
			st.Cities = append(st.Cities[:i], st.Cities[i+1:]...)
			if st.BaseID == id && len(st.Cities) > 0 {
				st.BaseID = st.Cities[0].ID
			}
			return
		}
	}
}

func (st *AppState) moveCity(index, delta int) {
	target := index + delta
	if index < 0 || index >= len(st.Cities) || target < 0 || target >= len(st.Cities) {
		return
	}
	st.Cities[index], st.Cities[target] = st.Cities[target], st.Cities[index]
}

// nowOffsetMinutes returns how many minutes into the currently displayed base day
// st.Now falls, along with whether st.Now is inside [0, 1440].
func (st *AppState) nowOffsetMinutes() (float32, bool) {
	diff := st.Now.Sub(st.baseMidnight()).Minutes()
	if diff < 0 || diff > 1440 {
		return 0, false
	}
	return float32(diff), true
}

func parseTimeFlag(s string) (int, error) {
	parts := strings.Split(strings.TrimSpace(s), ":")
	if len(parts) != 2 {
		return 0, fmt.Errorf("invalid -time %q: expected HH:MM", s)
	}
	var h, m int
	if _, err := fmt.Sscanf(parts[0], "%d", &h); err != nil {
		return 0, fmt.Errorf("invalid hour in -time %q", s)
	}
	if _, err := fmt.Sscanf(parts[1], "%d", &m); err != nil {
		return 0, fmt.Errorf("invalid minute in -time %q", s)
	}
	if h < 0 || h > 24 || m < 0 || m > 59 || (h == 24 && m > 0) {
		return 0, fmt.Errorf("-time %q out of range 00:00..24:00", s)
	}
	return h*60 + m, nil
}

func printTextReport(st *AppState) {
	base := st.baseCity()
	baseMid := st.baseMidnight()
	inst := st.scrubbedInstant()
	baseLocal := inst.In(base.Loc)
	stats := EvaluateInstant(st.Cities, inst)
	_, windows := ComputeOverlapScores(st.Cities, baseMid)

	fmt.Printf("Sunbelt — %s (%s %s)\n", baseLocal.Format("Mon, Jan 2, 2006"), FormatTime(baseLocal, st.Use24h), FormatUTCOffset(baseLocal))
	fmt.Printf("Overlap: %d/%d working, %d/%d awake, %d sleeping\n\n", stats.Working, stats.Total, stats.Awake, stats.Total, stats.Sleeping)

	for _, c := range st.Cities {
		lt := inst.In(c.Loc)
		off := DayOffset(lt, baseLocal)
		dayTag := ""
		if off > 0 {
			dayTag = fmt.Sprintf(" (+%dd)", off)
		} else if off < 0 {
			dayTag = fmt.Sprintf(" (%dd)", off)
		}
		mark := " "
		if c.ID == st.BaseID {
			mark = "*"
		}
		zone, _ := lt.Zone()
		fmt.Printf("%s %-15s %-9s%-6s %-8s %-9s %s\n",
			mark, c.Name, FormatTime(lt, st.Use24h), dayTag, LocalPhase(lt).Label(), FormatUTCOffset(lt), zone)
	}

	if len(windows) > 0 {
		fmt.Printf("\nBest overlap windows (%s time):\n", base.Name)
		for _, w := range windows {
			fmt.Printf("  %s – %s  (%d/%d working, %d/%d awake)\n",
				FormatMinutes(w.StartMin, st.Use24h),
				FormatMinutes(w.EndMin, st.Use24h),
				w.Working, w.Total, w.Awake, w.Total)
		}
	}
}

func main() {
	pngPath := flag.String("png", "", "render one settled frame to PATH and exit")
	quiet := flag.Bool("quiet", false, "open the window without activating it")
	dark := flag.Bool("dark", false, "start in dark mode")
	use12h := flag.Bool("12h", false, "use 12-hour clock labels")
	dateFlag := flag.String("date", "", "reference date YYYY-MM-DD (defaults to today, or 2026-06-15 in -png mode)")
	timeFlag := flag.String("time", "", "scrubbed time HH:MM in base city (defaults to now, or 08:30 in -png mode)")
	textMode := flag.Bool("text", false, "print a plain-text timezone table and exit")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Sunbelt — timezone overlap and 24-hour day/night scrubber\n\n")
		fmt.Fprintf(os.Stderr, "Usage: sunbelt [flags]\n\nModes:\n")
		fmt.Fprintf(os.Stderr, "  (default)        Launch interactive GUI window\n")
		fmt.Fprintf(os.Stderr, "  -png PATH        Render one deterministic settled frame to PATH and exit\n")
		fmt.Fprintf(os.Stderr, "  -text            Print a plain-text timezone overlap report and exit\n")
		fmt.Fprintf(os.Stderr, "  -quiet           Launch GUI window without activating it\n")
		fmt.Fprintf(os.Stderr, "  SHIREI_DRIVE_PORT enables automated drive testing.\n\nFlags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	// Use a deterministic reference instant for -png renders unless overridden.
	now := time.Now()
	if *pngPath != "" {
		sfLoc, err := time.LoadLocation("America/Los_Angeles")
		if err != nil {
			sfLoc = time.UTC
		}
		now = time.Date(2026, time.June, 15, 11, 15, 0, 0, sfLoc)
	}

	state = newAppState(now)
	state.DarkMode = *dark
	if *use12h {
		state.Use24h = false
	}

	if *dateFlag != "" {
		parsed, err := time.Parse("2006-01-02", *dateFlag)
		if err != nil {
			fmt.Fprintf(os.Stderr, "invalid -date %q: expected YYYY-MM-DD\n", *dateFlag)
			os.Exit(2)
		}
		state.Year, state.Month, state.Day = parsed.Date()
	}

	if *timeFlag != "" {
		mins, err := parseTimeFlag(*timeFlag)
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		state.ScrubMin = float32(mins)
	} else if *pngPath != "" {
		// Position the scrubber right inside a rich overlap morning slot (08:30 SF = 11:30 NY = 16:30 London = 17:30 Berlin = 21:00 Bengaluru = 00:30+1d Tokyo).
		state.ScrubMin = 8*60 + 30
	}

	if *textMode {
		printTextReport(state)
		return
	}

	// Configure preferred light & dark color schemes.
	SetLightColorScheme(WarmColorScheme())
	SetDarkColorScheme(WarmDarkColorScheme())
	SetDarkMode(state.DarkMode)

	if *pngPath != "" {
		if err := RenderToPNG(*pngPath, winWidth, winHeight, RootView); err != nil {
			fmt.Fprintln(os.Stderr, "render failed:", err)
			os.Exit(1)
		}
		return
	}

	// Live clock ticker for the "NOW" indicator, gated on !HeadlessRender.
	stopTick := make(chan struct{})
	generic.SetupSigTermCleanup()
	generic.AddExitCleanup(func() {
		close(stopTick)
	})
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-stopTick:
				return
			case t := <-ticker.C:
				if GetHost().HeadlessRender {
					continue
				}
				WithFrameLock(func() {
					state.Now = t
				})
				RequestNextFrame()
			}
		}
	}()

	app.SetupWindow("Sunbelt — Timezone Overlap", winWidth, winHeight)
	if *quiet {
		app.SetupQuiet()
	}
	app.SetupDrive()
	app.Run(RootView)
}
