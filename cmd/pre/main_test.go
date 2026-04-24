package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
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

func TestRunConfig(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"config"}, &out, &errOut)
	if code != 0 {
		t.Errorf("expected exit 0, got %d", code)
	}
	o := out.String()
	if !strings.Contains(o, "endpoint") || !strings.Contains(o, "ttl") {
		t.Errorf("expected config keys in output, got: %s", o)
	}
}

func TestRunConfigSet(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	os.Setenv("HOME", dir)

	var out, errOut bytes.Buffer
	code := run([]string{"config", "set", "cache.ttl", "12h"}, &out, &errOut)
	if code != 0 {
		t.Errorf("expected exit 0, got %d — err: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "cache.ttl") {
		t.Errorf("expected confirmation output, got: %s", out.String())
	}
}

func TestRunConfigSetDottedTTL(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	t.Setenv("HOME", dir)

	var out, errOut bytes.Buffer
	code := run([]string{"config", "set", "cache.ttl", "12h"}, &out, &errOut)
	if code != 0 {
		t.Errorf("expected exit 0, got %d — err: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "cache.ttl") {
		t.Errorf("expected confirmation output, got: %s", out.String())
	}
}

func TestRunConfigSetUnknownKey(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"config", "set", "boguskey", "val"}, &out, &errOut)
	if code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
	if !strings.Contains(errOut.String(), "unknown key") {
		t.Errorf("expected unknown key error, got: %s", errOut.String())
	}
}

func TestRunConfigUsageErrors(t *testing.T) {
	tests := [][]string{
		{"config", "get", "cache.ttl"},
		{"config", "set", "cache.ttl"},
	}
	for _, args := range tests {
		var out, errOut bytes.Buffer
		code := run(args, &out, &errOut)
		if code != 1 {
			t.Errorf("%v: expected exit 1, got %d", args, code)
		}
		if !strings.Contains(errOut.String(), "usage:") {
			t.Errorf("%v: expected usage error, got: %s", args, errOut.String())
		}
	}
}

func TestRunConfigRejectsInvalidDuration(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	tests := [][]string{
		{"config", "set", "cache.ttl", "soon"},
		{"config", "set", "systemTTL", "weekly"},
	}
	for _, args := range tests {
		var out, errOut bytes.Buffer
		code := run(args, &out, &errOut)
		if code != 1 {
			t.Errorf("%v: expected exit 1, got %d", args, code)
		}
		if !strings.Contains(errOut.String(), "invalid duration") {
			t.Errorf("%v: expected invalid duration error, got: %s", args, errOut.String())
		}
	}
}

func TestRunConfigRejectsInvalidSystemScanBool(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	var out, errOut bytes.Buffer
	code := run([]string{"config", "set", "systemScan", "sometimes"}, &out, &errOut)
	if code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
	if !strings.Contains(errOut.String(), "invalid boolean") {
		t.Errorf("expected invalid boolean error, got: %s", errOut.String())
	}
}

func TestRunStatus(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"status"}, &out, &errOut)
	if code != 0 {
		t.Errorf("expected exit 0, got %d", code)
	}
	o := out.String()
	if !strings.Contains(o, "managers") || !strings.Contains(o, "cached") {
		t.Errorf("expected managers and cached in status output, got: %s", o)
	}
}

func TestRunTeardown(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("SHELL", "/bin/zsh")

	run([]string{"setup"}, &bytes.Buffer{}, &bytes.Buffer{})

	rcPath := dir + "/.zshrc"
	before, _ := os.ReadFile(rcPath)
	if !strings.Contains(string(before), "# pre security proxy") {
		t.Fatal("expected setup to write hooks")
	}

	code := run([]string{"teardown"}, &bytes.Buffer{}, &bytes.Buffer{})
	if code != 0 {
		t.Errorf("expected exit 0, got %d", code)
	}

	after, _ := os.ReadFile(rcPath)
	if strings.Contains(string(after), "# pre security proxy") {
		t.Error("expected teardown to remove hooks from rc file")
	}
}

func TestRunScanMissingArg(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"scan"}, &out, &errOut)
	if code != 1 {
		t.Errorf("expected exit 1 for scan with no arg, got %d", code)
	}
}

func TestRunScanSystem(t *testing.T) {
	orig := proxy.ExecFn
	proxy.ExecFn = noopExec
	defer func() { proxy.ExecFn = orig }()

	var out, errOut bytes.Buffer
	code := run([]string{"scan", "system"}, &out, &errOut)
	if code != 0 {
		t.Errorf("expected exit 0, got %d", code)
	}
}

func TestRunScanManager(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"scan", "npm"}, &out, &errOut)
	if code != 0 {
		t.Errorf("expected exit 0, got %d", code)
	}
}

func TestRunScanUnknownManager(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"scan", "unknownxyz"}, &out, &errOut)
	if code != 1 {
		t.Errorf("expected exit 1 for unknown manager, got %d", code)
	}
}

func TestRunConfigSetEndpoint(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	var out, errOut bytes.Buffer
	code := run([]string{"config", "set", "api.endpoint", "https://custom.example.com"}, &out, &errOut)
	if code != 0 {
		t.Errorf("expected exit 0, got %d — err: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "api.endpoint") {
		t.Errorf("expected endpoint in output, got: %s", out.String())
	}
}

func TestRunConfigSetDottedEndpoint(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	var out, errOut bytes.Buffer
	code := run([]string{"config", "set", "api.endpoint", "https://custom.example.com"}, &out, &errOut)
	if code != 0 {
		t.Errorf("expected exit 0, got %d — err: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "api.endpoint") {
		t.Errorf("expected api.endpoint in output, got: %s", out.String())
	}
}

func TestRunConfigSetSystemScan(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	var out, errOut bytes.Buffer
	code := run([]string{"config", "set", "systemScan", "true"}, &out, &errOut)
	if code != 0 {
		t.Errorf("expected exit 0, got %d — err: %s", code, errOut.String())
	}
}

func TestRunConfigSetSystemTTL(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	var out, errOut bytes.Buffer
	code := run([]string{"config", "set", "systemTTL", "48h"}, &out, &errOut)
	if code != 0 {
		t.Errorf("expected exit 0, got %d — err: %s", code, errOut.String())
	}
}

func TestRunStatusWithSystemStats(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CACHE_HOME", "")

	cacheDir, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	statsDir := filepath.Join(cacheDir, "pre")
	os.MkdirAll(statsDir, 0755)
	statsData := `{"crit":2,"warn":3,"total":10,"lastUpdated":"2024-01-01T12:00:00Z"}`
	os.WriteFile(filepath.Join(statsDir, "system.json"), []byte(statsData), 0644)

	var out, errOut bytes.Buffer
	code := run([]string{"status"}, &out, &errOut)
	if code != 0 {
		t.Errorf("expected exit 0, got %d", code)
	}
	if !strings.Contains(out.String(), "10 total") {
		t.Errorf("expected system stats in output, got: %s", out.String())
	}
}

func TestRunWithCustomManagers(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	cfgDir := filepath.Join(dir, "Library", "Application Support", "pre")
	os.MkdirAll(cfgDir, 0755)
	cfgData, _ := json.Marshal(map[string]interface{}{
		"managers": []map[string]interface{}{
			{"name": "npm", "ecosystem": "npm", "installCmds": []string{"install"}},
		},
	})
	os.WriteFile(filepath.Join(cfgDir, "config.json"), cfgData, 0644)

	var out, errOut bytes.Buffer
	code := run([]string{"config"}, &out, &errOut)
	if code != 0 {
		t.Errorf("expected exit 0, got %d", code)
	}
}

func TestRunConfigSetSaveError(t *testing.T) {
	t.Setenv("HOME", "/dev/null")
	t.Setenv("XDG_CONFIG_HOME", "/dev/null")

	var out, errOut bytes.Buffer
	code := run([]string{"config", "set", "endpoint", "https://example.com"}, &out, &errOut)
	if code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
	if !strings.Contains(errOut.String(), "pre config:") {
		t.Errorf("expected config error, got: %s", errOut.String())
	}
}

func noopExec(string, []string) {}

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
