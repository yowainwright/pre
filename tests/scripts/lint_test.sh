#!/usr/bin/env sh

# shellcheck source-path=SCRIPTDIR
_PRE_LINT_SOURCED=1
# shellcheck source=../../scripts/lint.sh
. "$(dirname "$0")/../../scripts/lint.sh"
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

exit_code() {
  "$@" >/dev/null 2>&1
  echo $?
}

reset_lint_state() {
  strict=0
  all=0
  setup_only=0
  hook_mode=0
  LINT_BASE_REV=HEAD
  fake_legibility_status=0
  fake_shellcheck_status=0
  fake_shell_legibility_status=0
  fake_git_status=0
  changed_file="tests/scripts/lint_test.sh"
  untracked_file=""
  captured_args=""
  captured_arg_count=0
}

test_options() {
  reset_lint_state
  parse_args --agent --all
  check "parse_args enables agent strict mode" "1" "$strict"
  check "parse_args enables all-files mode" "1" "$all"
  reset_lint_state
  parse_args --setup-only
  check "parse_args enables setup-only mode" "1" "$setup_only"
  reset_lint_state
  parse_args --hook
  check "parse_args enables hook mode" "1" "$hook_mode"
  check "hook mode enables strict checks" "1" "$strict"
  check "parse_args rejects unknown option" "1" "$(exit_code parse_args --bad-option)"
}

run_fmt_check() { return 0; }
run_vet() { return 0; }
should_run_legibility() { return 0; }
ensure_legibility() { ensured=1; }
has_changed_go_inputs() { return 1; }

fake_legibility() {
  captured_args="$*"
  return "$fake_legibility_status"
}
LEGIBILITY_BIN=fake_legibility

fake_shellcheck() {
  captured_args="$*"
  return "$fake_shellcheck_status"
}
SHELLCHECK_BIN=fake_shellcheck

fake_shell_legibility() {
  captured_args="$*"
  captured_arg_count="$#"
  return "$fake_shell_legibility_status"
}
SHELL_LEGIBILITY_BIN=fake_shell_legibility

git() {
  case "$*" in
    'diff --name-only --diff-filter=ACMR HEAD -- *.sh *.bash')
      printf '%s\n' "$changed_file"
      return "$fake_git_status"
      ;;
    'ls-files --cached --others --exclude-standard -- *.sh *.bash') printf '%s\n' "$changed_file" "$untracked_file" ;;
    'ls-files --others --exclude-standard -- *.sh *.bash') printf '%s\n' "$untracked_file" ;;
    *) return 2 ;;
  esac
}

test_run_lint() {
  reset_lint_state
  check "run_lint passes legibility report mode" "0" "$(exit_code run_lint)"
  fake_legibility_status=2
  check "run_lint propagates legibility runner failures" "2" "$(exit_code run_lint)"
  strict=1
  fake_legibility_status=1
  check "run_lint fails legibility findings for agents" "1" "$(exit_code run_lint)"
  reset_lint_state
  setup_only=1
  ensured=0
  fake_shellcheck_status=127
  run_lint
  check "setup-only skips shell linters" "0" "$?"
  check "run_lint setup-only builds legibility binary" "1" "$ensured"
}

test_go_modes() {
  reset_lint_state
  run_legibility
  check "Go human mode reports findings" "run --issues-exit-code=0 --new-from-rev=HEAD ./..." "$captured_args"
  strict=1
  run_legibility
  check "Go agent mode enforces findings" "run --new-from-rev=HEAD ./..." "$captured_args"
  all=1
  run_legibility
  check "Go all-files mode removes the diff filter" "run ./..." "$captured_args"
}

test_shellcheck_modes() {
  reset_lint_state
  run_shellcheck example.sh
  check "ShellCheck follows sourced project files" "--external-sources -- example.sh" "$captured_args"
  fake_shellcheck_status=1
  check "ShellCheck findings are advisory for humans" "0" "$(exit_code run_shellcheck example.sh)"
  strict=1
  check "ShellCheck findings block agents" "1" "$(exit_code run_shellcheck example.sh)"
  strict=0
  fake_shellcheck_status=2
  check "ShellCheck invocation failures block humans" "2" "$(exit_code run_shellcheck example.sh)"
  fake_shellcheck_status=127
  check "missing ShellCheck blocks humans" "127" "$(exit_code run_shellcheck example.sh)"
}

test_shell_legibility_modes() {
  reset_lint_state
  run_shell_legibility "./file with spaces.sh"
  check "shell legibility is advisory for humans" "check ./file with spaces.sh --exit-zero" "$captured_args"
  check "spaced shell path stays one argument" "3" "$captured_arg_count"
  strict=1
  run_shell_legibility example.sh
  check "shell legibility is strict for agents" "check example.sh" "$captured_args"
  strict=0
  fake_shell_legibility_status=2
  check "shell legibility invocation failures block humans" "2" "$(exit_code run_shell_legibility example.sh)"
  fake_shell_legibility_status=127
  check "missing shell legibility blocks humans" "127" "$(exit_code run_shell_legibility example.sh)"
}

test_shell_selection() {
  reset_lint_state
  untracked_file="new file.sh"
  want="$(printf '%s\n' "$changed_file" "$untracked_file")"
  check "shell selection includes changed and untracked files" "$want" "$(shell_files)"
  all=1
  check "shell all-files selection includes tracked and untracked files" "$want" "$(shell_files)"
  all=0
  fake_git_status=2
  check "shell selection failures stop lint" "2" "$(exit_code run_shell_lint)"
}

test_shell_failures() {
  reset_lint_state
  strict=1
  fake_shellcheck_status=1
  check "agent lint propagates ShellCheck findings" "1" "$(exit_code run_lint)"
  fake_shellcheck_status=0
  fake_shell_legibility_status=1
  check "agent lint propagates shell legibility findings" "1" "$(exit_code run_lint)"
  fake_shell_legibility_status=127
  check "agent lint fails when shell legibility is missing" "127" "$(exit_code run_lint)"
}

test_hook_behavior() {
  reset_lint_state
  parse_args --hook
  fake_shellcheck_status=1
  hook_output="$(run_hook)"
  hook_status="$?"
  check "shell-only changes trigger strict hook lint" "1" "$hook_status"
  check "failed hook does not emit success JSON" "" "$hook_output"
  changed_file=""
  check "unchanged hook skips linters" "0" "$(exit_code run_hook)"
  check "unchanged hook emits JSON" "{}" "$(run_hook)"
}

run_tests() {
  test_options
  test_run_lint
  test_go_modes
  test_shellcheck_modes
  test_shell_legibility_modes
  test_shell_selection
  test_shell_failures
  test_hook_behavior
  printf "\n%d passed, %d failed\n" "$passed" "$failed"
  [ "$failed" -eq 0 ]
}

run_tests
