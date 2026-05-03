package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/yowainwright/pre/internal/manager"
)

func TestManageUIStateTransitions(t *testing.T) {
	ui := newManageUI(testPackageInventory())

	if got := ui.managerSummary(); got != "all" {
		t.Fatalf("expected all managers enabled, got %q", got)
	}
	if quit := handleListKey('j', &ui, manageTerminal{}, io.Discard, io.Discard); quit {
		t.Fatal("down key should not quit")
	}
	if ui.selected != 1 {
		t.Fatalf("expected selected row 1 after down, got %d", ui.selected)
	}
	handleListKey('k', &ui, manageTerminal{}, io.Discard, io.Discard)
	if ui.selected != 0 {
		t.Fatalf("expected selected row 0 after up, got %d", ui.selected)
	}

	handleListKey('/', &ui, manageTerminal{}, io.Discard, io.Discard)
	for _, key := range []int{'r', 'e', 'a', 'c', 't'} {
		handleSearchKey(key, &ui)
	}
	if ui.mode != modeSearch || ui.search != "react" {
		t.Fatalf("expected active react search, got mode=%v search=%q", ui.mode, ui.search)
	}
	if len(ui.filtered) != 1 || ui.filtered[0].Name != "react" {
		t.Fatalf("expected search to filter to react, got %#v", ui.filtered)
	}
	handleSearchKey(keyBackspace, &ui)
	if ui.search != "reac" {
		t.Fatalf("expected backspace to update search, got %q", ui.search)
	}
	handleSearchKey(keyEnter, &ui)
	if ui.mode != modeList {
		t.Fatalf("expected enter to close search, got %v", ui.mode)
	}

	handleListKey('m', &ui, manageTerminal{}, io.Discard, io.Discard)
	if ui.mode != modeManagers {
		t.Fatalf("expected manager dialog, got %v", ui.mode)
	}
	handleManagerKey(keyDown, &ui)
	if ui.managerSelected != 1 {
		t.Fatalf("expected manager selection to move down, got %d", ui.managerSelected)
	}
	handleManagerKey(' ', &ui)
	if ui.managerEnabled[ui.managerOptions[1]] {
		t.Fatalf("expected selected manager to be disabled: %#v", ui.managerEnabled)
	}
	handleManagerKey('a', &ui)
	for name, enabled := range ui.managerEnabled {
		if !enabled {
			t.Fatalf("expected manager %s to be enabled after all", name)
		}
	}
	handleManagerKey('x', &ui)
	if ui.mode != modeList {
		t.Fatalf("expected x to close manager dialog, got %v", ui.mode)
	}

	handleListKey(keyEnter, &ui, manageTerminal{}, io.Discard, io.Discard)
	if ui.mode != modeDialog {
		t.Fatalf("expected action dialog, got %v", ui.mode)
	}
	handleDialogKey('x', &ui, manageTerminal{}, io.Discard, io.Discard)
	if ui.mode != modeList {
		t.Fatalf("expected x to close action dialog, got %v", ui.mode)
	}
	if !handleListKey('q', &ui, manageTerminal{}, io.Discard, io.Discard) {
		t.Fatal("expected q to quit")
	}
}

func TestManageUIInputValidationAndDialogs(t *testing.T) {
	ui := newManageUI(testPackageInventory())

	ui.beginInput(inputInstallManager, "manager")
	if lines := inputDialogLines(ui, 60); len(lines) != 3 || !strings.Contains(lines[0], "manager") {
		t.Fatalf("expected manager input dialog lines, got %#v", lines)
	}
	ui.submitInput(manageTerminal{}, io.Discard, io.Discard)
	if ui.message != "manager is required" {
		t.Fatalf("expected manager required message, got %q", ui.message)
	}

	ui.inputValue = "npm"
	ui.submitInput(manageTerminal{}, io.Discard, io.Discard)
	if ui.inputKind != inputInstallPackage || ui.installManager != "npm" {
		t.Fatalf("expected package input for npm, got kind=%v manager=%q", ui.inputKind, ui.installManager)
	}
	ui.submitInput(manageTerminal{}, io.Discard, io.Discard)
	if ui.message != "package is required" {
		t.Fatalf("expected package required message, got %q", ui.message)
	}

	ui.installManager = "missing"
	ui.inputKind = inputInstallPackage
	ui.inputValue = "left-pad"
	ui.submitInput(manageTerminal{}, io.Discard, io.Discard)
	if ui.message != "unknown manager: missing" || ui.inputKind != inputInstallManager {
		t.Fatalf("expected unknown manager reset, got kind=%v message=%q", ui.inputKind, ui.message)
	}

	ui.pendingPackage = installedPackage{Manager: "missing", Name: "react"}
	ui.pendingAction = actionDowngrade
	ui.inputKind = inputVersion
	ui.inputValue = "17.0.0"
	ui.submitInput(manageTerminal{}, io.Discard, io.Discard)
	if ui.message != "unknown manager: missing" || ui.mode != modeList {
		t.Fatalf("expected unknown version manager message, got mode=%v message=%q", ui.mode, ui.message)
	}

	ui.beginInput(inputInstallPackage, "package")
	handleInputKey('a', &ui, manageTerminal{}, io.Discard, io.Discard)
	handleInputKey('b', &ui, manageTerminal{}, io.Discard, io.Discard)
	handleInputKey(keyBackspace, &ui, manageTerminal{}, io.Discard, io.Discard)
	if ui.inputValue != "a" {
		t.Fatalf("expected input editing to leave a, got %q", ui.inputValue)
	}
	handleInputKey(keyEsc, &ui, manageTerminal{}, io.Discard, io.Discard)
	if ui.mode != modeList || ui.inputValue != "" {
		t.Fatalf("expected esc to cancel input, got mode=%v value=%q", ui.mode, ui.inputValue)
	}
}

func TestManageUIRunActionFromVersionInput(t *testing.T) {
	defer withExecutablePath(func() (string, error) { return "/tmp/pre", nil })()
	defer withLookPath(func(string) (string, error) { return "", os.ErrNotExist })()
	defer withCommandOutput(func(string, []string) ([]byte, error) { return nil, os.ErrNotExist })()
	defer withManageActionPause(func() {})()

	var gotName string
	var gotArgs []string
	defer withCommandRunner(func(name string, args []string, env []string, stdout, stderr io.Writer) error {
		gotName = name
		gotArgs = append([]string(nil), args...)
		return nil
	})()

	ui := newManageUI(packageInventory{Packages: []installedPackage{
		{Manager: "npm", Ecosystem: "npm", Name: "react", Version: "18.2.0"},
	}})
	ui.beginVersionInput(actionDowngrade)
	ui.inputValue = "17.0.0"

	var out bytes.Buffer
	ui.submitInput(manageTerminal{}, &out, io.Discard)

	if gotName != "/tmp/pre" || strings.Join(gotArgs, " ") != "npm install react@17.0.0" {
		t.Fatalf("expected pre npm install react@17.0.0, got %q %v", gotName, gotArgs)
	}
	if ui.mode != modeList || ui.inputValue != "" || ui.message != "downgrade react" {
		t.Fatalf("expected completed action state, got mode=%v value=%q message=%q", ui.mode, ui.inputValue, ui.message)
	}
	if !strings.Contains(out.String(), "running: pre npm install react@17.0.0") {
		t.Fatalf("expected run banner, got %q", out.String())
	}
}

func TestManageUIRunSelectedActionErrors(t *testing.T) {
	ui := newManageUI(packageInventory{Packages: []installedPackage{
		{Manager: "missing", Ecosystem: "unknown", Name: "thing", Version: "1.0.0"},
	}})
	ui.runSelectedAction(actionUpdate, "", manageTerminal{}, io.Discard, io.Discard)
	if ui.message != "unknown manager: missing" {
		t.Fatalf("expected unknown manager message, got %q", ui.message)
	}

	ui = manageUI{}
	ui.runSelectedAction(actionUpdate, "", manageTerminal{}, io.Discard, io.Discard)
	if ui.message != "" {
		t.Fatalf("expected no current package to be a no-op, got %q", ui.message)
	}
}

func TestBuildPackageManagerArgs(t *testing.T) {
	generic := &manager.Manager{Name: "custom", Ecosystem: "npm", InstallCmds: []string{"install", "update"}}
	readonly := &manager.Manager{Name: "readonly", Ecosystem: "npm", InstallCmds: []string{"install"}}
	tests := []struct {
		name    string
		req     packageActionReq
		want    []string
		wantErr string
	}{
		{name: "brew install version", req: packageActionReq{Action: actionInstall, Manager: mustManager(t, "brew"), Package: "ripgrep", Version: "14.1.1"}, want: []string{"install", "ripgrep@14.1.1"}},
		{name: "brew update all", req: packageActionReq{Action: actionUpdate, Manager: mustManager(t, "brew")}, want: []string{"upgrade"}},
		{name: "brew update package", req: packageActionReq{Action: actionUpdate, Manager: mustManager(t, "brew"), Package: "ripgrep"}, want: []string{"upgrade", "ripgrep"}},
		{name: "brew downgrade", req: packageActionReq{Action: actionDowngrade, Manager: mustManager(t, "brew"), Package: "ripgrep", Version: "13.0.0"}, want: []string{"install", "ripgrep@13.0.0"}},
		{name: "npm update latest", req: packageActionReq{Action: actionUpdate, Manager: mustManager(t, "npm"), Package: "react"}, want: []string{"install", "react@latest"}},
		{name: "pnpm remove", req: packageActionReq{Action: actionUninstall, Manager: mustManager(t, "pnpm"), Package: "react"}, want: []string{"remove", "react"}},
		{name: "bun install", req: packageActionReq{Action: actionInstall, Manager: mustManager(t, "bun"), Package: "react", Version: "18.2.0"}, want: []string{"add", "react@18.2.0"}},
		{name: "go update all", req: packageActionReq{Action: actionUpdate, Manager: mustManager(t, "go")}, want: []string{"get", "-u", "./..."}},
		{name: "go uninstall", req: packageActionReq{Action: actionUninstall, Manager: mustManager(t, "go"), Package: "golang.org/x/text"}, want: []string{"get", "golang.org/x/text@none"}},
		{name: "pip update package", req: packageActionReq{Action: actionUpdate, Manager: mustManager(t, "pip"), Package: "urllib3", Version: "1.26.0"}, want: []string{"install", "--upgrade", "urllib3==1.26.0"}},
		{name: "pip update all error", req: packageActionReq{Action: actionUpdate, Manager: mustManager(t, "pip")}, wantErr: "pip updates require a package name"},
		{name: "uv downgrade", req: packageActionReq{Action: actionDowngrade, Manager: mustManager(t, "uv"), Package: "urllib3", Version: "1.26.0"}, want: []string{"add", "urllib3==1.26.0"}},
		{name: "uv update all error", req: packageActionReq{Action: actionUpdate, Manager: mustManager(t, "uv")}, wantErr: "uv updates require a package name"},
		{name: "poetry update all", req: packageActionReq{Action: actionUpdate, Manager: mustManager(t, "poetry")}, want: []string{"update"}},
		{name: "poetry downgrade", req: packageActionReq{Action: actionDowngrade, Manager: mustManager(t, "poetry"), Package: "django", Version: "4.2.0"}, want: []string{"add", "django@4.2.0"}},
		{name: "generic install", req: packageActionReq{Action: actionInstall, Manager: generic, Package: "react@18.2.0"}, want: []string{"install", "react@18.2.0"}},
		{name: "generic update package", req: packageActionReq{Action: actionUpdate, Manager: generic, Package: "react"}, want: []string{"update", "react"}},
		{name: "generic unsupported", req: packageActionReq{Action: actionUninstall, Manager: readonly, Package: "react"}, wantErr: "readonly does not support uninstall"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildPackageManagerArgs(tt.req)
			if tt.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("expected error containing %q, got %v", tt.wantErr, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Fatalf("expected %v, got %v", tt.want, got)
			}
		})
	}
}

func TestPackageActionRequestsAndManageFlags(t *testing.T) {
	req, err := packageActionRequest(actionDowngrade, []string{"pip", "urllib3", "1.24.1"})
	if err != nil {
		t.Fatalf("unexpected downgrade request error: %v", err)
	}
	if req.Manager.Name != "pip" || req.Package != "urllib3" || req.Version != "1.24.1" {
		t.Fatalf("unexpected downgrade request: %#v", req)
	}

	for _, args := range [][]string{
		{"npm"},
		{"missing", "react"},
		{"pip", "urllib3"},
	} {
		if _, err := packageActionRequest(actionDowngrade, args); err == nil {
			t.Fatalf("expected package action error for %v", args)
		}
	}

	req, err = packageActionRequestFromManageFlags([]string{"--manager", "npm", "--package", "react", "--upgrade", "18.3.1"})
	if err != nil {
		t.Fatalf("unexpected manage flag error: %v", err)
	}
	if req.Action != actionUpdate || req.Manager.Name != "npm" || req.Package != "react" || req.Version != "18.3.1" {
		t.Fatalf("unexpected manage flag request: %#v", req)
	}

	for _, args := range [][]string{
		{"--package", "react"},
		{"--manager", "npm", "--install", "--uninstall"},
		{"--manager"},
		{"--manager", "missing", "--package", "react", "--uninstall"},
		{"--package", "react", "--install"},
		{"--unknown"},
	} {
		if _, err := packageActionRequestFromManageFlags(args); err == nil {
			t.Fatalf("expected manage flag error for %v", args)
		}
	}
}

func TestInstalledPackageParsers(t *testing.T) {
	brew := parseBrewPackages(mustManager(t, "brew"), []byte("\nripgrep 14.1.1\nfoo 1.0 2.0\n"))
	assertPackage(t, brew, "ripgrep", "14.1.1")
	assertPackage(t, brew, "foo", "1.0 2.0")

	npm := parseNPMJSONPackages(mustManager(t, "npm"), []byte(`{"dependencies":{"react":{"version":"18.2.0"}}}`))
	assertPackage(t, npm, "react", "18.2.0")
	if got := parseNPMJSONPackages(mustManager(t, "npm"), []byte(`{`)); got != nil {
		t.Fatalf("expected invalid npm json to return nil, got %#v", got)
	}

	pnpm := parsePNPMJSONPackages(mustManager(t, "pnpm"), []byte(`[{"dependencies":{"react":{"version":"18.2.0"}},"devDependencies":{"react":{"version":"18.2.0"},"vite":{"version":"5.0.0"}}}]`))
	if len(pnpm) != 2 {
		t.Fatalf("expected pnpm duplicates to collapse to 2 packages, got %#v", pnpm)
	}
	assertPackage(t, pnpm, "react", "18.2.0")
	assertPackage(t, pnpm, "vite", "5.0.0")
	fallback := parsePNPMJSONPackages(mustManager(t, "pnpm"), []byte(`{"dependencies":{"lodash":{"version":"4.17.21"}}}`))
	assertPackage(t, fallback, "lodash", "4.17.21")

	goPkgs := parseGoListPackages(mustManager(t, "go"), []byte(`{"Path":"example.com/app","Main":true}
{"Path":"golang.org/x/text","Version":"v0.14.0"}
{"Version":"v1.0.0"}
`))
	if len(goPkgs) != 1 {
		t.Fatalf("expected one go dependency, got %#v", goPkgs)
	}
	assertPackage(t, goPkgs, "golang.org/x/text", "v0.14.0")

	pip := parsePipJSONPackages(mustManager(t, "pip"), []byte(`[{"name":"urllib3","version":"2.2.0"},{"name":"","version":"skip"}]`))
	assertPackage(t, pip, "urllib3", "2.2.0")
	if got := parsePipJSONPackages(mustManager(t, "pip"), []byte(`{`)); got != nil {
		t.Fatalf("expected invalid pip json to return nil, got %#v", got)
	}

	poetry := parsePoetryShowPackages(mustManager(t, "poetry"), []byte("cleo 2.1.0 terminal apps\nbad\n"))
	assertPackage(t, poetry, "cleo", "2.1.0")
}

func TestListInstalledPackagesRoutesAndFallbacks(t *testing.T) {
	defer withHomebrewPrefixes(func() []string { return nil })()
	defer withCommandOutput(func(name string, args []string) ([]byte, error) {
		switch name {
		case "brew":
			return []byte("ripgrep 14.1.1\n"), nil
		case "npm":
			return []byte(`{"dependencies":{"react":{"version":"18.2.0"}}}`), nil
		case "pnpm":
			return []byte(`[{"dependencies":{"vite":{"version":"5.0.0"}}}]`), nil
		case "go":
			return []byte(`{"Path":"example.com/app","Main":true}
{"Path":"golang.org/x/text","Version":"v0.14.0"}`), nil
		case "pip", "pip3", "uv":
			return []byte(`[{"name":"urllib3","version":"2.2.0"}]`), nil
		case "poetry":
			return []byte("cleo 2.1.0 terminal apps\n"), nil
		default:
			return nil, os.ErrNotExist
		}
	})()

	for _, name := range []string{"brew", "npm", "pnpm", "go", "pip", "pip3", "uv", "poetry"} {
		t.Run(name, func(t *testing.T) {
			pkgs, err := listInstalledPackages(mustManager(t, name))
			if err != nil {
				t.Fatalf("unexpected list error: %v", err)
			}
			if len(pkgs) == 0 {
				t.Fatalf("expected packages for %s", name)
			}
		})
	}

	pkgs, err := listInstalledPackages(&manager.Manager{Name: "custom", Ecosystem: "unknown"})
	if err != nil {
		t.Fatalf("unexpected default manager error: %v", err)
	}
	if len(pkgs) != 0 {
		t.Fatalf("expected no default packages, got %#v", pkgs)
	}
}

func TestCollectPackageInventoryUsesManifestFallback(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"dependencies":{"react":"18.2.0"}}`), 0644); err != nil {
		t.Fatal(err)
	}
	defer withWorkingDir(t, dir)()
	defer withLookPath(func(name string) (string, error) {
		if name == "npm" {
			return "/usr/bin/npm", nil
		}
		return "", os.ErrNotExist
	})()
	defer withCommandOutput(func(string, []string) ([]byte, error) {
		return nil, errors.New("list failed")
	})()

	inv := collectPackageInventory([]manager.Manager{*mustManager(t, "npm")})
	assertPackage(t, inv.Packages, "react", "18.2.0")
	if len(inv.Errors) != 1 || !strings.Contains(inv.Errors[0], "package manager list failed") {
		t.Fatalf("expected fallback warning, got %#v", inv.Errors)
	}
}

func TestHomebrewPrefixDefaultsAndVersions(t *testing.T) {
	t.Setenv("HOMEBREW_PREFIX", "/tmp/homebrew-test")
	prefixes := defaultHomebrewPrefixes()
	if len(prefixes) < 3 || prefixes[0] != "/tmp/homebrew-test" {
		t.Fatalf("expected env prefix first, got %#v", prefixes)
	}

	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "pkg", "2.0.0"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "pkg", "1.0.0"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "pkg", ".metadata"), 0755); err != nil {
		t.Fatal(err)
	}
	if got := homebrewPackageVersions(filepath.Join(dir, "pkg")); got != "1.0.0 2.0.0" {
		t.Fatalf("expected sorted visible versions, got %q", got)
	}
}

func TestReadManageKeyAndByteReaders(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  int
	}{
		{name: "byte", input: "x", want: 'x'},
		{name: "enter", input: "\n", want: keyEnter},
		{name: "ctrl-c", input: string([]byte{3}), want: keyCtrlC},
		{name: "backspace", input: string([]byte{127}), want: keyBackspace},
		{name: "escape", input: "\x1b", want: keyEsc},
		{name: "up", input: "\x1b[A", want: keyUp},
		{name: "down", input: "\x1b[B", want: keyDown},
		{name: "unknown escape", input: "\x1b[C", want: keyEsc},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := readManageKey(strings.NewReader(tt.input))
			if err != nil {
				t.Fatalf("unexpected read error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("expected %d, got %d", tt.want, got)
			}
		})
	}

	got, err := readByteBlocking(&retryReader{data: []byte("z")})
	if err != nil || got != 'z' {
		t.Fatalf("expected retrying blocking read to return z, got %q err=%v", got, err)
	}
	got, ok := readByteOptional(&retryReader{data: []byte("y")})
	if !ok || got != 'y' {
		t.Fatalf("expected retrying optional read to return y, got %q ok=%v", got, ok)
	}
	if !retryableReadError(syscall.EAGAIN) || !retryableReadError(syscall.EWOULDBLOCK) || !retryableReadError(syscall.EINTR) {
		t.Fatal("expected retryable syscall errors")
	}
	if retryableReadError(io.EOF) {
		t.Fatal("did not expect EOF to be retryable")
	}
}

func TestManageRenderingAndThemeBranches(t *testing.T) {
	t.Setenv("PRE_MANAGE_THEME", "mono")
	if got := themed(currentManageTheme().title, "plain"); got != "plain" {
		t.Fatalf("expected mono theme to leave text plain, got %q", got)
	}
	t.Setenv("PRE_MANAGE_THEME", "contrast")
	if currentManageTheme().selected != manageContrastTheme().selected {
		t.Fatal("expected contrast theme")
	}
	t.Setenv("PRE_MANAGE_THEME", "catppuccin")
	if currentManageTheme().selected != manageDefaultTheme().selected {
		t.Fatal("expected default theme")
	}

	ui := newManageUI(testPackageInventory())
	ui.mode = modeInput
	ui.inputLabel = "version"
	ui.inputValue = "1.2.3"
	if lines := manageDialogLines(ui, 50); len(lines) != 3 || !strings.Contains(lines[1], "1.2.3") {
		t.Fatalf("expected input dialog lines, got %#v", lines)
	}
	if got := managerDialogLines(manageUI{}, 50); len(got) != 3 || !strings.Contains(got[2], "none found") {
		t.Fatalf("expected empty manager dialog, got %#v", got)
	}
	if got := warningLines([]string{"one", "two", "three"}, 40); len(got) != 3 || !strings.Contains(got[2], "1 more") {
		t.Fatalf("expected capped warnings, got %#v", got)
	}
	ui.selected = 8
	ui.offset = 0
	ui.ensureSelectionVisible(4)
	if ui.offset != 5 {
		t.Fatalf("expected selected row to become visible at offset 5, got %d", ui.offset)
	}
}

func TestTerminalSizeAndTimeoutHelpers(t *testing.T) {
	t.Setenv("COLUMNS", "120")
	t.Setenv("LINES", "40")
	if width, height := detectTerminalSize(); width != 120 || height != 40 {
		t.Fatalf("expected env terminal size 120x40, got %dx%d", width, height)
	}
	t.Setenv("COLUMNS", "bad")
	if n, ok := envInt("COLUMNS"); ok || n != 0 {
		t.Fatalf("expected invalid env int to fail, got %d %v", n, ok)
	}
	if width, height := normalizeTerminalSize(10, 5); width != 40 || height != 12 {
		t.Fatalf("expected minimum size 40x12, got %dx%d", width, height)
	}

	t.Setenv("PRE_MANAGE_LIST_TIMEOUT", "15ms")
	if got := packageListTimeout(); got != 15*time.Millisecond {
		t.Fatalf("expected 15ms timeout, got %s", got)
	}
	t.Setenv("PRE_MANAGE_LIST_TIMEOUT", "bad")
	if got := packageListTimeout(); got != 2*time.Second {
		t.Fatalf("expected default timeout, got %s", got)
	}
}

func TestRunPreManagerCommandFallbackExecutable(t *testing.T) {
	defer withExecutablePath(func() (string, error) { return "", errors.New("no executable") })()
	var gotName string
	var gotArgs []string
	defer withCommandRunner(func(name string, args []string, env []string, stdout, stderr io.Writer) error {
		gotName = name
		gotArgs = append([]string(nil), args...)
		return nil
	})()

	if err := runPreManagerCommand(mustManager(t, "npm"), []string{"install", "react"}, io.Discard, io.Discard); err != nil {
		t.Fatalf("unexpected command error: %v", err)
	}
	if gotName != "pre" || strings.Join(gotArgs, " ") != "npm install react" {
		t.Fatalf("expected pre fallback command, got %q %v", gotName, gotArgs)
	}
}

type retryReader struct {
	data  []byte
	calls int
}

func (r *retryReader) Read(p []byte) (int, error) {
	if r.calls == 0 {
		r.calls++
		return 0, syscall.EAGAIN
	}
	if len(r.data) == 0 {
		return 0, io.EOF
	}
	p[0] = r.data[0]
	r.data = r.data[1:]
	return 1, nil
}

func testPackageInventory() packageInventory {
	return packageInventory{Packages: []installedPackage{
		{Manager: "brew", Ecosystem: "Homebrew", Name: "ripgrep", Version: "14.1.1"},
		{Manager: "npm", Ecosystem: "npm", Name: "react", Version: "18.2.0"},
		{Manager: "pip", Ecosystem: "PyPI", Name: "urllib3", Version: "2.2.0"},
		{Manager: "npm", Ecosystem: "npm", Name: "vite", Version: "5.0.0"},
		{Manager: "go", Ecosystem: "Go", Name: "golang.org/x/text", Version: "v0.14.0"},
		{Manager: "poetry", Ecosystem: "PyPI", Name: "cleo", Version: "2.1.0"},
		{Manager: "uv", Ecosystem: "PyPI", Name: "requests", Version: "2.32.0"},
		{Manager: "bun", Ecosystem: "npm", Name: "typescript", Version: "5.4.0"},
		{Manager: "pnpm", Ecosystem: "npm", Name: "eslint", Version: "9.0.0"},
	}}
}

func mustManager(t *testing.T, name string) *manager.Manager {
	t.Helper()
	mgr := manager.Get(name)
	if mgr == nil {
		t.Fatalf("expected manager %s", name)
	}
	return mgr
}

func assertPackage(t *testing.T, pkgs []installedPackage, name, version string) {
	t.Helper()
	for _, pkg := range pkgs {
		if pkg.Name == name {
			if pkg.Version != version {
				t.Fatalf("expected %s version %q, got %q in %#v", name, version, pkg.Version, pkgs)
			}
			return
		}
	}
	t.Fatalf("expected package %s in %#v", name, pkgs)
}

func withManageActionPause(fn func()) func() {
	orig := manageActionPauseFn
	manageActionPauseFn = fn
	return func() { manageActionPauseFn = orig }
}

func withWorkingDir(t *testing.T, dir string) func() {
	t.Helper()
	orig, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	return func() {
		if err := os.Chdir(orig); err != nil {
			t.Fatal(err)
		}
	}
}
