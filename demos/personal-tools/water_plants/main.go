package main

import (
	"errors"
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
	defaultWidth  = 390
	defaultHeight = 844
)

// UndoRecord lets the user revert the most recent one-tap watering.
type UndoRecord struct {
	Plant           *Plant
	PrevLastWatered string
	PrevWaterCount  int
}

// AppState is the durable application model for Water Plants.
type AppState struct {
	Plants   []*Plant
	Today    time.Time
	Filter   FilterTab
	DarkMode bool

	DataPath string // empty disables disk persistence (-demo / -png)
	SaveErr  string

	LastUndo *UndoRecord

	// Detail / edit modal state
	Selected      *Plant
	EditName      string
	EditLocation  string
	EditInterval  int
	EditKind      PlantKind
	EditLight     LightLevel
	EditNotes     string
	ConfirmDelete bool

	// Add modal state
	ShowAdd     bool
	AddName     string
	AddLocation string
	AddInterval int
	AddKind     PlantKind
	AddLight    LightLevel
	AddNotes    string
}

var state *AppState

func newAppState(today time.Time, plants []*Plant, dataPath string) *AppState {
	return &AppState{
		Plants:      plants,
		Today:       CalendarDay(today),
		Filter:      FilterAll,
		DataPath:    dataPath,
		AddInterval: 7,
		AddKind:     KindTropical,
		AddLight:    LightMedium,
	}
}

func (st *AppState) persist() {
	if st.DataPath == "" {
		return
	}
	if err := SavePlants(st.DataPath, st.Plants); err != nil {
		st.SaveErr = err.Error()
	} else {
		st.SaveErr = ""
	}
}

// WaterNow marks p as watered today and records an undo snapshot.
func (st *AppState) WaterNow(p *Plant) {
	if p == nil {
		return
	}
	todayStr := FormatISODate(st.Today)
	if p.LastWatered == todayStr {
		return
	}
	st.LastUndo = &UndoRecord{
		Plant:           p,
		PrevLastWatered: p.LastWatered,
		PrevWaterCount:  p.WaterCount,
	}
	p.PrevLastWatered = p.LastWatered
	p.LastWatered = todayStr
	p.WaterCount++
	st.persist()
}

// ToggleWaterToday waters p if not watered today, or reverts today's watering if possible.
func (st *AppState) ToggleWaterToday(p *Plant) {
	if p == nil {
		return
	}
	status := p.Status(st.Today)
	if !status.WateredToday {
		st.WaterNow(p)
		return
	}
	if p.PrevLastWatered != "" {
		p.LastWatered = p.PrevLastWatered
		p.PrevLastWatered = ""
		if p.WaterCount > 0 {
			p.WaterCount--
		}
		if st.LastUndo != nil && st.LastUndo.Plant == p {
			st.LastUndo = nil
		}
		st.persist()
	}
}

// UndoLastWatering reverts the most recent one-tap watering.
func (st *AppState) UndoLastWatering() {
	if st.LastUndo == nil || st.LastUndo.Plant == nil {
		return
	}
	u := st.LastUndo
	u.Plant.LastWatered = u.PrevLastWatered
	u.Plant.PrevLastWatered = ""
	u.Plant.WaterCount = u.PrevWaterCount
	st.LastUndo = nil
	st.persist()
}

func (st *AppState) openDetail(p *Plant) {
	st.ShowAdd = false
	st.Selected = p
	st.EditName = p.Name
	st.EditLocation = p.Location
	st.EditInterval = p.IntervalDays
	st.EditKind = p.Kind
	st.EditLight = p.Light
	st.EditNotes = p.Notes
	st.ConfirmDelete = false
}

func (st *AppState) closeDetail() {
	st.Selected = nil
	st.ConfirmDelete = false
}

func (st *AppState) saveDetail() {
	p := st.Selected
	if p == nil {
		return
	}
	name := strings.TrimSpace(st.EditName)
	if name == "" {
		return
	}
	interval := st.EditInterval
	if interval < 1 {
		interval = 1
	}
	if interval > 90 {
		interval = 90
	}
	p.Name = name
	p.Location = strings.TrimSpace(st.EditLocation)
	if p.Location == "" {
		p.Location = "Home"
	}
	p.IntervalDays = interval
	p.Kind = st.EditKind
	p.Light = st.EditLight
	p.Notes = strings.TrimSpace(st.EditNotes)
	st.closeDetail()
	st.persist()
}

func (st *AppState) deleteSelected() {
	p := st.Selected
	if p == nil {
		return
	}
	for i, cur := range st.Plants {
		if cur == p {
			st.Plants = append(st.Plants[:i], st.Plants[i+1:]...)
			break
		}
	}
	if st.LastUndo != nil && st.LastUndo.Plant == p {
		st.LastUndo = nil
	}
	st.closeDetail()
	st.persist()
}

func (st *AppState) openAdd() {
	st.Selected = nil
	st.ConfirmDelete = false
	st.ShowAdd = true
	st.AddName = ""
	st.AddLocation = ""
	st.AddInterval = 7
	st.AddKind = KindTropical
	st.AddLight = LightMedium
	st.AddNotes = ""
}

func (st *AppState) submitAdd() {
	name := strings.TrimSpace(st.AddName)
	if name == "" {
		return
	}
	loc := strings.TrimSpace(st.AddLocation)
	if loc == "" {
		loc = "Living Room"
	}
	interval := st.AddInterval
	if interval < 1 {
		interval = 1
	}
	if interval > 90 {
		interval = 90
	}
	p := &Plant{
		ID:           slugifyID(name, st.Plants),
		Name:         name,
		Location:     loc,
		Kind:         st.AddKind,
		Light:        st.AddLight,
		IntervalDays: interval,
		LastWatered:  FormatISODate(st.Today.AddDate(0, 0, -interval)), // due today on creation
		WaterCount:   0,
		Notes:        strings.TrimSpace(st.AddNotes),
	}
	st.Plants = append(st.Plants, p)
	st.ShowAdd = false
	st.persist()
}

func findPlantByQuery(plants []*Plant, query string) *Plant {
	q := strings.ToLower(strings.TrimSpace(query))
	if q == "" {
		return nil
	}
	for _, p := range plants {
		if strings.ToLower(p.ID) == q || strings.ToLower(p.Name) == q {
			return p
		}
	}
	for _, p := range plants {
		if strings.Contains(strings.ToLower(p.Name), q) {
			return p
		}
	}
	return nil
}

func printTextReport(st *AppState) {
	sum := ComputeSummary(st.Plants, st.Today)
	fmt.Printf("Water Plants — %s\n", st.Today.Format("Mon, Jan 2, 2006"))
	fmt.Printf("Summary: %d total (%d overdue, %d due today, %d soon, %d happy)\n\n",
		sum.Total, sum.Overdue, sum.DueToday, sum.Soon, sum.Happy)

	ordered := SortedPlants(st.Plants, st.Today, st.Filter)
	for _, p := range ordered {
		ps := p.Status(st.Today)
		fmt.Printf("%-20s  %-12s  every %2dd  %-18s  %s\n",
			p.Name, p.Location, p.IntervalDays, ps.DueHeadline(), ps.NextDateCaption())
	}
}

func main() {
	pngPath := flag.String("png", "", "render one settled portrait frame to PATH and exit")
	width := flag.Int("width", defaultWidth, "window or PNG logical width")
	height := flag.Int("height", defaultHeight, "window or PNG logical height")
	dark := flag.Bool("dark", false, "start in dark mode")
	demo := flag.Bool("demo", false, "use built-in sample plants without reading or writing disk")
	dataFlag := flag.String("data", "", "JSON storage file path (overrides WATER_PLANTS_DATA)")
	dateFlag := flag.String("date", "", "reference date YYYY-MM-DD (defaults to today, or 2026-06-15 in -png mode)")
	filterFlag := flag.String("filter", "all", "initial filter: all, thirsty, soon, happy")
	textMode := flag.Bool("text", false, "print a plain-text watering schedule and exit")
	waterQuery := flag.String("water", "", "mark matching plant watered today and exit")
	quiet := flag.Bool("quiet", false, "open the GUI window without activating it")

	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Water Plants — mobile-friendly houseplant watering tracker\n\n")
		fmt.Fprintf(os.Stderr, "Usage: water_plants [flags]\n\nModes:\n")
		fmt.Fprintf(os.Stderr, "  (default)        Launch interactive GUI window (390x844 portrait)\n")
		fmt.Fprintf(os.Stderr, "  -png PATH        Render one deterministic settled frame to PATH and exit\n")
		fmt.Fprintf(os.Stderr, "  -text            Print plain-text watering schedule to stdout and exit\n")
		fmt.Fprintf(os.Stderr, "  -water NAME      Mark matching plant watered today and exit\n")
		fmt.Fprintf(os.Stderr, "  -quiet           Launch GUI window without activating it\n")
		fmt.Fprintf(os.Stderr, "  SHIREI_DRIVE_PORT enables automated drive testing.\n\nFlags:\n")
		flag.PrintDefaults()
	}
	flag.Parse()

	filter, ok := ParseFilterTab(*filterFlag)
	if !ok {
		fmt.Fprintf(os.Stderr, "invalid -filter %q: expected all, thirsty, soon, or happy\n", *filterFlag)
		os.Exit(2)
	}

	today := CalendarDay(time.Now())
	if *pngPath != "" && *dateFlag == "" {
		today = time.Date(2026, time.June, 15, 0, 0, 0, 0, time.UTC)
	}
	if *dateFlag != "" {
		parsed, ok := ParseISODate(*dateFlag)
		if !ok {
			fmt.Fprintf(os.Stderr, "invalid -date %q: expected YYYY-MM-DD\n", *dateFlag)
			os.Exit(2)
		}
		today = parsed
	}

	useDemo := *demo || (*pngPath != "" && *dataFlag == "")
	dataPath := ""
	var plants []*Plant
	if useDemo {
		plants = SamplePlants(today)
	} else {
		dataPath = *dataFlag
		if dataPath == "" {
			dataPath = DefaultDataPath()
		}
		loaded, err := LoadPlants(dataPath)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				plants = SamplePlants(today)
			} else {
				fmt.Fprintf(os.Stderr, "failed to load %s: %v\n", dataPath, err)
				os.Exit(1)
			}
		} else {
			plants = loaded
		}
	}

	state = newAppState(today, plants, dataPath)
	state.Filter = filter
	state.DarkMode = *dark

	if *waterQuery != "" {
		p := findPlantByQuery(state.Plants, *waterQuery)
		if p == nil {
			fmt.Fprintf(os.Stderr, "no plant matching %q\n", *waterQuery)
			os.Exit(1)
		}
		state.WaterNow(p)
		if state.SaveErr != "" {
			fmt.Fprintf(os.Stderr, "failed to save: %s\n", state.SaveErr)
			os.Exit(1)
		}
		ps := p.Status(state.Today)
		fmt.Printf("Watered %s (%s). %s.\n", p.Name, p.Location, ps.NextDateCaption())
		return
	}

	if *textMode {
		printTextReport(state)
		return
	}

	SetLightColorScheme(WarmColorScheme())
	SetDarkColorScheme(WarmDarkColorScheme())
	SetDarkMode(state.DarkMode)

	if *pngPath != "" {
		w, h := *width, *height
		if w < 240 {
			w = defaultWidth
		}
		if h < 320 {
			h = defaultHeight
		}
		if err := RenderToPNG(*pngPath, w, h, RootView); err != nil {
			fmt.Fprintln(os.Stderr, "render failed:", err)
			os.Exit(1)
		}
		return
	}

	// Refresh calendar day if the app stays open across midnight (gated on !HeadlessRender).
	if *dateFlag == "" {
		stopTick := make(chan struct{})
		generic.SetupSigTermCleanup()
		generic.AddExitCleanup(func() {
			close(stopTick)
		})
		go func() {
			ticker := time.NewTicker(1 * time.Minute)
			defer ticker.Stop()
			for {
				select {
				case <-stopTick:
					return
				case t := <-ticker.C:
					if GetHost().HeadlessRender {
						continue
					}
					curDay := CalendarDay(t)
					Post(func() {
						if !curDay.Equal(state.Today) {
							state.Today = curDay
						}
					})
				}
			}
		}()
	}

	app.SetupWindow("Water Plants", *width, *height)
	if *quiet {
		app.SetupQuiet()
	}
	app.SetupDrive()
	app.Run(RootView)
}
