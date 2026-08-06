package main

import (
	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

// Layout constants: chrome (quote bars, marker column, indent) lives outside
// the text host. The text host gets MaxWidth(textBudget) and no horizontal pad.
const (
	docHPad       f32 = 18
	docVPad       f32 = 3
	spaceBefore   f32 = 10
	markerColW    f32 = 28
	quoteBarW     f32 = 3
	quoteBarGap   f32 = 10
	codeHPad      f32 = 10
	codeVPad      f32 = 2
	thematicH     f32 = 12
	bodyFontSize  f32 = 15
	codeFontSize  f32 = 13
	tableCellPad  f32 = 8
	tableMinCell  f32 = 48
	tableRule     f32 = 1
)

var mdListKey = new(int)

// mdHeightKey identifies a measured row for CachedMeasure by stable item
// identity (not document generation or list index).
type mdHeightKey struct {
	ID    uint64
	Gen   uint64
	Width f32
}

func markdownSurface(doc *Document, firstVisible *int) {
	if doc == nil || len(doc.Items) == 0 {
		Container(Attrs(Expand, Grow(1), Center), func() {
			Label("Empty document", FontSize(14), TextColor(0, 0, 50, 1))
		})
		return
	}
	items := doc.Items
	itemKey := func(i int) any {
		if i < 0 || i >= len(items) {
			return i
		}
		return items[i].ItemID
	}
	itemView := func(i int, width f32) {
		if i < 0 || i >= len(items) {
			return
		}
		paintItem(&items[i], width)
	}
	itemHeight := func(i int, width f32) f32 {
		if i < 0 || i >= len(items) {
			return 1
		}
		it := &items[i]
		return CachedMeasure(mdHeightKey{ID: it.ItemID, Gen: it.ItemGen, Width: width}, Vec2{width, 0}, func() {
			paintItem(it, width)
		})[1]
	}
	Container(Attrs(Grow(1), Expand, Clip), func() {
		VirtualListViewExt(mdListKey, VirtualListAttrs{
			ItemCount:       len(items),
			ItemKey:         itemKey,
			ItemHeight:      itemHeight,
			ItemView:        itemView,
			OutFirstVisible: firstVisible,
		})
	})
}

func paintItem(item *DisplayItem, rowWidth f32) {
	topPad := docVPad
	if item.Chrome&ChromeSpaceBefore != 0 {
		topPad += spaceBefore
	}
	botPad := docVPad
	switch item.Kind {
	case KindCodeLine:
		if item.Chrome&ChromeCodeFirst != 0 {
			topPad += codeVPad
		} else {
			topPad = 0
		}
		if item.Chrome&ChromeCodeLast != 0 {
			botPad += codeVPad
		} else {
			botPad = 0
		}
	case KindTableRow:
		if item.Chrome&ChromeTableFirst == 0 {
			topPad = 0
		}
		if item.Chrome&ChromeTableLast == 0 {
			botPad = 0
		}
	}

	Container(Attrs(Expand, MaxWidth(rowWidth), Pad2(0, docHPad)), func() {
		Container(Attrs(Row, Expand, MaxWidth(rowWidth-docHPad*2), Pad4(topPad, 0, botPad, 0), CrossAlign(AlignStart)), func() {
			paintQuoteBars(item.QuoteDepth)

			if item.Indent > 0 {
				Element(Attrs(FixWidth(item.Indent)))
			}

			if item.Indent > 0 || item.Marker != "" {
				Container(Attrs(FixWidth(markerColW)), func() {
					if item.Marker != "" {
						Label(item.Marker, FontSize(bodyFontSize), TextColor(0, 0, 40, 1), FontWeight(WeightSemibold))
					}
				})
			}

			budget := textBudget(rowWidth, item)
			switch item.Kind {
			case KindThematicBreak:
				Container(Attrs(Expand, MaxWidth(budget), Pad2(thematicH/2, 0)), func() {
					Element(Attrs(Expand, MaxWidth(budget), FixHeight(1), Background(0, 0, 0, 0.18)))
				})
			case KindCodeLine:
				paintCodeLine(item, budget)
			case KindTableRow:
				paintTableRow(item, budget)
			default:
				paintTextHost(item, budget)
			}
		})
	})
}

func paintQuoteBars(depth int) {
	for i := 0; i < depth; i++ {
		Element(Attrs(FixWidth(quoteBarW), MinHeight(bodyFontSize+4), Background(210, 35, 55, 0.55)))
		Element(Attrs(FixWidth(quoteBarGap-quoteBarW)))
	}
}

func textBudget(rowWidth f32, item *DisplayItem) f32 {
	w := rowWidth - docHPad*2
	w -= float32(item.QuoteDepth) * quoteBarGap
	w -= item.Indent
	if item.Indent > 0 || item.Marker != "" {
		w -= markerColW
	}
	if item.Kind == KindCodeLine {
		w -= codeHPad * 2
	}
	if w < 40 {
		return 40
	}
	return w
}

func paintCodeLine(item *DisplayItem, budget f32) {
	bg := Vec4{220, 12, 96, 1}
	corners := Vec4{}
	if item.Chrome&ChromeCodeFirst != 0 {
		corners[0] = 4
		corners[1] = 4
	}
	if item.Chrome&ChromeCodeLast != 0 {
		corners[2] = 4
		corners[3] = 4
	}
	Container(Attrs(
		Expand, MaxWidth(budget+codeHPad*2),
		Background(bg[0], bg[1], bg[2], bg[3]),
		Pad2(0, codeHPad),
		Corners4(corners[0], corners[1], corners[2], corners[3]),
	), func() {
		style := TextStyle(
			FontSize(codeFontSize),
			Fonts(Monospace...),
			TextColor(0, 0, 18, 1),
		)
		Container(Attrs(MaxWidth(budget)), func() {
			text := item.Text
			if text == "" {
				text = " "
			}
			Text(text, style)
		})
	})
}

func paintTextHost(item *DisplayItem, budget f32) {
	style := itemBaseStyle(item)
	spans := itemTextSpans(item.Spans, item.Links)
	Container(Attrs(MaxWidth(budget)), func() {
		if len(item.Links) > 0 && PressAction() {
			shaped := ShapeTextMax(item.Text, style, budget, spans...)
			idx := ComputeCursorIndex(GetContentRect(), GetInputState().MousePoint, Vec2{}, shaped)
			if target, ok := linkTargetAt(item.Links, idx); ok {
				activateLink(target)
			}
		}
		Text(item.Text, style, spans...)
	})
}

func paintTableRow(item *DisplayItem, budget f32) {
	n := len(item.Cells)
	if n == 0 {
		return
	}
	header := item.Chrome&ChromeTableHeader != 0
	bg := Vec4{0, 0, 100, 1}
	if header {
		bg = Vec4{220, 10, 96, 1}
	}

	corners := Vec4{}
	if item.Chrome&ChromeTableFirst != 0 {
		corners[0] = 5
		corners[1] = 5
	}
	if item.Chrome&ChromeTableLast != 0 {
		corners[2] = 5
		corners[3] = 5
	}

	sepBudget := budget - tableRule*float32(n-1)
	cellW := sepBudget / float32(n)
	if cellW < tableMinCell {
		cellW = tableMinCell
	}
	textBudget := cellW - tableCellPad*2
	if textBudget < 24 {
		textBudget = 24
	}

	Container(Attrs(
		Expand, MaxWidth(budget),
		Background(bg[0], bg[1], bg[2], bg[3]),
		BorderWidth(tableRule), BorderColor(0, 0, 0, 0.18),
		Corners4(corners[0], corners[1], corners[2], corners[3]),
	), func() {
		Container(Attrs(Row, Expand, CrossAlign(AlignStart)), func() {
			for i := range item.Cells {
				if i > 0 {
					Element(Attrs(FixWidth(tableRule), Expand, Background(0, 0, 0, 0.20)))
				}
				paintTableCell(&item.Cells[i], cellW, textBudget, header)
			}
		})
		if header {
			Element(Attrs(Expand, FixHeight(tableRule), Background(0, 0, 0, 0.14)))
		}
	})
}

func paintTableCell(cell *TableCell, width, textBudget f32, header bool) {
	styleMods := []TextStyleFn{
		FontSize(bodyFontSize),
		TextColor(0, 0, 16, 1),
	}
	if header {
		styleMods = append(styleMods, FontWeight(WeightSemibold))
	}
	style := TextStyle(styleMods...)
	spans := itemTextSpans(cell.Spans, cell.Links)

	align := AlignStart
	switch cell.Align {
	case CellAlignCenter:
		align = AlignMiddle
	case CellAlignRight:
		align = AlignEnd
	}

	Container(Attrs(FixWidth(width), Pad(tableCellPad), MainAlign(align)), func() {
		Container(Attrs(MaxWidth(textBudget)), func() {
			if len(cell.Links) > 0 && PressAction() {
				shaped := ShapeTextMax(cell.Text, style, textBudget, spans...)
				idx := ComputeCursorIndex(GetContentRect(), GetInputState().MousePoint, Vec2{}, shaped)
				if target, ok := linkTargetAt(cell.Links, idx); ok {
					activateLink(target)
				}
			}
			text := cell.Text
			if text == "" {
				text = " "
			}
			Text(text, style, spans...)
		})
	})
}

func itemBaseStyle(item *DisplayItem) TextStyleAttrs {
	size := bodyFontSize
	weight := WeightNormal
	color := Vec4{0, 0, 16, 1}
	switch item.Kind {
	case KindHeading:
		switch item.HeadingLevel {
		case 1:
			size = 28
			weight = WeightBold
		case 2:
			size = 22
			weight = WeightBold
		case 3:
			size = 18
			weight = WeightSemibold
		default:
			size = 16
			weight = WeightSemibold
		}
		color = Vec4{220, 20, 14, 1}
	}
	mods := []TextStyleFn{
		FontSize(size),
		FontWeight(weight),
		TextColor(color[0], color[1], color[2], color[3]),
	}
	if item.Monospace {
		mods = append(mods, Fonts(Monospace...))
	}
	return TextStyle(mods...)
}

func itemTextSpans(spans []ItemSpan, links []LinkSpan) []TextSpan {
	var out []TextSpan
	for _, sp := range spans {
		var mods []TextStyleFn
		if sp.Emph {
			mods = append(mods, FontStyle(StyleItalic))
		}
		if sp.Strong {
			mods = append(mods, FontWeight(WeightBold))
		}
		if sp.Code {
			mods = append(mods,
				Fonts(Monospace...),
				FontSize(codeFontSize),
				TextBackground(220, 15, 92, 1),
			)
		}
		if len(mods) > 0 {
			out = append(out, Span(sp.From, sp.To, mods...))
		}
	}
	for _, lk := range links {
		out = append(out, Span(lk.From, lk.To,
			TextColor(210, 70, 38, 1),
			TextUnderline(true),
		))
	}
	return out
}
