package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"go.hasen.dev/shirei/app"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

const (
	screenMain     = 0
	screenApp      = 1 // app hub: open version + platforms
	screenBasic    = 2 // shared package / identity config
	screenPlatform = 3 // one platform's packaging + identity overrides
	screenHistory  = 4 // secondary version archive
	// Legacy aliases (old navigation paths still resolve here).
	screenEdit     = screenBasic
	screenRelease  = screenApp
	screenProgress = 99
)

var (
	wd         string
	store      Store
	releaseLog ReleaseLog

	screen    int
	editApp   App // draft while editing
	editIsNew bool
	editIndex int // index in store.Apps when editing existing

	// Current app context (app / platform / history screens).
	historyIndex    int    // selected app index
	detailVersion   string // open marketing version on the app page
	newVersionDraft string // draft for "New version"
	editPlatform    string // platform id when on screenPlatform

	// Delete / re-bundle confirmation (Modal).
	confirmDeletePlat string // platform id when dialog open; empty = closed
	confirmDeleteVer  string // marketing version for that delete
	// Session-only Android signing secrets (never persisted).
	androidStorePass     string
	androidKeyPass       string // empty → use store password
	showAndroidPassModal bool   // Bundle/Re-bundle password prompt
	androidPassErr       string

	installSerial string // preferred adb serial for install
	installBusy   bool
	installMsg    string

	jobs       *JobHub
	showLogPtr map[string]*bool // platform → log popup open (addressable for PopupPanel)

	teams               []Team
	teamsReady          bool
	identities          []string // codesigning (app)
	installerIdentities []string // productbuild / pkg
	// Cached Mac App Store profiles (decoded once; menu must not re-scan).
	macProvisionProfiles []ProvisioningProfile
	identsReady          bool
	saveErr              string
	actionErr            string
	showNewVersion       bool // new-version draft panel on app page

	// provisionLabelCache avoids decoding a selected profile every frame.
	provisionLabelPath  string
	provisionLabelCache string

	// Screen snapshots: filled on navigation / explicit refresh, painted every frame.
	mainSnaps    []mainAppSnap
	editSnap     editScreenSnap
	versionSnaps map[string]platformSnap // platform → artifact snapshot
)

// mainAppSnap is disk/validation state for one main-list row (filled when entering main).
type mainAppSnap struct {
	IconPath string
	IconOK   bool
	Invalid  bool
}

// editScreenSnap is disk/validation state for the edit form.
type editScreenSnap struct {
	PkgKey  string // Package + IconPath — recompute when draft paths change
	IconAbs string
	IconOK  bool
	Issues  []ValidationIssue
}

// platformSnap is artifact state for one platform on the version page.
type platformSnap struct {
	Has       bool
	Exists    bool // primary path present
	Entry     ReleaseEntry
	PathOK    map[string]bool // path → exists
	Issues    []ValidationIssue
	LastVer   string
	LastBuild string
	NextBuild string
	Mutable   bool
}

func wakeUI() { RequestNextFrame() }

func initGUIState() {
	wd, _ = os.Getwd()
	if abs, err := filepath.Abs(wd); err == nil {
		wd = abs
	}
	store = loadStore()
	releaseLog = loadReleaseLog()
	jobs = newJobHub()
	showLogPtr = map[string]*bool{}
	screen = screenMain
	refreshMainSnaps()
}

func jobLogOpen(platform string) *bool {
	if p := showLogPtr[platform]; p != nil {
		return p
	}
	b := false
	showLogPtr[platform] = &b
	return &b
}

func jobTitle(platform string) string {
	switch platform {
	case platformIOS:
		return "iOS bundle"
	case platformAndroid:
		return "Android bundle"
	case platformMacOS:
		return "macOS bundle"
	case platformLinux:
		return "Linux bundle"
	case platformWindows:
		return "Windows bundle"
	case "macos-notarize":
		return "macOS notarize"
	default:
		return platform + " job"
	}
}

// recordAndSaveRelease updates history under a lock so parallel platform jobs
// do not clobber bundle-releases.json.
func recordAndSaveRelease(appID, platform, version, build, path string, extra ...string) error {
	releaseLogMu.Lock()
	defer releaseLogMu.Unlock()
	releaseLog.recordSuccess(appID, platform, version, build, path, extra...)
	return saveReleaseLog(releaseLog)
}

func markNotarizedAndSave(appID, version, platform string) error {
	releaseLogMu.Lock()
	defer releaseLogMu.Unlock()
	releaseLog.markNotarized(appID, version, platform)
	return saveReleaseLog(releaseLog)
}

// refreshMainSnaps loads path/validation data for the main list once per visit.
func refreshMainSnaps() {
	mainSnaps = make([]mainAppSnap, len(store.Apps))
	for i, a := range store.Apps {
		pkgDir := resolvePackagePath(wd, a.Package)
		iconPath := resolveIconPath(pkgDir, a.IconPath)
		exists, isDir := false, true
		if iconPath != "" {
			st, err := os.Stat(iconPath)
			exists = err == nil
			isDir = exists && st.IsDir()
		}
		mainSnaps[i] = mainAppSnap{
			IconPath: iconPath,
			IconOK:   exists && !isDir,
			Invalid:  appConfiguredIncomplete(a, wd),
		}
	}
}

// refreshEditSnap loads icon existence + validation for the current edit draft.
func refreshEditSnap() {
	pkgDir := resolvePackagePath(wd, editApp.Package)
	iconAbs := resolveIconPath(pkgDir, editApp.IconPath)
	ok := false
	if iconAbs != "" {
		if st, err := os.Stat(iconAbs); err == nil && !st.IsDir() {
			ok = true
		}
	}
	var issues []ValidationIssue
	if !editIsNew {
		issues = append(issues, validateAppShared(editApp, wd)...)
		if editApp.IOS != nil {
			issues = append(issues, validateIOSWithWD(editApp, wd)...)
		}
		if editApp.Android != nil {
			issues = append(issues, validateAndroidWithWD(editApp, wd)...)
		}
		if editApp.MacOS != nil {
			issues = append(issues, validateMacOSWithWD(editApp, wd)...)
		}
		if editApp.Linux != nil {
			issues = append(issues, validateLinuxWithWD(editApp, wd)...)
		}
		if editApp.Windows != nil {
			issues = append(issues, validateWindowsWithWD(editApp, wd)...)
		}
	}
	editSnap = editScreenSnap{
		PkgKey:  editApp.Package + "\x00" + editApp.IconPath,
		IconAbs: iconAbs,
		IconOK:  ok,
		Issues:  issues,
	}
}

// maybeRefreshEditSnap recomputes edit derived state when package/icon paths change.
func maybeRefreshEditSnap() {
	key := editApp.Package + "\x00" + editApp.IconPath
	if key != editSnap.PkgKey {
		refreshEditSnap()
	}
}

// refreshVersionSnaps reloads artifact existence for the open version page.
// Cost: a few os.Stat + in-memory validation — fine to call inline on the UI
// thread after open/delete/mark-released. From background jobs, use
// refreshVersionSnapsFromBackground so assignment happens under the frame lock.
func refreshVersionSnaps() {
	versionSnaps = computeVersionSnaps()
}

// refreshVersionSnapsFromBackground is for build/notarize goroutines.
// Still cheap (Stats), but must not mutate UI state off the frame lock.
func refreshVersionSnapsFromBackground() {
	snaps := computeVersionSnaps()
	WithFrameLock(func() {
		versionSnaps = snaps
	})
	RequestNextFrame()
}

func computeVersionSnaps() map[string]platformSnap {
	if historyIndex < 0 || historyIndex >= len(store.Apps) {
		return nil
	}
	a := store.Apps[historyIndex]
	ver := strings.TrimSpace(detailVersion)
	mutable := releaseLog.versionIsMutable(a.ID, ver)
	out := map[string]platformSnap{}
	for _, plat := range []string{platformIOS, platformAndroid, platformMacOS, platformLinux, platformWindows} {
		entry, has := releaseLog.entryFor(a.ID, ver, plat)
		pathOK := map[string]bool{}
		exists := false
		if has {
			for _, p := range append([]string{entry.Path}, entry.Extra...) {
				if p == "" {
					continue
				}
				st, err := os.Stat(p)
				ok := err == nil
				pathOK[p] = ok
				if p == entry.Path && ok {
					exists = true
				}
				if p == entry.Path && err == nil && st.IsDir() {
					exists = true
				}
			}
			if !exists {
				if _, err := os.Stat(entry.Path); err == nil {
					exists = true
				}
			}
		}
		lastVer, lastBuild := releaseLog.lastForPlatform(a.ID, plat)
		var issues []ValidationIssue
		if !has && mutable {
			switch plat {
			case platformIOS:
				issues = append(validateAppShared(a, wd), validateIOSWithWD(a, wd)...)
			case platformAndroid:
				issues = append(validateAppShared(a, wd), validateAndroidWithWD(a, wd)...)
			case platformMacOS:
				issues = append(validateAppShared(a, wd), validateMacOSWithWD(a, wd)...)
			case platformLinux:
				issues = append(validateAppShared(a, wd), validateLinuxWithWD(a, wd)...)
			case platformWindows:
				issues = append(validateAppShared(a, wd), validateWindowsWithWD(a, wd)...)
			}
		}
		out[plat] = platformSnap{
			Has: has, Exists: exists, Entry: entry, PathOK: pathOK,
			Issues: issues, LastVer: lastVer, LastBuild: lastBuild,
			NextBuild: nextBuildNumber(lastBuild), Mutable: mutable,
		}
	}
	return out
}

func guiMain() {
	initGUIState()

	go func() {
		if runtime.GOOS == "darwin" {
			t, _ := listXcodeTeams()
			teams = t
			teamsReady = true
			identities = listCodesignIdentities()
			installerIdentities = listInstallerIdentities()
			macProvisionProfiles = listMacAppStoreProfiles("")
			identsReady = true
		} else {
			teamsReady = true
			identsReady = true
		}
		ensurePackages(wd)
		wakeUI()
	}()

	app.SetupIconBytes(iconPNG)
	app.SetupWindow("Shirei Bundle", 880, 720)
	app.Run(RootView)
}

func RootView() {
	// Engine root is already window-sized. Viewport here matches mobilerun:
	// extrinsic budget so Grow children can take leftover main-axis space.
	ModAttrs(Viewport, Expand, Background(220, 12, 96, 1))

	switch screen {
	case screenApp:
		viewApp()
	case screenBasic:
		viewBasic()
	case screenPlatform:
		viewPlatform()
	case screenHistory:
		viewHistory()
	default:
		Container(Attrs(Pad2(12, 16), Gap(10), Expand), func() {
			viewMain()
		})
	}
}

func header(title, subtitle string) {
	Container(Attrs(Row, CrossMid, Gap(12)), func() {
		if appIcon != nil {
			ImageView(UseImage("bundle-app-icon", appIcon), Vec2{48, 48})
		}
		Container(Attrs(Gap(4)), func() {
			Label(title, FontSize(22), FontWeight(WeightBold))
			if subtitle != "" {
				Label(subtitle, FontSize(12), TextColor(0, 0, 45, 1))
			}
		})
	})
}

func fieldLabel(s string) {
	Label(s, FontSize(11), FontWeight(WeightBold), TextColor(220, 15, 30, 1))
}

// --- Main: list of apps ---

func viewMain() {
	header("Shirei Bundle", "Release packaging for shirei apps")
	Label(wd, FontSize(11), TextColor(0, 0, 55, 1))

	if len(store.Apps) == 0 {
		Container(Attrs(Pad(40), Gap(16), Center, Expand), func() {
			Label("No applications configured yet.", FontSize(14), TextColor(0, 0, 40, 1))
			if ButtonExt("Add application", ButtonAttrs{Accent: AccentMeadow}, DefaultButtonLook()) {
				startAddApp()
			}
		})
		return
	}

	Container(Attrs(Row, CrossMid, Gap(10)), func() {
		Filler(1)
		if ButtonExt("Add application", ButtonAttrs{Accent: AccentMeadow}, DefaultButtonLook()) {
			startAddApp()
		}
	})

	Container(Attrs(Gap(12), Expand), func() {
		for i := range store.Apps {
			i := i
			a := store.Apps[i]
			appListCard(i, a)
		}
	})
}

// appListCard is one full-width application row on the main screen.
// The whole row opens the app page.
func appListCard(i int, a App) {
	var snap mainAppSnap
	if i < len(mainSnaps) {
		snap = mainSnaps[i]
	}

	Container(Attrs(Expand, Pad(16), Gap(12),
		Background(0, 0, 100, 1),
		BorderWidth(1), BorderColor(0, 0, 82, 1),
		Corners(10)), func() {
		st := ProcessButtonEvents(false)
		if st.Hovered {
			ModAttrs(Background(0, 0, 97, 1))
		}
		if st.Clicked {
			openAppScreen(i)
		}

		Container(Attrs(Row, CrossAlign(AlignStart), Gap(16), Expand), func() {
			Container(Attrs(FixSize(72, 72), Clip, Corners(16),
				Background(0, 0, 92, 1), BorderWidth(1), BorderColor(0, 0, 80, 1), Center), func() {
				if snap.IconOK && snap.IconPath != "" {
					Image(snap.IconPath, Vec2{72, 72})
					return
				}
				Label("—", FontSize(18), TextColor(0, 0, 60, 1))
			})

			Container(Attrs(Gap(8), Expand), func() {
				Container(Attrs(Row, CrossMid, Gap(10), Expand), func() {
					Container(Attrs(Gap(2)), func() {
						Label(a.displayName(), FontSize(17), FontWeight(WeightBold))
						pkg := a.Package
						if pkg == "" {
							pkg = "(no package)"
						}
						Label(pkg, FontSize(12), TextColor(0, 0, 45, 1))
					})
					Filler(1)
					if snap.Invalid {
						Label("Invalid configuration", FontSize(11), TextColor(10, 70, 40, 1))
					}
					openVer := openVersionFor(a.ID)
					if openVer != "" {
						Label("v"+openVer+" open", FontSize(12), TextColor(220, 55, 40, 1))
					} else if lv := releaseLog.latestVersion(a.ID); lv != "" {
						Label("v"+lv, FontSize(12), TextColor(0, 0, 45, 1))
					}
				})

				Container(Attrs(Row, CrossAlign(AlignStart), Gap(20), Wrap), func() {
					mainPlatformChip(a, platformIOS, TypVendorApple, "iOS", a.IOS != nil)
					mainPlatformChip(a, platformAndroid, TypVendorAndroid, "Android", a.Android != nil)
					mainPlatformChip(a, platformMacOS, TypDeviceDesktop, "macOS", a.MacOS != nil)
					mainPlatformChip(a, platformLinux, TypDeviceLaptop, "Linux", a.Linux != nil)
					mainPlatformChip(a, platformWindows, TypVendorMicrosoft, "Windows", a.Windows != nil)
				})
			})
		})
	})
}

// mainPlatformChip shows enabled platforms with last release, or muted "not configured".
func mainPlatformChip(a App, platform string, icon IconGlyph, name string, enabled bool) {
	if enabled {
		ver, build := releaseLog.lastForPlatform(a.ID, platform)
		platformReleaseChip(icon, name, ver, build)
		return
	}
	Container(Attrs(Gap(4), MinWidth(88)), func() {
		Container(Attrs(Row, CrossMid, Gap(6)), func() {
			Icon(icon, FontSize(16), TextColor(0, 0, 60, 1))
			Label(name, FontSize(13), FontWeight(WeightBold), TextColor(0, 0, 55, 1))
		})
		Label("not configured", FontSize(11), TextColor(0, 0, 60, 1))
	})
}

// openVersionFor returns the mutable (open) marketing version, if any.
func openVersionFor(appID string) string {
	if ov := releaseLog.openVersion(appID); ov != "" && releaseLog.versionIsMutable(appID, ov) {
		return ov
	}
	latest := releaseLog.latestVersion(appID)
	if latest == "" {
		return ""
	}
	if releaseLog.versionIsMutable(appID, latest) {
		return latest
	}
	return ""
}

func openAppScreen(i int) {
	if i < 0 || i >= len(store.Apps) {
		return
	}
	historyIndex = i
	installMsg = ""
	installBusy = false
	actionErr = ""
	showNewVersion = false
	showAndroidPassModal = false
	androidPassErr = ""
	confirmDeletePlat = ""
	confirmDeleteVer = ""
	releaseLog = loadReleaseLog()
	a := store.Apps[i]
	detailVersion = openVersionFor(a.ID)
	if detailVersion == "" {
		// Suggest next version when nothing is open (first release or all frozen).
		newVersionDraft = defaultReleaseVersion(releaseLog.lastVersion(a.ID))
	} else {
		newVersionDraft = bumpPatchVersion(detailVersion)
	}
	if a.Android != nil && !adbDevCache.ready && !adbDevCache.scanning {
		rescanADBDevices()
	}
	refreshVersionSnaps()
	screen = screenApp
}

func platformReleaseChip(icon IconGlyph, name, version, build string) {
	Container(Attrs(Gap(4), MinWidth(88)), func() {
		Container(Attrs(Row, CrossMid, Gap(6)), func() {
			Icon(icon, FontSize(16), TextColor(0, 0, 35, 1))
			Label(name, FontSize(13), FontWeight(WeightBold))
		})
		if version != "" {
			Label("v"+version, FontSize(12), TextColor(0, 0, 30, 1))
		} else {
			Label("no release", FontSize(12), TextColor(0, 0, 55, 1))
		}
		if build != "" {
			Label("build "+build, FontSize(11), TextColor(0, 0, 50, 1))
		} else {
			Label("—", FontSize(11), TextColor(0, 0, 60, 1))
		}
	})
}

func startAddApp() {
	editIsNew = true
	editIndex = -1
	editPlatform = ""
	editApp = App{
		ID:          newAppID(),
		AppIDPrefix: "systems.judi",
	}
	saveErr = ""
	actionErr = ""
	refreshEditSnap()
	screen = screenBasic
}

func startBasicEdit(i int) {
	editIsNew = false
	editIndex = i
	editPlatform = ""
	editApp = copyAppDraft(store.Apps[i])
	saveErr = ""
	actionErr = ""
	refreshEditSnap()
	screen = screenBasic
}

// startEditApp is the old entry; basic config only.
func startEditApp(i int) { startBasicEdit(i) }

func startPlatformEdit(appIndex int, platform string) {
	if appIndex < 0 || appIndex >= len(store.Apps) {
		return
	}
	editIsNew = false
	editIndex = appIndex
	editPlatform = platform
	editApp = copyAppDraft(store.Apps[appIndex])
	// Ensure platform block exists so the form can edit it.
	switch platform {
	case platformIOS:
		if editApp.IOS == nil {
			editApp.IOS = defaultIOSConfig()
		}
	case platformAndroid:
		if editApp.Android == nil {
			editApp.Android = defaultAndroidConfig()
		}
	case platformMacOS:
		if editApp.MacOS == nil {
			editApp.MacOS = defaultMacOSConfig()
		}
	case platformLinux:
		if editApp.Linux == nil {
			editApp.Linux = defaultLinuxConfig()
		}
	case platformWindows:
		if editApp.Windows == nil {
			editApp.Windows = defaultWindowsConfig()
		}
	}
	saveErr = ""
	actionErr = ""
	refreshEditSnap()
	screen = screenPlatform
}

func copyAppDraft(a App) App {
	out := a
	if a.IOS != nil {
		ios := *a.IOS
		out.IOS = &ios
	}
	if a.Android != nil {
		and := *a.Android
		out.Android = &and
	}
	if a.MacOS != nil {
		m := *a.MacOS
		out.MacOS = &m
	}
	if a.Linux != nil {
		l := *a.Linux
		out.Linux = &l
	}
	if a.Windows != nil {
		w := *a.Windows
		out.Windows = &w
	}
	return out
}

// --- Basic config (shared identity) ---

func viewBasic() {
	pkgs, pkgsReady, pkgsErr := ensurePackages(wd)
	maybeRefreshEditSnap()

	title := "Basic configuration"
	if editIsNew {
		title = "Add application"
	}
	sub := "Shared package, name, app ID, and icon. Platforms can override these."

	Container(Attrs(Pad2(12, 16), Gap(10), Grow(1), Expand), func() {
		header(title, sub)

		Container(Attrs(Row, CrossMid, Gap(8)), func() {
			if ButtonExt("← Back", ButtonAttrs{}, DefaultButtonLook()) {
				if editIsNew || editIndex < 0 {
					refreshMainSnaps()
					screen = screenMain
				} else {
					openAppScreen(editIndex)
				}
			}
			Filler(1)
			if ButtonExt("Save", ButtonAttrs{Accent: AccentMeadow}, DefaultButtonLook()) {
				doSaveEdit()
			}
		})
		if saveErr != "" {
			Label(saveErr, FontSize(12), TextColor(10, 70, 40, 1))
		}
		if !editIsNew && len(editSnap.Issues) > 0 {
			Container(Attrs(Pad(8), Gap(2), Background(15, 40, 96, 1), Corners(6)), func() {
				Label("Needs attention:", FontSize(11), FontWeight(WeightBold), TextColor(10, 60, 30, 1))
				for _, is := range editSnap.Issues {
					Label("• "+is.Message, FontSize(11), TextColor(10, 55, 30, 1))
				}
			})
		}

		Container(Attrs(Viewport), func() {
			ScrollOnInput()
			basicFormBody(pkgs, pkgsReady, pkgsErr)
			ScrollBars()
		})
	})
}

// viewPlatform edits one platform: identity overrides + packaging.
func viewPlatform() {
	maybeRefreshEditSnap()
	plat := editPlatform
	title := platformDisplayName(plat) + " configuration"
	sub := "Empty identity fields inherit from basic configuration."

	Container(Attrs(Pad2(12, 16), Gap(10), Grow(1), Expand), func() {
		header(title, sub)

		Container(Attrs(Row, CrossMid, Gap(8)), func() {
			if ButtonExt("← Back", ButtonAttrs{}, DefaultButtonLook()) {
				if editIndex >= 0 {
					openAppScreen(editIndex)
				} else {
					refreshMainSnaps()
					screen = screenMain
				}
			}
			Filler(1)
			if ButtonExt("Save", ButtonAttrs{Accent: AccentMeadow}, DefaultButtonLook()) {
				doSaveEdit()
			}
		})
		if saveErr != "" {
			Label(saveErr, FontSize(12), TextColor(10, 70, 40, 1))
		}

		pkgDir := resolvePackagePath(wd, editApp.Package)
		Container(Attrs(Viewport), func() {
			ScrollOnInput()
			Container(Attrs(Gap(12), Expand), func() {
				Label("Identity overrides", FontSize(14), FontWeight(WeightBold))
				id := editApp.platformIdentityPtr(plat)
				if id != nil {
					platformIdentityFields(id, pkgDir)
				}
				Label("Packaging", FontSize(14), FontWeight(WeightBold))
				switch plat {
				case platformIOS:
					if editApp.IOS != nil {
						iosPackagingFields(editApp.IOS)
					}
				case platformAndroid:
					if editApp.Android != nil {
						androidPackagingFields(editApp.Android)
					}
				case platformMacOS:
					if editApp.MacOS != nil {
						macosPackagingFields(editApp.MacOS)
					}
				case platformLinux:
					if editApp.Linux != nil {
						linuxPackagingFields(editApp.Linux)
					}
				case platformWindows:
					if editApp.Windows != nil {
						windowsPackagingFields(editApp.Windows)
					}
				}
			})
			ScrollBars()
		})
	})
}

func basicFormBody(pkgs []MainPkg, pkgsReady bool, pkgsErr error) {
	Container(Attrs(Gap(12), Expand), func() {
		// Package
		Container(Attrs(Gap(4)), func() {
			fieldLabel("Package")
			current := editApp.Package
			if current == "" {
				current = "Select package…"
			}
			Container(Attrs(Row, CrossMid, Gap(8)), func() {
				switch {
				case pkgsErr != nil && len(pkgs) == 0:
					ButtonExt(current, ButtonAttrs{Icon: TypArrowSortedDown, Disabled: true}, DefaultButtonLook())
				case !pkgsReady && len(pkgs) == 0:
					ButtonExt("Scanning…", ButtonAttrs{Icon: TypArrowSortedDown, Disabled: true}, DefaultButtonLook())
				default:
					MenuButton(MenuIcon, current, func() {
						_ = MenuFilterQuery()
						for _, p := range pkgs {
							p := p
							label := p.Rel
							if label == "" || label == "." {
								label = p.Dir
							}
							if !MenuFilterMatches(label) {
								continue
							}
							if MenuItem(NoIcon, label) {
								editApp.Package = p.Rel
								if p.Rel == "." {
									editApp.Package = p.Dir
								}
								// Fill defaults when empty
								if strings.TrimSpace(editApp.Name) == "" {
									editApp.Name = filepath.Base(p.Dir)
								}
								if strings.TrimSpace(editApp.AppID) == "" && strings.TrimSpace(editApp.AppIDPrefix) != "" {
									editApp.AppID = strings.TrimRight(editApp.AppIDPrefix, ".") + "." + sanitizeAppIDComponent(filepath.Base(p.Dir))
								}
								if strings.TrimSpace(editApp.IconPath) == "" {
									if rel := defaultPackageIconRel(p.Dir); rel != "" {
										editApp.IconPath = rel
									}
								}
								refreshEditSnap()
							}
						}
					})
				}
				if ButtonExt("", ButtonAttrs{Icon: SymRefresh}, DefaultButtonLook()) {
					invalidatePackages()
					ensurePackages(wd)
				}
			})
		})

		pkgDir := resolvePackagePath(wd, editApp.Package)

		// Identity row
		Container(Attrs(Row, Gap(16), CrossAlign(AlignStart)), func() {
			// Icon preview
			Container(Attrs(FixSize(96, 96), Clip, Corners(18),
				Background(0, 0, 90, 1), BorderWidth(1), BorderColor(0, 0, 75, 1), Center), func() {
				if editSnap.IconOK && editSnap.IconAbs != "" {
					Image(editSnap.IconAbs, Vec2{96, 96})
					return
				}
				Label("no icon", FontSize(11), TextColor(0, 0, 55, 1))
			})

			Container(Attrs(Gap(8), Expand), func() {
				Container(Attrs(Gap(3)), func() {
					fieldLabel("Application name")
					TextInput(&editApp.Name)
				})
				Container(Attrs(Gap(3)), func() {
					fieldLabel("App ID prefix")
					TextInput(&editApp.AppIDPrefix)
				})
				Container(Attrs(Gap(3)), func() {
					fieldLabel("App ID (full reverse-DNS)")
					TextInput(&editApp.AppID)
					if bid := editApp.effectiveBundleID(); bid != "" {
						Label("Effective: "+bid, FontSize(10), TextColor(0, 0, 50, 1))
					}
				})
				Container(Attrs(Gap(3)), func() {
					fieldLabel("Icon")
					DirectoryBrowseExt(&editApp.IconPath, FileBrowserAttrs{
						Files: true, Dirs: false, Title: "Choose app icon",
						Width: 560, Start: iconBrowseStart(pkgDir, editApp.IconPath),
						Exts: []string{".png", ".jpg", ".jpeg", ".webp", ".gif"},
					})
					Label("Relative to package when under the package directory",
						FontSize(10), TextColor(0, 0, 50, 1))
				})
			})
		})
	})
}

func iosPackagingFields(c *IOSConfig) {
	Container(Attrs(Gap(8), Expand), func() {
		Container(Attrs(Gap(3)), func() {
			fieldLabel("Apple Team ID")
			if teamsReady && len(teams) > 0 {
				cur := c.TeamID
				lab := cur
				if lab == "" {
					lab = "Select team…"
				}
				for _, t := range teams {
					if t.ID == cur {
						lab = teamLabel(t)
						break
					}
				}
				MenuButton(MenuIcon, lab, func() {
					for _, t := range teams {
						t := t
						if MenuItem(NoIcon, teamLabel(t)) {
							c.TeamID = t.ID
						}
					}
				})
			} else {
				TextInput(&c.TeamID)
			}
		})
		Container(Attrs(Gap(3)), func() {
			fieldLabel("Signing identity (optional pin)")
			cur := c.Identity
			if cur == "" {
				cur = "Automatic"
			}
			MenuButton(MenuIcon, cur, func() {
				if MenuItem(NoIcon, "Automatic") {
					c.Identity = ""
				}
				for _, id := range identities {
					id := id
					if MenuItem(NoIcon, id) {
						c.Identity = id
					}
				}
			})
		})
		Container(Attrs(Gap(3)), func() {
			fieldLabel("Export method")
			SegmentedControl(&c.Method,
				Cell("Debugging", "debugging"),
				Cell("App Store", "app-store-connect"),
				Cell("Ad Hoc", "ad-hoc"),
			)
			Label("App Store export may show a system keychain password dialog (Allow / Always Allow).",
				FontSize(10), TextColor(0, 0, 50, 1))
		})
		Container(Attrs(Gap(3)), func() {
			fieldLabel("Release directory")
			DirectoryBrowseExt(&c.ReleaseDir, FileBrowserAttrs{
				Files: false, Dirs: true, Title: "Release output directory",
				Width: 560, Start: releaseDirStart(c.ReleaseDir),
			})
			Label("IPA is written here (created if missing). Relative paths are from the working directory.",
				FontSize(10), TextColor(0, 0, 50, 1))
		})
	})
}

func androidPackagingFields(c *AndroidConfig) {
	Container(Attrs(Gap(8), Expand), func() {
		Container(Attrs(Gap(3)), func() {
			fieldLabel("Keystore")
			DirectoryBrowseExt(&c.Keystore, FileBrowserAttrs{
				Files: true, Dirs: false, Title: "Choose release keystore",
				Width: 560, Start: keystoreBrowseStart(c.Keystore),
				Exts: []string{".jks", ".keystore"},
			})
			Label("Path to your .jks / .keystore. Passwords are prompted when bundling, not saved.",
				FontSize(10), TextColor(0, 0, 50, 1))
		})
		Container(Attrs(Gap(3)), func() {
			fieldLabel("Key alias")
			TextInput(&c.KeyAlias)
		})
		Container(Attrs(Gap(3)), func() {
			fieldLabel("ABI")
			SegmentedControl(&c.Arch,
				Cell("arm64", "arm64"),
				Cell("arm 32-bit", "arm"),
			)
		})
		Container(Attrs(Gap(3)), func() {
			fieldLabel("Release directory")
			DirectoryBrowseExt(&c.ReleaseDir, FileBrowserAttrs{
				Files: false, Dirs: true, Title: "Release output directory",
				Width: 560, Start: releaseDirStart(c.ReleaseDir),
			})
			Label("APK is written here (created if missing).",
				FontSize(10), TextColor(0, 0, 50, 1))
		})
	})
}

func macosPackagingFields(c *MacOSConfig) {
	normalizeMacOSConfig(c)
	Container(Attrs(Gap(8), Expand), func() {
		Container(Attrs(Gap(4)), func() {
			fieldLabel("Architectures")
			Container(Attrs(Row, Gap(16), CrossMid, Wrap), func() {
				CheckBox(&c.ArchARM64, "arm64")
				CheckBox(&c.ArchAMD64, "amd64")
			})
			Label("Select both to produce a universal binary (lipo).",
				FontSize(10), TextColor(0, 0, 50, 1))
		})
		Container(Attrs(Gap(4)), func() {
			fieldLabel("Outputs")
			Container(Attrs(Row, Gap(16), CrossMid, Wrap), func() {
				CheckBox(&c.SelfDist, "Self distribution (.app + zip)")
				CheckBox(&c.AppStore, "App Store submission (.pkg)")
			})
			Label("Self-dist uses Developer ID + optional notarize. App Store uses productbuild → Transporter.",
				FontSize(10), TextColor(0, 0, 50, 1))
		})
		if c.SelfDist {
			Container(Attrs(Pad(10), Gap(8),
				Background(0, 0, 97, 1), BorderWidth(1), BorderColor(0, 0, 88, 1), Corners(6)), func() {
				Label("Self distribution", FontSize(12), FontWeight(WeightBold))
				Container(Attrs(Gap(3)), func() {
					fieldLabel("Developer ID identity (optional pin)")
					cur := c.Identity
					if cur == "" {
						cur = "Automatic (first Developer ID Application)"
					}
					MenuButton(MenuIcon, cur, func() {
						if MenuItem(NoIcon, "Automatic") {
							c.Identity = ""
						}
						for _, id := range identities {
							id := id
							if !strings.Contains(id, "Developer ID Application") {
								continue
							}
							if MenuItem(NoIcon, id) {
								c.Identity = id
							}
						}
					})
				})
				Container(Attrs(Gap(3)), func() {
					fieldLabel("Notary keychain profile (for later notarize)")
					TextInput(&c.NotaryProfile)
					Label("Used when you run Notarize on a self-dist build.",
						FontSize(10), TextColor(0, 0, 50, 1))
				})
			})
		}
		if c.AppStore {
			Container(Attrs(Pad(10), Gap(8),
				Background(0, 0, 97, 1), BorderWidth(1), BorderColor(0, 0, 88, 1), Corners(6)), func() {
				Label("App Store", FontSize(12), FontWeight(WeightBold))
				Container(Attrs(Gap(3)), func() {
					fieldLabel("App category (LSApplicationCategoryType)")
					MenuButton(MenuIcon, macOSCategoryLabel(c.Category), func() {
						for _, cat := range macOSAppCategories {
							cat := cat
							if MenuItem(NoIcon, cat.Label) {
								c.Category = cat.UTI
							}
						}
					})
					Label("Required in Info.plist for Transporter / Mac App Store.",
						FontSize(10), TextColor(0, 0, 50, 1))
				})
				Container(Attrs(Gap(3)), func() {
					fieldLabel("App signing identity (optional pin)")
					cur := c.AppStoreIdentity
					if cur == "" {
						cur = "Automatic (Mac App Distribution)"
					}
					MenuButton(MenuIcon, cur, func() {
						if MenuItem(NoIcon, "Automatic") {
							c.AppStoreIdentity = ""
						}
						for _, id := range identities {
							id := id
							if !strings.Contains(id, "3rd Party Mac Developer Application") &&
								!strings.Contains(id, "Apple Distribution") &&
								!strings.Contains(id, "Mac App Distribution") {
								continue
							}
							if MenuItem(NoIcon, id) {
								c.AppStoreIdentity = id
							}
						}
					})
				})
				Container(Attrs(Gap(3)), func() {
					fieldLabel("Installer identity for .pkg")
					cur := c.InstallerIdentity
					if cur == "" {
						cur = "Automatic (3rd Party Mac Developer Installer)"
					}
					MenuButton(MenuIcon, cur, func() {
						if MenuItem(NoIcon, "Automatic") {
							c.InstallerIdentity = ""
						}
						for _, id := range installerIdentities {
							id := id
							if !isMacAppStoreInstallerIdentity(id) {
								continue
							}
							if MenuItem(NoIcon, id) {
								c.InstallerIdentity = id
							}
						}
						if len(installerIdentities) == 0 {
							MenuItem(NoIcon, "(none found — create Mac Installer Distribution in Xcode)")
						}
					})
					Label("Required for Transporter. Uses Mac Installer Distribution.",
						FontSize(10), TextColor(0, 0, 50, 1))
				})
				Container(Attrs(Gap(3)), func() {
					fieldLabel("Provisioning profile (Mac App Store)")
					bundleID := editApp.platformBundleID(platformMacOS)
					cur := strings.TrimSpace(c.ProvisionProfile)
					curLabel := provisionProfileMenuLabel(cur)
					MenuButton(MenuIcon, curLabel, func() {
						if MenuItem(NoIcon, "Automatic (from Xcode / known folders)") {
							c.ProvisionProfile = ""
						}
						found := filterMacProfilesCached(bundleID)
						for _, p := range found {
							p := p
							label := p.Name
							if label == "" {
								label = filepath.Base(p.Path)
							}
							if p.BundleID != "" {
								label += " · " + p.BundleID
							}
							if MenuItem(NoIcon, label) {
								c.ProvisionProfile = p.Path
								provisionLabelPath = ""
							}
						}
						if len(found) == 0 {
							MenuItem(NoIcon, "(none cached — Browse or restart after Download Manual Profiles)")
						}
					})
					DirectoryBrowseExt(&c.ProvisionProfile, FileBrowserAttrs{
						Files: true, Dirs: false,
						Title: "Mac App Store provisioning profile",
						Width: 560,
						Start: provisionBrowseStart(c.ProvisionProfile),
					})
					Label("Automatic uses profiles loaded at startup from Xcode’s folder.",
						FontSize(10), TextColor(0, 0, 50, 1))
				})
			})
		}
		Container(Attrs(Gap(3)), func() {
			fieldLabel("Release directory")
			DirectoryBrowseExt(&c.ReleaseDir, FileBrowserAttrs{
				Files: false, Dirs: true, Title: "Release output directory",
				Width: 560, Start: releaseDirStart(c.ReleaseDir),
			})
			Label("Artifacts (.app, .zip, .pkg) are written under this directory.",
				FontSize(10), TextColor(0, 0, 50, 1))
		})
	})
}

func linuxPackagingFields(c *LinuxConfig) {
	normalizeLinuxConfig(c)
	Container(Attrs(Gap(8), Expand), func() {
		Container(Attrs(Gap(4)), func() {
			fieldLabel("Architectures")
			Container(Attrs(Row, Gap(16), CrossMid, Wrap), func() {
				CheckBox(&c.ArchARM64, "arm64")
				CheckBox(&c.ArchAMD64, "amd64")
			})
			Label("One .tar.gz per selected arch (binary + .desktop + icon).",
				FontSize(10), TextColor(0, 0, 50, 1))
		})
		Container(Attrs(Gap(3)), func() {
			fieldLabel("Release directory")
			DirectoryBrowseExt(&c.ReleaseDir, FileBrowserAttrs{
				Files: false, Dirs: true, Title: "Release output directory",
				Width: 560, Start: releaseDirStart(c.ReleaseDir),
			})
			Label("Tarballs are written under this directory.",
				FontSize(10), TextColor(0, 0, 50, 1))
		})
	})
}

func windowsPackagingFields(c *WindowsConfig) {
	normalizeWindowsConfig(c)
	Container(Attrs(Gap(8), Expand), func() {
		Container(Attrs(Gap(4)), func() {
			fieldLabel("Architectures")
			Container(Attrs(Row, Gap(16), CrossMid, Wrap), func() {
				CheckBox(&c.ArchARM64, "arm64")
				CheckBox(&c.ArchAMD64, "amd64")
			})
			Label("One .zip per selected arch (App.exe + optional icon).",
				FontSize(10), TextColor(0, 0, 50, 1))
		})
		Container(Attrs(Gap(3)), func() {
			fieldLabel("Release directory")
			DirectoryBrowseExt(&c.ReleaseDir, FileBrowserAttrs{
				Files: false, Dirs: true, Title: "Release output directory",
				Width: 560, Start: releaseDirStart(c.ReleaseDir),
			})
			Label("Zips are written under this directory.",
				FontSize(10), TextColor(0, 0, 50, 1))
		})
	})
}

// provisionProfileMenuLabel returns a display string without re-decoding every frame.
func provisionProfileMenuLabel(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return "Automatic (match bundle id from Xcode profiles)"
	}
	if path == provisionLabelPath && provisionLabelCache != "" {
		return provisionLabelCache
	}
	// Prefer startup cache (no process spawn).
	for _, p := range macProvisionProfiles {
		if p.Path == path {
			label := p.Name
			if label == "" {
				label = filepath.Base(path)
			} else {
				label = label + " · " + filepath.Base(path)
			}
			provisionLabelPath = path
			provisionLabelCache = label
			return label
		}
	}
	label := filepath.Base(path)
	provisionLabelPath = path
	provisionLabelCache = label
	return label
}

// filterMacProfilesCached filters the startup profile cache by bundle id.
func filterMacProfilesCached(bundleID string) []ProvisioningProfile {
	bundleID = strings.TrimSpace(bundleID)
	if bundleID == "" {
		return append([]ProvisioningProfile(nil), macProvisionProfiles...)
	}
	var exact, all []ProvisioningProfile
	for _, p := range macProvisionProfiles {
		all = append(all, p)
		if p.BundleID == bundleID {
			exact = append(exact, p)
		}
	}
	if len(exact) > 0 {
		return exact
	}
	return all
}

func provisionBrowseStart(path string) string {
	path = strings.TrimSpace(path)
	if path != "" {
		if st, err := os.Stat(path); err == nil {
			if st.IsDir() {
				return path
			}
			return filepath.Dir(path)
		}
	}
	if home, err := os.UserHomeDir(); err == nil {
		xcode := filepath.Join(home, "Library", "Developer", "Xcode", "UserData", "Provisioning Profiles")
		if st, err := os.Stat(xcode); err == nil && st.IsDir() {
			return xcode
		}
		dl := filepath.Join(home, "Downloads")
		if st, err := os.Stat(dl); err == nil && st.IsDir() {
			return dl
		}
	}
	return wd
}

func keystoreBrowseStart(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		if home, err := os.UserHomeDir(); err == nil {
			return home
		}
		return wd
	}
	if st, err := os.Stat(path); err == nil {
		if st.IsDir() {
			return path
		}
		return filepath.Dir(path)
	}
	return wd
}

// platformIdentityFields edits the four platform identity overrides.
// Empty fields inherit from shared App identity (hinted below each field).
func platformIdentityFields(id *PlatformIdentity, pkgDir string) {
	if id == nil {
		return
	}
	inheritHint := func(empty bool, inherited, emptyMsg string) {
		if empty {
			if inherited != "" {
				Label("Inherits: "+inherited, FontSize(10), TextColor(0, 0, 50, 1))
			} else {
				Label(emptyMsg, FontSize(10), TextColor(0, 0, 50, 1))
			}
		} else {
			Label("Platform-specific; clear to inherit", FontSize(10), TextColor(0, 0, 50, 1))
		}
	}
	Container(Attrs(Gap(3)), func() {
		fieldLabel("Application name (optional override)")
		TextInput(&id.Name)
		inheritHint(strings.TrimSpace(id.Name) == "", strings.TrimSpace(editApp.Name), "Empty = use shared name")
	})
	Container(Attrs(Gap(3)), func() {
		fieldLabel("App ID prefix (optional override)")
		TextInput(&id.AppIDPrefix)
		inheritHint(strings.TrimSpace(id.AppIDPrefix) == "", strings.TrimSpace(editApp.AppIDPrefix), "Empty = use shared prefix")
	})
	Container(Attrs(Gap(3)), func() {
		fieldLabel("App ID (optional override)")
		TextInput(&id.AppID)
		inheritHint(strings.TrimSpace(id.AppID) == "", editApp.effectiveBundleID(), "Empty = use shared App ID")
	})
	Container(Attrs(Gap(3)), func() {
		fieldLabel("Icon (optional override)")
		DirectoryBrowseExt(&id.IconPath, FileBrowserAttrs{
			Files: true, Dirs: false, Title: "Choose platform icon",
			Width: 560, Start: iconBrowseStart(pkgDir, id.IconPath),
			Exts: []string{".png", ".jpg", ".jpeg", ".webp", ".gif"},
		})
		inheritHint(strings.TrimSpace(id.IconPath) == "", strings.TrimSpace(editApp.IconPath), "Empty = use shared icon")
	})
}

func trimPlatformIdentity(id *PlatformIdentity, pkgDir string) {
	if id == nil {
		return
	}
	id.Name = strings.TrimSpace(id.Name)
	id.AppIDPrefix = strings.TrimSpace(id.AppIDPrefix)
	id.AppID = strings.TrimSpace(id.AppID)
	id.IconPath = relativizeIconPath(pkgDir, id.IconPath)
}

func doSaveEdit() {
	pkgDir := resolvePackagePath(wd, editApp.Package)
	editApp.IconPath = relativizeIconPath(pkgDir, editApp.IconPath)
	editApp.Package = strings.TrimSpace(editApp.Package)
	editApp.Name = strings.TrimSpace(editApp.Name)
	editApp.AppIDPrefix = strings.TrimSpace(editApp.AppIDPrefix)
	editApp.AppID = strings.TrimSpace(editApp.AppID)
	if editApp.IOS != nil {
		trimPlatformIdentity(&editApp.IOS.PlatformIdentity, pkgDir)
		editApp.IOS.TeamID = strings.TrimSpace(editApp.IOS.TeamID)
		editApp.IOS.Identity = strings.TrimSpace(editApp.IOS.Identity)
		editApp.IOS.Method = strings.TrimSpace(editApp.IOS.Method)
		editApp.IOS.ReleaseDir = strings.TrimSpace(editApp.IOS.ReleaseDir)
		if editApp.IOS.Method == "" {
			editApp.IOS.Method = "debugging"
		}
	}
	if editApp.Android != nil {
		trimPlatformIdentity(&editApp.Android.PlatformIdentity, pkgDir)
		editApp.Android.Keystore = strings.TrimSpace(editApp.Android.Keystore)
		editApp.Android.KeyAlias = strings.TrimSpace(editApp.Android.KeyAlias)
		editApp.Android.ReleaseDir = strings.TrimSpace(editApp.Android.ReleaseDir)
		editApp.Android.Arch = strings.TrimSpace(editApp.Android.Arch)
		if editApp.Android.Arch == "" {
			editApp.Android.Arch = "arm64"
		}
	}
	if editApp.MacOS != nil {
		trimPlatformIdentity(&editApp.MacOS.PlatformIdentity, pkgDir)
		editApp.MacOS.Identity = strings.TrimSpace(editApp.MacOS.Identity)
		editApp.MacOS.AppStoreIdentity = strings.TrimSpace(editApp.MacOS.AppStoreIdentity)
		editApp.MacOS.InstallerIdentity = strings.TrimSpace(editApp.MacOS.InstallerIdentity)
		editApp.MacOS.ProvisionProfile = strings.TrimSpace(editApp.MacOS.ProvisionProfile)
		editApp.MacOS.Category = strings.TrimSpace(editApp.MacOS.Category)
		editApp.MacOS.NotaryProfile = strings.TrimSpace(editApp.MacOS.NotaryProfile)
		editApp.MacOS.ReleaseDir = strings.TrimSpace(editApp.MacOS.ReleaseDir)
		editApp.MacOS.Arch = "" // legacy field; archs live in ArchARM64/ArchAMD64
		normalizeMacOSConfig(editApp.MacOS)
	}
	if editApp.Linux != nil {
		trimPlatformIdentity(&editApp.Linux.PlatformIdentity, pkgDir)
		editApp.Linux.ReleaseDir = strings.TrimSpace(editApp.Linux.ReleaseDir)
		normalizeLinuxConfig(editApp.Linux)
	}
	if editApp.Windows != nil {
		trimPlatformIdentity(&editApp.Windows.PlatformIdentity, pkgDir)
		editApp.Windows.ReleaseDir = strings.TrimSpace(editApp.Windows.ReleaseDir)
		normalizeWindowsConfig(editApp.Windows)
	}
	if editIsNew {
		store.Apps = append(store.Apps, editApp)
		editIndex = len(store.Apps) - 1
		editIsNew = false
	} else if editIndex >= 0 && editIndex < len(store.Apps) {
		store.Apps[editIndex] = editApp
	}
	if err := saveStore(store); err != nil {
		saveErr = err.Error()
		return
	}
	saveErr = ""
	refreshMainSnaps()
	if editIndex >= 0 && editIndex < len(store.Apps) {
		openAppScreen(editIndex)
	} else {
		screen = screenMain
	}
}

func iconBrowseStart(pkgDir, iconPath string) string {
	iconPath = strings.TrimSpace(iconPath)
	if iconPath == "" {
		if pkgDir != "" {
			return pkgDir
		}
		return wd
	}
	abs := resolveIconPath(pkgDir, iconPath)
	if st, err := os.Stat(abs); err == nil {
		if st.IsDir() {
			return abs
		}
		return filepath.Dir(abs)
	}
	if pkgDir != "" {
		return pkgDir
	}
	return wd
}

func releaseDirStart(dir string) string {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return wd
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(wd, dir)
	}
	if st, err := os.Stat(dir); err == nil && st.IsDir() {
		return dir
	}
	return wd
}

// --- Releases: version list + version detail ---

func openHistoryScreen(i int) {
	historyIndex = i
	actionErr = ""
	releaseLog = loadReleaseLog()
	screen = screenHistory
}

func orDash(s string) string {
	if strings.TrimSpace(s) == "" {
		return "—"
	}
	return s
}

// viewApp is the primary hub: open version + platform rows.
func viewApp() {
	if historyIndex < 0 || historyIndex >= len(store.Apps) {
		screen = screenMain
		return
	}
	a := store.Apps[historyIndex]
	ver := strings.TrimSpace(detailVersion)
	// Keep open version in sync if log reloaded.
	if ver == "" {
		ver = openVersionFor(a.ID)
		detailVersion = ver
	}

	Container(Attrs(Pad2(12, 16), Gap(10), Grow(1), Expand), func() {
		header(a.displayName(), a.Package)

		// Confirm re-bundle (delete existing then build / password prompt).
		if confirmDeletePlat != "" && confirmDeleteVer == ver && ver != "" {
			Modal(420, func() {
				confirmDeletePlat = ""
				confirmDeleteVer = ""
			}, func() {
				Label("Re-bundle "+platformDisplayName(confirmDeletePlat)+"?",
					FontSize(16), FontWeight(WeightBold))
				Label(fmt.Sprintf("Removes the existing v%s build for this platform, then starts a new bundle.", ver),
					FontSize(13), TextColor(0, 0, 35, 1))
				Container(Attrs(Row, Gap(10), CrossMid), func() {
					if ButtonExt("Cancel", ButtonAttrs{}, DefaultButtonLook()) {
						confirmDeletePlat = ""
						confirmDeleteVer = ""
					}
					if ButtonExt("Re-bundle", ButtonAttrs{Accent: AccentMeadow}, DefaultButtonLook()) {
						plat := confirmDeletePlat
						doDeletePlatformBuild(a.ID, ver, plat)
						confirmDeletePlat = ""
						confirmDeleteVer = ""
						startPlatformBundle(historyIndex, plat)
					}
				})
			})
		}

		// Android keystore passwords (only when bundling).
		if showAndroidPassModal {
			Modal(420, func() {
				showAndroidPassModal = false
				androidPassErr = ""
			}, func() {
				Label("Android keystore", FontSize(16), FontWeight(WeightBold))
				Label("Passwords are not saved — enter them to sign this bundle.",
					FontSize(13), TextColor(0, 0, 35, 1))
				Container(Attrs(Gap(3), Expand), func() {
					fieldLabel("Keystore password")
					PasswordInput(&androidStorePass)
				})
				Container(Attrs(Gap(3), Expand), func() {
					fieldLabel("Key password (optional)")
					PasswordInput(&androidKeyPass)
				})
				if androidPassErr != "" {
					Label(androidPassErr, FontSize(12), TextColor(10, 70, 40, 1))
				}
				Container(Attrs(Row, Gap(10), CrossMid), func() {
					if ButtonExt("Cancel", ButtonAttrs{}, DefaultButtonLook()) {
						showAndroidPassModal = false
						androidPassErr = ""
					}
					can := strings.TrimSpace(androidStorePass) != "" && !jobs.isBusy(platformAndroid)
					if ButtonExt("Bundle", ButtonAttrs{Accent: AccentMeadow, Disabled: !can}, DefaultButtonLook()) {
						androidPassErr = ""
						startAndroidBundle(historyIndex)
						if actionErr != "" {
							// Keep modal open on validation failure.
							androidPassErr = actionErr
							actionErr = ""
						} else {
							showAndroidPassModal = false
						}
					}
				})
			})
		}

		Container(Attrs(Row, CrossMid, Gap(8), Wrap), func() {
			if ButtonExt("← Apps", ButtonAttrs{}, DefaultButtonLook()) {
				if jobs.anyBusy() {
					actionErr = "Cancel running jobs before leaving"
				} else {
					actionErr = ""
					refreshMainSnaps()
					screen = screenMain
				}
			}
			if ButtonExt("Basic config", ButtonAttrs{}, DefaultButtonLook()) {
				startBasicEdit(historyIndex)
			}
			if ButtonExt("History", ButtonAttrs{}, DefaultButtonLook()) {
				openHistoryScreen(historyIndex)
			}
			Filler(1)
			if a.Android != nil {
				devices, devsReady, devsErr := ensureADBDevices()
				devLabel := "No Android device"
				if !devsReady {
					devLabel = "Scanning adb…"
				} else if devsErr != nil {
					devLabel = "adb error"
				} else if len(devices) == 0 {
					installSerial = ""
				} else {
					syncInstallSerial(devices)
					devLabel = "Select device…"
					for _, d := range devices {
						if d.Serial == installSerial {
							devLabel = d.Display()
							break
						}
					}
				}
				MenuButton(MenuIcon, devLabel, func() {
					if len(devices) == 0 {
						MenuItem(NoIcon, "(none — Refresh after plugging in)")
						return
					}
					for _, d := range devices {
						d := d
						if MenuItem(NoIcon, d.Display()) {
							installSerial = d.Serial
						}
					}
				})
				if CtrlButton(SymRefresh, "", true) {
					rescanADBDevices()
				}
			}
		})

		// Current version strip
		Container(Attrs(Pad(14), Gap(8), Expand,
			Background(0, 0, 100, 1), BorderWidth(1), BorderColor(0, 0, 82, 1), Corners(8)), func() {
			Container(Attrs(Row, CrossMid, Gap(10), Expand), func() {
				if ver != "" {
					Label("Current version", FontSize(12), TextColor(0, 0, 45, 1))
					Label("v"+ver, FontSize(18), FontWeight(WeightBold))
					Label("Open", FontSize(11), TextColor(220, 55, 40, 1))
				} else {
					Label("No open version", FontSize(16), FontWeight(WeightBold))
					Label("Create a version to start bundling", FontSize(12), TextColor(0, 0, 45, 1))
				}
				Filler(1)
				if !showNewVersion {
					if ButtonExt("New version", ButtonAttrs{Accent: AccentMeadow, Disabled: jobs.anyBusy()}, DefaultButtonLook()) {
						showNewVersion = true
						if ver != "" {
							newVersionDraft = bumpPatchVersion(ver)
						} else {
							newVersionDraft = defaultReleaseVersion(releaseLog.lastVersion(a.ID))
						}
					}
				}
			})
			if showNewVersion {
				Container(Attrs(Row, CrossMid, Gap(10), Expand), func() {
					Container(Attrs(Gap(3), Expand), func() {
						fieldLabel("Version")
						TextInput(&newVersionDraft)
					})
					if ButtonExt("Create", ButtonAttrs{Accent: AccentMeadow, Disabled: jobs.anyBusy()}, DefaultButtonLook()) {
						createNewVersion(a)
					}
					if ButtonExt("Cancel", ButtonAttrs{}, DefaultButtonLook()) {
						showNewVersion = false
						actionErr = ""
					}
				})
				if ver != "" {
					Label("Creating a new version freezes v"+ver+" (no further builds).",
						FontSize(10), TextColor(0, 0, 50, 1))
				}
			}
		})

		if actionErr != "" {
			Label(actionErr, FontSize(12), TextColor(10, 70, 40, 1))
		}
		if installMsg != "" {
			Label(installMsg, FontSize(12), TextColor(0, 0, 35, 1))
		}

		Label("Platforms", FontSize(14), FontWeight(WeightBold))
		Container(Attrs(Viewport), func() {
			ScrollOnInput()
			Container(Attrs(Gap(8), Expand), func() {
				appPlatformRow(a, platformIOS, ver)
				appPlatformRow(a, platformAndroid, ver)
				appPlatformRow(a, platformMacOS, ver)
				appPlatformRow(a, platformLinux, ver)
				appPlatformRow(a, platformWindows, ver)
			})
			ScrollBars()
		})
	})
}

func createNewVersion(a App) {
	v := strings.TrimSpace(newVersionDraft)
	if v == "" {
		actionErr = "version is required"
		return
	}
	if err := versionFormatOK(v); err != nil {
		actionErr = err.Error()
		return
	}
	// Freeze current open version if any.
	if cur := openVersionFor(a.ID); cur != "" && cur != v {
		releaseLog.markVersionReleased(a.ID, cur)
	}
	releaseLog.setOpenVersion(a.ID, v)
	if err := saveReleaseLog(releaseLog); err != nil {
		actionErr = "save: " + err.Error()
		return
	}
	detailVersion = v
	showNewVersion = false
	actionErr = ""
	installMsg = "v" + v + " is now open"
	newVersionDraft = bumpPatchVersion(v)
	refreshVersionSnaps()
}

// appPlatformRow is one platform on the app page (enabled or Enable CTA).
func appPlatformRow(a App, platform, version string) {
	var icon IconGlyph
	var name string
	var enabled bool
	switch platform {
	case platformIOS:
		icon, name, enabled = TypVendorApple, "iOS", a.IOS != nil
	case platformAndroid:
		icon, name, enabled = TypVendorAndroid, "Android", a.Android != nil
	case platformMacOS:
		icon, name, enabled = TypDeviceDesktop, "macOS", a.MacOS != nil
	case platformLinux:
		icon, name, enabled = TypDeviceLaptop, "Linux", a.Linux != nil
	case platformWindows:
		icon, name, enabled = TypVendorMicrosoft, "Windows", a.Windows != nil
	default:
		return
	}

	Container(Attrs(Pad(14), Gap(6), Expand,
		Background(0, 0, 100, 1), BorderWidth(1), BorderColor(0, 0, 82, 1), Corners(8)), func() {
		if !enabled {
			Container(Attrs(Row, CrossMid, Gap(10), Expand), func() {
				Icon(icon, FontSize(18))
				Label(name, FontSize(16), FontWeight(WeightBold))
				Label("not configured", FontSize(12), TextColor(0, 0, 50, 1))
				Filler(1)
				if ButtonExt("Enable", ButtonAttrs{Accent: AccentMeadow}, DefaultButtonLook()) {
					startPlatformEdit(historyIndex, platform)
				}
			})
			return
		}

		// Enabled: metadata + control buttons
		snap, ok := versionSnaps[platform]
		if !ok && version != "" {
			refreshVersionSnaps()
			snap = versionSnaps[platform]
		}
		lastVer, lastBuild := releaseLog.lastForPlatform(a.ID, platform)
		when := ""
		if snap.Has {
			if t, err := time.Parse(time.RFC3339, snap.Entry.At); err == nil {
				when = t.Local().Format("2006-01-02 15:04")
			} else {
				when = snap.Entry.At
			}
		}

		mutable := version != "" && releaseLog.versionIsMutable(a.ID, version)
		thisBusy := jobs.isBusy(platform) || (platform == platformMacOS && jobs.isBusy("macos-notarize"))
		has := snap.Has
		exists := snap.Exists

		// Left: metadata. Filler pushes the action column to the right edge.
		// (Expand is cross-axis only — need Filler/Grow on the main axis.)
		Container(Attrs(Row, CrossAlign(AlignStart), Gap(16), Expand), func() {
			Container(Attrs(Gap(3)), func() {
				Container(Attrs(Row, CrossMid, Gap(8)), func() {
					Icon(icon, FontSize(18))
					Label(name, FontSize(16), FontWeight(WeightBold))
					if version == "" {
						Label("no open version", FontSize(11), TextColor(0, 0, 55, 1))
					} else if snap.Has && snap.Exists {
						Label("Built", FontSize(11), TextColor(140, 50, 35, 1))
						if snap.Entry.Notarized {
							Label("Notarized", FontSize(11), TextColor(140, 50, 35, 1))
						}
					} else if snap.Has && !snap.Exists {
						Label("file missing", FontSize(11), TextColor(10, 60, 40, 1))
					} else {
						Label("Not built", FontSize(11), TextColor(0, 0, 55, 1))
					}
				})
				if version != "" && snap.Has {
					Label(fmt.Sprintf("v%s · build %s", version, orDash(snap.Entry.Build)),
						FontSize(12), TextColor(0, 0, 40, 1))
					if when != "" {
						Label(when, FontSize(11), TextColor(0, 0, 55, 1))
					}
				} else {
					Label(fmt.Sprintf("Last: v%s · build %s", orDash(lastVer), orDash(lastBuild)),
						FontSize(12), TextColor(0, 0, 45, 1))
				}
				Label(a.platformBundleID(platform), FontSize(11), TextColor(0, 0, 50, 1))
			})

			Filler(1)

			// Action column (right edge): top = ctrl row, then Bundle below
			Container(Attrs(Gap(10), CrossAlign(AlignEnd)), func() {
				Container(Attrs(Row, CrossMid, Gap(6)), func() {
					// Open the platform output folder (all archs / formats live there).
					folder := ""
					if has {
						folder = artifactFolderFor(snap.Entry)
					}
					if CtrlButton(SymFolder, "Folder", folder != "" && !thisBusy) {
						if err := revealInFileManager(folder); err != nil {
							installMsg = "reveal: " + err.Error()
						}
					}
					if CtrlButton(SymEdit, "Edit", !thisBusy) {
						startPlatformEdit(historyIndex, platform)
					}
					if platform == platformMacOS && has && exists && mutable {
						zipPath, _ := macOSArtifacts(snap.Entry)
						profile := ""
						if a.MacOS != nil {
							profile = strings.TrimSpace(a.MacOS.NotaryProfile)
						}
						canNotary := zipPath != "" && snap.PathOK[zipPath] && profile != "" &&
							!snap.Entry.Notarized && !thisBusy
						if CtrlButton(TypCloudStorage, "Notarize", canNotary) {
							startMacOSNotarize(historyIndex, snap.Entry)
						}
					}
					if platform == platformAndroid && has && exists {
						if CtrlButton(SymPlay, "Install", !installBusy && installSerial != "" && !thisBusy) {
							startInstallAPK(a, snap.Entry)
						}
					}
				})

				const bundleTextSize float32 = 15
				if version == "" {
					// need an open version first
				} else if !mutable {
					Label("frozen", FontSize(11), TextColor(0, 0, 50, 1))
				} else if has {
					if ButtonExt("Re-bundle", ButtonAttrs{
						TextSize: bundleTextSize,
						Disabled: thisBusy,
					}, DefaultButtonLook()) {
						confirmDeletePlat = platform
						confirmDeleteVer = version
					}
				} else {
					can := len(snap.Issues) == 0 && versionFormatOK(version) == nil && !thisBusy
					if ButtonExt("Bundle", ButtonAttrs{
						Accent:   AccentMeadow,
						TextSize: bundleTextSize,
						Disabled: !can || thisBusy,
					}, DefaultButtonLook()) {
						startPlatformBundle(historyIndex, platform)
					}
				}
			})
		})

		// Job progress lives inside this platform panel.
		platformJobChrome(platform)

		// All artifacts for this platform build (primary + extras).
		if has {
			paths := releaseEntryPaths(snap.Entry)
			if len(paths) > 0 {
				Container(Attrs(Gap(4), Expand), func() {
					Label("Artifacts", FontSize(11), FontWeight(WeightBold), TextColor(0, 0, 45, 1))
					for _, p := range paths {
						p := p
						ok := snap.PathOK == nil || snap.PathOK[p]
						Container(Attrs(Row, CrossMid, Gap(8), Expand), func() {
							name := filepath.Base(p)
							col := TextColor(0, 0, 35, 1)
							if !ok {
								col = TextColor(10, 55, 35, 1)
								name = name + " (missing)"
							}
							Label(name, FontSize(12), col)
							Filler(1)
							if CtrlButton(SymFolder, "Reveal", ok && !thisBusy) {
								if err := revealInFileManager(p); err != nil {
									installMsg = "reveal: " + err.Error()
								}
							}
						})
					}
				})
			}
		}

		if !snap.Has && version != "" && releaseLog.versionIsMutable(a.ID, version) {
			for _, is := range snap.Issues {
				Label("• "+is.Message, FontSize(11), TextColor(10, 55, 30, 1))
			}
		}
	})
}

func startPlatformBundle(appIndex int, platform string) {
	switch platform {
	case platformIOS:
		startIOSBundle(appIndex)
	case platformAndroid:
		// Passwords via modal; do not start until the user confirms.
		androidPassErr = ""
		showAndroidPassModal = true
	case platformMacOS:
		startMacOSBundle(appIndex)
	case platformLinux:
		startLinuxBundle(appIndex)
	case platformWindows:
		startWindowsBundle(appIndex)
	}
}

// viewHistory is a secondary archive of marketing versions.
func viewHistory() {
	if historyIndex < 0 || historyIndex >= len(store.Apps) {
		screen = screenMain
		return
	}
	a := store.Apps[historyIndex]
	versions := releaseLog.versionList(a.ID)

	Container(Attrs(Pad2(12, 16), Gap(10), Grow(1), Expand), func() {
		header(a.displayName(), "History")

		Container(Attrs(Row, CrossMid, Gap(8), Wrap), func() {
			if ButtonExt("← App", ButtonAttrs{}, DefaultButtonLook()) {
				openAppScreen(historyIndex)
			}
		})
		if actionErr != "" {
			Label(actionErr, FontSize(12), TextColor(10, 70, 40, 1))
		}

		if len(versions) == 0 {
			Container(Attrs(Pad(24), Center, Expand), func() {
				Label("No version history yet.", FontSize(13), TextColor(0, 0, 50, 1))
			})
			return
		}

		Container(Attrs(Viewport), func() {
			ScrollOnInput()
			Container(Attrs(Gap(8), Expand), func() {
				for _, vr := range versions {
					vr := vr
					historyVersionRow(a, vr)
				}
			})
			ScrollBars()
		})
	})
}

func historyVersionRow(a App, vr VersionRelease) {
	when := vr.At
	if t, err := time.Parse(time.RFC3339, vr.At); err == nil {
		when = t.Local().Format("2006-01-02 15:04")
	}
	released := releaseLog.isVersionReleased(a.ID, vr.Version)
	open := openVersionFor(a.ID) == vr.Version
	status := ""
	if released {
		status = "Released"
	} else if open {
		status = "Open"
	}

	Container(Attrs(Expand, Pad(14), Gap(6),
		Background(0, 0, 100, 1), BorderWidth(1), BorderColor(0, 0, 82, 1), Corners(8)), func() {
		Container(Attrs(Row, CrossMid, Gap(8)), func() {
			Label("v"+vr.Version, FontSize(16), FontWeight(WeightBold))
			if status != "" {
				col := TextColor(0, 0, 45, 1)
				if open {
					col = TextColor(220, 55, 40, 1)
				} else if released {
					col = TextColor(140, 50, 35, 1)
				}
				Label(status, FontSize(11), col)
			}
			Filler(1)
			if when != "" {
				Label(when, FontSize(11), TextColor(0, 0, 55, 1))
			}
		})
		// Placeholder: per-platform artifact reveal when present.
		Container(Attrs(Row, CrossMid, Gap(12), Wrap), func() {
			for _, plat := range []string{platformIOS, platformAndroid, platformMacOS, platformLinux, platformWindows} {
				e, ok := vr.Platforms[plat]
				if !ok {
					continue
				}
				label := platformDisplayName(plat) + " build " + orDash(e.Build)
				if CtrlButton(NoIcon, label, e.Path != "") {
					_ = revealInFileManager(e.Path)
				}
			}
		})
	})
}

// platformJobKeys returns job hub keys that belong in this platform panel.
func platformJobKeys(platform string) []string {
	if platform == platformMacOS {
		return []string{platformMacOS, "macos-notarize"}
	}
	return []string{platform}
}

// platformJobChrome paints progress for jobs owned by this OS panel (if any).
func platformJobChrome(platform string) {
	for _, key := range platformJobKeys(platform) {
		key := key
		prog := jobs.get(key)
		steps, status, result, errMsg, busy := prog.snapshot()
		if !busy && result == "" && errMsg == "" {
			continue
		}
		Container(Attrs(Pad(10), Gap(6), Expand,
			Background(220, 10, 97, 1), BorderWidth(1), BorderColor(0, 0, 88, 1), Corners(6)), func() {
			Container(Attrs(Row, CrossMid, Gap(8), Expand), func() {
				title := jobTitle(key)
				if busy {
					Label(title+"…", FontSize(12), FontWeight(WeightBold))
				} else if errMsg != "" {
					Label(title+" failed", FontSize(12), FontWeight(WeightBold), TextColor(10, 70, 40, 1))
				} else {
					Label(title+" done", FontSize(12), FontWeight(WeightBold), TextColor(140, 50, 30, 1))
				}
				Filler(1)
				if busy {
					if ButtonExt("Cancel", ButtonAttrs{}, DefaultButtonLook()) {
						prog.RequestCancel()
						prog.appendfOnFrame("— cancel requested —")
					}
				} else {
					if ButtonExt("Dismiss", ButtonAttrs{}, DefaultButtonLook()) {
						prog.clearChrome()
						*jobLogOpen(key) = false
					}
				}
				logOpen := jobLogOpen(key)
				if ButtonExt("Show log", ButtonAttrs{}, DefaultButtonLook()) {
					*logOpen = !*logOpen
				}
				PopupPanel(logOpen, GetLastId(), Attrs(Pad(10), Gap(6),
					Background(0, 0, 100, 1), BorderWidth(1), BorderColor(0, 0, 80, 1),
					Corners(6), FixSize(520, 320)), func() {
					ring := prog.Ring()
					if ring == nil {
						Label("No log yet.", FontSize(12), TextColor(0, 0, 55, 1))
						return
					}
					Container(Attrs(Viewport, Expand, Grow(1)), func() {
						attrs := DefaultTextStyle()
						attrs.FontSize = 11
						attrs.FontFamilies = Monospace
						attrs.TextColor = Vec4{0, 0, 15, 1}
						LogView(ring, attrs)
					})
				})
			})
			doneN := 0
			activeName := ""
			for i, st := range status {
				if st == stepDone {
					doneN++
				}
				if st == stepActive && i < len(steps) {
					activeName = steps[i]
				}
			}
			total := len(steps)
			if total == 0 {
				total = 1
			}
			frac := float32(doneN) / float32(total)
			if busy && doneN < total {
				frac = (float32(doneN) + 0.35) / float32(total)
			}
			if !busy && result != "" {
				frac = 1
			}
			progressBar(frac)
			if busy && activeName != "" {
				Label(activeName, FontSize(11), TextColor(0, 0, 40, 1))
			} else if errMsg != "" {
				Label(errMsg, FontSize(11), TextColor(10, 60, 30, 1))
			} else if result != "" {
				Label(result, FontSize(11), TextColor(0, 0, 35, 1))
			}
		})
	}
}

func platformDisplayName(platform string) string {
	switch strings.ToLower(platform) {
	case platformIOS:
		return "iOS"
	case platformAndroid:
		return "Android"
	case platformMacOS:
		return "macOS"
	case platformLinux:
		return "Linux"
	case platformWindows:
		return "Windows"
	default:
		return platform
	}
}

func doDeletePlatformBuild(appID, version, platform string) {
	n, paths := releaseLog.deletePlatformBuild(appID, version, platform)
	for _, p := range paths {
		// Best-effort: remove files and .app bundles.
		_ = os.RemoveAll(p)
	}
	if err := saveReleaseLog(releaseLog); err != nil {
		actionErr = "save after delete: " + err.Error()
		return
	}
	if n == 0 {
		installMsg = "No recorded build to delete"
	} else {
		installMsg = fmt.Sprintf("Deleted %s build for v%s (%d record(s))",
			platformDisplayName(platform), version, n)
	}
	actionErr = ""
	refreshVersionSnaps()
}

func platformVersionCard(a App, platform, version string) {
	snap, ok := versionSnaps[platform]
	if !ok {
		// Page opened without snap (shouldn't happen); fill once.
		refreshVersionSnaps()
		snap = versionSnaps[platform]
	}
	entry, has, exists := snap.Entry, snap.Has, snap.Exists
	mutable := snap.Mutable

	var icon IconGlyph
	var name string
	switch platform {
	case platformIOS:
		icon, name = TypVendorApple, "iOS"
	case platformAndroid:
		icon, name = TypVendorAndroid, "Android"
	case platformMacOS:
		icon, name = TypDeviceDesktop, "macOS"
	default:
		icon, name = TypDeviceDesktop, platform
	}

	lastVer, lastBuild, nextBuild := snap.LastVer, snap.LastBuild, snap.NextBuild
	thisBusy := jobs.isBusy(platform) || (platform == platformMacOS && jobs.isBusy("macos-notarize"))

	Container(Attrs(Pad(14), Gap(8), Expand,
		Background(0, 0, 100, 1), BorderWidth(1), BorderColor(0, 0, 82, 1), Corners(8)), func() {
		Container(Attrs(Row, CrossMid, Gap(10)), func() {
			Icon(icon, FontSize(18))
			Label(name, FontSize(16), FontWeight(WeightBold))
			Filler(1)
			if has && exists {
				Label("Built", FontSize(11), TextColor(140, 50, 35, 1))
			} else if has && !exists {
				Label("file missing", FontSize(11), TextColor(10, 60, 40, 1))
			} else {
				Label("Not built", FontSize(11), TextColor(0, 0, 55, 1))
			}
		})

		// Config one-liner
		switch platform {
		case platformIOS:
			if a.IOS != nil {
				Label(fmt.Sprintf("Team %s · %s", orDash(a.IOS.TeamID), orDash(a.IOS.Method)),
					FontSize(11), TextColor(0, 0, 45, 1))
			}
		case platformAndroid:
			if a.Android != nil {
				Label(fmt.Sprintf("Keystore %s · %s",
					orDash(filepath.Base(a.Android.Keystore)), orDash(a.Android.Arch)),
					FontSize(11), TextColor(0, 0, 45, 1))
			}
		case platformMacOS:
			if a.MacOS != nil {
				normalizeMacOSConfig(a.MacOS)
				outs := []string{}
				if a.MacOS.SelfDist {
					outs = append(outs, "self-dist")
				}
				if a.MacOS.AppStore {
					outs = append(outs, "app-store")
				}
				Label(fmt.Sprintf("%s · %s", a.MacOS.macOSArchLabel(), strings.Join(outs, "+")),
					FontSize(11), TextColor(0, 0, 45, 1))
			}
		}

		if has {
			Label(fmt.Sprintf("build %s", orDash(entry.Build)), FontSize(11), TextColor(0, 0, 45, 1))
			for _, p := range append([]string{entry.Path}, entry.Extra...) {
				if p == "" {
					continue
				}
				col := TextColor(0, 0, 45, 1)
				if snap.PathOK != nil && !snap.PathOK[p] {
					col = TextColor(10, 50, 40, 1)
				}
				Label(p, FontSize(11), col)
			}
			if entry.Notarized {
				Label("Notarized", FontSize(11), TextColor(140, 50, 35, 1))
			}
		} else {
			Label(fmt.Sprintf("Last for this platform: v%s · build %s → next build %s",
				orDash(lastVer), orDash(lastBuild), nextBuild),
				FontSize(11), TextColor(0, 0, 45, 1))
		}

		// Validation + build affordances when not yet built for this version
		if !has {
			if !mutable {
				Label("This version is frozen — open the latest Open version to build.",
					FontSize(11), TextColor(0, 0, 50, 1))
				return
			}
			for _, is := range snap.Issues {
				Label("• "+is.Message, FontSize(11), TextColor(10, 55, 30, 1))
			}
			if verErr := versionFormatOK(version); verErr != nil {
				Label("• "+verErr.Error(), FontSize(11), TextColor(10, 55, 30, 1))
			}
			if platform == platformAndroid {
				Container(Attrs(Gap(3)), func() {
					fieldLabel("Keystore password")
					PasswordInput(&androidStorePass)
				})
				Container(Attrs(Gap(3)), func() {
					fieldLabel("Key password (optional)")
					PasswordInput(&androidKeyPass)
				})
			}
			can := len(snap.Issues) == 0 &&
				versionFormatOK(version) == nil &&
				!thisBusy
			if platform == platformAndroid {
				can = can && strings.TrimSpace(androidStorePass) != ""
			}
			label := "Build " + name
			if ButtonExt(label, ButtonAttrs{Accent: AccentMeadow, Disabled: !can || thisBusy}, DefaultButtonLook()) {
				switch platform {
				case platformIOS:
					startIOSBundle(historyIndex)
				case platformAndroid:
					startAndroidBundle(historyIndex)
				case platformMacOS:
					startMacOSBundle(historyIndex)
				}
			}
			return
		}

		// Built: post-build actions
		Container(Attrs(Row, CrossMid, Gap(8), Wrap), func() {
			if exists {
				if ButtonExt(revealButtonLabel(), ButtonAttrs{}, DefaultButtonLook()) {
					if err := revealInFileManager(entry.Path); err != nil {
						installMsg = "reveal: " + err.Error()
					}
				}
			}
			if mutable {
				if ButtonExt("Delete build…", ButtonAttrs{Disabled: thisBusy}, DefaultButtonLook()) {
					confirmDeletePlat = platform
					confirmDeleteVer = version
				}
			}
			switch platform {
			case platformAndroid:
				if exists {
					if ButtonExt("Install", ButtonAttrs{
						Accent:   AccentMeadow,
						Disabled: installBusy || installSerial == "" || thisBusy,
					}, DefaultButtonLook()) {
						startInstallAPK(a, entry)
					}
				}
			case platformIOS:
				Label("Upload with Transporter / App Store Connect for TestFlight",
					FontSize(11), TextColor(0, 0, 50, 1))
			case platformMacOS:
				zipPath, pkgPath := macOSArtifacts(entry)
				if pkgPath != "" {
					Label("Upload .pkg with Transporter for Mac App Store",
						FontSize(11), TextColor(0, 0, 50, 1))
					if snap.PathOK[pkgPath] {
						if ButtonExt("Reveal pkg", ButtonAttrs{}, DefaultButtonLook()) {
							_ = revealInFileManager(pkgPath)
						}
					}
				}
				if zipPath != "" {
					if entry.Notarized {
						Label("Self-dist notarized", FontSize(11), TextColor(0, 0, 50, 1))
					} else if mutable {
						profile := ""
						if a.MacOS != nil {
							profile = strings.TrimSpace(a.MacOS.NotaryProfile)
						}
						canNotary := snap.PathOK[zipPath] && profile != "" && !thisBusy
						if ButtonExt("Notarize", ButtonAttrs{Accent: AccentMeadow, Disabled: !canNotary}, DefaultButtonLook()) {
							startMacOSNotarize(historyIndex, entry)
						}
						if profile == "" {
							Label("Set notary profile in Edit to enable Notarize (self-dist)",
								FontSize(10), TextColor(0, 0, 50, 1))
						}
					}
				}
			}
		})
	})
}

// macOSArtifacts picks zip/pkg paths from a release entry (primary + extras).
func macOSArtifacts(e ReleaseEntry) (zipPath, pkgPath string) {
	consider := func(p string) {
		switch strings.ToLower(filepath.Ext(p)) {
		case ".zip":
			if zipPath == "" {
				zipPath = p
			}
		case ".pkg":
			if pkgPath == "" {
				pkgPath = p
			}
		}
	}
	consider(e.Path)
	for _, p := range e.Extra {
		consider(p)
	}
	return
}

// syncInstallSerial keeps installSerial pointing at a live device when possible.
func syncInstallSerial(devices []ADBDevice) {
	if installSerial != "" {
		for _, d := range devices {
			if d.Serial == installSerial {
				return
			}
		}
	}
	installSerial = ""
	for _, d := range devices {
		if d.State == "device" {
			installSerial = d.Serial
			return
		}
	}
	if len(devices) > 0 {
		installSerial = devices[0].Serial
	}
}

func startInstallAPK(a App, e ReleaseEntry) {
	if installBusy {
		return
	}
	if !fileExists(e.Path) {
		installMsg = "APK file is missing"
		return
	}
	serial := installSerial
	pkgDir := resolvePackagePath(wd, a.Package)
	packageID := androidPackageName(a.platformBundleID(platformAndroid), a.platformAppIDPrefix(platformAndroid), filepath.Base(pkgDir))
	installBusy = true
	installMsg = "Installing…"
	path := e.Path
	go func() {
		var lines []string
		logf := func(format string, args ...any) {
			lines = append(lines, fmt.Sprintf(format, args...))
		}
		err := installAndroidAPK(path, packageID, serial, logf)
		// Apply result on frame lock path via simple assignment + RequestNextFrame
		// (installBusy is only set from UI + this goroutine).
		WithFrameLock(func() {
			installBusy = false
			if err != nil {
				installMsg = "Install failed: " + err.Error()
				if len(lines) > 0 {
					installMsg += "\n" + strings.Join(lines, "\n")
				}
			} else {
				installMsg = "Installed and launched on " + serial
			}
		})
		RequestNextFrame()
	}()
}

func startIOSBundle(appIndex int) {
	actionErr = ""
	if appIndex < 0 || appIndex >= len(store.Apps) {
		actionErr = "app not found"
		return
	}
	a := store.Apps[appIndex]
	if issues := append(validateAppShared(a, wd), validateIOSWithWD(a, wd)...); len(issues) > 0 {
		var b strings.Builder
		b.WriteString("Cannot bundle — fix configuration:\n")
		for _, is := range issues {
			b.WriteString("• ")
			b.WriteString(is.Message)
			b.WriteByte('\n')
		}
		actionErr = strings.TrimSpace(b.String())
		return
	}
	version := strings.TrimSpace(detailVersion)
	if version == "" {
		actionErr = "version is required"
		return
	}
	if !releaseLog.versionIsMutable(a.ID, version) {
		actionErr = "this version is frozen — only the latest Open version can be built"
		return
	}
	if _, exists := releaseLog.entryFor(a.ID, version, platformIOS); exists {
		actionErr = "iOS already built — Delete build first, then Build again"
		return
	}
	if err := versionFormatOK(version); err != nil {
		actionErr = err.Error()
		return
	}
	prog := jobs.tryBegin(platformIOS, iosBundleSteps)
	if prog == nil {
		actionErr = "iOS already bundling"
		return
	}

	_, lastBuild := releaseLog.lastForPlatform(a.ID, platformIOS)
	build := nextBuildNumber(lastBuild)
	pkgDir := resolvePackagePath(wd, a.Package)
	ios := a.IOS
	outDir := ios.ReleaseDir
	if !filepath.IsAbs(outDir) {
		outDir = filepath.Join(wd, outDir)
	}

	appID := a.ID
	opts := IOSBundleOpts{
		PkgDir:   pkgDir,
		TeamID:   ios.TeamID,
		Identity: ios.Identity,
		BundleID: a.platformBundleID(platformIOS),
		Name:     a.platformName(platformIOS),
		Version:  version,
		Build:    build,
		Method:   ios.Method,
		OutDir:   outDir,
		IconPath: a.resolvePlatformIcon(wd, platformIOS),
		Logf:     prog.appendf,
		OnStep:   prog.beginStep,
		Cancelled: func() bool {
			return prog.Cancelled()
		},
		SetRunningCmd: func(cmd *exec.Cmd) {
			if cmd == nil {
				prog.clearRunningCmd()
			} else {
				prog.setRunningCmd(cmd)
			}
		},
	}

	go func() {
		ipa, err := bundleIOS(opts)
		if err != nil {
			prog.failStep(activeFailIndexFor(prog), err)
			if prog.Cancelled() {
				prog.appendf("— cancelled —")
			} else {
				prog.appendf("— error: %v —", err)
			}
			return
		}
		if prog.Cancelled() {
			prog.failStep(len(iosBundleSteps)-1, fmt.Errorf("cancelled"))
			prog.appendf("— cancelled —")
			return
		}
		if err := recordAndSaveRelease(appID, platformIOS, version, build, ipa); err != nil {
			prog.appendf("warning: could not save release log: %v", err)
		}
		prog.finishOK(ipa)
		prog.appendf("— done: %s —", ipa)
		refreshVersionSnapsFromBackground()
	}()
}

func startAndroidBundle(appIndex int) {
	actionErr = ""
	if appIndex < 0 || appIndex >= len(store.Apps) {
		actionErr = "app not found"
		return
	}
	a := store.Apps[appIndex]
	if issues := append(validateAppShared(a, wd), validateAndroidWithWD(a, wd)...); len(issues) > 0 {
		var b strings.Builder
		b.WriteString("Cannot bundle — fix configuration:\n")
		for _, is := range issues {
			b.WriteString("• ")
			b.WriteString(is.Message)
			b.WriteByte('\n')
		}
		actionErr = strings.TrimSpace(b.String())
		return
	}
	version := strings.TrimSpace(detailVersion)
	if version == "" {
		actionErr = "version is required"
		return
	}
	if !releaseLog.versionIsMutable(a.ID, version) {
		actionErr = "this version is frozen — only the latest Open version can be built"
		return
	}
	if _, exists := releaseLog.entryFor(a.ID, version, platformAndroid); exists {
		actionErr = "Android already built — Delete build first, then Build again"
		return
	}
	if err := versionFormatOK(version); err != nil {
		actionErr = err.Error()
		return
	}
	storePass := strings.TrimSpace(androidStorePass)
	if storePass == "" {
		actionErr = "Keystore password is required"
		return
	}
	prog := jobs.tryBegin(platformAndroid, androidBundleSteps)
	if prog == nil {
		actionErr = "Android already bundling"
		return
	}

	_, lastBuild := releaseLog.lastForPlatform(a.ID, platformAndroid)
	build := nextBuildNumber(lastBuild)
	pkgDir := resolvePackagePath(wd, a.Package)
	and := a.Android
	outDir := and.ReleaseDir
	if !filepath.IsAbs(outDir) {
		outDir = filepath.Join(wd, outDir)
	}

	appID := a.ID
	// Android application ids must be valid reverse-DNS segments.
	appIDAndroid := androidPackageName(a.platformBundleID(platformAndroid), a.platformAppIDPrefix(platformAndroid), filepath.Base(pkgDir))

	opts := AndroidBundleOpts{
		PkgDir:    pkgDir,
		AppID:     appIDAndroid,
		Name:      a.platformName(platformAndroid),
		Version:   version,
		Build:     build,
		IconPath:  a.resolvePlatformIcon(wd, platformAndroid),
		Arch:      and.Arch,
		Keystore:  and.Keystore,
		KeyAlias:  and.KeyAlias,
		StorePass: storePass,
		KeyPass:   strings.TrimSpace(androidKeyPass),
		OutDir:    outDir,
		Logf:      prog.appendf,
		OnStep:    prog.beginStep,
		Cancelled: func() bool { return prog.Cancelled() },
		SetRunningCmd: func(cmd *exec.Cmd) {
			if cmd == nil {
				prog.clearRunningCmd()
			} else {
				prog.setRunningCmd(cmd)
			}
		},
	}

	go func() {
		apk, err := bundleAndroid(opts)
		if err != nil {
			prog.failStep(activeFailIndexFor(prog), err)
			if prog.Cancelled() {
				prog.appendf("— cancelled —")
			} else {
				prog.appendf("— error: %v —", err)
			}
			return
		}
		if prog.Cancelled() {
			prog.failStep(len(androidBundleSteps)-1, fmt.Errorf("cancelled"))
			prog.appendf("— cancelled —")
			return
		}
		if err := recordAndSaveRelease(appID, platformAndroid, version, build, apk); err != nil {
			prog.appendf("warning: could not save release log: %v", err)
		}
		// Clear passwords from memory after success.
		androidStorePass = ""
		androidKeyPass = ""
		prog.finishOK(apk)
		prog.appendf("— done: %s —", apk)
		refreshVersionSnapsFromBackground()
	}()
}

func startMacOSBundle(appIndex int) {
	actionErr = ""
	if appIndex < 0 || appIndex >= len(store.Apps) {
		actionErr = "app not found"
		return
	}
	a := store.Apps[appIndex]
	if issues := append(validateAppShared(a, wd), validateMacOSWithWD(a, wd)...); len(issues) > 0 {
		var b strings.Builder
		b.WriteString("Cannot bundle — fix configuration:\n")
		for _, is := range issues {
			b.WriteString("• ")
			b.WriteString(is.Message)
			b.WriteByte('\n')
		}
		actionErr = strings.TrimSpace(b.String())
		return
	}
	version := strings.TrimSpace(detailVersion)
	if version == "" {
		actionErr = "version is required"
		return
	}
	if !releaseLog.versionIsMutable(a.ID, version) {
		actionErr = "this version is frozen — only the latest Open version can be built"
		return
	}
	if _, exists := releaseLog.entryFor(a.ID, version, platformMacOS); exists {
		actionErr = "macOS already built — Delete build first, then Build again"
		return
	}
	if err := versionFormatOK(version); err != nil {
		actionErr = err.Error()
		return
	}
	if jobs.isBusy("macos-notarize") {
		actionErr = "macOS notarize in progress"
		return
	}
	mac := a.MacOS
	normalizeMacOSConfig(mac)
	steps := macosBundleSteps(mac.SelfDist, mac.AppStore)
	prog := jobs.tryBegin(platformMacOS, steps)
	if prog == nil {
		actionErr = "macOS already bundling"
		return
	}

	_, lastBuild := releaseLog.lastForPlatform(a.ID, platformMacOS)
	build := nextBuildNumber(lastBuild)
	pkgDir := resolvePackagePath(wd, a.Package)
	outDir := mac.ReleaseDir
	if !filepath.IsAbs(outDir) {
		outDir = filepath.Join(wd, outDir)
	}

	appID := a.ID
	opts := MacOSBundleOpts{
		PkgDir:            pkgDir,
		BundleID:          a.platformBundleID(platformMacOS),
		Name:              a.platformName(platformMacOS),
		Version:           version,
		Build:             build,
		IconPath:          a.resolvePlatformIcon(wd, platformMacOS),
		Archs:             mac.macOSArchs(),
		SelfDist:          mac.SelfDist,
		AppStore:          mac.AppStore,
		Identity:          mac.Identity,
		AppStoreIdentity:  mac.AppStoreIdentity,
		InstallerIdentity: mac.InstallerIdentity,
		ProvisionProfile:  mac.ProvisionProfile,
		Category:          mac.Category,
		ReleaseDir:        outDir,
		Logf:              prog.appendf,
		OnStep:            prog.beginStep,
		Cancelled:         func() bool { return prog.Cancelled() },
		SetRunningCmd: func(cmd *exec.Cmd) {
			if cmd == nil {
				prog.clearRunningCmd()
			} else {
				prog.setRunningCmd(cmd)
			}
		},
	}

	go func() {
		result, err := bundleMacOS(opts)
		if err != nil {
			prog.failStep(activeFailIndexFor(prog), err)
			if prog.Cancelled() {
				prog.appendf("— cancelled —")
			} else {
				prog.appendf("— error: %v —", err)
			}
			return
		}
		if prog.Cancelled() {
			prog.failStep(len(steps)-1, fmt.Errorf("cancelled"))
			prog.appendf("— cancelled —")
			return
		}
		primary := result.Primary()
		if err := recordAndSaveRelease(appID, platformMacOS, version, build, primary, result.Extra()...); err != nil {
			prog.appendf("warning: could not save release log: %v", err)
		}
		prog.finishOK(primary)
		prog.appendf("— done: %s —", primary)
		for _, e := range result.Extra() {
			prog.appendf("— also: %s —", e)
		}
		refreshVersionSnapsFromBackground()
	}()
}

func startLinuxBundle(appIndex int) {
	actionErr = ""
	if appIndex < 0 || appIndex >= len(store.Apps) {
		actionErr = "app not found"
		return
	}
	a := store.Apps[appIndex]
	if issues := append(validateAppShared(a, wd), validateLinuxWithWD(a, wd)...); len(issues) > 0 {
		var b strings.Builder
		b.WriteString("Cannot bundle — fix configuration:\n")
		for _, is := range issues {
			b.WriteString("• ")
			b.WriteString(is.Message)
			b.WriteByte('\n')
		}
		actionErr = strings.TrimSpace(b.String())
		return
	}
	version := strings.TrimSpace(detailVersion)
	if version == "" {
		actionErr = "version is required"
		return
	}
	if !releaseLog.versionIsMutable(a.ID, version) {
		actionErr = "this version is frozen — only the latest Open version can be built"
		return
	}
	if _, exists := releaseLog.entryFor(a.ID, version, platformLinux); exists {
		actionErr = "Linux already built — Re-bundle to replace"
		return
	}
	if err := versionFormatOK(version); err != nil {
		actionErr = err.Error()
		return
	}
	prog := jobs.tryBegin(platformLinux, linuxBundleSteps)
	if prog == nil {
		actionErr = "Linux already bundling"
		return
	}

	_, lastBuild := releaseLog.lastForPlatform(a.ID, platformLinux)
	build := nextBuildNumber(lastBuild)
	pkgDir := resolvePackagePath(wd, a.Package)
	lin := a.Linux
	normalizeLinuxConfig(lin)
	outDir := lin.ReleaseDir
	if !filepath.IsAbs(outDir) {
		outDir = filepath.Join(wd, outDir)
	}

	appID := a.ID
	opts := LinuxBundleOpts{
		PkgDir:     pkgDir,
		Name:       a.platformName(platformLinux),
		BundleID:   a.platformBundleID(platformLinux),
		Version:    version,
		Build:      build,
		IconPath:   a.resolvePlatformIcon(wd, platformLinux),
		Archs:      lin.linuxArchs(),
		ReleaseDir: outDir,
		Logf:       prog.appendf,
		OnStep:     prog.beginStep,
		Cancelled:  func() bool { return prog.Cancelled() },
		SetRunningCmd: func(cmd *exec.Cmd) {
			if cmd == nil {
				prog.clearRunningCmd()
			} else {
				prog.setRunningCmd(cmd)
			}
		},
	}

	go func() {
		primary, extra, err := bundleLinux(opts)
		if err != nil {
			prog.failStep(activeFailIndexFor(prog), err)
			if prog.Cancelled() {
				prog.appendf("— cancelled —")
			} else {
				prog.appendf("— error: %v —", err)
			}
			return
		}
		if prog.Cancelled() {
			prog.failStep(len(linuxBundleSteps)-1, fmt.Errorf("cancelled"))
			prog.appendf("— cancelled —")
			return
		}
		if err := recordAndSaveRelease(appID, platformLinux, version, build, primary, extra...); err != nil {
			prog.appendf("warning: could not save release log: %v", err)
		}
		prog.finishOK(primary)
		prog.appendf("— done: %s —", primary)
		for _, e := range extra {
			prog.appendf("— also: %s —", e)
		}
		refreshVersionSnapsFromBackground()
	}()
}

func startWindowsBundle(appIndex int) {
	actionErr = ""
	if appIndex < 0 || appIndex >= len(store.Apps) {
		actionErr = "app not found"
		return
	}
	a := store.Apps[appIndex]
	if issues := append(validateAppShared(a, wd), validateWindowsWithWD(a, wd)...); len(issues) > 0 {
		var b strings.Builder
		b.WriteString("Cannot bundle — fix configuration:\n")
		for _, is := range issues {
			b.WriteString("• ")
			b.WriteString(is.Message)
			b.WriteByte('\n')
		}
		actionErr = strings.TrimSpace(b.String())
		return
	}
	version := strings.TrimSpace(detailVersion)
	if version == "" {
		actionErr = "version is required"
		return
	}
	if !releaseLog.versionIsMutable(a.ID, version) {
		actionErr = "this version is frozen — only the latest Open version can be built"
		return
	}
	if _, exists := releaseLog.entryFor(a.ID, version, platformWindows); exists {
		actionErr = "Windows already built — Re-bundle to replace"
		return
	}
	if err := versionFormatOK(version); err != nil {
		actionErr = err.Error()
		return
	}
	prog := jobs.tryBegin(platformWindows, windowsBundleSteps)
	if prog == nil {
		actionErr = "Windows already bundling"
		return
	}

	_, lastBuild := releaseLog.lastForPlatform(a.ID, platformWindows)
	build := nextBuildNumber(lastBuild)
	pkgDir := resolvePackagePath(wd, a.Package)
	win := a.Windows
	normalizeWindowsConfig(win)
	outDir := win.ReleaseDir
	if !filepath.IsAbs(outDir) {
		outDir = filepath.Join(wd, outDir)
	}

	appID := a.ID
	opts := WindowsBundleOpts{
		PkgDir:     pkgDir,
		Name:       a.platformName(platformWindows),
		BundleID:   a.platformBundleID(platformWindows),
		Version:    version,
		Build:      build,
		IconPath:   a.resolvePlatformIcon(wd, platformWindows),
		Archs:      win.windowsArchs(),
		ReleaseDir: outDir,
		Logf:       prog.appendf,
		OnStep:     prog.beginStep,
		Cancelled:  func() bool { return prog.Cancelled() },
		SetRunningCmd: func(cmd *exec.Cmd) {
			if cmd == nil {
				prog.clearRunningCmd()
			} else {
				prog.setRunningCmd(cmd)
			}
		},
	}

	go func() {
		primary, extra, err := bundleWindows(opts)
		if err != nil {
			prog.failStep(activeFailIndexFor(prog), err)
			if prog.Cancelled() {
				prog.appendf("— cancelled —")
			} else {
				prog.appendf("— error: %v —", err)
			}
			return
		}
		if prog.Cancelled() {
			prog.failStep(len(windowsBundleSteps)-1, fmt.Errorf("cancelled"))
			prog.appendf("— cancelled —")
			return
		}
		if err := recordAndSaveRelease(appID, platformWindows, version, build, primary, extra...); err != nil {
			prog.appendf("warning: could not save release log: %v", err)
		}
		prog.finishOK(primary)
		prog.appendf("— done: %s —", primary)
		for _, e := range extra {
			prog.appendf("— also: %s —", e)
		}
		refreshVersionSnapsFromBackground()
	}()
}

func startMacOSNotarize(appIndex int, entry ReleaseEntry) {
	actionErr = ""
	if appIndex < 0 || appIndex >= len(store.Apps) {
		actionErr = "app not found"
		return
	}
	a := store.Apps[appIndex]
	if a.MacOS == nil {
		actionErr = "macOS not configured"
		return
	}
	profile := strings.TrimSpace(a.MacOS.NotaryProfile)
	if profile == "" {
		actionErr = "set notary keychain profile in Edit configuration"
		return
	}
	// Share the macOS job slot so bundle and notarize don't race on the same app.
	if jobs.isBusy(platformMacOS) {
		actionErr = "macOS is already busy"
		return
	}
	prog := jobs.tryBegin("macos-notarize", macosNotarizeSteps)
	if prog == nil {
		actionErr = "notarize already running"
		return
	}
	zipPath, _ := macOSArtifacts(entry)
	if zipPath == "" {
		zipPath = entry.Path
	}
	appPath := macOSAppBesideZip(zipPath)
	if appPath == "" || !pathExists(appPath) {
		prog.clearChrome()
		actionErr = "could not find self-dist .app next to " + zipPath
		return
	}
	if !fileExists(zipPath) {
		// Notarize will rewrite a zip beside the .app.
		zipPath = filepath.Join(filepath.Dir(appPath),
			strings.TrimSuffix(filepath.Base(appPath), ".app")+"-notarized.zip")
	}

	appID := a.ID
	version := entry.Version

	opts := MacOSNotarizeOpts{
		AppPath:   appPath,
		ZipPath:   zipPath,
		Profile:   profile,
		Logf:      prog.appendf,
		OnStep:    prog.beginStep,
		Cancelled: func() bool { return prog.Cancelled() },
		SetRunningCmd: func(cmd *exec.Cmd) {
			if cmd == nil {
				prog.clearRunningCmd()
			} else {
				prog.setRunningCmd(cmd)
			}
		},
	}

	go func() {
		outZip, err := notarizeMacOS(opts)
		if err != nil {
			prog.failStep(activeFailIndexFor(prog), err)
			if prog.Cancelled() {
				prog.appendf("— cancelled —")
			} else {
				prog.appendf("— error: %v —", err)
			}
			return
		}
		if prog.Cancelled() {
			prog.failStep(len(macosNotarizeSteps)-1, fmt.Errorf("cancelled"))
			prog.appendf("— cancelled —")
			return
		}
		if err := markNotarizedAndSave(appID, version, platformMacOS); err != nil {
			prog.appendf("warning: could not save release log: %v", err)
		}
		prog.finishOK(outZip)
		prog.appendf("— notarized: %s —", outZip)
		refreshVersionSnapsFromBackground()
	}()
}

// progressBar paints a capsule track with a fill overlay (like custom-sliders):
// one rounded track + clipped fill, no rounded seam between green and gray.
func progressBar(frac float32) {
	if frac < 0 {
		frac = 0
	}
	if frac > 1 {
		frac = 1
	}
	const (
		barH float32 = 10
		barW float32 = 420
	)
	r := barH / 2
	// Outer capsule = full track. Clip so the fill is squared at the seam
	// but still rounded on the left by the parent.
	Container(Attrs(FixSize(barW, barH), Corners(r), Clip, Background(0, 0, 88, 1)), func() {
		if frac <= 0 {
			return
		}
		fillW := barW * frac
		if fillW < barH && frac > 0 {
			fillW = barH // keep a visible nub early on
		}
		if fillW > barW {
			fillW = barW
		}
		Element(Attrs(
			Float(0, 0),
			FixSize(fillW, barH),
			Background(140, 55, 45, 1),
			ClickThrough,
		))
	})
}
