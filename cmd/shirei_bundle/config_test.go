package main

import "testing"

// Platform identity inherit/override is the non-obvious product rule for
// per-platform Name / prefix / App ID / icon — worth locking.
func TestPlatformIdentityInheritsAndOverrides(t *testing.T) {
	a := App{
		Package:     "myapp",
		Name:        "My App",
		AppIDPrefix: "systems.judi",
		AppID:       "systems.judi.myapp",
		IconPath:    "icon.png",
		IOS: &IOSConfig{
			TeamID:     "TEAM",
			Method:     "debugging",
			ReleaseDir: "releases",
		},
		Android: &AndroidConfig{
			PlatformIdentity: PlatformIdentity{
				Name:        "My App Android",
				AppIDPrefix: "systems.judi.and",
				AppID:       "systems.judi.myapp.android",
				IconPath:    "android-icon.png",
			},
			Keystore:   "/tmp/ks.jks",
			KeyAlias:   "key",
			ReleaseDir: "releases",
			Arch:       "arm64",
		},
		MacOS: defaultMacOSConfig(),
	}

	// iOS: empty override → shared
	if got := a.platformName(platformIOS); got != "My App" {
		t.Fatalf("ios name = %q", got)
	}
	if got := a.platformAppIDPrefix(platformIOS); got != "systems.judi" {
		t.Fatalf("ios prefix = %q", got)
	}
	if got := a.platformBundleID(platformIOS); got != "systems.judi.myapp" {
		t.Fatalf("ios bundle id = %q, want shared", got)
	}
	if got := a.platformIconPath(platformIOS); got != "icon.png" {
		t.Fatalf("ios icon = %q, want shared", got)
	}

	// Android: full override
	if got := a.platformName(platformAndroid); got != "My App Android" {
		t.Fatalf("android name = %q", got)
	}
	if got := a.platformAppIDPrefix(platformAndroid); got != "systems.judi.and" {
		t.Fatalf("android prefix = %q", got)
	}
	if got := a.platformBundleID(platformAndroid); got != "systems.judi.myapp.android" {
		t.Fatalf("android bundle id = %q", got)
	}
	if got := a.platformIconPath(platformAndroid); got != "android-icon.png" {
		t.Fatalf("android icon = %q", got)
	}

	// macOS: empty override → shared
	if got := a.platformBundleID(platformMacOS); got != "systems.judi.myapp" {
		t.Fatalf("macos bundle id = %q", got)
	}

	// Clearing Android overrides restores inherit
	a.Android.PlatformIdentity = PlatformIdentity{}
	if got := a.platformName(platformAndroid); got != "My App" {
		t.Fatalf("android name after clear = %q", got)
	}
	if got := a.platformBundleID(platformAndroid); got != "systems.judi.myapp" {
		t.Fatalf("android after clear = %q", got)
	}
	if got := a.platformIconPath(platformAndroid); got != "icon.png" {
		t.Fatalf("android icon after clear = %q", got)
	}

	// Prefix-only platform override with no full AppID → construct from prefix + package.
	a2 := App{
		Package:     "piano",
		Name:        "Piano",
		AppIDPrefix: "systems.judi",
		IOS: &IOSConfig{
			PlatformIdentity: PlatformIdentity{AppIDPrefix: "systems.judi.mobile"},
		},
	}
	if got, want := a2.platformBundleID(platformIOS), "systems.judi.mobile.piano"; got != want {
		t.Fatalf("prefix-only construct: got %q want %q", got, want)
	}
}
