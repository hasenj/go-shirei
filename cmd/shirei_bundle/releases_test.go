package main

import "testing"

func TestVersionListGroupsPlatforms(t *testing.T) {
	log := ReleaseLog{Apps: map[string]*AppReleaseRecord{
		"app1": {
			History: []ReleaseEntry{
				{Platform: platformIOS, Version: "1.0.1", Build: "3", Path: "/a.ipa", At: "2026-07-02T12:00:00Z"},
				{Platform: platformAndroid, Version: "1.0.1", Build: "2", Path: "/a.apk", At: "2026-07-02T11:00:00Z"},
				{Platform: platformIOS, Version: "1.0.0", Build: "1", Path: "/old.ipa", At: "2026-07-01T10:00:00Z"},
			},
		},
	}}
	vs := log.versionList("app1")
	if len(vs) != 2 {
		t.Fatalf("versions = %d, want 2", len(vs))
	}
	if vs[0].Version != "1.0.1" {
		t.Fatalf("newest version = %s, want 1.0.1", vs[0].Version)
	}
	if len(vs[0].Platforms) != 2 {
		t.Fatalf("1.0.1 platforms = %d, want 2", len(vs[0].Platforms))
	}
	if _, ok := vs[0].Platforms[platformIOS]; !ok {
		t.Fatal("missing ios on 1.0.1")
	}
	if _, ok := vs[0].Platforms[platformAndroid]; !ok {
		t.Fatal("missing android on 1.0.1")
	}
	if vs[1].Version != "1.0.0" {
		t.Fatalf("second version = %s, want 1.0.0", vs[1].Version)
	}
}

func TestNormalizeMacOSConfig(t *testing.T) {
	c := &MacOSConfig{Arch: "amd64"} // legacy
	normalizeMacOSConfig(c)
	if !c.ArchAMD64 || c.ArchARM64 {
		t.Fatalf("legacy arch: arm64=%v amd64=%v", c.ArchARM64, c.ArchAMD64)
	}
	if !c.SelfDist || c.AppStore {
		t.Fatalf("default outputs: self=%v store=%v", c.SelfDist, c.AppStore)
	}
	if c.macOSArchLabel() != "amd64" {
		t.Fatalf("label = %s", c.macOSArchLabel())
	}
	c.ArchARM64 = true
	if c.macOSArchLabel() != "universal" {
		t.Fatalf("universal label = %s", c.macOSArchLabel())
	}
}

func TestDecodeMacAppStoreProfileIfPresent(t *testing.T) {
	// Integration-ish: only runs when Xcode profiles exist on this machine.
	found := listMacAppStoreProfiles("systems.judi.piano")
	if len(found) == 0 {
		found = listMacAppStoreProfiles("")
	}
	if len(found) == 0 {
		t.Skip("no Mac App Store profiles on disk")
	}
	p := found[0]
	if p.Path == "" || !p.MacAppStore {
		t.Fatalf("unexpected profile: %+v", p)
	}
	t.Logf("profile %q bundle=%s platform=%v", p.Name, p.BundleID, p.Platforms)
}

func TestVersionMutableAndDelete(t *testing.T) {
	log := ReleaseLog{Apps: map[string]*AppReleaseRecord{
		"app1": {
			History: []ReleaseEntry{
				{Platform: platformIOS, Version: "1.0.0", Build: "2", Path: "/a.ipa", At: "2026-07-02T12:00:00Z"},
				{Platform: platformMacOS, Version: "1.0.0", Build: "1", Path: "/a.zip", At: "2026-07-02T11:00:00Z"},
			},
		},
	}}
	if !log.versionIsMutable("app1", "1.0.0") {
		t.Fatal("latest open should be mutable")
	}
	if log.versionIsMutable("app1", "0.9.0") {
		t.Fatal("unknown older should not be mutable without history after release")
	}
	// New version while latest open: not mutable
	if log.versionIsMutable("app1", "1.0.1") {
		t.Fatal("new version while open latest should be blocked")
	}
	log.markVersionReleased("app1", "1.0.0")
	if log.versionIsMutable("app1", "1.0.0") {
		t.Fatal("released should freeze")
	}
	if !log.versionIsMutable("app1", "1.0.1") {
		t.Fatal("new version after release should be mutable")
	}
	n, paths := log.deletePlatformBuild("app1", "1.0.0", platformIOS)
	// still can delete from history even if released? currently delete is UI-gated only.
	// Function still removes. After release UI won't call it.
	if n != 1 || len(paths) != 1 {
		t.Fatalf("delete: n=%d paths=%v", n, paths)
	}
	if _, ok := log.entryFor("app1", "1.0.0", platformIOS); ok {
		t.Fatal("ios entry should be gone")
	}
}

func TestEntryForAndMarkNotarized(t *testing.T) {
	log := ReleaseLog{Apps: map[string]*AppReleaseRecord{
		"app1": {
			History: []ReleaseEntry{
				{Platform: platformMacOS, Version: "0.2.0", Build: "5", Path: "/z.zip", At: "2026-07-03T00:00:00Z"},
			},
		},
	}}
	e, ok := log.entryFor("app1", "0.2.0", platformMacOS)
	if !ok || e.Path != "/z.zip" {
		t.Fatalf("entryFor = %+v ok=%v", e, ok)
	}
	if !log.markNotarized("app1", "0.2.0", platformMacOS) {
		t.Fatal("markNotarized failed")
	}
	e, _ = log.entryFor("app1", "0.2.0", platformMacOS)
	if !e.Notarized {
		t.Fatal("expected notarized")
	}
}
