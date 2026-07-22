package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

const (
	hnBase     = "https://hacker-news.firebaseio.com/v0"
	userAgent  = "shirei-hacker-news-reader/1.0 (+https://go.hasen.dev/shirei)"
	pageSize   = 30
	// One level at a time can still be wide (100+ top-level kids on hot posts).
	maxWorkers = 32
)

var httpClient = &http.Client{Timeout: 20 * time.Second}

// Feed is one of the HN story lists (segmented control).
type Feed int

const (
	FeedFront Feed = iota
	FeedNew
	FeedShow
	FeedAsk
	FeedJobs
)

func (f Feed) Label() string {
	switch f {
	case FeedFront:
		return "Front"
	case FeedNew:
		return "New"
	case FeedShow:
		return "Show"
	case FeedAsk:
		return "Ask"
	case FeedJobs:
		return "Jobs"
	default:
		return "?"
	}
}

func (f Feed) Endpoint() string {
	switch f {
	case FeedFront:
		return hnBase + "/topstories.json"
	case FeedNew:
		return hnBase + "/newstories.json"
	case FeedShow:
		return hnBase + "/showstories.json"
	case FeedAsk:
		return hnBase + "/askstories.json"
	case FeedJobs:
		return hnBase + "/jobstories.json"
	default:
		return hnBase + "/topstories.json"
	}
}

// Item is one HN Firebase item (story, comment, job, …).
type Item struct {
	ID          int    `json:"id"`
	Type        string `json:"type"`
	By          string `json:"by"`
	Title       string `json:"title"`
	URL         string `json:"url"`
	Text        string `json:"text"` // HTML
	Score       int    `json:"score"`
	Time        int64  `json:"time"`
	Descendants int    `json:"descendants"`
	Kids        []int  `json:"kids"`
	Parent      int    `json:"parent"`
	Deleted     bool   `json:"deleted"`
	Dead        bool   `json:"dead"`
}

func (it *Item) Timestamp() string {
	if it == nil || it.Time == 0 {
		return ""
	}
	return time.Unix(it.Time, 0).Local().Format("2006-01-02 15:04")
}

func (it *Item) PlainText() string {
	if it == nil {
		return ""
	}
	if it.Deleted {
		return "[deleted]"
	}
	if it.Dead {
		return "[dead]"
	}
	return htmlToPlain(it.Text)
}

func (it *Item) IsCommentable() bool {
	if it == nil {
		return false
	}
	return it.Type == "story" || it.Type == "poll"
}

// CommentNode is a comment with depth for flat/virtual list rendering.
//
// Kids are loaded lazily: when the user expands a comment we fetch Item.Kids
// and fill Kids. KidsFetched is true after that request completes (even if
// there were no kids or all fetches failed).
type CommentNode struct {
	Item        *Item
	Depth       int
	Kids        []*CommentNode
	KidsFetched bool
}

func fetchJSON[T any](url string) (T, error) {
	var zero T
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return zero, err
	}
	req.Header.Set("User-Agent", userAgent)
	req.Header.Set("Accept", "application/json")
	resp, err := httpClient.Do(req)
	if err != nil {
		return zero, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return zero, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}
	var out T
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return zero, err
	}
	return out, nil
}

func fetchItem(id int) (*Item, error) {
	it, err := fetchJSON[Item](fmt.Sprintf("%s/item/%d.json", hnBase, id))
	if err != nil {
		return nil, err
	}
	if it.ID == 0 {
		return nil, fmt.Errorf("item %d missing", id)
	}
	return &it, nil
}

func fetchStoryIDs(feed Feed) ([]int, error) {
	return fetchJSON[[]int](feed.Endpoint())
}

// fetchItemsParallel loads items by id, preserving order. Holes from failed
// fetches are omitted from the returned slice.
func fetchItemsParallel(ids []int) ([]*Item, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	type result struct {
		idx  int
		item *Item
		err  error
	}
	out := make([]*Item, len(ids))
	ch := make(chan result, len(ids))
	sem := make(chan struct{}, maxWorkers)
	var wg sync.WaitGroup
	for i, id := range ids {
		wg.Add(1)
		go func(i, id int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			item, err := fetchItem(id)
			ch <- result{i, item, err}
		}(i, id)
	}
	go func() {
		wg.Wait()
		close(ch)
	}()
	var firstErr error
	for r := range ch {
		if r.err != nil {
			if firstErr == nil {
				firstErr = r.err
			}
			continue
		}
		out[r.idx] = r.item
	}
	compact := out[:0]
	for _, it := range out {
		if it != nil && it.ID != 0 {
			compact = append(compact, it)
		}
	}
	if len(compact) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return compact, nil
}

// fetchCommentLevel loads one tier of comments by id (no recursion).
// Order follows ids; missing/failed items are skipped. Depth is stored on each node.
func fetchCommentLevel(ids []int, depth int) ([]*CommentNode, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	fetched, err := fetchItemsParallel(ids)
	if err != nil && len(fetched) == 0 {
		return nil, err
	}
	byID := make(map[int]*Item, len(fetched))
	for _, it := range fetched {
		if it != nil {
			byID[it.ID] = it
		}
	}
	var nodes []*CommentNode
	for _, id := range ids {
		it := byID[id]
		if it == nil {
			continue
		}
		nodes = append(nodes, &CommentNode{Item: it, Depth: depth})
	}
	return nodes, nil
}

// findComment walks the loaded tree for id.
func findComment(roots []*CommentNode, id int) *CommentNode {
	for _, n := range roots {
		if n == nil || n.Item == nil {
			continue
		}
		if n.Item.ID == id {
			return n
		}
		if found := findComment(n.Kids, id); found != nil {
			return found
		}
	}
	return nil
}

// listVisibleComments flattens the tree in pre-order. expanded[id] true means
// this comment's loaded kids are shown (default is folded).
func listVisibleComments(roots []*CommentNode, expanded map[int]bool, out *[]*CommentNode) {
	for _, n := range roots {
		if n == nil || n.Item == nil {
			continue
		}
		*out = append(*out, n)
		if expanded != nil && expanded[n.Item.ID] {
			listVisibleComments(n.Kids, expanded, out)
		}
	}
}
