package main

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"go.hasen.dev/shirei"
	"go.hasen.dev/shirei/drive"
)

func haystackNode(t *testing.T, port int, name, value string) shirei.AccessNode {
	t.Helper()
	result, err := drive.Query(port, name)
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range result.Nodes {
		if value == "" || n.Value == value {
			return n
		}
	}
	t.Fatalf("missing %s %q", name, value)
	return shirei.AccessNode{}
}

func clickHaystack(t *testing.T, port int, name, value string) {
	t.Helper()
	n := haystackNode(t, port, name, value)
	if _, err := drive.ClickOne(port, fmt.Sprintf("#%d", n.ID)); err != nil {
		t.Fatal(err)
	}
}

func TestDriveGroupedSearch(t *testing.T) {
	port := drive.Start(t, ".", "-query", "RequestNextFrame", "testdata/grouped")
	if err := drive.WaitCount(port, "file_header", 3); err != nil {
		t.Fatal(err)
	}

	drive.Comment("Folder and search commands share the input height")
	input := haystackNode(t, port, "query", "")
	for _, name := range []string{"search", "browse_folder", "folder", "include", "exclude"} {
		n := haystackNode(t, port, name, "")
		if math.Abs(float64(n.Rect.Size[1]-input.Rect.Size[1])) > 2 {
			t.Fatalf("%s height %v differs from query height %v", name, n.Rect.Size[1], input.Rect.Size[1])
		}
	}

	drive.Comment("File headers collapse their code lines")
	// Workers publish in completion order, so exercise the first visible file.
	firstLine := haystackNode(t, port, "result_line", "").Value
	firstFile, _, _ := strings.Cut(firstLine, ":")
	clickHaystack(t, port, "result_line", firstLine)
	if !haystackNode(t, port, "result_line", firstLine).Checked {
		t.Fatal("line selection is missing")
	}
	clickHaystack(t, port, "collapse_file", firstFile)
	rows, err := drive.Query(port, "result_line")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range rows.Nodes {
		if strings.HasPrefix(n.Value, firstFile+":") {
			t.Fatal("collapsed file still renders code")
		}
	}
	clickHaystack(t, port, "collapse_file", firstFile)

	drive.Comment("A second search opens a tab; returning restores the query")
	if err := drive.Type(port, "query", "xxxxxxxx"); err != nil {
		t.Fatal(err)
	}
	if err := drive.Key(port, "enter"); err != nil {
		t.Fatal(err)
	}
	if err := drive.WaitCount(port, "search_tab", 2); err != nil {
		t.Fatal(err)
	}
	if err := drive.WaitCount(port, "empty_results", 1); err != nil {
		t.Fatal(err)
	}
	clickHaystack(t, port, "search_tab", "RequestNextFrame")
	if got := haystackNode(t, port, "query", "").Value; got != "RequestNextFrame" {
		t.Fatalf("restored query %q", got)
	}
	if err := drive.WaitCount(port, "file_header", 3); err != nil {
		t.Fatal(err)
	}
	tabs, err := drive.Query(port, "close_tab")
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range tabs.Nodes {
		if n.Value != "RequestNextFrame" {
			if _, err := drive.ClickOne(port, fmt.Sprintf("#%d", n.ID)); err != nil {
				t.Fatal(err)
			}
			break
		}
	}
	if err := drive.WaitCount(port, "search_tab", 1); err != nil {
		t.Fatal(err)
	}

	drive.Comment("The compact Browse command opens the folder picker")
	clickHaystack(t, port, "browse_folder", "")
	if err := drive.WaitCount(port, "folder_picker", 1); err != nil {
		t.Fatal(err)
	}
	clickHaystack(t, port, "cancel_folder", "")
	if err := drive.WaitCount(port, "folder_picker", 0); err != nil {
		t.Fatal(err)
	}
	if got := haystackNode(t, port, "folder", "").Value; got != "testdata/grouped" {
		t.Fatalf("cancel changes folder to %q", got)
	}
}

func TestDriveVirtualizedCodeLines(t *testing.T) {
	dir := t.TempDir()
	// One large context block: every line matches, so block-level virtualization
	// cannot limit the amount of code built by the UI.
	if err := os.WriteFile(filepath.Join(dir, "many.txt"), []byte(strings.Repeat("needle\n", 1000)), 0600); err != nil {
		t.Fatal(err)
	}
	port := drive.Start(t, ".", "-query", "needle", dir)
	if err := drive.WaitCount(port, "file_header", 1); err != nil {
		t.Fatal(err)
	}
	rows, err := drive.Query(port, "result_line")
	if err != nil {
		t.Fatal(err)
	}
	if rows.Count < 10 || rows.Count > 60 {
		t.Fatalf("built %d code lines for a viewport", rows.Count)
	}
	clickHaystack(t, port, "result_line", rows.Nodes[0].Value)
	if err := drive.Wheel(port, 100000); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		rows, err = drive.Query(port, "result_line")
		if err != nil {
			t.Fatal(err)
		}
		// Query records are bounded by UDP packet size, so inspect the first
		// visible line and the full count rather than requiring the last record.
		if len(rows.Nodes) > 0 {
			first, _ := strconv.Atoi(strings.TrimPrefix(rows.Nodes[0].Value, "many.txt:"))
			if first >= 950 && rows.Count <= 60 {
				break
			}
		}
		if time.Now().After(deadline) {
			t.Fatalf("cannot reach the final code lines: count=%d records=%v", rows.Count, rows.Nodes)
		}
		time.Sleep(50 * time.Millisecond)
	}
	drive.Comment("Switching tabs restores the scrolled result position")
	if err := drive.Type(port, "query", "xxxxxxxx"); err != nil {
		t.Fatal(err)
	}
	if err := drive.Key(port, "enter"); err != nil {
		t.Fatal(err)
	}
	if err := drive.WaitCount(port, "search_tab", 2); err != nil {
		t.Fatal(err)
	}
	clickHaystack(t, port, "search_tab", "needle")
	first, _ := strconv.Atoi(strings.TrimPrefix(haystackNode(t, port, "result_line", "").Value, "many.txt:"))
	if first < 950 {
		t.Fatalf("tab returns to line %d instead of the saved position", first)
	}
}
