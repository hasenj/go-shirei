package main

import (
	"bufio"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	_ "image/gif"
	_ "image/jpeg"

	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// iOS pipeline step labels (progress UI + ordering).
var iosBundleSteps = []string{
	"Build c-archive",
	"Stage host",
	"Archive (xcodebuild)",
	"Export IPA",
	"Write output",
}

// IOSBundleOpts is the minimal CLI surface for one iOS release IPA.
type IOSBundleOpts struct {
	PkgDir   string
	TeamID   string
	Identity string // empty → first "Apple Distribution" identity
	BundleID string
	Name     string // display + product name base
	Version  string // CFBundleShortVersionString
	Build    string // CFBundleVersion
	Method   string // ExportOptions method
	OutDir   string
	IconPath string
	Logf     func(format string, args ...any)
	// OnStep is called with 0-based step index when that step begins.
	OnStep func(step int)
	// Cancelled, when non-nil, is polled between steps and during subprocesses.
	Cancelled func() bool
	// SetRunningCmd tracks the live subprocess for kill-on-cancel (may be nil).
	SetRunningCmd func(cmd *exec.Cmd)
}

func (o IOSBundleOpts) logf(format string, args ...any) {
	if o.Logf != nil {
		o.Logf(format, args...)
	}
}

// bundleIOS builds a Release IPA and returns its absolute path.
func bundleIOS(o IOSBundleOpts) (string, error) {
	if runtime.GOOS != "darwin" {
		return "", fmt.Errorf("iOS bundling requires macOS + Xcode (host is %s)", runtime.GOOS)
	}
	if o.TeamID == "" {
		return "", fmt.Errorf("-team is required (Apple Team ID, e.g. FLGJ22JLN7)")
	}
	if o.BundleID == "" {
		return "", fmt.Errorf("-id is required (stable reverse-DNS bundle id)")
	}
	if st, err := os.Stat(o.PkgDir); err != nil || !st.IsDir() {
		return "", fmt.Errorf("package dir not found: %s", o.PkgDir)
	}

	display := o.Name
	if display == "" {
		display = filepath.Base(o.PkgDir)
	}
	// Product / executable: no spaces (xcodebuild + codesign).
	product := sanitizeProductName(display)
	if product == "" {
		product = "App"
	}

	identity := o.Identity
	if identity == "" {
		var err error
		identity, err = firstDistributionIdentity()
		if err != nil {
			return "", err
		}
		o.logf("identity: %s (auto)", identity)
	} else {
		o.logf("identity: %s", identity)
	}

	method := o.Method
	if method == "" {
		// development/debugging IPA works with local team profiles.
		// app-store-connect needs a valid Xcode Apple ID session (Xcode-Token).
		method = "debugging"
	}
	// Xcode renamed development → debugging for export.
	if method == "development" {
		method = "debugging"
	}
	switch method {
	case "app-store-connect", "ad-hoc", "debugging", "enterprise", "development":
	default:
		return "", fmt.Errorf("unknown -method %q (app-store-connect|ad-hoc|debugging)", method)
	}

	hostTemplate, err := findIOSHostTemplate()
	if err != nil {
		return "", err
	}

	moduleRoot, pkgSpec, err := resolvePackage(o.PkgDir)
	if err != nil {
		return "", err
	}

	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	buildDir := filepath.Join(cache, "shirei", "bundle-ios", product+"-"+fmt.Sprintf("%d", time.Now().Unix()))
	if err := os.MkdirAll(buildDir, 0o755); err != nil {
		return "", err
	}
	o.logf("build dir: %s", buildDir)
	step := func(i int) error {
		if o.Cancelled != nil && o.Cancelled() {
			return fmt.Errorf("cancelled")
		}
		if o.OnStep != nil {
			o.OnStep(i)
		}
		return nil
	}

	// 1) c-archive
	if err := step(0); err != nil {
		return "", err
	}
	archive := filepath.Join(buildDir, "libshirei.a")
	if err := buildCArchive(moduleRoot, pkgSpec, o.PkgDir, archive, o.logf, o.Cancelled, o.SetRunningCmd); err != nil {
		return "", err
	}

	// 2) stage host work copy
	if err := step(1); err != nil {
		return "", err
	}
	hostWork := filepath.Join(buildDir, "host-work")
	if err := copyDir(hostTemplate, hostWork); err != nil {
		return "", fmt.Errorf("copy host: %w", err)
	}
	if err := copyFile(archive, filepath.Join(hostWork, "libshirei.a")); err != nil {
		return "", err
	}

	plist := filepath.Join(hostWork, "Info.plist")
	if err := setPlist(plist, map[string]string{
		"CFBundleIdentifier":         o.BundleID,
		"CFBundleExecutable":         product,
		"CFBundleName":               display,
		"CFBundleDisplayName":        display,
		"CFBundleShortVersionString": o.Version,
		"CFBundleVersion":            o.Build,
		"CFBundleIconName":           "AppIcon",
	}); err != nil {
		return "", err
	}

	// App Store Connect requires a full App Icon set (e.g. 120×120). Stage a
	// single 1024×1024 source into Assets.xcassets; Xcode/actool expands sizes.
	iconSrc := strings.TrimSpace(o.IconPath)
	if iconSrc == "" {
		return "", fmt.Errorf("iOS release requires an app icon (configure Icon on the app)")
	}
	if st, err := os.Stat(iconSrc); err != nil || st.IsDir() {
		return "", fmt.Errorf("iOS icon not found: %s", iconSrc)
	}
	if err := stageIOSAppIcon(hostWork, iconSrc, o.logf); err != nil {
		return "", fmt.Errorf("stage app icon: %w", err)
	}

	proj := filepath.Join(hostWork, "DeviceHost.xcodeproj")
	if st, err := os.Stat(proj); err != nil || !st.IsDir() {
		return "", fmt.Errorf("host template missing DeviceHost.xcodeproj at %s", hostTemplate)
	}

	// 3) Archive with Automatic Signing + team only. Do not pin
	// CODE_SIGN_IDENTITY — Xcode rejects Distribution on auto-signed archive.
	if err := step(2); err != nil {
		return "", err
	}
	archivePath := filepath.Join(buildDir, product+".xcarchive")
	o.logf("archiving (Release, team=%s)…", o.TeamID)
	if err := runStreaming(buildDir, o.logf, o.Cancelled, o.SetRunningCmd,
		"xcodebuild",
		"-project", proj,
		"-scheme", "shirei",
		"-configuration", "Release",
		"-destination", "generic/platform=iOS",
		"-archivePath", archivePath,
		"-allowProvisioningUpdates",
		"-allowProvisioningDeviceRegistration",
		"DEVELOPMENT_TEAM="+o.TeamID,
		"PRODUCT_BUNDLE_IDENTIFIER="+o.BundleID,
		"PRODUCT_NAME="+product,
		"MARKETING_VERSION="+o.Version,
		"CURRENT_PROJECT_VERSION="+o.Build,
		"CODE_SIGN_STYLE=Automatic",
		"archive",
	); err != nil {
		if o.Cancelled != nil && o.Cancelled() {
			return "", fmt.Errorf("cancelled")
		}
		return "", fmt.Errorf("xcodebuild archive: %w", err)
	}

	// 4) Export IPA
	if err := step(3); err != nil {
		return "", err
	}
	exportOpts := filepath.Join(buildDir, "ExportOptions.plist")
	if err := writeExportOptions(exportOpts, method, o.TeamID, identity); err != nil {
		return "", err
	}
	exportDir := filepath.Join(buildDir, "export")
	_ = os.RemoveAll(exportDir)
	o.logf("exporting IPA (method=%s)…", method)
	if err := runStreaming(buildDir, o.logf, o.Cancelled, o.SetRunningCmd,
		"xcodebuild",
		"-exportArchive",
		"-archivePath", archivePath,
		"-exportPath", exportDir,
		"-exportOptionsPlist", exportOpts,
		"-allowProvisioningUpdates",
	); err != nil {
		if o.Cancelled != nil && o.Cancelled() {
			return "", fmt.Errorf("cancelled")
		}
		return "", fmt.Errorf("xcodebuild -exportArchive: %w", err)
	}

	ipaSrc, err := findIPA(exportDir)
	if err != nil {
		return "", err
	}

	// 5) Write output
	if err := step(4); err != nil {
		return "", err
	}
	outDir, err := resolvePlatformReleaseDir(o.OutDir, product, o.Version, platformIOS)
	if err != nil {
		return "", err
	}
	ipaName := fmt.Sprintf("%s-%s-%s-ios.ipa", product, o.Version, o.Build)
	ipaDst := filepath.Join(outDir, ipaName)
	if err := copyFile(ipaSrc, ipaDst); err != nil {
		return "", err
	}
	o.logf("IPA: %s", ipaDst)
	return ipaDst, nil
}

// stageIOSAppIcon writes Assets.xcassets/AppIcon.appiconset into hostWork
// for Xcode's ASSETCATALOG_COMPILER_APPICON_NAME=AppIcon.
//
// App Store Connect rejects the large (1024×1024) icon if it has an alpha
// channel — flatten onto an opaque white background.
func stageIOSAppIcon(hostWork, src string, logf func(string, ...any)) error {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	iconset := filepath.Join(hostWork, "Assets.xcassets", "AppIcon.appiconset")
	if err := os.MkdirAll(iconset, 0o755); err != nil {
		return err
	}
	_ = os.WriteFile(filepath.Join(hostWork, "Assets.xcassets", "Contents.json"), []byte(`{
  "info" : { "author" : "shirei-bundle", "version" : 1 }
}
`), 0o644)

	iconPNG := filepath.Join(iconset, "Icon.png")
	if err := writeOpaqueAppIconPNG(src, iconPNG, 1024); err != nil {
		return err
	}
	contents := `{
  "images" : [
    {
      "filename" : "Icon.png",
      "idiom" : "universal",
      "platform" : "ios",
      "size" : "1024x1024"
    }
  ],
  "info" : { "author" : "shirei-bundle", "version" : 1 }
}
`
	if err := os.WriteFile(filepath.Join(iconset, "Contents.json"), []byte(contents), 0o644); err != nil {
		return err
	}
	logf("app icon: staged opaque 1024×1024 AppIcon from %s", src)
	return nil
}

// writeOpaqueAppIconPNG resizes src to size×size and writes a PNG with no
// transparency (white underlay). Required for App Store large-icon validation.
func writeOpaqueAppIconPNG(src, dst string, size int) error {
	f, err := os.Open(src)
	if err != nil {
		return err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return fmt.Errorf("decode icon: %w", err)
	}
	// Scale into a temporary RGBA, then composite over white.
	scaled := image.NewRGBA(image.Rect(0, 0, size, size))
	xdraw.CatmullRom.Scale(scaled, scaled.Bounds(), img, img.Bounds(), draw.Over, nil)

	opaque := image.NewRGBA(image.Rect(0, 0, size, size))
	draw.Draw(opaque, opaque.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	draw.Draw(opaque, opaque.Bounds(), scaled, image.Point{}, draw.Over)
	// Force every pixel fully opaque so png.Encode omits the alpha channel
	// when possible (Opaque() == true).
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			i := opaque.PixOffset(x, y)
			opaque.Pix[i+3] = 0xff
		}
	}

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	enc := png.Encoder{CompressionLevel: png.DefaultCompression}
	if err := enc.Encode(out, opaque); err != nil {
		return err
	}
	return nil
}

func sanitizeProductName(s string) string {
	s = strings.TrimSpace(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
			b.WriteRune(r)
		case r == ' ':
			b.WriteByte('-')
		}
	}
	return b.String()
}

var reIdentityLine = regexp.MustCompile(`^\s*\d+\)\s+[A-F0-9]+\s+"([^"]+)"`)

func firstDistributionIdentity() (string, error) {
	out, err := exec.Command("security", "find-identity", "-v", "-p", "codesigning").Output()
	if err != nil {
		return "", fmt.Errorf("list codesigning identities: %w", err)
	}
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, "Apple Distribution") {
			continue
		}
		m := reIdentityLine.FindStringSubmatch(line)
		if m != nil {
			return m[1], nil
		}
	}
	return "", fmt.Errorf("no Apple Distribution identity in keychain (Xcode → Settings → Accounts → Manage Certificates)")
}

func findIOSHostTemplate() (string, error) {
	if d := os.Getenv("SHIREI_IOS_HOST_DIR"); d != "" {
		if st, err := os.Stat(filepath.Join(d, "main.m")); err == nil && !st.IsDir() {
			return d, nil
		}
		return "", fmt.Errorf("SHIREI_IOS_HOST_DIR=%s missing main.m", d)
	}
	// Walk from this source file's module: cmd/shirei_mobilerun/embed/ioshost
	candidates := []string{}
	if wd, err := os.Getwd(); err == nil {
		candidates = append(candidates,
			filepath.Join(wd, "shirei", "cmd", "shirei_mobilerun", "embed", "ioshost"),
			filepath.Join(wd, "cmd", "shirei_mobilerun", "embed", "ioshost"),
		)
		// walk up looking for cmd/shirei_mobilerun/embed/ioshost
		d := wd
		for i := 0; i < 8; i++ {
			candidates = append(candidates, filepath.Join(d, "shirei", "cmd", "shirei_mobilerun", "embed", "ioshost"))
			candidates = append(candidates, filepath.Join(d, "cmd", "shirei_mobilerun", "embed", "ioshost"))
			parent := filepath.Dir(d)
			if parent == d {
				break
			}
			d = parent
		}
	}
	for _, c := range candidates {
		if st, err := os.Stat(filepath.Join(c, "main.m")); err == nil && !st.IsDir() {
			if st2, err := os.Stat(filepath.Join(c, "DeviceHost.xcodeproj")); err == nil && st2.IsDir() {
				return c, nil
			}
		}
	}
	return "", fmt.Errorf("ioshost template not found (set SHIREI_IOS_HOST_DIR or run from the monorepo)")
}

func resolvePackage(pkgDir string) (moduleRoot, pkgSpec string, err error) {
	moduleRoot = findUp(pkgDir, "go.mod")
	if moduleRoot == "" {
		return "", "", fmt.Errorf("no go.mod above %s", pkgDir)
	}
	if pkgDir == moduleRoot {
		return moduleRoot, ".", nil
	}
	rel, err := filepath.Rel(moduleRoot, pkgDir)
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", "", fmt.Errorf("package %s is not under module %s", pkgDir, moduleRoot)
	}
	return moduleRoot, "./" + filepath.ToSlash(rel), nil
}

func findUp(start, name string) string {
	d := start
	for {
		if st, err := os.Stat(filepath.Join(d, name)); err == nil && !st.IsDir() {
			return d
		}
		parent := filepath.Dir(d)
		if parent == d {
			return ""
		}
		d = parent
	}
}

func buildCArchive(moduleRoot, pkgSpec, pkgDir, archiveOut string, logf func(string, ...any), cancelled func() bool, setCmd func(*exec.Cmd)) error {
	exportFile := filepath.Join(pkgDir, "zz_shirei_ios_export.go")
	content := `//go:build ios

// Code generated by shirei/bundle — do not commit.
package main

import "C"

//export shirei_ios_run
func shirei_ios_run() {
	main()
}
`
	if err := os.WriteFile(exportFile, []byte(content), 0o644); err != nil {
		return err
	}
	defer os.Remove(exportFile)

	sdkPath, err := exec.Command("xcrun", "--sdk", "iphoneos", "--show-sdk-path").Output()
	if err != nil {
		return fmt.Errorf("iphoneos SDK: %w", err)
	}
	sdk := strings.TrimSpace(string(sdkPath))
	clang, err := exec.Command("xcrun", "--sdk", "iphoneos", "-f", "clang").Output()
	if err != nil {
		return fmt.Errorf("clang for iphoneos: %w", err)
	}
	cc := strings.TrimSpace(string(clang))
	minOS := "16.0"
	versionMin := "-miphoneos-version-min=" + minOS

	logf("building c-archive GOOS=ios GOARCH=arm64 (%s)", pkgSpec)
	cmd := exec.Command("go", "build", "-buildmode=c-archive", "-o", archiveOut,
		"-ldflags="+releaseLdflags, pkgSpec)
	cmd.Dir = moduleRoot
	cmd.Env = append(os.Environ(),
		"CGO_ENABLED=1",
		"GOOS=ios",
		"GOARCH=arm64",
		"CC="+cc,
		"CGO_CFLAGS=-isysroot "+sdk+" "+versionMin+" -arch arm64",
		"CGO_LDFLAGS=-isysroot "+sdk+" "+versionMin+" -arch arm64",
	)
	if err := runCmdLog(cmd, logf, cancelled, setCmd); err != nil {
		if cancelled != nil && cancelled() {
			return fmt.Errorf("cancelled")
		}
		return fmt.Errorf("go build c-archive: %w", err)
	}
	if st, err := os.Stat(archiveOut); err != nil || st.Size() == 0 {
		return fmt.Errorf("c-archive not produced: %s", archiveOut)
	}
	return nil
}

func setPlist(path string, keys map[string]string) error {
	for k, v := range keys {
		// Set, or Add if missing
		set := exec.Command("/usr/libexec/PlistBuddy", "-c", fmt.Sprintf("Set :%s %s", k, v), path)
		if err := set.Run(); err != nil {
			add := exec.Command("/usr/libexec/PlistBuddy", "-c", fmt.Sprintf("Add :%s string %s", k, v), path)
			if err := add.Run(); err != nil {
				return fmt.Errorf("PlistBuddy %s: %w", k, err)
			}
		}
	}
	return nil
}

func writeExportOptions(path, method, teamID, identity string) error {
	// Certificate type for export. Debugging uses Development; store/ad-hoc use Distribution.
	cert := "Apple Distribution"
	switch method {
	case "debugging", "development":
		cert = "Apple Development"
	}
	_ = identity // reserved for future pin / manual signing
	// destination=export keeps the IPA local (no App Store Connect upload).
	body := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>method</key>
	<string>%s</string>
	<key>teamID</key>
	<string>%s</string>
	<key>signingStyle</key>
	<string>automatic</string>
	<key>signingCertificate</key>
	<string>%s</string>
	<key>destination</key>
	<string>export</string>
	<key>stripSwiftSymbols</key>
	<true/>
	<key>compileBitcode</key>
	<false/>
	<key>uploadSymbols</key>
	<false/>
</dict>
</plist>
`, method, teamID, cert)
	return os.WriteFile(path, []byte(body), 0o644)
}

func findIPA(dir string) (string, error) {
	var found string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(strings.ToLower(info.Name()), ".ipa") {
			found = path
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil && err != filepath.SkipAll {
		return "", err
	}
	if found == "" {
		return "", fmt.Errorf("no .ipa under %s", dir)
	}
	return found, nil
}

func runStreaming(dir string, logf func(string, ...any), cancelled func() bool, setCmd func(*exec.Cmd), name string, args ...string) error {
	logf("+ %s %s", name, strings.Join(args, " "))
	cmd := exec.Command(name, args...)
	cmd.Dir = dir
	return runCmdLog(cmd, logf, cancelled, setCmd)
}

func runCmdLog(cmd *exec.Cmd, logf func(string, ...any), cancelled func() bool, setCmd func(*exec.Cmd)) error {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return err
	}
	if setCmd != nil {
		setCmd(cmd)
		defer setCmd(nil)
	}
	var wg sync.WaitGroup
	pump := func(r io.Reader) {
		defer wg.Done()
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
		for sc.Scan() {
			logf("%s", sc.Text())
		}
	}
	wg.Add(2)
	go pump(stdout)
	go pump(stderr)
	wg.Wait()
	err = cmd.Wait()
	if cancelled != nil && cancelled() {
		return fmt.Errorf("cancelled")
	}
	return err
}

func copyFile(src, dst string) error {
	data, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, data, 0o644)
}

func copyDir(src, dst string) error {
	return filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		// Skip fat archives if present in a dirty tree
		base := filepath.Base(path)
		if base == "libshirei.a" || strings.HasSuffix(base, ".a") {
			if !info.IsDir() {
				return nil
			}
		}
		out := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		return copyFile(path, out)
	})
}
