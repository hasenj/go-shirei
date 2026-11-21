// Command go_board is a board for the game of Go. Each tab is one game.
// A new game asks for 9×9, 13×13, or 19×19 before the first move.
// The board fills the square beside the side panel.
package main

import (
	"flag"
	"fmt"
	"math"
	"os"
	"strconv"

	app "go.hasen.dev/shirei/app"
	"go.hasen.dev/shirei/ext/darkmode"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

type f32 = float32

// Reference proportions for a 28px pitch. The live pitch is an integer so
// the 1px grid lines sit on pixel boundaries, and it does not go below this
// reference: a smaller window scrolls.
const (
	lineT       f32 = 1
	labelBand   f32 = 18
	labelGutter f32 = 26
	panelW      f32 = 200
	rowGap      f32 = 28
	rowPad      f32 = 16

	refPitch  = 28
	refMargin = 24
	refStone  = 26
	refStar   = 6
	refMark   = 7
	minPitch  = refPitch

	winW = 880
	winH = 688
)

// Board geometry for this frame. fitBoard writes it before the board is drawn.
var (
	pitch  f32 = refPitch
	margin f32 = refMargin
	stoneD f32 = refStone
	starD  f32 = refStar
	markD  f32 = refMark
	tabH   f32
)

var (
	woodColor = Vec4{33, 46, 63, 1}
	woodEdge  = Vec4{28, 38, 42, 1}
	lineColor = Vec4{26, 34, 30, 1}
)

// A match is one game. The size is fixed when the match is created.
type match struct {
	game   Game
	notice string
}

var (
	matches     []*match
	active      *match
	newGameOpen bool
	newSize     = 19
)

func (m *match) label() string {
	n := len(m.game.past)
	if n == 0 {
		return fmt.Sprintf("%d×%d", m.game.N, m.game.N)
	}
	return fmt.Sprintf("%d×%d · %d", m.game.N, m.game.N, n)
}

func addMatch(n int) {
	m := &match{game: NewGame(n)}
	matches = append(matches, m)
	active = m
	newGameOpen = false
}

func closeMatch(m *match) {
	idx := -1
	for i, g := range matches {
		if g == m {
			idx = i
			break
		}
	}
	if idx < 0 {
		return
	}
	matches = append(matches[:idx], matches[idx+1:]...)
	if active != m {
		return
	}
	if len(matches) == 0 {
		active = nil
		return
	}
	if idx >= len(matches) {
		idx = len(matches) - 1
	}
	active = matches[idx]
}

func openNewGame() {
	newSize = 19
	newGameOpen = true
}

type lineKey struct {
	vertical bool
	i        int
}

type starKey struct{ x, y int }
type stoneKey struct{ x, y int }
type ghostKey struct{}

func main() {
	pngPath := flag.String("png", "", "write one settled frame to PATH and exit")
	flag.Parse()
	if *pngPath != "" {
		if err := RenderToPNG(*pngPath, winW, winH, RootView); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	addMatch(19)
	app.SetupIconBytes(iconPNG)
	app.SetupWindow("Go", winW, winH)
	app.Run(RootView)
}

func RootView() {
	SetDarkMode(darkmode.OSDarkMode())
	if active != nil && !newGameOpen {
		handleKeys()
	}
	ModAttrs(UseSurface(SurfaceCanvas))
	Container(Attrs(Viewport), func() {
		ScrollOnInput()
		tabBar()
		if active == nil {
			emptyState()
		} else {
			boardRow()
		}
		ScrollBars()
		ScrollBarExt(ScrollBarAttrs{Horizontal: true})
	})
	if newGameOpen {
		newGameDialog()
	}
}

func handleKeys() {
	g := &active.game
	switch GetFrameInput().Key {
	case KeyP:
		g.Pass()
		active.notice = ""
	case KeyZ:
		if GetInputState().Modifiers&PrimaryMod() != 0 && g.Undo() {
			active.notice = ""
		}
	}
}

func tabBar() {
	// The tab scroller sizes from its parent, so this row spans the window.
	Container(Attrs(Expand), func() {
		var close *match
		TabStrip(func() {
			for _, m := range matches {
				if TabItem(m, m.label(), m == active, func() {
					if TabCloseButton() {
						close = m
					}
				}) {
					active = m
				}
			}
		}, func() {
			if CtrlButton(SymPlus, "New", true) {
				openNewGame()
			}
		})
		if close != nil {
			closeMatch(close)
		}
		if h := GetResolvedHeight(); h > 1 {
			tabH = h
		}
	})
}

func emptyState() {
	Container(Attrs(Grow(1), Expand, Center, Gap(12)), func() {
		Label("No game", FontSize(18))
		if Button(NoIcon, "New game") {
			openNewGame()
		}
	})
}

func newGameDialog() {
	Modal(280, func() { newGameOpen = false }, func() {
		Label("New game", FontSize(18), FontWeight(WeightBold))
		Label("Board size")
		SegmentedControl(&newSize, func() {
			SegmentedCell("9", 9)
			SegmentedCell("13", 13)
			SegmentedCell("19", 19)
		})
		Container(Attrs(Row, Gap(8)), func() {
			if Button(NoIcon, "Cancel") {
				newGameOpen = false
			}
			if Button(NoIcon, "Start") {
				addMatch(newSize)
			}
		})
	})
}

// boardOuter is the board column for pitch p: wood plus the coordinate labels.
func boardOuter(n, p int) (w, h f32) {
	m := p * refMargin / refPitch
	wood := 2*m + (n-1)*p
	return f32(wood) + 2*labelGutter, f32(wood) + 2*labelBand
}

// choosePitch is the largest integer pitch whose board column fits in the
// budget. The budget includes the coordinate labels. The result is at least
// minPitch, even when that column is taller than the window.
func choosePitch(n int, budgetW, budgetH f32) int {
	p := minPitch
	if n < 2 || budgetW < 1 || budgetH < 1 {
		return p
	}
	limit := budgetW
	if budgetH < limit {
		limit = budgetH
	}
	// Margin is refMargin/refPitch of the pitch, so the column is a little
	// wider than the grid alone. Start near that estimate and step onto
	// the largest pitch that still fits.
	denom := float64(n-1) + 2*float64(refMargin)/float64(refPitch)
	guess := int(float64(limit)/denom) + 2
	if guess < p {
		guess = p
	}
	if guess > 512 {
		guess = 512
	}
	for guess > p && !pitchFits(n, guess, budgetW, budgetH) {
		guess--
	}
	for guess < 512 && pitchFits(n, guess+1, budgetW, budgetH) {
		guess++
	}
	return guess
}

func pitchFits(n, p int, budgetW, budgetH f32) bool {
	w, h := boardOuter(n, p)
	return w <= budgetW && h <= budgetH
}

// fitBoard stores the wood geometry for this frame from the space available
// to the board column.
func fitBoard(n int, budgetW, budgetH f32) {
	p := choosePitch(n, budgetW, budgetH)
	pitch = f32(p)
	margin = f32(p * refMargin / refPitch)
	stoneD = f32(p * refStone / refPitch)
	starD = f32(p * refStar / refPitch)
	if starD < 4 {
		starD = 4
	}
	markD = stoneD * f32(refMark) / f32(refStone)
	if markD < 3 {
		markD = 3
	}
}

// boardRow sizes the row to the viewport slot under the tab strip, then
// fits the board into the square beside the side panel. A pitch of minPitch
// that does not fit makes the row larger than the viewport, which scrolls.
func boardRow() {
	n := active.game.N
	vw := GetContentWidth()
	vh := GetContentHeight()
	if vw < 1 || vh < 1 {
		ws := GetHost().WindowSize
		if vw < 1 {
			vw = ws[0]
		}
		if vh < 1 {
			vh = ws[1]
		}
	}
	if vw < 1 {
		vw = winW
	}
	if vh < 1 {
		vh = winH
	}
	th := tabH
	if th < 1 {
		th = 33
	}
	rowW := vw
	rowH := vh - th
	minW, minH := boardOuter(n, minPitch)
	needW := minW + rowGap + panelW + 2*rowPad
	needH := minH + 2*rowPad
	if rowW < needW {
		rowW = needW
	}
	if rowH < needH {
		rowH = needH
	}
	fitBoard(n, rowW-2*rowPad-rowGap-panelW, rowH-2*rowPad)
	Container(Attrs(Row, FixSize(rowW, rowH), Pad(rowPad), Gap(rowGap), Center), func() {
		boardView()
		sidePanel()
	})
}

func boardPx(n int) f32 {
	return margin*2 + f32(n-1)*pitch
}

func pointAt(i int) f32 {
	return margin + f32(i)*pitch
}

func boardView() {
	n := active.game.N
	px := boardPx(n)
	Container(Attrs(), func() {
		letterRow(n, px)
		Container(Attrs(Row), func() {
			numberCol(n, px)
			wood(n)
			numberCol(n, px)
		})
		letterRow(n, px)
	})
}

func letterRow(n int, px f32) {
	Container(Attrs(Row, FixHeight(labelBand)), func() {
		Spacer(labelGutter)
		Container(Attrs(FixSize(px, labelBand)), func() {
			for x := 0; x < n; x++ {
				ContainerWithKey(x, Attrs(
					Float(pointAt(x)-pitch/2, 0),
					FixSize(pitch, labelBand),
					Center,
				), func() {
					Label(colName(x), FontSize(11))
				})
			}
		})
		Spacer(labelGutter)
	})
}

func numberCol(n int, px f32) {
	Container(Attrs(FixSize(labelGutter, px)), func() {
		for y := 0; y < n; y++ {
			ContainerWithKey(y, Attrs(
				Float(0, pointAt(y)-pitch/2),
				FixSize(labelGutter, pitch),
				Center,
			), func() {
				Label(rowName(y, n), FontSize(11))
			})
		}
	})
}

func colName(x int) string {
	if x >= 8 {
		x++
	}
	return string(rune('A' + x))
}

func rowName(y, n int) string {
	return strconv.Itoa(n - y)
}

func wood(n int) {
	px := boardPx(n)
	Container(Attrs(
		FixSize(px, px),
		Corners(4),
		BackgroundVec(woodColor),
		BorderWidth(2),
		BorderColorVec(woodEdge),
	), func() {
		g := &active.game
		if x, y, hit := pointerOnBoard(n); hit && GetFrameInput().Mouse.Has(MouseClick) {
			ok, reason := g.Play(x, y)
			switch {
			case ok:
				active.notice = ""
			case reason != "":
				active.notice = reasonLabel(reason)
			}
		}
		gridLines(n)
		for _, s := range starPoints(n) {
			disc(starKey{s[0], s[1]}, s[0], s[1], starD, lineColor, false)
		}
		for y := 0; y < n; y++ {
			for x := 0; x < n; x++ {
				c := g.At(x, y)
				if c == Empty {
					continue
				}
				last := g.HasLast && g.LastX == x && g.LastY == y
				paintStone(stoneKey{x, y}, x, y, c, false, last)
			}
		}
		if x, y, hit := pointerOnBoard(n); hit && g.At(x, y) == Empty && g.Legal(x, y) {
			paintStone(ghostKey{}, x, y, g.Turn, true, false)
		}
	})
}

func pointerOnBoard(n int) (x, y int, hit bool) {
	if !IsHovered() {
		return
	}
	rect := GetScreenRect()
	if rect.Size[0] < 1 {
		return
	}
	local := Vec2Sub(GetInputState().MousePoint, rect.Origin)
	x = int(math.Round(float64((local[0] - margin) / pitch)))
	y = int(math.Round(float64((local[1] - margin) / pitch)))
	if x < 0 || y < 0 || x >= n || y >= n {
		return
	}
	dx := local[0] - pointAt(x)
	dy := local[1] - pointAt(y)
	if dx < 0 {
		dx = -dx
	}
	if dy < 0 {
		dy = -dy
	}
	if dx > pitch/2 || dy > pitch/2 {
		return
	}
	return x, y, true
}

func gridLines(n int) {
	span := f32(n-1)*pitch + lineT
	for i := 0; i < n; i++ {
		p := pointAt(i) - lineT/2
		ElementWithKey(lineKey{false, i}, Attrs(
			Float(margin-lineT/2, p),
			FixSize(span, lineT),
			BackgroundVec(lineColor),
			Behind,
		))
		ElementWithKey(lineKey{true, i}, Attrs(
			Float(p, margin-lineT/2),
			FixSize(lineT, span),
			BackgroundVec(lineColor),
			Behind,
		))
	}
}

func starPoints(n int) [][2]int {
	switch n {
	case 9:
		return [][2]int{{2, 2}, {2, 6}, {4, 4}, {6, 2}, {6, 6}}
	case 13:
		return [][2]int{{3, 3}, {3, 9}, {6, 6}, {9, 3}, {9, 9}}
	default:
		return [][2]int{
			{3, 3}, {3, 9}, {3, 15},
			{9, 3}, {9, 9}, {9, 15},
			{15, 3}, {15, 9}, {15, 15},
		}
	}
}

func disc(key any, x, y int, d f32, color Vec4, front bool) {
	attrs := Attrs(
		Float(pointAt(x)-d/2, pointAt(y)-d/2),
		FixSize(d, d),
		Corners(d/2),
		BackgroundVec(color),
	)
	if front {
		attrs = AttrsWith(attrs, InFront)
	}
	ElementWithKey(key, attrs)
}

func paintStone(key any, x, y int, c Stone, ghost, last bool) {
	d := stoneD
	attrs := Attrs(
		Float(pointAt(x)-d/2, pointAt(y)-d/2),
		FixSize(d, d),
		Corners(d/2),
		InFront,
	)
	if ghost {
		attrs = AttrsWith(attrs, Trans(0.45))
	} else {
		attrs = AttrsWith(attrs, BoxShadow(2))
	}
	if c == Black {
		attrs = AttrsWith(attrs, Background(0, 0, 16, 1), Grad(0, 0, -8, 0))
	} else {
		attrs = AttrsWith(attrs, Background(36, 8, 98, 1), Grad(0, 0, -10, 0), BorderWidth(1), BorderColor(30, 12, 72, 1))
	}
	ContainerWithKey(key, attrs, func() {
		if !last {
			return
		}
		m := markD
		light := f32(92)
		if c == White {
			light = 14
		}
		Element(Attrs(
			Float((d-m)/2, (d-m)/2),
			FixSize(m, m),
			Corners(m/2),
			Background(0, 0, light, 1),
		))
	})
}

func sidePanel() {
	g := &active.game
	Container(Attrs(FixWidth(panelW), Gap(12)), func() {
		Label("Go", FontSize(22), FontWeight(WeightBold))
		Label(fmt.Sprintf("%d×%d", g.N, g.N), FontSize(13))
		Container(Attrs(Row, CrossMid, Gap(8)), func() {
			miniStone(g.Turn)
			if g.Turn == Black {
				Label("Black to play")
			} else {
				Label("White to play")
			}
		})
		if g.Passed {
			who := "White"
			if other(g.Turn) == Black {
				who = "Black"
			}
			Label(who+" passed", FontSize(12))
		}
		if active.notice != "" {
			Label(active.notice, FontSize(12))
		}

		Label("Prisoners", FontSize(12))
		prisonerRow(Black)
		prisonerRow(White)

		Container(Attrs(Row, Gap(8)), func() {
			if Button(NoIcon, "Pass") {
				g.Pass()
				active.notice = ""
			}
			NextButtonDisabled(!g.CanUndo())
			if Button(NoIcon, "Undo") {
				g.Undo()
				active.notice = ""
			}
		})
		Label("P to pass", FontSize(12))
	})
}

func prisonerRow(c Stone) {
	Container(Attrs(Row, CrossMid, Gap(8)), func() {
		miniStone(c)
		Label(strconv.Itoa(active.game.Captured[c]))
	})
}

func miniStone(c Stone) {
	const d f32 = 14
	attrs := Attrs(FixSize(d, d), Corners(d/2))
	if c == Black {
		attrs = AttrsWith(attrs, Background(0, 0, 12, 1), BorderWidth(1), BorderColor(0, 0, 78, 1))
	} else {
		attrs = AttrsWith(attrs, Background(0, 0, 98, 1), BorderWidth(1), BorderColor(0, 0, 60, 1))
	}
	Element(attrs)
}

func reasonLabel(reason string) string {
	switch reason {
	case "suicide":
		return "Suicide"
	case "ko":
		return "Ko"
	default:
		return ""
	}
}
