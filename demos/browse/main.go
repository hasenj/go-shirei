// browse: two-tab network demo for desktop and iPhone.
//
//	Posts  — Hacker News top stories (Firebase API, no key)
//	Images — Lorem Picsum photo list (no key)
//
// Tap a post to expand detail. Images download in the background and appear
// via UseImage (no photo-library permissions).
package main

import (
	"encoding/json"
	"fmt"
	"image"
	"image/draw"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"sync"
	"time"

	"go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

type tabKind int

const (
	tabPosts tabKind = iota
	tabImages
)

type hnItem struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	By          string `json:"by"`
	Score       int    `json:"score"`
	URL         string `json:"url"`
	Descendants int    `json:"descendants"`
	Time        int64  `json:"time"`
	Type        string `json:"type"`
}

type picsumPhoto struct {
	ID          string `json:"id"`
	Author      string `json:"author"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	URL         string `json:"url"`
	DownloadURL string `json:"download_url"`
}

type postsState struct {
	mu      sync.Mutex
	loading bool
	err     string
	items   []hnItem
	expanded int // story id, 0 = none
}

type imagesState struct {
	mu      sync.Mutex
	loading bool
	err     string
	photos  []picsumPhoto
	// rgba: demo-owned decoded pixels. Shirei may reclaim registry ImageIds
	// when the Images tab is not drawn for several frames; we re-UseImage
	// from this map every display frame (see images.go: prefer re-UseImage).
	rgba map[string]*image.RGBA
	// inFlight avoids double-fetch
	inFlight map[string]bool
}

var (
	tab        = tabPosts
	posts      = &postsState{expanded: 0}
	images     = &imagesState{rgba: map[string]*image.RGBA{}, inFlight: map[string]bool{}}
	httpClient = &http.Client{Timeout: 20 * time.Second}
)

const (
	hnTopN       = 40
	picsumLimit  = 24
	userAgent    = "shirei-browse-demo/1.0 (+https://go.hasen.dev/shirei)"
	imageThumbW  = 400
	imageThumbH  = 280
)

func main() {
	// Kick off both feeds so the inactive tab is warm.
	go loadPosts()
	go loadImages()

	app.SetupWindow("Browse", 420, 720)
	app.Run(RootView)
}

func RootView() {
	Container(Attrs(Viewport, Expand, Background(220, 10, 97, 1)), func() {
		// Header + tabs
		Container(Attrs(Pad2(12, 14), Gap(10), Background(220, 14, 94, 1), Expand), func() {
			Label("Browse", FontSize(20), FontWeight(WeightBold), TextColor(220, 20, 18, 1))
			Label("HN posts · Picsum images — free public APIs, no login",
				FontSize(11), TextColor(0, 0, 45, 1))
			Container(Attrs(Row, CrossMid, Gap(10), Expand), func() {
				SegmentedControl(&tab,
					Cell("Posts", tabPosts),
					Cell("Images", tabImages),
				)
				Filler(1)
				if CtrlButton(NoIcon, "Refresh", true) {
					switch tab {
					case tabPosts:
						go loadPosts()
					case tabImages:
						go loadImages()
					}
				}
			})
		})

		Element(Attrs(Expand, FixHeight(1), Background(0, 0, 0, 0.08)))

		Container(Attrs(Grow(1), Expand, Clip, Viewport), func() {
			ScrollOnInput()
			ScrollBars()
			switch tab {
			case tabPosts:
				postsView()
			case tabImages:
				imagesView()
			}
		})
	})
}

// ---- Posts (Hacker News) ---------------------------------------------------

func loadPosts() {
	posts.mu.Lock()
	if posts.loading {
		posts.mu.Unlock()
		return
	}
	posts.loading = true
	posts.err = ""
	posts.mu.Unlock()
	RequestNextFrame()

	ids, err := fetchJSON[[]int]("https://hacker-news.firebaseio.com/v0/topstories.json")
	if err != nil {
		posts.mu.Lock()
		posts.loading = false
		posts.err = err.Error()
		posts.mu.Unlock()
		RequestNextFrame()
		return
	}
	if len(ids) > hnTopN {
		ids = ids[:hnTopN]
	}

	// Parallel fetch with a small worker pool.
	type result struct {
		idx  int
		item hnItem
		err  error
	}
	out := make([]hnItem, len(ids))
	ch := make(chan result, len(ids))
	sem := make(chan struct{}, 8)
	var wg sync.WaitGroup
	for i, id := range ids {
		wg.Add(1)
		go func(i, id int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			item, err := fetchJSON[hnItem](fmt.Sprintf("https://hacker-news.firebaseio.com/v0/item/%d.json", id))
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
		if r.item.Type == "story" || r.item.Title != "" {
			out[r.idx] = r.item
		}
	}
	// Compact holes from failed fetches.
	compact := out[:0]
	for _, it := range out {
		if it.ID != 0 && it.Title != "" {
			compact = append(compact, it)
		}
	}

	posts.mu.Lock()
	posts.loading = false
	posts.items = compact
	if len(compact) == 0 && firstErr != nil {
		posts.err = firstErr.Error()
	} else {
		posts.err = ""
	}
	posts.mu.Unlock()
	RequestNextFrame()
}

func postsView() {
	posts.mu.Lock()
	loading := posts.loading
	err := posts.err
	items := append([]hnItem(nil), posts.items...)
	expanded := posts.expanded
	posts.mu.Unlock()

	Container(Attrs(Pad(12), Gap(10), Expand), func() {
		if loading && len(items) == 0 {
			Label("Loading top stories…", FontSize(14), TextColor(0, 0, 45, 1))
			return
		}
		if err != "" && len(items) == 0 {
			Label("Failed to load HN: "+err, FontSize(13), TextColor(10, 70, 40, 1))
			if Button(NoIcon, "Retry") {
				go loadPosts()
			}
			return
		}
		if loading {
			Label("Refreshing…", FontSize(11), TextColor(0, 0, 50, 1))
		}
		for i := range items {
			storyCard(&items[i], expanded)
		}
		Label(fmt.Sprintf("%d stories · hacker-news.firebaseio.com", len(items)),
			FontSize(10), TextColor(0, 0, 55, 1))
	})
}

func storyCard(it *hnItem, expandedID int) {
	open := expandedID == it.ID
	Container(Attrs(Expand, Gap(6), Pad(12), Corners(10),
		Background(0, 0, 100, 1), BorderWidth(1), BorderColor(0, 0, 88, 1)), func() {
		// Large tap target for the whole header.
		Container(Attrs(Expand, Gap(4)), func() {
			Label(it.Title, FontSize(15), FontWeight(WeightSemibold), TextColor(220, 25, 18, 1))
			meta := fmt.Sprintf("%d pts · %s · %d comments", it.Score, it.By, it.Descendants)
			if it.By == "" {
				meta = fmt.Sprintf("%d pts · %d comments", it.Score, it.Descendants)
			}
			Label(meta, FontSize(11), TextColor(0, 0, 45, 1))
			if PressAction() {
				posts.mu.Lock()
				if posts.expanded == it.ID {
					posts.expanded = 0
				} else {
					posts.expanded = it.ID
				}
				posts.mu.Unlock()
			}
		})
		if open {
			Element(Attrs(Expand, FixHeight(1), Background(0, 0, 0, 0.06)))
			when := time.Unix(it.Time, 0).Local().Format("2006-01-02 15:04")
			Label("id "+fmt.Sprint(it.ID)+" · "+when, FontSize(11), TextColor(0, 0, 40, 1))
			if it.URL != "" {
				Label(it.URL, FontSize(11), TextColor(210, 50, 35, 1))
			} else {
				Label("(HN discussion — no external URL)", FontSize(11), TextColor(0, 0, 50, 1))
			}
			Label("Tap header again to collapse.", FontSize(10), TextColor(0, 0, 55, 1))
		}
	})
}

// ---- Images (Picsum) -------------------------------------------------------

func loadImages() {
	images.mu.Lock()
	if images.loading {
		images.mu.Unlock()
		return
	}
	images.loading = true
	images.err = ""
	images.mu.Unlock()
	RequestNextFrame()

	url := fmt.Sprintf("https://picsum.photos/v2/list?page=1&limit=%d", picsumLimit)
	list, err := fetchJSON[[]picsumPhoto](url)
	images.mu.Lock()
	images.loading = false
	if err != nil {
		images.err = err.Error()
		images.photos = nil
	} else {
		images.err = ""
		images.photos = list
		// New list: drop old pixels/fetches (ids may still be reclaimed by core).
		images.rgba = map[string]*image.RGBA{}
		images.inFlight = map[string]bool{}
	}
	images.mu.Unlock()
	RequestNextFrame()
}

func imagesView() {
	images.mu.Lock()
	loading := images.loading
	err := images.err
	photos := append([]picsumPhoto(nil), images.photos...)
	images.mu.Unlock()

	Container(Attrs(Pad(12), Gap(12), Expand), func() {
		if loading && len(photos) == 0 {
			Label("Loading Picsum photos…", FontSize(14), TextColor(0, 0, 45, 1))
			return
		}
		if err != "" && len(photos) == 0 {
			Label("Failed to load Picsum: "+err, FontSize(13), TextColor(10, 70, 40, 1))
			if Button(NoIcon, "Retry") {
				go loadImages()
			}
			return
		}
		if loading {
			Label("Refreshing…", FontSize(11), TextColor(0, 0, 50, 1))
		}

		// Two-column-ish: full width cards for touch.
		for i := range photos {
			photoCard(&photos[i])
		}
		Label(fmt.Sprintf("%d photos · picsum.photos", len(photos)),
			FontSize(10), TextColor(0, 0, 55, 1))
	})
}

func photoCard(p *picsumPhoto) {
	// Kick network fetch if we do not own pixels yet.
	ensurePhotoDownloaded(p)

	Container(Attrs(Expand, Gap(6), Pad(10), Corners(10),
		Background(0, 0, 100, 1), BorderWidth(1), BorderColor(0, 0, 88, 1)), func() {
		const previewH float32 = 180
		Container(Attrs(Expand, FixHeight(previewH), Clip, Corners(8),
			Background(0, 0, 92, 1), Center), func() {
			images.mu.Lock()
			rgba := images.rgba[p.ID]
			images.mu.Unlock()
			if rgba != nil {
				// Re-register every frame we show it. Core may have reclaimed
				// the ImageId while the Posts tab was visible (unused keys
				// prune after a few frames); UseImage restores the same key.
				id := UseImage("picsum:"+p.ID, rgba)
				ImageView(id, Vec2{380, previewH})
			} else {
				Label("loading…", FontSize(12), TextColor(0, 0, 50, 1))
			}
		})
		Label(fmt.Sprintf("#%s · %s", p.ID, p.Author), FontSize(12), FontWeight(WeightSemibold))
		Label(fmt.Sprintf("%d×%d", p.Width, p.Height), FontSize(10), TextColor(0, 0, 50, 1))
	})
}

func ensurePhotoDownloaded(p *picsumPhoto) {
	images.mu.Lock()
	if images.rgba[p.ID] != nil || images.inFlight[p.ID] {
		images.mu.Unlock()
		return
	}
	images.inFlight[p.ID] = true
	images.mu.Unlock()

	go func(id string) {
		// Sized URL is much smaller than full download_url.
		url := fmt.Sprintf("https://picsum.photos/id/%s/%d/%d", id, imageThumbW, imageThumbH)
		rgba, err := fetchImageRGBA(url)
		images.mu.Lock()
		delete(images.inFlight, id)
		if err == nil && rgba != nil {
			images.rgba[id] = rgba
		}
		images.mu.Unlock()
		if err == nil {
			RequestNextFrame()
		}
	}(p.ID)
}

func fetchImageRGBA(url string) (*image.RGBA, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	// Limit decode size.
	img, _, err := image.Decode(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, err
	}
	return toRGBA(img), nil
}

func toRGBA(src image.Image) *image.RGBA {
	if src == nil {
		return nil
	}
	if r, ok := src.(*image.RGBA); ok {
		return r
	}
	b := src.Bounds()
	dst := image.NewRGBA(b)
	draw.Draw(dst, b, src, b.Min, draw.Src)
	return dst
}

// ---- HTTP helpers ----------------------------------------------------------

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
