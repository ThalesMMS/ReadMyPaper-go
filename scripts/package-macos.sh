#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT_DIR"

APP_NAME="${APP_NAME:-}"
BUNDLE_ID="${BUNDLE_ID:-}"
VERSION="${VERSION:-}"
BUILD="${BUILD:-}"
EXECUTABLE_NAME="${EXECUTABLE_NAME:-readmypaper}"
CMD_PATH="${CMD_PATH:-./cmd/readmypaper}"
DIST_DIR="${DIST_DIR:-$ROOT_DIR/dist}"
WORK_DIR="${WORK_DIR:-$ROOT_DIR/dist/package-macos-work}"
ARCH="${ARCH:-$(uname -m)}"
PYTHON_STANDALONE_REPO="${PYTHON_STANDALONE_REPO:-astral-sh/python-build-standalone}"
PYTHON_VERSION_PREFIX="${PYTHON_VERSION_PREFIX:-3.12}"
CODESIGN_IDENTITY="${CODESIGN_IDENTITY:-}"
CODESIGN_ENTITLEMENTS="${CODESIGN_ENTITLEMENTS:-}"
NOTARY_PROFILE="${NOTARY_PROFILE:-}"
DRY_RUN=0

usage() {
	cat <<'EOF'
Usage: scripts/package-macos.sh [options]

Build a self-contained macOS ReadMyPaper.app bundle with embedded Python TTS.

Options:
  --arch arm64|x86_64|universal  Target architecture (default: host arch)
  --dist DIR                     Output directory (default: ./dist)
  --work DIR                     Temporary work directory (default: ./dist/package-macos-work)
  --python-repo OWNER/REPO       python-build-standalone repo (default: astral-sh/python-build-standalone)
  --python-version PREFIX        CPython version prefix (default: 3.12)
  --codesign-identity IDENTITY   Developer ID identity for signing
  --notary-profile PROFILE       notarytool keychain profile for notarization
  --dry-run                      Print resolved settings without building
  -h, --help                     Show this help

Environment overrides:
  APP_NAME, BUNDLE_ID, VERSION, BUILD, EXECUTABLE_NAME, CMD_PATH
  DIST_DIR, WORK_DIR, ARCH, PYTHON_STANDALONE_REPO, PYTHON_VERSION_PREFIX
  PYTHON_STANDALONE_URL_ARM64, PYTHON_STANDALONE_URL_X86_64
  CODESIGN_IDENTITY, CODESIGN_ENTITLEMENTS, NOTARY_PROFILE
EOF
}

fail() {
	printf 'error: %s\n' "$*" >&2
	exit 1
}

while [[ $# -gt 0 ]]; do
	case "$1" in
	--arch)
		ARCH="${2:?missing --arch value}"
		shift 2
		;;
	--dist)
		DIST_DIR="${2:?missing --dist value}"
		shift 2
		;;
	--work)
		WORK_DIR="${2:?missing --work value}"
		shift 2
		;;
	--python-repo)
		PYTHON_STANDALONE_REPO="${2:?missing --python-repo value}"
		shift 2
		;;
	--python-version)
		PYTHON_VERSION_PREFIX="${2:?missing --python-version value}"
		shift 2
		;;
	--codesign-identity)
		CODESIGN_IDENTITY="${2:?missing --codesign-identity value}"
		shift 2
		;;
	--notary-profile)
		NOTARY_PROFILE="${2:?missing --notary-profile value}"
		shift 2
		;;
	--dry-run)
		DRY_RUN=1
		shift
		;;
	-h | --help)
		usage
		exit 0
		;;
	*)
		fail "unknown option: $1"
		;;
	esac
done

toml_string() {
	local key="$1"
	local fallback="$2"
	awk -F '"' -v key="$key" -v fallback="$fallback" '
		$0 ~ "^[[:space:]]*" key "[[:space:]]*=" {
			print $2
			found = 1
			exit
		}
		END {
			if (!found) {
				print fallback
			}
		}
	' FyneApp.toml
}

toml_number() {
	local key="$1"
	local fallback="$2"
	awk -F '=' -v key="$key" -v fallback="$fallback" '
		$0 ~ "^[[:space:]]*" key "[[:space:]]*=" {
			value = $2
			gsub(/[[:space:]]/, "", value)
			print value
			found = 1
			exit
		}
		END {
			if (!found) {
				print fallback
			}
		}
	' FyneApp.toml
}

APP_NAME="${APP_NAME:-$(toml_string Name ReadMyPaper)}"
BUNDLE_ID="${BUNDLE_ID:-$(toml_string ID io.github.thalesmms.readmypaper)}"
VERSION="${VERSION:-$(toml_string Version 0.2.0)}"
BUILD="${BUILD:-$(toml_number Build 1)}"

normalize_arch() {
	case "$1" in
	arm64 | aarch64)
		printf 'arm64\n'
		;;
	amd64 | x86_64)
		printf 'x86_64\n'
		;;
	universal)
		printf 'universal\n'
		;;
	*)
		fail "unsupported architecture '$1' (use arm64, x86_64, or universal)"
		;;
	esac
}

goarch_for() {
	case "$1" in
	arm64)
		printf 'arm64\n'
		;;
	x86_64)
		printf 'amd64\n'
		;;
	*)
		fail "no Go architecture mapping for $1"
		;;
	esac
}

python_triple_for() {
	case "$1" in
	arm64)
		printf 'aarch64-apple-darwin\n'
		;;
	x86_64)
		printf 'x86_64-apple-darwin\n'
		;;
	*)
		fail "no Python standalone triple for $1"
		;;
	esac
}

resource_dir_for() {
	local arch="$1"
	if [[ "$ARCH" == "universal" ]]; then
		printf 'python-%s\n' "$arch"
	else
		printf 'python\n'
	fi
}

run() {
	printf '+'
	printf ' %q' "$@"
	printf '\n'
	"$@"
}

require_tool() {
	command -v "$1" >/dev/null 2>&1 || fail "$1 is required"
}

release_asset_url() {
	local arch="$1"
	local override=""
	case "$arch" in
	arm64)
		override="${PYTHON_STANDALONE_URL_ARM64:-}"
		;;
	x86_64)
		override="${PYTHON_STANDALONE_URL_X86_64:-}"
		;;
	esac
	if [[ -n "$override" ]]; then
		printf '%s\n' "$override"
		return
	fi

	local triple
	triple="$(python_triple_for "$arch")"
	local release_json="$WORK_DIR/python-build-standalone-latest.json"
	mkdir -p "$WORK_DIR"
	curl -fsSL "https://api.github.com/repos/$PYTHON_STANDALONE_REPO/releases/latest" -o "$release_json"

	local url=""
	if command -v python3 >/dev/null 2>&1; then
		url="$(
			python3 - "$release_json" "$PYTHON_VERSION_PREFIX" "$triple" <<'PY'
import json
import sys

release_path, version_prefix, triple = sys.argv[1:]
suffix = f"-{triple}-install_only_stripped.tar.gz"
prefix = f"cpython-{version_prefix}"
with open(release_path, encoding="utf-8") as handle:
    release = json.load(handle)
for asset in release.get("assets", []):
    name = asset.get("name", "")
    if name.startswith(prefix) and name.endswith(suffix):
        print(asset["browser_download_url"])
        break
PY
		)"
	elif command -v jq >/dev/null 2>&1; then
		url="$(
			jq -r \
				--arg prefix "cpython-$PYTHON_VERSION_PREFIX" \
				--arg suffix "-$triple-install_only_stripped.tar.gz" \
				'.assets[] | select(.name | startswith($prefix) and endswith($suffix)) | .browser_download_url' \
				"$release_json" | head -n 1
		)"
	else
		fail "python3 or jq is required to select a Python standalone release asset"
	fi

	[[ -n "$url" && "$url" != "null" ]] || fail "no Python standalone asset found for $arch / CPython $PYTHON_VERSION_PREFIX"
	printf '%s\n' "$url"
}

install_python_runtime() {
	local arch="$1"
	local destination="$2"
	local download_url archive archive_name extract_dir
	download_url="$(release_asset_url "$arch")"
	archive_name="${download_url##*/}"
	archive="$WORK_DIR/python-$arch-$archive_name"
	extract_dir="$WORK_DIR/python-$arch"

	mkdir -p "$WORK_DIR"
	if [[ ! -f "$archive" ]]; then
		run curl -fL "$download_url" -o "$archive"
	fi
	rm -rf "$extract_dir" "$destination"
	mkdir -p "$extract_dir" "$(dirname "$destination")"
	run tar -xzf "$archive" -C "$extract_dir"
	run cp -a "$extract_dir/python" "$destination"

	run "$destination/bin/python3" -m pip install --upgrade pip
	run "$destination/bin/python3" -m pip install --no-cache-dir -r requirements-tts.txt
	"$destination/bin/python3" - <<'PY'
import importlib

for module_name in ("piper", "kokoro", "numpy", "soundfile"):
    importlib.import_module(module_name)
print("Python TTS packages import cleanly")
PY
	find "$destination" -name __pycache__ -type d -prune -exec rm -rf {} +
}

build_go_binary() {
	local output="$1"
	local arch="$2"
	local goarch
	goarch="$(goarch_for "$arch")"
	mkdir -p "$(dirname "$output")"
	printf 'Building Go binary for darwin/%s\n' "$goarch"
	GOOS=darwin GOARCH="$goarch" CGO_ENABLED="${CGO_ENABLED:-1}" go build -trimpath -ldflags "-s -w" -o "$output" "$CMD_PATH"
}

write_info_plist() {
	local plist="$1"
	cat >"$plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleDevelopmentRegion</key>
  <string>en</string>
  <key>CFBundleDisplayName</key>
  <string>$APP_NAME</string>
  <key>CFBundleExecutable</key>
  <string>$EXECUTABLE_NAME</string>
  <key>CFBundleIconFile</key>
  <string>Icon.png</string>
  <key>CFBundleIdentifier</key>
  <string>$BUNDLE_ID</string>
  <key>CFBundleInfoDictionaryVersion</key>
  <string>6.0</string>
  <key>CFBundleName</key>
  <string>$APP_NAME</string>
  <key>CFBundlePackageType</key>
  <string>APPL</string>
  <key>CFBundleShortVersionString</key>
  <string>$VERSION</string>
  <key>CFBundleVersion</key>
  <string>$BUILD</string>
  <key>LSMinimumSystemVersion</key>
  <string>12.0</string>
  <key>NSHighResolutionCapable</key>
  <true/>
</dict>
</plist>
EOF
}

sign_if_requested() {
	local app_bundle="$1"
	if [[ -z "$CODESIGN_IDENTITY" ]]; then
		printf 'Skipping codesign; set CODESIGN_IDENTITY to sign the bundle.\n'
		return
	fi

	local sign_args=(--force --timestamp --options runtime --sign "$CODESIGN_IDENTITY")
	if [[ -n "$CODESIGN_ENTITLEMENTS" ]]; then
		sign_args+=(--entitlements "$CODESIGN_ENTITLEMENTS")
	fi

	while IFS= read -r -d '' candidate; do
		if file "$candidate" | grep -q 'Mach-O'; then
			run codesign "${sign_args[@]}" "$candidate"
		fi
	done < <(find "$app_bundle/Contents" -type f \( -name '*.dylib' -o -name '*.so' -o -perm -111 \) -print0)

	run codesign "${sign_args[@]}" "$app_bundle"
	run codesign --verify --deep --strict --verbose=2 "$app_bundle"
}

notarize_if_requested() {
	local app_bundle="$1"
	if [[ -z "$NOTARY_PROFILE" ]]; then
		printf 'Skipping notarization; set NOTARY_PROFILE to submit with notarytool.\n'
		return
	fi
	[[ -n "$CODESIGN_IDENTITY" ]] || fail "NOTARY_PROFILE requires CODESIGN_IDENTITY"

	local zip_path="$DIST_DIR/${APP_NAME}-${VERSION}.zip"
	rm -f "$zip_path"
	run ditto -c -k --keepParent "$app_bundle" "$zip_path"
	run xcrun notarytool submit "$zip_path" --keychain-profile "$NOTARY_PROFILE" --wait
	run xcrun stapler staple "$app_bundle"
}

ARCH="$(normalize_arch "$ARCH")"
if [[ "$ARCH" == "universal" ]]; then
	TARGET_ARCHES=(arm64 x86_64)
else
	TARGET_ARCHES=("$ARCH")
fi

APP_BUNDLE="$DIST_DIR/$APP_NAME.app"

if [[ "$DRY_RUN" == "1" ]]; then
	cat <<EOF
App:              $APP_NAME
Bundle ID:        $BUNDLE_ID
Version:          $VERSION ($BUILD)
Executable:       $EXECUTABLE_NAME
Architecture:     $ARCH
Python repo:      $PYTHON_STANDALONE_REPO
Python version:   $PYTHON_VERSION_PREFIX
Output bundle:    $APP_BUNDLE
Work directory:   $WORK_DIR
Codesign:         ${CODESIGN_IDENTITY:-disabled}
Notarization:     ${NOTARY_PROFILE:-disabled}
EOF
	exit 0
fi

[[ "$(uname -s)" == "Darwin" ]] || fail "macOS packaging must run on macOS"
require_tool curl
require_tool tar
require_tool go
if [[ "$ARCH" == "universal" ]]; then
	require_tool lipo
fi

rm -rf "$APP_BUNDLE"
mkdir -p "$APP_BUNDLE/Contents/MacOS" "$APP_BUNDLE/Contents/Resources" "$WORK_DIR"

if [[ "$ARCH" == "universal" ]]; then
	arch_binaries=()
	for target_arch in "${TARGET_ARCHES[@]}"; do
		binary="$WORK_DIR/$EXECUTABLE_NAME-$target_arch"
		build_go_binary "$binary" "$target_arch"
		arch_binaries+=("$binary")
	done
	run lipo -create "${arch_binaries[@]}" -output "$APP_BUNDLE/Contents/MacOS/$EXECUTABLE_NAME"
else
	build_go_binary "$APP_BUNDLE/Contents/MacOS/$EXECUTABLE_NAME" "$ARCH"
fi
chmod 0755 "$APP_BUNDLE/Contents/MacOS/$EXECUTABLE_NAME"

for target_arch in "${TARGET_ARCHES[@]}"; do
	python_dir="$(resource_dir_for "$target_arch")"
	install_python_runtime "$target_arch" "$APP_BUNDLE/Contents/Resources/$python_dir"
done

run cp assets/Icon.png "$APP_BUNDLE/Contents/Resources/Icon.png"
write_info_plist "$APP_BUNDLE/Contents/Info.plist"
printf 'APPL????' >"$APP_BUNDLE/Contents/PkgInfo"

run "$APP_BUNDLE/Contents/MacOS/$EXECUTABLE_NAME" --version
sign_if_requested "$APP_BUNDLE"
notarize_if_requested "$APP_BUNDLE"

printf '\nCreated %s\n' "$APP_BUNDLE"
