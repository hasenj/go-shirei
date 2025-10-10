package main

import (
	"fmt"
	"math"
	"time"

	. "go.hasen.dev/shirei"
	app "go.hasen.dev/shirei/giobackend"
	. "go.hasen.dev/shirei/tw"
	. "go.hasen.dev/shirei/widgets"
)

var pickerItems []string

func init() {
	for i := 0; i <= 59; i++ {
		pickerItems = append(pickerItems, fmt.Sprintf("%d", i))
	}
}

const (
	itemHeight   float32 = 60.0
	pickerHeight float32 = 420.0
)

type ScrollerState struct {
	AnimatedIndex float32
	TargetIndex   float32
	Velocity      float32

	IsDragging     bool
	DragStartY     float32
	DragStartIndex float32
	LastMouseY     float32

	LastDelta float32
	Settled   bool
}

type CountdownState struct {
	Running   bool
	End       time.Time
	Remaining time.Duration
}

func main() {
	app.SetupWindow("Draggable Scroller", 800, 600)
	app.Run(appView)
}

func appView() {
	const (
		friction      float32 = 0.88
		snapStrength  float32 = 0.2
		snapThreshold float32 = 0.3
		momentumMult  float32 = 0.6
	)

	timeNow := time.Now()
	secState := UseWithInit("scroller-state-sec", func() *ScrollerState {
		second := timeNow.Second()
		return &ScrollerState{AnimatedIndex: float32(300 + second), TargetIndex: float32(300 + second)}
	})
	minState := UseWithInit("scroller-state-min", func() *ScrollerState {
		mins := timeNow.Minute()
		return &ScrollerState{AnimatedIndex: float32(300 + mins), TargetIndex: float32(300 + mins)}
	})
	hrState := UseWithInit("scroller-state-hr", func() *ScrollerState {
		hrs := timeNow.Hour()
		return &ScrollerState{AnimatedIndex: float32(120 + hrs), TargetIndex: float32(120 + hrs)}
	})

	secs := pickerItems
	mins := pickerItems
	hrs := make([]string, 24)
	for i := 0; i < 24; i++ {
		hrs[i] = fmt.Sprintf("%d", i)
	}

	Layout(TW(Pad(20), Row, Center, Gap(20)), func() {
		RenderVirtualScroller(hrState, hrs, func(s string) string { return s }, 200.0, itemHeight, 60.0)
		RenderVirtualScroller(minState, mins, func(s string) string { return s }, 200.0, itemHeight, 30.0)
		RenderVirtualScroller(secState, secs, func(s string) string { return s }, 200.0, itemHeight, 0.0)
	})

	countdown := UseWithInit("countdown-state", func() *CountdownState {
		return &CountdownState{Running: false}
	})

	Layout(TW(Pad(20), Row, Center, Gap(20)), func() {
		h := int(math.Round(float64(hrState.AnimatedIndex))) % 24
		m := int(math.Round(float64(minState.AnimatedIndex))) % 60
		s := int(math.Round(float64(secState.AnimatedIndex))) % 60

		Layout(TW(FixSize(520, 130), Center), func() {
			ModAttrs(func(a *Attrs) {
				a.Corners = Vec4{14, 14, 14, 14}
				a.Background = Vec4{255, 255, 255, 0.04}
				a.BorderWidth = 1.0
				a.BorderColor = Vec4{220, 220, 220, 0.08}
			})

			Layout(TW(Row, Center, Gap(18)), func() {
				Layout(TW(FixSize(140, 110), Center), func() {
					Label(fmt.Sprintf("%02d", h), Sz(64), FontWeight(WeightBold), Clr(0, 0, 255, 1.0))
				})

				Layout(TW(FixSize(20, 110), Center), func() {
					Label(":", Sz(56), Clr(0, 0, 85, 0.9))
				})

				Layout(TW(FixSize(140, 110), Center), func() {
					Label(fmt.Sprintf("%02d", m), Sz(64), FontWeight(WeightBold), Clr(0, 255, 255, 1.0))
				})

				Layout(TW(FixSize(20, 110), Center), func() {
					Label(":", Sz(56), Clr(0, 0, 85, 0.9))
				})

				Layout(TW(FixSize(100, 110), Center), func() {
					Label(fmt.Sprintf("%02d", s), Sz(64), FontWeight(WeightBold), Clr(255, 0, 0, 1.0))
				})
			})
		})
		Layout(TW(FixSize(120, 44), Center), func() {
			if !countdown.Running {
				if Button(3, "Set") {
					now := time.Now()
					alarmTime := time.Date(now.Year(), now.Month(), now.Day(), h, m, s, 0, now.Location())
					if alarmTime.Before(now) {
						alarmTime = alarmTime.Add(24 * time.Hour)
					}
					countdown.End = alarmTime
					countdown.Remaining = alarmTime.Sub(now)
					countdown.Running = true
					RequestNextFrame()
					go func(d time.Duration) {
						time.Sleep(d)
						fmt.Println("Time's up!")
					}(countdown.Remaining)
				}
			} else {
				now := time.Now()
				if now.After(countdown.End) {
					countdown.Running = false
					countdown.Remaining = 0
				} else {
					countdown.Remaining = countdown.End.Sub(now)
					RequestNextFrame()
				}
				if Button(4, formatDuration(countdown.Remaining)) {
					countdown.Running = false
					countdown.Remaining = 0
				}
			}
		})
	})

}

func minF32(a, b float32) float32 {
	if a < b {
		return a
	}
	return b
}

func absf32(x float32) float32 {
	if x < 0 {
		return -x
	}
	return x
}

func formatDuration(d time.Duration) string {
	if d <= 0 {
		return "00:00:00"
	}
	total := int(d.Seconds())
	h := total / 3600
	m := (total % 3600) / 60
	s := total % 60
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}

func drawPickerItemWithHue(text string, yPos float32, hueOffset float32, colWidth float32) {
	itemCenterY := yPos + (itemHeight / 2)
	pickerCenterY := pickerHeight / 2
	distanceFromCenter := absf32(itemCenterY - pickerCenterY)

	maxDistance := pickerHeight / 2
	progress := minF32(1.0, distanceFromCenter/maxDistance)

	perspectiveOffset := float32(math.Sin(float64(progress*math.Pi/3))) * 32.0
	if itemCenterY < pickerCenterY {
		perspectiveOffset = -perspectiveOffset
	}

	alpha := 1.0 - progress*0.8
	sizeFactor := 1.0 - (progress * 0.5)
	depthAlpha := 1.0 - (progress * 0.3)
	alpha *= depthAlpha

	var hue, saturation, lightness float32
	if progress < 0.3 {
		hue = 180.0 + hueOffset
		saturation = 80.0
		lightness = 55.0 + (1.0-progress)*15.0
	} else {
		hue = 180.0 + progress*60.0 + hueOffset
		saturation = 70.0 - progress*40.0
		lightness = 45.0 - progress*20.0
	}

	scale := colWidth / 200.0
	if scale < 0.6 {
		scale = 0.6
	}
	fontSize := 32.0 * sizeFactor * scale
	xOffset := 25.0 + perspectiveOffset

	innerWidth := colWidth - 40.0
	if innerWidth < 80.0 {
		innerWidth = colWidth - 10.0
	}

	hOffset := (colWidth-innerWidth)/2 + xOffset
	Layout(TW(Float(hOffset, yPos), FixSize(innerWidth, itemHeight), Row, Center), func() {
		textStyle := []TextAttrsFn{
			Sz(fontSize),
			FontWeight(WeightBold),
			Clr(hue, saturation, lightness, alpha),
		}
		Label(text, textStyle...)
	})
}

// generic vertical scroller - pass formatter to convert items to strings
func RenderVirtualScroller[T any](
	state *ScrollerState,
	items []T,
	formatter func(T) string,
	width float32,
	elemHeight float32,
	hueOffset float32,
) {
	const (
		friction      float32 = 0.88
		snapStrength  float32 = 0.2
		snapThreshold float32 = 0.3
		momentumMult  float32 = 0.6
	)

	needFrame := false

	if state.IsDragging {
		currentMouseY := InputState.MousePoint[1]
		if FrameInput.Mouse == MouseRelease {
			state.IsDragging = false
			state.Velocity *= momentumMult
			state.TargetIndex = float32(math.Round(float64(state.AnimatedIndex)))
			if absf32(state.Velocity) < snapThreshold*1.5 {
				state.AnimatedIndex = state.TargetIndex
				state.Velocity = 0
				state.Settled = true
			} else {
				state.Settled = false
			}
			needFrame = true
		} else {
			deltaY := state.DragStartY - currentMouseY
			deltaIndex := deltaY / elemHeight
			newIndex := state.DragStartIndex + deltaIndex
			frameDelta := state.LastMouseY - currentMouseY
			state.Velocity = frameDelta / elemHeight
			state.AnimatedIndex = newIndex
			state.TargetIndex = newIndex
			state.LastMouseY = currentMouseY
			state.Settled = false
			needFrame = true
		}
	} else {
		state.AnimatedIndex += state.Velocity
		state.Velocity *= friction

		if absf32(state.Velocity) < snapThreshold {
			nearestItem := float32(math.Round(float64(state.AnimatedIndex)))
			state.TargetIndex = nearestItem
			state.Velocity = 0
		}

		diff := state.TargetIndex - state.AnimatedIndex
		if absf32(diff) > 0.001 {
			state.AnimatedIndex += diff * snapStrength
			state.Settled = false
			needFrame = true
		} else if state.Velocity == 0 && absf32(diff) > 0 {
			state.AnimatedIndex = state.TargetIndex
			state.Settled = true
		} else if state.Velocity != 0 {
			state.Settled = false
			needFrame = true
		} else {
			state.Settled = true
		}
	}

	Layout(TW(FixSize(width, pickerHeight)), func() {
		ModAttrs(func(a *Attrs) {
			a.Clip = true
		})

		if IsHovered() && FrameInput.Mouse == MouseClick {
			state.IsDragging = true
			state.DragStartY = InputState.MousePoint[1]
			state.DragStartIndex = state.AnimatedIndex
			state.LastMouseY = InputState.MousePoint[1]
			state.Velocity = 0
			state.Settled = false
			needFrame = true
		}

		centerOffset := (pickerHeight - elemHeight) / 2
		filmstripOffset := centerOffset - (state.AnimatedIndex * elemHeight)

		visibleItems := int(math.Ceil(float64(pickerHeight / elemHeight)))
		bufferItems := visibleItems * 2

		centerIndex := int(math.Round(float64(state.AnimatedIndex)))
		firstIndexToCheck := centerIndex - bufferItems
		lastIndexToCheck := centerIndex + bufferItems

		for virtualIndex := firstIndexToCheck; virtualIndex <= lastIndexToCheck; virtualIndex++ {
			if virtualIndex < 0 {
				continue
			}

			dataIndex := (virtualIndex%len(items) + len(items)) % len(items)
			itemText := formatter(items[dataIndex])
			itemYOnFilmstrip := float32(virtualIndex) * elemHeight
			finalYPos := filmstripOffset + itemYOnFilmstrip

			if finalYPos > -elemHeight*2 && finalYPos < pickerHeight+elemHeight*2 {
				if virtualIndex == int(math.Round(float64(state.AnimatedIndex))) {
					Layout(TW(Float(0, finalYPos), FixSize(width, elemHeight), BG(255, 255, 255, 0.06), BR(10)), func() {})
				}
				drawPickerItemWithHue(itemText, finalYPos, hueOffset, width)
			}
		}

		if needFrame && !state.Settled {
			RequestNextFrame()
		}

	})
}
