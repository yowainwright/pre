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
check "hook_path builds path" "$tmp_dir/.git/hooks/pre-commit" "$(hook_path "$tmp_dir")"
rm -rf "$tmp_dir"

# hook_installed
tmp_dir="$(mktemp -d)"
check "hook_installed false when missing" "1" "$(exit_code hook_installed "$tmp_dir/pre-commit")"
touch "$tmp_dir/pre-commit"
check "hook_installed true when present"  "0" "$(exit_code hook_installed "$tmp_dir/pre-commit")"
rm -rf "$tmp_dir"

# install_hook
tmp_dir="$(mktemp -d)"
hook="$tmp_dir/pre-commit"
install_hook "$hook"
check "install_hook creates file"       "0" "$(exit_code test -f "$hook")"
check "install_hook sets executable"    "0" "$(exit_code test -x "$hook")"
check "install_hook contains gofmt"     "0" "$(exit_code grep -q gofmt "$hook")"
check "install_hook contains go vet"    "0" "$(exit_code grep -q "go vet" "$hook")"
rm -rf "$tmp_dir"

# install_hook is idempotent
tmp_dir="$(mktemp -d)"
hook="$tmp_dir/pre-commit"
install_hook "$hook"
install_hook "$hook"
check "install_hook idempotent" "0" "$(exit_code test -f "$hook")"
rm -rf "$tmp_dir"

printf "\n%d passed, %d failed\n" "$passed" "$failed"
[ "$failed" -eq 0 ]
