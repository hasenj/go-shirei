package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// MacOSBundleResult is the set of artifacts from one macOS bundle job.
type MacOSBundleResult struct {
	AppPath string // .app (self-dist copy when produced; else App Store staging)
	ZipPath string // self-dist zip
	PkgPath string // App Store productbuild package
}

// Primary returns the preferred path for release history (zip > pkg > app).
func (r MacOSBundleResult) Primary() string {
	if r.ZipPath != "" {
		return r.ZipPath
	}
	if r.PkgPath != "" {
		return r.PkgPath
	}
	return r.AppPath
}

// Extra returns non-primary artifact paths.
func (r MacOSBundleResult) Extra() []string {
	prim := r.Primary()
	var out []string
	for _, p := range []string{r.ZipPath, r.PkgPath, r.AppPath} {
		if p != "" && p != prim {
			out = append(out, p)
		}
	}
	return out
}

// macosBundleSteps builds the progress step list from output modes.
func macosBundleSteps(selfDist, appStore bool) []string {
	s := []string{"Build binary", "Assemble .app"}
	if selfDist {
		s = append(s, "Self-dist (codesign + zip)")
	}
	if appStore {
		s = append(s, "App Store (codesign + pkg)")
	}
	return s
}

// macOS notarize steps (run from the version page after a successful self-dist build).
var macosNotarizeSteps = []string{
	"Zip for notarytool",
	"Submit to Apple",
	"Staple ticket",
	"Rewrite distribution zip",
}

// MacOSBundleOpts is one macOS release packaging job.
type MacOSBundleOpts struct {
	PkgDir     string
	BundleID   string
	Name       string
	Version    string
	Build      string
	IconPath   string
	Archs      []string // arm64 and/or amd64; both → universal via lipo
	SelfDist   bool
	AppStore   bool
	// Identity: Developer ID Application for self-dist (empty → auto).
	Identity string
	// AppStoreIdentity: Mac App Distribution / 3rd Party Mac Developer Application (empty → auto).
	AppStoreIdentity string
	// InstallerIdentity: signs the .pkg (empty → auto Mac App Store installer).
	InstallerIdentity string
	// ProvisionProfile: Mac App Store profile path (empty → auto by bundle id).
	ProvisionProfile string
	// Category is LSApplicationCategoryType UTI (required for Mac App Store).
	Category   string
	ReleaseDir string
	Logf       func(format string, args ...any)
	OnStep     func(step int)
	Cancelled  func() bool
	SetRunningCmd func(cmd *exec.Cmd)
}

// MacOSNotarizeOpts notarizes an already-built .app and rewrites its distribution zip.
type MacOSNotarizeOpts struct {
	AppPath       string
	ZipPath       string
	Profile       string
	Logf          func(format string, args ...any)
	OnStep        func(step int)
	Cancelled     func() bool
	SetRunningCmd func(cmd *exec.Cmd)
}

func (o MacOSBundleOpts) logf(format string, args ...any) {
	if o.Logf != nil {
		o.Logf(format, args...)
	}
}

func bundleMacOS(o MacOSBundleOpts) (MacOSBundleResult, error) {
	var zero MacOSBundleResult
	if runtime.GOOS != "darwin" {
		return zero, fmt.Errorf("macOS bundling requires a Mac host (this host is %s)", runtime.GOOS)
	}
	if strings.TrimSpace(o.BundleID) == "" {
		return zero, fmt.Errorf("bundle id is required")
	}
	if st, err := os.Stat(o.PkgDir); err != nil || !st.IsDir() {
		return zero, fmt.Errorf("package dir not found: %s", o.PkgDir)
	}
	version := strings.TrimSpace(o.Version)
	build := strings.TrimSpace(o.Build)
	if version == "" || build == "" {
		return zero, fmt.Errorf("version and build are required")
	}
	if !o.SelfDist && !o.AppStore {
		return zero, fmt.Errorf("select at least one output (self-dist and/or App Store)")
	}

	archs, archLabel, err := normalizeMacOSArchs(o.Archs)
	if err != nil {
		return zero, err
	}

	display := strings.TrimSpace(o.Name)
	if display == "" {
		display = filepath.Base(o.PkgDir)
	}
	product := sanitizeProductName(display)
	if product == "" {
		product = "App"
	}

	moduleRoot, pkgSpec, err := resolvePackage(o.PkgDir)
	if err != nil {
		return zero, err
	}

	cache, err := os.UserCacheDir()
	if err != nil {
		return zero, err
	}
	buildDir := filepath.Join(cache, "shirei", "bundle-macos",
		product+"-"+fmt.Sprintf("%d", time.Now().Unix()))
	if err := os.MkdirAll(buildDir, 0o755); err != nil {
		return zero, err
	}
	o.logf("build dir: %s", buildDir)
	o.logf("arch: %s (%v)", archLabel, archs)
	o.logf("outputs: self-dist=%v app-store=%v", o.SelfDist, o.AppStore)

	stepN := 0
	step := func() error {
		if o.Cancelled != nil && o.Cancelled() {
			return fmt.Errorf("cancelled")
		}
		if o.OnStep != nil {
			o.OnStep(stepN)
		}
		stepN++
		return nil
	}

	// 1) go build (per arch) + optional lipo
	if err := step(); err != nil {
		return zero, err
	}
	binPath := filepath.Join(buildDir, product)
	if err := buildMacOSBinary(moduleRoot, pkgSpec, binPath, archs, o.logf, o.Cancelled, o.SetRunningCmd); err != nil {
		return zero, err
	}

	// 2) assemble unsigned .app template
	if err := step(); err != nil {
		return zero, err
	}
	category := strings.TrimSpace(o.Category)
	if category == "" {
		category = defaultMacOSCategory
	}
	templateApp := filepath.Join(buildDir, product+".app")
	if err := assembleMacOSApp(templateApp, product, display, o.BundleID, version, build, binPath, o.IconPath, category, o.PkgDir, o.logf); err != nil {
		return zero, err
	}

	outDir := o.ReleaseDir
	if outDir == "" {
		outDir = "releases"
	}
	if !filepath.IsAbs(outDir) {
		if abs, err := filepath.Abs(outDir); err == nil {
			outDir = abs
		}
	}
	destRoot, err := resolvePlatformReleaseDir(outDir, product, version, platformMacOS)
	if err != nil {
		return zero, err
	}

	var result MacOSBundleResult

	// 3) Self-dist: Developer ID codesign + zip
	if o.SelfDist {
		if err := step(); err != nil {
			return zero, err
		}
		identity := strings.TrimSpace(o.Identity)
		if identity == "" {
			identity, err = firstDeveloperIDIdentity()
			if err != nil {
				return zero, err
			}
			o.logf("self-dist identity: %s (auto)", identity)
		} else {
			o.logf("self-dist identity: %s", identity)
		}
		destApp := filepath.Join(destRoot, product+".app")
		_ = os.RemoveAll(destApp)
		if err := runCmdLog(exec.Command("cp", "-R", templateApp, destApp),
			o.logf, o.Cancelled, o.SetRunningCmd); err != nil {
			return zero, fmt.Errorf("copy .app: %w", err)
		}
		o.logf("— codesign (Developer ID, hardened runtime)")
		if err := runCmdLog(exec.Command("codesign",
			"--deep", "--force",
			"--options", "runtime",
			"--sign", identity,
			"--timestamp",
			destApp,
		), o.logf, o.Cancelled, o.SetRunningCmd); err != nil {
			if o.Cancelled != nil && o.Cancelled() {
				return zero, fmt.Errorf("cancelled")
			}
			return zero, fmt.Errorf("codesign self-dist: %w", err)
		}
		zipName := fmt.Sprintf("%s-%s-%s-macos-%s.zip", product, version, build, archLabel)
		zipPath := filepath.Join(destRoot, zipName)
		_ = os.Remove(zipPath)
		o.logf("— ditto zip %s", zipPath)
		if err := runCmdLog(exec.Command("ditto", "-c", "-k", "--keepParent", destApp, zipPath),
			o.logf, o.Cancelled, o.SetRunningCmd); err != nil {
			return zero, fmt.Errorf("distribution zip: %w", err)
		}
		result.AppPath = destApp
		result.ZipPath = zipPath
		o.logf("self-dist app: %s", destApp)
		o.logf("self-dist zip: %s", zipPath)
		o.logf("notarize later from Releases if needed")
	}

	// 4) App Store: Mac App Distribution codesign + productbuild .pkg
	if o.AppStore {
		if err := step(); err != nil {
			return zero, err
		}
		masID := strings.TrimSpace(o.AppStoreIdentity)
		if masID == "" {
			masID, err = firstMacAppStoreIdentity()
			if err != nil {
				return zero, err
			}
			o.logf("app-store identity: %s (auto)", masID)
		} else {
			o.logf("app-store identity: %s", masID)
		}
		masApp := filepath.Join(buildDir, product+"-mas.app")
		_ = os.RemoveAll(masApp)
		if err := runCmdLog(exec.Command("cp", "-R", templateApp, masApp),
			o.logf, o.Cancelled, o.SetRunningCmd); err != nil {
			return zero, fmt.Errorf("copy mas .app: %w", err)
		}
		// Embed Mac App Store provisioning profile (TestFlight + store eligibility).
		profilePath, err := resolveMacAppStoreProfile(o.ProvisionProfile, o.BundleID)
		if err != nil {
			return zero, err
		}
		o.logf("provisioning profile: %s", profilePath)
		if err := embedProvisioningProfile(masApp, profilePath); err != nil {
			return zero, fmt.Errorf("embed provisioning profile: %w", err)
		}
		// App Store / TestFlight: codesign entitlements must include the profile's
		// application-identifier (and team id). Sandbox alone is not enough (90886).
		entPath := filepath.Join(buildDir, "app-store.entitlements")
		if err := writeMacOSAppStoreEntitlements(entPath, profilePath, nil); err != nil {
			return zero, err
		}
		o.logf("— codesign entitlements: %s", entPath)
		o.logf("— codesign (Mac App Store + profile entitlements + sandbox)")
		if err := runCmdLog(exec.Command("codesign",
			"--deep", "--force",
			"--options", "runtime",
			"--entitlements", entPath,
			"--sign", masID,
			"--timestamp",
			masApp,
		), o.logf, o.Cancelled, o.SetRunningCmd); err != nil {
			if o.Cancelled != nil && o.Cancelled() {
				return zero, fmt.Errorf("cancelled")
			}
			return zero, fmt.Errorf("codesign app-store: %w", err)
		}
		pkgName := fmt.Sprintf("%s-%s-%s-macos-%s.pkg", product, version, build, archLabel)
		pkgPath := filepath.Join(destRoot, pkgName)
		_ = os.Remove(pkgPath)
		// productbuild places the app under /Applications in the install package.
		// Transporter requires a 3rd Party Mac Developer Installer signature on the .pkg.
		inst := strings.TrimSpace(o.InstallerIdentity)
		if inst == "" {
			var err error
			inst, err = firstMacInstallerIdentity()
			if err != nil {
				return zero, err
			}
			o.logf("installer identity: %s (auto)", inst)
		} else {
			o.logf("installer identity: %s", inst)
		}
		args := []string{"--sign", inst, "--component", masApp, "/Applications", pkgPath}
		o.logf("— productbuild (signed installer)")
		if err := runCmdLog(exec.Command("productbuild", args...),
			o.logf, o.Cancelled, o.SetRunningCmd); err != nil {
			if o.Cancelled != nil && o.Cancelled() {
				return zero, fmt.Errorf("cancelled")
			}
			return zero, fmt.Errorf("productbuild: %w", err)
		}
		result.PkgPath = pkgPath
		if result.AppPath == "" {
			// Keep a MAS-signed app copy under dest for inspection (only if no self-dist app).
			masDest := filepath.Join(destRoot, product+"-mas.app")
			_ = os.RemoveAll(masDest)
			_ = runCmdLog(exec.Command("cp", "-R", masApp, masDest), o.logf, o.Cancelled, o.SetRunningCmd)
			result.AppPath = masDest
		}
		o.logf("app-store pkg: %s", pkgPath)
		o.logf("upload the .pkg with Transporter (not the .app)")
	}

	return result, nil
}

// writeMacOSAppStoreEntitlements writes a codesign entitlements plist for MAS.
// It starts from the provisioning profile's Entitlements (application-identifier,
// team-identifier, …) so TestFlight accepts the signature, then forces App
// Sandbox and any extraKeys (optional com.apple.security.* booleans).
func writeMacOSAppStoreEntitlements(path, profilePath string, extraKeys []string) error {
	profilePath = strings.TrimSpace(profilePath)
	if profilePath == "" {
		return fmt.Errorf("provisioning profile is required for App Store entitlements")
	}
	// Base: full Entitlements dict from the profile.
	if err := extractProfileEntitlementsPlist(profilePath, path); err != nil {
		// Fallback: build from decoded team/bundle id only.
		p, derr := decodeProvisioningProfile(profilePath)
		if derr != nil {
			return fmt.Errorf("profile entitlements: %w (decode: %v)", err, derr)
		}
		if err := writeMacOSAppStoreEntitlementsMinimal(path, p.TeamID, p.BundleID, extraKeys); err != nil {
			return err
		}
	} else {
		// Force App Sandbox (Transporter rejects MAS apps without it).
		if err := plistBuddySetBool(path, ":com.apple.security.app-sandbox", true); err != nil {
			return fmt.Errorf("set app-sandbox: %w", err)
		}
		// Store distribution must not allow debugger attachment.
		_ = exec.Command("/usr/libexec/PlistBuddy", "-c", "Delete :get-task-allow", path).Run()
		for _, k := range extraKeys {
			k = strings.TrimSpace(k)
			if k == "" {
				continue
			}
			if err := plistBuddySetBool(path, ":"+k, true); err != nil {
				return fmt.Errorf("set %s: %w", k, err)
			}
		}
	}
	// iOS-style application-identifier is not valid on macOS (Transporter 409).
	// Profiles sometimes still list it; strip it and use the Mac key only.
	_ = exec.Command("/usr/libexec/PlistBuddy", "-c", "Delete :application-identifier", path).Run()
	// Ensure Mac application-identifier + team id (TestFlight 90886 without these).
	p, err := decodeProvisioningProfile(profilePath)
	if err == nil && p.TeamID != "" && p.BundleID != "" {
		fullID := p.TeamID + "." + p.BundleID
		_ = plistBuddySetString(path, ":com.apple.application-identifier", fullID)
		_ = plistBuddySetString(path, ":com.apple.developer.team-identifier", p.TeamID)
	}
	return nil
}

// writeMacOSAppStoreEntitlementsMinimal builds a minimal MAS entitlements plist
// when the profile Entitlements dict cannot be extracted as XML.
func writeMacOSAppStoreEntitlementsMinimal(path, teamID, bundleID string, extraKeys []string) error {
	teamID = strings.TrimSpace(teamID)
	bundleID = strings.TrimSpace(bundleID)
	fullID := ""
	if teamID != "" && bundleID != "" {
		fullID = teamID + "." + bundleID
	}
	seen := map[string]bool{"com.apple.security.app-sandbox": true}
	var boolKeys []string
	boolKeys = append(boolKeys, "com.apple.security.app-sandbox")
	for _, k := range extraKeys {
		k = strings.TrimSpace(k)
		if k == "" || seen[k] {
			continue
		}
		seen[k] = true
		boolKeys = append(boolKeys, k)
	}
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
`)
	if fullID != "" {
		// Mac only — do not write bare application-identifier (iOS; Transporter 409).
		fmt.Fprintf(&b, "\t<key>com.apple.application-identifier</key>\n\t<string>%s</string>\n", fullID)
	}
	if teamID != "" {
		fmt.Fprintf(&b, "\t<key>com.apple.developer.team-identifier</key>\n\t<string>%s</string>\n", teamID)
	}
	for _, k := range boolKeys {
		fmt.Fprintf(&b, "\t<key>%s</key>\n\t<true/>\n", k)
	}
	b.WriteString(`</dict>
</plist>
`)
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

// extractProfileEntitlementsPlist writes the profile's Entitlements dictionary
// as a standalone XML plist (for codesign --entitlements).
func extractProfileEntitlementsPlist(profilePath, dest string) error {
	cms, err := exec.Command("security", "cms", "-D", "-i", profilePath).Output()
	if err != nil {
		return fmt.Errorf("security cms -D: %w", err)
	}
	tmp, err := os.CreateTemp("", "shirei-prov-*.plist")
	if err != nil {
		return err
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.Write(cms); err != nil {
		tmp.Close()
		return err
	}
	tmp.Close()
	// -x → XML plist of the Entitlements dict only.
	out, err := exec.Command("/usr/libexec/PlistBuddy", "-x", "-c", "Print :Entitlements", tmpPath).Output()
	if err != nil {
		return fmt.Errorf("PlistBuddy Print :Entitlements: %w", err)
	}
	if len(bytesTrimSpace(out)) == 0 {
		return fmt.Errorf("profile has empty Entitlements")
	}
	return os.WriteFile(dest, out, 0o644)
}

func bytesTrimSpace(b []byte) []byte {
	return []byte(strings.TrimSpace(string(b)))
}

func plistBuddySetBool(plistPath, key string, v bool) error {
	val := "false"
	if v {
		val = "true"
	}
	// Set if present; otherwise Add.
	if err := exec.Command("/usr/libexec/PlistBuddy", "-c", "Set "+key+" "+val, plistPath).Run(); err == nil {
		return nil
	}
	return exec.Command("/usr/libexec/PlistBuddy", "-c", "Add "+key+" bool "+val, plistPath).Run()
}

func plistBuddySetString(plistPath, key, v string) error {
	v = strings.TrimSpace(v)
	if v == "" {
		return nil
	}
	// Escape is unnecessary for team/bundle ids (alphanumeric + dots).
	if err := exec.Command("/usr/libexec/PlistBuddy", "-c", "Set "+key+" "+v, plistPath).Run(); err == nil {
		return nil
	}
	return exec.Command("/usr/libexec/PlistBuddy", "-c", "Add "+key+" string "+v, plistPath).Run()
}

func normalizeMacOSArchs(archs []string) (list []string, label string, err error) {
	seen := map[string]bool{}
	for _, a := range archs {
		a = strings.TrimSpace(a)
		switch a {
		case "arm64", "amd64":
			if !seen[a] {
				list = append(list, a)
				seen[a] = true
			}
		case "x86_64":
			if !seen["amd64"] {
				list = append(list, "amd64")
				seen["amd64"] = true
			}
		case "", "universal":
			// ignore; handle empty below
		default:
			return nil, "", fmt.Errorf("unsupported arch %q (arm64 or amd64)", a)
		}
	}
	if len(list) == 0 {
		list = []string{runtime.GOARCH}
		if list[0] != "arm64" && list[0] != "amd64" {
			list = []string{"arm64"}
		}
	}
	if len(list) == 2 {
		return list, "universal", nil
	}
	return list, list[0], nil
}

func buildMacOSBinary(moduleRoot, pkgSpec, binPath string, archs []string,
	logf func(string, ...any), cancelled func() bool, setCmd func(*exec.Cmd)) error {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	if len(archs) == 1 {
		logf("— go build GOOS=darwin GOARCH=%s (%s)", archs[0], pkgSpec)
		cmd := exec.Command("go", "build", "-o", binPath, "-ldflags="+releaseLdflags, pkgSpec)
		cmd.Dir = moduleRoot
		cmd.Env = append(os.Environ(),
			"CGO_ENABLED=1",
			"GOOS=darwin",
			"GOARCH="+archs[0],
		)
		if err := runCmdLog(cmd, logf, cancelled, setCmd); err != nil {
			if cancelled != nil && cancelled() {
				return fmt.Errorf("cancelled")
			}
			return fmt.Errorf("go build: %w", err)
		}
		return nil
	}
	// Universal: build each arch then lipo.
	var slices []string
	for _, arch := range archs {
		out := binPath + "-" + arch
		logf("— go build GOOS=darwin GOARCH=%s (%s)", arch, pkgSpec)
		cmd := exec.Command("go", "build", "-o", out, "-ldflags="+releaseLdflags, pkgSpec)
		cmd.Dir = moduleRoot
		cmd.Env = append(os.Environ(),
			"CGO_ENABLED=1",
			"GOOS=darwin",
			"GOARCH="+arch,
		)
		if err := runCmdLog(cmd, logf, cancelled, setCmd); err != nil {
			if cancelled != nil && cancelled() {
				return fmt.Errorf("cancelled")
			}
			return fmt.Errorf("go build %s: %w", arch, err)
		}
		slices = append(slices, out)
	}
	logf("— lipo universal binary")
	args := append([]string{"-create", "-output", binPath}, slices...)
	if err := runCmdLog(exec.Command("lipo", args...), logf, cancelled, setCmd); err != nil {
		return fmt.Errorf("lipo: %w", err)
	}
	return nil
}

// notarizeMacOS submits a signed .app to Apple notarization, staples the ticket,
// and rewrites the distribution zip. Profile is a notarytool keychain profile.
func notarizeMacOS(o MacOSNotarizeOpts) (string, error) {
	if runtime.GOOS != "darwin" {
		return "", fmt.Errorf("notarization requires a Mac host")
	}
	profile := strings.TrimSpace(o.Profile)
	if profile == "" {
		return "", fmt.Errorf("notary keychain profile is required")
	}
	appPath := strings.TrimSpace(o.AppPath)
	if st, err := os.Stat(appPath); err != nil || !st.IsDir() {
		return "", fmt.Errorf(".app not found: %s", appPath)
	}
	zipPath := strings.TrimSpace(o.ZipPath)
	if zipPath == "" {
		zipPath = filepath.Join(filepath.Dir(appPath), strings.TrimSuffix(filepath.Base(appPath), ".app")+"-notarized.zip")
	}

	logf := o.Logf
	if logf == nil {
		logf = func(string, ...any) {}
	}
	step := func(i int) error {
		if o.Cancelled != nil && o.Cancelled() {
			return fmt.Errorf("cancelled")
		}
		if o.OnStep != nil {
			o.OnStep(i)
		}
		return nil
	}

	workDir := filepath.Dir(appPath)
	submitZip := filepath.Join(workDir, ".notary-submit.zip")
	_ = os.Remove(submitZip)

	if err := step(0); err != nil {
		return "", err
	}
	logf("— ditto zip for notarytool")
	if err := runCmdLog(exec.Command("ditto", "-c", "-k", "--keepParent", appPath, submitZip),
		logf, o.Cancelled, o.SetRunningCmd); err != nil {
		return "", fmt.Errorf("ditto zip: %w", err)
	}

	if err := step(1); err != nil {
		return "", err
	}
	logf("— notarytool submit (profile=%s, waits)…", profile)
	if err := runCmdLog(exec.Command("xcrun", "notarytool", "submit", submitZip,
		"--keychain-profile", profile,
		"--wait",
	), logf, o.Cancelled, o.SetRunningCmd); err != nil {
		if o.Cancelled != nil && o.Cancelled() {
			return "", fmt.Errorf("cancelled")
		}
		return "", fmt.Errorf("notarytool: %w", err)
	}
	_ = os.Remove(submitZip)

	if err := step(2); err != nil {
		return "", err
	}
	logf("— stapler staple")
	if err := runCmdLog(exec.Command("xcrun", "stapler", "staple", appPath),
		logf, o.Cancelled, o.SetRunningCmd); err != nil {
		return "", fmt.Errorf("stapler: %w", err)
	}

	if err := step(3); err != nil {
		return "", err
	}
	_ = os.Remove(zipPath)
	logf("— rewrite distribution zip %s", zipPath)
	if err := runCmdLog(exec.Command("ditto", "-c", "-k", "--keepParent", appPath, zipPath),
		logf, o.Cancelled, o.SetRunningCmd); err != nil {
		return "", fmt.Errorf("distribution zip: %w", err)
	}
	logf("notarized app: %s", appPath)
	logf("zip: %s", zipPath)
	return zipPath, nil
}

// macOSAppBesideZip finds Foo.app next to a recorded distribution zip path.
func macOSAppBesideZip(zipPath string) string {
	dir := filepath.Dir(strings.TrimSpace(zipPath))
	if dir == "" || dir == "." {
		return ""
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return ""
	}
	// Prefer plain Product.app over Product-mas.app.
	var mas string
	for _, e := range entries {
		if !e.IsDir() || !strings.HasSuffix(e.Name(), ".app") {
			continue
		}
		p := filepath.Join(dir, e.Name())
		if strings.HasSuffix(e.Name(), "-mas.app") {
			mas = p
			continue
		}
		return p
	}
	return mas
}

func assembleMacOSApp(appDir, product, display, bundleID, version, build, binPath, iconPath, category, pkgDir string, logf func(string, ...any)) error {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	_ = os.RemoveAll(appDir)
	macOS := filepath.Join(appDir, "Contents", "MacOS")
	resources := filepath.Join(appDir, "Contents", "Resources")
	if err := os.MkdirAll(macOS, 0o755); err != nil {
		return err
	}
	if err := os.MkdirAll(resources, 0o755); err != nil {
		return err
	}
	destBin := filepath.Join(macOS, product)
	if err := copyFile(binPath, destBin); err != nil {
		return err
	}
	if err := os.Chmod(destBin, 0o755); err != nil {
		return err
	}

	iconFile := ""
	if iconPath != "" {
		if st, err := os.Stat(iconPath); err == nil && !st.IsDir() {
			icns := filepath.Join(resources, product+".icns")
			if err := makeICNS(iconPath, icns, logf); err != nil {
				return fmt.Errorf("icns: %w", err)
			}
			iconFile = product // CFBundleIconFile without extension
		}
	}

	if err := copyPackageResources(pkgDir, resources, logf); err != nil {
		return err
	}

	if strings.TrimSpace(category) == "" {
		category = defaultMacOSCategory
	}

	plist := filepath.Join(appDir, "Contents", "Info.plist")
	body := macOSInfoPlist(product, display, bundleID, version, build, iconFile, category)
	if err := os.WriteFile(plist, []byte(body), 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(appDir, "Contents", "PkgInfo"), []byte("APPL????"), 0o644); err != nil {
		return err
	}
	logf("assembled %s (category %s)", appDir, category)
	return nil
}

func macOSInfoPlist(execName, display, bundleID, version, build, iconFile, category string) string {
	iconXML := ""
	if iconFile != "" {
		iconXML = fmt.Sprintf("\t<key>CFBundleIconFile</key>\n\t<string>%s</string>\n", iconFile)
	}
	if strings.TrimSpace(category) == "" {
		category = defaultMacOSCategory
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>CFBundleDevelopmentRegion</key>
	<string>en</string>
	<key>CFBundleExecutable</key>
	<string>%s</string>
	<key>CFBundleIdentifier</key>
	<string>%s</string>
	<key>CFBundleInfoDictionaryVersion</key>
	<string>6.0</string>
	<key>CFBundleName</key>
	<string>%s</string>
	<key>CFBundleDisplayName</key>
	<string>%s</string>
	<key>CFBundlePackageType</key>
	<string>APPL</string>
	<key>CFBundleShortVersionString</key>
	<string>%s</string>
	<key>CFBundleVersion</key>
	<string>%s</string>
%s	<key>LSApplicationCategoryType</key>
	<string>%s</string>
	<key>LSMinimumSystemVersion</key>
	<string>12.0</string>
	<key>NSHighResolutionCapable</key>
	<true/>
	<key>NSSupportsAutomaticGraphicsSwitching</key>
	<true/>
</dict>
</plist>
`, execName, bundleID, display, display, version, build, iconXML, category)
}

// makeICNS converts a PNG/JPEG (etc.) into a .icns via sips + iconutil.
func makeICNS(src, destICNS string, logf func(string, ...any)) error {
	work := destICNS + ".iconset"
	_ = os.RemoveAll(work)
	if err := os.MkdirAll(work, 0o755); err != nil {
		return err
	}
	names := map[string]int{
		"icon_16x16.png":      16,
		"icon_16x16@2x.png":   32,
		"icon_32x32.png":      32,
		"icon_32x32@2x.png":   64,
		"icon_128x128.png":    128,
		"icon_128x128@2x.png": 256,
		"icon_256x256.png":    256,
		"icon_256x256@2x.png": 512,
		"icon_512x512.png":    512,
		"icon_512x512@2x.png": 1024,
	}
	for name, px := range names {
		out := filepath.Join(work, name)
		cmd := exec.Command("sips", "-s", "format", "png", "-z", fmt.Sprint(px), fmt.Sprint(px), src, "--out", out)
		if b, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("sips %s: %w\n%s", name, err, b)
		}
	}
	_ = os.Remove(destICNS)
	cmd := exec.Command("iconutil", "-c", "icns", work, "-o", destICNS)
	if b, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("iconutil: %w\n%s", err, b)
	}
	_ = os.RemoveAll(work)
	logf("icns: %s", destICNS)
	return nil
}

func firstDeveloperIDIdentity() (string, error) {
	out, err := exec.Command("security", "find-identity", "-v", "-p", "codesigning").Output()
	if err != nil {
		return "", fmt.Errorf("list codesigning identities: %w", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, "Developer ID Application") {
			continue
		}
		m := reIdentityLine.FindStringSubmatch(line)
		if m != nil {
			return m[1], nil
		}
	}
	return "", fmt.Errorf("no Developer ID Application identity in keychain")
}

func firstMacAppStoreIdentity() (string, error) {
	out, err := exec.Command("security", "find-identity", "-v", "-p", "codesigning").Output()
	if err != nil {
		return "", fmt.Errorf("list codesigning identities: %w", err)
	}
	// Prefer explicit Mac App Distribution / 3rd Party Mac Developer Application names.
	prefer := []string{
		"3rd Party Mac Developer Application",
		"Apple Distribution",
		"Mac App Distribution",
	}
	for _, needle := range prefer {
		for _, line := range strings.Split(string(out), "\n") {
			if !strings.Contains(line, needle) {
				continue
			}
			// Skip iOS-only Apple Distribution if we can tell — still accept as last resort.
			m := reIdentityLine.FindStringSubmatch(line)
			if m != nil {
				return m[1], nil
			}
		}
	}
	return "", fmt.Errorf("no Mac App Store distribution identity in keychain (need 3rd Party Mac Developer Application or Apple Distribution)")
}

func firstMacInstallerIdentity() (string, error) {
	// Installer certs are not codesigning identities; list the full keychain set.
	for _, id := range listAllIdentities() {
		if isMacAppStoreInstallerIdentity(id) {
			return id, nil
		}
	}
	return "", fmt.Errorf("no Mac App Store installer identity in keychain (need 3rd Party Mac Developer Installer / Mac Installer Distribution)")
}
