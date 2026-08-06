package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSoftAndHardBreaks(t *testing.T) {
	src := []byte("soft\nbreak here.\n\nhard\\\nbreak here.\n")
	doc := ParseDocument("breaks.md", src, 1)
	var texts []string
	for _, it := range doc.Items {
		if it.Kind == KindParagraph {
			texts = append(texts, it.Text)
		}
	}
	if len(texts) != 2 {
		t.Fatalf("paragraphs: got %d %#v", len(texts), texts)
	}
	if texts[0] != "soft break here." {
		t.Fatalf("soft break: got %q", texts[0])
	}
	if texts[1] != "hard\nbreak here." {
		t.Fatalf("hard break: got %q", texts[1])
	}
}

func TestNestedEmphasisAndLinkRanges(t *testing.T) {
	src := []byte("Say ***hello* world** and [go](https://example.com).\n")
	doc := ParseDocument("inline.md", src, 1)
	if len(doc.Items) != 1 {
		t.Fatalf("items: %d", len(doc.Items))
	}
	it := doc.Items[0]
	if !strings.Contains(it.Text, "hello") || !strings.Contains(it.Text, "go") {
		t.Fatalf("text: %q", it.Text)
	}
	var hasEmph, hasStrong, hasLink bool
	for _, sp := range it.Spans {
		seg := []rune(it.Text)[sp.From:sp.To]
		if string(seg) == "hello" && sp.Emph && sp.Strong {
			hasEmph = true
		}
		if strings.Contains(string(seg), "world") && sp.Strong {
			hasStrong = true
		}
	}
	for _, lk := range it.Links {
		seg := string([]rune(it.Text)[lk.From:lk.To])
		if seg == "go" && lk.Target == "https://example.com" {
			hasLink = true
		}
	}
	if !hasEmph || !hasStrong || !hasLink {
		t.Fatalf("styles/links missing emph=%v strong=%v link=%v spans=%+v links=%+v text=%q",
			hasEmph, hasStrong, hasLink, it.Spans, it.Links, it.Text)
	}
}

func TestListMarkerOnlyOnFirstContent(t *testing.T) {
	src := []byte("1. First paragraph.\n\n   Continuation under the same item.\n")
	doc := ParseDocument("list.md", src, 1)
	var paras []DisplayItem
	for _, it := range doc.Items {
		if it.Kind == KindParagraph {
			paras = append(paras, it)
		}
	}
	if len(paras) != 2 {
		t.Fatalf("paragraphs: %d %#v", len(paras), itemSummary(doc.Items))
	}
	if paras[0].Marker != "1." {
		t.Fatalf("first marker: %q", paras[0].Marker)
	}
	if paras[1].Marker != "" {
		t.Fatalf("continuation marker should be empty, got %q", paras[1].Marker)
	}
	if paras[0].Indent != paras[1].Indent || paras[0].Indent != listIndentStep {
		t.Fatalf("indent mismatch: %v %v", paras[0].Indent, paras[1].Indent)
	}
}

func TestQuoteDepthAndCodeLines(t *testing.T) {
	src := []byte("> quote\n>\n> ```\n> a\n> b\n> ```\n")
	doc := ParseDocument("quote.md", src, 1)
	var quoteParas, codeLines int
	var ctx int = -1
	for _, it := range doc.Items {
		if it.QuoteDepth != 1 {
			t.Fatalf("quote depth want 1, item=%s depth=%d text=%q", it.Kind, it.QuoteDepth, it.Text)
		}
		switch it.Kind {
		case KindParagraph:
			quoteParas++
		case KindCodeLine:
			codeLines++
			if ctx < 0 {
				ctx = it.ContextID
			} else if it.ContextID != ctx {
				t.Fatalf("code context mismatch")
			}
		}
	}
	if quoteParas < 1 {
		t.Fatalf("expected quoted paragraph")
	}
	if codeLines != 2 {
		t.Fatalf("code lines: got %d summary=%v", codeLines, itemSummary(doc.Items))
	}
}

func TestHeadingFragments(t *testing.T) {
	src := []byte("# Hello World\n\n## Hello World\n")
	doc := ParseDocument("frag.md", src, 1)
	if doc.Fragments["hello-world"] != 0 {
		t.Fatalf("first fragment: %v", doc.Fragments)
	}
	if doc.Fragments["hello-world-1"] != 1 {
		t.Fatalf("second fragment: %v", doc.Fragments)
	}
}

func TestShowcaseFixture(t *testing.T) {
	path := filepath.Join("testdata", "showcase.md")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	doc := ParseDocument(path, src, 1)
	if len(doc.Items) < 10 {
		t.Fatalf("too few items: %d %v", len(doc.Items), itemSummary(doc.Items))
	}

	// Nested quote → list → paragraph → link → code fence
	var sawQuotedListLink bool
	var codeInQuote int
	var codeCtx = -1
	for _, it := range doc.Items {
		if it.QuoteDepth > 0 && it.Marker != "" && len(it.Links) > 0 {
			sawQuotedListLink = true
		}
		if it.Kind == KindParagraph && it.QuoteDepth > 0 {
			for _, lk := range it.Links {
				if lk.Target == "https://example.com" {
					sawQuotedListLink = true
				}
			}
		}
		if it.Kind == KindCodeLine && it.QuoteDepth > 0 {
			codeInQuote++
			if codeCtx < 0 {
				codeCtx = it.ContextID
			} else if it.ContextID != codeCtx {
				t.Fatalf("quoted code ContextID mismatch")
			}
		}
	}
	if !sawQuotedListLink {
		t.Fatalf("expected link inside quoted region; items=%v", itemSummary(doc.Items))
	}
	if codeInQuote < 2 {
		t.Fatalf("expected fenced code lines inside quote, got %d; items=%v", codeInQuote, itemSummary(doc.Items))
	}
	if _, ok := doc.Fragments["showcase"]; !ok {
		t.Fatalf("missing showcase fragment: %v", doc.Fragments)
	}
	if _, ok := doc.Fragments["fragment-target"]; !ok {
		t.Fatalf("missing fragment-target: %v", doc.Fragments)
	}

	var thematic bool
	for _, it := range doc.Items {
		if it.Kind == KindThematicBreak {
			thematic = true
		}
	}
	if !thematic {
		t.Fatalf("missing thematic break")
	}
}

func TestLinkTargetAtTrailingWhitespace(t *testing.T) {
	text := "see link here"
	links := []LinkSpan{{From: 4, To: 8, Target: "https://example.com"}} // "link"
	if target, ok := linkTargetAt(links, 4); !ok || target != "https://example.com" {
		t.Fatalf("link start: ok=%v target=%q", ok, target)
	}
	if target, ok := linkTargetAt(links, 7); !ok || target != "https://example.com" {
		t.Fatalf("link end-1: ok=%v target=%q", ok, target)
	}
	// Index on trailing text / past link must not activate.
	if _, ok := linkTargetAt(links, 8); ok {
		t.Fatalf("index at link.To should not activate")
	}
	if _, ok := linkTargetAt(links, 12); ok {
		t.Fatalf("trailing text should not activate")
	}
	if _, ok := linkTargetAt(links, len([]rune(text))); ok {
		t.Fatalf("past end should not activate")
	}
}

func TestGFMTableRows(t *testing.T) {
	src := []byte("" +
		"| Name | Score |\n" +
		"| :--- | ----: |\n" +
		"| Ada  | 10    |\n" +
		"| **Bob** | `9` |\n")
	doc := ParseDocument("table.md", src, 1)
	var rows []DisplayItem
	for _, it := range doc.Items {
		if it.Kind == KindTableRow {
			rows = append(rows, it)
		}
	}
	if len(rows) != 3 {
		t.Fatalf("rows: got %d summary=%v", len(rows), itemSummary(doc.Items))
	}
	if rows[0].Chrome&ChromeTableHeader == 0 || rows[0].Chrome&ChromeTableFirst == 0 {
		t.Fatalf("header chrome: %v", rows[0].Chrome)
	}
	if rows[2].Chrome&ChromeTableLast == 0 {
		t.Fatalf("last chrome missing")
	}
	ctx := rows[0].ContextID
	for i, r := range rows {
		if r.ContextID != ctx {
			t.Fatalf("row %d context mismatch", i)
		}
		if len(r.Cells) != 2 {
			t.Fatalf("row %d cells: %d", i, len(r.Cells))
		}
	}
	if rows[0].Cells[0].Align != CellAlignLeft || rows[0].Cells[1].Align != CellAlignRight {
		t.Fatalf("alignments: %+v %+v", rows[0].Cells[0].Align, rows[0].Cells[1].Align)
	}
	if rows[0].Cells[0].Text != "Name" || rows[1].Cells[0].Text != "Ada" {
		t.Fatalf("cell text: %q %q", rows[0].Cells[0].Text, rows[1].Cells[0].Text)
	}
	bob := rows[2].Cells[0]
	if !strings.Contains(bob.Text, "Bob") {
		t.Fatalf("bob text: %q", bob.Text)
	}
	var strong bool
	for _, sp := range bob.Spans {
		if sp.Strong {
			strong = true
		}
	}
	if !strong {
		t.Fatalf("expected strong span in bob cell: %+v", bob.Spans)
	}
	score := rows[2].Cells[1]
	var code bool
	for _, sp := range score.Spans {
		if sp.Code {
			code = true
		}
	}
	if !code || score.Text != "9" {
		t.Fatalf("code cell: text=%q spans=%+v", score.Text, score.Spans)
	}
}

func TestStaleParseDiscarded(t *testing.T) {
	prev := ParseDocument("a.md", []byte("# A\n"), 1)
	next := ParseDocument("a.md", []byte("# B\n"), 2)
	doc, scroll, ok := publishResult(3, 2, prev, next, 0)
	if ok || doc != prev || scroll != -1 {
		t.Fatalf("stale parse accepted: ok=%v scroll=%d", ok, scroll)
	}
}

func TestLongCodeBlockVirtualized(t *testing.T) {
	var b strings.Builder
	b.WriteString("```\n")
	for i := 0; i < 400; i++ {
		fmt.Fprintf(&b, "line %d\n", i)
	}
	b.WriteString("```\n")
	doc := ParseDocument("long.md", []byte(b.String()), 1)
	if len(doc.Items) != 400 {
		t.Fatalf("code lines: got %d", len(doc.Items))
	}
	ctx := doc.Items[0].ContextID
	for i, it := range doc.Items {
		if it.Kind != KindCodeLine || it.ContextID != ctx {
			t.Fatalf("item %d: kind=%s ctx=%d", i, it.Kind, it.ContextID)
		}
	}
	if doc.Items[0].Chrome&ChromeCodeFirst == 0 || doc.Items[399].Chrome&ChromeCodeLast == 0 {
		t.Fatalf("missing first/last chrome flags")
	}
}

func TestScrollPolicyOnPublish(t *testing.T) {
	prev := ParseDocument("a.md", []byte("# A\n\npara\n\n## B\n"), 1)
	next := ParseDocument("a.md", []byte("# A\n\npara\n\n## B\n\nmore\n"), 2)
	doc, scroll, ok := publishResult(2, 2, prev, next, 2)
	if !ok || doc != next || scroll != 2 {
		t.Fatalf("same-path keep: ok=%v scroll=%d", ok, scroll)
	}

	other := ParseDocument("b.md", []byte("# Other\n"), 3)
	doc, scroll, ok = publishResult(3, 3, next, other, 2)
	if !ok || doc != other || scroll != 0 {
		t.Fatalf("path change reset: ok=%v scroll=%d", ok, scroll)
	}

	_, scroll, ok = publishResult(4, 4, other, ParseDocument("b.md", []byte("# X\n"), 4), 99)
	if !ok || scroll != 0 {
		t.Fatalf("clamp: scroll=%d", scroll)
	}
}

func TestItemIdentities(t *testing.T) {
	a := ParseDocument("a.md", []byte("# A\n\npara\n"), 1)
	if len(a.Items) < 2 {
		t.Fatalf("items: %d", len(a.Items))
	}
	seen := map[uint64]bool{}
	for i, it := range a.Items {
		if it.ItemID == 0 {
			t.Fatalf("item %d: ItemID unset", i)
		}
		if it.ItemGen != 0 {
			t.Fatalf("item %d: ItemGen=%d want 0", i, it.ItemGen)
		}
		if seen[it.ItemID] {
			t.Fatalf("duplicate ItemID %d", it.ItemID)
		}
		seen[it.ItemID] = true
	}

	b := ParseDocument("a.md", []byte("# A\n\npara\n"), 2)
	// Fresh parse mints new IDs (no false cache sharing across unrelated publishes).
	if b.Items[0].ItemID == a.Items[0].ItemID {
		t.Fatalf("independent parses reused ItemID %d", a.Items[0].ItemID)
	}

	// Equal republish via adopt: restore prior IDs/gens.
	adoptItemIdentities(a.Items, b.Items)
	for i := range a.Items {
		if b.Items[i].ItemID != a.Items[i].ItemID || b.Items[i].ItemGen != a.Items[i].ItemGen {
			t.Fatalf("adopt equal: item %d id/gen %d/%d want %d/%d",
				i, b.Items[i].ItemID, b.Items[i].ItemGen, a.Items[i].ItemID, a.Items[i].ItemGen)
		}
	}

	// Growing tip: prefix kept, tip same ID with bumped gen.
	grown := ParseDocument("a.md", []byte("# A\n\npara more\n"), 3)
	tipID := grown.Items[len(grown.Items)-1].ItemID
	adoptItemIdentities(a.Items, grown.Items)
	if grown.Items[0].ItemID != a.Items[0].ItemID || grown.Items[0].ItemGen != 0 {
		t.Fatalf("prefix: id/gen %d/%d", grown.Items[0].ItemID, grown.Items[0].ItemGen)
	}
	last := len(grown.Items) - 1
	if grown.Items[last].ItemID != a.Items[last].ItemID {
		t.Fatalf("tip id: got %d want %d (pre-adopt minted %d)",
			grown.Items[last].ItemID, a.Items[last].ItemID, tipID)
	}
	if grown.Items[last].ItemGen != a.Items[last].ItemGen+1 {
		t.Fatalf("tip gen: got %d want %d", grown.Items[last].ItemGen, a.Items[last].ItemGen+1)
	}

	// Append new block: prefix IDs kept, new row keeps minted ID.
	appended := ParseDocument("a.md", []byte("# A\n\npara\n\n## B\n"), 4)
	newID := appended.Items[len(appended.Items)-1].ItemID
	adoptItemIdentities(a.Items, appended.Items)
	for i := range a.Items {
		if appended.Items[i].ItemID != a.Items[i].ItemID || appended.Items[i].ItemGen != a.Items[i].ItemGen {
			t.Fatalf("append prefix item %d: id/gen %d/%d want %d/%d",
				i, appended.Items[i].ItemID, appended.Items[i].ItemGen, a.Items[i].ItemID, a.Items[i].ItemGen)
		}
	}
	last = len(appended.Items) - 1
	if appended.Items[last].ItemID != newID || appended.Items[last].ItemGen != 0 {
		t.Fatalf("new block: id/gen %d/%d want minted %d/0",
			appended.Items[last].ItemID, appended.Items[last].ItemGen, newID)
	}
}

func itemSummary(items []DisplayItem) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.Kind.String()
		if it.Marker != "" {
			out[i] += "(" + it.Marker + ")"
		}
		if it.QuoteDepth > 0 {
			out[i] += "/q" + strings.Repeat(">", it.QuoteDepth)
		}
		if it.Text != "" {
			t := it.Text
			if len(t) > 24 {
				t = t[:24] + "…"
			}
			out[i] += ":" + t
		}
	}
	return out
}
