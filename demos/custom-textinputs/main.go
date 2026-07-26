package main

// custom-textinputs compares stock TextInput chrome with custom skins built
// from ProcessTextInput + DrawTextInputPlain (Material / Windows XP Luna).

import (
	"fmt"

	"go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

func main() {
	app.SetupWindow("Custom Text Inputs", 720, 720)
	app.Run(root)
}

var (
	defaultName  = "Taro Yamada"
	defaultNotes = "Multiline default TextArea.\nSecond line."
	ctrlFind     = ""

	matEmail = "you@example.com"
	matNotes = "Material multi-line notes.\nLine two."

	xpUser  = "Administrator"
	xpNotes = "XP multi-line field.\nSecond line of text."
)

func root() {
	ModAttrs(Viewport, Background(220, 8, 94, 1), Pad(28), Gap(18))
	ScrollOnInput()

	Label("Custom text inputs", FontWeight(WeightBold), FontSize(18))
	Label("Stock TextInputExt chrome vs ProcessTextInput + custom boxes (Material, XP Luna).",
		FontSize(13), TextColor(0, 0, 40, 1))

	// --- stock ----------------------------------------------------------------
	section("Default (stock chrome)")
	fieldLabel("Single-line (TextInput)")
	TextInput(&defaultName)
	fieldLabel("CtrlTextInput + CtrlButton (narrow strip)")
	Container(Attrs(Row, CrossMid, Gap(6)), func() {
		a := CtrlTextInputAttrs()
		a.NoAutoFocus = true
		a.Placeholder = "filter…"
		a.FixedWidth = true
		a.MinWidth = 120
		TextInputExt(&ctrlFind, a)
		CtrlButton(NoIcon, "Find", true)
		CtrlButton(NoIcon, "Copy path", true)
	})
	fieldLabel("Multi-line (TextArea)")
	TextArea(&defaultNotes)

	// --- Material / Android-ish -----------------------------------------------
	section("Material-inspired (Android-ish)")
	Label("Flat fill, thin border, thick accent underline on focus.",
		FontSize(12), TextColor(0, 0, 45, 1))
	fieldLabel("Single-line")
	MaterialTextField(&matEmail, materialPrimary, false)
	fieldLabel("Multi-line")
	MaterialTextField(&matNotes, materialTeal, true)

	// --- XP -------------------------------------------------------------------
	section("Windows XP Luna")
	Label("Blue border, white face, light top inset — a field, not a raised button.",
		FontSize(12), TextColor(0, 0, 45, 1))
	Container(Attrs(Gap(10), Pad(14), Background(51, 33, 89, 1),
		BorderWidth(1), BorderColor(50, 10, 70, 1)), func() {
		fieldLabel("Single-line")
		XPTextField(&xpUser, false)
		fieldLabel("Multi-line")
		XPTextField(&xpNotes, true)
	})

	// --- status ---------------------------------------------------------------
	Container(Attrs(Pad2(10, 14), Background(0, 0, 100, 1), Corners(4),
		BorderWidth(1), BorderColor(0, 0, 0, 0.08)), func() {
		Label("Live values", FontSize(11), TextColor(0, 0, 45, 1))
		Label(fmt.Sprintf("default=%q  ctrl=%q", defaultName, ctrlFind), FontSize(12))
		Label(fmt.Sprintf("mat=%q  xp=%q", matEmail, xpUser), FontSize(12))
	})
	ScrollBars()
}

func section(title string) {
	Label(title, FontWeight(WeightBold), FontSize(14), TextColor(0, 0, 30, 1))
}

func fieldLabel(s string) {
	Label(s, FontSize(12), TextColor(0, 0, 40, 1))
}

var (
	materialPrimary = Vec4{211, 72, 48, 1}
	materialTeal    = Vec4{174, 55, 42, 1}
)

func fieldConfig(multiline bool) TextInputConfig {
	cfg := TextInputConfig{
		FontSize:    DefaultTextSize,
		Padding:     N4(DefaultTextSize / 2),
		NoAutoFocus: true,
	}
	if multiline {
		cfg.Wrap = true
		cfg.MaxLines = 0
		cfg.Rows = 4
	} else {
		cfg.MaxLines = 1
	}
	return cfg
}

func fieldMinSize(cfg TextInputConfig) (minW, boxH float32) {
	padSize := PadSize(cfg.Padding)
	minW = padSize[0] + cfg.FontSize*12
	rows := cfg.Rows
	if rows <= 0 {
		if cfg.MaxLines == 1 {
			rows = 1
		} else {
			rows = 4
		}
	}
	boxH = float32(rows)*cfg.FontSize + padSize[1]
	return minW, boxH
}

func fieldSize(st TextInputState, minW, boxH float32) Vec2 {
	sz := st.FieldSize
	if sz == (Vec2{}) {
		sz = GetResolvedSize()
	}
	if sz == (Vec2{}) {
		sz = Vec2{minW, boxH}
	}
	return sz
}

// MaterialTextField — flat Material-ish field. multiline enables wrap + rows.
// Demo only.
func MaterialTextField(buf *string, accent Vec4, multiline bool) {
	cfg := fieldConfig(multiline)
	minW, boxH := fieldMinSize(cfg)

	Container(Attrs(
		Focusable, Clip, Expand,
		Corners(4),
		PadVec(cfg.Padding),
		MinSize(minW, boxH),
		MaxSizeVec(Vec2{0, boxH}),
		Background(0, 0, 100, 1),
		BorderWidth(1),
		BorderColor(0, 0, 80, 1),
	), func() {
		st := ProcessTextInput(buf, cfg)
		border := Vec4{0, 0, 80, 1}
		if st.HasFocus {
			border = accent
			ModAttrs(Background(0, 0, 100, 1))
		} else if st.Hovered {
			border = Vec4{0, 0, 65, 1}
		}
		ModAttrs(BorderColorVec(border))

		sz := fieldSize(st, minW, boxH)
		ul := Vec4{0, 0, 80, 0.5}
		ulH := float32(1)
		if st.HasFocus {
			ul = accent
			ulH = 2
		}
		Element(Attrs(NoAnimate, Float(0, sz[1]-ulH), FixSize(sz[0], ulH), BackgroundVec(ul)))

		DrawTextInputPlain(st, cfg)
	})
}

// XPTextField — Windows XP Luna sunken edit control. Demo only.
func XPTextField(buf *string, multiline bool) {
	cfg := fieldConfig(multiline)
	minW, boxH := fieldMinSize(cfg)
	borderBlue := Vec4{207, 40, 45, 1}

	Container(Attrs(
		Focusable, Clip, Expand,
		Corners(0),
		PadVec(cfg.Padding),
		MinSize(minW, boxH),
		MaxSizeVec(Vec2{0, boxH}),
		Background(0, 0, 100, 1),
		BorderWidth(1),
		BorderColorVec(borderBlue),
	), func() {
		st := ProcessTextInput(buf, cfg)
		if st.HasFocus {
			ModAttrs(BorderColor(210, 55, 40, 1))
		}
		sz := fieldSize(st, minW, boxH)
		Element(Attrs(NoAnimate, Float(0, 0), FixSize(sz[0], 1),
			Background(0, 0, 0, 0.12), ClickThrough))
		DrawTextInputPlain(st, cfg)
	})
}
