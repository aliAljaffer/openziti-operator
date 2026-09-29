#!/usr/bin/env bash
# Fails when internal data reaches the tree.
# Usage: hack/check-hygiene.sh [patterns-file]
# The patterns file has one extended regex per line. It is private and never committed.
# In CI, pass the patterns in the HYGIENE_PATTERNS environment variable (a secret).
set -euo pipefail

cd "$(git rev-parse --show-toplevel)"
fail=0

for f in CURRENT_STATE.md '*.private.yaml'; do
  if [ -n "$(git ls-files -- "$f")" ]; then
    echo "tracked private file: $f" >&2
    fail=1
  fi
done

patterns=$(mktemp)
trap 'rm -f "$patterns"' EXIT
[ -n "${1:-}" ] && cat "$1" >>"$patterns"
[ -n "${HYGIENE_PATTERNS:-}" ] && printf '%s\n' "$HYGIENE_PATTERNS" >>"$patterns"
sed -i.bak '/^[[:space:]]*$/d' "$patterns" && rm -f "$patterns.bak"

if [ -s "$patterns" ]; then
  if git grep -nIiE -f "$patterns" -- . ':!hack/check-hygiene.sh'; then
    echo "internal names found in tracked files" >&2
    fail=1
  fi
else
  echo "no patterns given, only checked for private files" >&2
fi
exit $fail
