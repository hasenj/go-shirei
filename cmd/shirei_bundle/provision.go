package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ProvisioningProfile is a decoded Apple provisioning profile on disk.
type ProvisioningProfile struct {
	Path        string
	Name        string
	BundleID    string // without team prefix
	TeamID      string
	Platforms   []string // e.g. iOS, OSX
	DistType    string   // STORE, DEVELOPMENT, … (when present)
	MacAppStore bool
}

// provisioningProfileDirs are places Xcode and manual installs put profiles.
func provisioningProfileDirs() []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}
	return []string{
		filepath.Join(home, "Library", "Developer", "Xcode", "UserData", "Provisioning Profiles"),
		filepath.Join(home, "Library", "MobileDevice", "Provisioning Profiles"),
	}
}

// listProvisioningProfiles scans known directories for readable profiles.
func listProvisioningProfiles() []ProvisioningProfile {
	var out []ProvisioningProfile
	seen := map[string]bool{}
	for _, dir := range provisioningProfileDirs() {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() {
				continue
			}
			name := e.Name()
			ext := strings.ToLower(filepath.Ext(name))
			if ext != ".mobileprovision" && ext != ".provisionprofile" {
				continue
			}
			path := filepath.Join(dir, name)
			if abs, err := filepath.Abs(path); err == nil {
				path = abs
			}
			if seen[path] {
				continue
			}
			p, err := decodeProvisioningProfile(path)
			if err != nil {
				continue
			}
			seen[path] = true
			out = append(out, p)
		}
	}
	return out
}

// listMacAppStoreProfiles returns Mac App Store profiles, optionally filtered by bundle id.
func listMacAppStoreProfiles(bundleID string) []ProvisioningProfile {
	bundleID = strings.TrimSpace(bundleID)
	var out []ProvisioningProfile
	for _, p := range listProvisioningProfiles() {
		if !p.MacAppStore {
			continue
		}
		if bundleID != "" && p.BundleID != "" && p.BundleID != bundleID && p.BundleID != "*" {
			continue
		}
		out = append(out, p)
	}
	return out
}

// resolveMacAppStoreProfile picks an explicit path or auto-finds for bundleID.
func resolveMacAppStoreProfile(explicit, bundleID string) (string, error) {
	explicit = strings.TrimSpace(explicit)
	if explicit != "" {
		if st, err := os.Stat(explicit); err != nil || st.IsDir() {
			return "", fmt.Errorf("provisioning profile not found: %s", explicit)
		}
		if _, err := decodeProvisioningProfile(explicit); err != nil {
			return "", fmt.Errorf("read provisioning profile: %w", err)
		}
		return explicit, nil
	}
	bundleID = strings.TrimSpace(bundleID)
	matches := listMacAppStoreProfiles(bundleID)
	var exact []ProvisioningProfile
	for _, p := range matches {
		if p.BundleID == bundleID {
			exact = append(exact, p)
		}
	}
	if len(exact) > 0 {
		return exact[0].Path, nil
	}
	if len(matches) == 1 {
		return matches[0].Path, nil
	}
	if len(matches) > 1 {
		return "", fmt.Errorf("multiple Mac App Store profiles for %s — pick one in Edit configuration", bundleID)
	}
	return "", fmt.Errorf("no Mac App Store provisioning profile found for %s (Xcode → Settings → Accounts → Download Manual Profiles, or Browse to the downloaded file)", bundleID)
}

// embedProvisioningProfile copies the profile into Contents/embedded.provisionprofile.
func embedProvisioningProfile(appDir, profilePath string) error {
	profilePath = strings.TrimSpace(profilePath)
	if profilePath == "" {
		return fmt.Errorf("provisioning profile path is empty")
	}
	contents := filepath.Join(appDir, "Contents")
	if err := os.MkdirAll(contents, 0o755); err != nil {
		return err
	}
	dest := filepath.Join(contents, "embedded.provisionprofile")
	return copyFile(profilePath, dest)
}

// decodeProvisioningProfile uses security cms -D + PlistBuddy (keys may contain dots).
func decodeProvisioningProfile(path string) (ProvisioningProfile, error) {
	var zero ProvisioningProfile
	cms, err := exec.Command("security", "cms", "-D", "-i", path).Output()
	if err != nil {
		return zero, err
	}
	tmp, err := os.CreateTemp("", "shirei-prov-*.plist")
	if err != nil {
		return zero, err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(cms); err != nil {
		tmp.Close()
		return zero, err
	}
	tmp.Close()

	name := plistBuddy(tmpPath, ":Name")
	platforms := plistBuddyArray(tmpPath, ":Platform")
	profileDist := plistBuddy(tmpPath, ":ProfileDistributionType")

	appID := plistBuddy(tmpPath, ":Entitlements:application-identifier")
	if appID == "" {
		appID = plistBuddy(tmpPath, ":Entitlements:com.apple.application-identifier")
	}
	teamID := plistBuddy(tmpPath, ":Entitlements:com.apple.developer.team-identifier")
	if teamID == "" {
		teams := plistBuddyArray(tmpPath, ":TeamIdentifier")
		if len(teams) > 0 {
			teamID = teams[0]
		}
	}
	bundleID := ""
	if appID != "" {
		if i := strings.IndexByte(appID, '.'); i >= 0 {
			if teamID == "" {
				teamID = appID[:i]
			}
			bundleID = appID[i+1:]
		} else {
			bundleID = appID
		}
	}

	isMac := false
	isIOS := false
	for _, p := range platforms {
		u := strings.ToUpper(strings.TrimSpace(p))
		switch u {
		case "OSX", "MACOS", "MACOSX":
			isMac = true
		case "IOS", "IPADOS", "TVOS", "WATCHOS", "XR", "XROS", "VISIONOS":
			isIOS = true
		}
		// Platform can be numeric on some iOS profiles — ignore.
	}

	// Mac App Store: OSX platform; prefer STORE when set; exclude pure development/direct.
	macStore := false
	if isMac {
		switch strings.ToUpper(strings.TrimSpace(profileDist)) {
		case "STORE":
			macStore = true
		case "DEVELOPMENT", "DIRECT", "ADHOC":
			macStore = false
		default:
			// Older Mac store profiles often omit ProfileDistributionType.
			// Treat OSX + not development (get-task-allow) as store-eligible.
			getTask := plistBuddy(tmpPath, ":Entitlements:get-task-allow")
			macStore = getTask != "true" && !isIOS
		}
	}

	return ProvisioningProfile{
		Path:        path,
		Name:        name,
		BundleID:    bundleID,
		TeamID:      teamID,
		Platforms:   platforms,
		DistType:    profileDist,
		MacAppStore: macStore,
	}, nil
}

func plistBuddy(plistPath, key string) string {
	out, err := exec.Command("/usr/libexec/PlistBuddy", "-c", "Print "+key, plistPath).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func plistBuddyArray(plistPath, key string) []string {
	// Print :Platform may return "Array {\n    OSX\n}" or fail for single string.
	raw := plistBuddy(plistPath, key)
	if raw == "" {
		return nil
	}
	if !strings.HasPrefix(raw, "Array {") {
		return []string{raw}
	}
	var out []string
	for i := 0; i < 16; i++ {
		s := plistBuddy(plistPath, fmt.Sprintf("%s:%d", key, i))
		if s == "" {
			break
		}
		out = append(out, s)
	}
	return out
}
