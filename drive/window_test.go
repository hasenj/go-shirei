package drive

import (
	"testing"

	"go.hasen.dev/shirei"
)

func TestParseWindowList(t *testing.T) {
	reply := `count: 2

id: main
kind: main
title: Shirei / Drive
size: 640 400
pos: 120 160
target: true

id: busy_alarm_popup
kind: popup
title: CPU: alert
size: 360 120
`
	list, err := parseWindowList(reply)
	if err != nil {
		t.Fatal(err)
	}
	if list.Count != 2 || len(list.Windows) != 2 {
		t.Fatalf("count=%d windows=%d", list.Count, len(list.Windows))
	}
	main := list.Windows[0]
	if main.ID != "main" || main.Kind != "main" || main.Title != "Shirei / Drive" {
		t.Fatalf("main identity = %+v", main)
	}
	if main.Size != (shirei.Vec2{640, 400}) || main.Pos != (shirei.Vec2{120, 160}) || !main.HasPos || !main.Target {
		t.Fatalf("main geometry = %+v", main)
	}
	popup := list.Windows[1]
	if popup.ID != "busy_alarm_popup" || popup.Kind != "popup" || popup.Title != "CPU: alert" {
		t.Fatalf("popup identity = %+v", popup)
	}
	if popup.HasPos || popup.Target {
		t.Fatalf("popup flags = %+v", popup)
	}
}

func TestParseWindowListEmpty(t *testing.T) {
	list, err := parseWindowList("count: 0")
	if err != nil {
		t.Fatal(err)
	}
	if list.Count != 0 || len(list.Windows) != 0 {
		t.Fatalf("empty list = %+v", list)
	}
}

func TestParseWindowListTruncated(t *testing.T) {
	list, err := parseWindowList("count: 40\n\nid: main\nkind: main\nsize: 1 2")
	if err != nil {
		t.Fatal(err)
	}
	if list.Count != 40 || len(list.Windows) != 1 {
		t.Fatalf("truncated list = %+v", list)
	}
}
