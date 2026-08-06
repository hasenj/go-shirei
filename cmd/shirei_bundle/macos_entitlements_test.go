package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteMacOSAppStoreEntitlementsMinimal(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "ents.plist")
	if err := writeMacOSAppStoreEntitlementsMinimal(path, "TEAM123", "systems.judi.piano", nil); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(data)
	for _, want := range []string{
		"com.apple.security.app-sandbox",
		"com.apple.application-identifier",
		"com.apple.developer.team-identifier",
		"TEAM123.systems.judi.piano",
		"TEAM123",
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("entitlements missing %q:\n%s", want, s)
		}
	}
	// Bare application-identifier is iOS-only; must not appear on macOS.
	if strings.Contains(s, "<key>application-identifier</key>") {
		t.Fatalf("must not include iOS application-identifier:\n%s", s)
	}
}

func TestExtractProfileEntitlementsIfPresent(t *testing.T) {
	// Optional smoke: use first Mac App Store profile on the machine, if any.
	profiles := listMacAppStoreProfiles("")
	if len(profiles) == 0 {
		t.Skip("no Mac App Store profiles on this machine")
	}
	dir := t.TempDir()
	dest := filepath.Join(dir, "from-profile.plist")
	if err := extractProfileEntitlementsPlist(profiles[0].Path, dest); err != nil {
		t.Fatal(err)
	}
	// Full write path must succeed and keep an application identifier.
	out := filepath.Join(dir, "merged.plist")
	if err := writeMacOSAppStoreEntitlements(out, profiles[0].Path, nil); err != nil {
		t.Fatal(err)
	}
	// codesign-friendly XML should contain sandbox + some application-identifier key.
	raw, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if !strings.Contains(s, "com.apple.security.app-sandbox") {
		t.Fatalf("missing sandbox:\n%s", s)
	}
	if !strings.Contains(s, "com.apple.application-identifier") {
		t.Fatalf("missing com.apple.application-identifier:\n%s", s)
	}
	if strings.Contains(s, "<key>application-identifier</key>") {
		t.Fatalf("must not include iOS application-identifier:\n%s", s)
	}
	// plutil -lint
	if err := exec.Command("/usr/bin/plutil", "-lint", out).Run(); err != nil {
		t.Fatalf("plutil lint: %v", err)
	}
}
