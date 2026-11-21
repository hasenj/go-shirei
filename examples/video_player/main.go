// Video player example: play local video files through shirei's GPU compositor
// with hovering translucent controls and a file-open dialog.
//
//	go run .
//	go run . /path/to/movie.mp4
//
// macOS and iOS decode with AVFoundation, Android decodes with MediaPlayer +
// SurfaceTexture, Windows decodes with Media Foundation (or libav with
// -tags libav), and Linux decodes with GStreamer 1.0 or ffmpeg (or libav with
// -tags libav). Desktop platforms and iOS also support WebM/MKV/MP4 with
// AV1/VP9/VP8 + Opus/Vorbis via BSD permissive codecs.
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	g "go.hasen.dev/generic"
	"go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
	"go.hasen.dev/shirei/media"
	. "go.hasen.dev/shirei/widgets"
)

var videoExtensions = []string{"mp4", "webm", "mkv", "mov", "m4v", "avi"}

var (
	player      *media.Player
	videoPath   string
	loadingPath string
	openErr     error
	openSeq     uint64
	started     bool

	scrub            float32
	scrubbing        bool
	resumeAfterScrub bool
	lastScrubSeek    time.Time
	volume           float32 = 1

	openDialogOn bool
	darkModalUI  = DarkColorScheme()
)

func main() {
	floatWin := flag.Bool("float", false, "keep the window above other applications")
	flag.Parse()

	if initial := flag.Arg(0); initial != "" {
		if err := openVideoInitial(initial); err != nil {
			fmt.Fprintf(os.Stderr, "media: %v\n", err)
			os.Exit(1)
		}
	}
	g.AddExitCleanup(func() {
		var p *media.Player
		WithFrameLock(func() {
			p = player
			player = nil
		})
		if p != nil {
			p.Close()
		}
	})

	app.SetupWindow("Shirei Video Player", 1100, 760)
	app.SetupIconBytes(iconPNG)
	if *floatWin {
		app.SetupOverlay()
	}
	app.Run(RootView)
}

func openVideoInitial(path string) error {
	p, err := media.Open(path)
	if err != nil {
		return err
	}
	p.SetLoop(true)
	p.SetVolume(volume)
	player = p
	videoPath = path
	return nil
}

// requestOpenVideo opens path off the frame thread and closes the previous
// player outside the frame lock (Player.Close acquires WithFrameLock).
func requestOpenVideo(path string) {
	openSeq++
	seq := openSeq
	loadingPath = path
	openErr = nil

	muted := false
	vol := volume
	if player != nil {
		muted = player.Muted()
		vol = player.Volume()
		player.Pause()
	}

	go func() {
		p, err := media.Open(path)
		if err != nil {
			WithFrameLock(func() {
				if seq == openSeq {
					openErr = err
					loadingPath = ""
				}
			})
			RequestNextFrame()
			return
		}
		p.SetLoop(true)
		p.SetMuted(muted)
		p.SetVolume(vol)

		var old *media.Player
		var stale bool
		WithFrameLock(func() {
			if seq != openSeq {
				stale = true
				return
			}
			old = player
			player = p
			videoPath = path
			loadingPath = ""
			openErr = nil
			scrub = 0
			scrubbing = false
			resumeAfterScrub = false
		})
		if stale {
			p.Close()
			return
		}
		if old != nil {
			old.Close()
		}
		p.Play()
		RequestNextFrame()
	}()
}

func openFileDialog() {
	openDialogOn = true
}

func defaultBrowseDir() string {
	if videoPath != "" && !strings.HasPrefix(videoPath, "http://") && !strings.HasPrefix(videoPath, "https://") {
		dir := filepath.Dir(videoPath)
		if dir != "" && dir != "." {
			if st, err := os.Stat(dir); err == nil && st.IsDir() {
				return dir
			}
		}
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		for _, sub := range []string{"Movies", "Videos", "Downloads"} {
			cand := filepath.Join(home, sub)
			if st, err := os.Stat(cand); err == nil && st.IsDir() {
				return cand
			}
		}
		return home
	}
	return "/"
}

func RootView() {
	if !started {
		started = true
		activate()
		if player != nil {
			player.Play()
		}
	}

	var pos, dur time.Duration
	var w, h int
	if player != nil {
		pos = player.Position()
		dur = player.Duration()
		w, h = player.Size()
	}

	Container(Attrs(Viewport, Background(0, 0, 3, 1), Clip), func() {
		box := GetAvailableSize()

		Container(Attrs(Expand, Grow(1), Extrinsic, Center), func() {
			if player != nil {
				player.View(box)
			} else {
				emptyStateView()
			}
		})

		// Overlay UI drawn after the video: same surface list, same pass,
		// so translucent controls hover cleanly above the external texture.
		if player != nil && box[0] > 0 && box[1] > 0 {
			Container(Attrs(Float(0, 0), FixSize(box[0], box[1]), NoAnimate, ClickThrough), func() {
				Container(Attrs(Row, CrossMid, Gap(8), Expand, Pad4(12, 12, 0, 12), ClickThrough), func() {
					maxPillW := box[0] - 24
					if maxPillW < 180 {
						maxPillW = 180
					}
					textMaxW := maxPillW - 66
					if textMaxW < 100 {
						textMaxW = 100
					}
					Container(Attrs(Row, CrossMid, Gap(10), Pad2(6, 12), MaxWidth(maxPillW), Background(220, 12, 6, 0.64), Corners(8), BorderWidth(1), BorderColor(0, 0, 100, 0.10), NoAnimate, NoClickThrough), func() {
						if flatIconButton(SymFolder, "Open video") {
							openFileDialog()
						}
						Container(Attrs(Gap(2), MaxWidth(textMaxW), Clip), func() {
							Container(Attrs(Row, CrossMid, Gap(8)), func() {
								Label(filepath.Base(videoPath), FontSize(13), FontWeight(WeightSemibold), TextColor(0, 0, 96, 1))
								if w > 0 && h > 0 {
									Label(fmt.Sprintf("%d×%d", w, h), FontSize(11), TextColor(0, 0, 72, 0.9))
								}
								if openErr != nil {
									Label(openErr.Error(), FontSize(11), TextColor(20, 70, 65, 1))
								} else if loadingPath != "" {
									Label("opening "+filepath.Base(loadingPath)+"…", FontSize(11), TextColor(0, 0, 72, 0.9))
								} else if player != nil {
									if err := player.Err(); err != nil {
										Label(err.Error(), FontSize(11), TextColor(20, 70, 65, 1))
									} else if w == 0 {
										Label("loading…", FontSize(11), TextColor(0, 0, 72, 0.9))
									}
								}
							})
							if videoPath != "" {
								Label(videoPath, FontSize(11), TextColor(0, 0, 70, 0.85))
							}
						})
					})
				})

				Container(Attrs(Grow(1), ClickThrough), func() {})

				if player != nil {
					showVolume := box[0] >= 520
					barGap := float32(10)
					if !showVolume {
						barGap = 8
					}
					Container(Attrs(Expand, Background(220, 12, 6, 0.72), NoAnimate, NoClickThrough), func() {
						Element(Attrs(Expand, FixHeight(1), Background(0, 0, 100, 0.12)))
						Container(Attrs(Row, CrossMid, Gap(barGap), Expand, Pad2(8, 12)), func() {
							icon, label := SymPlay, "Play"
							if player.Playing() {
								icon, label = SymPause, "Pause"
							}
							if flatIconButton(icon, label) {
								if player.Playing() {
									player.Pause()
								} else {
									player.Play()
								}
							}
							Container(Attrs(Row, CrossMid, Grow(1), Extrinsic, FixHeight(20)), func() {
								seekWidth := GetAvailableSize()[0]
								if seekWidth < 40 {
									seekWidth = 40
								}
								seekBar(dur, seekWidth)
							})
							Container(Attrs(Row, CrossMid, MainAlign(AlignEnd), FixWidth(78)), func() {
								Label(fmt.Sprintf("%s / %s", shortDuration(pos), shortDuration(dur)), FontSize(12), TextColor(0, 0, 88, 0.92))
							})
							icon, label = SymVolHigh, "Mute"
							if player.Muted() {
								icon, label = SymVolMute, "Unmute"
							}
							if flatIconButton(icon, label) {
								player.SetMuted(!player.Muted())
							}
							if showVolume {
								volume = player.Volume()
								prevVol := volume
								translucentSlider(&volume, 0, 1, 0.05, 88, false, "Volume")
								if volume != prevVol {
									player.SetVolume(volume)
									if volume > 0 && player.Muted() {
										player.SetMuted(false)
									}
								}
							}
						})
					})
				}
			})
		}

		// Run global shortcuts after controls so focused buttons/sliders get
		// first crack at Space/Enter/arrows and consume them before we run.
		handleShortcuts()

		fileOpenModal(box[0])
	})
}

func fileOpenModal(viewportW float32) {
	width := float32(600)
	if viewportW > 0 && viewportW-40 < width {
		width = max(280, viewportW-40)
	}
	var picked string
	if FileBrowserModalStyled(&openDialogOn, &picked, FileBrowserAttrs{
		Title: "Open Video",
		Width: width,
		Start: defaultBrowseDir(),
		Files: true,
		Exts:  videoExtensions,
	}, darkModalUI) && picked != "" {
		requestOpenVideo(picked)
	}
}

func emptyStateView() {
	shortcut := "Ctrl+O"
	if runtime.GOOS == "darwin" {
		shortcut = "⌘O"
	}
	Container(Attrs(CrossMid, Gap(14), Pad(24), NoAnimate), func() {
		Icon(SymPlay, FontSize(36), TextColor(0, 0, 100, 0.25))
		if loadingPath != "" {
			Label("Opening "+filepath.Base(loadingPath)+"…", FontSize(14), TextColor(0, 0, 82, 0.9))
		} else {
			Label("No video open", FontSize(15), FontWeight(WeightSemibold), TextColor(0, 0, 88, 0.92))
		}
		if openErr != nil {
			Label(openErr.Error(), FontSize(12), TextColor(20, 70, 65, 1))
		}
		if flatTextButton(SymFolder, "Open Video…") {
			openFileDialog()
		}
		Label("or press "+shortcut, FontSize(12), TextColor(0, 0, 65, 0.7))
	})
}

func flatTextButton(icon IconGlyph, text string) bool {
	var clicked bool
	Container(Attrs(Row, CrossMid, Gap(8), Pad2(8, 16), Corners(8), NoAnimate), func() {
		st := ProcessButtonEvents(false)
		if GetFrameInput().Mouse.Has(MouseClick) && st.Hovered {
			ClearFocus()
			st.HasFocus = false
			st.FocusVisible = false
		}
		if st.HasFocus && (GetFrameInput().Key == KeySpace || GetFrameInput().Key == KeyEnter) {
			GetFrameInput().Key = KeyCodeNone
		}
		NextAccessRole("button")
		NextAccessLabel(text)
		AssignAccess()
		clicked = st.Clicked

		bgAlpha := float32(0.10)
		borderAlpha := float32(0.14)
		fgAlpha := float32(0.92)
		switch {
		case st.Active:
			bgAlpha = 0.24
			borderAlpha = 0.22
			fgAlpha = 1
		case st.Hovered:
			bgAlpha = 0.17
			borderAlpha = 0.18
			fgAlpha = 1
		}
		ModAttrs(Background(0, 0, 100, bgAlpha), BorderWidth(1), BorderColor(0, 0, 100, borderAlpha))
		if st.FocusVisible {
			ModAttrs(BorderWidth(1.5), BorderColor(215, 85, 68, 0.85))
		}
		Icon(icon, FontSize(14), TextColor(0, 0, 100, fgAlpha))
		Label(text, FontSize(13), FontWeight(WeightSemibold), TextColor(0, 0, 100, fgAlpha))
	})
	return clicked
}

func flatIconButton(icon IconGlyph, accessLabel string) bool {
	var clicked bool
	Container(Attrs(FixSize(32, 32), Center, Corners(7), NoAnimate), func() {
		st := ProcessButtonEvents(false)
		if GetFrameInput().Mouse.Has(MouseClick) && st.Hovered {
			ClearFocus()
			st.HasFocus = false
			st.FocusVisible = false
		}
		if st.HasFocus && (GetFrameInput().Key == KeySpace || GetFrameInput().Key == KeyEnter) {
			GetFrameInput().Key = KeyCodeNone
		}
		NextAccessRole("button")
		NextAccessLabel(accessLabel)
		AssignAccess()
		clicked = st.Clicked

		bgAlpha := float32(0.08)
		borderAlpha := float32(0.08)
		fgAlpha := float32(0.90)
		switch {
		case st.Active:
			bgAlpha = 0.24
			borderAlpha = 0.18
			fgAlpha = 1
		case st.Hovered:
			bgAlpha = 0.16
			borderAlpha = 0.14
			fgAlpha = 1
		}
		ModAttrs(Background(0, 0, 100, bgAlpha), BorderWidth(1), BorderColor(0, 0, 100, borderAlpha))
		if st.FocusVisible {
			ModAttrs(BorderWidth(1.5), BorderColor(215, 85, 68, 0.85))
		}
		Icon(icon, FontSize(14), TextColor(0, 0, 100, fgAlpha))
	})
	return clicked
}

func handleShortcuts() {
	key := GetFrameInput().Key
	mods := GetInputState().Modifiers
	if key == KeyO && mods&(ModCmd|ModCtrl) != 0 {
		if openDialogOn {
			openDialogOn = false
		} else {
			openFileDialog()
		}
		return
	}
	if openDialogOn || player == nil {
		return
	}
	switch key {
	case KeyEscape:
		if FocusedId() != nil {
			ClearFocus()
			RequestNextFrame()
		}
	case KeyK, KeySpace:
		if player.Playing() {
			player.Pause()
		} else {
			player.Play()
		}
		RequestNextFrame()
	case KeyM:
		player.SetMuted(!player.Muted())
		RequestNextFrame()
	case KeyJ, KeyL, KeyLeft, KeyRight:
		step := float64(5)
		if key == KeyJ || key == KeyLeft {
			step = -step
		}
		target := player.Position().Seconds() + step
		if target < 0 {
			target = 0
		}
		if dur := player.Duration().Seconds(); dur > 0 && target > dur {
			target = dur
		}
		player.Seek(target)
		RequestNextFrame()
	}
}

func seekBar(dur time.Duration, width float32) {
	durSec := float32(dur.Seconds())
	if durSec <= 0 {
		durSec = 1
	}
	if !scrubbing {
		scrub = float32(player.Position().Seconds())
	}
	if scrub < 0 {
		scrub = 0
	} else if scrub > durSec {
		scrub = durSec
	}

	prevScrub := scrub
	st := translucentSlider(&scrub, 0, durSec, 0, width, dur <= 0, "Seek")

	switch {
	case st.Active && !scrubbing:
		scrubbing = true
		resumeAfterScrub = player.Playing()
		player.Pause()
		player.Seek(float64(scrub))
		lastScrubSeek = time.Now()
	case st.Active && scrubbing && scrub != prevScrub && time.Since(lastScrubSeek) >= 80*time.Millisecond:
		player.Seek(float64(scrub))
		lastScrubSeek = time.Now()
	case !st.Active && scrubbing:
		scrubbing = false
		player.Seek(float64(scrub))
		if resumeAfterScrub {
			player.Play()
		}
		resumeAfterScrub = false
	case !scrubbing && st.HasFocus && scrub != prevScrub:
		player.Seek(float64(scrub))
	}
}

func translucentSlider(val *float32, minV, maxV, step, width float32, disabled bool, accessLabel string) SliderState {
	const (
		height float32 = 20
		inset  float32 = 3
		thumbW float32 = 2.5
		thumbH float32 = 11
	)
	var st SliderState

	Container(Attrs(Row, CrossMid, FixWidth(width), FixHeight(height), NoAnimate), func() {
		st = ProcessSlider(val, SliderConfig{
			Min: minV, Max: maxV, Step: step,
			Width: width, HandleInset: inset,
			Disabled: disabled,
		})
		if GetFrameInput().Mouse.Has(MouseClick) && st.Hovered {
			ClearFocus()
			st.HasFocus = false
			st.FocusVisible = false
		}
		if st.HasFocus {
			switch GetFrameInput().Key {
			case KeyLeft, KeyRight, KeyUp, KeyDown, KeyHome, KeyEnd:
				GetFrameInput().Key = KeyCodeNone
			}
		}
		NextAccessRole("slider")
		NextAccessLabel(accessLabel)
		NextAccessValue(fmt.Sprintf("%.2f", *val))
		NextAccessRange(*val, minV, maxV, step)
		AssignAccess()

		barHeight := float32(2)
		trackAlpha := float32(0.72)
		handleAlpha := float32(0.92)
		remAlpha := float32(0.16)
		if disabled {
			trackAlpha = 0.25
			handleAlpha = 0.35
			remAlpha = 0.08
		} else if st.Active || st.Hovered {
			barHeight = 3
			trackAlpha = 0.92
			handleAlpha = 1.0
			remAlpha = 0.24
		}

		cx := st.HandleX + inset
		Element(Attrs(CrossMid, FixSize(width, barHeight), Background(0, 0, 100, remAlpha), Corners(barHeight/2)))
		Element(Attrs(Float(0, (height-barHeight)/2), FixSize(cx, barHeight), Background(0, 0, 100, trackAlpha), Corners(barHeight/2), ClickThrough))
		Element(Attrs(Float(cx-thumbW/2, (height-thumbH)/2), FixSize(thumbW, thumbH), Corners(thumbW/2), Background(0, 0, 100, handleAlpha), ClickThrough))
	})
	return st
}

func shortDuration(d time.Duration) string {
	if d <= 0 {
		return "0:00"
	}
	d = d.Round(time.Second)
	m := int(d / time.Minute)
	s := int((d % time.Minute) / time.Second)
	return fmt.Sprintf("%d:%02d", m, s)
}
