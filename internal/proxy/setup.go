package proxy

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/yowainwright/pre/internal/fileutil"
	"github.com/yowainwright/pre/internal/manager"
)

const (
	shellHookStart = "# pre security proxy"
	shellHookEnd   = "# end pre security proxy"
)

const refreshShellHookText = `
function _pre_refresh_hooks() {
  local _pre_exit_status=$?
  local _pre_next_version
  _pre_next_version="$(
    hash -r
    command pre --version 2>/dev/null
  )" || return "$_pre_exit_status"
  if [[ "$_pre_next_version" == "$_pre_hook_version" ]]; then
    return "$_pre_exit_status"
  fi
  local _pre_hook
  hash -r
  _pre_hook="$(command pre shell-hook 2>/dev/null)" || return "$_pre_exit_status"
  eval "$_pre_hook"
  return "$_pre_exit_status"
}

function _pre_register_prompt_hook() {
  if [[ -n "${ZSH_VERSION:-}" ]]; then
    autoload -Uz add-zsh-hook
    add-zsh-hook precmd _pre_zsh_prompt_hook
  else
    _pre_add_bash_prompt_hook
  fi
}

function _pre_zsh_prompt_hook() {
  _pre_refresh_hooks || return 0
}

function _pre_add_bash_prompt_hook() {
  local _pre_declaration
  _pre_declaration="$(declare -p PROMPT_COMMAND 2>/dev/null)" || _pre_declaration=""
  if [[ "$_pre_declaration" == "declare -"*a*" PROMPT_COMMAND="* ]]; then
    _pre_add_bash_prompt_array
    return
  fi
  local _pre_pattern=$'(^|[;\n])[[:space:]]*_pre_refresh_hooks[[:space:]]*([;\n]|$)'
  [[ "${PROMPT_COMMAND:-}" =~ $_pre_pattern ]] && return
  PROMPT_COMMAND="_pre_refresh_hooks${PROMPT_COMMAND:+; $PROMPT_COMMAND}"
}

function _pre_add_bash_prompt_array() {
  local _pre_prompt
  for _pre_prompt in "${PROMPT_COMMAND[@]}"; do
    [[ "$_pre_prompt" == _pre_refresh_hooks ]] && return
  done
  PROMPT_COMMAND=(_pre_refresh_hooks "${PROMPT_COMMAND[@]}")
}

_pre_register_prompt_hook
`

const cargoShellHookText = "function %s() {\n" + `  case "${PRE_DISABLE:-}" in
    1|true|TRUE|True|yes|YES|Yes|on|ON|On)
      command %s "$@"
      return
      ;;
  esac
  local _pre_cargo_arg
  local _pre_cargo_command=""
  local _pre_cargo_index=0
  local _pre_cargo_skip=0
  for _pre_cargo_arg in "$@"; do
    _pre_cargo_index=$((_pre_cargo_index + 1))
    if [[ "$_pre_cargo_skip" == "1" ]]; then
      _pre_cargo_skip=0
      continue
    fi
    if [[ "$_pre_cargo_index" == "1" && "$_pre_cargo_arg" == +* ]]; then
      continue
    fi
    case "$_pre_cargo_arg" in
      --color|--config|--explain|--manifest-path|--target-dir|-C|-Z)
		_pre_cargo_skip=1
		;;
      --color=*|--config=*|--explain=*|--manifest-path=*|--target-dir=*|-C?*|-Z?*|-*)
        ;;
      *)
        _pre_cargo_command="$_pre_cargo_arg"
        break
        ;;
    esac
  done
  case "$_pre_cargo_command" in
    add|install|update|fetch)
      %scommand pre cargo "$@"
      ;;
    *)
      command %s "$@"
      ;;
  esac
}
`

const managerShellHookText = `function %s() {
  case "${PRE_DISABLE:-}" in
    1|true|TRUE|True|yes|YES|Yes|on|ON|On)
      command %s "$@"
      return
      ;;
  esac
%s  if %s; then
    %scommand pre %s "$@"
  else
    command %s "$@"
  fi
}
`

const brewUpdateShellHookText = `  case "${1:-}" in
    update | up)
      command %s "$@"
      local _pre_brew_status=$?
      case " $* " in
        *" --help "* | *" -h "* | *" --version "*) return "$_pre_brew_status" ;;
      esac
      if [[ "$_pre_brew_status" == 0 ]]; then
        (
          HOMEBREW_NO_ASK=1 NONINTERACTIVE=1 command %s upgrade pre </dev/null &
        ) >/dev/null 2>&1
      fi
      return "$_pre_brew_status"
      ;;
  esac
`

const aliasShellHookText = `function _pre_alias_error() {
  printf 'pre: cannot safely intercept the %%s alias; use pre %%s directly or resolve the alias conflict\n' "$1" "$1" >&2
  return 1
}

function _pre_run_alias() {
  local _pre_alias_manager="$1"
  local _pre_alias_executable="$2"
  shift 2
  case "${PRE_DISABLE:-}" in
    1|true|TRUE|True|yes|YES|Yes|on|ON|On)
      "$_pre_alias_executable" "$@"
      return
      ;;
  esac
  local _pre_alias_target="${_pre_alias_executable##*/}"
  case "$_pre_alias_target" in
    %s) ;;
    *) _pre_alias_error "$_pre_alias_manager"; return ;;
  esac
  if [[ "$_pre_alias_executable" == "$_pre_alias_target" && "$_pre_alias_target" != "$_pre_alias_manager" ]] &&
     builtin alias "$_pre_alias_target" >/dev/null 2>&1; then
    _pre_alias_error "$_pre_alias_manager"
    return
  fi
  "_pre_alias_$_pre_alias_target" "$@"
}

function _pre_wrap_alias() {
  local _pre_alias_definition
  _pre_alias_definition="$(builtin alias "$1" 2>/dev/null)" || return 0
  # Decode the builtin's quoted value without executing the alias body.
  eval "local _pre_alias_value=${_pre_alias_definition#*=}"
  case "$_pre_alias_value" in
    '_pre_run_alias '*|'_pre_alias_error '*) return 0 ;;
    *[\;\&\|\<\>\$\(\)\{\}\#]*|*$'\x60'*|*$'\n'*|*$'\r'*)
      builtin alias "$1=_pre_alias_error '$1'"
      return
      ;;
  esac
  builtin alias "$1=_pre_run_alias '$1' $_pre_alias_value"
}

`

const shellCommandParserText = `  local _pre_arg
  local _pre_command=""
  local _pre_subcommand=""
  local _pre_skip=0
  for _pre_arg in "$@"; do
    if [[ "$_pre_skip" == "1" ]]; then
      _pre_skip=0
      continue
    fi
    case "$_pre_arg" in
%s      -*) ;;
      *)
        if [[ -z "$_pre_command" ]]; then
          _pre_command="$_pre_arg"
        else
          _pre_subcommand="$_pre_arg"
          break
        fi
        ;;
    esac
  done
`

func Setup(version string) {
	rcFile, err := detectRCFile()
	if err == nil {
		err = setupRCFile(rcFile, version)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "pre setup: %v\n", err)
		processExit(1)
	}
}

func RefreshShellHooks(version string) error {
	rcFile, err := detectRCFile()
	if err != nil {
		return err
	}
	content, err := os.ReadFile(rcFile)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return migrateRCHooks(rcFile, string(content), version)
}

func migrateRCHooks(rcFile, content, version string) error {
	start := strings.Index(content, shellHookStart)
	if start < 0 {
		return nil
	}
	block := content[start:]
	hook := strings.TrimPrefix(ShellHook(version), "\n")
	if strings.HasPrefix(block, hook) {
		return nil
	}
	end := strings.Index(block, shellHookEnd)
	if end < 0 {
		end = legacyHookEnd(block)
	} else {
		end = skipHookLineEnding(block, end+len(shellHookEnd))
	}
	updated := content[:start] + hook
	updated += block[end:]
	return writeRCFile(rcFile, []byte(updated))
}

func setupRCFile(rcFile, version string) error {
	content, err := os.ReadFile(rcFile)
	readFailed := err != nil && !errors.Is(err, os.ErrNotExist)
	if readFailed {
		return err
	}
	if strings.Contains(string(content), shellHookStart) {
		return refreshRCHooks(rcFile, string(content), version)
	}
	return installRCHooks(rcFile, content, "added hooks to", version)
}

func refreshRCHooks(rcFile, content, version string) error {
	cleaned, removed := removeShellHookBlock(content)
	if !removed {
		fmt.Println("pre: already set up in", rcFile)
		return nil
	}
	return installRCHooks(rcFile, []byte(cleaned), "refreshed hooks in", version)
}

func installRCHooks(rcFile string, content []byte, action, version string) error {
	appended := append(content, []byte(ShellHook(version))...)
	if err := writeRCFile(rcFile, appended); err != nil {
		return err
	}
	fmt.Println("pre:", action, rcFile)
	fmt.Println("pre: restart your shell or run: source", rcFile)
	return nil
}

func ShellHook(version string) string {
	var sb strings.Builder
	sb.WriteString("\n" + shellHookStart + "\n")
	quotedVersion := strings.ReplaceAll(version, "'", "'\\''")
	fmt.Fprintf(&sb, "_pre_hook_version='%s'\n", quotedVersion)
	sb.WriteString(aliasShellHook())

	for _, m := range manager.All() {
		sb.WriteString(managerShellHook(m))
		sb.WriteString(aliasManagerShellHook(m))
		fmt.Fprintf(&sb, "_pre_wrap_alias '%s'\n", m.Name)
	}

	sb.WriteString(refreshShellHookText)
	sb.WriteString(shellHookEnd + "\n")
	return sb.String()
}

func managerShellHook(m manager.Manager) string {
	return formatShellHook(m, m.Name, m.Name, "")
}

func aliasManagerShellHook(m manager.Manager) string {
	name := "_pre_alias_" + m.Name
	executable := `"$_pre_alias_executable"`
	environment := `PRE_EXEC_MANAGER="` + m.Name + `" PRE_EXEC_PATH="$_pre_alias_executable" `
	return formatShellHook(m, name, executable, environment)
}

func formatShellHook(m manager.Manager, name, executable, environment string) string {
	if m.Name == "cargo" {
		return fmt.Sprintf(cargoShellHookText, name, executable, environment, executable)
	}
	parser := shellCommandParser(m)
	if m.Name == "brew" {
		parser += fmt.Sprintf(brewUpdateShellHookText, executable, executable)
	}
	condition := shellInstallCondition(m)
	return fmt.Sprintf(managerShellHookText, name, executable, parser, condition, environment, m.Name, executable)
}

func aliasShellHook() string {
	managers := manager.All()
	names := make([]string, len(managers))
	for index, m := range managers {
		names[index] = m.Name
	}
	targets := strings.Join(names, "|")
	return fmt.Sprintf(aliasShellHookText, targets)
}

func shellCommandParser(m manager.Manager) string {
	cases := shellGlobalValueCases(m)
	return fmt.Sprintf(shellCommandParserText, cases)
}

func shellGlobalValueCases(m manager.Manager) string {
	flags := managerGlobalValueFlags(&m)
	if len(flags) == 0 {
		return ""
	}
	attached := make([]string, 0, len(flags))
	for _, flag := range flags {
		pattern := flag + "=*"
		if len(flag) == 2 {
			pattern = flag + "?*"
		}
		attached = append(attached, pattern)
	}
	valueFlags := strings.Join(flags, "|")
	attachedFlags := strings.Join(attached, "|")
	return fmt.Sprintf("      %s) _pre_skip=1 ;;\n      %s) ;;\n", valueFlags, attachedFlags)
}

func shellInstallCondition(m manager.Manager) string {
	conditions := make([]string, len(m.InstallCmds))
	for i, command := range m.InstallCmds {
		conditions[i] = fmt.Sprintf(`"$_pre_command" == "%s"`, command)
	}
	condition := "false"
	if len(conditions) > 0 {
		condition = "[[ " + strings.Join(conditions, ` || `) + " ]]"
	}
	if m.Name == "uv" {
		condition += ` || [[ "$_pre_command" == "pip" && "$_pre_subcommand" == "install" ]]`
	}
	return condition
}

func Teardown() error {
	rcFile, removed, err := RemoveShellHooks()
	if err != nil {
		fmt.Fprintf(os.Stderr, "pre teardown: %v\n", err)
		return err
	}
	if !removed {
		fmt.Println("pre: no hooks found in", rcFile)
		return nil
	}
	fmt.Println("pre: removed hooks from", rcFile)
	fmt.Println("pre: restart your shell or run: source", rcFile)
	return nil
}

func ShellHookStatus() (string, bool) {
	rcFile, err := detectRCFile()
	if err != nil {
		return "", false
	}
	content, err := os.ReadFile(rcFile)
	if err != nil {
		return rcFile, false
	}
	return rcFile, strings.Contains(string(content), shellHookStart)
}

func RemoveShellHooks() (string, bool, error) {
	rcFile, err := detectRCFile()
	if err != nil {
		return "", false, err
	}
	content, err := os.ReadFile(rcFile)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return rcFile, false, nil
		}
		return rcFile, false, err
	}
	return removeRCHooks(rcFile, string(content))
}

func removeRCHooks(rcFile, content string) (string, bool, error) {
	cleaned, removed := removeShellHookBlock(content)
	if !removed {
		return rcFile, false, nil
	}
	if err := writeRCFile(rcFile, []byte(cleaned)); err != nil {
		return rcFile, false, err
	}
	return rcFile, true, nil
}

func writeRCFile(path string, data []byte) error {
	target, err := rcWriteTarget(path)
	if err != nil {
		return err
	}
	perm, err := rcWritePerm(target)
	if err != nil {
		return err
	}
	return fileutil.AtomicWriteFile(target, data, perm)
}

func rcWriteTarget(path string) (string, error) {
	target, err := filepath.EvalSymlinks(path)
	missing := errors.Is(err, os.ErrNotExist)
	if !missing {
		return target, err
	}
	dir, name := filepath.Split(path)
	parent, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", err
	}
	return rcMissingWriteTarget(filepath.Join(parent, name))
}

func rcMissingWriteTarget(path string) (string, error) {
	target, err := os.Readlink(path)
	if errors.Is(err, os.ErrNotExist) {
		return path, nil
	}
	if err != nil {
		return "", err
	}
	relative := !filepath.IsAbs(target)
	if relative {
		dir, _ := filepath.Split(path)
		target = dir + target
	}
	return rcWriteTarget(target)
}

func rcWritePerm(path string) (os.FileMode, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return 0o600, nil
	}
	if err != nil {
		return 0, err
	}
	return info.Mode().Perm(), nil
}

func removeShellHookBlock(content string) (string, bool) {
	start := strings.Index(content, shellHookStart)
	if start < 0 {
		return content, false
	}
	afterStart := content[start:]
	if endIndex := strings.Index(afterStart, shellHookEnd); endIndex >= 0 {
		end := start + endIndex + len(shellHookEnd)
		end = skipHookLineEnding(content, end)
		return joinShellHookParts(content[:start], content[end:]), true
	}
	return removeLegacyShellHookBlock(content, start), true
}

func skipHookLineEnding(content string, end int) int {
	hasCarriageReturn := end < len(content) && content[end] == '\r'
	if hasCarriageReturn {
		end++
	}
	hasNewline := end < len(content) && content[end] == '\n'
	if hasNewline {
		end++
	}
	return end
}

func removeLegacyShellHookBlock(content string, start int) string {
	rest := content[start:]
	end := legacyHookEnd(rest)
	return joinShellHookParts(content[:start], rest[end:])
}

func legacyHookEnd(rest string) int {
	offset := 0
	for offset < len(rest) {
		line, n := nextLine(rest[offset:])
		trimmed := strings.TrimSpace(line)
		switch {
		case offset == 0 && trimmed == shellHookStart, trimmed == "":
			offset += n
		case isLegacyHookFunctionLine(trimmed):
			offset += n
			if !strings.Contains(trimmed, "}") {
				offset = legacyFunctionEnd(rest, offset)
			}
		default:
			return offset
		}
	}
	return offset
}

func legacyFunctionEnd(rest string, offset int) int {
	for offset < len(rest) {
		line, n := nextLine(rest[offset:])
		offset += n
		if strings.TrimSpace(line) == "}" {
			break
		}
	}
	return offset
}

func nextLine(s string) (string, int) {
	if idx := strings.IndexByte(s, '\n'); idx >= 0 {
		return s[:idx+1], idx + 1
	}
	return s, len(s)
}

func isLegacyHookFunctionLine(line string) bool {
	return strings.HasPrefix(line, "function ") && strings.Contains(line, "() {")
}

func joinShellHookParts(before, after string) string {
	before = strings.TrimRight(before, "\n")
	after = strings.TrimLeft(after, "\n")
	switch {
	case before == "":
		return after
	case after == "":
		return before + "\n"
	default:
		joined := before + "\n" + after
		return joined
	}
}

func detectRCFile() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve home directory: %w", err)
	}
	isZsh := strings.Contains(os.Getenv("SHELL"), "zsh")
	if isZsh {
		return filepath.Join(home, ".zshrc"), nil
	}
	return filepath.Join(home, ".bashrc"), nil
}
