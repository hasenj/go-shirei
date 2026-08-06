package main

import (
	"fmt"
	"time"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

type f32 = float32

func (s *appState) rootView() {
	ModAttrs(Viewport, Expand, Background(220, 8, 96, 1))

	pass, fail, building, ready, running, total := s.counts()
	s.mu.Lock()
	isRunning := s.running
	detail := s.detailIdx
	root := s.root
	s.mu.Unlock()

	Container(Attrs(Pad(14), Gap(12), Grow(1), Expand), func() {
		Container(Attrs(Row, CrossMid, Gap(12), Expand), func() {
			Label("behavior_runner", FontSize(18), FontWeight(WeightBold))
			Label(fmt.Sprintf("%d tests · %d pass · %d fail", total, pass, fail),
				FontSize(12), TextColor(0, 0, 45, 1))
			if building+ready+running > 0 {
				Label(fmt.Sprintf("· %d building · %d ready · %d running", building, ready, running),
					FontSize(12), TextColor(0, 0, 50, 1))
			}
			Filler(1)
			if isRunning {
				if ButtonExt("Stop", ButtonAttrs{}, DefaultButtonLook()) {
					s.stopRun()
				}
			} else {
				if ButtonExt("Run all", ButtonAttrs{Accent: AccentMeadow}, DefaultButtonLook()) {
					s.startRunAll()
				}
			}
		})
		Label(root, FontSize(11), TextColor(0, 0, 55, 1))
		Label("builds run ahead · tests run one-by-one · double-click for log / re-run",
			FontSize(10), TextColor(0, 0, 50, 1))

		Container(Attrs(Grow(1), Expand, Clip, Gap(4)), func() {
			s.mu.Lock()
			n := len(s.tests)
			s.mu.Unlock()
			for i := 0; i < n; i++ {
				s.row(i)
			}
		})
	})

	if detail >= 0 {
		s.detailModal(detail)
	}

	if isRunning {
		RequestNextFrame()
	}
}

func (s *appState) row(i int) {
	s.mu.Lock()
	it := s.tests[i]
	sel := s.selected == i
	s.mu.Unlock()

	bg := Vec4{0, 0, 100, 0}
	if sel {
		bg = Vec4{210, 30, 92, 1}
	}

	ContainerWithKey(it.Name, Attrs(Row, CrossMid, Gap(10), Pad2(8, 12), Expand, Corners(6), BackgroundVec(bg)), func() {
		if IsHovered() && !sel {
			ModAttrs(Background(210, 20, 94, 1))
		}
		if PressAction() {
			s.mu.Lock()
			s.selected = i
			s.mu.Unlock()
		}
		if IsDoubleClicked() {
			s.mu.Lock()
			s.selected = i
			s.detailIdx = i
			s.mu.Unlock()
		}

		statusLabel(it.Status)
		Label(it.Name, FontSize(14), FontWeight(WeightSemibold), TextColor(0, 0, 18, 1))
		Filler(1)
		if it.Duration > 0 {
			Label(fmtDuration(it.Duration), FontSize(11), TextColor(0, 0, 45, 1), Fonts(Monospace...))
		}
	})
}

func statusLabel(st status) {
	var text string
	var h, sat, light f32
	switch st {
	case statusIdle:
		text, h, sat, light = "idle", 0, 0, 55
	case statusQueued:
		text, h, sat, light = "queued", 45, 60, 40
	case statusBuilding:
		text, h, sat, light = "building", 280, 45, 42
	case statusBuilt:
		text, h, sat, light = "ready", 170, 40, 38
	case statusRunning:
		text, h, sat, light = "running", 210, 55, 40
	case statusPass:
		text, h, sat, light = "pass", 140, 55, 35
	case statusFail:
		text, h, sat, light = "fail", 8, 70, 42
	default:
		text, h, sat, light = "?", 0, 0, 40
	}
	Container(Attrs(Pad2(3, 8), Corners(4), Background(h, sat, light, 0.9), FixWidth(78), Center), func() {
		Label(text, FontSize(11), FontWeight(WeightBold), TextColor(0, 0, 98, 1))
	})
}

func (s *appState) detailModal(idx int) {
	s.mu.Lock()
	if idx < 0 || idx >= len(s.tests) {
		s.mu.Unlock()
		return
	}
	it := s.tests[idx]
	busy := s.running
	s.mu.Unlock()

	Modal(520, func() {
		s.mu.Lock()
		s.detailIdx = -1
		s.mu.Unlock()
	}, func() {
		Label(it.Name, FontSize(16), FontWeight(WeightBold))
		Container(Attrs(Row, CrossMid, Gap(8)), func() {
			statusLabel(it.Status)
			if it.Duration > 0 {
				Label(fmtDuration(it.Duration), FontSize(12), TextColor(0, 0, 45, 1))
			}
		})

		log := it.Log
		if log == "" {
			log = "(no output yet)"
		}
		Container(Attrs(MaxHeight(280), Expand, Clip, Pad(8), Gap(2),
			Background(0, 0, 98, 1), Corners(6), BorderWidth(1), BorderColor(0, 0, 85, 1)), func() {
			// Cap painted lines so huge logs stay usable.
			const maxLines = 200
			lines := splitLines(log)
			if len(lines) > maxLines {
				Label(fmt.Sprintf("… %d earlier lines omitted …", len(lines)-maxLines),
					FontSize(10), TextColor(0, 0, 50, 1), Fonts(Monospace...))
				lines = lines[len(lines)-maxLines:]
			}
			for _, line := range lines {
				Label(line, FontSize(11), TextColor(0, 0, 25, 1), Fonts(Monospace...))
			}
		})

		Label("re-run", FontSize(11), TextColor(0, 0, 50, 1))
		Container(Attrs(Row, Gap(8), Wrap), func() {
			disabled := busy
			if ButtonExt("Auto + close", ButtonAttrs{Accent: AccentMeadow, Disabled: disabled}, DefaultButtonLook()) && !disabled {
				s.mu.Lock()
				s.detailIdx = -1
				s.mu.Unlock()
				s.startSingle(idx, []string{"--window", "--drive", "--close"})
			}
			if ButtonExt("Auto + stay", ButtonAttrs{Disabled: disabled}, DefaultButtonLook()) && !disabled {
				s.mu.Lock()
				s.detailIdx = -1
				s.mu.Unlock()
				s.startSingle(idx, []string{"--window", "--drive"})
			}
			if ButtonExt("Manual", ButtonAttrs{Disabled: disabled}, DefaultButtonLook()) && !disabled {
				s.mu.Lock()
				s.detailIdx = -1
				s.mu.Unlock()
				s.startSingle(idx, []string{"--window"})
			}
		})
	})
}

func fmtDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return fmt.Sprintf("%.1fs", d.Seconds())
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}
