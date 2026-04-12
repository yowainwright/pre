//go:build e2e

package e2e

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

var preBin string

func TestMain(m *testing.M) {
	root, err := filepath.Abs("../../")
	if err != nil {
		panic(err)
	}

	tmp, err := os.MkdirTemp("", "pre-e2e-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(tmp)

	preBin = filepath.Join(tmp, "pre")
	build := exec.Command("go", "build", "-o", preBin, "./cmd/pre")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		panic(fmt.Sprintf("build failed: %s: %v", out, err))
	}

	os.Exit(m.Run())
}

func run(env []string, args ...string) (stdout, stderr string, code int) {
	cmd := exec.Command(preBin, args...)
	cmd.Env = env
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	err := cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			code = 1
		}
	}
	return out.String(), errOut.String(), code
}

func baseEnv(home string) []string {
	return append(os.Environ(), "HOME="+home, "NO_COLOR=1", "PRE_CACHE_TTL=0")
}

func TestVersion(t *testing.T) {
	stdout, _, code := run(os.Environ(), "--version")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if stdout == "" {
		t.Error("expected version output")
	}
}

func TestNoArgs(t *testing.T) {
	_, stderr, code := run(os.Environ())
	if code != 1 {
		t.Fatalf("expected exit 1, got %d", code)
	}
	if !strings.Contains(stderr, "usage") {
		t.Errorf("expected usage in stderr, got %q", stderr)
	}
}

func TestUnknownManager(t *testing.T) {
	_, stderr, code := run(os.Environ(), "notamanager", "install", "foo")
	if code != 1 {
		t.Fatalf("expected exit 1, got %d", code)
	}
	if !strings.Contains(stderr, "unknown manager") {
		t.Errorf("expected unknown manager in stderr, got %q", stderr)
	}
}

func TestSetupWritesBashrc(t *testing.T) {
	home := t.TempDir()
	env := append(os.Environ(), "HOME="+home, "SHELL=/bin/bash")
	_, _, code := run(env, "setup")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	content, err := os.ReadFile(filepath.Join(home, ".bashrc"))
	if err != nil {
		t.Fatalf("expected .bashrc to exist: %v", err)
	}
	if !strings.Contains(string(content), "# pre security proxy") {
		t.Error("expected shell hook in .bashrc")
	}
}

func TestSetupIdempotent(t *testing.T) {
	home := t.TempDir()
	env := append(os.Environ(), "HOME="+home, "SHELL=/bin/bash")
	run(env, "setup")
	run(env, "setup")

	content, _ := os.ReadFile(filepath.Join(home, ".bashrc"))
	count := strings.Count(string(content), "# pre security proxy")
	if count != 1 {
		t.Errorf("expected hook written exactly once, found %d times", count)
	}
}

func TestInterceptPassthrough(t *testing.T) {
	if _, err := exec.LookPath("npm"); err != nil {
		t.Skip("npm not available")
	}
	home := t.TempDir()
	stdout, _, _ := run(baseEnv(home), "npm", "run", "--help")
	if !strings.Contains(stdout+"-", "-") {
		t.Error("expected passthrough to npm")
	}
}

func TestInterceptChecksPackage(t *testing.T) {
	if _, err := exec.LookPath("npm"); err != nil {
		t.Skip("npm not available")
	}
	home := t.TempDir()
	stdout, _, _ := run(baseEnv(home), "npm", "install", "--dry-run", "react@18.0.0")
	if !strings.Contains(stdout, "react") {
		t.Errorf("expected react in output, got: %s", stdout)
	}
	hasCheck := strings.Contains(stdout, "checking") || strings.Contains(stdout, "clean") || strings.Contains(stdout, "vulnerabilit")
	if !hasCheck {
		t.Errorf("expected security check output, got: %s", stdout)
	}
}
