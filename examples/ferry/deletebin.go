package main

// The deletion bin — remote delete is two-phase by design (hasen:
// deleting is dangerous). "Delete" only stages rows here: full paths,
// surviving navigation, nothing sent to the server. Binned rows stay in
// the listing, stamped red, until the user commits the bin through the
// confirm dialog and the rm actually runs. The bin is pure session
// state: leaving the server forgets it, and the UI warns in BOTH
// directions — that staged files are NOT deleted yet, and that the
// commit cannot be undone.

import (
	"image"
	"image/color"
	"math"
	"path"
	"sync"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

type BinItem struct {
	Path  string
	IsDir bool
}

// stageDelete moves the remote pane's selection into the active session's
// bin. Nothing touches the server here — that is the point.
func stageDelete(p *Pane) {
	s := appData.active
	if s == nil {
		return
	}
	if s.binned == nil {
		s.binned = map[string]bool{}
	}
	for _, r := range p.selection() {
		r.Selected = false
		full := path.Join(p.CWD, r.Name)
		if s.binned[full] {
			continue
		}
		s.binned[full] = true
		s.deleteBin = append(s.deleteBin, BinItem{Path: full, IsDir: r.IsDir})
	}
}

// unstageDelete builds a NEW slice — never filter in place: a Restore
// click fires in the middle of a frame that is rendering the bin, and
// the virtual list's closures hold the frame-start slice header. Reusing
// the backing array shifted rows under a live render and crashed the
// list's itemId lookup (found in the field, 2026-07-05).
func unstageDelete(s *Session, full string) {
	delete(s.binned, full)
	keep := make([]BinItem, 0, len(s.deleteBin))
	for _, it := range s.deleteBin {
		if it.Path != full {
			keep = append(keep, it)
		}
	}
	s.deleteBin = keep
}

// clearDeleteBin empties the active session's bin.
func clearDeleteBin() {
	if s := appData.active; s != nil {
		s.deleteBin = nil
		s.binned = nil
		s.deleteErr = nil
	}
	appData.deleteConfirm = false
}

// rowBinned: is this row of pane p staged for deletion in its session?
func rowBinned(p *Pane, r *FileRow) bool {
	s := appData.active
	if s == nil || p != s.Pane || len(s.deleteBin) == 0 {
		return false
	}
	return s.binned[path.Join(p.CWD, r.Name)]
}

// commitDeleteBin runs the active session's staged deletion; the confirm
// modal calls it with the frame lock held. On failure the bin stays — the
// user can retry or restore; on success the bin clears and the pane
// refreshes. The session is captured so a mid-flight tab switch can't
// redirect the delete.
func commitDeleteBin() {
	s := appData.active
	if s == nil || s.deleteBusy || s.Conn == nil || len(s.deleteBin) == 0 {
		return
	}
	s.deleteBusy = true
	s.deleteErr = nil
	conn := s.Conn
	paths := make([]string, len(s.deleteBin))
	for i, it := range s.deleteBin {
		paths[i] = it.Path
	}
	go func() {
		err := conn.Delete(paths)
		WithFrameLock(func() {
			s.deleteBusy = false
			if err != nil {
				s.deleteErr = err
				s.binExpanded = true // the error line lives in the body
				return
			}
			s.deleteBin = nil
			s.binned = nil
			if s.Pane != nil {
				s.Pane.reload("")
			}
		})
		RequestNextFrame()
	}()
}

// ── the stamp ────────────────────────────────────────────────────────
// Binned rows carry a big red trash stamp tilted 20° left across the
// filename. shirei draws axis-aligned rects only, so the tilt is baked
// into pixels once: render the glyph headlessly (black on white), rotate
// the pixels, turn darkness into alpha, tint red. RenderToImage runs a
// whole frame and is not reentrant — ensureDeleteStamp must run OUTSIDE
// frames (initApp and the snapshot helper), never from a view function.

var stampOnce sync.Once
var stampImg *image.RGBA      // red, for light staged rows
var stampImgLight *image.RGBA // white, for the deep-red selected rows

func ensureDeleteStamp() {
	stampOnce.Do(func() {
		ensureIconFonts()
		// render big and let ImageView scale down: the rotation resamples
		// once at high resolution instead of smearing a row-sized glyph
		const n = 64
		flat := RenderToImage(n, n, func() {
			Container(Attrs(Viewport, Center, Background(0, 0, 100, 1)), func() {
				Icon(TypTrash, FontSize(56), TextColor(0, 0, 0, 1))
			})
		})
		const rad = -20 * math.Pi / 180
		stampImg = tiltStamp(flat, rad, 205, 32, 25)
		stampImgLight = tiltStamp(flat, rad, 255, 245, 243)
	})
}

// tiltStamp rotates the black-on-white glyph render by rad and converts
// it to a premultiplied tinted alpha image (the soft renderer blits
// premultiplied RGBA src-over).
func tiltStamp(src *image.RGBA, rad float64, stampR, stampG, stampB uint32) *image.RGBA {
	sb := src.Bounds()
	sw, sh := float64(sb.Dx()), float64(sb.Dy())
	cos, sin := math.Cos(rad), math.Sin(rad)
	dw := int(math.Ceil(math.Abs(cos)*sw + math.Abs(sin)*sh))
	dh := int(math.Ceil(math.Abs(sin)*sw + math.Abs(cos)*sh))
	dst := image.NewRGBA(image.Rect(0, 0, dw, dh))

	// darkness sampler: 0 = background, 255 = glyph; outside = background
	dark := func(ix, iy int) float64 {
		if ix < 0 || iy < 0 || ix >= sb.Dx() || iy >= sb.Dy() {
			return 0
		}
		px := src.RGBAAt(sb.Min.X+ix, sb.Min.Y+iy)
		return 255 - (0.299*float64(px.R) + 0.587*float64(px.G) + 0.114*float64(px.B))
	}

	for y := 0; y < dh; y++ {
		for x := 0; x < dw; x++ {
			// inverse-rotate the dst pixel center into source space
			dx := float64(x) + 0.5 - float64(dw)/2
			dy := float64(y) + 0.5 - float64(dh)/2
			sx := cos*dx + sin*dy + sw/2 - 0.5
			sy := -sin*dx + cos*dy + sh/2 - 0.5
			// bilinear
			x0, y0 := math.Floor(sx), math.Floor(sy)
			fx, fy := sx-x0, sy-y0
			d := dark(int(x0), int(y0))*(1-fx)*(1-fy) +
				dark(int(x0)+1, int(y0))*fx*(1-fy) +
				dark(int(x0), int(y0)+1)*(1-fx)*fy +
				dark(int(x0)+1, int(y0)+1)*fx*fy
			a := uint32(d)
			if a == 0 {
				continue
			}
			dst.SetRGBA(x, y, color.RGBA{ // premultiplied
				R: uint8(stampR * a / 255),
				G: uint8(stampG * a / 255),
				B: uint8(stampB * a / 255),
				A: uint8(a),
			})
		}
	}
	return dst
}
