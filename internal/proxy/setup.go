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

func Setup() {
	rcFile := detectRCFile()

	content, _ := os.ReadFile(rcFile)
	alreadyInstalled := strings.Contains(string(content), shellHookStart)
	if alreadyInstalled {
		cleaned, removed := removeShellHookBlock(string(content))
		if !removed {
			fmt.Println("pre: already set up in", rcFile)
			return
		}
		appended := append([]byte(cleaned), []byte(buildShellHook())...)
		if err := os.WriteFile(rcFile, appended, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "pre setup: %v\n", err)
			processExit(1)
			return
		}
		fmt.Println("pre: refreshed hooks in", rcFile)
		fmt.Println("pre: restart your shell or run: source", rcFile)
		return
	}

	appended := append(content, []byte(buildShellHook())...)
	if err := os.WriteFile(rcFile, appended, 0644); err != nil {
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
		conditions := make([]string, len(m.InstallCmds))
		for i, c := range m.InstallCmds {
			conditions[i] = fmt.Sprintf(`"$1" == "%s"`, c)
		}
		condition := strings.Join(conditions, ` || `)

		fmt.Fprintf(&sb,
			"function %s() {\n  if [[ %s ]]; then\n    command pre %s \"$@\"\n  else\n    command %s \"$@\"\n  fi\n}\n",
			m.Name, condition, m.Name, m.Name,
		)
	}

	sb.WriteString(shellHookEnd + "\n")
	return sb.String()
}

func Teardown() {
	rcFile, removed, err := RemoveShellHooks()
	if err != nil {
		fmt.Fprintf(os.Stderr, "pre teardown: %v\n", err)
		return
	}
	if !removed {
		fmt.Println("pre: no hooks found in", rcFile)
		return
	}
	fmt.Println("pre: removed hooks from", rcFile)
	fmt.Println("pre: restart your shell or run: source", rcFile)
}

func ShellHookStatus() (string, bool) {
	rcFile := detectRCFile()
	content, err := os.ReadFile(rcFile)
	if err != nil {
		return rcFile, false
	}
	return rcFile, strings.Contains(string(content), shellHookStart)
}

func RemoveShellHooks() (string, bool, error) {
	rcFile := detectRCFile()
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
	if err := os.WriteFile(rcFile, []byte(cleaned), 0644); err != nil {
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

func detectRCFile() string {
	home, _ := os.UserHomeDir()
	isZsh := strings.Contains(os.Getenv("SHELL"), "zsh")
	if isZsh {
		return filepath.Join(home, ".zshrc")
	}
	return filepath.Join(home, ".bashrc")
}
