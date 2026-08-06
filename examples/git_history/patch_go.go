package main

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	gdiff "github.com/go-git/go-git/v6/utils/diff"
	"github.com/sergi/go-diff/diffmatchpatch"
)

// Pure-Go first-parent commit patch: per-file blob snapshot under lockRepo
// (pool checkout; exclusive use of that handle for the snapshot duration),
// line-diff unlocked with DoWithTimeout. No parent.Patch / Change.PatchContext
// and no git subprocess for commit unified diffs. Sidebar stats share the same
// snapshot path (see stats_go.go).

const (
	// commitPatchMaxBytes caps each side of a text/binary snapshot.
	commitPatchMaxBytes = 2 << 20 // 2 MiB per side
	// perFileDiffTimeout bounds pure-Go Myers on one file.
	perFileDiffTimeout = 2 * time.Second
	// streamFlushRows / streamFlushEvery batch completed body rows for UI.
	streamFlushRows  = 64
	streamFlushEvery = 8 * time.Millisecond
)

// patchPublish appends a batch of rows (or signals done). Returns false when
// the selection is no longer current — streamer must stop.
// done=true may be called with an empty batch after the last body rows.
type patchPublish func(batch []DiffRow, done bool) bool

// errStreamAbandoned means publish rejected the batch (gen/selection changed).
var errStreamAbandoned = fmt.Errorf("patch stream abandoned")

// fileSnap is an owned blob pair for one tree change (no go-git pointers).
type fileSnap struct {
	label                string
	from, to             []byte
	fromBin, toBin       bool
	fromTrunc, toTrunc   bool
	modeOnly             bool
	skip                 bool
	skipReason           string
}

// loadCommitPatchIntoGo fills doc.Rows/Segs with a pure-Go first-parent patch
// in one shot (no mid-load UI publish).
func loadCommitPatchIntoGo(ctx context.Context, repoPath, hash string, doc *DiffDoc) error {
	if doc == nil {
		return fmt.Errorf("nil doc")
	}
	var rows []DiffRow
	err := streamCommitPatchGo(ctx, repoPath, hash, func(batch []DiffRow, done bool) bool {
		if len(batch) > 0 {
			rows = append(rows, batch...)
		}
		if done {
			doc.Rows = rows
			doc.Segs = buildDiffFileSegs(doc)
		}
		return true
	})
	return err
}

// streamCommitPatchGo walks first-parent changes and publishes rows via publish.
// Lock is held only for tree resolve, DiffTree, and per-file blob snapshots.
// Line-diff runs unlocked. publish must be non-nil.
func streamCommitPatchGo(ctx context.Context, repoPath, hash string, publish patchPublish) error {
	if publish == nil {
		return fmt.Errorf("nil publish")
	}
	snaps, err := snapshotFirstParentChanges(ctx, repoPath, hash)
	if err != nil {
		return err
	}

	// Stage D: header first (instant structure), then unlocked line-diff + body.
	for _, s := range snaps {
		if err := ctx.Err(); err != nil {
			return err
		}
		header := DiffRow{Kind: RowFileHeader, Text: s.label}
		if !publish([]DiffRow{header}, false) {
			return errStreamAbandoned
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		body := bodyFromSnapshot(s)
		if !publishBodyBatched(publish, body) {
			return errStreamAbandoned
		}
	}
	if !publish(nil, true) {
		return errStreamAbandoned
	}
	return nil
}

// snapshotFirstParentChanges is stages A–C: first-parent DiffTree + owned blob
// snapshots under lockRepo, then unlock. No Myers / no DiffRows. Shared by the
// patch streamer and pure-Go sidebar stats.
func snapshotFirstParentChanges(ctx context.Context, repoPath, hash string) ([]fileSnap, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	r, unlock, err := lockRepo(repoPath)
	if err != nil {
		return nil, err
	}
	// Stage A+B: resolve trees + change list under lock.
	c, err := r.CommitObject(plumbing.NewHash(hash))
	if err != nil {
		unlock()
		return nil, err
	}
	toTree, err := c.Tree()
	if err != nil {
		unlock()
		return nil, err
	}
	var fromTree *object.Tree
	if c.NumParents() > 0 {
		parent, err := c.Parent(0)
		if err != nil {
			unlock()
			return nil, err
		}
		fromTree, err = parent.Tree()
		if err != nil {
			unlock()
			return nil, err
		}
	} else {
		fromTree = &object.Tree{}
	}

	changes, err := object.DiffTreeWithOptions(ctx, fromTree, toTree, &object.DiffTreeOptions{
		DetectRenames:    true,
		OnlyExactRenames: true,
		RenameScore:      60,
	})
	if err != nil {
		unlock()
		return nil, err
	}

	// Stage C: snapshot each change under the same lock section (short; no Myers).
	snaps := make([]fileSnap, 0, len(changes))
	for _, ch := range changes {
		if err := ctx.Err(); err != nil {
			unlock()
			return nil, err
		}
		snap, err := snapshotChange(ch)
		if err != nil {
			unlock()
			return nil, err
		}
		snaps = append(snaps, snap)
	}
	unlock()
	return snaps, nil
}

// publishBodyBatched flushes body rows in streamFlushRows / streamFlushEvery chunks.
func publishBodyBatched(publish patchPublish, body []DiffRow) bool {
	if len(body) == 0 {
		return true
	}
	last := time.Now()
	for i := 0; i < len(body); {
		end := i + streamFlushRows
		if end > len(body) {
			end = len(body)
		}
		// Also flush if enough wall time passed mid-file (large files).
		if end < len(body) && time.Since(last) < streamFlushEvery && end-i < streamFlushRows {
			// keep growing batch until rows or time — simple path: always row-based
		}
		if !publish(body[i:end], false) {
			return false
		}
		last = time.Now()
		i = end
	}
	return true
}

func snapshotChange(ch *object.Change) (fileSnap, error) {
	var s fileSnap
	from, to, err := ch.Files()
	if err != nil {
		return s, err
	}
	// Non-file (symlink/submodule/dir): Files returns nil,nil,nil.
	if from == nil && to == nil {
		s.skip = true
		s.label = changePathLabel(ch)
		s.skipReason = "non-file change"
		return s, nil
	}

	s.label = changePathLabel(ch)
	if from != nil {
		s.from, s.fromBin, s.fromTrunc, err = readFileSnapshot(from)
		if err != nil {
			return s, err
		}
	}
	if to != nil {
		s.to, s.toBin, s.toTrunc, err = readFileSnapshot(to)
		if err != nil {
			return s, err
		}
	}
	// Mode-only: same content, different mode (and both sides present).
	if from != nil && to != nil && bytesEqual(s.from, s.to) {
		if from.Mode != to.Mode {
			s.modeOnly = true
		}
	}
	return s, nil
}

func changePathLabel(ch *object.Change) string {
	fromName, toName := ch.From.Name, ch.To.Name
	if fromName == "" {
		return toName
	}
	if toName == "" {
		return fromName
	}
	if fromName != toName {
		return fromName + " → " + toName
	}
	return toName
}

func readFileSnapshot(f *object.File) (data []byte, binary, truncated bool, err error) {
	if f == nil {
		return nil, false, false, nil
	}
	// Prefer IsBinary when available (reads content); fall back to NUL scan on snapshot.
	if bin, berr := f.IsBinary(); berr == nil {
		binary = bin
	}
	rd, err := f.Reader()
	if err != nil {
		return nil, false, false, err
	}
	defer rd.Close()
	limited := io.LimitReader(rd, int64(commitPatchMaxBytes)+1)
	data, err = io.ReadAll(limited)
	if err != nil {
		return nil, false, false, err
	}
	if len(data) > commitPatchMaxBytes {
		data = data[:commitPatchMaxBytes]
		truncated = true
	}
	if !binary {
		binary = isBinaryContent(data)
	}
	// Own the slice completely (LimitReader may share buffers in theory — copy).
	out := make([]byte, len(data))
	copy(out, data)
	return out, binary, truncated, nil
}

func bytesEqual(a, b []byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func rowsFromSnapshot(s fileSnap) []DiffRow {
	h := DiffRow{Kind: RowFileHeader, Text: s.label}
	return append([]DiffRow{h}, bodyFromSnapshot(s)...)
}

// bodyFromSnapshot is unlocked pure compute after the file header is published.
func bodyFromSnapshot(s fileSnap) []DiffRow {
	if s.skip {
		return []DiffRow{{Kind: RowMeta, Text: "  (" + s.skipReason + ")"}}
	}
	if s.modeOnly {
		return []DiffRow{{Kind: RowMeta, Text: "  (mode change only)"}}
	}

	// Binary / image: no Myers.
	if s.fromBin || s.toBin {
		path := s.label
		if i := strings.Index(path, " → "); i >= 0 {
			path = path[i+len(" → "):]
		}
		if isImagePath(path) {
			return []DiffRow{{Kind: RowImage, Text: path}}
		}
		return []DiffRow{{Kind: RowMeta, Text: "Binary files differ"}}
	}

	var body []DiffRow
	if s.fromTrunc || s.toTrunc {
		body = append(body, DiffRow{
			Kind: RowMeta,
			Text: fmt.Sprintf("  … truncated (showing first %d bytes per side)", commitPatchMaxBytes),
		})
	}

	unified := formatUnifiedFile(s.label, string(s.from), string(s.to), perFileDiffTimeout)
	parsed := parsePatch(unified)
	for len(parsed) > 0 && parsed[0].Kind == RowFileHeader {
		parsed = parsed[1:]
	}
	return append(body, parsed...)
}

// unifiedContext is git's default-style context lines around each change.
const unifiedContext = 3

type lineOp struct {
	typ byte // ' ', '-', '+'
	s   string
}

// formatUnifiedFile builds unified-diff text with real hunks (not the whole file
// as one hunk). Uses DoWithTimeout then folds equal runs to ±unifiedContext.
func formatUnifiedFile(label, from, to string, timeout time.Duration) string {
	path := label
	oldPath, newPath := path, path
	if old, newP, ok := strings.Cut(label, " → "); ok {
		oldPath, newPath = old, newP
		path = newP
	}

	var b strings.Builder
	fmt.Fprintf(&b, "diff --git a/%s b/%s\n", path, path)
	if from == "" && to == "" {
		return b.String()
	}
	if from == "" {
		fmt.Fprintf(&b, "--- /dev/null\n")
		fmt.Fprintf(&b, "+++ b/%s\n", newPath)
	} else if to == "" {
		fmt.Fprintf(&b, "--- a/%s\n", oldPath)
		fmt.Fprintf(&b, "+++ /dev/null\n")
	} else {
		fmt.Fprintf(&b, "--- a/%s\n", oldPath)
		fmt.Fprintf(&b, "+++ b/%s\n", newPath)
	}

	diffs := gdiff.DoWithTimeout(from, to, timeout)
	ops := expandLineOps(diffs)
	for _, h := range groupUnifiedHunks(ops, unifiedContext) {
		fmt.Fprintf(&b, "@@ -%d,%d +%d,%d @@\n", h.oldStart, h.oldCount, h.newStart, h.newCount)
		for _, o := range h.ops {
			b.WriteByte(o.typ)
			b.WriteString(o.s)
			b.WriteByte('\n')
		}
	}

	fromNL := strings.HasSuffix(from, "\n") || from == ""
	toNL := strings.HasSuffix(to, "\n") || to == ""
	if from != "" && !fromNL {
		b.WriteString("\\ No newline at end of file\n")
	} else if to != "" && !toNL && fromNL {
		b.WriteString("\\ No newline at end of file\n")
	}
	return b.String()
}

func expandLineOps(diffs []diffmatchpatch.Diff) []lineOp {
	var ops []lineOp
	for _, d := range diffs {
		typ := byte(' ')
		switch d.Type {
		case diffmatchpatch.DiffDelete:
			typ = '-'
		case diffmatchpatch.DiffInsert:
			typ = '+'
		case diffmatchpatch.DiffEqual:
			typ = ' '
		}
		text := d.Text
		for len(text) > 0 {
			i := strings.IndexByte(text, '\n')
			if i < 0 {
				ops = append(ops, lineOp{typ, text})
				break
			}
			ops = append(ops, lineOp{typ, text[:i]})
			text = text[i+1:]
		}
	}
	return ops
}

type unifiedHunk struct {
	oldStart, oldCount int
	newStart, newCount int
	ops                []lineOp
}

// groupUnifiedHunks folds full-file equal runs down to context around changes,
// matching normal `git diff` hunk layout (default 3 lines of context).
func groupUnifiedHunks(ops []lineOp, ctx int) []unifiedHunk {
	if len(ops) == 0 {
		return nil
	}
	if ctx < 0 {
		ctx = 0
	}
	// Collect indices of changed lines.
	var ch []int
	for i, o := range ops {
		if o.typ != ' ' {
			ch = append(ch, i)
		}
	}
	if len(ch) == 0 {
		return nil
	}

	type span struct{ a, b int } // [a, b) into ops
	var spans []span
	for _, i := range ch {
		a := i - ctx
		if a < 0 {
			a = 0
		}
		b := i + ctx + 1
		if b > len(ops) {
			b = len(ops)
		}
		if len(spans) == 0 || a > spans[len(spans)-1].b {
			spans = append(spans, span{a, b})
			continue
		}
		// Overlap or touch — merge (git merges nearby changes into one hunk).
		if b > spans[len(spans)-1].b {
			spans[len(spans)-1].b = b
		}
	}

	hunks := make([]unifiedHunk, 0, len(spans))
	for _, sp := range spans {
		oldStart, newStart := 1, 1
		for i := 0; i < sp.a; i++ {
			switch ops[i].typ {
			case ' ', '-':
				oldStart++
			}
			switch ops[i].typ {
			case ' ', '+':
				newStart++
			}
		}
		oldCount, newCount := 0, 0
		for i := sp.a; i < sp.b; i++ {
			switch ops[i].typ {
			case ' ', '-':
				oldCount++
			}
			switch ops[i].typ {
			case ' ', '+':
				newCount++
			}
		}
		if oldCount == 0 {
			oldStart = 0
		}
		if newCount == 0 {
			newStart = 0
		}
		hunks = append(hunks, unifiedHunk{
			oldStart: oldStart,
			oldCount: oldCount,
			newStart: newStart,
			newCount: newCount,
			ops:      ops[sp.a:sp.b],
		})
	}
	return hunks
}

