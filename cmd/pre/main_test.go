package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	precache "github.com/yowainwright/pre/internal/cache"
	preconfig "github.com/yowainwright/pre/internal/config"
	preobs "github.com/yowainwright/pre/internal/obs"
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
	hasConfigKeys := strings.Contains(o, "endpoint") && strings.Contains(o, "ttl")
	if !hasConfigKeys {
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
		{"config", "set", "cache.ttl", "-1h"},
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

func TestRunStatus(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"status"}, &out, &errOut)
	if code != 0 {
		t.Errorf("expected exit 0, got %d", code)
	}
	o := out.String()
	hasStatusSummary := strings.Contains(o, "managers") && strings.Contains(o, "cached")
	if !hasStatusSummary {
		t.Errorf("expected managers and cached in status output, got: %s", o)
	}
}

func TestRunObsStatus(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PRE_OBS_DIR", dir)
	t.Setenv("PRE_OBS", "1")
	preobs.Record("pre.scan.approved", map[string]any{"manager": "npm", "decision": "approved"})

	var out, errOut bytes.Buffer
	code := run([]string{"obs"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d: %s", code, errOut.String())
	}
	o := out.String()
	if !strings.Contains(o, "status: ok") {
		t.Fatalf("unexpected obs status: %s", o)
	}
	if !strings.Contains(o, "allowed: 1") {
		t.Fatalf("unexpected obs status: %s", o)
	}
}

func TestRunObsEvents(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PRE_OBS_DIR", dir)
	t.Setenv("PRE_OBS", "1")
	preobs.Record("pre.scan.completed", map[string]any{"manager": "npm"})

	var out, errOut bytes.Buffer
	code := run([]string{"obs", "--events", "scan.completed"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "pre.scan.completed") {
		t.Fatalf("expected event output, got: %s", out.String())
	}
}

func TestRunObsJSON(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PRE_OBS_DIR", dir)
	t.Setenv("PRE_OBS", "1")
	preobs.Record("pre.scan.blocked", map[string]any{"decision": "blocked"})

	var out, errOut bytes.Buffer
	code := run([]string{"observability", "--json"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("expected exit 0, got %d: %s", code, errOut.String())
	}
	output := out.String()
	if !strings.Contains(output, `"status": "ok"`) {
		t.Fatalf("expected obs JSON, got: %s", output)
	}
	if !strings.Contains(output, `"blocked": 1`) {
		t.Fatalf("expected obs JSON, got: %s", output)
	}
}

func TestRunObsRejectsInvalidOptions(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "old diagnostics route", args: []string{"diagnostics"}, want: "unknown manager"},
		{name: "old diag route", args: []string{"diag"}, want: "unknown manager"},
		{name: "unknown option", args: []string{"obs", "--format", "text"}, want: "usage:"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			code := run(test.args, &out, &errOut)
			if code != 1 {
				t.Fatalf("expected exit 1, got %d", code)
			}
			if !strings.Contains(errOut.String(), test.want) {
				t.Fatalf("expected %q, got: %s", test.want, errOut.String())
			}
		})
	}
}

func TestRunInstalled(t *testing.T) {
	defer withLookPath(func(name string) (string, error) {
		if name == "npm" {
			return "/usr/bin/npm", nil
		}
		return "", os.ErrNotExist
	})()
	defer withCommandOutput(func(name string, args []string) ([]byte, error) {
		if name != "npm" {
			return nil, os.ErrNotExist
		}
		return []byte(`{"dependencies":{"react":{"version":"18.2.0"}}}`), nil
	})()

	var out, errOut bytes.Buffer
	code := run([]string{"installed"}, &out, &errOut)
	if code != 0 {
		t.Errorf("expected exit 0, got %d", code)
	}
	o := out.String()
	for _, want := range []string{"installed packages:", "npm", "react", "18.2.0"} {
		if !strings.Contains(o, want) {
			t.Errorf("expected installed output to contain %q, got: %s", want, o)
		}
	}
}

func TestReadHomebrewPackagesFromFilesystem(t *testing.T) {
	prefix := t.TempDir()
	if err := os.MkdirAll(filepath.Join(prefix, "Cellar", "ripgrep", "14.1.1"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(prefix, "Caskroom", "visual-studio-code", "1.99.0"), 0755); err != nil {
		t.Fatal(err)
	}
	defer withHomebrewPrefixes(func() []string { return []string{prefix} })()

	mgr := mustManager(t, "brew")
	pkgs := readHomebrewPackages(mgr)
	assertPackage(t, pkgs, "ripgrep", "14.1.1")
	assertPackage(t, pkgs, "visual-studio-code", "1.99.0")
}

func TestRunPackageInstall(t *testing.T) {
	defer withExecutablePath(func() (string, error) { return "/tmp/pre", nil })()

	var gotName string
	var gotArgs []string
	defer withCommandRunner(func(name string, args []string, env []string, stdout, stderr io.Writer) error {
		gotName = name
		gotArgs = args
		return nil
	})()

	var out, errOut bytes.Buffer
	code := run([]string{"install", "npm", "react"}, &out, &errOut)
	if code != 0 {
		t.Errorf("expected exit 0, got %d: %s", code, errOut.String())
	}
	wrongCommand := gotName != "/tmp/pre" || strings.Join(gotArgs, " ") != "npm install react"
	if wrongCommand {
		t.Errorf("expected pre npm install react, got %q %v", gotName, gotArgs)
	}
}

func TestRunPackageUpdate(t *testing.T) {
	defer withExecutablePath(func() (string, error) { return "/tmp/pre", nil })()

	var gotName string
	var gotArgs []string
	defer withCommandRunner(func(name string, args []string, env []string, stdout, stderr io.Writer) error {
		gotName = name
		gotArgs = args
		return nil
	})()

	var out, errOut bytes.Buffer
	code := run([]string{"update", "npm", "react"}, &out, &errOut)
	if code != 0 {
		t.Errorf("expected exit 0, got %d: %s", code, errOut.String())
	}
	wrongCommand := gotName != "/tmp/pre" || strings.Join(gotArgs, " ") != "npm install react@latest"
	if wrongCommand {
		t.Errorf("expected pre npm install react@latest, got %q %v", gotName, gotArgs)
	}
}

func TestRunPackageDowngrade(t *testing.T) {
	defer withExecutablePath(func() (string, error) { return "/tmp/pre", nil })()

	var gotName string
	var gotArgs []string
	defer withCommandRunner(func(name string, args []string, env []string, stdout, stderr io.Writer) error {
		gotName = name
		gotArgs = args
		return nil
	})()

	var out, errOut bytes.Buffer
	code := run([]string{"downgrade", "pip", "urllib3", "1.24.1"}, &out, &errOut)
	if code != 0 {
		t.Errorf("expected exit 0, got %d: %s", code, errOut.String())
	}
	wrongCommand := gotName != "/tmp/pre" || strings.Join(gotArgs, " ") != "pip install urllib3==1.24.1"
	if wrongCommand {
		t.Errorf("expected pre pip install urllib3==1.24.1, got %q %v", gotName, gotArgs)
	}
}

func TestRunPackageUninstall(t *testing.T) {
	defer withExecutablePath(func() (string, error) { return "/tmp/pre", nil })()

	var gotName string
	var gotArgs []string
	defer withCommandRunner(func(name string, args []string, env []string, stdout, stderr io.Writer) error {
		gotName = name
		gotArgs = args
		return nil
	})()

	var out, errOut bytes.Buffer
	code := run([]string{"uninstall", "brew", "ripgrep"}, &out, &errOut)
	if code != 0 {
		t.Errorf("expected exit 0, got %d: %s", code, errOut.String())
	}
	wrongCommand := gotName != "/tmp/pre" || strings.Join(gotArgs, " ") != "brew uninstall ripgrep"
	if wrongCommand {
		t.Errorf("expected pre brew uninstall ripgrep, got %q %v", gotName, gotArgs)
	}
}

func TestRunManageAliasOpensTUIAndQuits(t *testing.T) {
	t.Setenv("PRE_MANAGE_THEME", "")
	defer withPackageInput("q")()
	defer withTerminalSize(80, 16)()
	defer withLookPath(func(name string) (string, error) {
		if name == "npm" {
			return "/usr/bin/npm", nil
		}
		return "", os.ErrNotExist
	})()
	defer withCommandOutput(func(name string, args []string) ([]byte, error) {
		return []byte(`{"dependencies":{"react":{"version":"18.2.0"}}}`), nil
	})()

	var out, errOut bytes.Buffer
	code := run([]string{"m"}, &out, &errOut)
	if code != 0 {
		t.Errorf("expected exit 0, got %d: %s", code, errOut.String())
	}
	o := out.String()
	for _, want := range []string{"\033[?1049h", "\033[2J", "pre manage", "react", "→", manageDefaultTheme().selected} {
		if !strings.Contains(o, want) {
			t.Errorf("expected TUI output to contain %q, got: %q", want, o)
		}
	}
}

func TestRunManageSearchDialog(t *testing.T) {
	defer withPackageInput("/rea\nq")()
	defer withLookPath(func(name string) (string, error) {
		if name == "npm" {
			return "/usr/bin/npm", nil
		}
		return "", os.ErrNotExist
	})()
	defer withCommandOutput(func(name string, args []string) ([]byte, error) {
		return []byte(`{"dependencies":{"react":{"version":"18.2.0"},"lodash":{"version":"4.17.21"}}}`), nil
	})()

	var out, errOut bytes.Buffer
	code := run([]string{"manage"}, &out, &errOut)
	if code != 0 {
		t.Errorf("expected exit 0, got %d: %s", code, errOut.String())
	}
	o := out.String()
	hasSearchDialog := strings.Contains(o, " search ") && strings.Contains(o, "/rea")
	if !hasSearchDialog {
		t.Errorf("expected search dialog in output, got: %q", o)
	}
}

func TestRunManageSearchAcceptsQ(t *testing.T) {
	defer withPackageInput("/reaq\nq")()
	defer withLookPath(func(name string) (string, error) {
		if name == "npm" {
			return "/usr/bin/npm", nil
		}
		return "", os.ErrNotExist
	})()
	defer withCommandOutput(func(name string, args []string) ([]byte, error) {
		return []byte(`{"dependencies":{"react":{"version":"18.2.0"},"lodash":{"version":"4.17.21"}}}`), nil
	})()

	var out, errOut bytes.Buffer
	code := run([]string{"manage"}, &out, &errOut)
	if code != 0 {
		t.Errorf("expected exit 0, got %d: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "/reaq") {
		t.Errorf("expected live search text in output, got: %q", out.String())
	}
}

func TestRunManageManagerFilterTogglesManager(t *testing.T) {
	defer withPackageInput("m q")()
	defer withTerminalSize(90, 20)()
	defer withHomebrewPrefixes(func() []string { return nil })()
	defer withLookPath(func(name string) (string, error) {
		switch name {
		case "brew", "npm":
			return "/usr/bin/" + name, nil
		default:
			return "", os.ErrNotExist
		}
	})()
	defer withCommandOutput(func(name string, args []string) ([]byte, error) {
		switch name {
		case "brew":
			return []byte("ripgrep 14.1.1\n"), nil
		case "npm":
			return []byte(`{"dependencies":{"react":{"version":"18.2.0"}}}`), nil
		default:
			return nil, os.ErrNotExist
		}
	})()

	var out, errOut bytes.Buffer
	code := run([]string{"manage"}, &out, &errOut)
	if code != 0 {
		t.Errorf("expected exit 0, got %d: %s", code, errOut.String())
	}
	o := out.String()
	for _, want := range []string{" managers", "[ ] brew", "managers npm"} {
		if !strings.Contains(o, want) {
			t.Errorf("expected manager filter output to contain %q, got: %q", want, o)
		}
	}
}

func TestRunManageActionDialogClosesWithX(t *testing.T) {
	defer withPackageInput("\rxq")()
	defer withLookPath(func(name string) (string, error) {
		if name == "npm" {
			return "/usr/bin/npm", nil
		}
		return "", os.ErrNotExist
	})()
	defer withCommandOutput(func(name string, args []string) ([]byte, error) {
		return []byte(`{"dependencies":{"react":{"version":"18.2.0"}}}`), nil
	})()

	var out, errOut bytes.Buffer
	code := run([]string{"manage"}, &out, &errOut)
	if code != 0 {
		t.Errorf("expected exit 0, got %d: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), " actions ") {
		t.Errorf("expected action dialog in output, got: %q", out.String())
	}
}

func TestRunManageFlagUpgradeWithVersion(t *testing.T) {
	defer withExecutablePath(func() (string, error) { return "/tmp/pre", nil })()

	var gotName string
	var gotArgs []string
	defer withCommandRunner(func(name string, args []string, env []string, stdout, stderr io.Writer) error {
		gotName = name
		gotArgs = args
		return nil
	})()

	var out, errOut bytes.Buffer
	code := run([]string{"manage", "--manager", "npm", "--package", "react", "--upgrade", "18.3.1"}, &out, &errOut)
	if code != 0 {
		t.Errorf("expected exit 0, got %d: %s", code, errOut.String())
	}
	wrongCommand := gotName != "/tmp/pre" || strings.Join(gotArgs, " ") != "npm install react@18.3.1"
	if wrongCommand {
		t.Errorf("expected pre npm install react@18.3.1, got %q %v", gotName, gotArgs)
	}
}

func TestRunManageFlagUninstallResolvesManagerFromInventory(t *testing.T) {
	defer withExecutablePath(func() (string, error) { return "/tmp/pre", nil })()
	defer withHomebrewPrefixes(func() []string { return nil })()
	defer withLookPath(func(name string) (string, error) {
		if name == "brew" {
			return "/opt/homebrew/bin/brew", nil
		}
		return "", os.ErrNotExist
	})()
	defer withCommandOutput(func(name string, args []string) ([]byte, error) {
		return []byte("ripgrep 14.1.1\n"), nil
	})()

	var gotName string
	var gotArgs []string
	defer withCommandRunner(func(name string, args []string, env []string, stdout, stderr io.Writer) error {
		gotName = name
		gotArgs = args
		return nil
	})()

	var out, errOut bytes.Buffer
	code := run([]string{"manage", "--package", "ripgrep", "--uninstall"}, &out, &errOut)
	if code != 0 {
		t.Errorf("expected exit 0, got %d: %s", code, errOut.String())
	}
	wrongCommand := gotName != "/tmp/pre" || strings.Join(gotArgs, " ") != "brew uninstall ripgrep"
	if wrongCommand {
		t.Errorf("expected pre brew uninstall ripgrep, got %q %v", gotName, gotArgs)
	}
}

func TestRunSelfUpdateManualInstall(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "pre")
	defer withExecutablePath(func() (string, error) { return exe, nil })()

	scriptContent := []byte("#!/bin/sh\necho updating\n")
	defer withSelfUpdateDownloads(scriptContent)()

	var commands []string
	var installEnv []string
	defer withCommandRunner(func(name string, args []string, env []string, stdout, stderr io.Writer) error {
		commands = append(commands, name)
		if name == "sh" {
			installEnv = env
		}
		return nil
	})()

	var out, errOut bytes.Buffer
	code := run([]string{"self", "update"}, &out, &errOut)
	if code != 0 {
		t.Errorf("expected exit 0, got %d: %s", code, errOut.String())
	}
	assertSelfUpdateCommands(t, commands, installEnv, dir)
}

func assertSelfUpdateCommands(t *testing.T, commands, installEnv []string, dir string) {
	t.Helper()
	wantCommands := []string{"cosign", "sh"}
	if !slices.Equal(commands, wantCommands) {
		t.Fatalf("expected cosign before sh, got %v", commands)
	}
	hasInstallEnv := len(installEnv) == 1 && installEnv[0] == "PRE_BIN_DIR="+dir
	if !hasInstallEnv {
		t.Errorf("expected PRE_BIN_DIR env, got %v", installEnv)
	}
}

func withSelfUpdateDownloads(scriptContent []byte) func() {
	sum := sha256.Sum256(scriptContent)
	fakeChecksums := []byte(hex.EncodeToString(sum[:]) + "  install.sh\n")
	fakeBundle := []byte(`{"fake":"bundle"}`)
	return withHTTPGetBytes(func(url string) ([]byte, error) {
		switch {
		case strings.HasSuffix(url, ".bundle"):
			return fakeBundle, nil
		case strings.Contains(url, "checksums.txt"):
			return fakeChecksums, nil
		default:
			return scriptContent, nil
		}
	})
}

func TestRunSelfUpdateHomebrewInstall(t *testing.T) {
	defer withExecutablePath(func() (string, error) {
		return "/opt/homebrew/Cellar/pre/1.2.3/bin/pre", nil
	})()
	defer withLookPath(func(name string) (string, error) {
		return "/opt/homebrew/bin/" + name, nil
	})()

	var gotName string
	var gotArgs []string
	defer withCommandRunner(func(name string, args []string, env []string, stdout, stderr io.Writer) error {
		gotName = name
		gotArgs = args
		return nil
	})()

	var out, errOut bytes.Buffer
	code := run([]string{"self", "update"}, &out, &errOut)
	if code != 0 {
		t.Errorf("expected exit 0, got %d: %s", code, errOut.String())
	}
	wrongCommand := gotName != "brew" || strings.Join(gotArgs, " ") != "upgrade pre"
	if wrongCommand {
		t.Errorf("expected brew upgrade pre, got %q %v", gotName, gotArgs)
	}
}

func TestRunSelfUpdateHomebrewCask(t *testing.T) {
	defer withExecutablePath(func() (string, error) {
		return "/opt/homebrew/Caskroom/pre/1.2.3/pre-darwin-arm64", nil
	})()
	defer withLookPath(func(name string) (string, error) { return "/opt/homebrew/bin/" + name, nil })()

	var gotArgs []string
	defer withCommandRunner(func(_ string, args []string, _ []string, _, _ io.Writer) error {
		gotArgs = args
		return nil
	})()

	code := run([]string{"self", "update"}, &bytes.Buffer{}, &bytes.Buffer{})
	upgradeFailed := code != 0 || strings.Join(gotArgs, " ") != "upgrade --cask pre"
	if upgradeFailed {
		t.Errorf("expected cask upgrade, got code=%d args=%v", code, gotArgs)
	}
}

func TestRunSelfUninstallManualInstall(t *testing.T) {
	dir := setupShellTest(t)
	exe := filepath.Join(dir, "bin", "pre")
	defer withExecutablePath(func() (string, error) { return exe, nil })()

	var removedPath string
	defer withRemoveFile(func(path string) error {
		removedPath = path
		return nil
	})()

	rcPath := filepath.Join(dir, ".zshrc")
	os.WriteFile(rcPath, []byte("export FOO=bar\n# pre security proxy\nfunction npm() {}\nexport BAR=baz\n"), 0644)

	var out, errOut bytes.Buffer
	code := run([]string{"self", "uninstall"}, &out, &errOut)
	if code != 0 {
		t.Errorf("expected exit 0, got %d: %s", code, errOut.String())
	}
	if removedPath != exe {
		t.Errorf("expected binary removal for %s, got %s", exe, removedPath)
	}
	assertUninstalledHooks(t, rcPath)
}

func setupShellTest(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("SHELL", "/bin/zsh")
	return dir
}

func assertUninstalledHooks(t *testing.T, rcPath string) {
	t.Helper()
	content, err := os.ReadFile(rcPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(content), "# pre security proxy") {
		t.Error("expected uninstall to remove hooks")
	}
	if !strings.Contains(string(content), "export BAR=baz") {
		t.Error("expected uninstall to preserve content after hooks")
	}
}

func TestRunSelfUninstallKeepsManualBinaryWhenHookRemovalFails(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("SHELL", "/bin/zsh")
	exe := filepath.Join(dir, "bin", "pre")
	defer withExecutablePath(func() (string, error) { return exe, nil })()
	binaryRemoved := false
	defer withRemoveFile(func(string) error {
		binaryRemoved = true
		return nil
	})()
	rcPath := filepath.Join(dir, ".zshrc")
	if err := os.Mkdir(rcPath, 0o755); err != nil {
		t.Fatal(err)
	}

	code := run([]string{"self", "uninstall"}, &bytes.Buffer{}, &bytes.Buffer{})
	if code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
	if binaryRemoved {
		t.Error("expected hook failure to preserve the manual binary")
	}
}

func TestRunSelfUninstallHomebrewInstall(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("SHELL", "/bin/zsh")

	defer withExecutablePath(func() (string, error) {
		return "/opt/homebrew/Cellar/pre/1.2.3/bin/pre", nil
	})()
	defer withLookPath(func(name string) (string, error) {
		return "/opt/homebrew/bin/" + name, nil
	})()

	var gotName string
	var gotArgs []string
	defer withCommandRunner(func(name string, args []string, env []string, stdout, stderr io.Writer) error {
		gotName = name
		gotArgs = args
		return nil
	})()

	var out, errOut bytes.Buffer
	code := run([]string{"self", "uninstall"}, &out, &errOut)
	if code != 0 {
		t.Errorf("expected exit 0, got %d: %s", code, errOut.String())
	}
	wrongCommand := gotName != "brew" || strings.Join(gotArgs, " ") != "uninstall pre"
	if wrongCommand {
		t.Errorf("expected brew uninstall pre, got %q %v", gotName, gotArgs)
	}
}

func TestRunSelfUninstallKeepsHomebrewInstallWhenHookRemovalFails(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("SHELL", "/bin/zsh")
	defer withExecutablePath(func() (string, error) {
		return "/opt/homebrew/Cellar/pre/1.2.3/bin/pre", nil
	})()
	defer withLookPath(func(name string) (string, error) {
		return "/opt/homebrew/bin/" + name, nil
	})()
	commandCalled := false
	defer withCommandRunner(func(string, []string, []string, io.Writer, io.Writer) error {
		commandCalled = true
		return nil
	})()
	rcPath := filepath.Join(dir, ".zshrc")
	if err := os.Mkdir(rcPath, 0o755); err != nil {
		t.Fatal(err)
	}

	code := run([]string{"self", "uninstall"}, &bytes.Buffer{}, &bytes.Buffer{})
	if code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
	if commandCalled {
		t.Error("expected hook failure to preserve the Homebrew install")
	}
}

func TestRunSelfUninstallHomebrewCask(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SHELL", "/bin/zsh")
	defer withExecutablePath(func() (string, error) {
		return "/opt/homebrew/Caskroom/pre/1.2.3/pre-darwin-arm64", nil
	})()
	defer withLookPath(func(name string) (string, error) { return "/opt/homebrew/bin/" + name, nil })()

	var gotArgs []string
	defer withCommandRunner(func(_ string, args []string, _ []string, _, _ io.Writer) error {
		gotArgs = args
		return nil
	})()

	code := run([]string{"self", "uninstall"}, &bytes.Buffer{}, &bytes.Buffer{})
	uninstallFailed := code != 0 || strings.Join(gotArgs, " ") != "uninstall --cask pre"
	if uninstallFailed {
		t.Errorf("expected cask uninstall, got code=%d args=%v", code, gotArgs)
	}
}

func TestDetectInstallSourceHomebrewCask(t *testing.T) {
	path := "/opt/homebrew/Caskroom/pre/1.2.3/pre-darwin-arm64"
	if source := detectInstallSource(path); source != installSourceHomebrewCask {
		t.Errorf("expected Homebrew cask source, got %q", source)
	}
}

func TestRunSelfUninstallPurge(t *testing.T) {
	dir := setupShellTest(t)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(dir, "config-root"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(dir, "cache-root"))

	configPath, _ := preconfig.Path()
	cachePath, _ := precache.Path()
	exe := filepath.Join(dir, "pre")
	defer withExecutablePath(func() (string, error) { return exe, nil })()
	defer withRemoveFile(func(path string) error { return nil })()

	var removedDirs []string
	defer withRemoveAll(func(path string) error {
		removedDirs = append(removedDirs, path)
		return nil
	})()

	var out, errOut bytes.Buffer
	code := run([]string{"self", "uninstall", "--purge"}, &out, &errOut)
	if code != 0 {
		t.Errorf("expected exit 0, got %d: %s", code, errOut.String())
	}
	assertPurgedInstallDirs(t, removedDirs, configPath, cachePath)
}

func assertPurgedInstallDirs(t *testing.T, removedDirs []string, configPath, cachePath string) {
	t.Helper()
	joined := strings.Join(removedDirs, "\n")
	if !strings.Contains(joined, filepath.Dir(configPath)) {
		t.Errorf("expected config dir purge, got %v", removedDirs)
	}
	if !strings.Contains(joined, filepath.Dir(cachePath)) {
		t.Errorf("expected cache dir purge, got %v", removedDirs)
	}
}

func TestRunSelfUninstallRefusesUnexpectedBinaryName(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("SHELL", "/bin/zsh")
	defer withExecutablePath(func() (string, error) {
		return filepath.Join(dir, "pre.test"), nil
	})()

	var out, errOut bytes.Buffer
	code := run([]string{"self", "uninstall"}, &out, &errOut)
	if code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
	if !strings.Contains(errOut.String(), "refusing to remove") {
		t.Errorf("expected refusal, got: %s", errOut.String())
	}
}

func TestRunTeardown(t *testing.T) {
	dir := setupShellTest(t)
	rcPath := installTestHooks(t, dir)
	code := run([]string{"teardown"}, &bytes.Buffer{}, &bytes.Buffer{})
	if code != 0 {
		t.Errorf("expected exit 0, got %d", code)
	}

	after, _ := os.ReadFile(rcPath)
	if strings.Contains(string(after), "# pre security proxy") {
		t.Error("expected teardown to remove hooks from rc file")
	}
}

func installTestHooks(t *testing.T, dir string) string {
	t.Helper()
	run([]string{"setup"}, &bytes.Buffer{}, &bytes.Buffer{})
	rcPath := filepath.Join(dir, ".zshrc")
	before, err := os.ReadFile(rcPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(before), "# pre security proxy") {
		t.Fatal("expected setup to write hooks")
	}
	return rcPath
}

func TestRunTeardownPropagatesHookRemovalFailure(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("SHELL", "/bin/zsh")
	rcPath := filepath.Join(dir, ".zshrc")
	if err := os.Mkdir(rcPath, 0o755); err != nil {
		t.Fatal(err)
	}

	code := run([]string{"teardown"}, &bytes.Buffer{}, &bytes.Buffer{})
	if code != 1 {
		t.Errorf("expected exit 1, got %d", code)
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

func TestRunScanRejectsManager(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"scan", "npm"}, &out, &errOut)
	if code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
	if !strings.Contains(errOut.String(), "usage: pre scan system") {
		t.Errorf("expected scan usage, got: %s", errOut.String())
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

func TestRunStatusWithSystemStats(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("XDG_CACHE_HOME", "")
	writeTestSystemStats(t)
	var out, errOut bytes.Buffer
	code := run([]string{"status"}, &out, &errOut)
	if code != 0 {
		t.Errorf("expected exit 0, got %d", code)
	}
	if !strings.Contains(out.String(), "10 total") {
		t.Errorf("expected system stats in output, got: %s", out.String())
	}
}

func writeTestSystemStats(t *testing.T) {
	t.Helper()
	cacheDir, err := os.UserCacheDir()
	if err != nil {
		t.Fatal(err)
	}
	statsDir := filepath.Join(cacheDir, "pre")
	if err := os.MkdirAll(statsDir, 0755); err != nil {
		t.Fatal(err)
	}
	statsData := `{"crit":2,"warn":3,"total":10,"lastUpdated":"2024-01-01T12:00:00Z"}`
	if err := os.WriteFile(filepath.Join(statsDir, "system.json"), []byte(statsData), 0644); err != nil {
		t.Fatal(err)
	}
}

func TestRunWithCustomManagers(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)

	cfgDir := filepath.Join(dir, "Library", "Application Support", "pre")
	os.MkdirAll(cfgDir, 0755)
	cfg := map[string]interface{}{
		"managers": []map[string]interface{}{
			{"name": "npm", "ecosystem": "npm", "installCmds": []string{"install"}},
		},
	}
	cfgData, _ := json.Marshal(cfg)
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

func withExecutablePath(fn func() (string, error)) func() {
	orig := executablePathFn
	executablePathFn = fn
	return func() { executablePathFn = orig }
}

func withLookPath(fn func(string) (string, error)) func() {
	orig := lookPathFn
	lookPathFn = fn
	return func() { lookPathFn = orig }
}

func withCommandRunner(fn func(string, []string, []string, io.Writer, io.Writer) error) func() {
	orig := commandRunnerFn
	commandRunnerFn = fn
	return func() { commandRunnerFn = orig }
}

func withCommandRunnerWithInput(fn func(string, []string, []string, commandStreams) error) func() {
	orig := commandRunnerWithInputFn
	commandRunnerWithInputFn = fn
	return func() { commandRunnerWithInputFn = orig }
}

func withHTTPGetBytes(fn func(string) ([]byte, error)) func() {
	orig := httpGetBytesFn
	httpGetBytesFn = fn
	return func() { httpGetBytesFn = orig }
}

func withCommandOutput(fn func(string, []string) ([]byte, error)) func() {
	orig := commandOutputFn
	commandOutputFn = fn
	return func() { commandOutputFn = orig }
}

func withPackageInput(input string) func() {
	orig := packageInputReader
	packageInputReader = strings.NewReader(input)
	return func() { packageInputReader = orig }
}

func withTerminalSize(width, height int) func() {
	orig := terminalSizeFn
	terminalSizeFn = func() (int, int) { return width, height }
	return func() { terminalSizeFn = orig }
}

func withHomebrewPrefixes(fn func() []string) func() {
	orig := homebrewPrefixesFn
	homebrewPrefixesFn = fn
	return func() { homebrewPrefixesFn = orig }
}

func withRemoveFile(fn func(string) error) func() {
	orig := removeFileFn
	removeFileFn = fn
	return func() { removeFileFn = orig }
}

func withRemoveAll(fn func(string) error) func() {
	orig := removeAllFn
	removeAllFn = fn
	return func() { removeAllFn = orig }
}

func TestRunSelfNoArgs(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"self"}, &out, &errOut)
	if code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
	if !strings.Contains(errOut.String(), "usage:") {
		t.Errorf("expected usage in stderr, got: %s", errOut.String())
	}
}

func TestRunSelfUnknownSubcommand(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"self", "badcmd"}, &out, &errOut)
	if code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
	if !strings.Contains(errOut.String(), "usage:") {
		t.Errorf("expected usage in stderr, got: %s", errOut.String())
	}
}

func TestRunSelfInstalledCommand(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"self", "installed"}, &out, &errOut)
	if code != 0 {
		t.Errorf("expected exit 0, got %d: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "pre:") {
		t.Errorf("expected pre: in stdout, got: %s", out.String())
	}
}

func TestRunSelfStatusAlias(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"self", "status"}, &out, &errOut)
	if code != 0 {
		t.Errorf("expected exit 0, got %d: %s", code, errOut.String())
	}
}

func TestRunSelfInstalledExtraArgs(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"self", "installed", "extra"}, &out, &errOut)
	if code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
}

func TestRunSelfUpdateTooManyArgs(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"self", "update", "extra"}, &out, &errOut)
	if code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
	if !strings.Contains(errOut.String(), "usage:") {
		t.Errorf("expected usage in stderr, got: %s", errOut.String())
	}
}

func TestRunSelfUpdateNoBinaryPath(t *testing.T) {
	defer withExecutablePath(func() (string, error) { return "", os.ErrNotExist })()
	var out, errOut bytes.Buffer
	code := run([]string{"self", "update"}, &out, &errOut)
	if code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
	if !strings.Contains(errOut.String(), "could not locate") {
		t.Errorf("expected could not locate in stderr, got: %s", errOut.String())
	}
}

func TestRunSelfUpdateHomebrewNoBrew(t *testing.T) {
	defer withExecutablePath(func() (string, error) {
		return "/opt/homebrew/Cellar/pre/1.0/bin/pre", nil
	})()
	defer withLookPath(func(string) (string, error) { return "", os.ErrNotExist })()
	var out, errOut bytes.Buffer
	code := run([]string{"self", "update"}, &out, &errOut)
	if code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
	if !strings.Contains(errOut.String(), "brew is not on PATH") {
		t.Errorf("expected brew is not on PATH in stderr, got: %s", errOut.String())
	}
}

func TestRunSelfUpdateHomebrewCommandFails(t *testing.T) {
	defer withExecutablePath(func() (string, error) {
		return "/opt/homebrew/Cellar/pre/1.0/bin/pre", nil
	})()
	defer withLookPath(func(name string) (string, error) { return "/usr/bin/" + name, nil })()
	defer withCommandRunner(func(name string, args []string, env []string, stdout, stderr io.Writer) error {
		return os.ErrInvalid
	})()
	var out, errOut bytes.Buffer
	code := run([]string{"self", "update"}, &out, &errOut)
	if code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
}

func TestRunSelfUpdateManualBadDir(t *testing.T) {
	defer withExecutablePath(func() (string, error) { return "pre", nil })()
	var out, errOut bytes.Buffer
	code := run([]string{"self", "update"}, &out, &errOut)
	if code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
	if !strings.Contains(errOut.String(), "could not determine binary directory") {
		t.Errorf("expected could not determine binary directory in stderr, got: %s", errOut.String())
	}
}

func TestRunSelfUpdateManualCommandFails(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "pre")
	defer withExecutablePath(func() (string, error) { return exe, nil })()
	scriptContent := []byte("#!/bin/sh\necho updating\n")
	defer withSelfUpdateDownloads(scriptContent)()
	defer withCommandRunner(func(name string, args []string, env []string, stdout, stderr io.Writer) error {
		return os.ErrInvalid
	})()
	var out, errOut bytes.Buffer
	code := run([]string{"self", "update"}, &out, &errOut)
	if code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
}

func TestRunSelfUpdateManualCosignFailsBeforeScript(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "pre")
	defer withExecutablePath(func() (string, error) { return exe, nil })()
	scriptContent := []byte("#!/bin/sh\necho updating\n")
	defer withSelfUpdateDownloads(scriptContent)()
	var ranScript bool
	defer withCommandRunner(func(name string, args []string, env []string, stdout, stderr io.Writer) error {
		if name == "sh" {
			ranScript = true
		}
		return os.ErrInvalid
	})()

	var out, errOut bytes.Buffer
	code := run([]string{"self", "update"}, &out, &errOut)
	if code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
	if ranScript {
		t.Error("expected self update to block before running install.sh")
	}
	if !strings.Contains(errOut.String(), "cosign verification failed") {
		t.Errorf("expected cosign failure in stderr, got: %s", errOut.String())
	}
}

func TestRunSelfUpdateManualDownloadFails(t *testing.T) {
	bundleURL := installChecksumsURL + ".bundle"
	tests := []struct{ asset, url string }{
		{"install.sh", installScriptURL},
		{"checksums.txt", installChecksumsURL},
		{"checksums.txt.bundle", bundleURL},
	}
	for _, tt := range tests {
		t.Run(tt.asset, func(t *testing.T) {
			assertSelfUpdateDownloadFailure(t, tt.asset, tt.url)
		})
	}
}

func assertSelfUpdateDownloadFailure(t *testing.T, asset, url string) {
	t.Helper()
	exe := filepath.Join(t.TempDir(), "pre")
	defer withExecutablePath(func() (string, error) { return exe, nil })()
	defer withFailedSelfUpdateDownload(url)()
	defer withCommandRunner(func(name string, args []string, env []string, stdout, stderr io.Writer) error {
		t.Fatalf("unexpected command after download failure: %s", name)
		return nil
	})()
	var out, errOut bytes.Buffer
	code := run([]string{"self", "update"}, &out, &errOut)
	if code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
	wantMessage := "downloading " + asset
	if !strings.Contains(errOut.String(), wantMessage) {
		t.Errorf("expected %q in stderr, got: %s", wantMessage, errOut.String())
	}
}

func withFailedSelfUpdateDownload(failedURL string) func() {
	return withHTTPGetBytes(func(url string) ([]byte, error) {
		if url == failedURL {
			return nil, os.ErrNotExist
		}
		return nil, nil
	})
}

func TestSelfUpdateMissingTempDirectory(t *testing.T) {
	t.Setenv("TMPDIR", filepath.Join(t.TempDir(), "missing"))
	defer withSelfUpdateDownloads(nil)()
	defer withCommandRunner(func(name string, args []string, env []string, stdout, stderr io.Writer) error {
		t.Fatalf("unexpected command without temporary files: %s", name)
		return nil
	})()
	err := downloadVerifyAndRun(installScriptURL, installChecksumsURL, nil, io.Discard, io.Discard)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected missing checksum directory error, got %v", err)
	}
	err = runInstallScript(nil, nil, io.Discard, io.Discard)
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected missing script directory error, got %v", err)
	}
}

func TestCloseTempWithErrorPreservesFailures(t *testing.T) {
	file, err := os.CreateTemp(t.TempDir(), "write-error-*")
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	writeErr := errors.New("write failed")
	err = closeTempWithError(file, writeErr)
	if err != writeErr {
		t.Fatalf("expected original write error, got %v", err)
	}
	err = closeTempWithError(file, writeErr)
	hasBothErrors := errors.Is(err, writeErr) && errors.Is(err, os.ErrClosed)
	if !hasBothErrors {
		t.Fatalf("expected write and close errors, got %v", err)
	}
}

func TestRunSelfUninstallInvalidArg(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SHELL", "/bin/zsh")
	var out, errOut bytes.Buffer
	code := run([]string{"self", "uninstall", "--invalid"}, &out, &errOut)
	if code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
}

func TestRunSelfUninstallHomebrewNoBrew(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SHELL", "/bin/zsh")
	defer withExecutablePath(func() (string, error) {
		return "/opt/homebrew/Cellar/pre/1.0/bin/pre", nil
	})()
	defer withLookPath(func(string) (string, error) { return "", os.ErrNotExist })()
	var out, errOut bytes.Buffer
	code := run([]string{"self", "uninstall"}, &out, &errOut)
	if code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
	if !strings.Contains(errOut.String(), "brew is not on PATH") {
		t.Errorf("expected brew is not on PATH in stderr, got: %s", errOut.String())
	}
}

func TestRunSelfUninstallHomebrewCommandFails(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SHELL", "/bin/zsh")
	defer withExecutablePath(func() (string, error) {
		return "/opt/homebrew/Cellar/pre/1.0/bin/pre", nil
	})()
	defer withLookPath(func(name string) (string, error) { return "/usr/bin/" + name, nil })()
	defer withCommandRunner(func(name string, args []string, env []string, stdout, stderr io.Writer) error {
		return os.ErrInvalid
	})()
	var out, errOut bytes.Buffer
	code := run([]string{"self", "uninstall"}, &out, &errOut)
	if code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
}

func TestRunSelfUninstallNoBinaryPath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SHELL", "/bin/zsh")
	defer withExecutablePath(func() (string, error) { return "", os.ErrNotExist })()
	var out, errOut bytes.Buffer
	code := run([]string{"self", "uninstall"}, &out, &errOut)
	if code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
	if !strings.Contains(errOut.String(), "could not locate") {
		t.Errorf("expected could not locate in stderr, got: %s", errOut.String())
	}
}

func TestRunSelfUninstallBinaryRemovalFails(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("SHELL", "/bin/zsh")
	dir := t.TempDir()
	exe := filepath.Join(dir, "pre")
	defer withExecutablePath(func() (string, error) { return exe, nil })()
	defer withRemoveFile(func(string) error { return os.ErrInvalid })()
	var out, errOut bytes.Buffer
	code := run([]string{"self", "uninstall"}, &out, &errOut)
	if code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
}

func TestRunPackagesNoArgs(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"packages"}, &out, &errOut)
	if code != 0 {
		t.Errorf("expected exit 0, got %d: %s", code, errOut.String())
	}
}

func TestRunPackagesUnknown(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"packages", "badcmd"}, &out, &errOut)
	if code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
	if !strings.Contains(errOut.String(), "usage:") {
		t.Errorf("expected usage in stderr, got: %s", errOut.String())
	}
}

func TestRunPackagesInstall(t *testing.T) {
	defer withExecutablePath(func() (string, error) { return "/tmp/pre", nil })()
	defer withCommandRunner(func(name string, args []string, env []string, stdout, stderr io.Writer) error {
		return nil
	})()
	var out, errOut bytes.Buffer
	code := run([]string{"packages", "install", "npm", "react"}, &out, &errOut)
	if code != 0 {
		t.Errorf("expected exit 0, got %d: %s", code, errOut.String())
	}
}

func TestRunPackagesUpdate(t *testing.T) {
	defer withExecutablePath(func() (string, error) { return "/tmp/pre", nil })()
	defer withCommandRunner(func(name string, args []string, env []string, stdout, stderr io.Writer) error {
		return nil
	})()
	var out, errOut bytes.Buffer
	code := run([]string{"packages", "update", "npm", "react"}, &out, &errOut)
	if code != 0 {
		t.Errorf("expected exit 0, got %d: %s", code, errOut.String())
	}
}

func TestRunPackagesDowngrade(t *testing.T) {
	defer withExecutablePath(func() (string, error) { return "/tmp/pre", nil })()
	defer withCommandRunner(func(name string, args []string, env []string, stdout, stderr io.Writer) error {
		return nil
	})()
	var out, errOut bytes.Buffer
	code := run([]string{"packages", "downgrade", "pip", "urllib3", "1.0.0"}, &out, &errOut)
	if code != 0 {
		t.Errorf("expected exit 0, got %d: %s", code, errOut.String())
	}
}

func TestRunPackagesUninstall(t *testing.T) {
	defer withExecutablePath(func() (string, error) { return "/tmp/pre", nil })()
	defer withCommandRunner(func(name string, args []string, env []string, stdout, stderr io.Writer) error {
		return nil
	})()
	var out, errOut bytes.Buffer
	code := run([]string{"packages", "uninstall", "brew", "ripgrep"}, &out, &errOut)
	if code != 0 {
		t.Errorf("expected exit 0, got %d: %s", code, errOut.String())
	}
}

func TestRunSelfInstalledHomebrew(t *testing.T) {
	defer withExecutablePath(func() (string, error) {
		return "/opt/homebrew/Cellar/pre/1.2.3/bin/pre", nil
	})()

	var out bytes.Buffer
	code := run([]string{"self", "installed"}, &out, &bytes.Buffer{})
	if code != 0 {
		t.Errorf("expected exit 0, got %d", code)
	}
	if !strings.Contains(out.String(), "Homebrew") {
		t.Errorf("expected Homebrew source label in output, got: %s", out.String())
	}
}

func TestRenderInstallInfoNoBinary(t *testing.T) {
	defer withExecutablePath(func() (string, error) { return "", errors.New("no exe") })()

	var out bytes.Buffer
	code := run([]string{"self", "installed"}, &out, &bytes.Buffer{})
	if code != 0 {
		t.Errorf("expected exit 0, got %d", code)
	}
	if !strings.Contains(out.String(), "unknown") {
		t.Errorf("expected 'unknown' when no binary path, got: %s", out.String())
	}
}

func TestRemoveInstallDirEmpty(t *testing.T) {
	out := removeInstallDir("label", "", &bytes.Buffer{}, &bytes.Buffer{})
	if !out {
		t.Fatal("expected empty dir to return true (no-op)")
	}
	out2 := removeInstallDir("label", ".", &bytes.Buffer{}, &bytes.Buffer{})
	if !out2 {
		t.Fatal("expected '.' dir to return true (no-op)")
	}
}

func TestFileExistsEmpty(t *testing.T) {
	if fileExists("") {
		t.Fatal("expected fileExists('') to return false")
	}
}

func TestRemoveInstallDirError(t *testing.T) {
	orig := removeAllFn
	removeAllFn = func(string) error { return errors.New("rm fail") }
	defer func() { removeAllFn = orig }()

	var errOut bytes.Buffer
	ok := removeInstallDir("cache", "/some/dir", &bytes.Buffer{}, &errOut)
	if ok {
		t.Fatal("expected false when removeAll fails")
	}
	if !strings.Contains(errOut.String(), "rm fail") {
		t.Errorf("expected rm fail in stderr, got: %s", errOut.String())
	}
}

func TestRunExternalCommand(t *testing.T) {
	var out bytes.Buffer
	err := runExternalCommand("echo", []string{"hello"}, nil, &out, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "hello") {
		t.Errorf("expected hello in output, got: %s", out.String())
	}
}

func TestRunExternalCommandWithEnv(t *testing.T) {
	var out bytes.Buffer
	err := runExternalCommand("sh", []string{"-c", "echo $TESTVAR"}, []string{"TESTVAR=world"}, &out, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out.String(), "world") {
		t.Errorf("expected world in output, got: %s", out.String())
	}
}
