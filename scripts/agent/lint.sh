#!/usr/bin/env sh
set -u

has_lint_project() {
  [ -f go.mod ] && [ -f scripts/lint.sh ]
}

main() {
  root="$(git rev-parse --show-toplevel 2>/dev/null)" || return 0
  cd "$root" || return 0
  has_lint_project && project_status=0 || project_status=$?
  case "$project_status" in
  0) ./scripts/lint.sh --hook ;;
  *) printf '{}\n' ;;
  esac
}

main
