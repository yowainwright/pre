package proxy

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/yowainwright/pre/internal/config"
	"github.com/yowainwright/pre/internal/manager"
)

func Setup() {
	rcFile := detectRCFile()

	content, _ := os.ReadFile(rcFile)
	alreadyInstalled := strings.Contains(string(content), "# pre security proxy")
	if alreadyInstalled {
		fmt.Println("pre: already set up in", rcFile)
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
	sb.WriteString("\n# pre security proxy\n")

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

	return sb.String()
}

func Teardown() {
	rcFile := detectRCFile()
	content, err := os.ReadFile(rcFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "pre teardown: %v\n", err)
		return
	}
	marker := "# pre security proxy"
	idx := strings.Index(string(content), marker)
	if idx < 0 {
		fmt.Println("pre: no hooks found in", rcFile)
		return
	}
	cleaned := strings.TrimRight(string(content[:idx]), "\n") + "\n"
	if err := os.WriteFile(rcFile, []byte(cleaned), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "pre teardown: %v\n", err)
		return
	}
	fmt.Println("pre: removed hooks from", rcFile)
	fmt.Println("pre: restart your shell or run: source", rcFile)
}

func detectRCFile() string {
	home, _ := os.UserHomeDir()
	isZsh := strings.Contains(os.Getenv("SHELL"), "zsh")
	if isZsh {
		return filepath.Join(home, ".zshrc")
	}
	return filepath.Join(home, ".bashrc")
}
