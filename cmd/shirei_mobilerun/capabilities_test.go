package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestParseCapabilitiesFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, capabilitiesFileName)
	content := `
# comment
camera
photos  # trailing
camera

photos-add
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	toks, err := parseCapabilitiesFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"camera", "photos", "photos-add"}
	if len(toks) != len(want) {
		t.Fatalf("got %v want %v", toks, want)
	}
	for i := range want {
		if toks[i] != want[i] {
			t.Fatalf("got %v want %v", toks, want)
		}
	}
}

func TestExpandCapabilities(t *testing.T) {
	r, err := expandCapabilities([]string{"camera", "photos", "camera"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.IOS) < 2 {
		t.Fatalf("ios usages: %+v", r.IOS)
	}
	foundCam := false
	for _, u := range r.IOS {
		if u.Key == "NSCameraUsageDescription" {
			foundCam = true
		}
	}
	if !foundCam {
		t.Fatalf("missing camera usage: %+v", r.IOS)
	}
	foundAndroidCam := false
	for _, p := range r.Android {
		if p == "android.permission.CAMERA" {
			foundAndroidCam = true
		}
	}
	if !foundAndroidCam {
		t.Fatalf("missing CAMERA: %v", r.Android)
	}
}

func TestExpandUnknownTokenIgnored(t *testing.T) {
	var warned []string
	logf := func(format string, args ...any) {
		warned = append(warned, fmt.Sprintf(format, args...))
	}
	r, err := expandCapabilities([]string{"not-a-real-token", "camera"}, logf)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Tokens) != 1 || r.Tokens[0] != "camera" {
		t.Fatalf("tokens = %v, want [camera]", r.Tokens)
	}
	if len(warned) != 1 || !strings.Contains(warned[0], "unknown capability token") {
		t.Fatalf("warning = %v", warned)
	}
	foundCam := false
	for _, p := range r.Android {
		if p == "android.permission.CAMERA" {
			foundCam = true
		}
	}
	if !foundCam {
		t.Fatalf("camera still required: %v", r.Android)
	}
}

func TestExpandOrientationConflictUsesLast(t *testing.T) {
	var warned []string
	logf := func(format string, args ...any) {
		warned = append(warned, fmt.Sprintf(format, args...))
	}
	// Last token in the slice wins for this expansion pass.
	r, err := expandCapabilities([]string{"orientation-portrait", "orientation-landscape"}, logf)
	if err != nil {
		t.Fatal(err)
	}
	if r.Orientation != "landscape" {
		t.Fatalf("orientation = %q, want landscape (last in list)", r.Orientation)
	}
	if len(warned) != 1 || !strings.Contains(warned[0], "conflicting orientation") {
		t.Fatalf("warning = %v", warned)
	}
}

func TestAndroidPermissionXMLSkipsInternet(t *testing.T) {
	xml := androidPermissionXML([]string{
		"android.permission.INTERNET",
		"android.permission.CAMERA",
	})
	if !strings.Contains(xml, "CAMERA") {
		t.Fatalf("missing CAMERA: %s", xml)
	}
	if strings.Contains(xml, "INTERNET") {
		t.Fatalf("INTERNET should be skipped (host baseline): %s", xml)
	}
}
