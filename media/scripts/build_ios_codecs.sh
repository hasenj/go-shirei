#!/usr/bin/env bash
# build_ios_codecs.sh — cross-compile BSD-licensed decoder static libraries
# (libdav1d, libvpx, libopus, libogg, libvorbis) for iOS device and Simulator.
#
# Output:
#   ~/Library/Caches/shirei/ios-codecs/iphoneos/*.a
#   ~/Library/Caches/shirei/ios-codecs/iphonesimulator/*.a
#
# ios-run.sh (used by shirei_mobilerun) links any .a files present there
# automatically — no Go build tags required.
#
# Usage:
#   ./shirei/media/scripts/build_ios_codecs.sh          # both sim and device
#   ./shirei/media/scripts/build_ios_codecs.sh --sim    # simulator only
#   ./shirei/media/scripts/build_ios_codecs.sh --device # physical iPhone only
set -euo pipefail

export PATH="/opt/homebrew/bin:/usr/local/bin:$PATH"

DAV1D_VER="1.5.1"
LIBVPX_VER="1.15.0"
OPUS_VER="1.5.2"
OGG_VER="1.3.5"
VORBIS_VER="1.3.7"

MIN_OS="${SHIREI_IOS_MIN_OS:-16.0}"
OUT_ROOT="${SHIREI_IOS_CODECS_DIR:-${HOME}/Library/Caches/shirei/ios-codecs}"
WORK_ROOT="${HOME}/Library/Caches/shirei/ios-codecs-build"
SRC_DIR="$WORK_ROOT/src"

TARGETS=()
while [[ $# -gt 0 ]]; do
	case "$1" in
		--sim|--simulator) TARGETS+=("iphonesimulator"); shift ;;
		--device) TARGETS+=("iphoneos"); shift ;;
		--all) TARGETS=("iphonesimulator" "iphoneos"); shift ;;
		-h|--help)
			sed -n '2,15p' "$0"
			exit 0
			;;
		*) echo "unknown arg: $1" >&2; exit 2 ;;
	esac
done
if [[ ${#TARGETS[@]} -eq 0 ]]; then
	TARGETS=("iphonesimulator" "iphoneos")
fi

command -v xcrun >/dev/null || { echo "xcrun not found (install Xcode)" >&2; exit 1; }
command -v cmake >/dev/null || { echo "cmake not found (brew install cmake)" >&2; exit 1; }
command -v make >/dev/null || { echo "make not found" >&2; exit 1; }
command -v curl >/dev/null || { echo "curl not found" >&2; exit 1; }

NCPU="$(sysctl -n hw.ncpu 2>/dev/null || echo 4)"
mkdir -p "$SRC_DIR" "$OUT_ROOT"

fetch_tarball() {
	local url="$1" file="$2" dir="$3"
	if [[ -d "$SRC_DIR/$dir" ]]; then
		return 0
	fi
	local archive="$SRC_DIR/$file"
	if [[ ! -f "$archive" ]]; then
		echo "==> downloading $url"
		curl -fL --retry 3 -o "$archive.tmp" "$url"
		mv "$archive.tmp" "$archive"
	fi
	echo "==> extracting $file"
	tar -xf "$archive" -C "$SRC_DIR"
}

fetch_tarball "https://code.videolan.org/videolan/dav1d/-/archive/${DAV1D_VER}/dav1d-${DAV1D_VER}.tar.bz2" "dav1d-${DAV1D_VER}.tar.bz2" "dav1d-${DAV1D_VER}"
fetch_tarball "https://github.com/webmproject/libvpx/archive/refs/tags/v${LIBVPX_VER}.tar.gz" "libvpx-${LIBVPX_VER}.tar.gz" "libvpx-${LIBVPX_VER}"
fetch_tarball "https://downloads.xiph.org/releases/opus/opus-${OPUS_VER}.tar.gz" "opus-${OPUS_VER}.tar.gz" "opus-${OPUS_VER}"
fetch_tarball "https://downloads.xiph.org/releases/ogg/libogg-${OGG_VER}.tar.gz" "libogg-${OGG_VER}.tar.gz" "libogg-${OGG_VER}"
fetch_tarball "https://downloads.xiph.org/releases/vorbis/libvorbis-${VORBIS_VER}.tar.gz" "libvorbis-${VORBIS_VER}.tar.gz" "libvorbis-${VORBIS_VER}"

find_meson() {
	if command -v meson >/dev/null 2>&1; then
		command -v meson
		return 0
	fi
	local venv="$WORK_ROOT/venv"
	if [[ -x "$venv/bin/meson" ]]; then
		echo "$venv/bin/meson"
		return 0
	fi
	if command -v python3 >/dev/null 2>&1; then
		echo "==> installing meson into $venv (only needed for dav1d)" >&2
		python3 -m venv "$venv" >&2
		"$venv/bin/pip" install --quiet meson ninja >&2
		if [[ -x "$venv/bin/meson" ]]; then
			echo "$venv/bin/meson"
			return 0
		fi
	fi
	return 1
}

MESON_BIN=""
if MESON_BIN="$(find_meson)"; then
	:
else
	echo "warning: meson unavailable; skipping libdav1d (brew install meson to enable AV1 on iOS)" >&2
fi

build_for_sdk() {
	local sdk="$1"
	local arch="arm64"
	local minflag="-miphoneos-version-min=${MIN_OS}"
	local vpx_target="arm64-darwin20-gcc"
	local cmake_sysroot
	cmake_sysroot="$(xcrun --sdk "$sdk" --show-sdk-path)"
	local cc cxx ar ranlib strip_bin
	cc="$(xcrun --sdk "$sdk" -f clang)"
	cxx="$(xcrun --sdk "$sdk" -f clang++)"
	ar="$(xcrun --sdk "$sdk" -f ar)"
	ranlib="$(xcrun --sdk "$sdk" -f ranlib)"
	strip_bin="$(xcrun --sdk "$sdk" -f strip)"

	if [[ "$sdk" == "iphonesimulator" ]]; then
		minflag="-mios-simulator-version-min=${MIN_OS}"
		if [[ "$(uname -m)" == "x86_64" ]]; then
			arch="x86_64"
			vpx_target="x86_64-iphonesimulator-gcc"
		fi
	fi

	local out_dir="$OUT_ROOT/$sdk"
	local prefix="$WORK_ROOT/prefix-$sdk"
	local bdir="$WORK_ROOT/build-$sdk"
	mkdir -p "$out_dir" "$prefix" "$bdir"

	local cflags="-arch $arch -isysroot $cmake_sysroot $minflag -O2 -fPIC"
	local cmake_common=(
		-DCMAKE_SYSTEM_NAME=iOS
		-DCMAKE_OSX_SYSROOT="$cmake_sysroot"
		-DCMAKE_OSX_ARCHITECTURES="$arch"
		-DCMAKE_OSX_DEPLOYMENT_TARGET="$MIN_OS"
		-DCMAKE_C_COMPILER="$cc"
		-DCMAKE_CXX_COMPILER="$cxx"
		-DCMAKE_C_FLAGS="$minflag -O2"
		-DCMAKE_INSTALL_PREFIX="$prefix"
		-DBUILD_SHARED_LIBS=OFF
		-DCMAKE_POLICY_VERSION_MINIMUM=3.5
	)

	echo "=== Building BSD codecs for $sdk ($arch) ==="

	# 1. libogg (required by libvorbis)
	echo "--- libogg ($sdk) ---"
	rm -rf "$bdir/ogg"
	cmake -S "$SRC_DIR/libogg-${OGG_VER}" -B "$bdir/ogg" "${cmake_common[@]}" \
		-DINSTALL_DOCS=OFF -DINSTALL_PKG_CONFIG_MODULE=OFF -DINSTALL_CMAKE_PACKAGE_MODULE=OFF
	cmake --build "$bdir/ogg" -j "$NCPU"
	cmake --install "$bdir/ogg"
	cp "$prefix/lib/libogg.a" "$out_dir/libogg.a"

	# 2. libvorbis (decoder only: libvorbis.a; skip vorbisenc/vorbisfile)
	echo "--- libvorbis ($sdk) ---"
	rm -rf "$bdir/vorbis"
	cmake -S "$SRC_DIR/libvorbis-${VORBIS_VER}" -B "$bdir/vorbis" "${cmake_common[@]}" \
		-DOGG_INCLUDE_DIR="$prefix/include" \
		-DOGG_LIBRARY="$prefix/lib/libogg.a"
	cmake --build "$bdir/vorbis" --target vorbis -j "$NCPU"
	cp "$bdir/vorbis/lib/libvorbis.a" "$out_dir/libvorbis.a"

	# 3. libopus
	echo "--- libopus ($sdk) ---"
	rm -rf "$bdir/opus"
	cmake -S "$SRC_DIR/opus-${OPUS_VER}" -B "$bdir/opus" "${cmake_common[@]}" \
		-DOPUS_BUILD_PROGRAMS=OFF -DOPUS_BUILD_TESTING=OFF -DOPUS_BUILD_SHARED_LIBRARY=OFF
	cmake --build "$bdir/opus" -j "$NCPU"
	cp "$bdir/opus/libopus.a" "$out_dir/libopus.a"

	# 4. libvpx (VP8 + VP9 decoders only)
	echo "--- libvpx ($sdk) ---"
	rm -rf "$bdir/vpx"
	mkdir -p "$bdir/vpx"
	(
		cd "$bdir/vpx"
		CC="$cc" CXX="$cxx" AR="$ar" RANLIB="$ranlib" STRIP="$strip_bin" \
		CFLAGS="$cflags" CXXFLAGS="$cflags" LDFLAGS="$cflags" \
		"$SRC_DIR/libvpx-${LIBVPX_VER}/configure" \
			--target="$vpx_target" \
			--prefix="$prefix" \
			--disable-examples \
			--disable-tools \
			--disable-docs \
			--disable-unit-tests \
			--disable-vp8-encoder \
			--disable-vp9-encoder \
			--enable-vp8-decoder \
			--enable-vp9-decoder \
			--enable-vp9-highbitdepth \
			--disable-shared \
			--enable-static
		make -j "$NCPU"
	)
	cp "$bdir/vpx/libvpx.a" "$out_dir/libvpx.a"

	# 5. libdav1d (AV1 decoder)
	if [[ -n "$MESON_BIN" ]]; then
		echo "--- libdav1d ($sdk) ---"
		local cross="$bdir/dav1d-cross.ini"
		local cpu_family="aarch64"
		local cpu="arm64"
		if [[ "$arch" == "x86_64" ]]; then
			cpu_family="x86_64"
			cpu="x86_64"
		fi
		cat >"$cross" <<EOF
[binaries]
c = '$cc'
ar = '$ar'
strip = '$strip_bin'

[built-in options]
c_args = ['-arch', '$arch', '-isysroot', '$cmake_sysroot', '$minflag']
c_link_args = ['-arch', '$arch', '-isysroot', '$cmake_sysroot', '$minflag']

[host_machine]
system = 'darwin'
subsystem = 'ios'
cpu_family = '$cpu_family'
cpu = '$cpu'
endian = 'little'
EOF
		rm -rf "$bdir/dav1d"
		local ninja_dir
		ninja_dir="$(dirname "$MESON_BIN")"
		PATH="$ninja_dir:$PATH" "$MESON_BIN" setup "$bdir/dav1d" "$SRC_DIR/dav1d-${DAV1D_VER}" \
			--cross-file "$cross" \
			--default-library=static \
			--buildtype=release \
			-Denable_tools=false \
			-Denable_tests=false \
			-Denable_examples=false \
			-Denable_docs=false \
			-Denable_asm=true
		PATH="$ninja_dir:$PATH" ninja -C "$bdir/dav1d" src/libdav1d.a
		cp "$bdir/dav1d/src/libdav1d.a" "$out_dir/libdav1d.a"
	fi

	echo "=== Installed into $out_dir ==="
	ls -lh "$out_dir"/*.a
}

for sdk in "${TARGETS[@]}"; do
	build_for_sdk "$sdk"
done
