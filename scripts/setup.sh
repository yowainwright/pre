#!/usr/bin/env sh
set -e

passed=0
failed=0
warned=0

ok()   { printf "ok  %s\n"        "$1";      passed=$((passed + 1)); }
fail() { printf "FAIL %s\n  %s\n" "$1" "$2"; failed=$((failed + 1)); }
warn() { printf "warn %s\n  %s\n" "$1" "$2"; warned=$((warned + 1)); }

cmd_exists() {
  command -v "$1" >/dev/null 2>&1 || return 1
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
  label="${1:?}"
  env_file="${2:?}"
  val="$(op run --env-file "$env_file" -- sh -c "echo \$$label" 2>/dev/null)"
  [ -n "$val" ]
}

gh_secret_exists() {
  secret="${1:?}"
  repo="${2:-yowainwright/pre}"
  gh secret list --repo "$repo" 2>/dev/null | grep -q "^$secret"
}

hook_installed() {
  [ -f "${1}" ]
}

hook_path() {
  root="${1:-$(git rev-parse --show-toplevel 2>/dev/null)}"
  name="${2:-pre-commit}"
  echo "${root}/.git/hooks/${name}"
}

pre_commit_content() {
  cat <<'HOOK'
#!/usr/bin/env sh
set -e
make fmt-check
make lint
make gosec
go build ./...
go test ./...
HOOK
}

post_merge_content() {
  cat <<'HOOK'
#!/usr/bin/env sh
set -e
./scripts/setup.sh
HOOK
}

hook_content() {
  name="${1:-pre-commit}"
  case "$name" in
    post-merge) post_merge_content ;;
    *)          pre_commit_content ;;
  esac
}

install_hook() {
  hook="${1:?}"
  name="${2:-pre-commit}"
  hook_content "$name" > "$hook"
  chmod +x "$hook"
}

agent_lint_hook_path() {
  root="${1:-$(git rev-parse --show-toplevel 2>/dev/null)}"
  echo "${root}/scripts/agent/lint.sh"
}

codex_hooks_path() {
  root="${1:-$(git rev-parse --show-toplevel 2>/dev/null)}"
  echo "${root}/.codex/hooks.json"
}

claude_settings_path() {
  root="${1:-$(git rev-parse --show-toplevel 2>/dev/null)}"
  echo "${root}/.claude/settings.json"
}

codex_agent_hook_installed() {
  root="${1:-$(git rev-parse --show-toplevel 2>/dev/null)}"
  hook="$(agent_lint_hook_path "$root")"
  settings="$(codex_hooks_path "$root")"
  [ -x "$hook" ] && [ -f "$settings" ] && grep -Fq "scripts/agent/lint.sh" "$settings"
}

claude_agent_hook_installed() {
  root="${1:-$(git rev-parse --show-toplevel 2>/dev/null)}"
  hook="$(agent_lint_hook_path "$root")"
  settings="$(claude_settings_path "$root")"
  [ -x "$hook" ] && [ -f "$settings" ] && grep -Fq "scripts/agent/lint.sh" "$settings"
}

merge_agent_hook_config() {
  settings="${2:?}"
  write_agent_hook_config "$settings"
}

write_agent_hook_config() {
  settings="${1:?}"
  dir="$(dirname "$settings")"
  mkdir -p "$dir" || return 1
  tmp="${settings}.$$"
  write_agent_hook_json "$settings" > "$tmp" || { rm -f "$tmp"; return 1; }
  mv "$tmp" "$settings"
}

write_agent_hook_json() {
  settings="${1:?}"
  matcher="$(write_agent_lint_matcher)"
  [ -e "$settings" ] || { write_new_agent_hook_json "$matcher"; return; }
  jq --slurp --exit-status --argjson matcher "$matcher" '
    select(length == 1) | .[0] | objects | .hooks.PostToolUse += [$matcher]
  ' "$settings"
}

write_new_agent_hook_json() {
  jq --null-input --argjson matcher "${1:?}" '{hooks: {PostToolUse: [$matcher]}}'
}

write_agent_lint_matcher() {
  printf '      {\n'
  printf '        "matcher": "Edit|MultiEdit|Write",\n'
  printf '        "hooks": [\n'
  printf '          {\n'
  printf '            "type": "command",\n'
  printf '            "command": "sh scripts/agent/lint.sh"\n'
  printf '          }\n'
  printf '        ]\n'
  printf '      }\n'
}

install_codex_agent_hook() {
  root="${1:-$(git rev-parse --show-toplevel 2>/dev/null)}"
  settings="$(codex_hooks_path "$root")"
  codex_agent_hook_installed "$root" && return 0
  merge_agent_hook_config "codex" "$settings" &&
    codex_agent_hook_installed "$root"
}

install_claude_agent_hook() {
  root="${1:-$(git rev-parse --show-toplevel 2>/dev/null)}"
  settings="$(claude_settings_path "$root")"
  claude_agent_hook_installed "$root" && return 0
  merge_agent_hook_config "claude" "$settings" &&
    claude_agent_hook_installed "$root"
}

check_required_command() {
  cmd_exists "${1:?}" || {
    fail "$1" "not found — ${2:?}"
    return
  }
  ok "$1"
}

check_optional_command() {
  cmd_exists "${1:?}" || {
    warn "${2:?}" "${3:?}"
    return
  }
  ok "${2:?}"
}

check_deps() {
  echo "--- deps"
  check_required_command go "https://go.dev/dl"
  check_required_command git "brew install git"
  check_required_command make "brew install make"
  check_required_command gh "brew install gh"
  check_required_command jq "install jq for agent settings"
  check_required_command op "brew install 1password-cli"
  check_required_command svu "run: mise install"
  check_optional_command goreleaser "goreleaser (release)" "brew install goreleaser for local release snapshots"
  check_optional_command cosign "cosign (optional)" "brew install cosign"
}

check_gh_auth() {
  gh_authed || {
    fail "gh authenticated" "run: gh auth login"
    return
  }
  ok "gh authenticated"
}

check_op_auth() {
  op_authed || {
    fail "op authenticated" "run: op signin"
    return
  }
  ok "op authenticated"
}

check_auth() {
  echo "--- auth"
  check_gh_auth
  check_op_auth
}

check_env() {
  env_file="${1:-$(dirname "$0")/../.env.example}"
  echo "--- env secrets"
  op_authed || { warn "env secrets" "skipped — op not authenticated"; return; }
  op_ref_resolves "HOMEBREW_TAP_TOKEN" "$env_file" || {
    fail "HOMEBREW_TAP_TOKEN resolves" "check op:// ref in $env_file"
    return
  }
  ok "HOMEBREW_TAP_TOKEN resolves"
}

check_secrets() {
  repo="${1:-yowainwright/pre}"
  echo "--- github secrets"
  gh_authed || { warn "github secrets" "skipped — gh not authenticated"; return; }
  gh_secret_exists "HOMEBREW_TAP_TOKEN" "$repo" || {
    warn "HOMEBREW_TAP_TOKEN set" "run: op run --env-file .env.example -- make secrets"
    return
  }
  ok "HOMEBREW_TAP_TOKEN set"
}

check_hook() {
  hook="${1:?}"
  name="${2:?}"
  hook_installed "$hook" || install_hook "$hook" "$name" || {
    fail "$name hook" "could not write $hook"
    return
  }
  ok "$name hook installed"
}

check_hooks() {
  root="${1:-$(git rev-parse --show-toplevel 2>/dev/null)}"
  echo "--- git hooks"
  for name in pre-commit post-merge; do
    hook="$(hook_path "$root" "$name")"
    check_hook "$hook" "$name"
  done
}

check_codex_agent_hook() {
  install_codex_agent_hook "${1:?}" || {
    fail "Codex agent lint hook" "could not merge scripts/agent/lint.sh into .codex/hooks.json"
    return
  }
  ok "Codex agent lint hook installed"
}

check_claude_agent_hook() {
  install_claude_agent_hook "${1:?}" || {
    fail "Claude agent lint hook" "could not merge scripts/agent/lint.sh into .claude/settings.json"
    return
  }
  ok "Claude agent lint hook installed"
}

check_agent_hooks() {
  root="${1:-$(git rev-parse --show-toplevel 2>/dev/null)}"
  echo "--- agent hooks"
  check_codex_agent_hook "$root"
  check_claude_agent_hook "$root"
}

main() {
  [ "${_PRE_SETUP_SOURCED:-0}" = "1" ] && return 0
  check_deps
  echo ""; check_auth
  echo ""; check_env
  echo ""; check_secrets
  echo ""; check_hooks
  echo ""; check_agent_hooks
  echo ""
  printf "%d ok  %d warned  %d failed\n" "$passed" "$warned" "$failed"
  [ "$failed" -eq 0 ]
}

main "$@"
