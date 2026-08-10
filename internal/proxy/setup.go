package proxy

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/yowainwright/pre/internal/config"
	"github.com/yowainwright/pre/internal/manager"
)

const (
	shellHookStart = "# pre security proxy"
	shellHookEnd   = "# end pre security proxy"
)

const cargoShellHookText = "function " + "cargo() {\n" + `  case "${PRE_DISABLE:-}" in
    1|true|TRUE|True|yes|YES|Yes|on|ON|On)
      command cargo "$@"
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
      command pre cargo "$@"
      ;;
    *)
      command cargo "$@"
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
    command pre %s "$@"
  else
    command %s "$@"
  fi
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

func Setup() {
	rcFile, err := detectRCFile()
	if err != nil {
		fmt.Fprintf(os.Stderr, "pre setup: %v\n", err)
		processExit(1)
		return
	}

	content, err := os.ReadFile(rcFile)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(os.Stderr, "pre setup: %v\n", err)
		processExit(1)
		return
	}
	alreadyInstalled := strings.Contains(string(content), shellHookStart)
	if alreadyInstalled {
		cleaned, removed := removeShellHookBlock(string(content))
		if !removed {
			fmt.Println("pre: already set up in", rcFile)
			return
		}
		appended := append([]byte(cleaned), []byte(buildShellHook())...)
		if err := os.WriteFile(rcFile, appended, 0600); err != nil {
			fmt.Fprintf(os.Stderr, "pre setup: %v\n", err)
			processExit(1)
			return
		}
		fmt.Println("pre: refreshed hooks in", rcFile)
		fmt.Println("pre: restart your shell or run: source", rcFile)
		return
	}

	appended := append(content, []byte(buildShellHook())...)
	if err := os.WriteFile(rcFile, appended, 0600); err != nil {
		fmt.Fprintf(os.Stderr, "pre setup: %v\n", err)
		processExit(1)
		return
	}

	fmt.Println("pre: added hooks to", rcFile)
	fmt.Println("pre: restart your shell or run: source", rcFile)

	if confirm("Enable weekly background system scan? (checks all cached packages for new CVEs)") {
		cfg := config.Load()
		cfg.SystemScan = true
		if err := config.Save(cfg); err != nil {
			fmt.Fprintf(os.Stderr, "pre setup: could not save config: %v\n", err)
		} else {
			fmt.Println("pre: system scan enabled — runs weekly in the background after installs")
		}
	}
}

func buildShellHook() string {
	var sb strings.Builder
	sb.WriteString("\n" + shellHookStart + "\n")

	for _, m := range manager.All() {
		if m.Name == "cargo" {
			sb.WriteString(cargoShellHookText)
			continue
		}
		sb.WriteString(managerShellHook(m))
	}

	sb.WriteString(shellHookEnd + "\n")
	return sb.String()
}

func managerShellHook(m manager.Manager) string {
	parser := shellCommandParser(m)
	condition := shellInstallCondition(m)
	return fmt.Sprintf(managerShellHookText, m.Name, m.Name, parser, condition, m.Name, m.Name)
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
	cleaned, removed := removeShellHookBlock(string(content))
	if !removed {
		return rcFile, false, nil
	}
	if err := os.WriteFile(rcFile, []byte(cleaned), 0600); err != nil {
		return rcFile, false, err
	}
	return rcFile, true, nil
}

func removeShellHookBlock(content string) (string, bool) {
	idx := strings.Index(content, shellHookStart)
	if idx < 0 {
		return content, false
	}

	afterStart := content[idx:]
	if endIdx := strings.Index(afterStart, shellHookEnd); endIdx >= 0 {
		end := idx + endIdx + len(shellHookEnd)
		if end < len(content) && content[end] == '\r' {
			end++
		}
		if end < len(content) && content[end] == '\n' {
			end++
		}
		return joinShellHookParts(content[:idx], content[end:]), true
	}

	return removeLegacyShellHookBlock(content, idx), true
}

func removeLegacyShellHookBlock(content string, start int) string {
	before := content[:start]
	rest := content[start:]
	offset := 0

	for offset < len(rest) {
		line, n := nextLine(rest[offset:])
		trimmed := strings.TrimSpace(line)
		switch {
		case offset == 0 && trimmed == shellHookStart:
			offset += n
		case trimmed == "":
			offset += n
		case isLegacyHookFunctionLine(trimmed):
			offset += n
			if strings.Contains(trimmed, "}") {
				continue
			}
			for offset < len(rest) {
				line, n = nextLine(rest[offset:])
				offset += n
				if strings.TrimSpace(line) == "}" {
					break
				}
			}
		default:
			return joinShellHookParts(before, rest[offset:])
		}
	}

	return joinShellHookParts(before, "")
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
		return before + "\n" + after
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
