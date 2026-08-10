package proxy

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yowainwright/pre/internal/manager"
)

const globalOptionCommands = `
cargo +nightly --color always update
cargo +nightly build
npm --prefix app install react
pnpm --dir app add react
go -C app get example.com/mod
`

const globalOptionOutput = `pre:cargo +nightly --color always update
cargo:+nightly build
pre:npm --prefix app install react
pre:pnpm --dir app add react
pre:go -C app get example.com/mod
`

func TestBuildShellHookContents(t *testing.T) {
	hook := buildShellHook()
	if !strings.Contains(hook, "# pre security proxy") {
		t.Error("expected hook to contain '# pre security proxy'")
	}
	if !strings.Contains(hook, "# end pre security proxy") {
		t.Error("expected hook to contain '# end pre security proxy'")
	}
	for _, m := range manager.All() {
		if !strings.Contains(hook, m.Name) {
			t.Errorf("expected hook to contain manager name %q", m.Name)
		}
	}
}

func TestBuildShellHookIncludesDisableBypass(t *testing.T) {
	hook := buildShellHook()
	if !strings.Contains(hook, "PRE_DISABLE") {
		t.Error("expected hook to include PRE_DISABLE bypass")
	}
	if !strings.Contains(hook, `command npm "$@"`) {
		t.Error("expected hook bypass to call the original package manager")
	}
}

func TestBuildShellHookIncludesNestedUVInstall(t *testing.T) {
	hook := buildShellHook()
	condition := `[[ "$_pre_command" == "pip" && "$_pre_subcommand" == "install" ]]`
	if !strings.Contains(hook, condition) {
		t.Errorf("expected uv pip install condition, got:\n%s", hook)
	}
}

func TestBuildShellHookParsesCargoGlobalOptions(t *testing.T) {
	hook := buildShellHook()
	markers := []string{"_pre_cargo_command", "+*", "--color|--config|--explain|--manifest-path|--target-dir|-C|-Z", "add|install|update|fetch"}
	for _, marker := range markers {
		if !strings.Contains(hook, marker) {
			t.Errorf("expected Cargo hook marker %q", marker)
		}
	}
}

func TestBuildShellHookParsesManagerGlobalOptions(t *testing.T) {
	hook := buildShellHook()
	markers := []string{"_pre_command", "--prefix", "--dir", "-C"}
	for _, marker := range markers {
		hasMarker := strings.Contains(hook, marker)
		if !hasMarker {
			t.Errorf("expected manager hook marker %q", marker)
		}
	}
}

func TestBuildShellHookHasValidBashSyntax(t *testing.T) {
	cmd := exec.Command("bash", "-n")
	cmd.Stdin = strings.NewReader(buildShellHook())
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("invalid Bash hook: %v\n%s", err, output)
	}
}

func TestCargoShellHookRoutesGlobalOptions(t *testing.T) {
	binDir := t.TempDir()
	prePath := filepath.Join(binDir, "pre")
	cargoPath := filepath.Join(binDir, "cargo")
	writeShellFixture(t, prePath, "#!/bin/sh\nprintf 'pre:%s\\n' \"$*\"\n")
	writeShellFixture(t, cargoPath, "#!/bin/sh\nprintf 'cargo:%s\\n' \"$*\"\n")
	t.Setenv("PATH", binDir)

	hook := buildShellHook()
	cmd := exec.Command("/bin/bash", "-c", hook+globalOptionCommands)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run Cargo hook: %v\n%s", err, output)
	}
	want := globalOptionOutput
	if string(output) != want {
		t.Fatalf("unexpected Cargo hook output: %q", output)
	}
}

func writeShellFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write shell fixture: %v", err)
	}
}

func TestDetectRCFileZsh(t *testing.T) {
	t.Setenv("SHELL", "/bin/zsh")
	t.Setenv("HOME", t.TempDir())
	rc := detectRCFile()
	if !strings.HasSuffix(rc, ".zshrc") {
		t.Errorf("expected .zshrc, got %s", rc)
	}
}

func TestDetectRCFileBash(t *testing.T) {
	t.Setenv("SHELL", "/bin/bash")
	t.Setenv("HOME", t.TempDir())
	rc := detectRCFile()
	if !strings.HasSuffix(rc, ".bashrc") {
		t.Errorf("expected .bashrc, got %s", rc)
	}
}

func TestSetupFresh(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("SHELL", "/bin/zsh")

	Setup()

	content, err := os.ReadFile(filepath.Join(dir, ".zshrc"))
	if err != nil {
		t.Fatalf("expected .zshrc to be created: %v", err)
	}
	if !strings.Contains(string(content), "# pre security proxy") {
		t.Error("expected hook to be written to .zshrc")
	}
}

func TestSetupRefreshesExistingHooks(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("SHELL", "/bin/zsh")

	rcPath := filepath.Join(dir, ".zshrc")
	initial := "export FOO=bar\n# pre security proxy\nfunction npm() {}\n"
	os.WriteFile(rcPath, []byte(initial), 0644)

	Setup()

	content, _ := os.ReadFile(rcPath)
	if !strings.Contains(string(content), "export FOO=bar") {
		t.Error("expected setup to preserve content before existing hook")
	}
	if !strings.Contains(string(content), "# end pre security proxy") {
		t.Error("expected setup to refresh hook block")
	}
	contentText := string(content)
	containsUpdate := strings.Contains(contentText, `"$_pre_command" == "update"`)
	if !containsUpdate {
		t.Error("expected refreshed hooks to include update commands")
	}
}

func TestTeardownRemovesHooks(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("SHELL", "/bin/zsh")

	rcPath := filepath.Join(dir, ".zshrc")
	os.WriteFile(rcPath, []byte("export FOO=bar\n"+buildShellHook()+"export BAR=baz\n"), 0644)

	Teardown()

	content, _ := os.ReadFile(rcPath)
	if strings.Contains(string(content), "# pre security proxy") {
		t.Error("expected hook marker to be removed")
	}
	if !strings.Contains(string(content), "export FOO=bar") {
		t.Error("expected content before marker to be preserved")
	}
	if !strings.Contains(string(content), "export BAR=baz") {
		t.Error("expected content after marker to be preserved")
	}
}

func TestTeardownRemovesLegacyHooksWithoutDeletingTrailingContent(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("SHELL", "/bin/zsh")

	rcPath := filepath.Join(dir, ".zshrc")
	os.WriteFile(rcPath, []byte("export FOO=bar\n# pre security proxy\nfunction bun() {}\nexport BAR=baz\n"), 0644)

	Teardown()

	content, _ := os.ReadFile(rcPath)
	if strings.Contains(string(content), "# pre security proxy") {
		t.Error("expected hook marker to be removed")
	}
	if !strings.Contains(string(content), "export FOO=bar") {
		t.Error("expected content before marker to be preserved")
	}
	if !strings.Contains(string(content), "export BAR=baz") {
		t.Error("expected content after legacy hook to be preserved")
	}
}

func TestShellHookStatus(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("SHELL", "/bin/zsh")

	rcPath := filepath.Join(dir, ".zshrc")
	os.WriteFile(rcPath, []byte(buildShellHook()), 0644)

	path, installed := ShellHookStatus()
	if path != rcPath {
		t.Errorf("expected rc path %s, got %s", rcPath, path)
	}
	if !installed {
		t.Error("expected hooks to be installed")
	}
}

func TestTeardownNoHooks(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("SHELL", "/bin/zsh")

	rcPath := filepath.Join(dir, ".zshrc")
	os.WriteFile(rcPath, []byte("export FOO=bar\n"), 0644)

	Teardown()

	content, _ := os.ReadFile(rcPath)
	if string(content) != "export FOO=bar\n" {
		t.Error("expected file to be unchanged when no hooks present")
	}
}

func TestTeardownReadError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("SHELL", "/bin/zsh")

	Teardown()
}

func TestTeardownWriteError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("SHELL", "/bin/zsh")

	rcPath := filepath.Join(dir, ".zshrc")
	os.WriteFile(rcPath, []byte("# pre security proxy\nstuff\n"), 0444)
	defer os.Chmod(rcPath, 0644)

	Teardown()
}

func TestSetupEnablesSystemScan(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("SHELL", "/bin/zsh")
	defer withStdinInput("y\n")()

	Setup()
}

func TestSetupEnablesSystemScanConfigError(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root can write to read-only dirs")
	}
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("SHELL", "/bin/zsh")
	defer withStdinInput("y\n")()

	libDir := filepath.Join(dir, "Library")
	os.MkdirAll(libDir, 0555)
	defer os.Chmod(libDir, 0755)

	Setup()
}

func TestSetupWriteError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("SHELL", "/bin/zsh")

	os.Mkdir(filepath.Join(dir, ".zshrc"), 0755)

	exited := false
	origExit := processExit
	processExit = func(code int) { exited = true; panic("exit") }
	defer func() {
		recover()
		processExit = origExit
		if !exited {
			t.Error("expected processExit to be called on write error")
		}
	}()

	Setup()
}

func TestSetupDeclinesSystemScan(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("SHELL", "/bin/zsh")
	defer withStdinInput("n\n")()

	Setup()

	rcPath := filepath.Join(dir, ".zshrc")
	content, err := os.ReadFile(rcPath)
	if err != nil {
		t.Fatalf("expected rc file: %v", err)
	}
	if !strings.Contains(string(content), shellHookStart) {
		t.Error("expected hooks to be written even when scan declined")
	}
}

func TestShellHookStatusNotInstalled(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("SHELL", "/bin/zsh")

	rcPath := filepath.Join(dir, ".zshrc")
	os.WriteFile(rcPath, []byte("export FOO=bar\n"), 0644)

	_, installed := ShellHookStatus()
	if installed {
		t.Error("expected hooks not to be installed")
	}
}

func TestNextLineNoNewline(t *testing.T) {
	line, n := nextLine("hello")
	if line != "hello" || n != 5 {
		t.Errorf("expected (hello, 5), got (%q, %d)", line, n)
	}
}

func TestRemoveLegacyShellHookBlockMultiLineFunction(t *testing.T) {
	content := "# pre security proxy\nfunction npm() {\n  command pre npm \"$@\"\n}\nexport BAR=baz\n"
	result := removeLegacyShellHookBlock(content, 0)
	if strings.Contains(result, "function npm") {
		t.Errorf("expected function to be removed, got %q", result)
	}
	if !strings.Contains(result, "export BAR=baz") {
		t.Errorf("expected trailing content preserved, got %q", result)
	}
}

func TestRemoveLegacyShellHookBlockNoTrailing(t *testing.T) {
	content := "# pre security proxy\nfunction npm() { command pre npm \"$@\"; }\n"
	result := removeLegacyShellHookBlock(content, 0)
	if strings.Contains(result, "function npm") {
		t.Errorf("expected function removed when no trailing content, got %q", result)
	}
}

func TestManagerAllInHook(t *testing.T) {
	hook := buildShellHook()
	for _, mgr := range manager.All() {
		if !strings.Contains(hook, "function "+mgr.Name+"()") {
			t.Errorf("expected hook for manager %s", mgr.Name)
		}
	}
}
