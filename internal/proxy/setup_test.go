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
	rc, err := detectRCFile()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(rc, ".zshrc") {
		t.Errorf("expected .zshrc, got %s", rc)
	}
}

func TestDetectRCFileBash(t *testing.T) {
	t.Setenv("SHELL", "/bin/bash")
	t.Setenv("HOME", t.TempDir())
	rc, err := detectRCFile()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(rc, ".bashrc") {
		t.Errorf("expected .bashrc, got %s", rc)
	}
}

func TestDetectRCFileRequiresHome(t *testing.T) {
	t.Setenv("HOME", "")
	if _, err := detectRCFile(); err == nil {
		t.Fatal("expected missing home directory to fail")
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

func TestSetupPreservesExistingRCFileMode(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("SHELL", "/bin/zsh")

	rcPath := filepath.Join(dir, ".zshrc")
	if err := os.WriteFile(rcPath, []byte("export FOO=bar\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	Setup()

	info, err := os.Stat(rcPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Errorf("expected rc file mode 0644, got %o", info.Mode().Perm())
	}
}

func TestWriteRCFileFollowsSymlinks(t *testing.T) {
	for _, kind := range []string{"relative", "absolute", "chain"} {
		t.Run(kind, func(t *testing.T) {
			path, target := profileSymlinkFixture(t, kind)
			assertRCWrite(t, path, target, 0o600)
			if err := os.Chmod(target, 0o644); err != nil {
				t.Fatal(err)
			}
			assertRCWrite(t, path, target, 0o644)
		})
	}
}

func profileSymlinkFixture(t *testing.T, kind string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	target := filepath.Join(dir, "profile")
	linkTarget := target
	switch kind {
	case "relative":
		linkTarget = "profile"
	case "chain":
		linkTarget = filepath.Join(dir, "profile-link")
		createProfileSymlink(t, "profile", linkTarget)
	}
	path := filepath.Join(dir, ".zshrc")
	createProfileSymlink(t, linkTarget, path)
	return path, target
}

func createProfileSymlink(t *testing.T, target, path string) {
	t.Helper()
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
}

func assertRCWrite(t *testing.T, path, target string, perm os.FileMode) {
	t.Helper()
	link, err := os.Readlink(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeRCFile(path, []byte("updated profile\n")); err != nil {
		t.Fatal(err)
	}
	assertRCFile(t, target, perm)
	got, err := os.Readlink(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != link {
		t.Errorf("symlink target = %q, want %q", got, link)
	}
}

func assertRCFile(t *testing.T, path string, perm os.FileMode) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "updated profile\n" {
		t.Errorf("unexpected profile contents: %q", content)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != perm {
		t.Errorf("profile mode = %o, want %o", info.Mode().Perm(), perm)
	}
}

func TestWriteRCFileResolvesParentBeforeDotDot(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "actual", "nested")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(dir, "alias")
	createProfileSymlink(t, nested, alias)
	path := filepath.Join(dir, ".zshrc")
	createProfileSymlink(t, "alias/../profile", path)
	target := filepath.Join(dir, "actual", "profile")
	assertRCWrite(t, path, target, 0o600)
}

func TestWriteRCFileRejectsInvalidSymlinks(t *testing.T) {
	for _, target := range []string{".zshrc", "missing/profile"} {
		t.Run(target, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, ".zshrc")
			createProfileSymlink(t, target, path)
			if err := writeRCFile(path, []byte("updated profile\n")); err == nil {
				t.Fatal("expected invalid symlink target to fail")
			}
			if _, err := os.Readlink(path); err != nil {
				t.Fatalf("profile symlink was replaced: %v", err)
			}
		})
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

func TestSetupPreservesUnreadableRCFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read files without read permission")
	}
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("SHELL", "/bin/zsh")
	rcPath := filepath.Join(dir, ".zshrc")
	original := []byte("export IMPORTANT=value\n")
	if err := os.WriteFile(rcPath, original, 0o200); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(rcPath, 0o600)

	exitCode := 0
	origExit := processExit
	processExit = func(code int) { exitCode = code }
	defer func() { processExit = origExit }()

	Setup()
	if exitCode != 1 {
		t.Fatalf("expected setup to fail, got exit code %d", exitCode)
	}
	if err := os.Chmod(rcPath, 0o600); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(rcPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != string(original) {
		t.Fatalf("expected RC file to remain unchanged, got %q", content)
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
