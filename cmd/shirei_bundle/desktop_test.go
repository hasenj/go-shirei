package main

import (
	"archive/tar"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Archive layout is easy to break (paths, top-level folder, compression).
func TestTarGzipDirRoundTrip(t *testing.T) {
	src := t.TempDir()
	inner := filepath.Join(src, "MyApp")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inner, "MyApp"), []byte("bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inner, "MyApp.desktop"), []byte("[Desktop Entry]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "out.tar.gz")
	if err := tarGzipDir(inner, dest); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(dest)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	var names []string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, hdr.Name)
		if !hdr.FileInfo().IsDir() {
			_, _ = io.Copy(io.Discard, tr)
		}
	}
	joined := strings.Join(names, "\n")
	if !strings.Contains(joined, "MyApp/MyApp") {
		t.Fatalf("binary missing from tar:\n%s", joined)
	}
	if !strings.Contains(joined, "MyApp/MyApp.desktop") {
		t.Fatalf("desktop missing from tar:\n%s", joined)
	}
}

func TestZipDirRoundTrip(t *testing.T) {
	src := t.TempDir()
	inner := filepath.Join(src, "MyApp")
	if err := os.MkdirAll(inner, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inner, "MyApp.exe"), []byte("bin"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "out.zip")
	if err := zipDir(inner, dest); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(dest)
	if err != nil || st.Size() == 0 {
		t.Fatalf("zip missing or empty: %v", err)
	}
}
