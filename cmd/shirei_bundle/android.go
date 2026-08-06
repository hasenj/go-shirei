package main

import (
	"archive/zip"
	_ "embed"
	"fmt"
	_ "golang.org/x/image/webp"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/image/draw"
)

//go:embed embed/ShireiActivity.java
var shireiActivityJava []byte

const (
	androidMinSdk    = 21
	androidTargetSdk = 34 // modern bar for release APKs
	androidLibName   = "shireiapp"
	launcherIconSize = 192
)

// Android bundle pipeline steps (progress UI).
var androidBundleSteps = []string{
	"Build native library",
	"Compile Java host",
	"Package resources",
	"Align & sign APK",
	"Write output",
}

// AndroidBundleOpts is one release APK build.
type AndroidBundleOpts struct {
	PkgDir       string
	AppID        string // full application id
	Name         string // label / product name
	Version      string // versionName
	Build        string // versionCode (integer string)
	IconPath     string
	Arch         string // arm64 | arm | both (v1: arm64 default)
	Keystore     string
	KeyAlias     string
	StorePass    string
	KeyPass      string // empty → same as StorePass
	OutDir       string
	Logf         func(format string, args ...any)
	OnStep       func(step int)
	Cancelled    func() bool
	SetRunningCmd func(cmd *exec.Cmd)
}

func (o AndroidBundleOpts) logf(format string, args ...any) {
	if o.Logf != nil {
		o.Logf(format, args...)
	}
}

func bundleAndroid(o AndroidBundleOpts) (string, error) {
	if strings.TrimSpace(o.AppID) == "" {
		return "", fmt.Errorf("application id is required")
	}
	if strings.TrimSpace(o.Keystore) == "" {
		return "", fmt.Errorf("keystore path is required")
	}
	if strings.TrimSpace(o.KeyAlias) == "" {
		return "", fmt.Errorf("key alias is required")
	}
	if strings.TrimSpace(o.StorePass) == "" {
		return "", fmt.Errorf("keystore password is required")
	}
	keyPass := o.KeyPass
	if keyPass == "" {
		keyPass = o.StorePass
	}
	if st, err := os.Stat(o.PkgDir); err != nil || !st.IsDir() {
		return "", fmt.Errorf("package dir not found: %s", o.PkgDir)
	}
	if st, err := os.Stat(o.Keystore); err != nil || st.IsDir() {
		return "", fmt.Errorf("keystore not found: %s", o.Keystore)
	}
	versionCode, err := strconv.Atoi(strings.TrimSpace(o.Build))
	if err != nil || versionCode < 1 {
		return "", fmt.Errorf("build number must be a positive integer (got %q)", o.Build)
	}
	versionName := strings.TrimSpace(o.Version)
	if versionName == "" {
		return "", fmt.Errorf("version is required")
	}

	display := o.Name
	if display == "" {
		display = filepath.Base(o.PkgDir)
	}
	product := sanitizeProductName(display)
	if product == "" {
		product = "App"
	}

	arch := o.Arch
	if arch == "" {
		arch = "arm64"
	}

	sdk, err := findAndroidSDK()
	if err != nil {
		return "", err
	}
	ndk, err := newestSubdir(filepath.Join(sdk, "ndk"))
	if err != nil {
		return "", fmt.Errorf("NDK not found under %s/ndk: %w", sdk, err)
	}
	buildTools, err := newestSubdir(filepath.Join(sdk, "build-tools"))
	if err != nil {
		return "", fmt.Errorf("build-tools not found: %w", err)
	}
	platform, err := newestSubdir(filepath.Join(sdk, "platforms"))
	if err != nil {
		return "", fmt.Errorf("platforms not found: %w", err)
	}
	androidJar := filepath.Join(platform, "android.jar")

	moduleRoot := findUp(o.PkgDir, "go.mod")
	if moduleRoot == "" {
		return "", fmt.Errorf("no go.mod above %s", o.PkgDir)
	}

	cache, err := os.UserCacheDir()
	if err != nil {
		return "", err
	}
	buildDir := filepath.Join(cache, "shirei", "bundle-android",
		product+"-"+fmt.Sprintf("%d", time.Now().Unix()))
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

	// 1) native .so
	if err := step(0); err != nil {
		return "", err
	}
	abi, soPath, err := buildAndroidSO(o, moduleRoot, ndk, buildDir, arch)
	if err != nil {
		return "", err
	}

	// 2) Java host → dex
	if err := step(1); err != nil {
		return "", err
	}
	dexFile, err := buildAndroidDex(buildTools, androidJar, buildDir, o.logf, o.Cancelled, o.SetRunningCmd)
	if err != nil {
		return "", err
	}

	// 3) icon + manifest + aapt2
	if err := step(2); err != nil {
		return "", err
	}
	var compiledIcon string
	if o.IconPath != "" {
		if st, err := os.Stat(o.IconPath); err == nil && !st.IsDir() {
			o.logf("— launcher icon %s", o.IconPath)
			compiledIcon, err = prepareAndroidLauncherIcon(buildTools, buildDir, o.IconPath, o.logf, o.Cancelled, o.SetRunningCmd)
			if err != nil {
				return "", fmt.Errorf("launcher icon: %w", err)
			}
		}
	}
	manifest := filepath.Join(buildDir, "AndroidManifest.xml")
	if err := os.WriteFile(manifest, []byte(androidReleaseManifest(
		o.AppID, display, compiledIcon != "",
	)), 0o644); err != nil {
		return "", err
	}
	baseAPK := filepath.Join(buildDir, "base.apk")
	o.logf("— aapt2 link (targetSdk=%d versionName=%s versionCode=%d)", androidTargetSdk, versionName, versionCode)
	linkArgs := []string{
		"link", "-o", baseAPK, "--manifest", manifest, "-I", androidJar,
		"--min-sdk-version", fmt.Sprint(androidMinSdk),
		"--target-sdk-version", fmt.Sprint(androidTargetSdk),
		"--version-code", fmt.Sprint(versionCode),
		"--version-name", versionName,
	}
	if compiledIcon != "" {
		linkArgs = append(linkArgs, "-R", compiledIcon)
	}
	if err := runAndroidTool(o, buildToolPath(buildTools, "aapt2"), linkArgs...); err != nil {
		return "", fmt.Errorf("aapt2 link: %w", err)
	}

	unsigned := filepath.Join(buildDir, "unsigned.apk")
	if err := addFilesToAPK(baseAPK, unsigned, map[string]string{
		"lib/" + abi + "/lib" + androidLibName + ".so": soPath,
		"classes.dex": dexFile,
	}); err != nil {
		return "", fmt.Errorf("inject lib/dex: %w", err)
	}

	// 4) zipalign + sign
	if err := step(3); err != nil {
		return "", err
	}
	aligned := filepath.Join(buildDir, "aligned.apk")
	o.logf("— zipalign")
	if err := runAndroidTool(o, buildToolPath(buildTools, "zipalign"),
		"-f", "-p", "4", unsigned, aligned); err != nil {
		return "", fmt.Errorf("zipalign: %w", err)
	}
	signed := filepath.Join(buildDir, product+"-signed.apk")
	o.logf("— apksigner sign (alias=%s)", o.KeyAlias)
	if err := runAndroidTool(o, buildToolPath(buildTools, "apksigner"), "sign",
		"--ks", o.Keystore,
		"--ks-pass", "pass:"+o.StorePass,
		"--ks-key-alias", o.KeyAlias,
		"--key-pass", "pass:"+keyPass,
		"--out", signed, aligned); err != nil {
		if o.Cancelled != nil && o.Cancelled() {
			return "", fmt.Errorf("cancelled")
		}
		return "", fmt.Errorf("apksigner: %w", err)
	}

	// 5) write output
	if err := step(4); err != nil {
		return "", err
	}
	outDir, err := resolvePlatformReleaseDir(o.OutDir, product, versionName, platformAndroid)
	if err != nil {
		return "", err
	}
	apkName := fmt.Sprintf("%s-%s-%s-android.apk", product, versionName, o.Build)
	apkDst := filepath.Join(outDir, apkName)
	if err := copyFile(signed, apkDst); err != nil {
		return "", err
	}
	o.logf("APK: %s", apkDst)
	return apkDst, nil
}

func buildAndroidSO(o AndroidBundleOpts, moduleRoot, ndk, buildDir, arch string) (abi, soPath string, err error) {
	var goarch, cc string
	ccDir := filepath.Join(ndk, "toolchains/llvm/prebuilt", ndkHostTag(), "bin")
	switch arch {
	case "", "arm64":
		goarch, abi = "arm64", "arm64-v8a"
		cc = filepath.Join(ccDir, fmt.Sprintf("aarch64-linux-android%d-clang", androidMinSdk))
	case "arm":
		goarch, abi = "arm", "armeabi-v7a"
		cc = filepath.Join(ccDir, fmt.Sprintf("armv7a-linux-androideabi%d-clang", androidMinSdk))
	default:
		return "", "", fmt.Errorf("unsupported arch %q (arm64 or arm)", arch)
	}
	if runtime.GOOS == "windows" {
		cc += ".cmd"
	}
	if err := checkNDKHostToolchain(ndk, cc); err != nil {
		return "", "", err
	}

	exportFile := filepath.Join(o.PkgDir, "zz_shirei_android_export.go")
	exportSrc := `//go:build android

// Code generated by shirei/bundle — do not commit.
package main

import "C"

//export shirei_android_main
func shirei_android_main() {
	main()
}
`
	if err := os.WriteFile(exportFile, []byte(exportSrc), 0o644); err != nil {
		return "", "", err
	}
	defer os.Remove(exportFile)

	rel, err := filepath.Rel(moduleRoot, o.PkgDir)
	if err != nil {
		return "", "", err
	}
	pkgSpec := "./" + filepath.ToSlash(rel)
	if o.PkgDir == moduleRoot {
		pkgSpec = "."
	}
	soPath = filepath.Join(buildDir, "lib", abi, "lib"+androidLibName+".so")
	if err := os.MkdirAll(filepath.Dir(soPath), 0o755); err != nil {
		return "", "", err
	}
	o.logf("— go build c-shared GOOS=android GOARCH=%s (%s)", goarch, pkgSpec)
	cmd := exec.Command("go", "build", "-buildmode=c-shared", "-o", soPath,
		"-ldflags="+releaseLdflags, pkgSpec)
	cmd.Dir = moduleRoot
	cmd.Env = append(os.Environ(),
		"CGO_ENABLED=1", "GOOS=android", "GOARCH="+goarch, "CC="+cc)
	if goarch == "arm" {
		cmd.Env = append(cmd.Env, "GOARM=7")
	}
	if err := runCmdLog(cmd, o.logf, o.Cancelled, o.SetRunningCmd); err != nil {
		if o.Cancelled != nil && o.Cancelled() {
			return "", "", fmt.Errorf("cancelled")
		}
		return "", "", fmt.Errorf("go build: %w", err)
	}
	_ = os.Remove(strings.TrimSuffix(soPath, ".so") + ".h")
	return abi, soPath, nil
}

func buildAndroidDex(buildTools, androidJar, buildDir string, logf func(string, ...any), cancelled func() bool, setCmd func(*exec.Cmd)) (string, error) {
	javaSrc := filepath.Join(buildDir, "java", "dev", "shirei", "host", "ShireiActivity.java")
	if err := os.MkdirAll(filepath.Dir(javaSrc), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(javaSrc, shireiActivityJava, 0o644); err != nil {
		return "", err
	}
	classesDir := filepath.Join(buildDir, "classes")
	if err := os.MkdirAll(classesDir, 0o755); err != nil {
		return "", err
	}
	logf("— javac + d8 (ShireiActivity)")
	if err := runCmdLog(exec.Command("javac",
		"-classpath", androidJar, "-source", "8", "-target", "8",
		"-Xlint:-options", "-d", classesDir, javaSrc), logf, cancelled, setCmd); err != nil {
		if cancelled != nil && cancelled() {
			return "", fmt.Errorf("cancelled")
		}
		return "", fmt.Errorf("javac (JDK installed?): %w", err)
	}
	classFiles, err := filepath.Glob(filepath.Join(classesDir, "dev", "shirei", "host", "*.class"))
	if err != nil || len(classFiles) == 0 {
		return "", fmt.Errorf("no class files produced by javac")
	}
	dexDir := filepath.Join(buildDir, "dex")
	if err := os.MkdirAll(dexDir, 0o755); err != nil {
		return "", err
	}
	d8Args := append([]string{"--lib", androidJar, "--output", dexDir}, classFiles...)
	if err := runAndroidToolRaw(logf, cancelled, setCmd, buildToolPath(buildTools, "d8"), d8Args...); err != nil {
		if cancelled != nil && cancelled() {
			return "", fmt.Errorf("cancelled")
		}
		return "", fmt.Errorf("d8: %w", err)
	}
	return filepath.Join(dexDir, "classes.dex"), nil
}

func androidReleaseManifest(appID, label string, withIcon bool) string {
	iconAttr := ""
	if withIcon {
		iconAttr = ` android:icon="@mipmap/ic_launcher"`
	}
	return fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<manifest xmlns:android="http://schemas.android.com/apk/res/android"
    package=%q>
  <uses-permission android:name="android.permission.INTERNET"/>
  <queries>
    <intent>
      <action android:name="android.intent.action.VIEW"/>
      <category android:name="android.intent.category.BROWSABLE"/>
      <data android:scheme="https"/>
    </intent>
    <intent>
      <action android:name="android.intent.action.VIEW"/>
      <category android:name="android.intent.category.BROWSABLE"/>
      <data android:scheme="http"/>
    </intent>
  </queries>
  <application android:label=%q%s android:debuggable="false"
      android:theme="@android:style/Theme.Material.NoActionBar">
    <activity android:name="dev.shirei.host.ShireiActivity" android:exported="true"
        android:windowSoftInputMode="adjustResize"
        android:configChanges="orientation|screenSize|screenLayout|smallestScreenSize|keyboard|keyboardHidden|uiMode|density|locale|fontScale">
      <meta-data android:name="android.app.lib_name" android:value=%q/>
      <intent-filter>
        <action android:name="android.intent.action.MAIN"/>
        <category android:name="android.intent.category.LAUNCHER"/>
      </intent-filter>
    </activity>
  </application>
</manifest>
`, appID, label, iconAttr, androidLibName)
}

func prepareAndroidLauncherIcon(buildTools, buildDir, src string, logf func(string, ...any), cancelled func() bool, setCmd func(*exec.Cmd)) (string, error) {
	f, err := os.Open(src)
	if err != nil {
		return "", err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return "", fmt.Errorf("decode: %w", err)
	}
	dst := image.NewRGBA(image.Rect(0, 0, launcherIconSize, launcherIconSize))
	draw.CatmullRom.Scale(dst, dst.Bounds(), img, img.Bounds(), draw.Over, nil)

	resDir := filepath.Join(buildDir, "res", "mipmap-xxxhdpi")
	if err := os.MkdirAll(resDir, 0o755); err != nil {
		return "", err
	}
	pngPath := filepath.Join(resDir, "ic_launcher.png")
	out, err := os.Create(pngPath)
	if err != nil {
		return "", err
	}
	if err := png.Encode(out, dst); err != nil {
		out.Close()
		return "", err
	}
	if err := out.Close(); err != nil {
		return "", err
	}
	compiledDir := filepath.Join(buildDir, "compiled")
	if err := os.MkdirAll(compiledDir, 0o755); err != nil {
		return "", err
	}
	if err := runAndroidToolRaw(logf, cancelled, setCmd, buildToolPath(buildTools, "aapt2"),
		"compile", "-o", compiledDir+string(filepath.Separator), pngPath); err != nil {
		return "", fmt.Errorf("aapt2 compile: %w", err)
	}
	flats, err := filepath.Glob(filepath.Join(compiledDir, "*.flat"))
	if err != nil || len(flats) == 0 {
		return "", fmt.Errorf("aapt2 compile produced no .flat in %s", compiledDir)
	}
	return flats[0], nil
}

func addFilesToAPK(srcAPK, dstAPK string, entries map[string]string) error {
	zr, err := zip.OpenReader(srcAPK)
	if err != nil {
		return err
	}
	defer zr.Close()
	out, err := os.Create(dstAPK)
	if err != nil {
		return err
	}
	defer out.Close()
	zw := zip.NewWriter(out)
	for _, f := range zr.File {
		raw, err := f.OpenRaw()
		if err != nil {
			return err
		}
		hdr := f.FileHeader
		w, err := zw.CreateRaw(&hdr)
		if err != nil {
			return err
		}
		if _, err := io.Copy(w, raw); err != nil {
			return err
		}
	}
	for entryName, filePath := range entries {
		src, err := os.Open(filePath)
		if err != nil {
			return err
		}
		w, err := zw.Create(entryName)
		if err != nil {
			src.Close()
			return err
		}
		_, err = io.Copy(w, src)
		src.Close()
		if err != nil {
			return err
		}
	}
	return zw.Close()
}

func ndkHostTag() string {
	switch runtime.GOOS {
	case "darwin":
		return "darwin-x86_64"
	case "windows":
		return "windows-x86_64"
	default:
		return "linux-x86_64"
	}
}

func checkNDKHostToolchain(ndk, cc string) error {
	if _, err := os.Stat(cc); err != nil {
		return fmt.Errorf("NDK clang not found: %s", cc)
	}
	if runtime.GOOS == "linux" && runtime.GOARCH == "arm64" {
		return fmt.Errorf("Android NDK host tools are x86_64-only; this Linux is arm64")
	}
	return nil
}

func buildToolPath(buildTools, name string) string {
	p := filepath.Join(buildTools, name)
	if runtime.GOOS != "windows" {
		return p
	}
	for _, ext := range []string{".exe", ".bat", ".cmd"} {
		if _, err := os.Stat(p + ext); err == nil {
			return p + ext
		}
	}
	return p
}

func findAndroidSDK() (string, error) {
	candidates := []string{
		os.Getenv("ANDROID_HOME"),
		os.Getenv("ANDROID_SDK_ROOT"),
	}
	home, _ := os.UserHomeDir()
	switch runtime.GOOS {
	case "darwin":
		candidates = append(candidates,
			"/opt/homebrew/share/android-commandlinetools",
			filepath.Join(home, "Library/Android/sdk"))
	case "windows":
		if lad := os.Getenv("LOCALAPPDATA"); lad != "" {
			candidates = append(candidates, filepath.Join(lad, "Android", "Sdk"))
		}
	default:
		candidates = append(candidates,
			filepath.Join(home, "Android/Sdk"),
			"/usr/lib/android-sdk")
	}
	for _, c := range candidates {
		if c == "" {
			continue
		}
		if _, err := os.Stat(c); err == nil {
			return c, nil
		}
	}
	return "", fmt.Errorf("Android SDK not found (set ANDROID_HOME)")
}

func newestSubdir(parent string) (string, error) {
	ents, err := os.ReadDir(parent)
	if err != nil {
		return "", err
	}
	var names []string
	for _, e := range ents {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	if len(names) == 0 {
		return "", fmt.Errorf("no entries in %s", parent)
	}
	sort.Strings(names)
	return filepath.Join(parent, names[len(names)-1]), nil
}

func runAndroidTool(o AndroidBundleOpts, name string, args ...string) error {
	return runAndroidToolRaw(o.logf, o.Cancelled, o.SetRunningCmd, name, args...)
}

func runAndroidToolRaw(logf func(string, ...any), cancelled func() bool, setCmd func(*exec.Cmd), name string, args ...string) error {
	cmd := toolCommand(name, args...)
	return runCmdLog(cmd, logf, cancelled, setCmd)
}

func toolCommand(name string, args ...string) *exec.Cmd {
	if runtime.GOOS == "windows" {
		switch strings.ToLower(filepath.Ext(name)) {
		case ".bat", ".cmd":
			full := append([]string{"/c", name}, args...)
			return exec.Command("cmd", full...)
		}
	}
	return exec.Command(name, args...)
}

// androidPackageName mirrors mobilerun rules for valid application ids.
var nonAlnumAndroid = regexp.MustCompile(`[^a-z0-9]+`)

func sanitizeAndroidSegment(s string) string {
	s = nonAlnumAndroid.ReplaceAllString(strings.ToLower(s), "")
	if s == "" || s[0] >= '0' && s[0] <= '9' {
		s = "a" + s
	}
	return s
}

func androidPackageName(id, prefix, folderBase string) string {
	prefix = strings.TrimRight(strings.TrimSpace(prefix), ".")
	if prefix == "" {
		prefix = "dev.shirei"
	}
	var pSegs []string
	for _, p := range strings.Split(prefix, ".") {
		if s := sanitizeAndroidSegment(p); s != "" {
			pSegs = append(pSegs, s)
		}
	}
	prefix = strings.Join(pSegs, ".")
	if prefix == "" {
		prefix = "dev.shirei"
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return prefix + "." + sanitizeAndroidSegment(folderBase)
	}
	var segs []string
	for _, p := range strings.Split(id, ".") {
		if s := sanitizeAndroidSegment(p); s != "" {
			segs = append(segs, s)
		}
	}
	if len(segs) == 0 {
		return prefix + "." + sanitizeAndroidSegment(folderBase)
	}
	if len(segs) == 1 {
		return prefix + "." + segs[0]
	}
	return strings.Join(segs, ".")
}
