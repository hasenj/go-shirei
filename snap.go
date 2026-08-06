package shirei

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Full-UI PNG snapshot helpers for headless RenderToImage goldens and the
// optional SHIREI_SNAP_REPORT JSONL channel used by review UIs.
//
// Snapshot / CompareImage return data only; callers map SnapResult onto
// testing.T (or any other harness). When SHIREI_SNAP_REPORT is set,
// ReportSnap appends one SnapEvent line per assertion.
//
// Missing golden -> Status Created (caller usually passes). Mismatch ->
// writes <name>.actual.png. UPDATE_SNAPSHOTS=1 regenerates goldens.
// Goldens include host system fonts: refactor guard-rails, not portable artifacts.

// EnvSnapReport is the path to an append-only JSONL file of SnapEvent lines.
const EnvSnapReport = "SHIREI_SNAP_REPORT"

// SnapResult statuses (also SnapEvent.Status).
const (
	SnapMatch    = "match"
	SnapMismatch = "mismatch"
	SnapCreated  = "created"
	SnapUpdated  = "updated"
	SnapSkip     = "skip"
)

// SnapResult is the outcome of CompareImage or Snapshot.
type SnapResult struct {
	Name   string // snapshot id (basename without .png)
	Status string // SnapMatch | SnapMismatch | SnapCreated | SnapUpdated | SnapSkip
	Golden string // path used or written
	Actual string // path written on mismatch
	Err    error  // IO / encode / decode failure
	Reason string // skip (or other) explanation
}

// SnapEvent is one snapshot assertion for SHIREI_SNAP_REPORT.
type SnapEvent struct {
	Pkg    string `json:"pkg"`              // absolute package directory
	Test   string `json:"test"`             // caller-supplied name, e.g. testing.T.Name()
	Name   string `json:"name"`             // snapshot id
	Status string `json:"status"`           // match | mismatch | created | updated | skip
	Golden string `json:"golden,omitempty"` // absolute path
	Actual string `json:"actual,omitempty"` // absolute path when written
}

var snapReportMu sync.Mutex

// SnapAbsPath resolves path relative to the process working directory to an
// absolute path for display and the report file.
func SnapAbsPath(path string) string {
	if path == "" {
		return ""
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}

// ReportSnap appends one SnapEvent when SHIREI_SNAP_REPORT is set.
// testName is stored in SnapEvent.Test (pass testing.T.Name() from go test).
func ReportSnap(testName string, r SnapResult) {
	reportPath := os.Getenv(EnvSnapReport)
	if reportPath == "" {
		return
	}
	cwd, _ := os.Getwd()
	ev := SnapEvent{
		Pkg:    SnapAbsPath(cwd),
		Test:   testName,
		Name:   r.Name,
		Status: r.Status,
		Golden: SnapAbsPath(r.Golden),
		Actual: SnapAbsPath(r.Actual),
	}
	data, err := json.Marshal(ev)
	if err != nil {
		return
	}
	data = append(data, '\n')

	snapReportMu.Lock()
	defer snapReportMu.Unlock()
	f, err := os.OpenFile(reportPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	_, _ = f.Write(data)
	_ = f.Sync()
	_ = f.Close()
}

// CompareImage compares img to the PNG at goldenPath (create / update /
// mismatch / match). name is the snapshot id for reports.
func CompareImage(name, goldenPath string, img *image.RGBA) SnapResult {
	r := SnapResult{Name: name, Golden: goldenPath}

	if os.Getenv("UPDATE_SNAPSHOTS") != "" {
		if err := writeSnapPNG(goldenPath, img); err != nil {
			r.Err = err
			return r
		}
		r.Status = SnapUpdated
		return r
	}

	saved, err := os.ReadFile(goldenPath)
	if os.IsNotExist(err) {
		if err := writeSnapPNG(goldenPath, img); err != nil {
			r.Err = err
			return r
		}
		r.Status = SnapCreated
		return r
	}
	if err != nil {
		r.Err = fmt.Errorf("reading snapshot %s: %w", SnapAbsPath(goldenPath), err)
		return r
	}
	want, err := png.Decode(bytes.NewReader(saved))
	if err != nil {
		r.Err = fmt.Errorf("decoding snapshot %s: %w", SnapAbsPath(goldenPath), err)
		return r
	}
	if !snapSameRGBA(img, snapToRGBA(want)) {
		actual := strings.TrimSuffix(goldenPath, ".png") + ".actual.png"
		if err := writeSnapPNG(actual, img); err != nil {
			r.Err = err
			return r
		}
		r.Status = SnapMismatch
		r.Actual = actual
		return r
	}
	r.Status = SnapMatch
	return r
}

// Snapshot renders fn at the given logical size, compares against
// testdata/snapshots/<name>.png, and calls ReportSnap with the outcome.
// Skips (Status SnapSkip) when the host has no usable fonts.
//
// Each invocation renders inside a fresh pointer-scoped container so every
// snapshot is a fresh app launch: stable identity across the invocation's
// settle frames, no state inherited from earlier invocations in the same
// process. RenderToImage resets the global input/focus session for the same
// reason.
func Snapshot(testName, name string, w, h int, fn FrameFn) SnapResult {
	InitFontSubsystem()
	shaped := ShapeText("alpha", DefaultTextStyle())
	if len(shaped.Lines) != 1 || len(shaped.Lines[0].Segments) == 0 {
		r := SnapResult{Name: name, Status: SnapSkip, Reason: "no usable system fonts for text shaping"}
		ReportSnap(testName, r)
		return r
	}

	scope := new(int) // fresh identity per invocation, stable across its frames
	img := RenderToImage(w, h, func() {
		ContainerWithKey(scope, Attrs(Viewport), fn)
	})

	path := filepath.Join("testdata", "snapshots", name+".png")
	r := CompareImage(name, path, img)
	ReportSnap(testName, r)
	return r
}

func snapToRGBA(img image.Image) *image.RGBA {
	if r, ok := img.(*image.RGBA); ok && r.Bounds().Min == (image.Point{}) {
		return r
	}
	b := img.Bounds()
	dst := image.NewRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(dst, dst.Bounds(), img, b.Min, draw.Src)
	return dst
}

func snapSameRGBA(a, b *image.RGBA) bool {
	return a.Bounds() == b.Bounds() && bytes.Equal(a.Pix, b.Pix)
}

func writeSnapPNG(path string, img *image.RGBA) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("mkdir: %w", err)
	}
	f, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("create %s: %w", path, err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		return fmt.Errorf("encode %s: %w", path, err)
	}
	return nil
}
