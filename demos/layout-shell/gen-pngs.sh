#!/usr/bin/env bash
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/../.." && pwd)"
OUT="$ROOT/docs/layout-tutorial/images"
mkdir -p "$OUT"
cd "$ROOT"
for s in 01 02 03 04 05 06 07 08 09 10 11 12 13 14 15a 15 16; do
  echo "step$s → $OUT/step$s.png"
  go run "./demos/layout-shell/step$s" --png "$OUT/step$s.png"
done
echo "done"
