// Package themetest renders example snapshots in both application color schemes.
package themetest

import (
	"testing"

	"go.hasen.dev/shirei"
	"go.hasen.dev/shirei/widgets"
)

// Each pins both preferences to one scheme, so application code can continue
// following the OS while the rendered colors remain deterministic.
func Each(t *testing.T, render func(t *testing.T, suffix string)) {
	t.Helper()
	defer func() {
		widgets.SetLightColorScheme(widgets.LightColorScheme())
		widgets.SetDarkColorScheme(widgets.DarkColorScheme())
	}()
	for _, dark := range []bool{false, true} {
		name, suffix, scheme := "light", "", widgets.LightColorScheme()
		if dark {
			name, suffix, scheme = "dark", "_dark", widgets.DarkColorScheme()
		}
		t.Run(name, func(t *testing.T) {
			widgets.SetLightColorScheme(scheme)
			widgets.SetDarkColorScheme(scheme)
			render(t, suffix)
		})
	}
}

func Snapshot(t *testing.T, name string, w, h int, frame shirei.FrameFn) {
	t.Helper()
	Each(t, func(t *testing.T, suffix string) {
		r := shirei.Snapshot(t.Name(), name+suffix, w, h, frame)
		switch {
		case r.Status == shirei.SnapSkip:
			t.Skip(r.Reason)
		case r.Err != nil:
			t.Fatal(r.Err)
		case r.Status == shirei.SnapMismatch:
			t.Errorf("render differs from %s; wrote %s", shirei.SnapAbsPath(r.Golden), shirei.SnapAbsPath(r.Actual))
		case r.Status == shirei.SnapCreated:
			t.Logf("created %s; review before committing", shirei.SnapAbsPath(r.Golden))
		}
	})
}
