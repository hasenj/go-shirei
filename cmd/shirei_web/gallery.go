package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// webDemo is one web-friendly demo package under demos/.
type webDemo struct {
	Dir  string // under demos/, also URL slug
	Name string
	Desc string
}

// Gallery sets built by -gallery=<name>. Names match the static-site directory
// under static-sites/judi.systems/shirei/ (custom-widgets, demos).
var gallerySets = map[string][]webDemo{
	// Custom chrome on Process* helpers — skins never enter the widgets package.
	"custom-widgets": {
		{Dir: "custom-buttons", Name: "Buttons", Desc: "ProcessButtonEvents with custom skins."},
		{Dir: "custom-toggles", Name: "Toggles", Desc: "Toggle skins on the press model."},
		{Dir: "custom-checkboxes", Name: "Checkboxes", Desc: "Custom checkbox chrome."},
		{Dir: "custom-radios", Name: "Radios", Desc: "Mutually exclusive option skins."},
		{Dir: "custom-sliders", Name: "Sliders", Desc: "ProcessSlider chrome variants."},
		{Dir: "custom-segmented", Name: "Segmented", Desc: "ProcessSegmentEvents skins."},
		{Dir: "custom-textinputs", Name: "Text inputs", Desc: "ProcessTextInput field chrome."},
		{Dir: "custom-scrollbars", Name: "Scrollbars", Desc: "ScrollBarExt skins."},
		{Dir: "custom-icon-fonts", Name: "Icon fonts", Desc: "Third-party icon font via UseFontBytes."},
	},
	// General interactive demos (DnD, layout, text, audio, …).
	"demos": {
		{Dir: "balls-buckets", Name: "Balls & Buckets", Desc: "Drag-and-drop balls into lettered buckets."},
		{Dir: "kanban", Name: "Kanban", Desc: "Drag cards between lanes."},
		{Dir: "small", Name: "Small demo", Desc: "Name field, Hello button, background toggle."},
		{Dir: "theme", Name: "Theme", Desc: "Themed controls and scroll regions."},
		{Dir: "temp-converter", Name: "Temp converter", Desc: "°C to °F with a live text field."},
		{Dir: "text-fields", Name: "Text fields", Desc: "Single-line, multi-line, password, and RTL."},
		{Dir: "style-spans", Name: "Style spans", Desc: "Inline text styling with Span."},
		{Dir: "layout", Name: "Layout", Desc: "Flexbox-like row/column playground."},
		{Dir: "animate-size", Name: "Animate size", Desc: "Layout size easing vs snap."},
	},
}

func knownGalleryNames() string {
	return "custom-widgets | demos"
}

// buildGallery builds each demo in the named set under outDir/<slug>/.
// The static site gallery index is scgo (not generated here).
func buildGallery(setName, outDir, demosRoot string) error {
	demos, ok := gallerySets[setName]
	if !ok {
		return fmt.Errorf("unknown gallery %q (want %s)", setName, knownGalleryNames())
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(outDir, "embed.js"), embedJS, 0o644); err != nil {
		return err
	}

	var n int
	for _, d := range demos {
		pkg := filepath.Join(demosRoot, d.Dir)
		if _, err := os.Stat(filepath.Join(pkg, "main.go")); err != nil {
			fmt.Fprintf(os.Stderr, "shirei_web: skip %s (no main.go)\n", d.Dir)
			continue
		}
		dest := filepath.Join(outDir, d.Dir)
		fmt.Fprintf(os.Stderr, "shirei_web: gallery[%s] demo %s\n", setName, d.Dir)
		if err := buildStatic(pkg, dest); err != nil {
			return fmt.Errorf("%s: %w", d.Dir, err)
		}
		n++
	}
	if n == 0 {
		return fmt.Errorf("no demos built (set %q, demos root %q)", setName, demosRoot)
	}
	fmt.Fprintf(os.Stderr, "shirei_web: built %d demos under %s\n", n, outDir)
	return nil
}
