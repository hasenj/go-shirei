// mobilerun builds and launches shirei main packages on iOS or Android for
// local development (device/simulator iteration). It is not a production or
// store packaging pipeline: Android uses a debug keystore and debuggable
// APKs; iOS uses free/personal team or simulator signing as configured.
//
//	go run ./mobilerun                              # GUI
//	go run ./mobilerun -platform android <pkgdir>   # Android CLI
//	go run ./mobilerun -platform ios <pkgdir>       # iOS CLI
//	go run ./mobilerun --png out.png                # headless GUI snapshot
//
// Preferences: <os.UserConfigDir>/shirei/mobile-run.json
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"go.hasen.dev/shirei"
	"go.hasen.dev/shirei/app"
)

func main() {
	platform := flag.String("platform", "", "ios or android (required for CLI; GUI uses saved/default)")
	arch := flag.String("arch", "", "android ABI: arm64 (default) or arm")
	id := flag.String("id", "", "app id / bundle id (default <prefix>.<folder>)")
	label := flag.String("label", "", "app name / launcher label (default folder name)")
	name := flag.String("name", "", "alias for -label")
	icon := flag.String("icon", "", "path to launcher icon image")
	serial := flag.String("serial", "", "adb device serial")
	buildOnly := flag.Bool("build-only", false, "android: build APK without install")
	screencap := flag.String("screencap", "", "android: screenshot path after launch")
	delay := flag.Int("delay", 2000, "ms to wait before -screencap")
	logcat := flag.Bool("logcat", false, "android: stream logcat after launch")
	team := flag.String("team", "", "ios: DEVELOPMENT_TEAM")
	device := flag.Bool("device", false, "ios: physical device")
	sim := flag.Bool("sim", false, "ios: simulator (default)")
	flagPNG := flag.String("png", "", "render the GUI to a PNG headlessly and exit")
	flag.Parse()

	if *flagPNG != "" {
		initGUIState()
		if err := shirei.RenderToPNG(*flagPNG, 720, 900, RootView); err != nil {
			fmt.Fprintln(os.Stderr, "render failed:", err)
			os.Exit(1)
		}
		return
	}

	if flag.NArg() >= 1 {
		plat := strings.ToLower(strings.TrimSpace(*platform))
		if plat == "" {
			fmt.Fprintln(os.Stderr, "mobilerun: -platform ios|android required for CLI")
			os.Exit(2)
		}
		if err := cliMain(plat, flag.Arg(0), cliFlags{
			arch: *arch, id: *id, label: firstNonEmpty(*label, *name),
			icon: *icon, serial: *serial, buildOnly: *buildOnly,
			screencap: *screencap, delay: *delay, logcat: *logcat,
			team: *team, device: *device, sim: *sim,
		}); err != nil {
			fmt.Fprintln(os.Stderr, "mobilerun:", err)
			os.Exit(1)
		}
		return
	}

	guiMain()
}

type cliFlags struct {
	arch, id, label, icon, serial string
	buildOnly                     bool
	screencap                     string
	delay                         int
	logcat                        bool
	team                          string
	device, sim                   bool
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

func cliMain(plat, pkgArg string, f cliFlags) error {
	pkgDir, err := filepath.Abs(pkgArg)
	if err != nil {
		return err
	}
	logf := func(format string, args ...any) {
		fmt.Printf(format+"\n", args...)
	}

	switch plat {
	case platformAndroid:
		icon := f.icon
		if icon == "" {
			icon = resolveIconPath(pkgDir, "")
		} else if !filepath.IsAbs(icon) {
			icon = resolveIconPath(pkgDir, icon)
		}
		err = buildAndRun(AndroidOpts{
			PkgDir:    pkgDir,
			Arch:      f.arch,
			AppID:     f.id,
			Label:     f.label,
			IconPath:  icon,
			IDPrefix:  defaultAppIDPrefix,
			Serial:    f.serial,
			BuildOnly: f.buildOnly,
			Screencap: f.screencap,
			DelayMs:   f.delay,
		}, logf)
		if err != nil {
			return err
		}
		if f.logcat && !f.buildOnly {
			sdk, err := findSDK()
			if err != nil {
				return err
			}
			adb, err := findADB(sdk)
			if err != nil {
				return err
			}
			logf("— logcat (Ctrl-C to stop)")
			lc := adbCmd(adb, f.serial, "logcat",
				"shirei:V", "threaded_app:V", "AndroidRuntime:E", "DEBUG:I", "*:S")
			lc.Stdout = os.Stdout
			lc.Stderr = os.Stderr
			return lc.Run()
		}
		return nil

	case platformIOS:
		if runtime.GOOS != "darwin" {
			return fmt.Errorf("iOS builds require macOS")
		}
		cfg := defaultConfig()
		cfg.Platform = platformIOS
		cfg.AppID = f.id
		cfg.AppName = f.label
		cfg.IconPath = f.icon
		cfg.TeamID = f.team
		if f.device {
			cfg.IOSTarget = "device"
		} else {
			cfg.IOSTarget = "sim"
		}
		_ = f.sim
		return startIOS(pkgDir, cfg, logf)

	default:
		return fmt.Errorf("unknown platform %q (want ios or android)", plat)
	}
}

func initGUIState() {
	wd, _ = os.Getwd()
	if abs, err := filepath.Abs(wd); err == nil {
		wd = abs
	}
	cfg = loadConfig()
	runner = newRunner()
}

func guiMain() {
	initGUIState()

	go func() {
		// Warm discoveries for both platforms so switching is instant.
		if runtime.GOOS == "darwin" {
			t, _ := listXcodeTeams()
			d := listSimDeviceNames()
			r := listSimRuntimeNames()
			teams = t
			teamsReady = true
			deviceNames = d
			runtimeNames = r
			if cfg.TeamID == "" && len(teams) == 1 {
				cfg.TeamID = teams[0].ID
			}
		} else {
			teamsReady = true
		}
		scanDevices()
		ensureShireiPackages(wd)
		wakeUI()
	}()

	app.SetupIconBytes(iconPNG)
	app.SetupWindow("Shirei Mobile Run", 900, 700)
	app.Run(RootView)
}
