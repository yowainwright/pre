#!/usr/bin/env sh
set -e

GOLANGCI_LINT_VERSION="${GOLANGCI_LINT_VERSION:-v2.12.2}"
LEGIBILITY_BIN="${LEGIBILITY_BIN:-./bin/legibility-golangci-lint}"
LINT_BASE_REV="${LINT_BASE_REV:-HEAD}"

strict=0
all=0
setup_only=0

usage() {
  echo "usage: sh scripts/lint.sh [--agent] [--all] [--setup-only]" >&2
}

parse_args() {
  while [ "$#" -gt 0 ]; do
    case "$1" in
      --agent) strict=1 ;;
      --all) all=1 ;;
      --setup-only) setup_only=1 ;;
      -h|--help)
        usage
        return 0
        ;;
      *)
        usage
        return 1
        ;;
    esac
    shift
  done
}

repo_root() {
  git rev-parse --show-toplevel 2>/dev/null
}

run_fmt_check() {
  unformatted="$(gofmt -l .)"
  if [ -n "$unformatted" ]; then
    echo "run 'make fmt' to fix formatting" >&2
    printf "%s\n" "$unformatted" >&2
    return 1
  fi
}

run_vet() {
  go vet ./...
}

build_legibility() {
  go run "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${GOLANGCI_LINT_VERSION}" custom
}

ensure_legibility() {
  if [ -x "$LEGIBILITY_BIN" ] && [ ! ".custom-gcl.yml" -nt "$LEGIBILITY_BIN" ]; then
    return 0
  fi
  build_legibility
  test -x "$LEGIBILITY_BIN"
}

has_changed_go_inputs() {
  git diff --name-only --diff-filter=ACMR "$LINT_BASE_REV" -- \
    '*.go' go.mod go.sum .golangci.yml .custom-gcl.yml Makefile scripts/lint.sh scripts/agent/lint.sh |
    grep -q . && return 0
  git ls-files --others --exclude-standard -- '*.go' | grep -q .
}

should_run_legibility() {
  [ "$all" -eq 1 ] && return 0
  has_changed_go_inputs
}

run_legibility() {
  should_run_legibility || return 0
  ensure_legibility
  issues_flag=""
  [ "$strict" -eq 1 ] || issues_flag="--issues-exit-code=0"
  if [ "$all" -eq 1 ]; then
    "$LEGIBILITY_BIN" run $issues_flag ./...
    return
  fi
  "$LEGIBILITY_BIN" run $issues_flag "--new-from-rev=${LINT_BASE_REV}" ./...
}

run_lint() {
  run_fmt_check
  run_vet
  [ "$setup_only" -eq 1 ] && { ensure_legibility; return; }
  run_legibility
}

main() {
  parse_args "$@"
  root="$(repo_root)" || exit 0
  cd "$root"
  run_lint
}

if [ "${_PRE_LINT_SOURCED:-0}" != "1" ]; then
  main "$@"
fi
