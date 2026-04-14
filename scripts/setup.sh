#!/usr/bin/env sh
set -e

passed=0
failed=0
warned=0

ok()   { printf "ok  %s\n"        "$1";      passed=$((passed + 1)); }
fail() { printf "FAIL %s\n  %s\n" "$1" "$2"; failed=$((failed + 1)); }
warn() { printf "warn %s\n  %s\n" "$1" "$2"; warned=$((warned + 1)); }

cmd_exists() {
  command -v "$1" >/dev/null 2>&1
}

gh_authed() {
  check_cmd="${1:-gh auth status}"
  $check_cmd >/dev/null 2>&1
}

op_authed() {
  check_cmd="${1:-op account list}"
  $check_cmd >/dev/null 2>&1
}

op_ref_resolves() {
  label="$1"
  env_file="$2"
  val="$(op run --env-file "$env_file" -- sh -c "echo \$$label" 2>/dev/null)"
  [ -n "$val" ]
}

gh_secret_exists() {
  secret="$1"
  repo="${2:-yowainwright/pre}"
  gh secret list --repo "$repo" 2>/dev/null | grep -q "^$secret"
}

hook_installed() {
  [ -f "${1}" ]
}

hook_path() {
  root="${1:-$(git rev-parse --show-toplevel 2>/dev/null)}"
  echo "${root}/.git/hooks/pre-commit"
}

install_hook() {
  hook="$1"
  cat > "$hook" <<'HOOK'
#!/usr/bin/env sh
set -e
if gofmt -l . | grep -q .; then
  echo "pre-commit: formatting issues, run 'make fmt'"
  exit 1
fi
go vet ./...
HOOK
  chmod +x "$hook"
}

check_deps() {
  echo "--- deps"
  cmd_exists go   && ok "go"   || fail "go"   "not found — https://go.dev/dl"
  cmd_exists git  && ok "git"  || fail "git"  "not found — brew install git"
  cmd_exists make && ok "make" || fail "make" "not found — brew install make"
  cmd_exists gh   && ok "gh"   || fail "gh"   "not found — brew install gh"
  cmd_exists op   && ok "op"   || fail "op"   "not found — brew install 1password-cli"
  cmd_exists cosign && ok "cosign (optional)" || warn "cosign (optional)" "brew install cosign"
}

check_auth() {
  echo "--- auth"
  gh_authed  && ok "gh authenticated" || fail "gh authenticated" "run: gh auth login"
  op_authed  && ok "op authenticated" || fail "op authenticated" "run: op signin"
}

check_env() {
  env_file="${1:-$(dirname "$0")/../.env.example}"
  echo "--- env secrets"
  if cmd_exists op && op_authed; then
    op_ref_resolves "HOMEBREW_TAP_TOKEN" "$env_file" && ok "HOMEBREW_TAP_TOKEN resolves" || fail "HOMEBREW_TAP_TOKEN resolves" "check op:// ref in $env_file"
    op_ref_resolves "CODECOV_TOKEN"      "$env_file" && ok "CODECOV_TOKEN resolves"      || fail "CODECOV_TOKEN resolves"      "check op:// ref in $env_file"
  else
    warn "env secrets" "skipped — op not authenticated"
  fi
}

check_secrets() {
  repo="${1:-yowainwright/pre}"
  echo "--- github secrets"
  if cmd_exists gh && gh_authed; then
    gh_secret_exists "HOMEBREW_TAP_TOKEN" "$repo" && ok "HOMEBREW_TAP_TOKEN set" || warn "HOMEBREW_TAP_TOKEN set" "run: op run --env-file .env.example -- make secrets"
    gh_secret_exists "CODECOV_TOKEN"      "$repo" && ok "CODECOV_TOKEN set"      || warn "CODECOV_TOKEN set"      "run: op run --env-file .env.example -- make secrets"
  else
    warn "github secrets" "skipped — gh not authenticated"
  fi
}

check_hooks() {
  hook="${1:-$(hook_path)}"
  echo "--- git hooks"
  if hook_installed "$hook"; then
    ok "pre-commit hook installed"
  else
    install_hook "$hook" && ok "pre-commit hook installed" || fail "pre-commit hook" "could not write $hook"
  fi
}

main() {
  check_deps
  echo ""; check_auth
  echo ""; check_env
  echo ""; check_secrets
  echo ""; check_hooks
  echo ""
  printf "%d ok  %d warned  %d failed\n" "$passed" "$warned" "$failed"
  [ "$failed" -eq 0 ]
}

if [ "${_PRE_SETUP_SOURCED:-0}" != "1" ]; then
  main "$@"
fi
