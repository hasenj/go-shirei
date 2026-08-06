package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// releaseLogMu guards the in-memory ReleaseLog and its JSON file when multiple
// platform bundle jobs finish around the same time.
var releaseLogMu sync.Mutex

// ReleaseLog is the persisted history of successful bundles (separate from app config).
// File: <configDir>/shirei/bundle-releases.json
type ReleaseLog struct {
	// Apps is keyed by App.ID from the config store.
	Apps map[string]*AppReleaseRecord `json:"apps"`
}

// AppReleaseRecord tracks release history for one configured application.
type AppReleaseRecord struct {
	// LastVersion is the marketing version of the most recent successful release.
	LastVersion string `json:"last_version,omitempty"`
	// LastBuild is the build number of the most recent successful release.
	LastBuild string `json:"last_build,omitempty"`
	// OpenVersion is the marketing version currently open for bundling
	// (may have no history yet). Empty when none / all frozen.
	OpenVersion string `json:"open_version,omitempty"`
	// History is newest-first optional detail (v1: last few successes).
	History []ReleaseEntry `json:"history,omitempty"`
	// Versions holds per-marketing-version status (e.g. released/frozen).
	Versions map[string]*VersionMeta `json:"versions,omitempty"`
}

// VersionMeta is status for one marketing version string.
type VersionMeta struct {
	// Released freezes the version: no delete platform builds, no new builds.
	Released   bool   `json:"released,omitempty"`
	ReleasedAt string `json:"released_at,omitempty"` // RFC3339
}

// ReleaseEntry is one successful platform bundle.
type ReleaseEntry struct {
	Platform  string `json:"platform"` // "ios" | "android" | "macos"
	Version   string `json:"version"`
	Build     string `json:"build"`
	Path      string `json:"path,omitempty"` // primary artifact (zip, ipa, apk, or pkg)
	// Extra holds additional artifact paths from the same build (e.g. .pkg + .zip).
	Extra     []string `json:"extra,omitempty"`
	At        string   `json:"at"` // RFC3339
	Notarized bool     `json:"notarized,omitempty"` // macOS self-dist post-build stamp
}

// VersionRelease is one marketing version with its platform bundles.
// Platforms maps platform id → newest entry for that version+platform.
type VersionRelease struct {
	Version   string
	At        string // newest activity (RFC3339)
	Platforms map[string]ReleaseEntry
}

func releasesPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "shirei", "bundle-releases.json"), nil
}

func loadReleaseLog() ReleaseLog {
	path, err := releasesPath()
	if err != nil {
		return ReleaseLog{Apps: map[string]*AppReleaseRecord{}}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ReleaseLog{Apps: map[string]*AppReleaseRecord{}}
	}
	var log ReleaseLog
	if err := json.Unmarshal(data, &log); err != nil {
		return ReleaseLog{Apps: map[string]*AppReleaseRecord{}}
	}
	if log.Apps == nil {
		log.Apps = map[string]*AppReleaseRecord{}
	}
	return log
}

func saveReleaseLog(log ReleaseLog) error {
	path, err := releasesPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	if log.Apps == nil {
		log.Apps = map[string]*AppReleaseRecord{}
	}
	data, err := json.MarshalIndent(log, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func (log *ReleaseLog) recordFor(appID string) *AppReleaseRecord {
	if log.Apps == nil {
		log.Apps = map[string]*AppReleaseRecord{}
	}
	r := log.Apps[appID]
	if r == nil {
		r = &AppReleaseRecord{}
		log.Apps[appID] = r
	}
	return r
}

// recordSuccess appends a successful release and updates last version/build.
// extra lists optional additional artifact paths (e.g. .pkg beside a .zip).
func (log *ReleaseLog) recordSuccess(appID, platform, version, build, path string, extra ...string) {
	r := log.recordFor(appID)
	r.LastVersion = strings.TrimSpace(version)
	r.LastBuild = strings.TrimSpace(build)
	var extraCopy []string
	for _, e := range extra {
		e = strings.TrimSpace(e)
		if e != "" && e != path {
			extraCopy = append(extraCopy, e)
		}
	}
	entry := ReleaseEntry{
		Platform: platform,
		Version:  r.LastVersion,
		Build:    r.LastBuild,
		Path:     path,
		Extra:    extraCopy,
		At:       time.Now().Format(time.RFC3339),
	}
	// newest first; keep a short history
	r.History = append([]ReleaseEntry{entry}, r.History...)
	const maxHistory = 50
	if len(r.History) > maxHistory {
		r.History = r.History[:maxHistory]
	}
}

func (log *ReleaseLog) lastVersion(appID string) string {
	if log.Apps == nil {
		return ""
	}
	r := log.Apps[appID]
	if r == nil {
		return ""
	}
	return strings.TrimSpace(r.LastVersion)
}

func (log *ReleaseLog) lastBuild(appID string) string {
	if log.Apps == nil {
		return ""
	}
	r := log.Apps[appID]
	if r == nil {
		return ""
	}
	return strings.TrimSpace(r.LastBuild)
}

// lastForPlatform returns the newest recorded version/build for platform
// (from history, newest-first). Falls back to app-level Last* for ios when
// history has no platform tag (early records).
func (log *ReleaseLog) lastForPlatform(appID, platform string) (version, build string) {
	if log.Apps == nil {
		return "", ""
	}
	r := log.Apps[appID]
	if r == nil {
		return "", ""
	}
	for _, e := range r.History {
		if strings.EqualFold(e.Platform, platform) {
			return strings.TrimSpace(e.Version), strings.TrimSpace(e.Build)
		}
	}
	if platform == platformIOS {
		return strings.TrimSpace(r.LastVersion), strings.TrimSpace(r.LastBuild)
	}
	return "", ""
}

// history returns newest-first release entries for appID (may be empty).
func (log *ReleaseLog) history(appID string) []ReleaseEntry {
	if log.Apps == nil {
		return nil
	}
	r := log.Apps[appID]
	if r == nil {
		return nil
	}
	return append([]ReleaseEntry(nil), r.History...)
}

// versionList groups history by marketing version (newest version first).
// Within a version, each platform keeps the newest entry only.
func (log *ReleaseLog) versionList(appID string) []VersionRelease {
	hist := log.history(appID)
	if len(hist) == 0 {
		return nil
	}
	order := make([]string, 0)
	byVer := map[string]*VersionRelease{}
	for _, e := range hist {
		v := strings.TrimSpace(e.Version)
		if v == "" {
			continue
		}
		g := byVer[v]
		if g == nil {
			g = &VersionRelease{
				Version:   v,
				At:        e.At,
				Platforms: map[string]ReleaseEntry{},
			}
			byVer[v] = g
			order = append(order, v)
		}
		plat := strings.ToLower(strings.TrimSpace(e.Platform))
		if plat == "" {
			continue
		}
		// History is newest-first: first hit for version+platform wins.
		if _, ok := g.Platforms[plat]; !ok {
			g.Platforms[plat] = e
		}
		// Keep At as the newest activity for the version.
		if e.At > g.At {
			g.At = e.At
		}
	}
	out := make([]VersionRelease, 0, len(order))
	for _, v := range order {
		out = append(out, *byVer[v])
	}
	return out
}

// entryFor returns the newest history entry for version+platform, if any.
func (log *ReleaseLog) entryFor(appID, version, platform string) (ReleaseEntry, bool) {
	version = strings.TrimSpace(version)
	platform = strings.ToLower(strings.TrimSpace(platform))
	for _, e := range log.history(appID) {
		if strings.TrimSpace(e.Version) == version &&
			strings.EqualFold(e.Platform, platform) {
			return e, true
		}
	}
	return ReleaseEntry{}, false
}

// markNotarized sets Notarized on the newest matching entry and saves nothing
// (caller persists). Returns false if no matching entry.
func (log *ReleaseLog) markNotarized(appID, version, platform string) bool {
	if log.Apps == nil {
		return false
	}
	r := log.Apps[appID]
	if r == nil {
		return false
	}
	version = strings.TrimSpace(version)
	for i := range r.History {
		e := &r.History[i]
		if strings.TrimSpace(e.Version) == version &&
			strings.EqualFold(e.Platform, platform) {
			e.Notarized = true
			return true
		}
	}
	return false
}

// versionMeta returns (or creates) VersionMeta for a marketing version.
func (log *ReleaseLog) versionMeta(appID, version string) *VersionMeta {
	r := log.recordFor(appID)
	version = strings.TrimSpace(version)
	if version == "" {
		return &VersionMeta{}
	}
	if r.Versions == nil {
		r.Versions = map[string]*VersionMeta{}
	}
	m := r.Versions[version]
	if m == nil {
		m = &VersionMeta{}
		r.Versions[version] = m
	}
	return m
}

// isVersionReleased reports whether the marketing version was marked released.
func (log *ReleaseLog) isVersionReleased(appID, version string) bool {
	if log.Apps == nil {
		return false
	}
	r := log.Apps[appID]
	if r == nil || r.Versions == nil {
		return false
	}
	m := r.Versions[strings.TrimSpace(version)]
	return m != nil && m.Released
}

// latestVersion returns the first version in versionList (newest activity group).
func (log *ReleaseLog) latestVersion(appID string) string {
	vs := log.versionList(appID)
	if len(vs) == 0 {
		return ""
	}
	return vs[0].Version
}

// versionHasHistory is true if any history entry uses this marketing version.
func (log *ReleaseLog) versionHasHistory(appID, version string) bool {
	version = strings.TrimSpace(version)
	for _, e := range log.history(appID) {
		if strings.TrimSpace(e.Version) == version {
			return true
		}
	}
	return false
}

// versionIsMutable is true when the user may build or delete platform
// artifacts for this marketing version:
//   - not marked released
//   - and either it is the open version, the latest known version, or a brand-new
//     version string after the previous latest was released (or none exists)
func (log *ReleaseLog) versionIsMutable(appID, version string) bool {
	version = strings.TrimSpace(version)
	if version == "" {
		return false
	}
	if log.isVersionReleased(appID, version) {
		return false
	}
	if ov := log.openVersion(appID); ov != "" && ov == version {
		return true
	}
	latest := log.latestVersion(appID)
	if latest == "" {
		return true
	}
	if version == latest {
		return true
	}
	// Brand-new version string: only if current latest is already frozen.
	if !log.versionHasHistory(appID, version) {
		return log.isVersionReleased(appID, latest)
	}
	// Older versions with history: frozen even if never marked released.
	return false
}

// openVersion returns the persisted open marketing version, if any.
func (log *ReleaseLog) openVersion(appID string) string {
	if log.Apps == nil {
		return ""
	}
	r := log.Apps[appID]
	if r == nil {
		return ""
	}
	return strings.TrimSpace(r.OpenVersion)
}

// setOpenVersion records which marketing version is open for bundling.
func (log *ReleaseLog) setOpenVersion(appID, version string) {
	r := log.recordFor(appID)
	r.OpenVersion = strings.TrimSpace(version)
}

// markVersionReleased freezes a marketing version. Caller persists.
func (log *ReleaseLog) markVersionReleased(appID, version string) {
	m := log.versionMeta(appID, version)
	m.Released = true
	m.ReleasedAt = time.Now().Format(time.RFC3339)
	// Clear open pointer if we just froze it.
	if log.openVersion(appID) == strings.TrimSpace(version) {
		log.setOpenVersion(appID, "")
	}
}

// deletePlatformBuild removes history entries for version+platform and best-effort
// deletes their artifact files. Returns how many history rows were removed.
func (log *ReleaseLog) deletePlatformBuild(appID, version, platform string) (removed int, paths []string) {
	if log.Apps == nil {
		return 0, nil
	}
	r := log.Apps[appID]
	if r == nil {
		return 0, nil
	}
	version = strings.TrimSpace(version)
	platform = strings.ToLower(strings.TrimSpace(platform))
	var keep []ReleaseEntry
	seenPath := map[string]bool{}
	for _, e := range r.History {
		if strings.TrimSpace(e.Version) == version &&
			strings.EqualFold(e.Platform, platform) {
			removed++
			for _, p := range append([]string{e.Path}, e.Extra...) {
				p = strings.TrimSpace(p)
				if p != "" && !seenPath[p] {
					seenPath[p] = true
					paths = append(paths, p)
				}
			}
			continue
		}
		keep = append(keep, e)
	}
	r.History = keep
	// Refresh LastVersion/LastBuild from remaining history head if needed.
	if len(r.History) > 0 {
		r.LastVersion = strings.TrimSpace(r.History[0].Version)
		r.LastBuild = strings.TrimSpace(r.History[0].Build)
	}
	return removed, paths
}

// fileExists reports whether path is a non-empty existing file.
// Uses a short TTL cache — safe for immediate-mode UI paint paths.
func fileExists(path string) bool {
	exists, isDir := cachedPathInfo(path)
	return exists && !isDir
}

// pathExists reports a file or directory at path.
// Uses a short TTL cache — safe for immediate-mode UI paint paths.
func pathExists(path string) bool {
	exists, _ := cachedPathInfo(path)
	return exists
}
