#!/usr/bin/env bash
# Thin wrapper: the real script + host template live under mobilerun/embed/
# (also embedded in the mobilerun GUI). Prefer: go run ./mobilerun
set -euo pipefail
ROOT="$(cd "$(dirname "$0")" && pwd)"
SCRIPT="$ROOT/mobilerun/embed/ios-run.sh"
HOST="$ROOT/mobilerun/embed/ioshost"
[[ -f "$SCRIPT" ]] || { echo "ios-run: missing $SCRIPT" >&2; exit 1; }
export SHIREI_IOS_HOST_DIR="${SHIREI_IOS_HOST_DIR:-$HOST}"
# When invoked from this monorepo checkout, wire local shirei for go.work.
if [[ -z "${SHIREI_MODULE:-}" && -f "$ROOT/go.mod" ]]; then
	export SHIREI_MODULE="$ROOT"
fi
exec bash "$SCRIPT" "$@"
