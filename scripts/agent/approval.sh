#!/usr/bin/env sh
set -eu

cat <<'JSON'
{
  "decision": "block",
  "reason": "Show the actual git diff for the one file just changed and ask: Approve this file? Do not touch another file until the human approves or requests a revision."
}
JSON
