#!/usr/bin/env sh

_PRE_TAG_SOURCED=1
. "$(dirname "$0")/../../scripts/tag.sh"
set +e

passed=0
failed=0

check() {
  label="$1"; expected="$2"; actual="$3"
  if [ "$expected" = "$actual" ]; then
    printf "ok  %s\n" "$label"; passed=$((passed + 1))
  else
    printf "FAIL %s\n  want: %s\n  got:  %s\n" "$label" "$expected" "$actual"
    failed=$((failed + 1))
  fi
}

# runs $@ in a subshell so exit calls don't swallow the exit code
exit_code() { ( "$@" 2>/dev/null ); echo $?; }

# --- validate_tag ---

check "validate_tag accepts v1.0.0"      "0" "$(exit_code validate_tag "v1.0.0")"
check "validate_tag accepts v0.1.2-rc.1" "0" "$(exit_code validate_tag "v0.1.2-rc.1")"
check "validate_tag accepts build metadata" "0" "$(exit_code validate_tag "v1.2.3+build.7")"
check "validate_tag rejects bare number" "1" "$(exit_code validate_tag "1.0.0")"
check "validate_tag rejects empty"       "1" "$(exit_code validate_tag "")"
check "validate_tag rejects short version" "1" "$(exit_code validate_tag "v1.2")"
check "validate_tag rejects leading zero"  "1" "$(exit_code validate_tag "v01.2.3")"
check "validate_tag rejects empty identifier" "1" "$(exit_code validate_tag "v1.2.3-rc..1")"

# --- annotated tags ---

create_tag_fixture() (
  repo="$(mktemp -d)"
  trap 'rm -rf "$repo"' EXIT
  cd "$repo" || exit 1
  git init -q
  git config user.name "pre release test"
  git config user.email "release-test@example.com"
  git commit -q --allow-empty -m "initial"
  git_create_tag "v1.0.0" "Release v1.0.0"
  type="$(git cat-file -t refs/tags/v1.0.0)"
  subject="$(git for-each-ref --format='%(contents:subject)' refs/tags/v1.0.0)"
  printf '%s|%s\n' "$type" "$subject"
)

check "git_create_tag creates an annotated tag" "tag|Release v1.0.0" "$(create_tag_fixture)"

# --- check_prerequisites ---

cmd_exists() { [ "$1" != "gh" ]; }
gh_auth_valid() { return 0; }
check "check_prerequisites requires gh" "1" "$(exit_code check_prerequisites)"

cmd_exists() { [ "$1" != "svu" ]; }
check "check_prerequisites requires svu" "1" "$(exit_code check_prerequisites)"

cmd_exists() { return 0; }
gh_auth_valid() { return 1; }
check "check_prerequisites requires gh auth" "1" "$(exit_code check_prerequisites)"

gh_auth_valid() { return 0; }
check "check_prerequisites passes" "0" "$(exit_code check_prerequisites)"

# --- check_clean ---

git_is_dirty() { return 0; }
check "check_clean fails when dirty"  "1" "$(exit_code check_clean)"

git_is_dirty() { return 1; }
check "check_clean passes when clean" "0" "$(exit_code check_clean)"

# --- check_exists ---

git_tag_exists() { return 0; }
check "check_exists fails when tag exists"  "1" "$(exit_code check_exists "v1.0.0")"

git_tag_exists() { return 1; }
check "check_exists passes when tag is new" "0" "$(exit_code check_exists "v1.0.0")"

# --- release context ---

git_current_branch() { echo "feature"; }
check "check_branch requires main" "1" "$(exit_code check_branch)"

git_current_branch() { echo "main"; }
check "check_branch accepts main" "0" "$(exit_code check_branch)"

gh_repository() { echo "someone/pre"; }
check "check_repository rejects a fork" "1" "$(exit_code check_repository)"

gh_repository() { echo "yowainwright/pre"; }
check "check_repository accepts canonical repo" "0" "$(exit_code check_repository)"

git_fetch_origin() { return 1; }
check "refresh_origin propagates failure" "1" "$(exit_code refresh_origin)"

git_fetch_origin() { return 0; }
check "refresh_origin passes" "0" "$(exit_code refresh_origin)"

git_head_sha() { echo "aaa"; }
git_origin_branch_sha() { echo "bbb"; }
check "check_synced rejects stale main" "1" "$(exit_code check_synced)"

git_origin_branch_sha() { echo "aaa"; }
check "check_synced accepts current main" "0" "$(exit_code check_synced)"

# --- svu ---

svu() { printf '%s\n' "$*"; }
check "svu_current delegates" "current" "$(svu_current)"
check "svu_patch delegates" "patch" "$(svu_patch)"
check "svu_minor delegates" "minor" "$(svu_minor)"
check "svu_major delegates" "major" "$(svu_major)"
check "svu_prerelease delegates" "patch --prerelease alpha" "$(svu_prerelease patch alpha)"

# --- prompt_prerelease ---

svu_prerelease() {
  case "$1/$2" in
    patch/alpha) echo "v1.0.1-alpha" ;;
    patch/beta)  echo "v1.0.1-beta"  ;;
    patch/rc)    echo "v1.0.1-rc"    ;;
    minor/alpha) echo "v1.1.0-alpha" ;;
    minor/beta)  echo "v1.1.0-beta"  ;;
    minor/rc)    echo "v1.1.0-rc"    ;;
    major/alpha) echo "v2.0.0-alpha" ;;
    major/beta)  echo "v2.0.0-beta"  ;;
    major/rc)    echo "v2.0.0-rc"    ;;
  esac
}

read_line() { REPLY="1"; }
check "prompt_prerelease none"              "v1.0.1"         "$(prompt_prerelease patch v1.0.1)"

read_line() { REPLY="2"; }
check "prompt_prerelease alpha"             "v1.0.1-alpha" "$(prompt_prerelease patch v1.0.1)"

read_line() { REPLY="3"; }
check "prompt_prerelease beta"              "v1.0.1-beta"  "$(prompt_prerelease patch v1.0.1)"

read_line() { REPLY="4"; }
check "prompt_prerelease rc"                "v1.0.1-rc"    "$(prompt_prerelease patch v1.0.1)"

read_line() { REPLY=""; }
check "prompt_prerelease empty → none"      "v1.0.1"         "$(prompt_prerelease patch v1.0.1)"

# --- prompt_bump (real stdin via pipe) ---

svu_current() { echo "v1.0.0"; }
svu_patch()   { echo "v1.0.1"; }
svu_minor()   { echo "v1.1.0"; }
svu_major()   { echo "v2.0.0"; }
read_line()   { read -r REPLY; }

check "prompt_bump patch+none"  "v1.0.1"         "$(printf '1\n1\n' | prompt_bump)"
check "prompt_bump minor+none"  "v1.1.0"         "$(printf '2\n1\n' | prompt_bump)"
check "prompt_bump major+none"  "v2.0.0"         "$(printf '3\n1\n' | prompt_bump)"
check "prompt_bump patch+alpha" "v1.0.1-alpha" "$(printf '1\n2\n' | prompt_bump)"
check "prompt_bump minor+beta"  "v1.1.0-beta"  "$(printf '2\n3\n' | prompt_bump)"
check "prompt_bump custom"      "v1.2.3"         "$(printf '4\nv1.2.3\n' | prompt_bump)"
check "prompt_bump raw version" "v9.0.0"         "$(printf 'v9.0.0\n' | prompt_bump)"
check "prompt_bump defaults to patch" "v1.0.1"    "$(printf '\n\n' | prompt_bump)"

# --- tag message and confirmation ---

git_short_sha() { echo "abc1234"; }

read_line() { REPLY=""; }
check "prompt_tag_message defaults" "Release v1.0.0" "$(prompt_tag_message "v1.0.0")"

read_line() { REPLY="Cargo support"; }
check "prompt_tag_message accepts custom message" "Cargo support" "$(prompt_tag_message "v1.0.0")"

read_line() { REPLY="y"; }
check "confirm_release yes passes" "0" "$(exit_code confirm_release "v1.0.0" "Release v1.0.0")"

read_line() { REPLY="N"; }
check "confirm_release no exits 0" "0" "$(exit_code confirm_release "v1.0.0" "Release v1.0.0")"

# --- publish_release ---

git_create_tag() { printf 'create %s %s\n' "$1" "$2"; }
git_push_tag() { printf 'push %s\n' "$1"; }
expected="create v1.0.0 Release v1.0.0
push v1.0.0"
check "publish_release creates annotated tag then pushes" "$expected" "$(publish_release "v1.0.0" "Release v1.0.0")"

git_create_tag() { return 0; }
git_push_tag() { return 1; }
check "publish_release reports push failure" "1" "$(exit_code publish_release "v1.0.0" "Release v1.0.0")"

# --- release workflow ---

gh_release_run_id() { echo "12345"; }
sleep_briefly() { return 0; }
check "wait_for_release_run finds exact run" "12345" "$(wait_for_release_run "v1.0.0" "aaa")"

gh_release_run_id() { return 1; }
check "wait_for_release_run propagates query failure" "1" "$(exit_code wait_for_release_run "v1.0.0" "aaa")"

gh_release_run_id() { return 0; }
check "wait_for_release_run times out" "1" "$(exit_code wait_for_release_run "v1.0.0" "aaa")"

wait_for_release_run() { echo "12345"; }
gh_run_url() { echo "https://github.com/yowainwright/pre/actions/runs/12345"; }
gh_watch_run() { printf 'watch %s\n' "$1"; }
check "watch_release watches selected run" "watch 12345" "$(watch_release "v1.0.0" "aaa" 2>/dev/null)"

gh_watch_run() { return 1; }
check "watch_release propagates workflow failure" "1" "$(exit_code watch_release "v1.0.0" "aaa")"

printf "\n%d passed, %d failed\n" "$passed" "$failed"
[ "$failed" -eq 0 ]
