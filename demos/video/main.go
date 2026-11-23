// Video demo: play an embedded sample video (or a path passed on the command
// line) through shirei's GPU compositor.
//
//	go run ./demos/video
//	go run ./demos/video /path/to/movie.mp4
//
// macOS and iOS decode with AVFoundation, Android decodes with MediaPlayer +
// SurfaceTexture, Windows decodes with Media Foundation (or libav with
// -tags libav), and Linux decodes with GStreamer 1.0 or ffmpeg (or libav with
// -tags libav). Desktop platforms and iOS also support BSD-permissive WebM/MKV/MP4
// codecs (AV1/VP9/VP8 + Opus/Vorbis).
package main

import (
	_ "embed"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"time"

	g "go.hasen.dev/generic"
	"go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
	"go.hasen.dev/shirei/media"
	. "go.hasen.dev/shirei/widgets"
)

//go:embed sample.mp4
var embeddedSample []byte

var (
	player    *media.Player
	videoPath string
	started   bool

	scrub            float32
	scrubbing        bool
	resumeAfterScrub bool
	lastScrubSeek    time.Time
	volume           float32 = 1
)

func main() {
	float := flag.Bool("float", false, "keep the window above other applications")
	flag.Parse()

	videoPath = flag.Arg(0)
	if videoPath == "" {
		var err error
		videoPath, err = writeEmbeddedSample()
		if err != nil {
			fmt.Fprintf(os.Stderr, "media: %v\n", err)
			os.Exit(2)
		}
	}

	p, err := media.Open(videoPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "media: %v\n", err)
		os.Exit(1)
	}
	p.SetLoop(true)
	player = p
	g.AddExitCleanup(p.Close)

	app.SetupWindow("Shirei Video", 1100, 760)
	if *float {
		app.SetupOverlay()
	}
	app.Run(RootView)
}

func writeEmbeddedSample() (string, error) {
	dir, err := os.UserCacheDir()
	if err != nil || dir == "" {
		dir = os.TempDir()
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		dir = os.TempDir()
	}
	dst := filepath.Join(dir, "shirei-video-sample.mp4")
	if info, err := os.Stat(dst); err == nil && info.Size() == int64(len(embeddedSample)) {
		return dst, nil
	}
	if err := os.WriteFile(dst, embeddedSample, 0o644); err != nil {
		return "", err
	}
	return dst, nil
}

func RootView() {
	if !started {
		started = true
		activate()
		player.Play()
	}

	handleShortcuts()
	pos := player.Position()
	dur := player.Duration()
	w, h := player.Size()

	Container(Attrs(Viewport, Background(0, 0, 3, 1), Clip), func() {
		box := GetAvailableSize()

		Container(Attrs(Expand, Grow(1), Extrinsic, Center), func() {
			player.View(box)
		})

		// Overlay UI drawn after the video: same surface list, same pass,
		// so translucent controls hover cleanly above the external texture.
		if box[0] > 0 && box[1] > 0 {
			Container(Attrs(Float(0, 0), FixSize(box[0], box[1]), Pad(14), NoAnimate, ClickThrough), func() {
				Container(Attrs(Row, CrossMid, Gap(10), Expand, ClickThrough), func() {
					Container(Attrs(Row, CrossMid, Gap(10), Pad2(6, 12), Background(220, 12, 6, 0.64), Corners(8), BorderWidth(1), BorderColor(0, 0, 100, 0.10), NoAnimate), func() {
						Label(filepath.Base(videoPath), FontSize(13), FontWeight(WeightSemibold), TextColor(0, 0, 96, 1))
						if w > 0 && h > 0 {
							Label(fmt.Sprintf("%d×%d", w, h), FontSize(11), TextColor(0, 0, 72, 0.9))
						}
						if err := player.Err(); err != nil {
							Label(err.Error(), FontSize(11), TextColor(20, 70, 65, 1))
						} else if w == 0 {
							Label("loading…", FontSize(11), TextColor(0, 0, 72, 0.9))
						}
					})
				})

				Container(Attrs(Grow(1), ClickThrough), func() {})

				showVolume := box[0] >= 520
				barGap := float32(10)
				if !showVolume {
					barGap = 8
				}
				Container(Attrs(Row, CrossMid, Gap(barGap), Expand, Pad2(8, 12), Background(220, 12, 6, 0.68), Corners(10), BorderWidth(1), BorderColor(0, 0, 100, 0.12), NoAnimate, NoClickThrough), func() {
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
					if flatIconButton(SymRefresh, "Restart") {
						player.Seek(0)
						player.Play()
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

func flatIconButton(icon IconGlyph, accessLabel string) bool {
	var clicked bool
	Container(Attrs(FixSize(32, 32), Center, Corners(7), NoAnimate), func() {
		st := ProcessButtonEvents(false)
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
	if GetInputState().Modifiers&(ModCmd|ModCtrl|ModAlt) != 0 {
		return
	}
	key := GetFrameInput().Key
	switch key {
	case KeyK, KeySpace:
		if player.Playing() {
			player.Pause()
		} else {
			player.Play()
		}
	case KeyM:
		player.SetMuted(!player.Muted())
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
	}
}

func translucentSlider(val *float32, minV, maxV, step, width float32, disabled bool, accessLabel string) SliderState {
	const (
		radius    float32 = 6
		focusPad  float32 = 4
		barHeight float32 = 4
	)
	height := (radius + focusPad) * 2
	var st SliderState

	Container(Attrs(Row, CrossMid, FixWidth(width), FixHeight(height), NoAnimate), func() {
		st = ProcessSlider(val, SliderConfig{
			Min: minV, Max: maxV, Step: step,
			Width: width, HandleInset: radius + focusPad,
			Disabled: disabled,
		})
		NextAccessRole("slider")
		NextAccessLabel(accessLabel)
		NextAccessValue(fmt.Sprintf("%.2f", *val))
		NextAccessRange(*val, minV, maxV, step)
		AssignAccess()

		trackAlpha := float32(0.68)
		handleAlpha := float32(0.86)
		remAlpha := float32(0.14)
		if disabled {
			trackAlpha = 0.25
			handleAlpha = 0.35
			remAlpha = 0.08
		} else if st.Active || st.Hovered {
			trackAlpha = 0.90
			handleAlpha = 1.0
			remAlpha = 0.20
		}

		Element(Attrs(CrossMid, FixSize(width, barHeight), Background(0, 0, 100, remAlpha), Corners(barHeight/2)))
		Element(Attrs(Float(0, (height-barHeight)/2), FixSize(st.HandleX+radius+focusPad, barHeight), Background(0, 0, 100, trackAlpha), Corners(barHeight/2), ClickThrough))
		Container(Attrs(Float(st.HandleX+focusPad, focusPad), FixSize(radius*2, radius*2), NoClip, ClickThrough), func() {
			Element(Attrs(FixSize(radius*2, radius*2), Corners(radius), Background(0, 0, 100, handleAlpha), BorderWidth(1), BorderColor(0, 0, 0, 0.25), ClickThrough))
		})
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
