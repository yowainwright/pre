#!/usr/bin/env sh
set -u

root="$(git rev-parse --show-toplevel 2>/dev/null)" || exit 0
cd "$root" || exit 0

if [ ! -f go.mod ] || [ ! -f scripts/lint.sh ]; then
  printf '{}\n'
  exit 0
fi

_PRE_LINT_SOURCED=1
. scripts/lint.sh
if ! has_changed_go_inputs; then
  printf '{}\n'
  exit 0
fi

sh scripts/lint.sh --agent
printf '{}\n'
