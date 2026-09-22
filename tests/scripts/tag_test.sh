#!/usr/bin/env sh

# shellcheck source-path=SCRIPTDIR
_PRE_TAG_SOURCED=1
# shellcheck source=../../scripts/tag.sh
. "$(dirname "$0")/../../scripts/tag.sh"
set +e

passed=0
failed=0

check() {
  label="${1:?}"
  expected="${2-}"
  actual="${3-}"
  case "$actual" in
  "$expected")
    printf "ok  %s\n" "$label"
    passed=$((passed + 1))
    ;;
  *)
    printf "FAIL %s\n  want: %s\n  got:  %s\n" "$label" "$expected" "$actual"
    failed=$((failed + 1))
    ;;
  esac
}

# runs $@ in a subshell so exit calls don't swallow the exit code
exit_code() {
  ("$@" 2>/dev/null)
  echo $?
}

create_tag_fixture() {
  (
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
}
test_validate_tag() {
  check "validate_tag accepts v1.0.0" "0" "$(validate_tag_status "v1.0.0")"
  check "validate_tag accepts v0.1.2-rc.1" "0" "$(validate_tag_status "v0.1.2-rc.1")"
  check "validate_tag accepts build metadata" "0" "$(validate_tag_status "v1.2.3+build.7")"
  check "validate_tag rejects bare number" "1" "$(validate_tag_status "1.0.0")"
  check "validate_tag rejects empty" "1" "$(validate_tag_status "")"
  check "validate_tag rejects short version" "1" "$(validate_tag_status "v1.2")"
  check "validate_tag rejects leading zero" "1" "$(validate_tag_status "v01.2.3")"
  check "validate_tag rejects empty identifier" "1" "$(validate_tag_status "v1.2.3-rc..1")"
  check "validate_tag rejects numeric prerelease leading zero" "1" "$(validate_tag_status "v1.2.3-01")"
  check "validate_tag rejects nested numeric prerelease leading zero" "1" "$(validate_tag_status "v1.2.3-rc.01")"
  check "validate_tag accepts zero prerelease" "0" "$(validate_tag_status "v1.2.3-0")"
  check "validate_tag accepts numeric build leading zero" "0" "$(validate_tag_status "v1.2.3+build.01")"
}

test_annotated_tags() {
  check "git_create_tag creates an annotated tag" "tag|Release v1.0.0" "$(create_tag_fixture)"
}

test_check_prerequisites() {
  cmd_exists() { [ "${1-}" != "gh" ]; }
  gh_auth_valid() { return 0; }
  check "check_prerequisites requires gh" "1" "$(check_prerequisites_status)"

  cmd_exists() { [ "${1-}" != "svu" ]; }
  check "check_prerequisites requires svu" "1" "$(check_prerequisites_status)"

  cmd_exists() { return 0; }
  gh_auth_valid() { return 1; }
  check "check_prerequisites requires gh auth" "1" "$(check_prerequisites_status)"

  gh_auth_valid() { return 0; }
  check "check_prerequisites passes" "0" "$(check_prerequisites_status)"
}

test_check_clean() {
  git_is_dirty() { return 0; }
  check "check_clean fails when dirty" "1" "$(check_clean_status)"

  git_is_dirty() { return 1; }
  check "check_clean passes when clean" "0" "$(check_clean_status)"
}

test_check_exists() {
  git_tag_exists() { return 0; }
  check "check_exists fails when tag exists" "1" "$(check_exists_status "v1.0.0")"

  git_tag_exists() { return 1; }
  check "check_exists passes when tag is new" "0" "$(check_exists_status "v1.0.0")"
}

test_release_context() {
  git_current_branch() { echo "feature"; }
  check "check_branch requires main" "1" "$(check_branch_status)"

  git_current_branch() { echo "main"; }
  check "check_branch accepts main" "0" "$(check_branch_status)"

  gh_repository() { echo "someone/pre"; }
  check "check_repository rejects a fork" "1" "$(check_repository_status)"

  gh_repository() { echo "yowainwright/pre"; }
  check "check_repository accepts canonical repo" "0" "$(check_repository_status)"

}

test_release_sync() {
  git_fetch_origin() { return 1; }
  check "refresh_origin propagates failure" "1" "$(refresh_origin_status)"

  git_fetch_origin() { return 0; }
  check "refresh_origin passes" "0" "$(refresh_origin_status)"

  git_head_sha() { echo "aaa"; }
  git_origin_branch_sha() { echo "bbb"; }
  check "check_synced rejects stale main" "1" "$(check_synced_status)"

  git_origin_branch_sha() { echo "aaa"; }
  check "check_synced accepts current main" "0" "$(check_synced_status)"
}

test_svu() {
  svu() { printf '%s\n' "$*"; }
  check "svu_current delegates" "current" "$(svu_current)"
  check "svu_patch delegates" "patch" "$(svu_patch)"
  check "svu_minor delegates" "minor" "$(svu_minor)"
  check "svu_major delegates" "major" "$(svu_major)"
  check "svu_prerelease delegates" "patch --prerelease alpha" "$(svu_prerelease patch alpha)"
}

test_prompt_prerelease() {
  svu_prerelease() {
    case "${1-}/${2-}" in
    patch/alpha) echo "v1.0.1-alpha" ;;
    patch/beta) echo "v1.0.1-beta" ;;
    patch/rc) echo "v1.0.1-rc" ;;
    minor/alpha) echo "v1.1.0-alpha" ;;
    minor/beta) echo "v1.1.0-beta" ;;
    minor/rc) echo "v1.1.0-rc" ;;
    major/alpha) echo "v2.0.0-alpha" ;;
    major/beta) echo "v2.0.0-beta" ;;
    major/rc) echo "v2.0.0-rc" ;;
    esac
  }

  read_line() { REPLY="1"; }
  check "prompt_prerelease none" "v1.0.1" "$(prompt_prerelease patch v1.0.1)"

  read_line() { REPLY="2"; }
  check "prompt_prerelease alpha" "v1.0.1-alpha" "$(prompt_prerelease patch v1.0.1)"

  read_line() { REPLY="3"; }
  check "prompt_prerelease beta" "v1.0.1-beta" "$(prompt_prerelease patch v1.0.1)"

  read_line() { REPLY="4"; }
  check "prompt_prerelease rc" "v1.0.1-rc" "$(prompt_prerelease patch v1.0.1)"

  read_line() { REPLY=""; }
  check "prompt_prerelease empty → none" "v1.0.1" "$(prompt_prerelease patch v1.0.1)"
}

test_prompt_bump_real_stdin_via_pipe() {
  svu_current() { echo "v1.0.0"; }
  svu_patch() { echo "v1.0.1"; }
  svu_minor() { echo "v1.1.0"; }
  svu_major() { echo "v2.0.0"; }
  read_line() { read -r REPLY; }

  check "prompt_bump patch+none" "v1.0.1" "$(printf '1\n1\n' | prompt_bump)"
  check "prompt_bump minor+none" "v1.1.0" "$(printf '2\n1\n' | prompt_bump)"
  check "prompt_bump major+none" "v2.0.0" "$(printf '3\n1\n' | prompt_bump)"
  check "prompt_bump patch+alpha" "v1.0.1-alpha" "$(printf '1\n2\n' | prompt_bump)"
  check "prompt_bump minor+beta" "v1.1.0-beta" "$(printf '2\n3\n' | prompt_bump)"
  check "prompt_bump custom" "v1.2.3" "$(printf '4\nv1.2.3\n' | prompt_bump)"
  check "prompt_bump raw version" "v9.0.0" "$(printf 'v9.0.0\n' | prompt_bump)"
  check "prompt_bump defaults to patch" "v1.0.1" "$(printf '\n\n' | prompt_bump)"
}

test_tag_message_and_confirmation() {
  git_short_sha() { echo "abc1234"; }

  read_line() { REPLY=""; }
  check "prompt_tag_message defaults" "Release v1.0.0" "$(prompt_tag_message "v1.0.0")"

  read_line() { REPLY="Cargo support"; }
  check "prompt_tag_message accepts custom message" "Cargo support" "$(prompt_tag_message "v1.0.0")"

  read_line() { REPLY="y"; }
  check "confirm_release yes passes" "0" "$(confirm_release_status "v1.0.0" "Release v1.0.0")"

  read_line() { REPLY="N"; }
  check "confirm_release no exits 0" "0" "$(confirm_release_status "v1.0.0" "Release v1.0.0")"
}

test_publish_release() {
  git_create_tag() { printf 'create %s %s\n' "${1-}" "${2-}"; }
  git_push_tag() { printf 'push %s\n' "${1-}"; }
  expected="create v1.0.0 Release v1.0.0
push v1.0.0"
  check "publish_release creates annotated tag then pushes" "$expected" "$(publish_release "v1.0.0" "Release v1.0.0")"

  git_create_tag() { return 0; }
  git_push_tag() { return 1; }
  check "publish_release reports push failure" "1" "$(publish_release_status "v1.0.0" "Release v1.0.0")"
}

test_release_workflow() {
  gh_release_run_id() { echo "12345"; }
  sleep_briefly() { return 0; }
  check "wait_for_release_run finds exact run" "12345" "$(wait_for_release_run "v1.0.0" "aaa")"

  gh_release_run_id() { return 1; }
  check "wait_for_release_run propagates query failure" "1" "$(wait_for_release_run_status "v1.0.0" "aaa")"

  gh_release_run_id() { return 0; }
  check "wait_for_release_run times out" "1" "$(wait_for_release_run_status "v1.0.0" "aaa")"

  wait_for_release_run() { echo "12345"; }
  gh_run_url() { echo "https://github.com/yowainwright/pre/actions/runs/12345"; }
  gh_watch_run() { printf 'watch %s\n' "${1-}"; }
  check "watch_release watches selected run" "watch 12345" "$(watch_release "v1.0.0" "aaa" 2>/dev/null)"

  gh_watch_run() { return 1; }
  check "watch_release propagates workflow failure" "1" "$(watch_release_status "v1.0.0" "aaa")"
}

validate_tag_status() {
  (validate_tag "$@") 2>/dev/null
  status=$?
  printf '%s\n' "$status"
}

check_prerequisites_status() {
  (check_prerequisites) 2>/dev/null
  status=$?
  printf '%s\n' "$status"
}

check_clean_status() {
  (check_clean) 2>/dev/null
  status=$?
  printf '%s\n' "$status"
}

check_exists_status() {
  (check_exists "$@") 2>/dev/null
  status=$?
  printf '%s\n' "$status"
}

check_branch_status() {
  (check_branch) 2>/dev/null
  status=$?
  printf '%s\n' "$status"
}

check_repository_status() {
  (check_repository) 2>/dev/null
  status=$?
  printf '%s\n' "$status"
}

refresh_origin_status() {
  (refresh_origin) 2>/dev/null
  status=$?
  printf '%s\n' "$status"
}

check_synced_status() {
  (check_synced) 2>/dev/null
  status=$?
  printf '%s\n' "$status"
}

confirm_release_status() {
  (confirm_release "$@") 2>/dev/null
  status=$?
  printf '%s\n' "$status"
}

publish_release_status() {
  (publish_release "$@") 2>/dev/null
  status=$?
  printf '%s\n' "$status"
}

wait_for_release_run_status() {
  (wait_for_release_run "$@") 2>/dev/null
  status=$?
  printf '%s\n' "$status"
}

watch_release_status() {
  (watch_release "$@") 2>/dev/null
  status=$?
  printf '%s\n' "$status"
}

run_tests() {
  test_validate_tag
  test_annotated_tags
  test_check_prerequisites
  test_check_clean
  test_check_exists
  test_release_context
  test_release_sync
  test_svu
  test_prompt_prerelease
  test_prompt_bump_real_stdin_via_pipe
  test_tag_message_and_confirmation
  test_publish_release
  test_release_workflow
  printf "\n%d passed, %d failed\n" "$passed" "$failed"
  [ "$failed" -eq 0 ]
}

run_tests
