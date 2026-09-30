#!/usr/bin/env sh
set -e

GOLANGCI_LINT_VERSION="${GOLANGCI_LINT_VERSION:-v2.12.2}"
LEGIBILITY_BIN="${LEGIBILITY_BIN:-./bin/legibility-golangci-lint}"
SHELLCHECK_BIN="${SHELLCHECK_BIN:-shellcheck}"
SHELL_LEGIBILITY_VERSION="0.3.1"
SHELL_LEGIBILITY_SHA256_DARWIN_ARM64="1aabd1c8ca0d45002eaaae850279c565be5f8c5b2299b0f50f93c8d8290c6e46"
SHELL_LEGIBILITY_SHA256_DARWIN_AMD64="1fea29d94b6419f56801ff58dd9a8d156e88f21702d325ac59c5a7e0bfc25dd9"
SHELL_LEGIBILITY_SHA256_LINUX_ARM64="690e9c8e510c3327138e4e4911e1845c16c2722a3a66f64f275572b03e8a651f"
SHELL_LEGIBILITY_SHA256_LINUX_AMD64="82cb4dee7b70947e719f59afeb89a98c03558e1810cf29e6503f0178f74e36bc"
SHELL_LEGIBILITY_DIR="./bin/shellcheck-legibility-${SHELL_LEGIBILITY_VERSION}"
SHELL_LEGIBILITY_BIN="${SHELL_LEGIBILITY_BIN:-${SHELL_LEGIBILITY_DIR}/shellcheck-legibility}"
LINT_BASE_REV="${LINT_BASE_REV:-HEAD}"

strict=0
all=0
setup_only=0
hook_mode=0

usage() {
  echo "usage: sh scripts/lint.sh [--agent] [--all] [--setup-only] [--hook]" >&2
}

parse_args() {
  while [ "$#" -gt 0 ]; do
    case "$1" in
      --agent) strict=1 ;;
      --hook) strict=1; hook_mode=1 ;;
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
  [ -z "$unformatted" ] && return 0
  echo "run 'make fmt' to fix formatting" >&2
  printf "%s\n" "$unformatted" >&2
  return 1
}

run_vet() {
  go vet ./...
}

build_legibility() {
  build_status=0
  build_output="$(go run "github.com/golangci/golangci-lint/v2/cmd/golangci-lint@${GOLANGCI_LINT_VERSION}" custom 2>&1)" || build_status=$?
  [ "$build_status" -eq 0 ] && return 0
  printf '%s\n' "$build_output" >&2
  return "$build_status"
}

legibility_is_current() {
  [ -x "$LEGIBILITY_BIN" ] || return 1
  newer_config="$(find .custom-gcl.yml -newer "$LEGIBILITY_BIN" -print)" || return "$?"
  [ -z "$newer_config" ]
}

ensure_legibility() {
  legibility_is_current && return 0
  build_legibility || return "$?"
  test -x "$LEGIBILITY_BIN"
}

shell_legibility_target() {
  host_os="$(uname -s)"
  host_machine="$(uname -m)"
  case "$host_os" in
    Darwin) target_os="darwin" ;;
    Linux) target_os="linux" ;;
    *) return 1 ;;
  esac
  case "$host_machine" in
    x86_64) target_arch="amd64" ;;
    arm64|aarch64) target_arch="arm64" ;;
    *) return 1 ;;
  esac
  printf '%s-%s\n' "$target_os" "$target_arch"
}

shell_legibility_sha256() {
  case "${1:-}" in
    darwin-arm64) echo "$SHELL_LEGIBILITY_SHA256_DARWIN_ARM64" ;;
    darwin-amd64) echo "$SHELL_LEGIBILITY_SHA256_DARWIN_AMD64" ;;
    linux-arm64) echo "$SHELL_LEGIBILITY_SHA256_LINUX_ARM64" ;;
    linux-amd64) echo "$SHELL_LEGIBILITY_SHA256_LINUX_AMD64" ;;
    *) return 1 ;;
  esac
}

install_shell_legibility() {
  target="$(shell_legibility_target)" || {
    printf 'unsupported platform for shellcheck-legibility\n' >&2
    return 1
  }
  expected_sha="$(shell_legibility_sha256 "$target")" || return "$?"
  archive="${SHELL_LEGIBILITY_DIR}.tar.gz"
  release_url="https://github.com/yowainwright/shellcheck_legibility/releases/download/v${SHELL_LEGIBILITY_VERSION}"
  mkdir -p "$SHELL_LEGIBILITY_DIR" || return "$?"
  curl --fail --location --silent --show-error --retry 3 \
    --proto '=https' --proto-redir '=https' \
    --output "$archive" "${release_url}/shellcheck-legibility-${target}.tar.gz" || return "$?"
  printf '%s  %s\n' "$expected_sha" "$archive" | shasum -a 256 -c - || return "$?"
  tar -xzf "$archive" -C "$SHELL_LEGIBILITY_DIR"
}

ensure_shell_legibility() {
  command -v "$SHELL_LEGIBILITY_BIN" >/dev/null 2>&1 && return 0
  install_shell_legibility || return "$?"
  test -x "$SHELL_LEGIBILITY_BIN"
}

setup_linters() {
  command -v "$SHELLCHECK_BIN" >/dev/null 2>&1 || {
    printf 'missing ShellCheck: install shellcheck before running lint setup\n' >&2
    return 127
  }
  ensure_legibility || return "$?"
  ensure_shell_legibility
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
  ensure_legibility || return "$?"
  issues_flag=""
  [ "$strict" -eq 1 ] || issues_flag="--issues-exit-code=0"
  case "$all" in
    1) "$LEGIBILITY_BIN" run $issues_flag ./... ;;
    *) "$LEGIBILITY_BIN" run $issues_flag "--new-from-rev=${LINT_BASE_REV}" ./... ;;
  esac
}

shell_files() {
  case "$all" in
    1) git ls-files --cached --others --exclude-standard -- '*.sh' '*.bash' ;;
    *)
      git diff --name-only --diff-filter=ACMR "$LINT_BASE_REV" -- '*.sh' '*.bash' || return "$?"
      git ls-files --others --exclude-standard -- '*.sh' '*.bash'
      ;;
  esac
}

run_shellcheck() {
  check_status=0
  "$SHELLCHECK_BIN" --external-sources -- "$@" || check_status=$?
  case "$check_status:$strict" in
    1:0) return 0 ;;
    *) return "$check_status" ;;
  esac
}

run_shell_legibility() {
  ensure_shell_legibility || return "$?"
  set -- check "$@"
  [ "$strict" -eq 1 ] || set -- "$@" --exit-zero
  "$SHELL_LEGIBILITY_BIN" "$@"
}

run_shell_lint() {
  files="$(shell_files)" || return "$?"
  printf '%s\n' "$files" | while IFS= read -r file; do
    [ -f "$file" ] || continue
    run_shellcheck "./$file" || return "$?"
    run_shell_legibility "./$file" || return "$?"
  done
}

run_lint() {
  run_fmt_check || return "$?"
  run_vet || return "$?"
  [ "$setup_only" -eq 1 ] && { setup_linters; return; }
  run_legibility || return "$?"
  run_shell_lint
}

has_changed_inputs() {
  has_changed_go_inputs && return 0
  [ -n "$(shell_files)" ]
}

run_changed_lint() {
  [ -f go.mod ] || return 0
  has_changed_inputs || return 0
  run_lint
}

run_hook() {
  run_changed_lint || return "$?"
  printf '{}\n'
}

main() {
  parse_args "$@"
  root="$(repo_root)" || exit 0
  cd "$root"
  case "$hook_mode" in
    1) run_hook ;;
    *) run_lint ;;
  esac
}

run_script() {
  [ "${_PRE_LINT_SOURCED:-0}" = "1" ] && return 0
  main "$@"
}

run_script "$@"
