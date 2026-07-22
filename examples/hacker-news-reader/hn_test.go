package main

import (
	"strings"
	"testing"

	. "go.hasen.dev/shirei"
)

func TestHtmlToPlain(t *testing.T) {
	in := `Aw shucks, guys ... you make me blush with your compliments.<p>Tell you what, Ill make a deal: I&#x27;ll keep writing if you keep reading. K?`
	got := htmlToPlain(in)
	if strings.Contains(got, "<p>") || strings.Contains(got, "&#") {
		t.Fatalf("still has markup/entities: %q", got)
	}
	if !strings.Contains(got, "I'll keep writing") {
		t.Fatalf("missing decoded text: %q", got)
	}
	if !strings.Contains(got, "\n") {
		t.Fatalf("expected paragraph break: %q", got)
	}
}

func TestListVisibleCommentsExpand(t *testing.T) {
	// root
	//  ├─ a
	//  │   └─ a1
	//  └─ b
	a1 := &CommentNode{Item: &Item{ID: 11}, Depth: 1}
	a := &CommentNode{Item: &Item{ID: 1, Kids: []int{11}}, Depth: 0, Kids: []*CommentNode{a1}, KidsFetched: true}
	b := &CommentNode{Item: &Item{ID: 2}, Depth: 0, KidsFetched: true}
	roots := []*CommentNode{a, b}

	// Default: folded — only top-level.
	var folded []*CommentNode
	listVisibleComments(roots, nil, &folded)
	if len(folded) != 2 {
		t.Fatalf("folded: got %d want 2", len(folded))
	}

	expanded := map[int]bool{1: true}
	var vis []*CommentNode
	listVisibleComments(roots, expanded, &vis)
	if len(vis) != 3 {
		t.Fatalf("expanded a: got %d want 3", len(vis))
	}
	if vis[0].Item.ID != 1 || vis[1].Item.ID != 11 || vis[2].Item.ID != 2 {
		t.Fatalf("order: %d, %d, %d", vis[0].Item.ID, vis[1].Item.ID, vis[2].Item.ID)
	}
}

func TestFindComment(t *testing.T) {
	a1 := &CommentNode{Item: &Item{ID: 11}, Depth: 1}
	a := &CommentNode{Item: &Item{ID: 1}, Depth: 0, Kids: []*CommentNode{a1}}
	roots := []*CommentNode{a}
	if findComment(roots, 11) == nil {
		t.Fatal("expected to find nested id 11")
	}
	if findComment(roots, 99) != nil {
		t.Fatal("expected miss for 99")
	}
}

func TestFeedEndpoints(t *testing.T) {
	for _, f := range []Feed{FeedFront, FeedNew, FeedShow, FeedAsk, FeedJobs} {
		if f.Endpoint() == "" || f.Label() == "?" {
			t.Fatalf("bad feed %v", f)
		}
	}
}

func TestItemTimestamp(t *testing.T) {
	it := &Item{Time: 1160418111} // 2006-10-09-ish UTC
	ts := it.Timestamp()
	if len(ts) != len("2006-01-02 15:04") {
		t.Fatalf("timestamp format: %q", ts)
	}
}

func TestCommentRowAutoMeasure(t *testing.T) {
	InitFontSubsystem()
	ResetInputSession()
	GetHost().WindowSize = Vec2{400, 600}

	short := &CommentNode{Item: &Item{ID: 1, Text: "hi"}, Depth: 0}
	long := &CommentNode{Item: &Item{ID: 2, Text: strings.Repeat("word ", 80)}, Depth: 0}

	// Same path VirtualList uses when ItemHeight is nil.
	measureComment := func(n *CommentNode, width f32) f32 {
		return Measure(Vec2{width, 0}, func() {
			commentRow(n, nil, nil, width)
		})[1]
	}
	hs := measureComment(short, 400)
	hl := measureComment(long, 400)
	if hl <= hs {
		t.Fatalf("long comment should be taller: short=%v long=%v", hs, hl)
	}
	hlNarrow := measureComment(long, 120)
	if hlNarrow < hl {
		t.Fatalf("narrower width should not shrink height: narrow=%v wide=%v", hlNarrow, hl)
	}
}

func TestStoryRowAutoMeasure(t *testing.T) {
	InitFontSubsystem()
	ResetInputSession()

	short := &Item{ID: 10, Title: "Hi", Score: 1, By: "a", Time: 1}
	long := &Item{
		ID:    11,
		Title: strings.Repeat("Long title word ", 30),
		Score: 1, By: "a", Time: 1,
		URL: "https://example.com/path",
	}
	measureStory := func(it *Item, width f32) f32 {
		return Measure(Vec2{width, 0}, func() {
			storyRow(it, width)
		})[1]
	}
	hs := measureStory(short, 400)
	hl := measureStory(long, 400)
	if hl <= hs {
		t.Fatalf("long title should be taller: short=%v long=%v", hs, hl)
	}
}
