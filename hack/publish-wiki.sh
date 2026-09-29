#!/usr/bin/env bash
# Builds the wiki pages and shows what would change in the GitHub wiki repository.
# Usage: hack/publish-wiki.sh [--push] [wiki-git-url]
# The URL defaults to the origin remote with .wiki.git. Without --push nothing is sent.
# Prerequisites: the wiki is enabled on the repository and has at least one page (create Home in the web UI once).
set -euo pipefail
cd "$(git rev-parse --show-toplevel)"

push=false
if [ "${1:-}" = "--push" ]; then push=true; shift; fi
url=${1:-}
if [ -z "$url" ]; then
  origin=$(git remote get-url origin 2>/dev/null) || { echo "no origin remote, pass the wiki git URL" >&2; exit 1; }
  url="${origin%.git}.wiki.git"
fi

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
git clone --quiet "$url" "$work/wiki"
pages=$(mktemp -d)
hack/gen-wiki.py "$pages"

find "$work/wiki" -maxdepth 1 -name '*.md' -delete
cp "$pages"/*.md "$work/wiki/"
cd "$work/wiki"
git add -A
if git diff --cached --quiet; then echo "wiki is up to date"; exit 0; fi
git diff --cached --stat
if $push; then
  git commit --quiet -s -m "Update wiki from $(git -C "$OLDPWD" rev-parse --short HEAD)"
  git push --quiet origin HEAD
  echo "pushed"
else
  echo "dry run. Run with --push to publish."
fi
