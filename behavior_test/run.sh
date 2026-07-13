#!/usr/bin/env bash
# Run all headless behavior tests. Exit non-zero if any fail.
set -euo pipefail
cd "$(dirname "$0")/.."
fail=0
for dir in behavior_test/*/; do
  name=$(basename "$dir")
  # skip non-packages
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
