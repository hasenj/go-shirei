package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAndroidPackageName(t *testing.T) {
	// Empty id → prefix + sanitized folder (hyphens gone).
	if got := androidPackageName("", "dev.shirei", "hacker-news-reader"); got != "dev.shirei.hackernewsreader" {
		t.Fatalf("empty id: %q", got)
	}
	// Single segment with hyphens is not reverse-DNS — prefix it.
	if got := androidPackageName("hacker-news-reader", "dev.hasen", "x"); got != "dev.hasen.hackernewsreader" {
		t.Fatalf("single segment: %q", got)
	}
	// Multi-segment: strip hyphens inside segments.
	if got := androidPackageName("dev.hasen.hacker-news", "dev.shirei", "x"); got != "dev.hasen.hackernews" {
		t.Fatalf("hyphen in segment: %q", got)
	}
	// Valid id preserved (lowercased via sanitize).
	if got := androidPackageName("dev.hasen.hn", "", "x"); got != "dev.hasen.hn" {
		t.Fatalf("valid: %q", got)
	}
	// Default prefix when empty.
	if got := androidPackageName("", "", "piano"); got != defaultAppIDPrefix+".piano" {
		t.Fatalf("default prefix: %q", got)
	}
}

func TestEffectiveAppIDPrefix(t *testing.T) {
	if got := (Config{}).effectiveAppIDPrefix(); got != defaultAppIDPrefix {
		t.Fatalf("empty → %q", got)
	}
	if got := (Config{AppIDPrefix: "  dev.hasen.  "}).effectiveAppIDPrefix(); got != "dev.hasen" {
		t.Fatalf("trim → %q", got)
	}
}

func TestDefaultPackageIconPath(t *testing.T) {
	dir := t.TempDir()
	if got := defaultPackageIconPath(dir); got != "" {
		t.Fatalf("want empty, got %q", got)
	}
	if got := resolveIconPath(dir, ""); got != "" {
		t.Fatalf("resolve empty dir: %q", got)
	}
	abs := filepath.Join(dir, "icon.png")
	if err := os.WriteFile(abs, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := defaultPackageIconPath(dir); got != "icon.png" {
		t.Fatalf("got %q want icon.png", got)
	}
	// Field stays empty; resolve still finds icon.png when present.
	cfg := defaultConfig()
	cfg.Package = dir
	cfg.applyPackageSettings()
	if cfg.IconPath != "" {
		t.Fatalf("IconPath should stay empty, got %q", cfg.IconPath)
	}
	if got := resolveIconPath(dir, ""); got != abs {
		t.Fatalf("resolve fallback = %q want %q", got, abs)
	}
}

func TestRelativizeIconPath(t *testing.T) {
	dir := t.TempDir()
	abs := filepath.Join(dir, "assets", "app.png")
	if got := relativizeIconPath(dir, abs); got != filepath.Join("assets", "app.png") {
		t.Fatalf("under package: %q", got)
	}
	if got := relativizeIconPath(dir, "icon.png"); got != "icon.png" {
		t.Fatalf("already relative: %q", got)
	}
	outside := filepath.Join(t.TempDir(), "x.png")
	if got := relativizeIconPath(dir, outside); got != outside {
		t.Fatalf("outside package kept abs: %q", got)
	}
}

func TestPackageSettingsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	pkg := filepath.Join(dir, "piano")
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := defaultConfig()
	cfg.Package = pkg
	cfg.AppID = "dev.test.piano"
	cfg.AppName = "Piano"
	cfg.IconPath = filepath.Join(pkg, "icon.png")
	cfg.storePackageSettings()

	cfg2 := defaultConfig()
	cfg2.Packages = cfg.Packages
	cfg2.Package = cfg.Package
	cfg2.applyPackageSettings()
	if cfg2.AppID != "dev.test.piano" || cfg2.AppName != "Piano" || cfg2.IconPath != "icon.png" {
		t.Fatalf("apply: %+v", cfg2)
	}
}
