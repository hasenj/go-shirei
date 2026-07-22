#!/usr/bin/env bash
# ios-run.sh — build a shirei main package as an iOS app and run it.
#
# Usage:
#   ./ios-run.sh ./demos/theme              # iOS Simulator (default)
#   ./ios-run.sh --device ./demos/theme     # physical iPhone (USB)
#   ./ios-run.sh --sim ./demos/theme        # force Simulator
#
# What it does:
#   1. Injects a temporary //go:build ios export that calls the package's main()
#   2. go build -buildmode=c-archive (simulator or iphoneos SDK)
#   3. Links a thin UIKit host into an .app bundle
#   4. Signs + installs + launches (simctl or devicectl)
#
# Device signing uses the free/personal Xcode team via Automatic Signing
# (ioshost/DeviceHost.xcodeproj). First device run may prompt for keychain
# access and can take longer while Xcode creates a cert + profile.
#
# Layout (portable — no monorepo required):
#   SHIREI_IOS_HOST_DIR  template UIKit host (never mutated)
#   SHIREI_IOS_BUILD_DIR scratch (archive, host work copy, .app)
#   SHIREI_MODULE        optional path to a local go.hasen.dev/shirei module
#                        for go.work when the package lives outside that module
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
# Template host: env override, else ioshost/ next to this script (embedded extract).
HOST_TEMPLATE="${SHIREI_IOS_HOST_DIR:-$SCRIPT_DIR/ioshost}"
# Build scratch: prefer env; else user cache; never write into the template tree.
if [[ -n "${SHIREI_IOS_BUILD_DIR:-}" ]]; then
	BUILD_DIR="$SHIREI_IOS_BUILD_DIR"
elif [[ -n "${XDG_CACHE_HOME:-}" ]]; then
	BUILD_DIR="$XDG_CACHE_HOME/shirei/ios-build"
else
	BUILD_DIR="${HOME}/Library/Caches/shirei/ios-build"
fi
# Optional local shirei module (for go.work). Not the same as HOST_TEMPLATE.
SHIREI_MODULE="${SHIREI_MODULE:-}"

# Device signing team. Prefer SHIREI_IOS_TEAM; otherwise leave empty and let
# xcodebuild / the iosrun GUI supply it. No personal team id hard-coded.
DEVELOPMENT_TEAM="${SHIREI_IOS_TEAM:-}"

die() { echo "ios-run: $*" >&2; exit 1; }
log() { echo "ios-run: $*" >&2; }

# apply_usage_descriptions injects privacy purpose strings from
# SHIREI_IOS_USAGE_FILE (KEY=text lines, written by mobilerun from
# shirei.capabilities on the app's Go deps). Safe no-op if unset/empty.
apply_usage_descriptions() {
	local plist="$1"
	local file="${SHIREI_IOS_USAGE_FILE:-}"
	[[ -n "$file" && -f "$file" ]] || return 0
	local line key val
	while IFS= read -r line || [[ -n "$line" ]]; do
		[[ -z "$line" || "$line" == \#* ]] && continue
		key="${line%%=*}"
		val="${line#*=}"
		[[ -n "$key" && "$key" != "$line" ]] || continue
		/usr/libexec/PlistBuddy -c "Add :${key} string ${val}" "$plist" 2>/dev/null \
			|| /usr/libexec/PlistBuddy -c "Set :${key} ${val}" "$plist" 2>/dev/null \
			|| log "warning: could not set Info.plist $key"
	done < "$file"
}

# apply_orientation rewrites UISupportedInterfaceOrientations from
# SHIREI_IOS_ORIENTATION (landscape | portrait). Empty = leave template (any).
# Written by mobilerun from orientation-landscape / orientation-portrait tokens.
apply_orientation() {
	local plist="$1"
	local orient="${SHIREI_IOS_ORIENTATION:-}"
	[[ -n "$orient" ]] || return 0
	/usr/libexec/PlistBuddy -c "Delete :UISupportedInterfaceOrientations" "$plist" 2>/dev/null || true
	/usr/libexec/PlistBuddy -c "Delete :UISupportedInterfaceOrientations~ipad" "$plist" 2>/dev/null || true
	/usr/libexec/PlistBuddy -c "Add :UISupportedInterfaceOrientations array" "$plist" 2>/dev/null || true
	/usr/libexec/PlistBuddy -c "Add :UISupportedInterfaceOrientations~ipad array" "$plist" 2>/dev/null || true
	case "$orient" in
		landscape)
			/usr/libexec/PlistBuddy -c "Add :UISupportedInterfaceOrientations:0 string UIInterfaceOrientationLandscapeLeft" "$plist" 2>/dev/null || true
			/usr/libexec/PlistBuddy -c "Add :UISupportedInterfaceOrientations:1 string UIInterfaceOrientationLandscapeRight" "$plist" 2>/dev/null || true
			/usr/libexec/PlistBuddy -c "Add :UISupportedInterfaceOrientations~ipad:0 string UIInterfaceOrientationLandscapeLeft" "$plist" 2>/dev/null || true
			/usr/libexec/PlistBuddy -c "Add :UISupportedInterfaceOrientations~ipad:1 string UIInterfaceOrientationLandscapeRight" "$plist" 2>/dev/null || true
			log "Info.plist orientations: landscape only"
			;;
		portrait)
			/usr/libexec/PlistBuddy -c "Add :UISupportedInterfaceOrientations:0 string UIInterfaceOrientationPortrait" "$plist" 2>/dev/null || true
			/usr/libexec/PlistBuddy -c "Add :UISupportedInterfaceOrientations~ipad:0 string UIInterfaceOrientationPortrait" "$plist" 2>/dev/null || true
			/usr/libexec/PlistBuddy -c "Add :UISupportedInterfaceOrientations~ipad:1 string UIInterfaceOrientationPortraitUpsideDown" "$plist" 2>/dev/null || true
			log "Info.plist orientations: portrait only"
			;;
		*)
			log "warning: unknown SHIREI_IOS_ORIENTATION=$orient (ignored)"
			;;
	esac
}

usage() {
	cat >&2 <<'EOF'
Usage: ./ios-run.sh [--device|--sim] <path-to-main-package>

  Builds the package for iOS, packages it with the thin UIKit host, and
  launches it on the Simulator (default) or a USB iPhone.

Examples:
  ./ios-run.sh ./demos/theme
  ./ios-run.sh --device /path/to/package

Environment:
  SHIREI_IOS_HOST_DIR      UIKit host template (default: <script>/ioshost)
  SHIREI_IOS_BUILD_DIR     build scratch (default: ~/Library/Caches/shirei/ios-build)
  SHIREI_MODULE            local go.hasen.dev/shirei module for go.work (optional)
  SHIREI_IOS_BUNDLE_ID     CFBundleIdentifier (default: dev.shirei.<folder>)
  SHIREI_IOS_APP_NAME      home-screen / .app name (default: package folder name)
  SHIREI_IOS_ICON_PATH     PNG/JPEG path for AppIcon (optional)
  SHIREI_IOS_TEAM          DEVELOPMENT_TEAM for device signing (required for --device)
  SHIREI_IOS_DEVICE        force sim UDID / device UDID
  SHIREI_IOS_DEVICE_NAME   preferred sim model (empty = any available)
  SHIREI_IOS_RUNTIME       preferred sim runtime (empty = any available)
  SHIREI_IOS_MIN_OS        deployment target (default: 16.0)
  SHIREI_IOS_NO_LAUNCH     if set, build only
  SHIREI_IOS_USAGE_FILE    KEY=text lines for privacy purpose strings (mobilerun)
  SHIREI_IOS_ORIENTATION   landscape | portrait (from orientation-* capabilities)
EOF
	exit 2
}

TARGET=sim
PKG_ARG=""
while [[ $# -gt 0 ]]; do
	case "$1" in
		-h|--help) usage ;;
		--device|-d) TARGET=device; shift ;;
		--sim|--simulator) TARGET=sim; shift ;;
		-*) die "unknown flag: $1 (try --help)" ;;
		*) PKG_ARG="$1"; shift; break ;;
	esac
done
[[ -n "$PKG_ARG" ]] || usage
[[ $# -eq 0 ]] || die "unexpected args: $*"

# Package path: absolute or relative to the current working directory.
if [[ -d "$PKG_ARG" ]]; then
	PKG_DIR="$(cd "$PKG_ARG" && pwd)"
else
	die "package path not found: $PKG_ARG (use an absolute path or a path relative to cwd)"
fi

# Bundle id: env, else dev.shirei.<sanitized package folder>.
if [[ -n "${SHIREI_IOS_BUNDLE_ID:-}" ]]; then
	BUNDLE_ID="$SHIREI_IOS_BUNDLE_ID"
else
	_pkg_base="$(basename "$PKG_DIR")"
	_comp="$(printf '%s' "$_pkg_base" | tr '[:upper:]' '[:lower:]' | tr -cd 'a-z0-9')"
	[[ -n "$_comp" && "$_comp" != [0-9]* ]] || _comp="a${_comp}"
	BUNDLE_ID="dev.shirei.${_comp}"
fi
# Home-screen label (CFBundleDisplayName) vs filesystem product name (executable / Foo.app).
APP_NAME_RAW="${SHIREI_IOS_APP_NAME:-}"
if [[ -z "$APP_NAME_RAW" ]]; then
	APP_NAME_RAW="$(basename "$PKG_DIR")"
fi
APP_DISPLAY="$(printf '%s' "$APP_NAME_RAW" | tr -cd 'A-Za-z0-9 ._-')"
APP_DISPLAY="$(echo "$APP_DISPLAY" | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//')"
[[ -n "$APP_DISPLAY" ]] || APP_DISPLAY="shirei"
# Product/executable: no spaces (xcodebuild + codesign are happier).
APP_NAME="$(echo "$APP_DISPLAY" | tr ' ' '-')"
[[ -n "$APP_NAME" ]] || APP_NAME="shirei"

[[ -d "$HOST_TEMPLATE" ]] || die "host template not found: $HOST_TEMPLATE (set SHIREI_IOS_HOST_DIR)"
[[ -f "$HOST_TEMPLATE/main.m" ]] || die "host template missing main.m: $HOST_TEMPLATE"

find_module_root() {
	local d="$1"
	while [[ "$d" != "/" ]]; do
		if [[ -f "$d/go.mod" ]]; then
			echo "$d"
			return 0
		fi
		d="$(dirname "$d")"
	done
	return 1
}

MODULE_ROOT="$(find_module_root "$PKG_DIR")" \
	|| die "no go.mod found above $PKG_DIR"

# Package may be the module root itself (e.g. shirei/examples/piano has its own
# go.mod) or a subpackage of a larger module (e.g. shirei/demos/theme).
if [[ "$PKG_DIR" == "$MODULE_ROOT" ]]; then
	PKG_SPEC="."
else
	PKG_REL="${PKG_DIR#"$MODULE_ROOT"/}"
	if [[ "$PKG_REL" == "$PKG_DIR" || -z "$PKG_REL" ]]; then
		die "package $PKG_DIR is not under module root $MODULE_ROOT"
	fi
	PKG_SPEC="./$PKG_REL"
fi

if ! grep -qE '^package main' "$PKG_DIR"/*.go 2>/dev/null; then
	die "expected package main in $PKG_DIR"
fi

# Optional: wire a local shirei module via go.work when the package lives
# outside it (e.g. examples with their own go.mod). Set SHIREI_MODULE to the
# path of a checkout that has the iOS backend.
GOWORK_FILE=""
if [[ -n "$SHIREI_MODULE" && -f "$SHIREI_MODULE/go.mod" && "$MODULE_ROOT" != "$SHIREI_MODULE" ]]; then
	mkdir -p "$BUILD_DIR"
	GOWORK_FILE="$BUILD_DIR/ios-run.go.work"
	# go.work's "go" line must be >= every use'd module's go version.
	# Prefer the max of shirei + the app module (e.g. demo go 1.25 vs shirei 1.24).
	GO_LINE_A="$(awk '/^go /{print $2; exit}' "$SHIREI_MODULE/go.mod")"
	GO_LINE_B="$(awk '/^go /{print $2; exit}' "$MODULE_ROOT/go.mod" 2>/dev/null || true)"
	GO_VER="1.24"
	if [[ -n "$GO_LINE_A" ]]; then GO_VER="$GO_LINE_A"; fi
	if [[ -n "$GO_LINE_B" ]]; then
		# sort -V: pick the higher version
		GO_VER="$(printf '%s\n%s\n' "$GO_VER" "$GO_LINE_B" | sort -V | tail -1)"
	fi
	cat >"$GOWORK_FILE" <<EOF
go $GO_VER

use (
	$MODULE_ROOT
	$SHIREI_MODULE
)
EOF
	export GOWORK="$GOWORK_FILE"
	log "using local shirei module $SHIREI_MODULE via $GOWORK_FILE"
fi

command -v go >/dev/null || die "go not found"
command -v xcrun >/dev/null || die "xcrun not found (install Xcode)"
command -v codesign >/dev/null || die "codesign not found"

MIN_OS="${SHIREI_IOS_MIN_OS:-16.0}"
GOARCH=arm64
CLANG_ARCH=arm64

if [[ "$TARGET" == "sim" ]]; then
	SDK_NAME=iphonesimulator
	VERSION_MIN_FLAG="-mios-simulator-version-min=$MIN_OS"
	HOST_ARCH="$(uname -m)"
	case "$HOST_ARCH" in
		arm64) GOARCH=arm64; CLANG_ARCH=arm64 ;;
		x86_64) GOARCH=amd64; CLANG_ARCH=x86_64 ;;
		*) die "unsupported host arch: $HOST_ARCH" ;;
	esac
else
	SDK_NAME=iphoneos
	VERSION_MIN_FLAG="-miphoneos-version-min=$MIN_OS"
	GOARCH=arm64
	CLANG_ARCH=arm64
fi

SDK_PATH="$(xcrun --sdk "$SDK_NAME" --show-sdk-path 2>/dev/null)" \
	|| die "$SDK_NAME SDK not found"
CLANG="$(xcrun --sdk "$SDK_NAME" -f clang)" \
	|| die "clang for $SDK_NAME not found"

mkdir -p "$BUILD_DIR"
APP_DIR="$BUILD_DIR/$APP_NAME.app"
ARCHIVE="$BUILD_DIR/libshirei.a"
HEADER="$BUILD_DIR/libshirei.h"
BIN="$APP_DIR/$APP_NAME"
DERIVED="$BUILD_DIR/DerivedData"

# Temporary export file so c-archive can call the package's main().
EXPORT_FILE="$PKG_DIR/zz_shirei_ios_export.go"
cleanup() {
	rm -f "$EXPORT_FILE"
}
trap cleanup EXIT

cat >"$EXPORT_FILE" <<'EOF'
//go:build ios

// Code generated by ios-run.sh — do not commit.
// Calls this package's main() from the UIKit host after UIApplicationMain starts.
package main

import "C"

//export shirei_ios_run
func shirei_ios_run() {
	main()
}
EOF

log "building c-archive GOOS=ios GOARCH=$GOARCH target=$TARGET ($PKG_SPEC)"
log "  module: $MODULE_ROOT"
log "  sdk:    $SDK_PATH"

export CGO_ENABLED=1
export GOOS=ios
export GOARCH
export CC="$CLANG"
export CGO_CFLAGS="-isysroot $SDK_PATH $VERSION_MIN_FLAG -arch $CLANG_ARCH"
export CGO_LDFLAGS="-isysroot $SDK_PATH $VERSION_MIN_FLAG -arch $CLANG_ARCH"

(
	cd "$MODULE_ROOT"
	go build -buildmode=c-archive -o "$ARCHIVE" "$PKG_SPEC"
)

[[ -f "$ARCHIVE" ]] || die "c-archive not produced: $ARCHIVE"
if [[ ! -f "$HEADER" ]]; then
	if compgen -G "$BUILD_DIR"/*.h >/dev/null; then
		HEADER="$(ls "$BUILD_DIR"/*.h | head -1)"
	else
		die "c-archive header not found next to $ARCHIVE"
	fi
fi

# install_app_icon SRC_IMAGE APP_DIR PLATFORM
# Builds a minimal AppIcon asset catalog via actool and merges icon keys into
# Info.plist. PLATFORM is iphoneos or iphonesimulator. No-op if SRC empty.
install_app_icon() {
	local src="$1" app_dir="$2" platform="$3"
	[[ -n "$src" && -f "$src" ]] || return 0
	command -v sips >/dev/null || { log "warning: sips not found; skipping icon"; return 0; }
	command -v xcrun >/dev/null || return 0

	local asset_root="$BUILD_DIR/AppIcon.xcassets"
	local iconset="$asset_root/AppIcon.appiconset"
	rm -rf "$asset_root"
	mkdir -p "$iconset"

	# Single 1024×1024 universal iOS icon (Xcode 14+ style).
	local icon_png="$iconset/Icon.png"
	if ! sips -s format png -z 1024 1024 "$src" --out "$icon_png" >/dev/null 2>&1; then
		log "warning: could not convert icon $src (need PNG/JPEG); skipping"
		return 0
	fi
	cat >"$iconset/Contents.json" <<'JSON'
{
  "images" : [
    {
      "filename" : "Icon.png",
      "idiom" : "universal",
      "platform" : "ios",
      "size" : "1024x1024"
    }
  ],
  "info" : { "author" : "shirei-ios-run", "version" : 1 }
}
JSON

	local partial="$BUILD_DIR/icon-partial.plist"
	rm -f "$partial"
	if ! xcrun actool "$asset_root" \
		--compile "$app_dir" \
		--platform "$platform" \
		--minimum-deployment-target "$MIN_OS" \
		--app-icon AppIcon \
		--output-partial-info-plist "$partial" \
		--notices --warnings >/dev/null 2>"$BUILD_DIR/actool.err"; then
		log "warning: actool failed for icon (see $BUILD_DIR/actool.err); skipping"
		return 0
	fi

	# Merge CFBundleIcons / CFBundleIconName from actool partial plist.
	if [[ -f "$partial" ]]; then
		/usr/libexec/PlistBuddy -c "Delete :CFBundleIcons" "$app_dir/Info.plist" 2>/dev/null || true
		/usr/libexec/PlistBuddy -c "Delete :CFBundleIconName" "$app_dir/Info.plist" 2>/dev/null || true
		# Copy keys if present
		if /usr/libexec/PlistBuddy -c "Print :CFBundleIcons" "$partial" >/dev/null 2>&1; then
			# Export as xml fragment is painful; set CFBundleIconName at least.
			:
		fi
		if /usr/libexec/PlistBuddy -c "Print :CFBundleIconName" "$partial" >/dev/null 2>&1; then
			local iname
			iname="$(/usr/libexec/PlistBuddy -c "Print :CFBundleIconName" "$partial" 2>/dev/null || true)"
			if [[ -n "$iname" ]]; then
				/usr/libexec/PlistBuddy -c "Add :CFBundleIconName string $iname" "$app_dir/Info.plist" 2>/dev/null \
					|| /usr/libexec/PlistBuddy -c "Set :CFBundleIconName $iname" "$app_dir/Info.plist" 2>/dev/null \
					|| true
			fi
		fi
		# Always declare primary icon name AppIcon when Assets.car was produced.
		if [[ -f "$app_dir/Assets.car" ]]; then
			/usr/libexec/PlistBuddy -c "Add :CFBundleIconName string AppIcon" "$app_dir/Info.plist" 2>/dev/null \
				|| /usr/libexec/PlistBuddy -c "Set :CFBundleIconName AppIcon" "$app_dir/Info.plist" 2>/dev/null \
				|| true
			/usr/libexec/PlistBuddy -c "Add :CFBundleIcons:CFBundlePrimaryIcon:CFBundleIconName string AppIcon" "$app_dir/Info.plist" 2>/dev/null \
				|| true
		fi
	fi
	log "installed app icon from $src"
}

ICON_PATH="${SHIREI_IOS_ICON_PATH:-}"

# ---------------------------------------------------------------------------
# Simulator path: hand-link + ad-hoc sign + simctl
# ---------------------------------------------------------------------------
if [[ "$TARGET" == "sim" ]]; then
	log "linking UIKit host → $APP_DIR"
	rm -rf "$APP_DIR"
	mkdir -p "$APP_DIR"

	# Mutate a *copy* of the template plist — never the host template on disk.
	cp "$HOST_TEMPLATE/Info.plist" "$APP_DIR/Info.plist"
	/usr/libexec/PlistBuddy -c "Set :CFBundleExecutable $APP_NAME" "$APP_DIR/Info.plist" 2>/dev/null || true
	/usr/libexec/PlistBuddy -c "Set :CFBundleName $APP_DISPLAY" "$APP_DIR/Info.plist" 2>/dev/null || true
	/usr/libexec/PlistBuddy -c "Set :CFBundleDisplayName $APP_DISPLAY" "$APP_DIR/Info.plist" 2>/dev/null \
		|| /usr/libexec/PlistBuddy -c "Add :CFBundleDisplayName string $APP_DISPLAY" "$APP_DIR/Info.plist" 2>/dev/null \
		|| true
	/usr/libexec/PlistBuddy -c "Set :CFBundleIdentifier $BUNDLE_ID" "$APP_DIR/Info.plist" 2>/dev/null || true
	apply_usage_descriptions "$APP_DIR/Info.plist"
	apply_orientation "$APP_DIR/Info.plist"

	"$CLANG" \
		-isysroot "$SDK_PATH" \
		$VERSION_MIN_FLAG \
		-arch "$CLANG_ARCH" \
		-fobjc-arc \
		-o "$BIN" \
		"$HOST_TEMPLATE/main.m" \
		"$ARCHIVE" \
		-framework UIKit \
		-framework Foundation \
		-framework QuartzCore \
		-framework CoreGraphics \
		-framework CoreFoundation \
		-framework Security \
		-framework CoreServices \
		-framework SystemConfiguration \
		-framework CFNetwork \
		-framework AudioToolbox \
		-framework AVFoundation \
		-framework GameController \
		-lresolv \
		|| die "clang link failed"

	install_app_icon "$ICON_PATH" "$APP_DIR" iphonesimulator

	codesign -s - --force --entitlements /dev/null "$APP_DIR" 2>/dev/null \
		|| codesign -s - --force "$APP_DIR" \
		|| die "codesign failed"

	log "built $APP_DIR"

	if [[ -n "${SHIREI_IOS_NO_LAUNCH:-}" ]]; then
		log "SHIREI_IOS_NO_LAUNCH set; skipping install/launch"
		exit 0
	fi

	# Empty prefs mean "any available" — no machine-specific model defaults.
	PREF_NAME="${SHIREI_IOS_DEVICE_NAME:-}"
	PREF_RUNTIME="${SHIREI_IOS_RUNTIME:-}"

	udid_from_line() {
		echo "$1" | grep -oE '[A-F0-9]{8}-[A-F0-9]{4}-[A-F0-9]{4}-[A-F0-9]{4}-[A-F0-9]{12}' | head -1
	}

	device_type_id() {
		local want="$1" line
		line="$(xcrun simctl list devicetypes 2>/dev/null | grep -F "$want" | head -1 || true)"
		[[ -n "$line" ]] || return 1
		echo "$line" | sed -n 's/.*(\(.*\))$/\1/p'
	}

	pick_runtime_id() {
		local pref="$1" line rid
		if [[ -n "$pref" ]]; then
			line="$(xcrun simctl list runtimes 2>/dev/null | grep -E '^iOS ' | grep -i "$pref" | grep -v unavailable | head -1 || true)"
		fi
		if [[ -z "$line" ]]; then
			line="$(xcrun simctl list runtimes 2>/dev/null | grep -E '^iOS ' | grep -v unavailable | head -1 || true)"
		fi
		[[ -n "$line" ]] || return 1
		rid="$(echo "$line" | awk -F' - ' '{print $NF}' | tr -d '[:space:]')"
		[[ -n "$rid" ]] || return 1
		echo "$rid"
	}

	ensure_preferred_device() {
		local name="$1" runtime_pref="$2" line udid dtype rtid block
		[[ -n "$name" ]] || return 1
		line="$(xcrun simctl list devices available 2>/dev/null | grep -F "$name" | grep '(Booted)' | head -1 || true)"
		if [[ -n "$line" ]]; then udid_from_line "$line"; return 0; fi
		if [[ -n "$runtime_pref" ]]; then
			block="$(xcrun simctl list devices available 2>/dev/null | awk -v rt="$runtime_pref" -v name="$name" '
				/^-- / { section=$0; want = (section ~ rt); next }
				want && index($0, name) { print; exit }
			')"
			if [[ -n "$block" ]]; then udid_from_line "$block"; return 0; fi
		fi
		line="$(xcrun simctl list devices available 2>/dev/null | grep -F "$name" | head -1 || true)"
		if [[ -n "$line" ]]; then udid_from_line "$line"; return 0; fi
		dtype="$(device_type_id "$name")" || return 1
		rtid="$(pick_runtime_id "$runtime_pref")" || return 1
		log "creating simulator: $name on $rtid"
		udid="$(xcrun simctl create "$name" "$dtype" "$rtid" 2>/dev/null)" || return 1
		echo "$udid"
	}

	pick_sim_device() {
		if [[ -n "${SHIREI_IOS_DEVICE:-}" ]]; then echo "$SHIREI_IOS_DEVICE"; return 0; fi
		local udid line
		if [[ -n "$PREF_NAME" ]]; then
			if udid="$(ensure_preferred_device "$PREF_NAME" "$PREF_RUNTIME")"; then echo "$udid"; return 0; fi
		fi
		# Generic fallback: booted iPhone, any available iPhone, then any iPhone/iPad.
		line="$(xcrun simctl list devices available 2>/dev/null | grep iPhone | grep '(Booted)' | head -1 || true)"
		if [[ -n "$line" ]]; then udid_from_line "$line"; return 0; fi
		line="$(xcrun simctl list devices available 2>/dev/null | grep iPhone | head -1 || true)"
		if [[ -n "$line" ]]; then udid_from_line "$line"; return 0; fi
		line="$(xcrun simctl list devices available 2>/dev/null | grep -E 'iPhone|iPad' | head -1 || true)"
		if [[ -n "$line" ]]; then udid_from_line "$line"; return 0; fi
		return 1
	}

	if ! DEVICE="$(pick_sim_device)"; then
		die "no available iOS Simulator device (install a runtime via Xcode → Settings → Platforms)"
	fi

	log "simulator device: $DEVICE"
	STATE="$(xcrun simctl list devices 2>/dev/null | grep "$DEVICE" | head -1 || true)"
	if ! echo "$STATE" | grep -q '(Booted)'; then
		log "booting simulator (first boot can take a few minutes)…"
		xcrun simctl boot "$DEVICE" 2>/dev/null || true
	fi
	open -a Simulator 2>/dev/null || true
	log "waiting until simulator is ready…"
	xcrun simctl bootstatus "$DEVICE" -b

	log "installing $APP_DIR"
	xcrun simctl install "$DEVICE" "$APP_DIR"
	log "launching $BUNDLE_ID"
	if pid="$(xcrun simctl launch "$DEVICE" "$BUNDLE_ID" 2>&1)"; then
		log "launched: $pid"
	else
		die "launch failed: $pid"
	fi
	log "running. rebuild+relaunch: $0 --sim $PKG_ARG"
	exit 0
fi

# ---------------------------------------------------------------------------
# Physical device path: Xcode Automatic Signing + devicectl
# ---------------------------------------------------------------------------
# Work on a *copy* of the host template under BUILD_DIR so the template
# (and any git checkout) is never dirtied with per-app names or .a files.
HOST_WORK="$BUILD_DIR/host-work"
rm -rf "$HOST_WORK"
mkdir -p "$BUILD_DIR"
cp -R "$HOST_TEMPLATE" "$HOST_WORK"
# Drop any stale artifacts if someone copied a dirty tree.
rm -f "$HOST_WORK"/libshirei.a "$HOST_WORK"/*.a 2>/dev/null || true

PROJ="$HOST_WORK/DeviceHost.xcodeproj"
[[ -d "$PROJ" ]] || die "missing $PROJ (host template incomplete)"

cp "$ARCHIVE" "$HOST_WORK/libshirei.a"
/usr/libexec/PlistBuddy -c "Set :CFBundleIdentifier $BUNDLE_ID" "$HOST_WORK/Info.plist" 2>/dev/null || true
/usr/libexec/PlistBuddy -c "Set :CFBundleExecutable $APP_NAME" "$HOST_WORK/Info.plist" 2>/dev/null || true
/usr/libexec/PlistBuddy -c "Set :CFBundleName $APP_DISPLAY" "$HOST_WORK/Info.plist" 2>/dev/null || true
/usr/libexec/PlistBuddy -c "Set :CFBundleDisplayName $APP_DISPLAY" "$HOST_WORK/Info.plist" 2>/dev/null \
	|| /usr/libexec/PlistBuddy -c "Add :CFBundleDisplayName string $APP_DISPLAY" "$HOST_WORK/Info.plist" 2>/dev/null \
	|| true
apply_usage_descriptions "$HOST_WORK/Info.plist"
apply_orientation "$HOST_WORK/Info.plist"

# Pick physical device.
pick_physical_device() {
	if [[ -n "${SHIREI_IOS_DEVICE:-}" ]]; then
		echo "$SHIREI_IOS_DEVICE"
		return 0
	fi
	# Prefer hardware UDID from xctrace (00008… form).
	local line udid
	line="$(xcrun xctrace list devices 2>/dev/null | grep -E 'iPhone|iPad' | grep -v Simulator | head -1 || true)"
	if [[ -n "$line" ]]; then
		udid="$(echo "$line" | grep -oE '\([0-9A-Fa-f-]{20,}\)' | tr -d '()' | head -1)"
		if [[ -n "$udid" ]]; then echo "$udid"; return 0; fi
	fi
	# Fallback: devicectl core-device identifier.
	line="$(xcrun devicectl list devices 2>/dev/null | grep -i 'iPhone\|iPad' | grep -i connected | head -1 || true)"
	if [[ -n "$line" ]]; then
		udid="$(echo "$line" | grep -oE '[A-F0-9]{8}-[A-F0-9]{4}-[A-F0-9]{4}-[A-F0-9]{4}-[A-F0-9]{12}' | head -1)"
		if [[ -n "$udid" ]]; then echo "$udid"; return 0; fi
	fi
	return 1
}

if ! PHYS_DEVICE="$(pick_physical_device)"; then
	cat >&2 <<'EOF'
ios-run: no connected physical iPhone/iPad found.

  Unlock the phone, trust this Mac, keep the cable plugged in, then:
    xcrun devicectl list devices
    xcrun xctrace list devices
EOF
	exit 1
fi

if [[ -z "$DEVELOPMENT_TEAM" ]]; then
	cat >&2 <<'EOF'
ios-run: SHIREI_IOS_TEAM is not set (required for --device).

  Find your Team ID: Xcode → Settings → Accounts → your Apple ID → Team
  Or run the GUI:  go run ./mobilerun

  Then:
    SHIREI_IOS_TEAM=XXXXXXXXXX ./ios-run.sh --device ./demos/theme
EOF
	exit 1
fi

log "physical device: $PHYS_DEVICE"
log "signing with DEVELOPMENT_TEAM=$DEVELOPMENT_TEAM bundle=$BUNDLE_ID app=$APP_DISPLAY"
log "  (first run may create an Apple Development cert + provisioning profile)"

rm -rf "$DERIVED"
mkdir -p "$DERIVED"

# Destination must be the *specific* phone (not generic/platform=iOS) so free-team
# Automatic Signing registers the device UDID into the provisioning profile.
# generic builds sign with a profile that often rejects install (0xe8008012).
DESTINATION="platform=iOS,id=$PHYS_DEVICE"

# -allowProvisioningUpdates creates/refreshes free-team profiles and can
# generate a Development cert when the keychain only has an orphaned one.
# PRODUCT_NAME drives the .app folder + binary name (home-screen label is
# CFBundleDisplayName from Info.plist above).
set +e
xcodebuild \
	-project "$PROJ" \
	-scheme shirei \
	-configuration Debug \
	-destination "$DESTINATION" \
	-derivedDataPath "$DERIVED" \
	-allowProvisioningUpdates \
	-allowProvisioningDeviceRegistration \
	DEVELOPMENT_TEAM="$DEVELOPMENT_TEAM" \
	PRODUCT_BUNDLE_IDENTIFIER="$BUNDLE_ID" \
	PRODUCT_NAME="$APP_NAME" \
	CODE_SIGN_STYLE=Automatic \
	CODE_SIGN_IDENTITY="Apple Development" \
	build 2>&1 | tee "$BUILD_DIR/xcodebuild.log"
XCODE_STATUS=${PIPESTATUS[0]}
set -e

if [[ "$XCODE_STATUS" -ne 0 ]]; then
	cat >&2 <<EOF
ios-run: xcodebuild failed (see $BUILD_DIR/xcodebuild.log).

Common fixes:
  1. Xcode → Settings → Accounts → select your Apple ID → Manage Certificates
     → "+" → Apple Development  (creates cert + private key)
  2. Unlock the phone and tap Trust if prompted
  3. Free Personal Team: only ~3 apps; try reusing bundle id $BUNDLE_ID
  4. Confirm team id: SHIREI_IOS_TEAM=$DEVELOPMENT_TEAM

  Then re-run: $0 --device $PKG_ARG
EOF
	exit 1
fi

# Find the signed .app product.
SIGNED_APP="$(find "$DERIVED" -name "$APP_NAME.app" -type d 2>/dev/null | head -1 || true)"
[[ -n "$SIGNED_APP" && -d "$SIGNED_APP" ]] || die "xcodebuild succeeded but no $APP_NAME.app under $DERIVED"

# Stage a copy for inspection / reinstall.
rm -rf "$APP_DIR"
cp -R "$SIGNED_APP" "$APP_DIR"

# Icon after xcodebuild so we re-sign once with the final payload.
if [[ -n "$ICON_PATH" && -f "$ICON_PATH" ]]; then
	install_app_icon "$ICON_PATH" "$APP_DIR" iphoneos
	# Re-sign with the same identity used by xcodebuild (from the embedded profile).
	# codesign -s - works for sim only; device needs a real identity.
	IDENT="${SHIREI_IOS_IDENTITY:-}"
	if [[ -z "$IDENT" ]]; then
		IDENT="$(security find-identity -v -p codesigning 2>/dev/null | grep 'Apple Development' | head -1 | sed -E 's/.*"([^"]+)".*/\1/' || true)"
	fi
	if [[ -n "$IDENT" ]]; then
		XCENT="$(find "$DERIVED" -name '*.xcent' 2>/dev/null | head -1 || true)"
		if [[ -n "$XCENT" ]]; then
			codesign --force --sign "$IDENT" --entitlements "$XCENT" --timestamp=none --generate-entitlement-der "$APP_DIR" 2>/dev/null \
				|| codesign --force --sign "$IDENT" --timestamp=none --generate-entitlement-der "$APP_DIR" 2>/dev/null \
				|| log "warning: re-sign after icon failed; install may reject the app"
		else
			codesign --force --sign "$IDENT" --timestamp=none --generate-entitlement-der "$APP_DIR" 2>/dev/null \
				|| log "warning: re-sign after icon failed; install may reject the app"
		fi
	else
		log "warning: no Apple Development identity for re-sign after icon"
	fi
fi

log "built+signed $APP_DIR"

if [[ -n "${SHIREI_IOS_NO_LAUNCH:-}" ]]; then
	log "SHIREI_IOS_NO_LAUNCH set; skipping install/launch"
	exit 0
fi

# devicectl accepts the hardware UDID (00008…) or the core-device UUID.
# Prefer the connected device's core-device identifier when we can resolve it.
resolve_core_device_id() {
	local hw="$1"
	# Table output: Name / Hostname / Identifier / State / Model
	local id
	id="$(xcrun devicectl list devices 2>/dev/null | awk -v hw="$hw" '
		$0 ~ hw { for (i=1;i<=NF;i++) if ($i ~ /^[A-F0-9-]{36}$/) { print $i; exit } }
		/connected/ && /iPhone|iPad/ {
			for (i=1;i<=NF;i++) if ($i ~ /^[A-F0-9-]{36}$/) { print $i; exit }
		}
	')"
	if [[ -n "$id" ]]; then
		echo "$id"
		return 0
	fi
	echo "$hw"
}

CORE_ID="$(resolve_core_device_id "$PHYS_DEVICE")"
log "installing on device ($CORE_ID)…"
if ! xcrun devicectl device install app --device "$CORE_ID" "$APP_DIR" 2>&1; then
	xcrun devicectl device install app --device "$PHYS_DEVICE" "$APP_DIR" 2>&1 \
		|| die "devicectl install failed — unlock phone, trust developer cert in Settings → General → VPN & Device Management if prompted"
fi

log "launching ${BUNDLE_ID}..."
if ! xcrun devicectl device process launch --device "$CORE_ID" "$BUNDLE_ID" 2>&1; then
	xcrun devicectl device process launch --device "$PHYS_DEVICE" "$BUNDLE_ID" 2>&1 \
		|| die "launch failed - open the app icon once and trust the developer certificate if iOS asks"
fi

log "running on device. rebuild+relaunch: $0 --device $PKG_ARG"
log "  if iOS blocks the app: Settings → General → VPN & Device Management → trust your developer"
