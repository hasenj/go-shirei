package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultPackageIconRel(t *testing.T) {
	pkg := t.TempDir()
	if got := defaultPackageIconRel(pkg); got != "" {
		t.Fatalf("empty package: got %q", got)
	}
	if err := os.WriteFile(filepath.Join(pkg, "icon.png"), []byte("root"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := defaultPackageIconRel(pkg); got != "icon.png" {
		t.Fatalf("root icon only: got %q", got)
	}
	res := filepath.Join(pkg, packageResourcesDir)
	if err := os.MkdirAll(res, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(res, "icon.png"), []byte("res"), 0o644); err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(packageResourcesDir, "icon.png")
	if got := defaultPackageIconRel(pkg); got != want {
		t.Fatalf("Resources preferred: got %q want %q", got, want)
	}
}

func TestCopyPackageResourcesFlat(t *testing.T) {
	pkg := t.TempDir()
	src := filepath.Join(pkg, "Resources")
	if err := os.MkdirAll(src, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "icon.png"), []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, ".DS_Store"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(src, "nested")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(t.TempDir(), "Contents", "Resources")
	if err := copyPackageResources(pkg, dest, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "icon.png")); err != nil {
		t.Fatalf("icon.png: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "nested", "a.txt")); err != nil {
		t.Fatalf("nested/a.txt: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, ".DS_Store")); err == nil {
		t.Fatal("expected .DS_Store to be skipped")
	}
}

func TestCopyPackageResourcesMissingOK(t *testing.T) {
	pkg := t.TempDir()
	dest := filepath.Join(t.TempDir(), "Resources")
	if err := copyPackageResources(pkg, dest, nil); err != nil {
		t.Fatal(err)
	}
}
