package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	precache "github.com/yowainwright/pre/internal/cache"
	preconfig "github.com/yowainwright/pre/internal/config"
	"github.com/yowainwright/pre/internal/manager"
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

	mgr := manager.Get("brew")
	if mgr == nil {
		t.Fatal("expected brew manager")
	}

	pkgs := readHomebrewPackages(mgr)
	got := make(map[string]string, len(pkgs))
	for _, pkg := range pkgs {
		got[pkg.Name] = pkg.Version
	}
	if got["ripgrep"] != "14.1.1" || got["visual-studio-code"] != "1.99.0" {
		t.Fatalf("expected Homebrew filesystem inventory, got %#v", pkgs)
	}
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
	if gotName != "/tmp/pre" || strings.Join(gotArgs, " ") != "npm install react" {
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
	if gotName != "/tmp/pre" || strings.Join(gotArgs, " ") != "npm install react@latest" {
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
	if gotName != "/tmp/pre" || strings.Join(gotArgs, " ") != "pip install urllib3==1.24.1" {
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
	if gotName != "/tmp/pre" || strings.Join(gotArgs, " ") != "brew uninstall ripgrep" {
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
	if !strings.Contains(o, " search ") || !strings.Contains(o, "/rea") {
		t.Errorf("expected search dialog in output, got: %q", o)
	}
}

func TestRunManageSearchQuitsWithoutEnter(t *testing.T) {
	defer withPackageInput("/reaq")()
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
	if !strings.Contains(out.String(), "/rea") {
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
	if gotName != "/tmp/pre" || strings.Join(gotArgs, " ") != "npm install react@18.3.1" {
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
	if gotName != "/tmp/pre" || strings.Join(gotArgs, " ") != "brew uninstall ripgrep" {
		t.Errorf("expected pre brew uninstall ripgrep, got %q %v", gotName, gotArgs)
	}
}

func TestRunSelfUpdateManualInstall(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "pre")
	defer withExecutablePath(func() (string, error) { return exe, nil })()

	var gotName string
	var gotArgs []string
	var gotEnv []string
	defer withCommandRunner(func(name string, args []string, env []string, stdout, stderr io.Writer) error {
		gotName = name
		gotArgs = args
		gotEnv = env
		return nil
	})()

	var out, errOut bytes.Buffer
	code := run([]string{"self", "update"}, &out, &errOut)
	if code != 0 {
		t.Errorf("expected exit 0, got %d: %s", code, errOut.String())
	}
	if gotName != "sh" {
		t.Fatalf("expected sh command, got %q", gotName)
	}
	if len(gotArgs) != 2 || gotArgs[0] != "-c" || !strings.Contains(gotArgs[1], installScriptURL) {
		t.Errorf("expected installer shell command, got %v", gotArgs)
	}
	if len(gotEnv) != 1 || gotEnv[0] != "PRE_BIN_DIR="+dir {
		t.Errorf("expected PRE_BIN_DIR env, got %v", gotEnv)
	}
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
	if gotName != "brew" || strings.Join(gotArgs, " ") != "upgrade pre" {
		t.Errorf("expected brew upgrade pre, got %q %v", gotName, gotArgs)
	}
}

func TestRunSelfUninstallManualInstall(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("SHELL", "/bin/zsh")

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
	content, _ := os.ReadFile(rcPath)
	if strings.Contains(string(content), "# pre security proxy") {
		t.Error("expected uninstall to remove hooks")
	}
	if !strings.Contains(string(content), "export BAR=baz") {
		t.Error("expected uninstall to preserve content after hooks")
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
	if gotName != "brew" || strings.Join(gotArgs, " ") != "uninstall pre" {
		t.Errorf("expected brew uninstall pre, got %q %v", gotName, gotArgs)
	}
}

func TestRunSelfUninstallPurge(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("SHELL", "/bin/zsh")
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
