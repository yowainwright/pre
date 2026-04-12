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
