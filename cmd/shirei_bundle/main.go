// shirei_bundle builds release packages for shirei apps.
//
// Install:
//
//	go install go.hasen.dev/shirei/cmd/shirei_bundle@latest
//
// GUI (default):
//
//	shirei_bundle
//
// CLI iOS:
//
//	shirei_bundle -platform ios \
//	  -team FLGJ22JLN7 -id systems.judi.myapp \
//	  -version 0.1.0 -build 1 ./path/to/main
//
// CLI Android:
//
//	SHIREI_ANDROID_KS_PASS=… shirei_bundle -platform android \
//	  -keystore ~/keys/app.jks -key-alias myalias \
//	  -id systems.judi.myapp -version 0.1.0 -build 1 ./path/to/main
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func main() {
	platform := flag.String("platform", "", "ios | android | macos (empty + no package → GUI)")
	notary := flag.String("notary-profile", "", "macOS: after build, notarize with this keychain profile (optional)")
	team := flag.String("team", "", "iOS: Apple team id")
	identity := flag.String("identity", "", "iOS: codesign identity (optional)")
	appID := flag.String("id", "", "bundle / application id")
	name := flag.String("name", "", "display name")
	version := flag.String("version", "0.1.0", "marketing version")
	build := flag.String("build", "1", "build number / versionCode")
	method := flag.String("method", "debugging", "iOS export method")
	outDir := flag.String("o", "", "output directory")
	icon := flag.String("icon", "", "icon image path")
	keystore := flag.String("keystore", "", "Android: keystore path")
	keyAlias := flag.String("key-alias", "", "Android: key alias")
	arch := flag.String("arch", "arm64", "Android: arm64 | arm")
	flag.Parse()

	if flag.NArg() < 1 {
		guiMain()
		return
	}

	plat := strings.ToLower(strings.TrimSpace(*platform))
	if plat == "" {
		plat = "ios"
	}

	pkgDir, err := filepath.Abs(flag.Arg(0))
	if err != nil {
		fmt.Fprintln(os.Stderr, "shirei_bundle:", err)
		os.Exit(1)
	}

	logf := func(format string, args ...any) {
		fmt.Fprintf(os.Stderr, format+"\n", args...)
	}

	switch plat {
	case "ios":
		ipa, err := bundleIOS(IOSBundleOpts{
			PkgDir:   pkgDir,
			TeamID:   strings.TrimSpace(*team),
			Identity: strings.TrimSpace(*identity),
			BundleID: strings.TrimSpace(*appID),
			Name:     strings.TrimSpace(*name),
			Version:  strings.TrimSpace(*version),
			Build:    strings.TrimSpace(*build),
			Method:   strings.TrimSpace(*method),
			OutDir:   strings.TrimSpace(*outDir),
			IconPath: strings.TrimSpace(*icon),
			Logf:     logf,
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, "shirei_bundle:", err)
			os.Exit(1)
		}
		fmt.Println(ipa)

	case "android":
		storePass := os.Getenv("SHIREI_ANDROID_KS_PASS")
		keyPass := os.Getenv("SHIREI_ANDROID_KEY_PASS")
		if storePass == "" {
			fmt.Fprintln(os.Stderr, "shirei_bundle: set SHIREI_ANDROID_KS_PASS for Android CLI signing")
			os.Exit(2)
		}
		apk, err := bundleAndroid(AndroidBundleOpts{
			PkgDir:    pkgDir,
			AppID:     strings.TrimSpace(*appID),
			Name:      strings.TrimSpace(*name),
			Version:   strings.TrimSpace(*version),
			Build:     strings.TrimSpace(*build),
			IconPath:  strings.TrimSpace(*icon),
			Arch:      strings.TrimSpace(*arch),
			Keystore:  strings.TrimSpace(*keystore),
			KeyAlias:  strings.TrimSpace(*keyAlias),
			StorePass: storePass,
			KeyPass:   keyPass,
			OutDir:    strings.TrimSpace(*outDir),
			Logf:      logf,
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, "shirei_bundle:", err)
			os.Exit(1)
		}
		fmt.Println(apk)

	case "macos":
		archFlag := strings.TrimSpace(*arch)
		var archs []string
		switch archFlag {
		case "universal", "both", "all":
			archs = []string{"arm64", "amd64"}
		case "":
			archs = nil // host default inside bundleMacOS
		default:
			archs = []string{archFlag}
		}
		result, err := bundleMacOS(MacOSBundleOpts{
			PkgDir:     pkgDir,
			BundleID:   strings.TrimSpace(*appID),
			Name:       strings.TrimSpace(*name),
			Version:    strings.TrimSpace(*version),
			Build:      strings.TrimSpace(*build),
			IconPath:   strings.TrimSpace(*icon),
			Archs:      archs,
			SelfDist:   true,
			AppStore:   false,
			Identity:   strings.TrimSpace(*identity),
			ReleaseDir: strings.TrimSpace(*outDir),
			Logf:       logf,
		})
		if err != nil {
			fmt.Fprintln(os.Stderr, "shirei_bundle:", err)
			os.Exit(1)
		}
		out := result.Primary()
		if profile := strings.TrimSpace(*notary); profile != "" {
			appPath := result.AppPath
			if appPath == "" {
				appPath = macOSAppBesideZip(result.ZipPath)
			}
			zipOut, err := notarizeMacOS(MacOSNotarizeOpts{
				AppPath: appPath,
				ZipPath: result.ZipPath,
				Profile: profile,
				Logf:    logf,
			})
			if err != nil {
				fmt.Fprintln(os.Stderr, "shirei_bundle notarize:", err)
				os.Exit(1)
			}
			out = zipOut
		}
		fmt.Println(out)

	default:
		fmt.Fprintf(os.Stderr, "shirei_bundle: unknown platform %q (ios|android|macos)\n", plat)
		os.Exit(2)
	}
}
