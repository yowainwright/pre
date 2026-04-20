#!/usr/bin/env sh

_PRE_SETUP_SOURCED=1
. "$(dirname "$0")/../../scripts/setup.sh"
set +e

passed=0
failed=0

check() {
  label="$1"
  expected="$2"
  actual="$3"
  if [ "$expected" = "$actual" ]; then
    printf "ok  %s\n" "$label"
    passed=$((passed + 1))
  else
    printf "FAIL %s\n  want: %s\n  got:  %s\n" "$label" "$expected" "$actual"
    failed=$((failed + 1))
  fi
}

exit_code() {
  "$@" 2>/dev/null; echo $?
}

# cmd_exists
check "cmd_exists finds sh"       "0" "$(exit_code cmd_exists "sh")"
check "cmd_exists finds ls"       "0" "$(exit_code cmd_exists "ls")"
check "cmd_exists rejects fake"   "1" "$(exit_code cmd_exists "notarealcmd_xyz")"

# gh_authed
check "gh_authed passes with true"  "0" "$(exit_code gh_authed "true")"
check "gh_authed fails with false"  "1" "$(exit_code gh_authed "false")"

# op_authed
check "op_authed passes with true"  "0" "$(exit_code op_authed "true")"
check "op_authed fails with false"  "1" "$(exit_code op_authed "false")"

# hook_path
tmp_dir="$(mktemp -d)"
check "hook_path defaults to pre-commit"  "$tmp_dir/.git/hooks/pre-commit" "$(hook_path "$tmp_dir")"
check "hook_path accepts second arg"      "$tmp_dir/.git/hooks/post-merge"  "$(hook_path "$tmp_dir" "post-merge")"
rm -rf "$tmp_dir"

# pre_commit_content
tmp_content="$(mktemp)"
pre_commit_content > "$tmp_content"
check "pre_commit_content contains fmt-check"  "0" "$(exit_code grep -q "fmt-check" "$tmp_content")"
check "pre_commit_content contains make lint"  "0" "$(exit_code grep -q "make lint" "$tmp_content")"
check "pre_commit_content contains go build"   "0" "$(exit_code grep -q "go build" "$tmp_content")"
check "pre_commit_content contains go test"    "0" "$(exit_code grep -q "go test" "$tmp_content")"
rm -f "$tmp_content"

# post_merge_content
tmp_content="$(mktemp)"
post_merge_content > "$tmp_content"
check "post_merge_content contains setup.sh"   "0" "$(exit_code grep -q "setup.sh" "$tmp_content")"
rm -f "$tmp_content"

# hook_content dispatches by name
tmp_content="$(mktemp)"
hook_content "pre-commit" > "$tmp_content"
check "hook_content pre-commit contains fmt-check"  "0" "$(exit_code grep -q "fmt-check" "$tmp_content")"
hook_content "post-merge" > "$tmp_content"
check "hook_content post-merge contains setup.sh"   "0" "$(exit_code grep -q "setup.sh" "$tmp_content")"
rm -f "$tmp_content"

# hook_installed
tmp_dir="$(mktemp -d)"
check "hook_installed false when missing" "1" "$(exit_code hook_installed "$tmp_dir/pre-commit")"
touch "$tmp_dir/pre-commit"
check "hook_installed true when present"  "0" "$(exit_code hook_installed "$tmp_dir/pre-commit")"
rm -rf "$tmp_dir"

# install_hook
tmp_dir="$(mktemp -d)"
hook="$tmp_dir/pre-commit"
install_hook "$hook" "pre-commit"
check "install_hook creates file"            "0" "$(exit_code test -f "$hook")"
check "install_hook sets executable"         "0" "$(exit_code test -x "$hook")"
check "install_hook contains fmt-check"      "0" "$(exit_code grep -q "fmt-check" "$hook")"
check "install_hook contains make lint"      "0" "$(exit_code grep -q "make lint" "$hook")"
post_hook="$tmp_dir/post-merge"
install_hook "$post_hook" "post-merge"
check "install_hook post-merge contains setup.sh"  "0" "$(exit_code grep -q "setup.sh" "$post_hook")"
rm -rf "$tmp_dir"

# install_hook is idempotent
tmp_dir="$(mktemp -d)"
hook="$tmp_dir/pre-commit"
install_hook "$hook"
install_hook "$hook"
check "install_hook idempotent" "0" "$(exit_code test -f "$hook")"
rm -rf "$tmp_dir"

# check_hooks installs both pre-commit and post-merge
tmp_dir="$(mktemp -d)"
mkdir -p "$tmp_dir/.git/hooks"
check_hooks "$tmp_dir"
check "check_hooks installs pre-commit"  "0" "$(exit_code test -f "$tmp_dir/.git/hooks/pre-commit")"
check "check_hooks installs post-merge"  "0" "$(exit_code test -f "$tmp_dir/.git/hooks/post-merge")"
rm -rf "$tmp_dir"

printf "\n%d passed, %d failed\n" "$passed" "$failed"
[ "$failed" -eq 0 ]
