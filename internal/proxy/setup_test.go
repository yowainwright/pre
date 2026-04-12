package proxy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yowainwright/pre/internal/manager"
)

func TestBuildShellHookContents(t *testing.T) {
	hook := buildShellHook()
	if !strings.Contains(hook, "# pre security proxy") {
		t.Error("expected hook to contain '# pre security proxy'")
	}
	for _, m := range manager.All() {
		if !strings.Contains(hook, m.Name) {
			t.Errorf("expected hook to contain manager name %q", m.Name)
		}
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

func TestSetupAlreadySetUp(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("SHELL", "/bin/zsh")

	rcPath := filepath.Join(dir, ".zshrc")
	initial := "# pre security proxy\n"
	os.WriteFile(rcPath, []byte(initial), 0644)

	Setup()

	content, _ := os.ReadFile(rcPath)
	if string(content) != initial {
		t.Error("expected file to be unchanged when already set up")
	}
}

func TestTeardownRemovesHooks(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("SHELL", "/bin/zsh")

	rcPath := filepath.Join(dir, ".zshrc")
	os.WriteFile(rcPath, []byte("export FOO=bar\n# pre security proxy\nfunction bun() {}\n"), 0644)

	Teardown()

	content, _ := os.ReadFile(rcPath)
	if strings.Contains(string(content), "# pre security proxy") {
		t.Error("expected hook marker to be removed")
	}
	if !strings.Contains(string(content), "export FOO=bar") {
		t.Error("expected content before marker to be preserved")
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
