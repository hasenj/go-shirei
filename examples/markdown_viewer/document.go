package main

import (
	"bytes"
	"fmt"
	"strings"
	"sync/atomic"
	"unicode"
	"unicode/utf8"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
	"github.com/yuin/goldmark/util"
)

// nextDisplayItemID mints opaque DisplayItem.ItemID values process-wide.
// IDs are never reused for different row lifetimes (full re-parse mints new
// ones; adoptItemIdentities may copy prior IDs onto an equal prefix).
var nextDisplayItemID atomic.Uint64

// Display item kinds for the virtualized markdown surface.
type ItemKind int

const (
	KindParagraph ItemKind = iota
	KindHeading
	KindCodeLine
	KindThematicBreak
	KindTableRow
)

func (k ItemKind) String() string {
	switch k {
	case KindParagraph:
		return "paragraph"
	case KindHeading:
		return "heading"
	case KindCodeLine:
		return "codeLine"
	case KindThematicBreak:
		return "thematicBreak"
	case KindTableRow:
		return "tableRow"
	default:
		return fmt.Sprintf("kind(%d)", int(k))
	}
}

// Chrome flags describe continuous block chrome and optional spacing.
type ChromeFlags uint32

const (
	ChromeCodeFirst ChromeFlags = 1 << iota
	ChromeCodeMid
	ChromeCodeLast
	ChromeSpaceBefore
	ChromeTableFirst
	ChromeTableMid
	ChromeTableLast
	ChromeTableHeader
)

// CellAlign is GFM table column alignment.
type CellAlign int

const (
	CellAlignDefault CellAlign = iota
	CellAlignLeft
	CellAlignCenter
	CellAlignRight
)

// TableCell is one cell inside a KindTableRow item.
type TableCell struct {
	Text  string
	Spans []ItemSpan
	Links []LinkSpan
	Align CellAlign
}

// ItemSpan is a rune range on DisplayItem.Text with style deltas.
type ItemSpan struct {
	From, To int
	Emph     bool
	Strong   bool
	Code     bool
}

// LinkSpan is a rune range on DisplayItem.Text with a link target.
type LinkSpan struct {
	From, To int
	Target   string
}

// DisplayItem is one virtual-list row after Markdown lowering.
type DisplayItem struct {
	// ItemID is an opaque row identity minted at lower time. Stable across
	// republish when adoptItemIdentities reuses a content-equal prefix (or
	// bumps ItemGen for an in-place tip/edit). Not a list index and not a
	// hash of the markdown text.
	ItemID uint64
	// ItemGen increments when the same ItemID's content is rewritten (growing
	// stream tip, mid-doc edit). Sealed/unchanged rows keep ItemGen fixed.
	ItemGen uint64

	Kind         ItemKind
	Text         string
	Spans        []ItemSpan
	Links        []LinkSpan
	Cells        []TableCell // KindTableRow
	Indent       float32
	QuoteDepth   int
	Marker       string
	HeadingLevel int
	Monospace    bool
	Chrome       ChromeFlags
	ContextID    int
	SourceFrom   int // 1-based source line; 0 if unknown
	SourceTo     int // exclusive
}

// Document is an immutable published view of a lowered Markdown file.
type Document struct {
	Path       string
	Source     []byte
	Items      []DisplayItem
	Fragments  map[string]int // fragment id → item index
	PlainText  string
	Generation uint64
}

const (
	listIndentStep  float32 = 22
	quoteIndentStep float32 = 0 // quote depth is painted as bars; indent stays list-only
)

var mdParser = goldmark.New(
	goldmark.WithExtensions(extension.Table),
).Parser()

// ParseDocument parses Markdown source and lowers it to a flat item stream.
// Each item receives a fresh ItemID. Pass prev into adoptItemIdentities when
// republishing so an unchanged prefix keeps IDs (and a rewritten tip bumps
// ItemGen) for height-cache stability.
func ParseDocument(path string, source []byte, generation uint64) *Document {
	root := mdParser.Parse(text.NewReader(source))
	lx := &lowerer{
		source:    source,
		fragments: make(map[string]int),
		fragUsed:  make(map[string]int),
	}
	lx.lowerBlocks(root)
	doc := &Document{
		Path:       path,
		Source:     bytes.Clone(source),
		Items:      lx.items,
		Fragments:  lx.fragments,
		PlainText:  buildPlainText(lx.items),
		Generation: generation,
	}
	return doc
}

// adoptItemIdentities copies row identities from prev onto next for a leading
// content-equal prefix. The first differing row keeps prev's ItemID and bumps
// ItemGen (growing tip / in-place edit). Further new rows keep the IDs minted
// during parse. next is updated in place.
func adoptItemIdentities(prev, next []DisplayItem) {
	i := 0
	for i < len(prev) && i < len(next) && displayItemContentEqual(&prev[i], &next[i]) {
		next[i].ItemID = prev[i].ItemID
		next[i].ItemGen = prev[i].ItemGen
		i++
	}
	if i < len(next) && i < len(prev) {
		next[i].ItemID = prev[i].ItemID
		next[i].ItemGen = prev[i].ItemGen + 1
	}
}

// displayItemContentEqual reports whether a and b are the same display row
// ignoring identity fields (ItemID / ItemGen).
func displayItemContentEqual(a, b *DisplayItem) bool {
	if a.Kind != b.Kind ||
		a.Text != b.Text ||
		a.Indent != b.Indent ||
		a.QuoteDepth != b.QuoteDepth ||
		a.Marker != b.Marker ||
		a.HeadingLevel != b.HeadingLevel ||
		a.Monospace != b.Monospace ||
		a.Chrome != b.Chrome ||
		a.ContextID != b.ContextID ||
		a.SourceFrom != b.SourceFrom ||
		a.SourceTo != b.SourceTo ||
		len(a.Spans) != len(b.Spans) ||
		len(a.Links) != len(b.Links) ||
		len(a.Cells) != len(b.Cells) {
		return false
	}
	for i := range a.Spans {
		if a.Spans[i] != b.Spans[i] {
			return false
		}
	}
	for i := range a.Links {
		if a.Links[i] != b.Links[i] {
			return false
		}
	}
	for i := range a.Cells {
		if !tableCellEqual(&a.Cells[i], &b.Cells[i]) {
			return false
		}
	}
	return true
}

func tableCellEqual(a, b *TableCell) bool {
	if a.Text != b.Text || a.Align != b.Align ||
		len(a.Spans) != len(b.Spans) || len(a.Links) != len(b.Links) {
		return false
	}
	for i := range a.Spans {
		if a.Spans[i] != b.Spans[i] {
			return false
		}
	}
	for i := range a.Links {
		if a.Links[i] != b.Links[i] {
			return false
		}
	}
	return true
}

type lowerer struct {
	source       []byte
	items        []DisplayItem
	fragments    map[string]int
	fragUsed     map[string]int
	quoteDepth   int
	listDepth    int
	nextContext  int
	pendingMark  string
	needSpace    bool // next content item gets ChromeSpaceBefore
}

func (lx *lowerer) push(item DisplayItem) int {
	item.ItemID = nextDisplayItemID.Add(1)
	item.ItemGen = 0
	lx.items = append(lx.items, item)
	return len(lx.items) - 1
}

func (lx *lowerer) lowerBlocks(n ast.Node) {
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		lx.lowerBlock(c)
	}
}

func (lx *lowerer) lowerBlock(n ast.Node) {
	switch n.Kind() {
	case ast.KindParagraph, ast.KindTextBlock:
		lx.emitInlineBlock(KindParagraph, 0, n)
	case ast.KindHeading:
		h := n.(*ast.Heading)
		idx := lx.emitInlineBlock(KindHeading, h.Level, n)
		if idx >= 0 {
			plain := lx.items[idx].Text
			frag := lx.uniqueFragment(slugify(plain))
			lx.fragments[frag] = idx
		}
	case ast.KindThematicBreak:
		item := DisplayItem{
			Kind: KindThematicBreak,
		}
		if lx.needSpace {
			item.Chrome |= ChromeSpaceBefore
		}
		item.QuoteDepth = lx.quoteDepth
		item.Indent = float32(lx.listDepth) * listIndentStep
		lx.takeMarker(&item)
		lx.push(item)
		lx.needSpace = true
	case ast.KindBlockquote:
		lx.quoteDepth++
		lx.lowerBlocks(n)
		lx.quoteDepth--
		lx.needSpace = true
	case ast.KindList:
		list := n.(*ast.List)
		lx.listDepth++
		i := 0
		for c := n.FirstChild(); c != nil; c = c.NextSibling() {
			marker := listMarker(list, i)
			lx.pendingMark = marker
			lx.lowerBlocks(c) // ListItem children
			lx.pendingMark = ""
			i++
		}
		lx.listDepth--
		lx.needSpace = true
	case ast.KindListItem:
		lx.lowerBlocks(n)
	case ast.KindFencedCodeBlock, ast.KindCodeBlock:
		lx.emitCodeBlock(n)
	case east.KindTable:
		lx.emitTable(n.(*east.Table))
	case ast.KindHTMLBlock:
		// v1: raw HTML blocks are omitted
	default:
		if n.Type() == ast.TypeBlock {
			lx.lowerBlocks(n)
		}
	}
}

func listMarker(list *ast.List, index int) string {
	if list.IsOrdered() {
		n := list.Start
		if n <= 0 {
			n = 1
		}
		return fmt.Sprintf("%d.", n+index)
	}
	switch list.Marker {
	case '+':
		return "+"
	case '*':
		return "*"
	default:
		return "-"
	}
}

func (lx *lowerer) takeMarker(item *DisplayItem) {
	if lx.pendingMark == "" {
		return
	}
	item.Marker = lx.pendingMark
	lx.pendingMark = ""
}

func (lx *lowerer) emitInlineBlock(kind ItemKind, headingLevel int, n ast.Node) int {
	out := lowerInlines(n, lx.source)
	if out.text == "" && kind == KindParagraph && lx.pendingMark == "" {
		// Skip empty paragraphs that carry no marker.
		return -1
	}
	item := DisplayItem{
		Kind:         kind,
		Text:         out.text,
		Spans:        out.spans,
		Links:        out.links,
		Indent:       float32(lx.listDepth)*listIndentStep + float32(lx.quoteDepth)*quoteIndentStep,
		QuoteDepth:   lx.quoteDepth,
		HeadingLevel: headingLevel,
	}
	if lx.needSpace {
		item.Chrome |= ChromeSpaceBefore
		lx.needSpace = false
	}
	lx.takeMarker(&item)
	from, to := sourceLineRange(n, lx.source)
	item.SourceFrom = from
	item.SourceTo = to
	idx := lx.push(item)
	lx.needSpace = true
	return idx
}

func (lx *lowerer) emitCodeBlock(n ast.Node) {
	lines := n.Lines()
	nLines := lines.Len()
	if nLines == 0 {
		// Still emit a single empty code line so fences are visible.
		nLines = 1
	}
	ctx := lx.nextContext
	lx.nextContext++
	indent := float32(lx.listDepth)*listIndentStep + float32(lx.quoteDepth)*quoteIndentStep
	markerTaken := false
	for i := 0; i < nLines; i++ {
		var lineText string
		var from, to int
		if lines.Len() > 0 {
			seg := lines.At(i)
			raw := seg.Value(lx.source)
			raw = bytes.TrimRight(raw, "\n\r")
			lineText = string(raw)
			from = lineNumberAt(lx.source, seg.Start)
			to = from + 1
		}
		item := DisplayItem{
			Kind:       KindCodeLine,
			Text:       lineText,
			Indent:     indent,
			QuoteDepth: lx.quoteDepth,
			Monospace:  true,
			ContextID:  ctx,
			SourceFrom: from,
			SourceTo:   to,
		}
		if i == 0 {
			item.Chrome |= ChromeCodeFirst
			if lx.needSpace {
				item.Chrome |= ChromeSpaceBefore
			}
			if !markerTaken {
				lx.takeMarker(&item)
				markerTaken = true
			}
		}
		if i == nLines-1 {
			item.Chrome |= ChromeCodeLast
		}
		if i > 0 && i < nLines-1 {
			item.Chrome |= ChromeCodeMid
		}
		if nLines == 1 {
			item.Chrome |= ChromeCodeFirst | ChromeCodeLast
		}
		lx.push(item)
	}
	lx.needSpace = true
	lx.pendingMark = "" // in case marker was never consumed
}

func (lx *lowerer) emitTable(table *east.Table) {
	type rowData struct {
		header bool
		cells  []TableCell
	}
	var rows []rowData
	for c := table.FirstChild(); c != nil; c = c.NextSibling() {
		switch c.Kind() {
		case east.KindTableHeader:
			rows = append(rows, rowData{header: true, cells: lowerTableCells(c, lx.source)})
		case east.KindTableRow:
			rows = append(rows, rowData{header: false, cells: lowerTableCells(c, lx.source)})
		}
	}
	if len(rows) == 0 {
		return
	}
	ctx := lx.nextContext
	lx.nextContext++
	indent := float32(lx.listDepth)*listIndentStep + float32(lx.quoteDepth)*quoteIndentStep
	for i, row := range rows {
		item := DisplayItem{
			Kind:       KindTableRow,
			Cells:      row.cells,
			Indent:     indent,
			QuoteDepth: lx.quoteDepth,
			ContextID:  ctx,
		}
		if row.header {
			item.Chrome |= ChromeTableHeader
		}
		if i == 0 {
			item.Chrome |= ChromeTableFirst
			if lx.needSpace {
				item.Chrome |= ChromeSpaceBefore
			}
			lx.takeMarker(&item)
		}
		if i == len(rows)-1 {
			item.Chrome |= ChromeTableLast
		}
		if i > 0 && i < len(rows)-1 {
			item.Chrome |= ChromeTableMid
		}
		if len(rows) == 1 {
			item.Chrome |= ChromeTableFirst | ChromeTableLast
		}
		// Plain-text fallback: pipe-joined cell text on the item.
		item.Text = joinCellText(row.cells)
		lx.push(item)
	}
	lx.needSpace = true
	lx.pendingMark = ""
}

func lowerTableCells(row ast.Node, source []byte) []TableCell {
	var cells []TableCell
	for c := row.FirstChild(); c != nil; c = c.NextSibling() {
		if c.Kind() != east.KindTableCell {
			continue
		}
		tc := c.(*east.TableCell)
		out := lowerInlines(tc, source)
		cells = append(cells, TableCell{
			Text:  out.text,
			Spans: out.spans,
			Links: out.links,
			Align: mapCellAlign(tc.Alignment),
		})
	}
	return cells
}

func mapCellAlign(a east.Alignment) CellAlign {
	switch a {
	case east.AlignLeft:
		return CellAlignLeft
	case east.AlignRight:
		return CellAlignRight
	case east.AlignCenter:
		return CellAlignCenter
	default:
		return CellAlignDefault
	}
}

func joinCellText(cells []TableCell) string {
	parts := make([]string, len(cells))
	for i, c := range cells {
		parts[i] = c.Text
	}
	return strings.Join(parts, " | ")
}

func (lx *lowerer) uniqueFragment(base string) string {
	if base == "" {
		base = "section"
	}
	n := lx.fragUsed[base]
	lx.fragUsed[base] = n + 1
	if n == 0 {
		return base
	}
	return fmt.Sprintf("%s-%d", base, n)
}

type inlineResult struct {
	text  string
	spans []ItemSpan
	links []LinkSpan
}

type inlineStyle struct {
	emph   bool
	strong bool
	code   bool
}

func lowerInlines(n ast.Node, source []byte) inlineResult {
	var b strings.Builder
	var spans []ItemSpan
	var links []LinkSpan
	runeCount := 0
	appendRunes := func(s string) (from, to int) {
		from = runeCount
		b.WriteString(s)
		runeCount += utf8.RuneCountInString(s)
		return from, runeCount
	}
	appendBytes := func(p []byte) (from, to int) {
		return appendRunes(string(p))
	}
	noteStyle := func(from, to int, st inlineStyle, linkTarget string) {
		if from >= to {
			return
		}
		if st.emph || st.strong || st.code {
			spans = append(spans, ItemSpan{
				From: from, To: to,
				Emph: st.emph, Strong: st.strong, Code: st.code,
			})
		}
		if linkTarget != "" {
			links = append(links, LinkSpan{From: from, To: to, Target: linkTarget})
		}
	}
	var walk func(ast.Node, inlineStyle, string)
	walk = func(node ast.Node, st inlineStyle, linkTarget string) {
		for c := node.FirstChild(); c != nil; c = c.NextSibling() {
			switch c.Kind() {
			case ast.KindText:
				t := c.(*ast.Text)
				var tmp strings.Builder
				writeTextValue(&tmp, t, source)
				from, to := appendRunes(tmp.String())
				noteStyle(from, to, st, linkTarget)
				if t.HardLineBreak() {
					appendRunes("\n")
				} else if t.SoftLineBreak() {
					appendRunes(" ")
				}
			case ast.KindString:
				s := c.(*ast.String)
				from, to := appendBytes(s.Value)
				nst := st
				if s.IsCode() {
					nst.code = true
				}
				noteStyle(from, to, nst, linkTarget)
			case ast.KindEmphasis:
				em := c.(*ast.Emphasis)
				nst := st
				if em.Level >= 2 {
					nst.strong = true
				} else {
					nst.emph = true
				}
				walk(c, nst, linkTarget)
			case ast.KindCodeSpan:
				var tmp strings.Builder
				for t := c.FirstChild(); t != nil; t = t.NextSibling() {
					if tn, ok := t.(*ast.Text); ok {
						seg := tn.Segment.Value(source)
						if bytes.HasSuffix(seg, []byte("\n")) {
							tmp.Write(seg[:len(seg)-1])
							tmp.WriteByte(' ')
						} else {
							tmp.Write(seg)
						}
					}
				}
				from, to := appendRunes(tmp.String())
				nst := st
				nst.code = true
				noteStyle(from, to, nst, linkTarget)
			case ast.KindLink:
				link := c.(*ast.Link)
				walk(c, st, string(link.Destination))
			case ast.KindAutoLink:
				al := c.(*ast.AutoLink)
				label := string(al.Label(source))
				target := string(al.URL(source))
				if al.AutoLinkType == ast.AutoLinkEmail && !strings.HasPrefix(strings.ToLower(target), "mailto:") {
					target = "mailto:" + label
				}
				from, to := appendRunes(label)
				noteStyle(from, to, st, target)
			case ast.KindImage:
				// v1: render alt text only, no image chrome
				walk(c, st, linkTarget)
			case ast.KindRawHTML:
				// v1: omit raw HTML
			default:
				walk(c, st, linkTarget)
			}
		}
	}
	walk(n, inlineStyle{}, "")
	return inlineResult{text: b.String(), spans: spans, links: links}
}

func writeTextValue(b *strings.Builder, t *ast.Text, source []byte) {
	v := t.Segment.Value(source)
	if t.IsRaw() {
		b.Write(v)
		return
	}
	v = util.UnescapePunctuations(v)
	v = util.ResolveNumericReferences(v)
	v = util.ResolveEntityNames(v)
	b.Write(v)
}

func slugify(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	lastHyphen := false
	for _, r := range s {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			lastHyphen = false
		case r == ' ' || r == '-' || r == '_':
			if !lastHyphen && b.Len() > 0 {
				b.WriteByte('-')
				lastHyphen = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	return out
}

func sourceLineRange(n ast.Node, source []byte) (from, to int) {
	lines := n.Lines()
	if lines == nil || lines.Len() == 0 {
		return 0, 0
	}
	first := lines.At(0)
	last := lines.At(lines.Len() - 1)
	from = lineNumberAt(source, first.Start)
	to = lineNumberAt(source, max(0, last.Stop-1)) + 1
	return from, to
}

func lineNumberAt(source []byte, offset int) int {
	if offset < 0 {
		offset = 0
	}
	if offset > len(source) {
		offset = len(source)
	}
	n := 1
	for i := 0; i < offset; i++ {
		if source[i] == '\n' {
			n++
		}
	}
	return n
}

func buildPlainText(items []DisplayItem) string {
	var b strings.Builder
	for i, item := range items {
		if i > 0 {
			switch {
			case item.Kind == KindCodeLine && i > 0 && items[i-1].Kind == KindCodeLine &&
				items[i-1].ContextID == item.ContextID:
				b.WriteByte('\n')
			default:
				b.WriteByte('\n')
				if item.Chrome&ChromeSpaceBefore != 0 {
					b.WriteByte('\n')
				}
			}
		}
		if item.Marker != "" {
			b.WriteString(item.Marker)
			b.WriteByte(' ')
		}
		switch item.Kind {
		case KindThematicBreak:
			b.WriteString("---")
		case KindHeading:
			for level := 0; level < item.HeadingLevel; level++ {
				b.WriteByte('#')
			}
			if item.HeadingLevel > 0 && item.Text != "" {
				b.WriteByte(' ')
			}
			b.WriteString(item.Text)
		case KindTableRow:
			if len(item.Cells) > 0 {
				b.WriteString("| ")
				b.WriteString(joinCellText(item.Cells))
				b.WriteString(" |")
			} else {
				b.WriteString(item.Text)
			}
		default:
			b.WriteString(item.Text)
		}
	}
	if b.Len() > 0 {
		b.WriteByte('\n')
	}
	return b.String()
}
