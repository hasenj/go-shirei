package widgets

// Full-UI snapshot tests for widget compositions; see internal/snaptest for
// the harness and conventions (UPDATE_SNAPSHOTS=1, .actual.png on mismatch).

import (
	"fmt"
	"testing"

	"go.hasen.dev/shirei/internal/snaptest"

	. "go.hasen.dev/shirei"
)

// --- the widget gallery ---

type galleryRow struct {
	Name string
	Size int64
	Hits int
}

// deterministic pseudo-data: enough rows to overflow the viewport so the
// table's virtualization and scrollbar render
var galleryRows = func() []*galleryRow {
	words := []string{"alpha", "bravo", "charlie", "delta", "echo", "foxtrot", "golf", "hotel"}
	pkgs := []string{"runtime", "widgets", "shirei", "main"}
	out := make([]*galleryRow, 0, 40)
	for i := 0; i < 40; i++ {
		out = append(out, &galleryRow{
			Name: fmt.Sprintf("%s.%s%02d", pkgs[i%len(pkgs)], words[i%len(words)], i),
			Size: int64(i*i*137 + 42),
			Hits: (i * 31) % 97,
		})
	}
	return out
}()

func galleryColumns() []TableColumn[*galleryRow] {
	return []TableColumn[*galleryRow]{
		{
			Label:  "Name",
			Render: func(r *galleryRow) { Label(r.Name) },
			Less:   func(a, b *galleryRow) bool { return a.Name < b.Name },
		},
		{
			Label: "Size", Width: 90, DefaultDesc: true,
			Render: func(r *galleryRow) { Label(fmt.Sprintf("%d", r.Size)) },
			Less:   func(a, b *galleryRow) bool { return a.Size < b.Size },
		},
		{
			Label: "Hits", Width: 70, DefaultDesc: true,
			Render: func(r *galleryRow) { Label(fmt.Sprintf("%d", r.Hits)) },
			Less:   func(a, b *galleryRow) bool { return a.Hits < b.Hits },
		},
	}
}

var (
	galleryCheckOn  = true
	galleryCheckOff = false
	galleryOption   = "b"
	gallerySlider   = float32(0.6)
	galleryInput    = "hello 世界"
)

// widgetGallery is a static composition exercising the widget set: buttons
// (incl. disabled), checkboxes, option buttons, a slider, a text input
// (auto-focused: focused styling + caret render), labels in several styles
// and scripts, and a sortable virtualized Table with a scrollbar.
func widgetGallery() {
	Container(Attrs(Row, Viewport, Background(0, 0, 98, 1)), func() {
		Container(Attrs(FixWidth(300), Expand, Clip, Pad(14), Gap(10), Background(0, 0, 94, 1)), func() {
			Label("Widget Gallery", FontWeight(WeightBold), FontSize(16))
			Label("mixed 日本語 and English 123", FontSize(12))
			Label("subtle italic caption", FontStyle(StyleItalic), FontSize(11), TextColor(0, 0, 45, 1))

			Button(0, "Primary Button")
			Container(Attrs(Row, Gap(8)), func() {
				CtrlButton(0, "Ctrl Button", true)
				CtrlButton(0, "Disabled", false)
			})

			CheckBox(&galleryCheckOn, "Enabled option")
			CheckBox(&galleryCheckOff, "Disabled option")
			OptionButton(&galleryOption, "Choice A", "a")
			OptionButton(&galleryOption, "Choice B", "b")

			Slider(&gallerySlider, SliderAttrs{Min: 0, Max: 1, Width: 200})
			TextInput(&galleryInput)
		})

		Container(Attrs(Grow(1), Expand, Clip), func() {
			Table(nil, 28, galleryColumns(), galleryRows, func(r *galleryRow) any { return r }, 2)
		})
	})
}

func TestSnapshotWidgetGallery(t *testing.T) {
	snaptest.Snapshot(t, "widget_gallery", 900, 640, widgetGallery)
}

// Pins that the bundled icon fonts (Microns + Typicons) are registered for
// headless renders: a regression turns every glyph below into a missing-glyph
// box. Covers both families through Icon and a Button icon.
func TestSnapshotIconGlyphs(t *testing.T) {
	snaptest.Snapshot(t, "icon_glyphs", 340, 90, func() {
		Container(Attrs(Row, Viewport, CrossMid, Pad(16), Gap(14), Background(0, 0, 98, 1)), func() {
			Icon(TypArrowUpThick, FontSize(24))
			Icon(SymRefresh, FontSize(24))
			Button(SymCopy, "Copy")
		})
	})
}

func TestSnapshotWrappedTextSelection(t *testing.T) {
	snaptest.Snapshot(t, "wrapped_text_selection", 260, 110, func() {
		Container(Attrs(Pad(12), Background(0, 0, 98, 1)), func() {
			attrs := DefaultTextStyle()
			attrs.FontSize = 14
			shaped := ShapeTextMax("alpha beta gamma delta epsilon", attrs, 150)
			Container(Attrs(MaxWidth(150)), func() {
				ShapedTextLayout(shaped, attrs, 6, 24)
			})
		})
	})
}

func snapshotTextInputIMEState(t *testing.T, name string, committed string, composition string) {
	t.Helper()
	snaptest.Snapshot(t, name, 320, 70, func() {
		Container(Attrs(Pad(14), Background(0, 0, 98, 1)), func() {
			buf := committed
			GetInputState().Composition = composition
			GetInputState().CompositionSel = [2]int{0, len([]rune(composition))}
			activeInput.cursor = len([]rune(committed))
			activeInput.anchor = activeInput.cursor

			attrs := DefaultTextInputAttrs()
			attrs.MinWidth = 260
			TextInputExt(&buf, attrs)
		})
	})
}

func TestSnapshotTextInputIMEState1(t *testing.T) {
	snapshotTextInputIMEState(t, "textinput_ime_state1_hiragana", "日本語での", "にゅうりょ")
}

func TestSnapshotTextInputIMEState2(t *testing.T) {
	snapshotTextInputIMEState(t, "textinput_ime_state2_converted", "日本語での", "入力")
}

func TestSnapshotTextInputIMEState3(t *testing.T) {
	snapshotTextInputIMEState(t, "textinput_ime_state3_committed", "日本語での入力", "")
}
