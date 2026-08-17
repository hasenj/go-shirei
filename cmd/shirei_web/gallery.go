package main

import (
	"fmt"
	"os"
	"path/filepath"
)

// webDemo is one web-friendly demo package under demos/.
// Dir is the package slug under demos/ and the output folder name.
// Name is the page title; Desc is for the static-site gallery cards only.
// Dir is also the GitHub path under demos/ for the Source link label.
// Keep in sync with static-sites/.../shirei/{demos,custom-widgets}/index.scgo.
type webDemo struct {
	Dir  string
	Name string
	Desc string
}

// publicDemoSource is the GitHub tree for demo sources on the public mirror.
const publicDemoSource = "https://github.com/hasenj/go-shirei/tree/master/demos"

// Gallery sets built by -gallery=<name>. Names match the static-site directory
// under static-sites/judi.systems/shirei/ (custom-widgets, demos).
//
// Only demos linked from those gallery pages belong here — not every package
// under demos/ (font-scan, IME-heavy text-view, mobile-only, … stay out).
// demos/small is built separately into /shirei/try by rebuild-web-demos.sh.
var gallerySets = map[string][]webDemo{
	// Custom chrome on Process* helpers — skins never enter the widgets package.
	"custom-widgets": {
		{Dir: "custom-buttons", Name: "Buttons", Desc: "ProcessButtonEvents with Material, Win98, and XP skins."},
		{Dir: "custom-toggles", Name: "Toggles", Desc: "Toggle skins on the same press model as checkboxes."},
		{Dir: "custom-checkboxes", Name: "Checkboxes", Desc: "Custom checkbox chrome via ProcessButtonEvents."},
		{Dir: "custom-radios", Name: "Radios", Desc: "Mutually exclusive options with Material and Luna skins."},
		{Dir: "custom-sliders", Name: "Sliders", Desc: "ProcessSlider with Apple, Material, and XP handles."},
		{Dir: "custom-segmented", Name: "Segmented", Desc: "ProcessSegmentEvents — modern pill and iOS 7 styles."},
		{Dir: "custom-textinputs", Name: "Text inputs", Desc: "ProcessTextInput with Material and Luna field chrome."},
		{Dir: "custom-scrollbars", Name: "Scrollbars", Desc: "ScrollBarExt skins and the default overlay bar."},
		{Dir: "custom-icon-fonts", Name: "Icon fonts", Desc: "Third-party icon font registered with UseFontBytes."},
	},
	// Interactive demos — keep in lockstep with demos/index.scgo cards.
	"demos": {
		{Dir: "balls-buckets", Name: "Balls & Buckets", Desc: "Drag-and-drop balls into lettered buckets."},
		{Dir: "kanban", Name: "Kanban", Desc: "Drag cards between lanes."},
		{Dir: "theme", Name: "Theme", Desc: "Themed controls and scroll regions."},
		{Dir: "style-spans", Name: "Style spans", Desc: "Inline text styling with Span."},
		{Dir: "layout", Name: "Layout", Desc: "Flexbox-like row/column playground."},
		{Dir: "animate-size", Name: "Animate size", Desc: "Layout size easing vs snap."},
	},
}

func knownGalleryNames() string {
	return "custom-widgets | demos"
}

// buildGallery builds each demo in the named set under outDir/<slug>/.
// Removes leftover sibling demo dirs that are not in the set (stale wasm).
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

	keep := map[string]bool{"embed.js": true}
	var n int
	for _, d := range demos {
		pkg := filepath.Join(demosRoot, d.Dir)
		if _, err := os.Stat(filepath.Join(pkg, "main.go")); err != nil {
			fmt.Fprintf(os.Stderr, "shirei_web: skip %s (no main.go)\n", d.Dir)
			continue
		}
		dest := filepath.Join(outDir, d.Dir)
		keep[d.Dir] = true
		srcPath := "demos/" + d.Dir
		meta := pageMeta{
			Title:       d.Name + " — Shirei",
			SourceURL:   publicDemoSource + "/" + d.Dir,
			SourceLabel: srcPath,
		}
		fmt.Fprintf(os.Stderr, "shirei_web: gallery[%s] demo %s\n", setName, d.Dir)
		if err := buildStatic(pkg, dest, meta); err != nil {
			return fmt.Errorf("%s: %w", d.Dir, err)
		}
		n++
	}
	if n == 0 {
		return fmt.Errorf("no demos built (set %q, demos root %q)", setName, demosRoot)
	}

	// Drop stale demo folders from previous gallery membership.
	entries, err := os.ReadDir(outDir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		if keep[e.Name()] {
			continue
		}
		path := filepath.Join(outDir, e.Name())
		fmt.Fprintf(os.Stderr, "shirei_web: remove stale %s\n", path)
		if err := os.RemoveAll(path); err != nil {
			return err
		}
	}

	fmt.Fprintf(os.Stderr, "shirei_web: built %d demos under %s\n", n, outDir)
	return nil
}
