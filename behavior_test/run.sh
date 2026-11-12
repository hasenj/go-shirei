#!/usr/bin/env bash
# Run all windowed behavior tests with --close. Exit non-zero if any fail.
set -euo pipefail
cd "$(dirname "$0")/.."
bin_dir=$(mktemp -d)
trap 'rm -rf "$bin_dir"' EXIT
fail=0
for dir in behavior_test/*/; do
  name=$(basename "$dir")
  # skip non-packages (e.g. btmode library)
  [[ -f "$dir/main.go" ]] || continue
  echo "-------- $name --------"
  if go -C behavior_test build -o "$bin_dir/$name" "./$name" && "$bin_dir/$name" --close; then
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
