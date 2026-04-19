#!/usr/bin/env sh
set -eu

if [ -n "$(git status --porcelain)" ]; then
  echo "tag: refusing to tag a dirty worktree" >&2
  exit 1
fi

tag="${1:-$(svu next)}"

if git rev-parse "$tag" >/dev/null 2>&1; then
  echo "tag: ${tag} already exists" >&2
  exit 1
fi

echo "tag: creating ${tag} at $(git rev-parse --short HEAD)"
git tag "$tag"
git push origin "$tag"
