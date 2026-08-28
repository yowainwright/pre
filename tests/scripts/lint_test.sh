#!/usr/bin/env sh

_PRE_LINT_SOURCED=1
. "$(dirname "$0")/../../scripts/lint.sh"
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
  "$@" >/dev/null 2>&1
  echo $?
}

reset_lint_flags() {
  strict=0
  all=0
  setup_only=0
}

reset_lint_flags
parse_args --agent --all
check "parse_args enables agent strict mode" "1" "$strict"
check "parse_args enables all-files mode" "1" "$all"

reset_lint_flags
parse_args --setup-only
check "parse_args enables setup-only mode" "1" "$setup_only"

reset_lint_flags
check "parse_args rejects unknown option" "1" "$(exit_code parse_args --bad-option)"

run_fmt_check() { return 0; }
run_vet() { return 0; }
should_run_legibility() { return 0; }
ensure_legibility() { ensured=1; }
run_legibility() { return 1; }

reset_lint_flags
check "run_lint treats legibility findings as warnings for humans" "0" "$(exit_code run_lint)"

reset_lint_flags
strict=1
check "run_lint fails legibility findings for agents" "1" "$(exit_code run_lint)"

reset_lint_flags
setup_only=1
ensured=0
run_lint
check "run_lint setup-only builds legibility binary" "1" "$ensured"

printf "\n%d passed, %d failed\n" "$passed" "$failed"
[ "$failed" -eq 0 ]
