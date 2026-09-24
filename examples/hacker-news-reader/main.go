// hacker-news-reader: browse Hacker News feeds and threaded comments.
//
// Front / New / Show / Ask / Jobs via the public Firebase API (no key).
// Virtual lists for the feed and for comments; ItemHeight omitted so rows are
// auto-measured from ItemView via Measure. Comments load one level at a time:
// top-level on open, nested only when the user expands a parent.
//
//	go run .                 # GUI
//	go run . -png out.png        # headless front page (live HN API; sample on failure)
//	go run . -png-post out.png   # headless post view (sample data, offline)
package main

import (
	"flag"
	"fmt"
	"os"
	"sync"
	"time"

	app "go.hasen.dev/shirei/app"
	"go.hasen.dev/shirei/ext/darkmode"
	. "go.hasen.dev/shirei/widgets"

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

	demo        bool
	feedRequest uint64
	screen      Screen
	feed        Feed

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
	png := flag.String("png", "", "write a front-page frame to PATH and exit")
	pngPost := flag.String("png-post", "", "write a post-view frame to PATH and exit (sample data)")
	demo := flag.Bool("demo", false, "browse offline sample stories and comments")
	flag.Parse()
	appData.demo = *demo
	if *png != "" {
		if *demo {
			seedSampleData(false)
		} else if err := seedLiveFrontPage(); err != nil {
			fmt.Fprintln(os.Stderr, "live front page failed, using sample data:", err)
			seedSampleData(false)
		}
		if err := RenderToPNG(*png, 420, 720, RootView); err != nil {
			fmt.Println("render to png failed:", err)
			os.Exit(1)
		}
		return
	}
	if *pngPost != "" {
		seedSampleData(true)
		if err := RenderToPNG(*pngPost, 420, 720, RootView); err != nil {
			fmt.Println("render to png failed:", err)
			os.Exit(1)
		}
		return
	}

	if *demo {
		seedSampleData(false)
	} else {
		go loadFeed(true, true)
	}
	app.SetupIconBytes(iconPNG)
	app.SetupWindow("Hacker News Reader", 460, 800)
	app.SetupDrive()
	app.Run(RootView)
}

func RootView() {
	SetDarkMode(darkmode.OSDarkMode())
	colors := readerColors()
	Container(Attrs(Viewport, Expand, BackgroundVec(colors.page), AmendTextStyle(TextColorVec(colors.text))), func() {
		switch appData.screen {
		case ScreenPost:
			postScreen()
		default:
			feedScreen()
		}
	})
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
	if appData.demo {
		appData.mu.Unlock()
		RequestNextFrame()
		return
	}
	if reset {
		if appData.feedLoading && !clearFirst {
			appData.mu.Unlock()
			return
		}
		appData.feedLoading = true
		appData.feedMore = false
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
	if reset {
		appData.feedRequest++
	}
	request := appData.feedRequest
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
			if request != appData.feedRequest {
				appData.mu.Unlock()
				return
			}
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
	if appData.feed != feed || appData.feedRequest != request {
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
	if appData.demo {
		appData.mu.Lock()
		seedSampleComments()
		appData.mu.Unlock()
		RequestNextFrame()
	} else {
		go loadPost(id)
	}
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
	if id == 0 || appData.postLoading || appData.demo {
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

// seedSampleData supplies offline content for previews and --demo.
func seedSampleData(postScreen bool) {
	now := time.Now().Unix()
	appData.feed = FeedFront
	appData.screen = ScreenFeed
	appData.stories = []*Item{
		{ID: 1, Type: "story", By: "taylor", Title: "SQLite is not a database. It is a way of life.", URL: "https://sqlite.org", Score: 284, Time: now - 7200, Descendants: 96},
		{ID: 2, Type: "story", By: "alex", Title: "Show HN: A tiny native reader for Hacker News", URL: "https://github.com/hasenj/go-shirei", Score: 128, Time: now - 10800, Descendants: 6, Text: "I wanted a quiet place to read the front page and follow a conversation. Built with Shirei, with native controls and a small footprint."},
		{ID: 3, Type: "story", By: "maya", Title: "The quiet craft of building useful software", URL: "https://notes.example.org", Score: 176, Time: now - 14400, Descendants: 58},
		{ID: 4, Type: "story", By: "sam", Title: "Ask HN: What are you working on?", Score: 210, Time: now - 14400, Descendants: 89},
		{ID: 5, Type: "story", By: "leo", Title: "A visual introduction to the Fourier transform", URL: "https://math.example.org", Score: 93, Time: now - 18000, Descendants: 21},
		{ID: 6, Type: "story", By: "robin", Title: "Why old computers still feel fast", URL: "https://computing.example.org", Score: 147, Time: now - 18000, Descendants: 64},
		{ID: 7, Type: "story", By: "devon", Title: "Show HN: A filesystem you can browse through time", URL: "https://github.com", Score: 82, Time: now - 21600, Descendants: 18},
	}
	appData.storyIDs = []int{1, 2, 3, 4, 5, 6, 7}
	if !postScreen {
		return
	}
	appData.screen = ScreenPost
	appData.openID = 2
	appData.post = appData.stories[1]
	seedSampleComments()
}

func seedSampleComments() {
	now := time.Now().Unix()
	appData.commentsLoaded = true
	appData.postLoading = false
	appData.expanded = map[int]bool{10: true}
	appData.kidsLoading = map[int]bool{}
	appData.comments = []*CommentNode{
		{Item: &Item{ID: 10, By: "morgan", Time: now - 7200, Text: "This is exactly the kind of app I like: one window, a readable list, and no distractions.", Kids: []int{11, 12}}, KidsFetched: true,
			Kids: []*CommentNode{
				{Item: &Item{ID: 11, By: "alex", Time: now - 7200, Text: "Thank you! Keeping the reading experience simple was the main goal."}, Depth: 1, KidsFetched: true},
				{Item: &Item{ID: 12, By: "jules", Time: now - 3600, Text: "The thread guides make it much easier to follow the conversation."}, Depth: 1, KidsFetched: true},
			}},
		{Item: &Item{ID: 13, By: "riley", Time: now - 3600, Text: "How does it handle very long threads? Some discussions have hundreds of comments.", Kids: []int{14}}, KidsFetched: true,
			Kids: []*CommentNode{{Item: &Item{ID: 14, By: "alex", Time: now - 3600, Text: "The list measures and renders the visible comments. Replies load when you expand a thread."}, Depth: 1, KidsFetched: true}}},
		{Item: &Item{ID: 15, By: "devon", Time: now - 3600, Text: "I appreciate that the original article opens in my browser."}, KidsFetched: true},
	}
}
