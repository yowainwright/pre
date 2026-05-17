#!/usr/bin/env sh
set -eu

# --- injectable primitives (redefine to test) ---

cmd_exists() { command -v "$1" >/dev/null 2>&1; }

svu_available() { cmd_exists svu; }

latest_semver_tag() {
  git tag --list 'v[0-9]*' --sort=-v:refname | sed -n '1p'
}

fallback_current() {
  tag="$(latest_semver_tag)"
  echo "${tag:-v0.0.0}"
}

fallback_bump() {
  version="${1#v}"
  bump="$2"
  pre="${3:-}"
  version="${version%%-*}"
  old_ifs="$IFS"
  IFS=.
  set -- $version
  IFS="$old_ifs"

  major="${1:-0}"
  minor="${2:-0}"
  patch="${3:-0}"
  case "$bump" in
    patch) patch=$((patch + 1)) ;;
    minor) minor=$((minor + 1)); patch=0 ;;
    major) major=$((major + 1)); minor=0; patch=0 ;;
    *) die "invalid bump: $bump" ;;
  esac

  next="v${major}.${minor}.${patch}"
  if [ -n "$pre" ]; then
    next="${next}-${pre}.1"
  fi
  echo "$next"
}

svu_current() {
  if svu_available; then
    svu current
  else
    fallback_current
  fi
}

svu_patch() {
  if svu_available; then
    svu patch
  else
    fallback_bump "$(svu_current)" patch
  fi
}

svu_minor() {
  if svu_available; then
    svu minor
  else
    fallback_bump "$(svu_current)" minor
  fi
}

svu_major() {
  if svu_available; then
    svu major
  else
    fallback_bump "$(svu_current)" major
  fi
}

svu_prerelease() {
  if svu_available; then
    svu "$1" --pre-release "$2"
  else
    fallback_bump "$(svu_current)" "$1" "$2"
  fi
}

git_is_dirty()   { [ -n "$(git status --porcelain)" ]; }
git_tag_exists() { git rev-parse "$1" >/dev/null 2>&1; }
git_short_sha()  { git rev-parse --short HEAD; }
git_create_tag() { git tag "$1"; }
git_push_tag()   { git push origin "$1"; }

read_line()      { read -r REPLY; }

# --- logic ---

die() { printf "tag: %s\n" "$1" >&2; exit 1; }

validate_tag() {
  case "$1" in
    v[0-9]*) ;;
    *) die "version must start with 'v' (got: $1)" ;;
  esac
}

check_clean() {
  git_is_dirty && die "refusing to tag a dirty worktree" || return 0
}

check_exists() {
  git_tag_exists "$1" && die "$1 already exists" || return 0
}

prompt_prerelease() {
  bump="$1"
  base="$2"
  alpha="$(svu_prerelease "$bump" alpha)"
  beta="$(svu_prerelease "$bump" beta)"
  rc="$(svu_prerelease "$bump" rc)"

  printf "\n  pre-release?\n\n" >&2
  printf "  1) none   →  %s\n" "$base"  >&2
  printf "  2) alpha  →  %s\n" "$alpha" >&2
  printf "  3) beta   →  %s\n" "$beta"  >&2
  printf "  4) rc     →  %s\n" "$rc"    >&2
  printf "\n  pre-release [1]: " >&2
  read_line

  case "${REPLY:-1}" in
    1|none)  echo "$base"  ;;
    2|alpha) echo "$alpha" ;;
    3|beta)  echo "$beta"  ;;
    4|rc)    echo "$rc"    ;;
    *) die "invalid choice: $REPLY" ;;
  esac
}

prompt_bump() {
  current="$(svu_current)"
  patch="$(svu_patch)"
  minor="$(svu_minor)"
  major="$(svu_major)"

  printf "\n  current  %s\n\n" "$current" >&2
  printf "  1) patch  →  %s\n" "$patch"   >&2
  printf "  2) minor  →  %s\n" "$minor"   >&2
  printf "  3) major  →  %s\n" "$major"   >&2
  printf "  4) custom\n\n" >&2
  printf "  bump: " >&2
  read_line

  case "$REPLY" in
    1|patch)  prompt_prerelease patch "$patch" ;;
    2|minor)  prompt_prerelease minor "$minor" ;;
    3|major)  prompt_prerelease major "$major" ;;
    4|custom)
      printf "  version (e.g. v1.2.3-beta.1): " >&2
      read_line
      echo "$REPLY"
      ;;
    v[0-9]*) echo "$REPLY" ;;
    *) die "invalid choice: $REPLY" ;;
  esac
}

confirm_tag() {
  printf "\n  tag %s at %s — proceed? [y/N] " "$1" "$(git_short_sha)" >&2
  read_line
  case "$REPLY" in
    y|Y|yes|YES) ;;
    *) printf "  cancelled\n" >&2; exit 0 ;;
  esac
}

main() {
  check_clean

  if [ -n "${1:-}" ]; then
    tag="$1"
  else
    tag="$(prompt_bump)"
  fi

  validate_tag "$tag"
  check_exists "$tag"
  confirm_tag "$tag"

  git_create_tag "$tag"
  git_push_tag "$tag"
  printf "\n  released %s\n" "$tag"
}

if [ "${_PRE_TAG_SOURCED:-0}" != "1" ]; then
  main "$@"
fi
