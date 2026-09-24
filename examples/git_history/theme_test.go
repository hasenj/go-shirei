package main

import (
	"fmt"
	"testing"
	"time"

	"go.hasen.dev/shirei/examples/internal/themetest"
)

func TestSnapshotColorSchemes(t *testing.T) {
	previous := appData
	defer func() { appData = previous }()
	tab := newRepoTab("/projects/shirei", "shirei")
	defer tab.statsCancel()
	other := newRepoTab("/projects/haystack", "haystack")
	defer other.statsCancel()
	tab.loaded, other.loaded = true, true
	tab.showStats = false
	tab.showAuthor = true
	tab.history = []HistoryEntry{{Kind: KindWorkingTree, ID: idWorkingTree}, {Kind: KindStaging, ID: idStaging}}
	for i, subject := range []string{"Add layout warning diagnostics", "Clarify row boundaries", "Share flat tabs between examples", "Add preferred light and dark schemes", "Theme the remaining widgets", "Improve file browser navigation", "Preserve selection across refresh", "Stream large commit patches", "Add search across diffs", "Remember open repositories"} {
		id := fmt.Sprintf("abc%04d", i)
		tab.history = append(tab.history, HistoryEntry{Kind: KindCommit, ID: id, Short: id, Subject: subject, Author: "Codex", When: time.Date(2026, 9, 20-i, 12, 0, 0, 0, time.UTC)})
	}
	tab.selected, tab.docID = "abc0000", "abc0000"
	tab.doc = &DiffDoc{
		Subject: "Add layout warning diagnostics", Author: "Codex", Email: "codex@openai.com", Date: "2026-09-20",
		Body: "Report collapsed extrinsic containers when layout diagnostics are enabled.",
		Rows: []DiffRow{
			{Kind: RowFileHeader, Text: "shirei/layout_diagnostics.go"},
			{Kind: RowHunkHeader, Text: "@@ -0,0 +1,10 @@"},
			{Kind: RowAdd, Text: "+package shirei"},
			{Kind: RowAdd, Text: "+"},
			{Kind: RowAdd, Text: "+var layoutWarnings = os.Getenv(\"SHIREI_LAYOUT_WARN\") == \"1\""},
			{Kind: RowAdd, Text: "+"},
			{Kind: RowAdd, Text: "+func warnCollapsedLayout(c *_Container) {"},
			{Kind: RowAdd, Text: "+    if c.ExtrinsicSize && c.Clip {"},
			{Kind: RowAdd, Text: "+        // Inspect the final layout dimensions."},
			{Kind: RowAdd, Text: "+        reportCollapsedContent(c)"},
			{Kind: RowAdd, Text: "+    }"},
			{Kind: RowAdd, Text: "+}"},
			{Kind: RowFileHeader, Text: "shirei/shirei.go"},
			{Kind: RowHunkHeader, Text: "@@ -412,4 +412,6 @@"},
			{Kind: RowContext, Text: "     if finalPass {"},
			{Kind: RowDel, Text: "-        collectFrameArtifacts(ui.current)"},
			{Kind: RowAdd, Text: "+        if layoutWarnings {"},
			{Kind: RowAdd, Text: "+            warnCollapsedLayout(ui.current)"},
			{Kind: RowAdd, Text: "+        }"},
			{Kind: RowContext, Text: "         collectFrameArtifacts(ui.current)"},
			{Kind: RowContext, Text: "     }"},
			{Kind: RowFileHeader, Text: "docs/tutorial.md"},
			{Kind: RowHunkHeader, Text: "@@ -1 +1,2 @@"},
			{Kind: RowAdd, Text: "+Enable layout warnings with SHIREI_LAYOUT_WARN=1."},
		},
	}
	tab.doc.Segs = buildDiffFileSegs(tab.doc)
	for _, s := range tab.doc.Segs {
		tab.doc.Stats = append(tab.doc.Stats, FileStat{Path: s.Path, Added: s.Added, Deleted: s.Deleted})
	}
	tab.doc.recomputeTotals()
	appData = &App{tabs: []*RepoTab{tab, other}, active: tab}
	themetest.Snapshot(t, "color_schemes", 1100, 700, RootView)
	tab.diffFindOpen, tab.findQuery = true, "layoutWarnings"
	syncDiffFind(tab)
	tab.findIdx = 1
	themetest.Snapshot(t, "find", 1100, 700, RootView)
	tab.diffFindOpen = false
	tab.showStats = true
	for _, e := range tab.history {
		if e.Kind == KindCommit {
			tab.commitStats[e.ID] = statsFromDoc(tab.doc)
		}
	}
	themetest.Snapshot(t, "compact", 800, 600, RootView)

	for _, name := range []string{
		"shirei/examples/git_history/testdata/snapshots/color_schemes.png",
		"shirei/examples/git_history/testdata/snapshots/color_schemes_dark.png",
		"shirei/examples/git_history/testdata/snapshots/compact.png",
		"shirei/examples/git_history/testdata/snapshots/compact_dark.png",
		"shirei/examples/git_history/testdata/snapshots/find.png",
		"shirei/examples/git_history/testdata/snapshots/find_dark.png",
	} {
		tab.doc.Rows = append(tab.doc.Rows, DiffRow{Kind: RowFileHeader, Text: name}, DiffRow{Kind: RowMeta, Text: "binary file"})
	}
	tab.doc.Rows = append(tab.doc.Rows,
		DiffRow{Kind: RowFileHeader, Text: "old/guide.md → docs/README.md"}, DiffRow{Kind: RowMeta, Text: "renamed"},
		DiffRow{Kind: RowFileHeader, Text: "notes.txt (untracked)"}, DiffRow{Kind: RowAdd, Text: "+New notes"},
	)
	tab.doc.Segs = buildDiffFileSegs(tab.doc)
	tab.doc.Stats = nil
	for _, seg := range tab.doc.Segs {
		tab.doc.Stats = append(tab.doc.Stats, FileStat{Path: seg.Path, Added: seg.Added, Deleted: seg.Deleted, Binary: seg.Binary})
	}
	tab.doc.recomputeTotals()
	tab.commitStats[tab.selected] = statsFromDoc(tab.doc)
	tab.diffView = newDiffView(tab.docID, tab.doc.Segs)
	tab.diffView.SetAllCollapsed(true)
	themetest.Snapshot(t, "collapsed", 1100, 800, RootView)
}
