#!/usr/bin/env sh
set -eu

RELEASE_BRANCH="${RELEASE_BRANCH:-main}"
RELEASE_REPOSITORY="${RELEASE_REPOSITORY:-yowainwright/pre}"

# --- injectable primitives (redefine to test) ---

cmd_exists() { command -v "$1" >/dev/null 2>&1; }

svu_current()    { svu current; }
svu_patch()      { svu patch; }
svu_minor()      { svu minor; }
svu_major()      { svu major; }
svu_prerelease() { svu "$1" --prerelease "$2"; }

git_is_dirty()          { [ -n "$(git status --porcelain)" ]; }
git_tag_exists()        { git show-ref --verify --quiet "refs/tags/$1"; }
git_current_branch()    { git branch --show-current; }
git_head_sha()          { git rev-parse HEAD; }
git_short_sha()         { git rev-parse --short HEAD; }
git_origin_branch_sha() { git rev-parse "refs/remotes/origin/$RELEASE_BRANCH"; }
git_fetch_origin()      { git fetch --quiet origin "$RELEASE_BRANCH" --tags; }
git_create_tag()        { git tag -a "$1" -m "$2"; }
git_push_tag()          { git push origin "refs/tags/$1"; }

gh_auth_valid() { gh auth status --hostname github.com >/dev/null 2>&1; }
gh_repository() { gh repo view --json nameWithOwner --jq .nameWithOwner; }
gh_release_run_id() {
  gh run list \
    --repo "$RELEASE_REPOSITORY" \
    --workflow release.yml \
    --branch "$1" \
    --commit "$2" \
    --event push \
    --limit 1 \
    --json databaseId \
    --jq '.[0].databaseId // empty'
}
gh_run_url() {
  gh run view "$1" --repo "$RELEASE_REPOSITORY" --json url --jq .url
}
gh_watch_run() {
  gh run watch "$1" --repo "$RELEASE_REPOSITORY" --compact --exit-status
}

run_release_preview() { "${MAKE:-make}" release-preview; }
sleep_briefly()       { sleep 2; }

read_line()      { read -r REPLY; }

# --- logic ---

die() { printf "release: %s\n" "$1" >&2; exit 1; }

check_prerequisites() {
  for executable in git go make gh goreleaser svu; do
    cmd_exists "$executable" || die "$executable is required"
  done

  gh_auth_valid || die "GitHub CLI authentication is required"
}

validate_tag() {
  semver_pattern='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?(\+[0-9A-Za-z-]+(\.[0-9A-Za-z-]+)*)?$'
  printf '%s\n' "$1" | grep -Eq "$semver_pattern" || die "invalid semantic version: $1"
}

check_clean() {
  git_is_dirty && die "refusing to release a dirty worktree"
  return 0
}

check_exists() {
  git_tag_exists "$1" && die "$1 already exists" || return 0
}

check_branch() {
  branch="$(git_current_branch)"
  [ "$branch" = "$RELEASE_BRANCH" ] || die "releases must run from $RELEASE_BRANCH (on $branch)"
}

check_repository() {
  repository="$(gh_repository)" || die "could not identify the GitHub repository"
  [ "$repository" = "$RELEASE_REPOSITORY" ] || die "refusing to release $repository"
}

refresh_origin() {
  git_fetch_origin || die "could not refresh origin/$RELEASE_BRANCH"
}

check_synced() {
  head_sha="$(git_head_sha)"
  origin_sha="$(git_origin_branch_sha)" || die "origin/$RELEASE_BRANCH is unavailable"
  [ "$head_sha" = "$origin_sha" ] || die "HEAD is not synchronized with origin/$RELEASE_BRANCH"
}

check_release_context() {
  check_clean
  check_branch
  check_repository
  refresh_origin
  check_synced
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
  printf "  bump [1]: " >&2
  read_line

  case "${REPLY:-1}" in
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

select_tag() {
  if [ -n "${1:-}" ]; then
    printf '%s\n' "$1"
    return
  fi

  prompt_bump
}

prompt_tag_message() {
  default_message="Release $1"
  printf "\n  tag message [%s]: " "$default_message" >&2
  read_line
  printf '%s\n' "${REPLY:-$default_message}"
}

confirm_release() {
  printf "\n  version     %s\n" "$1" >&2
  printf "  commit      %s\n" "$(git_short_sha)" >&2
  printf "  tag message %s\n" "$2" >&2
  printf "\n  validate, tag, push, and watch the release? [y/N] " >&2
  read_line
  case "$REPLY" in
    y|Y|yes|YES) ;;
    *) printf "  cancelled\n" >&2; exit 0 ;;
  esac
}

preview_release() {
  printf "\n  validating release\n\n" >&2
  run_release_preview
}

recheck_release() {
  check_clean
  refresh_origin
  check_synced
  check_exists "$1"
}

publish_release() {
  git_create_tag "$1" "$2" || die "could not create $1"
  git_push_tag "$1" || die "push failed; $1 remains as a local tag"
}

wait_for_release_run() {
  attempt=0
  while [ "$attempt" -lt 30 ]; do
    run_id="$(gh_release_run_id "$1" "$2")" || die "could not query the release workflow"
    if [ -n "$run_id" ]; then
      printf '%s\n' "$run_id"
      return 0
    fi
    attempt=$((attempt + 1))
    [ "$attempt" -lt 30 ] && sleep_briefly
  done

  die "release workflow did not start for $1"
}

watch_release() {
  printf "\n  waiting for the release workflow\n" >&2
  run_id="$(wait_for_release_run "$1" "$2")"
  run_url="$(gh_run_url "$run_id")" || die "could not load release workflow $run_id"
  printf "  %s\n\n" "$run_url" >&2
  gh_watch_run "$run_id" || die "release workflow failed for $1"
}

main() {
  check_prerequisites
  check_release_context

  tag="$(select_tag "${1:-}")"

  validate_tag "$tag"
  check_exists "$tag"
  message="$(prompt_tag_message "$tag")"
  confirm_release "$tag" "$message"

  preview_release
  recheck_release "$tag"
  head_sha="$(git_head_sha)"
  publish_release "$tag" "$message"
  watch_release "$tag" "$head_sha"
  printf "\n  released %s\n  https://github.com/%s/releases/tag/%s\n" "$tag" "$RELEASE_REPOSITORY" "$tag"
}

if [ "${_PRE_TAG_SOURCED:-0}" != "1" ]; then
  main "$@"
fi
