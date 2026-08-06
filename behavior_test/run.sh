#!/usr/bin/env bash
# Run all headless behavior tests. Exit non-zero if any fail.
# Window modes (--window / --drive / --close) are for the interactive runner;
# see behavior_test/btmode and notes/2026-0804-behavior-test-runner-gui.md.
set -euo pipefail
cd "$(dirname "$0")/.."
fail=0
for dir in behavior_test/*/; do
  name=$(basename "$dir")
  # skip non-packages (e.g. btmode library)
  [[ -f "$dir/main.go" ]] || continue
  echo "-------- $name --------"
  if go run "./$dir"; then
    echo
  else
    echo
    fail=1
  fi
done
if [[ $fail -ne 0 ]]; then
  echo "behavior_test: FAILED"
  exit 1
fi
echo "behavior_test: all passed"
