package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// capabilitiesFileName is the module-root declaration file for privacy /
// packaging needs. One token per line (known tokens from capabilityCatalog).
const capabilitiesFileName = "shirei.capabilities"

// iosUsage is one Info.plist privacy purpose string.
type iosUsage struct {
	Key string
	// Default English text for dev builds (mobilerun injects this).
	Text string
}

// capSpec is the closed map from a coarse token to platform declarations.
type capSpec struct {
	// Desc is a one-line product meaning (docs / errors).
	Desc string
	IOS  []iosUsage
	// Android is uses-permission android:name values (full name).
	Android []string
	// Orientation is packaging launch lock: "" (any), "landscape", or "portrait".
	// Injected into Info.plist / AndroidManifest; not a privacy permission.
	Orientation string
}

// capabilityCatalog maps known tokens to platform packaging. Unknown tokens
// in a capabilities file are warned and ignored (build continues).
var capabilityCatalog = map[string]capSpec{
	"camera": {
		Desc: "capture photo/video with the camera",
		IOS: []iosUsage{{
			Key:  "NSCameraUsageDescription",
			Text: "This app uses the camera.",
		}},
		Android: []string{"android.permission.CAMERA"},
	},
	"microphone": {
		Desc: "record or stream microphone audio",
		IOS: []iosUsage{{
			Key:  "NSMicrophoneUsageDescription",
			Text: "This app uses the microphone.",
		}},
		Android: []string{"android.permission.RECORD_AUDIO"},
	},
	"photos": {
		Desc: "read the photo library / pick images",
		IOS: []iosUsage{{
			Key:  "NSPhotoLibraryUsageDescription",
			Text: "This app accesses your photo library.",
		}},
		Android: []string{"android.permission.READ_MEDIA_IMAGES"},
	},
	"photos-add": {
		Desc: "save images into the photo library only",
		IOS: []iosUsage{{
			Key:  "NSPhotoLibraryAddUsageDescription",
			Text: "This app saves photos to your library.",
		}},
		// MediaStore inserts typically need no extra dangerous permission.
		Android: nil,
	},
	"videos": {
		Desc: "read videos from shared storage",
		IOS: []iosUsage{{
			// iOS: same library string as photos for v1 (picker / library).
			Key:  "NSPhotoLibraryUsageDescription",
			Text: "This app accesses your photo library.",
		}},
		Android: []string{"android.permission.READ_MEDIA_VIDEO"},
	},
	"location": {
		Desc: "location while the app is in use",
		IOS: []iosUsage{{
			Key:  "NSLocationWhenInUseUsageDescription",
			Text: "This app uses your location while you use it.",
		}},
		Android: []string{
			"android.permission.ACCESS_COARSE_LOCATION",
			"android.permission.ACCESS_FINE_LOCATION",
		},
	},
	"location-always": {
		Desc: "location in the background",
		IOS: []iosUsage{
			{
				Key:  "NSLocationWhenInUseUsageDescription",
				Text: "This app uses your location while you use it.",
			},
			{
				Key:  "NSLocationAlwaysAndWhenInUseUsageDescription",
				Text: "This app uses your location in the background.",
			},
		},
		Android: []string{
			"android.permission.ACCESS_COARSE_LOCATION",
			"android.permission.ACCESS_FINE_LOCATION",
			"android.permission.ACCESS_BACKGROUND_LOCATION",
		},
	},
	"contacts": {
		Desc: "access contacts",
		IOS: []iosUsage{{
			Key:  "NSContactsUsageDescription",
			Text: "This app accesses your contacts.",
		}},
		Android: []string{"android.permission.READ_CONTACTS"},
	},
	"calendar": {
		Desc: "access calendar",
		IOS: []iosUsage{{
			Key:  "NSCalendarsUsageDescription",
			Text: "This app accesses your calendar.",
		}},
		Android: []string{"android.permission.READ_CALENDAR"},
	},
	"bluetooth": {
		Desc: "Bluetooth accessories",
		IOS: []iosUsage{{
			Key:  "NSBluetoothAlwaysUsageDescription",
			Text: "This app uses Bluetooth.",
		}},
		Android: []string{
			"android.permission.BLUETOOTH_CONNECT",
			"android.permission.BLUETOOTH_SCAN",
		},
	},
	"local-network": {
		Desc: "local network / LAN discovery",
		IOS: []iosUsage{{
			Key:  "NSLocalNetworkUsageDescription",
			Text: "This app uses the local network.",
		}},
		Android: nil,
	},
	"notifications": {
		Desc:    "post user-visible notifications",
		IOS:     nil, // runtime permission on modern iOS; no purpose string in v1
		Android: []string{"android.permission.POST_NOTIFICATIONS"},
	},
	"internet": {
		Desc:    "outbound network access",
		IOS:     nil,
		Android: []string{"android.permission.INTERNET"},
	},
	"network-state": {
		Desc:    "query network connectivity",
		IOS:     nil,
		Android: []string{"android.permission.ACCESS_NETWORK_STATE"},
	},
	"vibrate": {
		Desc:    "vibration motor",
		IOS:     nil,
		Android: []string{"android.permission.VIBRATE"},
	},
	"nfc": {
		Desc: "NFC",
		IOS: []iosUsage{{
			Key:  "NFCReaderUsageDescription",
			Text: "This app uses NFC.",
		}},
		Android: []string{"android.permission.NFC"},
	},
	"motion": {
		Desc: "motion / activity sensors",
		IOS: []iosUsage{{
			Key:  "NSMotionUsageDescription",
			Text: "This app uses motion data.",
		}},
		Android: []string{"android.permission.ACTIVITY_RECOGNITION"},
	},
	"biometrics": {
		Desc: "Face ID / biometrics prompt copy",
		IOS: []iosUsage{{
			Key:  "NSFaceIDUsageDescription",
			Text: "This app uses Face ID.",
		}},
		Android: nil,
	},
	"speech": {
		Desc: "speech recognition",
		IOS: []iosUsage{{
			Key:  "NSSpeechRecognitionUsageDescription",
			Text: "This app uses speech recognition.",
		}},
		Android: nil,
	},
	"tracking": {
		Desc: "App Tracking Transparency",
		IOS: []iosUsage{{
			Key:  "NSUserTrackingUsageDescription",
			Text: "This app requests tracking permission.",
		}},
		Android: nil,
	},
	// Packaging-only: OS launches in this orientation (avoids software
	// force-rotate at first paint). Still set Host.PreferredOrientation at
	// runtime for in-app consistency.
	"orientation-landscape": {
		Desc:        "launch and support landscape only",
		Orientation: "landscape",
	},
	"orientation-portrait": {
		Desc:        "launch and support portrait only",
		Orientation: "portrait",
	},
}

// ResolvedCapabilities is the union of tokens from the app's module deps,
// expanded to platform packaging entries.
type ResolvedCapabilities struct {
	Tokens  []string
	IOS     []iosUsage // de-duped by Key (first text wins)
	Android []string   // de-duped permission names, sorted
	// Orientation is "" (any / default host template), "landscape", or "portrait".
	Orientation string
}

// collectCapabilities walks modules in the build dependency graph of pkgDir's
// package main (go list -deps) and unions shirei.capabilities files.
func collectCapabilities(pkgDir string, logf logFn) (ResolvedCapabilities, error) {
	var zero ResolvedCapabilities
	dirs, err := moduleDirsForPackage(pkgDir)
	if err != nil {
		return zero, err
	}
	tokenSet := map[string]bool{}
	var sources []string
	for _, dir := range dirs {
		path := filepath.Join(dir, capabilitiesFileName)
		toks, err := parseCapabilitiesFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return zero, fmt.Errorf("%s: %w", path, err)
		}
		if len(toks) == 0 {
			continue
		}
		sources = append(sources, path)
		for _, t := range toks {
			tokenSet[t] = true
		}
	}
	tokens := make([]string, 0, len(tokenSet))
	for t := range tokenSet {
		tokens = append(tokens, t)
	}
	sort.Strings(tokens)
	if logf != nil && len(tokens) > 0 {
		logf("— capabilities %v", tokens)
		for _, s := range sources {
			logf("    from %s", s)
		}
	}
	return expandCapabilities(tokens, logf)
}

func expandCapabilities(tokens []string, logf logFn) (ResolvedCapabilities, error) {
	var out ResolvedCapabilities
	iosByKey := map[string]string{}
	androidSet := map[string]bool{}
	var known []string
	for _, t := range tokens {
		spec, ok := capabilityCatalog[t]
		if !ok {
			if logf != nil {
				logf("warning: unknown capability token %q (ignored; see docs/mobile-extensions.md)", t)
			}
			continue
		}
		known = append(known, t)
		if spec.Orientation != "" {
			if out.Orientation != "" && out.Orientation != spec.Orientation {
				if logf != nil {
					logf("warning: conflicting orientation tokens (%q then %q); using %q",
						out.Orientation, spec.Orientation, spec.Orientation)
				}
			}
			// Last known orientation token wins (order is sorted token names
			// after union — do not rely on this as a stable app API).
			out.Orientation = spec.Orientation
		}
		for _, u := range spec.IOS {
			if _, exists := iosByKey[u.Key]; !exists {
				iosByKey[u.Key] = u.Text
			}
		}
		for _, p := range spec.Android {
			androidSet[p] = true
		}
	}
	out.Tokens = known
	for k, text := range iosByKey {
		out.IOS = append(out.IOS, iosUsage{Key: k, Text: text})
	}
	sort.Slice(out.IOS, func(i, j int) bool { return out.IOS[i].Key < out.IOS[j].Key })
	for p := range androidSet {
		out.Android = append(out.Android, p)
	}
	sort.Strings(out.Android)
	return out, nil
}

// parseCapabilitiesFile reads one token per line. Returns os.ErrNotExist if missing.
func parseCapabilitiesFile(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var tokens []string
	seen := map[string]bool{}
	sc := bufio.NewScanner(f)
	lineNo := 0
	for sc.Scan() {
		lineNo++
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// Allow optional trailing comments: camera  # note
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		if line == "" {
			continue
		}
		if strings.ContainsAny(line, " \t") {
			return nil, fmt.Errorf("line %d: tokens must be a single word (got %q)", lineNo, line)
		}
		if !seen[line] {
			seen[line] = true
			tokens = append(tokens, line)
		}
	}
	return tokens, sc.Err()
}

// moduleDirsForPackage returns unique module root directories for all packages
// in the dependency graph of package main at pkgDir.
func moduleDirsForPackage(pkgDir string) ([]string, error) {
	// Prefer listing the directory as a package (works for package main).
	cmd := exec.Command("go", "list", "-deps", "-f", `{{if .Module}}{{.Module.Dir}}{{end}}`, ".")
	cmd.Dir = pkgDir
	// Inherit env (GOWORK, GOPATH, …).
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok {
			return nil, fmt.Errorf("go list -deps: %w\n%s", err, ee.Stderr)
		}
		return nil, fmt.Errorf("go list -deps: %w", err)
	}
	seen := map[string]bool{}
	var dirs []string
	for _, line := range strings.Split(string(out), "\n") {
		dir := strings.TrimSpace(line)
		if dir == "" || seen[dir] {
			continue
		}
		seen[dir] = true
		dirs = append(dirs, dir)
	}
	// Always include the package's own module (in case list edge cases).
	if mod := findModuleRoot(pkgDir); mod != "" && !seen[mod] {
		dirs = append(dirs, mod)
	}
	sort.Strings(dirs)
	return dirs, nil
}

func findModuleRoot(dir string) string {
	for d := dir; d != "" && d != string(filepath.Separator); d = filepath.Dir(d) {
		if _, err := os.Stat(filepath.Join(d, "go.mod")); err == nil {
			return d
		}
		parent := filepath.Dir(d)
		if parent == d {
			break
		}
	}
	return ""
}

// writeIOSUsageFile writes KEY=text lines for ios-run.sh to inject into Info.plist.
// Values are single-line; newlines in text are replaced with spaces.
func writeIOSUsageFile(path string, usages []iosUsage) error {
	if len(usages) == 0 {
		// Empty file is fine — script no-ops.
		return os.WriteFile(path, nil, 0o644)
	}
	var b strings.Builder
	for _, u := range usages {
		text := strings.ReplaceAll(u.Text, "\n", " ")
		text = strings.ReplaceAll(text, "\r", "")
		// KEY must not contain '='.
		b.WriteString(u.Key)
		b.WriteByte('=')
		b.WriteString(text)
		b.WriteByte('\n')
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// androidPermissionXML returns <uses-permission/> lines for extra capabilities
// (does not include the baseline INTERNET always present in the host template).
func androidPermissionXML(perms []string) string {
	if len(perms) == 0 {
		return ""
	}
	// Skip INTERNET if listed — template already has it.
	var b strings.Builder
	for _, p := range perms {
		if p == "android.permission.INTERNET" {
			continue
		}
		b.WriteString(fmt.Sprintf("  <uses-permission android:name=%q/>\n", p))
	}
	return b.String()
}
