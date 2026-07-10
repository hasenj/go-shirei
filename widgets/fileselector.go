package widgets

import (
	"os"
	"path/filepath"
	"strings"

	. "go.hasen.dev/shirei"
)

// FileSelectorAttrs configures FileSelector. All parameters live here —
// including the selection and optional query bindings.
type FileSelectorAttrs struct {
	// Selection is written on the accept frame (Enter or row click).
	// Required for a useful accept; if nil, accept still returns true but
	// nowhere to write.
	Selection *string

	// Query binds the filter box. Nil keeps the query in widget-local Use
	// state (fine when the caller does not need to observe typing).
	Query *string

	// Candidates is the fixed list to filter this frame. The caller owns
	// scanning, recents, DWARF, etc.
	Candidates []string

	// Root, when set, makes row labels relative to this directory (and
	// falls back to ~/… display for absolute paths outside it).
	Root string

	// ResultCount, when non-nil, is set each frame to the number of paths
	// that match the current query (before MaxResults truncation).
	ResultCount *int

	// Hint, when non-nil, is called after ranking and drawn between the
	// filter box and the list (e.g. "12 files", "no matches").
	Hint func(matchCount int) string

	// Width is the filter field's MinWidth; zero → 520.
	Width f32

	// MaxRows is the visible list viewport in rows; zero → 12.
	MaxRows int

	// MaxResults caps how many ranked rows are kept; zero → 200.
	MaxResults int
}

// fileSelectorRefilterEvery: while Candidates grow (streaming scan), re-rank
// only after this many new paths arrive — not every frame.
const fileSelectorRefilterEvery = 512

type fileSelectorState struct {
	query         string
	selected      int
	cachedQuery   string
	cachedCandN   int
	cachedRoot    string
	cachedResult  []string
	prevFrameCandN int // last frame's len(Candidates); stable ⇒ scan paused/done
}

const fileSelectorRowH f32 = 28

// FileSelector renders a filter box and a ranked path list. It is not a
// modal — callers wrap it (Modal, panel, …). Returns true on the frame the
// user accepts a row (Enter or click); that frame also writes attrs.Selection
// when non-nil.
func FileSelector(attrs FileSelectorAttrs) bool {
	if attrs.Width == 0 {
		attrs.Width = 520
	}
	if attrs.MaxRows == 0 {
		attrs.MaxRows = 12
	}
	if attrs.MaxResults == 0 {
		attrs.MaxResults = 200
	}

	st := Use[fileSelectorState]("file-selector")
	query := attrs.Query
	if query == nil {
		query = &st.query
	}

	n := len(attrs.Candidates)
	stable := n == st.prevFrameCandN // scan not appending this frame
	st.prevFrameCandN = n
	needRank := st.cachedResult == nil ||
		*query != st.cachedQuery ||
		attrs.Root != st.cachedRoot ||
		n < st.cachedCandN || // scan restarted / shrank
		n-st.cachedCandN >= fileSelectorRefilterEvery ||
		(stable && n != st.cachedCandN) // catch the last batch when BFS finishes

	if needRank {
		display := func(abs string) string {
			return fileSelectorDisplay(attrs.Root, abs)
		}
		if *query == "" {
			// empty query: preserve caller order; no scoring
			st.cachedResult = append([]string(nil), attrs.Candidates...)
		} else {
			st.cachedResult = fuzzyRankPaths(*query, attrs.Candidates, display)
		}
		queryChanged := *query != st.cachedQuery
		st.cachedQuery = *query
		st.cachedCandN = n
		st.cachedRoot = attrs.Root
		if queryChanged {
			st.selected = 0
		}
	}

	results := st.cachedResult
	if attrs.ResultCount != nil {
		*attrs.ResultCount = len(results)
	}
	if st.selected >= len(results) {
		st.selected = max(0, len(results)-1)
	}
	if st.selected < 0 {
		st.selected = 0
	}

	accepted := false
	accept := func(path string) {
		if path == "" {
			return
		}
		if attrs.Selection != nil {
			*attrs.Selection = path
		}
		accepted = true
	}

	qAttrs := DefaultTextInputAttrs()
	qAttrs.FontSize = 14
	qAttrs.MinWidth = attrs.Width
	qAttrs.NoUpDownLineEdges = true
	TextInputExt(query, qAttrs)

	limit := min(len(results), attrs.MaxResults)
	switch FrameInput.Key {
	case KeyDown:
		if st.selected+1 < limit {
			st.selected++
			VirtualListScrollIntoView(st, results[st.selected])
		}
	case KeyUp:
		if st.selected > 0 {
			st.selected--
			VirtualListScrollIntoView(st, results[st.selected])
		}
	case KeyEnter:
		if limit > 0 {
			accept(results[st.selected])
		}
	}

	if attrs.Hint != nil {
		if h := attrs.Hint(len(results)); h != "" {
			Label(h, FontSize(10), TextColor(0, 0, 55, 1))
		}
	}

	Container(Attrs(Expand, FixHeight(f32(attrs.MaxRows)*fileSelectorRowH), Clip, Background(220, 8, 98, 1), Corners(4)), func() {
		VirtualListView(st, limit,
			func(i int) any { return results[i] },
			func(i int, _ f32) f32 { return fileSelectorRowH },
			func(i int, _ f32) {
				fileSelectorRow(st, i, results[i], attrs.Root, accept)
			},
		)
	})

	return accepted
}

func fileSelectorRow(st *fileSelectorState, i int, abs, root string, accept func(string)) {
	label := fileSelectorDisplay(root, abs)
	Container(Attrs(Row, Expand, CrossMid, Pad2(5, 10), FixHeight(fileSelectorRowH), NoAnimate), func() {
		if i == st.selected {
			ModAttrs(Background(220, 45, 88, 1))
		} else if IsHovered() {
			ModAttrs(Background(220, 15, 93, 1))
		}
		if IsClicked() {
			accept(abs)
		}
		Label(label, FontSize(12), TextColor(220, 20, 22, 1))
	})
}

func fileSelectorDisplay(root, abs string) string {
	if root != "" {
		if rel, err := filepath.Rel(root, abs); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return rel
		}
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		if abs == home {
			return "~"
		}
		prefix := home + string(filepath.Separator)
		if strings.HasPrefix(abs, prefix) {
			return "~" + string(filepath.Separator) + abs[len(prefix):]
		}
	}
	return abs
}
