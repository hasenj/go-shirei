package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

const (
	platformIOS        = "ios"
	platformAndroid    = "android"
	defaultAppIDPrefix = "localhost.dev"
)

// PackageSettings is per-package identity (app id, display name, icon).
type PackageSettings struct {
	AppID    string `json:"app_id"`
	AppName  string `json:"app_name"`
	IconPath string `json:"icon_path,omitempty"`
}

// Config is the persisted launcher preferences.
type Config struct {
	Platform string `json:"platform"` // "ios" | "android"

	Package     string `json:"package"`
	AppIDPrefix string `json:"app_id_prefix,omitempty"` // empty → defaultAppIDPrefix

	// iOS-only
	TeamID     string `json:"team_id,omitempty"`
	DeviceName string `json:"device_name,omitempty"`
	Runtime    string `json:"runtime,omitempty"`
	IOSTarget  string `json:"ios_target,omitempty"` // "sim" | "device"

	// Android-only
	Arch   string `json:"arch,omitempty"`   // "arm64" | "arm"
	Serial string `json:"serial,omitempty"` // adb serial; empty = only device

	Packages map[string]PackageSettings `json:"packages,omitempty"`

	// Working fields for the current package (UI binding).
	AppID    string `json:"-"`
	AppName  string `json:"-"`
	IconPath string `json:"-"`
}

func defaultConfig() Config {
	plat := platformAndroid
	if runtime.GOOS == "darwin" {
		plat = platformIOS
	}
	return Config{
		Platform:  plat,
		IOSTarget: "sim",
		Arch:      "arm64",
		Packages:  map[string]PackageSettings{},
	}
}

func configPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "shirei", "mobile-run.json"), nil
}

func packageKey(pkg string) string {
	pkg = strings.TrimSpace(pkg)
	if pkg == "" {
		return ""
	}
	if abs, err := filepath.Abs(pkg); err == nil {
		return abs
	}
	return pkg
}

// effectiveAppIDPrefix returns the reverse-DNS stem for derived app ids.
func (c Config) effectiveAppIDPrefix() string {
	p := strings.TrimSpace(c.AppIDPrefix)
	p = strings.TrimRight(p, ".")
	if p == "" {
		return defaultAppIDPrefix
	}
	return p
}

// packageIconPNG returns the absolute path of <pkgDir>/icon.png when that
// file exists; otherwise empty. Used only as a resolve fallback when the
// user left App icon blank — never written into Config.IconPath by default.
func packageIconPNG(pkgDir string) string {
	pkgDir = strings.TrimSpace(pkgDir)
	if pkgDir == "" {
		return ""
	}
	candidate := filepath.Join(pkgDir, "icon.png")
	if st, err := os.Stat(candidate); err == nil && !st.IsDir() {
		return candidate
	}
	return ""
}

// defaultPackageIconPath is the relative form used by older call sites; empty
// unless icon.png is present under the package.
func defaultPackageIconPath(pkgDir string) string {
	if packageIconPNG(pkgDir) == "" {
		return ""
	}
	return "icon.png"
}

// resolveIconPath turns a stored icon path into an absolute filesystem path.
// Relative paths are resolved against pkgDir. When icon is empty, uses
// <pkgDir>/icon.png only if that file exists (does not invent a path).
func resolveIconPath(pkgDir, icon string) string {
	icon = strings.TrimSpace(icon)
	if icon == "" {
		return packageIconPNG(pkgDir)
	}
	if filepath.IsAbs(icon) {
		return icon
	}
	if pkgDir != "" {
		return filepath.Join(pkgDir, icon)
	}
	return icon
}

// relativizeIconPath stores icon paths relative to pkgDir when possible.
// Absolute paths outside the package are kept absolute.
func relativizeIconPath(pkgDir, icon string) string {
	icon = strings.TrimSpace(icon)
	if icon == "" || pkgDir == "" {
		return icon
	}
	abs := icon
	if !filepath.IsAbs(abs) {
		// Already relative — normalize clean form (e.g. ./icon.png → icon.png).
		return filepath.Clean(icon)
	}
	rel, err := filepath.Rel(pkgDir, abs)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return abs
	}
	return rel
}

func (c *Config) applyPackageSettings() {
	key := packageKey(c.Package)
	if key == "" {
		return
	}
	if c.Packages == nil {
		c.Packages = map[string]PackageSettings{}
	}
	if s, ok := c.Packages[key]; ok {
		c.AppID = s.AppID
		if s.AppName != "" {
			c.AppName = s.AppName
		} else {
			c.AppName = filepath.Base(key)
		}
		// Keep IconPath as stored (relative when under the package). Do not
		// auto-fill "icon.png" into the field — resolveIconPath still uses
		// icon.png at build/preview time when the field is empty and the file exists.
		c.IconPath = relativizeIconPath(key, s.IconPath)
		return
	}
	c.AppID = ""
	c.IconPath = ""
	if base := filepath.Base(key); base != "" && base != "." && base != "/" {
		c.AppName = base
	} else {
		c.AppName = ""
	}
}

func (c *Config) storePackageSettings() {
	key := packageKey(c.Package)
	if key == "" {
		return
	}
	if c.Packages == nil {
		c.Packages = map[string]PackageSettings{}
	}
	c.Packages[key] = PackageSettings{
		AppID:    strings.TrimSpace(c.AppID),
		AppName:  strings.TrimSpace(c.AppName),
		IconPath: relativizeIconPath(key, strings.TrimSpace(c.IconPath)),
	}
}

func loadConfig() Config {
	cfg := defaultConfig()
	path, err := configPath()
	if err != nil {
		return cfg
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return cfg
	}
	var file Config
	if err := json.Unmarshal(data, &file); err != nil {
		return cfg
	}
	cfg.Package = file.Package
	cfg.AppIDPrefix = file.AppIDPrefix
	cfg.TeamID = file.TeamID
	cfg.DeviceName = file.DeviceName
	cfg.Runtime = file.Runtime
	cfg.Serial = file.Serial
	if file.Packages != nil {
		cfg.Packages = file.Packages
	}
	switch file.Platform {
	case platformIOS, platformAndroid:
		cfg.Platform = file.Platform
	}
	switch file.IOSTarget {
	case "sim", "device":
		cfg.IOSTarget = file.IOSTarget
	}
	if file.Arch == "arm" || file.Arch == "arm64" {
		cfg.Arch = file.Arch
	}
	cfg.applyPackageSettings()
	return cfg
}

func saveConfig(cfg Config) error {
	cfg.storePackageSettings()
	path, err := configPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	// Persist without working-only fields (they're already in Packages).
	out := Config{
		Platform:    cfg.Platform,
		Package:     cfg.Package,
		AppIDPrefix: strings.TrimSpace(cfg.AppIDPrefix),
		TeamID:      cfg.TeamID,
		DeviceName:  cfg.DeviceName,
		Runtime:     cfg.Runtime,
		IOSTarget:   cfg.IOSTarget,
		Arch:        cfg.Arch,
		Serial:      cfg.Serial,
		Packages:    cfg.Packages,
	}
	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}
