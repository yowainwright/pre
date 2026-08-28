#!/usr/bin/env sh
set -u

root="$(git rev-parse --show-toplevel 2>/dev/null)" || exit 0
cd "$root" || exit 0

if [ ! -f go.mod ] || [ ! -f scripts/lint.sh ]; then
  printf '{}\n'
  exit 0
fi

sh scripts/lint.sh --agent
status="$?"
if [ "$status" -ne 0 ]; then
  exit "$status"
fi

printf '{}\n'
