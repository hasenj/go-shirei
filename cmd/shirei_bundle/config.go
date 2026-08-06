package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	platformIOS     = "ios"
	platformAndroid = "android"
	platformMacOS   = "macos"
	platformLinux   = "linux"
	platformWindows = "windows"
)

// Store is the persisted list of configured applications.
type Store struct {
	Apps []App `json:"apps"`
}

// App is one release-managed application (shared identity + per-platform settings).
type App struct {
	// ID is a stable key for the list (not the Apple bundle id).
	ID string `json:"id"`

	Package     string `json:"package"` // absolute or cwd-relative path to package main
	Name        string `json:"name"`
	AppIDPrefix string `json:"app_id_prefix"`
	AppID       string `json:"app_id"` // full reverse-DNS id when set
	IconPath    string `json:"icon_path"`

	// Per-platform configuration.
	IOS     *IOSConfig     `json:"ios,omitempty"`
	Android *AndroidConfig `json:"android,omitempty"`
	MacOS   *MacOSConfig   `json:"macos,omitempty"`
	Linux   *LinuxConfig   `json:"linux,omitempty"`
	Windows *WindowsConfig `json:"windows,omitempty"`
}

// PlatformIdentity is the four identity fields a platform may override.
// Empty fields inherit from the shared App values.
type PlatformIdentity struct {
	Name        string `json:"name,omitempty"`
	AppIDPrefix string `json:"app_id_prefix,omitempty"`
	AppID       string `json:"app_id,omitempty"` // full reverse-DNS when set
	IconPath    string `json:"icon_path,omitempty"`
}

// IOSConfig is iOS-specific packaging settings (not release history).
// Version/build numbers live in bundle-releases.json (ReleaseLog).
// PlatformIdentity fields override App-level identity when set; empty inherits.
type IOSConfig struct {
	PlatformIdentity
	TeamID     string `json:"team_id"`
	Identity   string `json:"identity,omitempty"` // pin; empty = auto Distribution/Development by method
	Method     string `json:"method"`             // debugging | app-store-connect | ad-hoc
	ReleaseDir string `json:"release_dir"`        // output directory for IPAs
}

// AndroidConfig is Android-specific packaging settings.
// Keystore passwords are never stored — prompted at bundle time.
// PlatformIdentity fields override App-level identity when set; empty inherits.
type AndroidConfig struct {
	PlatformIdentity
	Keystore   string `json:"keystore"`    // path to .jks / .keystore
	KeyAlias   string `json:"key_alias"`   // key alias inside the keystore
	ReleaseDir string `json:"release_dir"` // output directory for APKs
	Arch       string `json:"arch,omitempty"` // arm64 (default) | arm
}

func defaultIOSConfig() *IOSConfig {
	return &IOSConfig{
		Method:     "debugging",
		ReleaseDir: "releases",
	}
}

func defaultAndroidConfig() *AndroidConfig {
	return &AndroidConfig{
		ReleaseDir: "releases",
		Arch:       "arm64",
	}
}

// LinuxConfig is Linux desktop packaging (tarball + .desktop).
// PlatformIdentity fields override App-level identity when set; empty inherits.
type LinuxConfig struct {
	PlatformIdentity
	// Architectures (multi-select). Each selected arch produces one tarball.
	ArchARM64  bool   `json:"arch_arm64"`
	ArchAMD64  bool   `json:"arch_amd64"`
	ReleaseDir string `json:"release_dir"`
}

func defaultLinuxConfig() *LinuxConfig {
	return &LinuxConfig{
		ArchAMD64:  true,
		ArchARM64:  false,
		ReleaseDir: "releases",
	}
}

func normalizeLinuxConfig(c *LinuxConfig) {
	if c == nil {
		return
	}
	if !c.ArchARM64 && !c.ArchAMD64 {
		c.ArchAMD64 = true
	}
}

func (c *LinuxConfig) linuxArchs() []string {
	if c == nil {
		return []string{"amd64"}
	}
	var out []string
	if c.ArchARM64 {
		out = append(out, "arm64")
	}
	if c.ArchAMD64 {
		out = append(out, "amd64")
	}
	if len(out) == 0 {
		out = []string{"amd64"}
	}
	return out
}

// WindowsConfig is Windows desktop packaging (zip of .exe).
// PlatformIdentity fields override App-level identity when set; empty inherits.
type WindowsConfig struct {
	PlatformIdentity
	// Architectures (multi-select). Each selected arch produces one zip.
	ArchARM64  bool   `json:"arch_arm64"`
	ArchAMD64  bool   `json:"arch_amd64"`
	ReleaseDir string `json:"release_dir"`
}

func defaultWindowsConfig() *WindowsConfig {
	return &WindowsConfig{
		ArchAMD64:  true,
		ArchARM64:  false,
		ReleaseDir: "releases",
	}
}

func normalizeWindowsConfig(c *WindowsConfig) {
	if c == nil {
		return
	}
	if !c.ArchARM64 && !c.ArchAMD64 {
		c.ArchAMD64 = true
	}
}

func (c *WindowsConfig) windowsArchs() []string {
	if c == nil {
		return []string{"amd64"}
	}
	var out []string
	if c.ArchARM64 {
		out = append(out, "arm64")
	}
	if c.ArchAMD64 {
		out = append(out, "amd64")
	}
	if len(out) == 0 {
		out = []string{"amd64"}
	}
	return out
}

// MacOSConfig is macOS-specific packaging settings.
// PlatformIdentity fields override App-level identity when set; empty inherits.
type MacOSConfig struct {
	PlatformIdentity

	// Output modes (multi-select). At least one must be true after normalize.
	SelfDist bool `json:"self_dist"` // Developer ID .app + zip (direct distribution)
	AppStore bool `json:"app_store"` // Mac App Store .pkg via productbuild

	// Architectures (multi-select). Both → universal binary.
	ArchARM64 bool `json:"arch_arm64"`
	ArchAMD64 bool `json:"arch_amd64"`

	// Identity is Developer ID Application for self-dist (empty = auto).
	Identity string `json:"identity,omitempty"`
	// AppStoreIdentity is Mac App Distribution / 3rd Party Mac Developer Application (empty = auto).
	AppStoreIdentity string `json:"app_store_identity,omitempty"`
	// InstallerIdentity signs the .pkg (3rd Party Mac Developer Installer; empty = unsigned pkg).
	InstallerIdentity string `json:"installer_identity,omitempty"`
	// ProvisionProfile is a path to a Mac App Store .provisionprofile / .mobileprovision.
	// Empty = auto-find matching profile under Xcode's Provisioning Profiles folders.
	ProvisionProfile string `json:"provision_profile,omitempty"`

	// Category is LSApplicationCategoryType UTI (required for Mac App Store).
	// Example: public.app-category.music
	Category string `json:"category,omitempty"`

	NotaryProfile string `json:"notary_profile,omitempty"` // notarytool profile for post-build Notarize (self-dist)
	ReleaseDir    string `json:"release_dir"`

	// Arch is legacy single-arch JSON ("arm64"|"amd64"); migrated into Arch* bools on load.
	Arch string `json:"arch,omitempty"`
}

// defaultMacOSCategory is used when Category is empty (App Store validation requires it).
const defaultMacOSCategory = "public.app-category.utilities"

func defaultMacOSConfig() *MacOSConfig {
	return &MacOSConfig{
		SelfDist:   true,
		AppStore:   false,
		ArchARM64:  true,
		ArchAMD64:  false,
		Category:   defaultMacOSCategory,
		ReleaseDir: "releases",
	}
}

// macOSAppCategories is the LSApplicationCategoryType menu (label → UTI).
var macOSAppCategories = []struct{ Label, UTI string }{
	{"Business", "public.app-category.business"},
	{"Developer Tools", "public.app-category.developer-tools"},
	{"Education", "public.app-category.education"},
	{"Entertainment", "public.app-category.entertainment"},
	{"Finance", "public.app-category.finance"},
	{"Games", "public.app-category.games"},
	{"Graphics & Design", "public.app-category.graphics-design"},
	{"Healthcare & Fitness", "public.app-category.healthcare-fitness"},
	{"Lifestyle", "public.app-category.lifestyle"},
	{"Medical", "public.app-category.medical"},
	{"Music", "public.app-category.music"},
	{"News", "public.app-category.news"},
	{"Photography", "public.app-category.photography"},
	{"Productivity", "public.app-category.productivity"},
	{"Reference", "public.app-category.reference"},
	{"Social Networking", "public.app-category.social-networking"},
	{"Sports", "public.app-category.sports"},
	{"Travel", "public.app-category.travel"},
	{"Utilities", "public.app-category.utilities"},
	{"Video", "public.app-category.video"},
	{"Weather", "public.app-category.weather"},
}

// normalizeMacOSConfig fills defaults and migrates legacy Arch / missing modes.
func normalizeMacOSConfig(c *MacOSConfig) {
	if c == nil {
		return
	}
	if !c.ArchARM64 && !c.ArchAMD64 {
		switch strings.TrimSpace(c.Arch) {
		case "amd64", "x86_64":
			c.ArchAMD64 = true
		default:
			c.ArchARM64 = true
		}
	}
	// Clear legacy field once bools are set (kept in JSON only if still written).
	if !c.SelfDist && !c.AppStore {
		// Older configs had no flags → self-dist only.
		c.SelfDist = true
	}
	if strings.TrimSpace(c.Category) == "" {
		c.Category = defaultMacOSCategory
	}
}

// macOSCategoryLabel returns a short label for the configured UTI.
func macOSCategoryLabel(uti string) string {
	uti = strings.TrimSpace(uti)
	if uti == "" {
		uti = defaultMacOSCategory
	}
	for _, c := range macOSAppCategories {
		if c.UTI == uti {
			return c.Label
		}
	}
	return uti
}

// macOSArchs returns the selected GOARCH list (arm64 and/or amd64).
func (c *MacOSConfig) macOSArchs() []string {
	if c == nil {
		return []string{"arm64"}
	}
	var out []string
	if c.ArchARM64 {
		out = append(out, "arm64")
	}
	if c.ArchAMD64 {
		out = append(out, "amd64")
	}
	if len(out) == 0 {
		out = []string{"arm64"}
	}
	return out
}

// macOSArchLabel is a filename-friendly arch tag (arm64 | amd64 | universal).
func (c *MacOSConfig) macOSArchLabel() string {
	as := c.macOSArchs()
	if len(as) == 2 {
		return "universal"
	}
	return as[0]
}

// nextBuildNumber returns the build number for the next release (monotonic int).
func nextBuildNumber(last string) string {
	last = strings.TrimSpace(last)
	if last == "" {
		return "1"
	}
	n := 0
	for _, r := range last {
		if r < '0' || r > '9' {
			return "1"
		}
		n = n*10 + int(r-'0')
	}
	return fmt.Sprintf("%d", n+1)
}

// defaultReleaseVersion suggests the next marketing version (patch + 1).
func defaultReleaseVersion(last string) string {
	return bumpPatchVersion(last)
}

func newAppID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

func storePath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "shirei", "bundle.json"), nil
}

func loadStore() Store {
	path, err := storePath()
	if err != nil {
		return Store{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Store{}
	}
	var s Store
	if err := json.Unmarshal(data, &s); err != nil {
		return Store{}
	}
	for i := range s.Apps {
		if s.Apps[i].MacOS != nil {
			normalizeMacOSConfig(s.Apps[i].MacOS)
		}
		if s.Apps[i].Linux != nil {
			normalizeLinuxConfig(s.Apps[i].Linux)
		}
		if s.Apps[i].Windows != nil {
			normalizeWindowsConfig(s.Apps[i].Windows)
		}
	}
	return s
}

func saveStore(s Store) error {
	path, err := storePath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

// effectiveBundleID returns the shared (App-level) full reverse-DNS id.
// Prefer platformBundleID when bundling for a specific platform.
func (a App) effectiveBundleID() string {
	return bundleIDFrom(a.AppID, a.AppIDPrefix, a.Package)
}

// platformIdentity returns the raw platform identity overrides (may be zero).
func (a App) platformIdentity(platform string) PlatformIdentity {
	switch strings.ToLower(strings.TrimSpace(platform)) {
	case platformIOS:
		if a.IOS != nil {
			return a.IOS.PlatformIdentity
		}
	case platformAndroid:
		if a.Android != nil {
			return a.Android.PlatformIdentity
		}
	case platformMacOS:
		if a.MacOS != nil {
			return a.MacOS.PlatformIdentity
		}
	case platformLinux:
		if a.Linux != nil {
			return a.Linux.PlatformIdentity
		}
	case platformWindows:
		if a.Windows != nil {
			return a.Windows.PlatformIdentity
		}
	}
	return PlatformIdentity{}
}

// platformIdentityPtr returns a pointer to the platform's identity block, or nil.
func (a *App) platformIdentityPtr(platform string) *PlatformIdentity {
	switch strings.ToLower(strings.TrimSpace(platform)) {
	case platformIOS:
		if a.IOS != nil {
			return &a.IOS.PlatformIdentity
		}
	case platformAndroid:
		if a.Android != nil {
			return &a.Android.PlatformIdentity
		}
	case platformMacOS:
		if a.MacOS != nil {
			return &a.MacOS.PlatformIdentity
		}
	case platformLinux:
		if a.Linux != nil {
			return &a.Linux.PlatformIdentity
		}
	case platformWindows:
		if a.Windows != nil {
			return &a.Windows.PlatformIdentity
		}
	}
	return nil
}

// platformName is the effective display name for a platform.
func (a App) platformName(platform string) string {
	if n := strings.TrimSpace(a.platformIdentity(platform).Name); n != "" {
		return n
	}
	return strings.TrimSpace(a.Name)
}

// platformAppIDPrefix is the effective reverse-DNS prefix for a platform.
func (a App) platformAppIDPrefix(platform string) string {
	if p := strings.TrimSpace(a.platformIdentity(platform).AppIDPrefix); p != "" {
		return p
	}
	return strings.TrimSpace(a.AppIDPrefix)
}

// platformBundleID is the effective reverse-DNS id for a platform.
// Platform AppID wins; else shared AppID; else prefix+package (platform prefix if set).
func (a App) platformBundleID(platform string) string {
	id := a.platformIdentity(platform)
	if v := strings.TrimSpace(id.AppID); v != "" {
		return v
	}
	if v := strings.TrimSpace(a.AppID); v != "" {
		return v
	}
	return bundleIDFrom("", a.platformAppIDPrefix(platform), a.Package)
}

// platformIconPath is the effective icon path string for a platform.
func (a App) platformIconPath(platform string) string {
	if p := strings.TrimSpace(a.platformIdentity(platform).IconPath); p != "" {
		return p
	}
	return strings.TrimSpace(a.IconPath)
}

// resolvePlatformIcon returns the absolute (or joined) icon path for bundling.
func (a App) resolvePlatformIcon(wd, platform string) string {
	pkg := resolvePackagePath(wd, a.Package)
	return resolveIconPath(pkg, a.platformIconPath(platform))
}

// bundleIDFrom builds a reverse-DNS id: explicit full id, else prefix + package base.
func bundleIDFrom(appID, prefix, pkg string) string {
	if id := strings.TrimSpace(appID); id != "" {
		return id
	}
	p := strings.TrimRight(strings.TrimSpace(prefix), ".")
	base := sanitizeAppIDComponent(filepath.Base(strings.TrimSpace(pkg)))
	if p == "" || base == "" {
		return ""
	}
	return p + "." + base
}

func sanitizeAppIDComponent(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	out := b.String()
	if out == "" {
		return ""
	}
	if out[0] >= '0' && out[0] <= '9' {
		return "a" + out
	}
	return out
}

// ValidationIssue is one problem with an app or platform config.
type ValidationIssue struct {
	Field   string
	Message string
}

// validateAppShared checks identity fields required for any platform.
// Path checks use cachedPathInfo so paint/validate in IMGUI is not Stat-per-frame.
func validateAppShared(a App, wd string) []ValidationIssue {
	var issues []ValidationIssue
	pkg := resolvePackagePath(wd, a.Package)
	if strings.TrimSpace(a.Package) == "" {
		issues = append(issues, ValidationIssue{"package", "Package is required"})
	} else if exists, isDir := cachedPathInfo(pkg); !exists || !isDir {
		issues = append(issues, ValidationIssue{"package", "Package directory not found"})
	}
	if strings.TrimSpace(a.Name) == "" {
		issues = append(issues, ValidationIssue{"name", "Application name is required"})
	}
	if strings.TrimSpace(a.AppIDPrefix) == "" {
		issues = append(issues, ValidationIssue{"app_id_prefix", "App ID prefix is required"})
	}
	if strings.TrimSpace(a.AppID) == "" && a.effectiveBundleID() == "" {
		issues = append(issues, ValidationIssue{"app_id", "App ID is required"})
	} else if bid := a.effectiveBundleID(); bid != "" && !strings.Contains(bid, ".") {
		issues = append(issues, ValidationIssue{"app_id", "App ID should be reverse-DNS (e.g. com.example.app)"})
	}
	icon := resolveIconPath(pkg, a.IconPath)
	if strings.TrimSpace(a.IconPath) == "" {
		issues = append(issues, ValidationIssue{"icon", "Icon path is required"})
	} else if exists, isDir := cachedPathInfo(icon); !exists || isDir {
		issues = append(issues, ValidationIssue{"icon", "Icon file not found"})
	}
	return issues
}

// validatePlatformIdentity checks effective name, prefix, App ID, and icon
// for one platform (override when set, otherwise shared App fields).
func validatePlatformIdentity(a App, wd, platform string) []ValidationIssue {
	var issues []ValidationIssue
	if a.platformName(platform) == "" {
		issues = append(issues, ValidationIssue{"name", "Application name is required"})
	}
	if a.platformAppIDPrefix(platform) == "" {
		issues = append(issues, ValidationIssue{"app_id_prefix", "App ID prefix is required"})
	}
	bid := a.platformBundleID(platform)
	if bid == "" {
		issues = append(issues, ValidationIssue{"app_id", "App ID is required"})
	} else if !strings.Contains(bid, ".") {
		issues = append(issues, ValidationIssue{"app_id", "App ID should be reverse-DNS (e.g. com.example.app)"})
	}
	pkg := resolvePackagePath(wd, a.Package)
	iconRel := a.platformIconPath(platform)
	icon := resolveIconPath(pkg, iconRel)
	if strings.TrimSpace(iconRel) == "" {
		issues = append(issues, ValidationIssue{"icon", "Icon path is required"})
	} else if exists, isDir := cachedPathInfo(icon); !exists || isDir {
		issues = append(issues, ValidationIssue{"icon", "Icon file not found"})
	}
	return issues
}

// validateIOS checks iOS platform readiness for Create release bundle.
func validateIOS(a App) []ValidationIssue {
	var issues []ValidationIssue
	if a.IOS == nil {
		issues = append(issues, ValidationIssue{"ios", "iOS platform is not configured"})
		return issues
	}
	// wd is not always available on pure config checks; identity files use
	// package path resolution with empty wd only when Package is absolute.
	// Callers that need path checks pass through validateIOSWithWD.
	return append(issues, validateIOSFields(a)...)
}

// validateIOSWithWD is validateIOS plus effective platform App ID / icon paths.
func validateIOSWithWD(a App, wd string) []ValidationIssue {
	if a.IOS == nil {
		return []ValidationIssue{{"ios", "iOS platform is not configured"}}
	}
	return append(validatePlatformIdentity(a, wd, platformIOS), validateIOSFields(a)...)
}

func validateIOSFields(a App) []ValidationIssue {
	var issues []ValidationIssue
	c := a.IOS
	if c == nil {
		return []ValidationIssue{{"ios", "iOS platform is not configured"}}
	}
	if strings.TrimSpace(c.TeamID) == "" {
		issues = append(issues, ValidationIssue{"team", "Apple Team ID is required"})
	}
	if strings.TrimSpace(c.ReleaseDir) == "" {
		issues = append(issues, ValidationIssue{"release_dir", "Release directory is required"})
	}
	method := strings.TrimSpace(c.Method)
	if method == "" {
		method = "debugging"
	}
	switch method {
	case "debugging", "development", "app-store-connect", "ad-hoc", "enterprise":
	default:
		issues = append(issues, ValidationIssue{"method", "Unknown export method"})
	}
	return issues
}

// validateAndroid checks Android platform readiness (passwords not included).
func validateAndroid(a App) []ValidationIssue {
	if a.Android == nil {
		return []ValidationIssue{{"android", "Android platform is not configured"}}
	}
	return validateAndroidFields(a)
}

// validateAndroidWithWD includes effective platform App ID / icon.
func validateAndroidWithWD(a App, wd string) []ValidationIssue {
	if a.Android == nil {
		return []ValidationIssue{{"android", "Android platform is not configured"}}
	}
	return append(validatePlatformIdentity(a, wd, platformAndroid), validateAndroidFields(a)...)
}

func validateAndroidFields(a App) []ValidationIssue {
	var issues []ValidationIssue
	c := a.Android
	if c == nil {
		return []ValidationIssue{{"android", "Android platform is not configured"}}
	}
	if strings.TrimSpace(c.Keystore) == "" {
		issues = append(issues, ValidationIssue{"keystore", "Keystore path is required"})
	} else if exists, isDir := cachedPathInfo(c.Keystore); !exists || isDir {
		issues = append(issues, ValidationIssue{"keystore", "Keystore file not found"})
	}
	if strings.TrimSpace(c.KeyAlias) == "" {
		issues = append(issues, ValidationIssue{"key_alias", "Key alias is required"})
	}
	if strings.TrimSpace(c.ReleaseDir) == "" {
		issues = append(issues, ValidationIssue{"release_dir", "Release directory is required"})
	}
	arch := strings.TrimSpace(c.Arch)
	if arch == "" {
		arch = "arm64"
	}
	if arch != "arm64" && arch != "arm" {
		issues = append(issues, ValidationIssue{"arch", "Arch must be arm64 or arm"})
	}
	return issues
}

// appReadyForIOS is true when shared + iOS config can produce a bundle.
func appReadyForIOS(a App, wd string) bool {
	return len(validateAppShared(a, wd)) == 0 && len(validateIOSWithWD(a, wd)) == 0
}

// appReadyForAndroid is true when shared + Android config can produce a bundle.
func appReadyForAndroid(a App, wd string) bool {
	return len(validateAppShared(a, wd)) == 0 && len(validateAndroidWithWD(a, wd)) == 0
}

func validateMacOS(a App) []ValidationIssue {
	if a.MacOS == nil {
		return []ValidationIssue{{"macos", "macOS platform is not configured"}}
	}
	return validateMacOSFields(a)
}

// validateMacOSWithWD includes effective platform App ID / icon.
func validateMacOSWithWD(a App, wd string) []ValidationIssue {
	if a.MacOS == nil {
		return []ValidationIssue{{"macos", "macOS platform is not configured"}}
	}
	return append(validatePlatformIdentity(a, wd, platformMacOS), validateMacOSFields(a)...)
}

func validateMacOSFields(a App) []ValidationIssue {
	var issues []ValidationIssue
	c := a.MacOS
	if c == nil {
		return []ValidationIssue{{"macos", "macOS platform is not configured"}}
	}
	normalizeMacOSConfig(c)
	if strings.TrimSpace(c.ReleaseDir) == "" {
		issues = append(issues, ValidationIssue{"release_dir", "Release directory is required"})
	}
	if !c.ArchARM64 && !c.ArchAMD64 {
		issues = append(issues, ValidationIssue{"arch", "Select at least one architecture (arm64 and/or amd64)"})
	}
	if !c.SelfDist && !c.AppStore {
		issues = append(issues, ValidationIssue{"output", "Select at least one output: Self distribution and/or App Store"})
	}
	// Installer cert presence is checked at package time (auto-detect); no hard
	// config requirement when InstallerIdentity is empty.
	return issues
}

func appReadyForMacOS(a App, wd string) bool {
	return len(validateAppShared(a, wd)) == 0 && len(validateMacOSWithWD(a, wd)) == 0
}

func validateLinux(a App) []ValidationIssue {
	if a.Linux == nil {
		return []ValidationIssue{{"linux", "Linux platform is not configured"}}
	}
	return validateLinuxFields(a)
}

func validateLinuxWithWD(a App, wd string) []ValidationIssue {
	if a.Linux == nil {
		return []ValidationIssue{{"linux", "Linux platform is not configured"}}
	}
	return append(validatePlatformIdentity(a, wd, platformLinux), validateLinuxFields(a)...)
}

func validateLinuxFields(a App) []ValidationIssue {
	var issues []ValidationIssue
	c := a.Linux
	if c == nil {
		return []ValidationIssue{{"linux", "Linux platform is not configured"}}
	}
	normalizeLinuxConfig(c)
	if strings.TrimSpace(c.ReleaseDir) == "" {
		issues = append(issues, ValidationIssue{"release_dir", "Release directory is required"})
	}
	if !c.ArchARM64 && !c.ArchAMD64 {
		issues = append(issues, ValidationIssue{"arch", "Select at least one architecture (arm64 and/or amd64)"})
	}
	return issues
}

func validateWindows(a App) []ValidationIssue {
	if a.Windows == nil {
		return []ValidationIssue{{"windows", "Windows platform is not configured"}}
	}
	return validateWindowsFields(a)
}

func validateWindowsWithWD(a App, wd string) []ValidationIssue {
	if a.Windows == nil {
		return []ValidationIssue{{"windows", "Windows platform is not configured"}}
	}
	return append(validatePlatformIdentity(a, wd, platformWindows), validateWindowsFields(a)...)
}

func validateWindowsFields(a App) []ValidationIssue {
	var issues []ValidationIssue
	c := a.Windows
	if c == nil {
		return []ValidationIssue{{"windows", "Windows platform is not configured"}}
	}
	normalizeWindowsConfig(c)
	if strings.TrimSpace(c.ReleaseDir) == "" {
		issues = append(issues, ValidationIssue{"release_dir", "Release directory is required"})
	}
	if !c.ArchARM64 && !c.ArchAMD64 {
		issues = append(issues, ValidationIssue{"arch", "Select at least one architecture (arm64 and/or amd64)"})
	}
	return issues
}

// appConfiguredIncomplete is true if the app has missing shared fields or
// has platforms that are configured but incomplete.
func appConfiguredIncomplete(a App, wd string) bool {
	if len(validateAppShared(a, wd)) > 0 {
		return true
	}
	if a.IOS != nil && len(validateIOSWithWD(a, wd)) > 0 {
		return true
	}
	if a.Android != nil && len(validateAndroidWithWD(a, wd)) > 0 {
		return true
	}
	if a.MacOS != nil && len(validateMacOSWithWD(a, wd)) > 0 {
		return true
	}
	if a.Linux != nil && len(validateLinuxWithWD(a, wd)) > 0 {
		return true
	}
	if a.Windows != nil && len(validateWindowsWithWD(a, wd)) > 0 {
		return true
	}
	return false
}

func resolvePackagePath(wd, pkg string) string {
	pkg = strings.TrimSpace(pkg)
	if pkg == "" {
		return ""
	}
	if filepath.IsAbs(pkg) {
		return pkg
	}
	return filepath.Join(wd, pkg)
}

func resolveIconPath(pkgDir, icon string) string {
	icon = strings.TrimSpace(icon)
	if icon == "" {
		return ""
	}
	if filepath.IsAbs(icon) {
		return icon
	}
	if pkgDir != "" {
		return filepath.Join(pkgDir, icon)
	}
	return icon
}

func relativizeIconPath(pkgDir, icon string) string {
	icon = strings.TrimSpace(icon)
	if icon == "" || pkgDir == "" {
		return icon
	}
	if !filepath.IsAbs(icon) {
		return filepath.Clean(icon)
	}
	rel, err := filepath.Rel(pkgDir, icon)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return icon
	}
	return rel
}

// displayName for list rows.
func (a App) displayName() string {
	if n := strings.TrimSpace(a.Name); n != "" {
		return n
	}
	if p := strings.TrimSpace(a.Package); p != "" {
		return filepath.Base(p)
	}
	return "(unnamed)"
}
