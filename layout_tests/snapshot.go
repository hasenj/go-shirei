package layout_tests

import (
	"path/filepath"
	"strings"
	"testing"

	"go.hasen.dev/shirei"
)

// layoutSnapshot renders a single frame and compares it pixel by pixel
// against the PNG snapshot at snapshotPath.
//
// If the snapshot file does not exist it is created and the test passes:
// review the generated image and commit it. On mismatch the rendered frame is
// written next to the snapshot with an ".actual.png" suffix and the test
// fails. Run with UPDATE_SNAPSHOTS=1 to overwrite all snapshots.
//
// When SHIREI_SNAP_REPORT is set, one JSON line is appended per assertion.
func layoutSnapshot(t *testing.T, snapshotPath string, width int, height int, frameFn shirei.FrameFn) {
	t.Helper()

	name := strings.TrimSuffix(filepath.Base(snapshotPath), ".png")

	// the snapshot path doubles as the id namespace isolating this test's
	// container state from the other tests
	img := renderFrame(snapshotPath, width, height, frameFn)
	r := shirei.CompareImage(name, snapshotPath, img)
	shirei.ReportSnap(t.Name(), r)
	switch {
	case r.Err != nil:
		t.Fatal(r.Err)
	case r.Status == shirei.SnapMismatch:
		t.Errorf("layout does not match snapshot %s; rendered frame written to %s",
			shirei.SnapAbsPath(r.Golden), shirei.SnapAbsPath(r.Actual))
	case r.Status == shirei.SnapCreated:
		t.Logf("created snapshot %s; review it and commit it", shirei.SnapAbsPath(r.Golden))
	}
}
