package main

import (
	"fmt"
	"testing"
	"time"

	"go.hasen.dev/shirei"
	"go.hasen.dev/shirei/drive"
)

func readerNode(t *testing.T, port int, name, value string) shirei.AccessNode {
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
	t.Fatalf("missing %s %q: %+v", name, value, result)
	return shirei.AccessNode{}
}

func TestDriveReader(t *testing.T) {
	port := drive.Start(t, ".", "--demo")
	click := func(name, value string) {
		t.Helper()
		n := readerNode(t, port, name, value)
		if _, err := drive.ClickOne(port, fmt.Sprintf("#%d", n.ID)); err != nil {
			t.Fatal(err)
		}
	}
	wait := func(name string, count int) {
		t.Helper()
		if err := drive.WaitCount(port, name, count); err != nil {
			t.Fatal(err)
		}
	}
	waitSelected := func(label string) {
		t.Helper()
		deadline := time.Now().Add(3 * time.Second)
		for {
			tab := readerNode(t, port, "feed_tab", label)
			if tab.Checked {
				if tab.Role != "tab" {
					t.Fatalf("feed tab has the wrong accessibility role: %+v", tab)
				}
				return
			}
			if time.Now().After(deadline) {
				t.Fatalf("%s tab did not become selected", label)
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
	wait("story", 7)
	click("feed_tab", "Ask")
	waitSelected("Ask")
	// Move focus to the next feed and activate it from the keyboard.
	if err := drive.Key(port, "tab"); err != nil {
		t.Fatal(err)
	}
	if err := drive.Key(port, "enter"); err != nil {
		t.Fatal(err)
	}
	waitSelected("Jobs")
	if readerNode(t, port, "feed_tab", "Ask").Checked {
		t.Fatal("Ask remains selected after switching to Jobs")
	}
	click("story", "2")
	wait("back_to_feed", 1)
	readerNode(t, port, "comment", "11")
	readerNode(t, port, "comment", "12")
	// Comment text is not a fold target.
	click("comment", "10")
	readerNode(t, port, "comment", "11")
	click("comment_replies", "10")
	wait("comment", 3)
	// The focused disclosure also works from the keyboard.
	if err := drive.Key(port, "enter"); err != nil {
		t.Fatal(err)
	}
	wait("comment", 5)
	readerNode(t, port, "comment", "11")
	click("comment_replies", "10")
	wait("comment", 3)
	click("comment_replies", "13")
	wait("comment", 4)
	readerNode(t, port, "comment", "14")
	if err := drive.Shot(port, "Reader with nested replies"); err != nil {
		t.Fatal(err)
	}
	click("back_to_feed", "")
	wait("story", 7)
	wait("back_to_feed", 0)
}
