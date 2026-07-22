// hacker-news-reader: browse Hacker News feeds and threaded comments.
//
// Front / New / Show / Ask / Jobs via the public Firebase API (no key).
// Virtual lists for the feed and for comments; ItemHeight omitted so rows are
// auto-measured from ItemView via Measure. Comments load one level at a time:
// top-level on open, nested only when the user expands a parent.
//
//	go run .                 # GUI
//	go run . --png out.png   # headless front page (live HN API; sample on failure)
//	go run . --png-post out.png  # headless post view (sample data, offline)
package main

import (
	"fmt"
	"os"
	"sync"

	app "go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
)

type f32 = float32

// Screen is the top-level navigation surface.
type Screen int

const (
	ScreenFeed Screen = iota
	ScreenPost
)

// AppState owns all durable UI + network state.
type AppState struct {
	mu sync.Mutex

	screen Screen
	feed   Feed

	// Feed
	storyIDs    []int // full id list for current feed
	stories     []*Item
	feedLoading bool
	feedErr     string
	feedMore    bool // true while a "More" page is in flight

	// Post
	openID         int
	post           *Item
	comments       []*CommentNode
	postLoading    bool
	postErr        string
	expanded       map[int]bool // comment id → show loaded kids
	kidsLoading    map[int]bool // comment id → kids fetch in flight
	commentsLoaded bool
}

var appData = &AppState{
	feed:        FeedFront,
	expanded:    map[int]bool{},
	kidsLoading: map[int]bool{},
}

func main() {
	if len(os.Args) >= 3 && os.Args[1] == "--png" {
		if err := seedLiveFrontPage(); err != nil {
			fmt.Fprintln(os.Stderr, "live front page failed, using sample data:", err)
			seedSampleData(false)
		}
		if err := RenderToPNG(os.Args[2], 420, 720, RootView); err != nil {
			fmt.Println("render to png failed:", err)
			os.Exit(1)
		}
		return
	}
	if len(os.Args) >= 3 && os.Args[1] == "--png-post" {
		seedSampleData(true)
		if err := RenderToPNG(os.Args[2], 420, 720, RootView); err != nil {
			fmt.Println("render to png failed:", err)
			os.Exit(1)
		}
		return
	}

	go loadFeed(true, true)
	app.SetupIconBytes(iconPNG)
	app.SetupWindow("Hacker News Reader", 420, 720)
	app.Run(RootView)
}

func RootView() {
	Container(Attrs(Viewport, Expand, Background(220, 10, 97, 1)), func() {
		switch appData.screen {
		case ScreenPost:
			postScreen()
		default:
			feedScreen()
		}
	})
}

// pressedFeedback is true while a finger is on the current container (or a
// child), or while a real mouse button holds it. Touch is preferred; when the
// backend is synthesizing mouse from a finger (MouseFromTouch), we ignore
// IsActive so a delayed synthetic mouse-up cannot re-highlight after lift.
// Same pattern as examples/piano.
func pressedFeedback() bool {
	if IsTouched() {
		return true
	}
	if GetInputState().MouseFromTouch {
		return false
	}
	return IsActive()
}

// ---- feed loading ----------------------------------------------------------

// loadFeed loads or extends the current feed.
//
//	reset=true  — reload from the top (feed switch or Refresh)
//	reset=false — append the next page ("More")
//
// clearFirst=true drops the visible list immediately (feed switch). Refresh
// keeps the old rows until the new page arrives.
func loadFeed(reset, clearFirst bool) {
	appData.mu.Lock()
	feed := appData.feed
	if reset {
		if appData.feedLoading {
			appData.mu.Unlock()
			return
		}
		appData.feedLoading = true
		appData.feedErr = ""
		appData.storyIDs = nil
		if clearFirst {
			appData.stories = nil
		}
	} else {
		if appData.feedMore || appData.feedLoading {
			appData.mu.Unlock()
			return
		}
		appData.feedMore = true
	}
	have := 0
	if !reset {
		have = len(appData.stories)
	}
	ids := append([]int(nil), appData.storyIDs...)
	appData.mu.Unlock()
	RequestNextFrame()

	if reset || len(ids) == 0 {
		var err error
		ids, err = fetchStoryIDs(feed)
		if err != nil {
			appData.mu.Lock()
			appData.feedLoading = false
			appData.feedMore = false
			appData.feedErr = err.Error()
			appData.mu.Unlock()
			RequestNextFrame()
			return
		}
	}

	end := have + pageSize
	if end > len(ids) {
		end = len(ids)
	}
	var page []int
	if have < end {
		page = ids[have:end]
	}

	items, err := fetchItemsParallel(page)

	appData.mu.Lock()
	// Ignore stale results if the user switched feeds mid-flight.
	if appData.feed != feed {
		appData.feedLoading = false
		appData.feedMore = false
		appData.mu.Unlock()
		return
	}
	appData.storyIDs = ids
	appData.feedLoading = false
	appData.feedMore = false
	if err != nil && len(items) == 0 && (reset || have == 0) {
		appData.feedErr = err.Error()
	} else {
		appData.feedErr = ""
		if reset {
			appData.stories = items
		} else {
			appData.stories = append(appData.stories, items...)
		}
	}
	appData.mu.Unlock()
	RequestNextFrame()
}

func openPost(id int) {
	appData.mu.Lock()
	appData.screen = ScreenPost
	appData.openID = id
	appData.post = nil
	appData.comments = nil
	appData.expanded = map[int]bool{}
	appData.kidsLoading = map[int]bool{}
	appData.postErr = ""
	appData.commentsLoaded = false
	appData.postLoading = true
	// Prefer a story we already have for instant title paint.
	for _, s := range appData.stories {
		if s != nil && s.ID == id {
			cp := *s
			appData.post = &cp
			break
		}
	}
	appData.mu.Unlock()
	RequestNextFrame()
	go loadPost(id)
}

func closePost() {
	appData.mu.Lock()
	appData.screen = ScreenFeed
	appData.openID = 0
	appData.post = nil
	appData.comments = nil
	appData.expanded = map[int]bool{}
	appData.kidsLoading = map[int]bool{}
	appData.postLoading = false
	appData.postErr = ""
	appData.commentsLoaded = false
	appData.mu.Unlock()
}

// refreshPost re-fetches the open story and its top-level comments without
// leaving the thread screen. Keeps current content on screen until the new
// payload arrives.
func refreshPost() {
	appData.mu.Lock()
	id := appData.openID
	if id == 0 || appData.postLoading {
		appData.mu.Unlock()
		return
	}
	appData.postLoading = true
	appData.postErr = ""
	appData.mu.Unlock()
	RequestNextFrame()
	go loadPost(id)
}

func loadPost(id int) {
	item, err := fetchItem(id)
	if err != nil {
		appData.mu.Lock()
		if appData.openID == id {
			appData.postLoading = false
			appData.postErr = err.Error()
		}
		appData.mu.Unlock()
		RequestNextFrame()
		return
	}

	appData.mu.Lock()
	if appData.openID != id {
		appData.mu.Unlock()
		return
	}
	appData.post = item
	appData.mu.Unlock()
	RequestNextFrame()

	// Top-level comments only; nested tiers load when the user expands.
	var kids []int
	if item != nil {
		kids = item.Kids
	}
	tree, cerr := fetchCommentLevel(kids, 0)

	appData.mu.Lock()
	if appData.openID == id {
		appData.postLoading = false
		appData.commentsLoaded = true
		if cerr != nil && len(tree) == 0 {
			appData.postErr = cerr.Error()
		} else {
			appData.comments = tree
		}
	}
	appData.mu.Unlock()
	RequestNextFrame()
}

// toggleCommentExpand folds/unfolds a comment. On first expand, kicks off a
// fetch of direct kids (Item.Kids). Re-expand after fold reuses already-loaded kids.
func toggleCommentExpand(id int) {
	appData.mu.Lock()
	if appData.expanded[id] {
		delete(appData.expanded, id)
		appData.mu.Unlock()
		return
	}
	appData.expanded[id] = true
	n := findComment(appData.comments, id)
	if n == nil || n.Item == nil {
		appData.mu.Unlock()
		return
	}
	if n.KidsFetched || appData.kidsLoading[id] {
		appData.mu.Unlock()
		return
	}
	kidIDs := append([]int(nil), n.Item.Kids...)
	if len(kidIDs) == 0 {
		n.KidsFetched = true
		n.Kids = nil
		appData.mu.Unlock()
		return
	}
	openID := appData.openID
	depth := n.Depth
	appData.kidsLoading[id] = true
	appData.mu.Unlock()
	RequestNextFrame()
	go loadCommentKids(openID, id, kidIDs, depth)
}

func loadCommentKids(openID, parentID int, kidIDs []int, parentDepth int) {
	nodes, err := fetchCommentLevel(kidIDs, parentDepth+1)

	appData.mu.Lock()
	delete(appData.kidsLoading, parentID)
	if appData.openID != openID {
		appData.mu.Unlock()
		return
	}
	n := findComment(appData.comments, parentID)
	if n != nil {
		n.KidsFetched = true
		n.Kids = nodes
		if err != nil && len(nodes) == 0 {
			// Keep expanded; row shows no children. Parent Item.Kids still
			// indicates replies existed if the user folds and reopens.
		}
	}
	appData.mu.Unlock()
	RequestNextFrame()
}

// seedLiveFrontPage loads the real HN top stories for a marketing / --png frame.
func seedLiveFrontPage() error {
	appData.feed = FeedFront
	appData.screen = ScreenFeed
	ids, err := fetchStoryIDs(FeedFront)
	if err != nil {
		return err
	}
	end := pageSize
	if end > len(ids) {
		end = len(ids)
	}
	items, err := fetchItemsParallel(ids[:end])
	if err != nil {
		return err
	}
	if len(items) == 0 {
		return fmt.Errorf("no stories returned")
	}
	appData.storyIDs = ids
	appData.stories = items
	appData.feedLoading = false
	appData.feedErr = ""
	return nil
}

// seedSampleData fills app state so --png-post (and --png fallback) work offline.
func seedSampleData(postScreen bool) {
	appData.feed = FeedFront
	appData.stories = []*Item{
		{ID: 1, Type: "story", By: "pg", Title: "Y Combinator", URL: "http://ycombinator.com", Score: 57, Time: 1160418111, Descendants: 15},
		{ID: 2, Type: "story", By: "sama", Title: "Show HN: A demo Hacker News reader in Shirei", Score: 128, Time: 1710000000, Descendants: 42, Text: "Built with virtual lists and collapsible comments."},
		{ID: 3, Type: "story", By: "dang", Title: "Ask HN: What are you working on?", Score: 210, Time: 1710003600, Descendants: 89},
		{ID: 4, Type: "job", By: "whoishiring", Title: "Is Hiring (demo)", Score: 1, Time: 1710007200, URL: "https://news.ycombinator.com"},
	}
	appData.storyIDs = []int{1, 2, 3, 4}
	if !postScreen {
		return
	}

	// Long text to verify soft wrap + row height stay aligned.
	long := "This is a long comment that should soft-wrap across several lines inside the virtual list row. " +
		"Without MaxWidth on the content column, Label paints one unbroken line while the height estimator still reserves multi-line space. " +
		"The fix mirrors LogView: pass the row width into MaxWidth so text layout and itemHeight agree."
	appData.screen = ScreenPost
	appData.openID = 2
	appData.post = &Item{
		ID: 2, Type: "story", By: "sama",
		Title: "Show HN: A demo Hacker News reader in Shirei",
		Score: 128, Time: 1710000000, Descendants: 3,
		Text: "Built with virtual lists and collapsible comments. Self-text should wrap too when the post body is long enough to need more than one line in this narrow phone-width window.",
	}
	appData.commentsLoaded = true
	// Sample data pretends kids were already fetched (for --png-post layout).
	// Live path leaves Kids empty until the user expands.
	appData.expanded = map[int]bool{10: true}
	appData.comments = []*CommentNode{
		{
			Item: &Item{ID: 10, By: "alice", Time: 1710001000, Text: long, Kids: []int{11}},
			Depth: 0, KidsFetched: true,
			Kids: []*CommentNode{
				{Item: &Item{ID: 11, By: "bob", Time: 1710002000, Text: "Nested reply with more wrapping text: the indent eats width so the budget must shrink with depth."}, Depth: 1, KidsFetched: true},
			},
		},
		{Item: &Item{ID: 12, By: "carol", Time: 1710003000, Text: "Short one."}, Depth: 0, KidsFetched: true},
	}
}
