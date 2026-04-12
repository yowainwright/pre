package main

import (
	"bytes"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/yowainwright/pre/internal/proxy"
)

func TestRunNoArgs(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{}, &out, &errOut)
	if code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
	if !strings.Contains(errOut.String(), "usage:") {
		t.Errorf("expected usage message, got: %s", errOut.String())
	}
}

func TestRunVersion(t *testing.T) {
	for _, arg := range []string{"--version", "-v"} {
		var out, errOut bytes.Buffer
		code := run([]string{arg}, &out, &errOut)
		if code != 0 {
			t.Errorf("%s: expected exit 0, got %d", arg, code)
		}
		if !strings.Contains(out.String(), version) {
			t.Errorf("%s: expected version in output, got: %s", arg, out.String())
		}
	}
}

func TestRunSetup(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SHELL", "/bin/zsh")
	var out, errOut bytes.Buffer
	code := run([]string{"setup"}, &out, &errOut)
	if code != 0 {
		t.Errorf("expected exit 0, got %d", code)
	}
}

func TestRunUnknownManager(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"unknown-mgr-xyz"}, &out, &errOut)
	if code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
	if !strings.Contains(errOut.String(), "unknown manager") {
		t.Errorf("expected unknown manager message, got: %s", errOut.String())
	}
}

func TestMainSubprocess(t *testing.T) {
	if os.Getenv("TEST_MAIN_SUBPROCESS") != "1" {
		return
	}
	main()
}

func TestMainExitsOnNoArgs(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Skip("cannot find test executable")
	}
	c := exec.Command(exe, "-test.run=TestMainSubprocess")
	c.Env = append(os.Environ(), "TEST_MAIN_SUBPROCESS=1")
	if err := c.Run(); err == nil {
		t.Error("expected non-zero exit")
	}
}

func TestRunKnownManagerNonInstall(t *testing.T) {
	orig := proxy.ExecFn
	called := false
	proxy.ExecFn = func(name string, args []string) { called = true }
	defer func() { proxy.ExecFn = orig }()

	var out, errOut bytes.Buffer
	code := run([]string{"npm", "run", "build"}, &out, &errOut)
	if code != 0 {
		t.Errorf("expected exit 0, got %d", code)
	}
	if !called {
		t.Error("expected ExecFn to be called")
	}
}
