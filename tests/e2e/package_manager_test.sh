#!/usr/bin/env bash

set -euo pipefail

readonly E2E_RESET="\033[0m"
readonly E2E_DIM="\033[2m"
readonly E2E_CYAN="\033[36m"
readonly E2E_YELLOW="\033[33m"
readonly E2E_BRIGHT_YELLOW="\033[38;5;226m"
readonly E2E_BRIGHT_RED="\033[91m"
readonly E2E_ORANGE="\033[38;5;208m"
readonly E2E_LIGHT_GRAY="\033[37m"
readonly E2E_TYPE_DELAY="0.03"
readonly E2E_STEP_DELAY="2"
readonly E2E_LOGO="${E2E_BRIGHT_YELLOW}PRE${E2E_RESET}${E2E_BRIGHT_RED}≋${E2E_RESET}${E2E_ORANGE}≈${E2E_RESET}${E2E_YELLOW}~${E2E_RESET}${E2E_LIGHT_GRAY}∿${E2E_RESET}"

e2e_type_command() {
  local text="$1"
  local index=0
  printf '%b' "${E2E_DIM}\$${E2E_RESET} "
  if [ "${PRE_E2E_FAST:-}" = "1" ]; then
    printf '%s\n' "$text"
    return
  fi
  while [ "$index" -lt "${#text}" ]; do
    printf '%s' "${text:$index:1}"
    sleep "$E2E_TYPE_DELAY"
    index=$((index + 1))
  done
  printf '\n'
}

e2e_note() {
  printf '%b\n' "${E2E_DIM}# $1${E2E_RESET}"
}

e2e_pause() {
  printf '\n'
  if [ "${PRE_E2E_FAST:-}" = "1" ]; then
    return
  fi
  sleep "$E2E_STEP_DELAY"
}

e2e_start() {
  local manager="$1"
  source "$HOME/.bashrc"
  printf '%b\n\n' "\n  ${E2E_LOGO}  ${E2E_DIM}${manager} security test${E2E_RESET}"
  e2e_note "pre setup installs a shell hook for ${manager}"
  e2e_type_command "grep -A8 'function ${manager}' ~/.bashrc"
  grep -A8 "function ${manager}" "$HOME/.bashrc"
  e2e_pause
}

e2e_run() {
  local note="$1"
  shift
  e2e_note "$note"
  e2e_type_command "$*"
  "$@"
  e2e_pause
}

e2e_expect_block() {
  local note="$1"
  shift
  e2e_note "$note"
  e2e_type_command "$*"
  if printf 'n\n' | "$@"; then
    printf 'expected pre to block the vulnerable package\n' >&2
    return 1
  fi
  e2e_pause
}

e2e_finish() {
  e2e_type_command "pre status"
  pre status
  printf '%b\n' "\n${E2E_DIM}──────────────────────────────────────────────────────────${E2E_RESET}"
  printf '%b\n' "Install  ${E2E_CYAN}brew install --cask yowainwright/tap/pre${E2E_RESET}"
  printf '%b\n\n' "Docs     ${E2E_DIM}github.com/yowainwright/pre${E2E_RESET}"
  if [ "${PRE_E2E_NONINTERACTIVE:-}" = "1" ]; then
    return
  fi
  exec bash -i
}

e2e_main() {
  local name="${1:-}"
  case "$name" in
    ""|*[!a-z0-9_-]*)
      printf 'invalid test name\n' >&2
      return 64
      ;;
  esac
  local test_dir
  test_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
  local test_path="$test_dir/${name}_test.sh"
  if [ ! -x "$test_path" ]; then
    printf 'unknown test: %s\n' "$name" >&2
    return 64
  fi
  exec "$test_path"
}

if [ "${BASH_SOURCE[0]}" = "$0" ]; then
  e2e_main "$@"
fi
