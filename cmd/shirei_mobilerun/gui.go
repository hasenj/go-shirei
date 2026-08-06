package main

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	. "go.hasen.dev/shirei"
	. "go.hasen.dev/shirei/widgets"
)

var (
	wd     string
	cfg    Config
	runner *Runner

	teams        []Team
	teamsReady   bool
	deviceNames  []string
	runtimeNames []string

	lastPkgKey string
)

func RootView() {
	pkgs, pkgsReady, pkgsErr := ensureShireiPackages(wd)
	devices, devsReady, devsErr := ensureDevices()

	if key := packageKey(cfg.Package); key != lastPkgKey {
		if cfg.Packages == nil {
			cfg.Packages = map[string]PackageSettings{}
		}
		if lastPkgKey != "" {
			cfg.Packages[lastPkgKey] = PackageSettings{
				AppID:    strings.TrimSpace(cfg.AppID),
				AppName:  strings.TrimSpace(cfg.AppName),
				IconPath: strings.TrimSpace(cfg.IconPath),
			}
		}
		lastPkgKey = key
		cfg.applyPackageSettings()
	}

	ModAttrs(Viewport, Expand, Background(220, 12, 96, 1))
	// Header
	Container(Attrs(Row, CrossMid, Gap(10)), func() {
		if appIcon != nil {
			ImageView(UseImage("mobilerun-app-icon", appIcon), Vec2{64, 64})
		}
		Container(Attrs(Gap(10)), func() {
			Label("Shirei Mobile Run", FontSize(24), FontWeight(WeightBold))
			Label("Dev launcher: build a package main and run it on iOS or Android (not store packaging).",
				FontSize(12), TextColor(0, 0, 40, 1))
		})
	})
	Label(wd, FontSize(11), TextColor(0, 0, 55, 1))

	Container(Attrs(Pad2(10, 20), Gap(4)), func() {
		// Platform
		Container(Attrs(Gap(6)), func() {
			fieldLabel("Platform")
			Container(Attrs(Row, CrossMid, Gap(12), Wrap), func() {
				SegmentedControl(&cfg.Platform,
					Cell("iOS", platformIOS),
					Cell("Android", platformAndroid),
				)
			})
			if cfg.Platform == platformIOS && runtime.GOOS != "darwin" {
				Label("iOS builds require macOS + Xcode.",
					FontSize(10), TextColor(10, 70, 40, 1))
			}
		})

		if cfg.Platform == platformIOS {
			iosTargetPanel()
		} else {
			androidDevicePanel(devices, devsReady, devsErr)
		}

		// Package (menu + rescan)
		Container(Attrs(Gap(6)), func() {
			fieldLabel("Package")
			// Always use menu chrome so height stays stable while scanning.
			current := cfg.Package
			if current == "" {
				current = "Select package…"
			}
			Container(Attrs(Row, CrossMid, Gap(8)), func() {
				switch {
				case pkgsErr != nil && len(pkgs) == 0:
					ButtonExt(current, ButtonAttrs{Icon: TypArrowSortedDown, Disabled: true}, DefaultButtonLook())
				case !pkgsReady && len(pkgs) == 0:
					ButtonExt("Scanning for package main…", ButtonAttrs{Icon: TypArrowSortedDown, Disabled: true}, DefaultButtonLook())
				case pkgsReady && len(pkgs) == 0:
					ButtonExt("No package main found", ButtonAttrs{Icon: TypArrowSortedDown, Disabled: true}, DefaultButtonLook())
				default:
					MenuButton(MenuIcon, current, func() {
						// Opt into typeahead (MenuFilterQuery); long package lists are searchable.
						_ = MenuFilterQuery()
						for _, p := range pkgs {
							p := p
							label := p.Rel
							if label == "" || label == "." {
								label = p.ImportPath
							}
							if !MenuFilterMatches(label) {
								continue
							}
							if MenuItem(NoIcon, label) {
								cfg.Package = p.Rel
								if p.Rel == "." {
									cfg.Package = p.Dir
								}
							}
						}
					})
				}
				if ButtonExt("", ButtonAttrs{Icon: SymRefresh, Disabled: runner.IsBusy()}, DefaultButtonLook()) {
					invalidatePackages()
					ensureShireiPackages(wd)
				}
			})
			if pkgsErr != nil && len(pkgs) == 0 {
				Label(pkgsErr.Error(), FontSize(10), TextColor(10, 70, 40, 1))
			}
			Label("directories with a .go file containing func main (filterable; approximate).",
				FontSize(10), TextColor(0, 0, 50, 1))
		})

		// Identity: row of three columns; every form field uses the same fixed
		// cell size so inputs line up horizontally (long help text must not
		// widen one column).
		pkgDir := packageDirOf(cfg.Package)
		if cfg.IconPath != "" && pkgDir != "" {
			cfg.IconPath = relativizeIconPath(pkgDir, cfg.IconPath)
		}
		const (
			idFieldW float32 = 260
			idFieldH float32 = 72
			idColGap float32 = 10
		)
		// No Clip: TextInput borders sit on the cell edge and would get shaved.
		identityField := func(body func()) {
			Container(Attrs(FixSize(idFieldW, idFieldH), Gap(4)), body)
		}

		Container(Attrs(Row, Gap(16), Expand, CrossAlign(AlignStart)), func() {
			// Col 0: launcher icon preview
			Container(Attrs(FixSize(128, 128), Clip, Corners(22),
				Background(0, 0, 90, 1), BorderWidth(1), BorderColor(0, 0, 75, 1), Center), func() {
				path := resolveIconPath(pkgDir, cfg.IconPath)
				if path != "" {
					if st, err := os.Stat(path); err == nil && !st.IsDir() {
						Image(path, Vec2{128, 128})
						return
					}
				}
				Label("no icon", FontSize(11), TextColor(0, 0, 55, 1))
			})

			// Col 1: App ID prefix, App ID
			Container(Attrs(Gap(idColGap)), func() {
				identityField(func() {
					fieldLabel("App ID prefix")
					TextInput(&cfg.AppIDPrefix)
					Label("Defaults to "+defaultAppIDPrefix, FontSize(10), TextColor(0, 0, 50, 1))
				})
				identityField(func() {
					fieldLabel("App ID")
					TextInput(&cfg.AppID)
					Label(fmt.Sprintf("Defaults to <folder>. Full App ID: %s.%s",
						cfg.effectiveAppIDPrefix(), filepath.Base(cfg.Package)),
						FontSize(10), TextColor(0, 0, 50, 1))
				})
			})

			// Col 2: App name, App icon
			Container(Attrs(Gap(idColGap)), func() {
				identityField(func() {
					fieldLabel("App name")
					TextInput(&cfg.AppName)
					Label("Defaults to folder name.", FontSize(10), TextColor(0, 0, 50, 1))
				})
				identityField(func() {
					fieldLabel("App icon")
					DirectoryBrowseExt(&cfg.IconPath, FileBrowserAttrs{
						Files:    true,
						Dirs:     false,
						Title:    "Choose app icon",
						Width:    560,
						Start:    iconBrowseStart(pkgDir, cfg.IconPath),
						MinWidth: idFieldW - 90,
						Exts:     []string{".png", ".jpg", ".jpeg", ".webp", ".gif"},
					})
					Label("Relative to package directory",
						FontSize(10), TextColor(0, 0, 50, 1))
				})
			})
		})

		// Actions
		Container(Attrs(Row, CrossMid, Gap(10)), func() {
			busy := runner.IsBusy()
			runLabel := "Run"
			if busy {
				runLabel = "Running…"
			}
			if ButtonExt(runLabel, ButtonAttrs{Accent: AccentMeadow, Disabled: busy}, DefaultButtonLook()) {
				go persistAndRun()
			}
			if cfg.Platform == platformAndroid {
				if ButtonExt("Screencap", ButtonAttrs{Disabled: busy}, DefaultButtonLook()) {
					go grabScreencap()
				}
			}
		})
	})

	// Log
	Container(Attrs(Viewport, Background(0, 0, 100, 1),
		BorderWidth(1), BorderColor(0, 0, 80, 1)), func() {
		if runner.Ring().Len() == 0 {
			Container(Attrs(Pad(10)), func() {
				Label("build and device output appears here…",
					FontSize(12), TextColor(0, 0, 60, 1), FontStyle(StyleItalic))
			})
		} else {
			attrs := DefaultTextStyle()
			attrs.FontSize = 11
			attrs.FontFamilies = Monospace
			attrs.TextColor = Vec4{0, 0, 15, 1}
			LogView(runner.Ring(), attrs)
		}
	})
}

func iosTargetPanel() {
	Container(Attrs(Gap(6)), func() {
		fieldLabel("Target")
		Container(Attrs(Row, CrossMid, Gap(12), Wrap), func() {
			SegmentedControl(&cfg.IOSTarget,
				Cell("Device", "device"),
				Cell("Simulator", "sim"),
			)
			if cfg.IOSTarget == "sim" {
				name := cfg.DeviceName
				if name == "" {
					name = "Automatic"
				}
				MenuButton(MenuIcon, name, func() {
					_ = MenuFilterQuery()
					if MenuFilterMatches("Automatic") && MenuItem(NoIcon, "Automatic") {
						cfg.DeviceName = ""
					}
					if len(deviceNames) == 0 {
						MenuItem(NoIcon, "(no device types — is Xcode installed?)")
						return
					}
					for _, n := range deviceNames {
						n := n
						if !MenuFilterMatches(n) {
							continue
						}
						if MenuItem(NoIcon, n) {
							cfg.DeviceName = n
						}
					}
				})
				rt := cfg.Runtime
				if rt == "" {
					rt = "Automatic"
				}
				MenuButton(MenuIcon, rt, func() {
					_ = MenuFilterQuery()
					if MenuFilterMatches("Automatic") && MenuItem(NoIcon, "Automatic") {
						cfg.Runtime = ""
					}
					if len(runtimeNames) == 0 {
						MenuItem(NoIcon, "(no iOS runtimes installed)")
						return
					}
					for _, n := range runtimeNames {
						n := n
						if !MenuFilterMatches(n) {
							continue
						}
						if MenuItem(NoIcon, n) {
							cfg.Runtime = n
						}
					}
				})
			}
		})
		if cfg.IOSTarget == "sim" {
			Label("Automatic → first booted or installed Simulator.",
				FontSize(10), TextColor(0, 0, 50, 1))
		} else {
			Label("Uses the physical iPhone currently connected over USB.",
				FontSize(10), TextColor(0, 0, 50, 1))
		}
	})

	const teamBlockH float32 = 40
	Container(Attrs(Gap(4), MinHeight(teamBlockH), MaxHeight(teamBlockH), Expand), func() {
		fieldLabel("Apple Team")
		switch {
		case !teamsReady:
			if cfg.TeamID != "" {
				Label(cfg.TeamID, FontSize(12), TextColor(0, 0, 40, 1))
			} else {
				Label("Looking up Xcode accounts…", FontSize(12), TextColor(0, 0, 50, 1))
			}
		case len(teams) == 0:
			TextInput(&cfg.TeamID)
		case len(teams) == 1:
			cfg.TeamID = teams[0].ID
			Label(teamLabel(teams[0]), FontSize(12))
		default:
			current := cfg.TeamID
			curLabel := current
			for _, t := range teams {
				if t.ID == current {
					curLabel = teamLabel(t)
					break
				}
			}
			if curLabel == "" {
				curLabel = "Select team…"
			}
			MenuButton(MenuIcon, curLabel, func() {
				for _, t := range teams {
					t := t
					if MenuItem(NoIcon, teamLabel(t)) {
						cfg.TeamID = t.ID
					}
				}
			})
		}
	})
	if teamsReady && len(teams) == 0 {
		Label("No Xcode teams found — paste a Team ID, or sign in via Xcode → Settings → Accounts.",
			FontSize(10), TextColor(10, 70, 40, 1))
	}
}

func androidDevicePanel(devices []ADBDevice, devsReady bool, devsErr error) {
	Container(Attrs(Gap(6)), func() {
		Container(Attrs(Row, CrossMid, Gap(8)), func() {
			fieldLabel("Device")
			Filler(1)
			if CtrlButton(NoIcon, "Refresh", !runner.IsBusy()) {
				rescanDevices()
			}
		})
		Container(Attrs(Row, CrossMid, Gap(12), Wrap), func() {
			current := "Select device…"
			if cfg.Serial != "" {
				current = cfg.Serial
				for _, d := range devices {
					if d.Serial == cfg.Serial {
						current = d.Display()
						break
					}
				}
			} else if len(devices) == 1 {
				current = devices[0].Display()
			}
			MenuButton(MenuIcon, current, func() {
				_ = MenuFilterQuery()
				if len(devices) == 0 {
					MenuItem(NoIcon, "(no devices — check USB / enable USB debugging)")
					return
				}
				if MenuFilterMatches("Default (only device)") && MenuItem(NoIcon, "Default (only device)") {
					cfg.Serial = ""
				}
				for _, d := range devices {
					d := d
					if !MenuFilterMatches(d.Display()) {
						continue
					}
					if MenuItem(NoIcon, d.Display()) {
						cfg.Serial = d.Serial
					}
				}
			})
			SegmentedControl(&cfg.Arch,
				Cell("arm64", "arm64"),
				Cell("arm 32-bit", "arm"),
			)
		})
		switch {
		case !devsReady:
			Label("Scanning adb devices…", FontSize(10), TextColor(0, 0, 50, 1))
		case devsErr != nil:
			Label(devsErr.Error(), FontSize(10), TextColor(10, 70, 40, 1))
		case len(devices) == 0:
			Label("No devices. Plug a phone in with USB debugging on, accept the RSA prompt, then Refresh.",
				FontSize(10), TextColor(10, 70, 40, 1))
		default:
			for _, d := range devices {
				if d.State == "unauthorized" {
					Label("A device is unauthorized — accept the USB-debugging prompt on its screen.",
						FontSize(10), TextColor(10, 70, 40, 1))
					break
				}
			}
		}
	})
}

func fieldLabel(s string) {
	Label(s, FontSize(11), FontWeight(WeightBold), TextColor(220, 15, 30, 1))
}

// packageDirOf resolves cfg.Package to an absolute package directory.
func packageDirOf(pkg string) string {
	pkg = strings.TrimSpace(pkg)
	if pkg == "" {
		return ""
	}
	if filepath.IsAbs(pkg) {
		return pkg
	}
	return filepath.Join(wd, pkg)
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
	if dir := filepath.Dir(abs); dir != "" && dir != "." {
		if st, err := os.Stat(dir); err == nil && st.IsDir() {
			return dir
		}
	}
	if pkgDir != "" {
		return pkgDir
	}
	return wd
}

func wakeUI() {
	RequestNextFrame()
}

func persistAndRun() {
	if err := saveConfig(cfg); err != nil {
		runner.appendf("save prefs: %v", err)
	}

	pkg := strings.TrimSpace(cfg.Package)
	if pkg == "" {
		runner.appendf("pick a package first")
		RequestNextFrame()
		return
	}
	pkgDir := pkg
	if !filepath.IsAbs(pkgDir) {
		pkgDir = filepath.Join(wd, pkgDir)
	}
	if st, err := os.Stat(pkgDir); err != nil || !st.IsDir() {
		runner.appendf("package path not found: %s", pkgDir)
		RequestNextFrame()
		return
	}

	if cfg.Platform == platformIOS && cfg.IOSTarget == "device" && strings.TrimSpace(cfg.TeamID) == "" {
		runner.appendf("Team ID required for device builds (Xcode → Settings → Accounts)")
		RequestNextFrame()
		return
	}
	if strings.TrimSpace(cfg.AppName) == "" {
		cfg.AppName = filepath.Base(pkgDir)
	}

	if err := runner.Start(pkgDir, cfg); err != nil {
		runner.appendf("%v", err)
		RequestNextFrame()
	}
}

func grabScreencap() {
	dir, err := os.UserCacheDir()
	if err != nil {
		runner.appendf("screencap: %v", err)
		return
	}
	out := filepath.Join(dir, "shirei", fmt.Sprintf("android-screen-%d.png", os.Getpid()))
	if err := screencapNow(cfg.Serial, out); err != nil {
		runner.appendf("screencap: %v", err)
	} else {
		runner.appendf("screencap: %s", out)
	}
	RequestNextFrame()
}
