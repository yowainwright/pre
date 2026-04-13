#!/usr/bin/env sh
set -e

passed=0
failed=0
warned=0

ok()   { printf "ok  %s\n"      "$1"; passed=$((passed + 1)); }
fail() { printf "FAIL %s\n  %s\n" "$1" "$2"; failed=$((failed + 1)); }
warn() { printf "warn %s\n  %s\n" "$1" "$2"; warned=$((warned + 1)); }

check_cmd() {
  label="$1"
  cmd="$2"
  hint="$3"
  if command -v "$cmd" >/dev/null 2>&1; then
    ok "$label"
  else
    fail "$label" "not found — $hint"
  fi
}

check_cmd_optional() {
  label="$1"
  cmd="$2"
  hint="$3"
  if command -v "$cmd" >/dev/null 2>&1; then
    ok "$label"
  else
    warn "$label" "not found (optional) — $hint"
  fi
}

echo "--- deps"
check_cmd     "go"     "go"     "https://go.dev/dl"
check_cmd     "git"    "git"    "brew install git"
check_cmd     "make"   "make"   "brew install make"
check_cmd     "gh"     "gh"     "brew install gh"
check_cmd     "op"     "op"     "brew install 1password-cli"
check_cmd_optional "cosign" "cosign" "brew install cosign"

echo ""
echo "--- gh auth"
if gh auth status >/dev/null 2>&1; then
  ok "gh authenticated"
else
  fail "gh authenticated" "run: gh auth login"
fi

echo ""
echo "--- op auth"
if op account list >/dev/null 2>&1; then
  ok "op authenticated"
else
  fail "op authenticated" "run: op signin"
fi

echo ""
echo "--- env secrets"
env_file="$(dirname "$0")/../.env.example"

check_op_ref() {
  label="$1"
  ref="$2"
  val="$(op run --env-file "$env_file" -- sh -c "echo \$$label" 2>/dev/null)"
  if [ -n "$val" ]; then
    ok "$label resolves"
  else
    fail "$label resolves" "check $ref in .env.example"
  fi
}

if command -v op >/dev/null 2>&1 && op account list >/dev/null 2>&1; then
  check_op_ref "HOMEBREW_TAP_TOKEN" "op://Private/homebrew-tap-pat/token"
  check_op_ref "CODECOV_TOKEN"      "op://Private/codecov-pre/token"
else
  warn "env secrets" "skipped — op not authenticated"
fi

echo ""
echo "--- github secrets"
repo="yowainwright/pre"
if command -v gh >/dev/null 2>&1 && gh auth status >/dev/null 2>&1; then
  secrets="$(gh secret list --repo "$repo" 2>/dev/null)"
  for s in HOMEBREW_TAP_TOKEN CODECOV_TOKEN; do
    if echo "$secrets" | grep -q "^$s"; then
      ok "$s set on $repo"
    else
      warn "$s set on $repo" "run: op run --env-file .env.example -- make secrets"
    fi
  done
else
  warn "github secrets" "skipped — gh not authenticated"
fi

echo ""
printf "%d ok  %d warned  %d failed\n" "$passed" "$warned" "$failed"
[ "$failed" -eq 0 ]
