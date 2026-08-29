#!/usr/bin/env sh

_PRE_SETUP_SOURCED=1
. "$(dirname "$0")/../../scripts/setup.sh"
set +e

passed=0
failed=0

check() {
  label="$1"
  expected="$2"
  actual="$3"
  if [ "$expected" = "$actual" ]; then
    printf "ok  %s\n" "$label"
    passed=$((passed + 1))
  else
    printf "FAIL %s\n  want: %s\n  got:  %s\n" "$label" "$expected" "$actual"
    failed=$((failed + 1))
  fi
}

exit_code() {
  "$@" 2>/dev/null; echo $?
}

# cmd_exists
check "cmd_exists finds sh"       "0" "$(exit_code cmd_exists "sh")"
check "cmd_exists finds ls"       "0" "$(exit_code cmd_exists "ls")"
check "cmd_exists rejects fake"   "1" "$(exit_code cmd_exists "notarealcmd_xyz")"

check_deps_missing_release_tools_counts() (
  passed=0
  failed=0
  warned=0
  cmd_exists() {
    case "$1" in
      goreleaser|svu|cosign) return 1 ;;
      *)                     return 0 ;;
    esac
  }
  check_deps >/dev/null
  printf "%s/%s/%s" "$passed" "$warned" "$failed"
)

check "check_deps requires svu" "5/2/1" "$(check_deps_missing_release_tools_counts)"

# gh_authed
check "gh_authed passes with true"  "0" "$(exit_code gh_authed "true")"
check "gh_authed fails with false"  "1" "$(exit_code gh_authed "false")"

# op_authed
check "op_authed passes with true"  "0" "$(exit_code op_authed "true")"
check "op_authed fails with false"  "1" "$(exit_code op_authed "false")"

# hook_path
tmp_dir="$(mktemp -d)"
check "hook_path defaults to pre-commit"  "$tmp_dir/.git/hooks/pre-commit" "$(hook_path "$tmp_dir")"
check "hook_path accepts second arg"      "$tmp_dir/.git/hooks/post-merge"  "$(hook_path "$tmp_dir" "post-merge")"
rm -rf "$tmp_dir"

# agent hook paths
tmp_dir="$(mktemp -d)"
check "agent_lint_hook_path" "$tmp_dir/scripts/agent-lint-hook.sh" "$(agent_lint_hook_path "$tmp_dir")"
check "codex_hooks_path" "$tmp_dir/.codex/hooks.json" "$(codex_hooks_path "$tmp_dir")"
check "claude_settings_path" "$tmp_dir/.claude/settings.json" "$(claude_settings_path "$tmp_dir")"
rm -rf "$tmp_dir"

# pre_commit_content
tmp_content="$(mktemp)"
pre_commit_content > "$tmp_content"
check "pre_commit_content contains fmt-check"  "0" "$(exit_code grep -q "fmt-check" "$tmp_content")"
check "pre_commit_content contains make lint"  "0" "$(exit_code grep -q "make lint" "$tmp_content")"
check "pre_commit_content contains make gosec" "0" "$(exit_code grep -q "make gosec" "$tmp_content")"
check "pre_commit_content contains go build"   "0" "$(exit_code grep -q "go build" "$tmp_content")"
check "pre_commit_content contains go test"    "0" "$(exit_code grep -q "go test" "$tmp_content")"
rm -f "$tmp_content"

# post_merge_content
tmp_content="$(mktemp)"
post_merge_content > "$tmp_content"
check "post_merge_content contains setup.sh"   "0" "$(exit_code grep -q "setup.sh" "$tmp_content")"
rm -f "$tmp_content"

# hook_content dispatches by name
tmp_content="$(mktemp)"
hook_content "pre-commit" > "$tmp_content"
check "hook_content pre-commit contains fmt-check"  "0" "$(exit_code grep -q "fmt-check" "$tmp_content")"
hook_content "post-merge" > "$tmp_content"
check "hook_content post-merge contains setup.sh"   "0" "$(exit_code grep -q "setup.sh" "$tmp_content")"
rm -f "$tmp_content"

# hook_installed
tmp_dir="$(mktemp -d)"
check "hook_installed false when missing" "1" "$(exit_code hook_installed "$tmp_dir/pre-commit")"
touch "$tmp_dir/pre-commit"
check "hook_installed true when present"  "0" "$(exit_code hook_installed "$tmp_dir/pre-commit")"
rm -rf "$tmp_dir"

# install_hook
tmp_dir="$(mktemp -d)"
hook="$tmp_dir/pre-commit"
install_hook "$hook" "pre-commit"
check "install_hook creates file"            "0" "$(exit_code test -f "$hook")"
check "install_hook sets executable"         "0" "$(exit_code test -x "$hook")"
check "install_hook contains fmt-check"      "0" "$(exit_code grep -q "fmt-check" "$hook")"
check "install_hook contains make lint"      "0" "$(exit_code grep -q "make lint" "$hook")"
post_hook="$tmp_dir/post-merge"
install_hook "$post_hook" "post-merge"
check "install_hook post-merge contains setup.sh"  "0" "$(exit_code grep -q "setup.sh" "$post_hook")"
rm -rf "$tmp_dir"

# install_hook is idempotent
tmp_dir="$(mktemp -d)"
hook="$tmp_dir/pre-commit"
install_hook "$hook"
install_hook "$hook"
check "install_hook idempotent" "0" "$(exit_code test -f "$hook")"
rm -rf "$tmp_dir"

# check_hooks installs both pre-commit and post-merge
tmp_dir="$(mktemp -d)"
mkdir -p "$tmp_dir/.git/hooks"
check_hooks "$tmp_dir"
check "check_hooks installs pre-commit"  "0" "$(exit_code test -f "$tmp_dir/.git/hooks/pre-commit")"
check "check_hooks installs post-merge"  "0" "$(exit_code test -f "$tmp_dir/.git/hooks/post-merge")"
rm -rf "$tmp_dir"

# check_agent_hooks installs ignored agent configs and verifies tracked hook
tmp_dir="$(mktemp -d)"
mkdir -p "$tmp_dir/scripts"
printf '#!/usr/bin/env sh\n' > "$tmp_dir/scripts/agent-lint-hook.sh"
chmod +x "$tmp_dir/scripts/agent-lint-hook.sh"
check_agent_hooks "$tmp_dir" >/dev/null
check "check_agent_hooks installs Codex hooks" "0" "$(exit_code grep -q "scripts/agent-lint-hook.sh" "$tmp_dir/.codex/hooks.json")"
check "check_agent_hooks installs Claude settings" "0" "$(exit_code grep -q "scripts/agent-lint-hook.sh" "$tmp_dir/.claude/settings.json")"
rm -rf "$tmp_dir"

tmp_dir="$(mktemp -d)"
mkdir -p "$tmp_dir/scripts"
printf '#!/usr/bin/env sh\n' > "$tmp_dir/scripts/agent-lint-hook.sh"
chmod +x "$tmp_dir/scripts/agent-lint-hook.sh"
mkdir -p "$tmp_dir/.codex"
printf '%s\n' '{"hooks":{"PostToolUse":[{"matcher":"Read","hooks":[]}]}}' > "$tmp_dir/.codex/hooks.json"
check "install_codex_agent_hook merges existing config" "0" "$(exit_code install_codex_agent_hook "$tmp_dir")"
check "install_codex_agent_hook preserves existing hook" "0" "$(exit_code grep -q '"matcher": "Read"' "$tmp_dir/.codex/hooks.json")"
check "install_codex_agent_hook adds lint hook" "0" "$(exit_code grep -q "scripts/agent-lint-hook.sh" "$tmp_dir/.codex/hooks.json")"
rm -rf "$tmp_dir"

tmp_dir="$(mktemp -d)"
mkdir -p "$tmp_dir/scripts"
printf '#!/usr/bin/env sh\n' > "$tmp_dir/scripts/agent-lint-hook.sh"
chmod +x "$tmp_dir/scripts/agent-lint-hook.sh"
mkdir -p "$tmp_dir/.claude"
printf '%s\n' '{"hooks":{"PostToolUse":[{"matcher":"Read","hooks":[]}]}}' > "$tmp_dir/.claude/settings.json"
check "install_claude_agent_hook merges existing config" "0" "$(exit_code install_claude_agent_hook "$tmp_dir")"
check "install_claude_agent_hook preserves existing hook" "0" "$(exit_code grep -q '"matcher": "Read"' "$tmp_dir/.claude/settings.json")"
check "install_claude_agent_hook adds lint hook" "0" "$(exit_code grep -q "scripts/agent-lint-hook.sh" "$tmp_dir/.claude/settings.json")"
rm -rf "$tmp_dir"

# secrets target
check "secrets requires a token" "0" "$(exit_code grep -Fq 'test -n "$$HOMEBREW_TAP_TOKEN"' Makefile)"
check "secrets passes token through stdin" "0" "$(exit_code grep -Fq "printf '%s'" Makefile)"
check "secrets avoids --body argv" "1" "$(exit_code grep -Fq 'gh secret set --body' Makefile)"
check "Makefile exposes lint-agent" "0" "$(exit_code grep -Fq "lint-agent:" Makefile)"
check "Makefile exposes lint-agent-all" "0" "$(exit_code grep -Fq "lint-agent-all:" Makefile)"

printf "\n%d passed, %d failed\n" "$passed" "$failed"
[ "$failed" -eq 0 ]
