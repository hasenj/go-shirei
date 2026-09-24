package main

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"go.hasen.dev/shirei"
	"go.hasen.dev/shirei/drive"
)

func historyNode(t *testing.T, port int, name string) shirei.AccessNode {
	t.Helper()
	result, err := drive.Query(port, name)
	if err != nil || len(result.Nodes) != 1 {
		t.Fatalf("query %s: %+v, %v", name, result, err)
	}
	return result.Nodes[0]
}

func TestDriveDiffToolbar(t *testing.T) {
	repo, run := gitTestRepo(t)
	for _, name := range []string{"a.go", "b.go", "c.go"} {
		var content strings.Builder
		for i := 0; i < 70; i++ {
			fmt.Fprintf(&content, "line %d in %s\n", i, name)
			if i == 20 && name != "c.go" {
				content.WriteString("searchNeedle\n")
			}
		}
		writeFile(t, repo, name, content.String())
	}
	run("add", ".")
	run("commit", "-m", "Add three files")
	port := startDriveGitHistory(t, sessionData{Tabs: []string{repo}})
	mustCount(t, port, "find_diff", 1)
	mustCount(t, port, "diff_file", 1)
	click := func(q string) {
		t.Helper()
		if _, err := drive.ClickOne(port, q); err != nil {
			t.Fatal(err)
		}
	}
	key := func(k string, mods ...string) {
		t.Helper()
		if err := drive.Key(port, k, mods...); err != nil {
			t.Fatal(err)
		}
	}
	waitValue := func(q, want string) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for {
			n := historyNode(t, port, q)
			if n.Value == want {
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s value %q, want %q", q, n.Value, want)
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
	viewport := historyNode(t, port, "diff_viewport").Rect
	toolbar := historyNode(t, port, "diff_toolbar").Rect
	firstFile := historyNode(t, port, "diff_file")
	click("find_diff")
	mustCount(t, port, "diff_query", 1)
	mustCount(t, port, "next_file", 0)
	if got := historyNode(t, port, "diff_viewport").Rect; got != viewport {
		t.Fatalf("Find moved viewport: %v -> %v", viewport, got)
	}
	if got := historyNode(t, port, "diff_toolbar").Rect; got != toolbar {
		t.Fatalf("Find resized toolbar: %v -> %v", toolbar, got)
	}
	if got := historyNode(t, port, "diff_file"); got.Value != firstFile.Value || got.Rect != firstFile.Rect {
		t.Fatal("opening empty Find moved the diff")
	}
	if err := drive.Type(port, "diff_query", "searchNeedle"); err != nil {
		t.Fatal(err)
	}
	waitValue("diff_matches", "1 of 2")
	click("next_match")
	waitValue("diff_matches", "2 of 2")
	if err := drive.Shot(port, "Find replaces file controls"); err != nil {
		t.Fatal(err)
	}
	// Escape also works after clicking a match-navigation button.
	key("escape")
	mustCount(t, port, "diff_query", 0)
	mustCount(t, port, "next_file", 1)
	if got := historyNode(t, port, "diff_viewport").Rect; got != viewport {
		t.Fatal("closing Find resized viewport")
	}
	mod := "ctrl"
	if runtime.GOOS == "darwin" {
		mod = "cmd"
	}
	key("f", mod)
	mustCount(t, port, "diff_query", 1)
	waitValue("diff_query", "searchNeedle")
	waitValue("diff_matches", "2 of 2")
	click("recent")
	mustCount(t, port, "recent_menu", 1)
	key("escape")
	mustCount(t, port, "recent_menu", 0)
	mustCount(t, port, "diff_query", 1)
	click("previous_match")
	waitValue("diff_matches", "1 of 2")
	click("close_find")
	mustCount(t, port, "next_file", 1)
	// Navigation in normal mode changes files, not search matches.
	click("next_file")
	waitValue("diff_file", "b.go")
	click("previous_file")
	waitValue("diff_file", "a.go")
	click("collapse_all")
	mustCount(t, port, "diff_file", 3)
	files, err := drive.Query(port, "diff_file")
	if err != nil || len(files.Nodes) != 3 {
		t.Fatalf("collapsed files: %+v, %v", files, err)
	}
	for i, n := range files.Nodes {
		if n.Rect.Size[1] != fileHeaderH {
			t.Fatalf("collapsed row height = %v", n.Rect.Size[1])
		}
		if i > 0 && n.Rect.Origin[1] != files.Nodes[i-1].Rect.Origin[1]+fileHeaderH {
			t.Fatal("collapsed files have extra space between their headers")
		}
	}
	first := files.Nodes[0]
	click(fmt.Sprintf("#%d", first.ID))
	mustCount(t, port, "diff_file", 1)
	if header := historyNode(t, port, "diff_file"); header.Rect != first.Rect {
		t.Fatal("expanding a file moved or resized its header")
	}
	// Keyboard activation folds the same row; the entire header is the control.
	key("enter")
	mustCount(t, port, "diff_file", 3)
	if err := drive.Shot(port, "Compact collapsed files"); err != nil {
		t.Fatal(err)
	}
	// Finding a body match reveals the containing file while its neighbors stay folded.
	click("find_diff")
	click("next_match")
	waitValue("diff_matches", "2 of 2")
	click("close_find")
	click("previous_file")
	waitValue("diff_file", "b.go")
	click("diff_file")
	mustCount(t, port, "diff_file", 3)
	click("collapse_all")
	mustCount(t, port, "diff_file", 1)
	if err := drive.Shot(port, "File navigation restored"); err != nil {
		t.Fatal(err)
	}
	// The always-visible history filter still supports its shortcut and Escape.
	key("l", mod)
	if err := drive.Type(port, "history_query", "not present"); err != nil {
		t.Fatal(err)
	}
	mustCount(t, port, "history_entry", 0)
	key("escape")
	mustCount(t, port, "history_entry", 1)
	mustCount(t, port, "history_query", 1)
}
