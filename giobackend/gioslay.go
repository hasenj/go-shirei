package giobackend

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	"image"
	"image/color"

	"gioui.org/app"
	"gioui.org/f32"
	"gioui.org/io/clipboard"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/io/transfer"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/unit"
	"github.com/dboslee/lru"
	fonts "github.com/go-text/typesetting/font"
	ot "github.com/go-text/typesetting/font/opentype"
	"go.hasen.dev/generic"
	"go.hasen.dev/slay"
	"go.hasen.dev/slay/widgets"
)

var window *app.Window

func SetupWindow(title string, width int, height int) {
	window = new(app.Window)
	window.Option(app.Title(title))
	window.Option(app.Size(unit.Dp(width), unit.Dp(height)))
}

var frameMacro op.CallOp
var frameWasRequested = true

func Run(frameFn slay.FrameFn) {
	slay.InitFontSubsystem()
	widgets.UseMicronFont()
	widgets.UseTypiconsFont()

	// hard limit fps so we don't eat up cpu resources during mouse movements, resize, etc
	const fps = 60
	const syncMS = 1000 / fps
	frameTicker := time.NewTicker(time.Millisecond * syncMS)

	// force at least one frame per second ... for reasons!
	slowTicker := time.NewTicker(time.Second)
	go func() {
		for range slowTicker.C {
			window.Invalidate()
		}
	}()

	var lastEventTime time.Time

	var tag = new(int) // just a thing that gio events can attach to
	go func() {
		for {
			switch e := window.Event().(type) {
			case app.DestroyEvent:
				os.Exit(0)
			case app.FrameEvent:
				// force waiting for frame time
				<-frameTicker.C

				var now = time.Now()

				frameEventStart := now
				dpi = e.Metric.PxPerDp
				ctx := app.NewContext(new(op.Ops), e)
				slay.WindowSize = slay.Vec2Mul(imgVec2(e.Size), 1/ctx.Metric.PxPerDp)

				// to not receive events about mouse movement outside window
				clip.Rect{
					Max: e.Size,
				}.Push(ctx.Ops)

				ctx.Execute(key.FocusCmd{Tag: tag})
				ctx.Execute(key.SelectionCmd{
					Tag: tag,
					Caret: key.Caret{
						// arbitrary value!
						Pos:     f32Point(slay.CaretPos),
						Ascent:  20,
						Descent: 10,
					},
				})
				event.Op(ctx.Ops, tag)

				// read tagged events
				var eventCount int
				for {
					// tried to read all input events!
					e, ok := ctx.Event(
						pointer.Filter{
							Target:  tag,
							Kinds:   pointer.Press | pointer.Release | pointer.Move | pointer.Scroll | pointer.Drag,
							ScrollX: pointer.ScrollRange{Min: -100, Max: 100},
							ScrollY: pointer.ScrollRange{Min: -100, Max: 100},
						},
						key.Filter{
							Focus:    tag,
							Optional: key.ModSuper | key.ModAlt | key.ModCommand | key.ModShift | key.ModCtrl,
						},
						// receiving tab key requires a special additional filter!
						key.Filter{
							Focus:    tag,
							Optional: key.ModSuper | key.ModAlt | key.ModCommand | key.ModShift | key.ModCtrl,
							Name:     key.NameTab,
						},
						key.FocusFilter{
							Target: tag,
						},
						transfer.TargetFilter{
							Target: tag,
							Type:   "application/text",
						},
					)
					if !ok {
						break
					}
					lastEventTime = now
					eventCount++
					switch e := e.(type) {
					case pointer.Event:
						// fmt.Println("mouse event!", e)
						prevMousePoint := slay.InputState.MousePoint
						slay.InputState.MousePoint = slay.Vec2Mul(f32Vec2(e.Position), 1/ctx.Metric.PxPerDp)
						slay.InputState.MouseButton = slay.MouseButton(e.Buttons) // we try to keep the same values
						slay.FrameInput.Motion = slay.Vec2Add(slay.FrameInput.Motion, slay.Vec2Sub(slay.InputState.MousePoint, prevMousePoint))
						slay.FrameInput.Scroll = f32Vec2(e.Scroll)
						switch e.Kind {
						case pointer.Press:
							slay.FrameInput.Mouse = slay.MouseClick
						case pointer.Release:
							slay.FrameInput.Mouse = slay.MouseRelease
						}
					case key.Event:
						// fmt.Println("key event!", e)
						slay.InputState.Modifiers = slay.Modifiers(e.Modifiers)
						keyCode := mapKeyCode(e.Name)

						if e.State == key.Press {
							// fmt.Println("Key:", e.Name)
							slay.FrameInput.Key = keyCode
						}
						if keyCode != 0 {
							switch e.State {
							case key.Press:
								generic.SliceAddUniq(&slay.InputState.DownKeys, keyCode)
							case key.Release:
								generic.SliceRemove(&slay.InputState.DownKeys, keyCode)
							}
						}
					case transfer.DataEvent:
						if e.Type == "application/text" {
							// assume it's a paste event
							// TODO should we also check that we are waiting for a paste event?
							// I mean, we might need to distinguish between paste and extrnal drag and drop?
							f := e.Open()
							pasteData, _ := io.ReadAll(f)
							f.Close()
							slay.FrameInput.Text = string(pasteData)
						}

					case key.FocusEvent:
						// fmt.Printf("Focus event: %#v\n", e)
					case key.EditEvent:
						slay.FrameInput.Text = e.Text
						// fmt.Printf("Edit: %#v\n", e)
					case key.SnippetEvent:
						// fmt.Printf("Snippet: %#v\n", e)
					case key.SelectionEvent:
						// fmt.Printf("Selection %#v\n", e)
					default:
						fmt.Printf("unhandled %#v\n", e)
					}
				}

				frameData := slay.RunFrameFn(frameFn)

				// renderStart := time.Now()

				if frameData.FrameHasChanges {
					frameMacro = renderSurfaces(frameData.Surfaces)
				}

				frameMacro.Add(ctx.Ops)
				e.Frame(ctx.Ops)
				// renderDur := time.Since(renderStart)

				if frameData.Copy != "" {
					e.Source.Execute(clipboard.WriteCmd{
						Type: "application/text",
						Data: io.NopCloser(strings.NewReader(frameData.Copy)),
					})
				}
				if frameData.Paste {
					e.Source.Execute(clipboard.ReadCmd{Tag: tag})
				}

				slay.TotalFrameTime = time.Since(frameEventStart)
				// fmt.Printf("Layout Time: %v, Render Time: %v, Total Frame Time: %v, frameHasChanges?: %v   :::::\r", slay.LayoutTime, renderDur, slay.TotalFrameTime, frameData.FrameHasChanges)

				if frameData.NextFrameRequested || time.Since(lastEventTime) < time.Second {
					window.Invalidate()
				}
			}
		}
	}()
	app.Main()
}

func imgPoint(v slay.Vec2) image.Point {
	return image.Point{
		X: int(v[0]),
		Y: int(v[1]),
	}
}

func imgPointMul(p image.Point, f int) image.Point {
	return image.Point{
		X: p.X * f,
		Y: p.Y * f,
	}
}

func f32Point(v slay.Vec2) f32.Point {
	return f32.Pt(v[0], v[1])
}

func f32Vec2(p f32.Point) slay.Vec2 {
	return slay.Vec2{p.X, p.Y}
}

func imgVec2(p image.Point) slay.Vec2 {
	return slay.Vec2{float32(p.X), float32(p.Y)}
}

func hsla_to_nrgba(c slay.Vec4) color.NRGBA {
	r, g, b := slay.FloatHSLToRGB(float64(c[0]/360), float64(c[1]/100), float64(c[2]/100))
	return color.NRGBA{
		R: uint8(r * 0xff),
		G: uint8(g * 0xff),
		B: uint8(b * 0xff),
		A: uint8(c[3] * 0xff),
	}
}

var dpi float32

func renderSurfaces(surfaces []slay.Surface) op.CallOp {
	ops := new(op.Ops)
	macro := op.Record(ops)

	// support hidpi
	op.Affine(f32.Affine2D{}.Scale(f32.Pt(0, 0), f32.Pt(dpi, dpi))).Add(ops)

	var stackStack []clip.Stack

	pushClipMask := func(rrect clip.RRect) {
		// fmt.Println("Pushing", rrect)
		s := rrect.Op(ops).Push(ops)
		stackStack = append(stackStack, s)
	}
	popClipMask := func() {
		// fmt.Println("Popping")
		if len(stackStack) == 0 {
			panic("surface rendering: uneven push/pop operation stack")
		}
		last := stackStack[len(stackStack)-1]
		last.Pop()
		stackStack = stackStack[:len(stackStack)-1]
	}

	var opacityStack []paint.OpacityStack
	pushOpacity := func(opacity float32) {
		s := paint.PushOpacity(ops, opacity)
		opacityStack = append(opacityStack, s)
	}
	popOpacity := func() {
		if len(opacityStack) == 0 {
			panic("surface rendering: unevent push/pop opacity stack")
		}
		last := opacityStack[len(opacityStack)-1]
		last.Pop()
		opacityStack = opacityStack[:len(opacityStack)-1]
	}

	for _, s := range surfaces {
		r := s.Rect
		grad := paint.LinearGradientOp{
			// NOTE: I'm not sure why we need the dpi multiplication here but oh well
			Stop1:  f32.Pt(r.Origin[0]*dpi, r.Origin[1]*dpi),
			Stop2:  f32.Pt(r.Origin[0]*dpi, (r.Origin[1]+s.Rect.Size[1])*dpi),
			Color1: hsla_to_nrgba(s.Color1),
			Color2: hsla_to_nrgba(s.Color2),
		}

		rectSize := s.Rect.Size
		rectOrigin := s.Rect.Origin
		corners := s.Corners

		if s.Transperancy > 0 {
			pushOpacity(1 - s.Transperancy)
		}

		// FIXME: clip rrect uses ints, but we should build the shape from float32 instead
		rrect := clip.RRect{
			Rect: image.Rectangle{
				Min: imgPoint(rectOrigin),
				Max: imgPoint(slay.Vec2Add(rectOrigin, rectSize)),
			},
			// css order: top-left, top-right, bottom-right, bottom-left
			NW: int(corners[0]),
			NE: int(corners[1]),
			SE: int(corners[2]),
			SW: int(corners[3]),
		}

		if s.Clip == slay.ClipPush {
			pushClipMask(rrect)
		}

		if s.FontId > 0 && s.GlyphId > 0 {
			// this is a character
			var affine f32.Affine2D
			face := slay.GetFace(s.FontId)
			sh := clip.Outline{
				Path: FontGlyphPathSpec(s.FontId, s.GlyphId),
			}.Op()

			// font quirks: position it relative to top left and fix direction
			affine = affine.Scale(f32.Pt(0, 0), f32.Pt(1, -1))
			// affine = affine.Offset(f32.Point{X: 0, Y: -face.Ascender})
			affine = affine.Offset(f32Point(s.GlyphOffset))

			scale := s.Rect.Size[1] * face.InvUPM

			// scale it to match rectangle height (width may leak outside)
			affine = affine.Scale(f32.Pt(0, 0), f32.Pt(scale, scale))
			affine = affine.Offset(f32Point(s.Rect.Origin))

			affine = affine.Offset(f32.Pt(0, s.Rect.Size[1]*0.82)) // place the baseline at 0.82 point of the height

			stack := op.Affine(affine).Push(ops)
			stack2 := sh.Push(ops)

			grad.Add(ops)
			paint.PaintOp{}.Add(ops)

			stack2.Pop()
			stack.Pop()
		} else if s.ImageId > 0 {
			img := slay.LookupImage(s.ImageId)
			// FIXME: we should cache this op or something ..
			imgOp := paint.NewImageOp(img)

			var affine f32.Affine2D

			// just like with glyphs, use the height as the deciding factor for scaling
			if s.ImageScale {
				var scale = s.Rect.Size[1] / float32(img.Bounds().Dy())
				affine = affine.Scale(f32.Pt(0, 0), f32.Pt(scale, scale))
			}

			affine = affine.Offset(f32Point(s.Rect.Origin))

			stack := op.Affine(affine).Push(ops)

			imgOp.Add(ops)
			paint.PaintOp{}.Add(ops)

			stack.Pop()
		} else {
			var sh clip.Op

			if s.Stroke == 0 {
				sh = rrect.Op(ops)
			} else {
				sh = clip.Stroke{Path: rrect.Path(ops), Width: s.Stroke}.Op()
			}

			// fmt.Println("Drawing", rrect)

			stack := sh.Push(ops)

			grad.Add(ops)
			paint.PaintOp{}.Add(ops)

			stack.Pop()
		}

		if s.PopTransperancy {
			popOpacity()
		}

		if s.Clip == slay.ClipPop {
			popClipMask()
		}
	}

	if len(stackStack) != 0 {
		panic(fmt.Sprintf("uneven clip stack %d", len(stackStack)))
	}

	return macro.Stop()
}

// -----------------------------------------------------------------------------
//      Text Rendering
// -----------------------------------------------------------------------------

type FontGlyphKey struct {
	FontId  slay.FontId
	GlyphId slay.GlyphId
}

var glyphCache = lru.New[FontGlyphKey, fonts.GlyphOutline]()

var glyphPathCache = lru.New[FontGlyphKey, clip.PathSpec]()

func FontGlyphPathSpec(fontId slay.FontId, glyphId slay.GlyphId) clip.PathSpec {
	key := FontGlyphKey{FontId: fontId, GlyphId: glyphId}
	cached, ok := glyphPathCache.Get(key)
	if ok {
		return cached
	} else {
		outline := slay.GlyphOutline(fontId, glyphId)
		ops := new(op.Ops)

		var path clip.Path
		path.Begin(ops)

		for _, segment := range outline.Segments {
			switch segment.Op {
			case ot.SegmentOpMoveTo:
				p0 := (f32.Point)(segment.Args[0])
				path.MoveTo(p0)
			case ot.SegmentOpLineTo:
				p0 := (f32.Point)(segment.Args[0])
				path.LineTo(p0)
			case ot.SegmentOpQuadTo:
				p0 := (f32.Point)(segment.Args[0])
				p1 := (f32.Point)(segment.Args[1])
				path.QuadTo(p0, p1)
			case ot.SegmentOpCubeTo:
				p0 := (f32.Point)(segment.Args[0])
				p1 := (f32.Point)(segment.Args[1])
				p2 := (f32.Point)(segment.Args[2])
				path.CubeTo(p0, p1, p2)
			}
		}

		pathSpec := path.End()

		// make sure the outline is not the default "empty" before we cache the
		// result!!
		if len(outline.Segments) > 0 {
			glyphPathCache.Set(key, pathSpec)
		}

		return pathSpec
	}
}

func mapKeyCode(name key.Name) slay.KeyCode {
	switch name {
	case key.NameLeftArrow:
		return slay.KeyLeft
	case key.NameRightArrow:
		return slay.KeyRight
	case key.NameUpArrow:
		return slay.KeyUp
	case key.NameDownArrow:
		return slay.KeyDown
	case key.NameReturn:
		return slay.KeyEnter
	case key.NameEnter:
		return slay.KeyEnter
	case key.NameEscape:
		return slay.KeyEscape
	case key.NameHome:
		return slay.KeyHome
	case key.NameEnd:
		return slay.KeyEnd
	case key.NameDeleteBackward:
		return slay.KeyDeleteBackward
	case key.NameDeleteForward:
		return slay.KeyDeleteForward
	case key.NamePageUp:
		return slay.KeyPageUp
	case key.NamePageDown:
		return slay.KeyPageDown
	case key.NameTab:
		return slay.KeyTab
	case key.NameSpace:
		return slay.KeySpace
	case key.NameCtrl:
		return slay.KeyCtrl
	case key.NameShift:
		return slay.KeyShift
	case key.NameAlt:
		return slay.KeyAlt
	case key.NameSuper:
		return slay.KeySuper
	case key.NameCommand:
		return slay.KeyCommand
	case key.NameF1:
		return slay.KeyF1
	case key.NameF2:
		return slay.KeyF2
	case key.NameF3:
		return slay.KeyF3
	case key.NameF4:
		return slay.KeyF4
	case key.NameF5:
		return slay.KeyF5
	case key.NameF6:
		return slay.KeyF6
	case key.NameF7:
		return slay.KeyF7
	case key.NameF8:
		return slay.KeyF8
	case key.NameF9:
		return slay.KeyF9
	case key.NameF10:
		return slay.KeyF10
	case key.NameF11:
		return slay.KeyF11
	case key.NameF12:
		return slay.KeyF12
	case key.NameBack:
		return slay.KeyBack
	}
	if utf8.RuneCountInString(string(name)) == 1 {
		r, _ := utf8.DecodeRuneInString(string(name))
		if r < 255 {
			return slay.KeyCode(r)
		}
	}
	return slay.KeyCodeNone
}
