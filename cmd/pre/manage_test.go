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

func TestManageUIListNavigation(t *testing.T) {
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
}

func TestManageUISearchInput(t *testing.T) {
	ui := newManageUI(testPackageInventory())
	handleListKey('/', &ui, manageTerminal{}, io.Discard, io.Discard)
	for _, key := range []int{'r', 'e', 'a', 'c', 't'} {
		handleSearchKey(key, &ui)
	}
	invalidSearch := ui.mode != modeSearch || ui.search != "react"
	if invalidSearch {
		t.Fatalf("expected active react search, got mode=%v search=%q", ui.mode, ui.search)
	}
	invalidResults := len(ui.filtered) != 1 || ui.filtered[0].Name != "react"
	if invalidResults {
		t.Fatalf("expected search to filter to react, got %#v", ui.filtered)
	}
}

func TestManageUISearchEditing(t *testing.T) {
	ui := newManageUI(testPackageInventory())
	ui.mode = modeSearch
	ui.search = "react"
	ui.applyFilter()
	handleSearchKey(keyBackspace, &ui)
	if ui.search != "reac" {
		t.Fatalf("expected backspace to update search, got %q", ui.search)
	}
	handleSearchKey(keyEnter, &ui)
	if ui.mode != modeList {
		t.Fatalf("expected enter to close search, got %v", ui.mode)
	}
}

func TestManageUIManagerNavigation(t *testing.T) {
	ui := newManageUI(testPackageInventory())
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
}

func TestManageUIManagersEnableAndClose(t *testing.T) {
	ui := newManageUI(testPackageInventory())
	ui.mode = modeManagers
	ui.managerEnabled[ui.managerOptions[1]] = false
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
}

func TestManageUIActionDialogTransitions(t *testing.T) {
	ui := newManageUI(testPackageInventory())
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

func TestManageSearchAcceptsCommandKeys(t *testing.T) {
	ui := newManageUI(testPackageInventory())
	ui.toggleSearch()
	for _, key := range []int{'j', 'q', 'k'} {
		if quit := handleSearchKey(key, &ui); quit {
			t.Fatal("search input should not quit")
		}
	}
	if ui.search != "jqk" {
		t.Fatalf("expected search input jqk, got %q", ui.search)
	}
}

func TestManageUIManagerInputRequired(t *testing.T) {
	ui := newManageUI(testPackageInventory())

	ui.beginInput(inputInstallManager, "manager")
	lines := inputDialogLines(ui, 60)
	invalidDialog := len(lines) != 3 || !strings.Contains(lines[0], "manager")
	if invalidDialog {
		t.Fatalf("expected manager input dialog lines, got %#v", lines)
	}
	ui.submitInput(manageTerminal{}, io.Discard, io.Discard)
	if ui.message != "manager is required" {
		t.Fatalf("expected manager required message, got %q", ui.message)
	}
}

func TestManageUIPackageInputRequired(t *testing.T) {
	ui := newManageUI(testPackageInventory())
	ui.beginInput(inputInstallManager, "manager")
	ui.inputValue = "npm"
	ui.submitInput(manageTerminal{}, io.Discard, io.Discard)
	invalidPackageInput := ui.inputKind != inputInstallPackage || ui.installManager != "npm"
	if invalidPackageInput {
		t.Fatalf("expected package input for npm, got kind=%v manager=%q", ui.inputKind, ui.installManager)
	}
	ui.submitInput(manageTerminal{}, io.Discard, io.Discard)
	if ui.message != "package is required" {
		t.Fatalf("expected package required message, got %q", ui.message)
	}
}

func TestManageUIUnknownInstallManager(t *testing.T) {
	ui := newManageUI(testPackageInventory())
	ui.installManager = "missing"
	ui.inputKind = inputInstallPackage
	ui.inputValue = "left-pad"
	ui.submitInput(manageTerminal{}, io.Discard, io.Discard)
	wantMessage := "unknown manager: missing"
	invalidManagerReset := ui.message != wantMessage || ui.inputKind != inputInstallManager
	if invalidManagerReset {
		t.Fatalf("expected unknown manager reset, got kind=%v message=%q", ui.inputKind, ui.message)
	}
}

func TestManageUIUnknownVersionManager(t *testing.T) {
	ui := newManageUI(testPackageInventory())
	ui.pendingPackage = installedPackage{Manager: "missing", Name: "react"}
	ui.pendingAction = actionDowngrade
	ui.inputKind = inputVersion
	ui.inputValue = "17.0.0"
	ui.submitInput(manageTerminal{}, io.Discard, io.Discard)
	wantMessage := "unknown manager: missing"
	invalidVersionReset := ui.message != wantMessage || ui.mode != modeList
	if invalidVersionReset {
		t.Fatalf("expected unknown version manager message, got mode=%v message=%q", ui.mode, ui.message)
	}
}

func TestManageUIInputEditing(t *testing.T) {
	ui := newManageUI(testPackageInventory())
	ui.beginInput(inputInstallPackage, "package")
	handleInputKey('a', &ui, manageTerminal{}, io.Discard, io.Discard)
	handleInputKey('b', &ui, manageTerminal{}, io.Discard, io.Discard)
	handleInputKey(keyBackspace, &ui, manageTerminal{}, io.Discard, io.Discard)
	if ui.inputValue != "a" {
		t.Fatalf("expected input editing to leave a, got %q", ui.inputValue)
	}
	handleInputKey(keyEsc, &ui, manageTerminal{}, io.Discard, io.Discard)
	invalidCancel := ui.mode != modeList || ui.inputValue != ""
	if invalidCancel {
		t.Fatalf("expected esc to cancel input, got mode=%v value=%q", ui.mode, ui.inputValue)
	}
}

func TestManageUIRunActionFromVersionInput(t *testing.T) {
	stubManageActions(t)

	command := captureManageCommand(t)

	inv := packageInventory{Packages: []installedPackage{
		{Manager: "npm", Ecosystem: "npm", Name: "react", Version: "18.2.0"},
	}}
	ui := newManageUI(inv)
	ui.beginVersionInput(actionDowngrade)
	ui.inputValue = "17.0.0"

	var out bytes.Buffer
	ui.submitInput(manageTerminal{}, &out, io.Discard)

	assertVersionActionResult(t, ui, out.String(), command)
}

func TestManageUIRunActionUsesTerminalInput(t *testing.T) {
	stubManageActions(t)

	input := openManageTestInput(t)

	command := captureManageInputCommand(t)

	inv := packageInventory{Packages: []installedPackage{
		{Manager: "npm", Ecosystem: "npm", Name: "react", Version: "18.2.0"},
	}}
	ui := newManageUI(inv)
	ui.runSelectedAction(actionUpdate, "", manageTerminal{input: input}, io.Discard, io.Discard)

	if command.stdin != input {
		t.Fatalf("expected child stdin to use terminal input, got %#v", command.stdin)
	}
	if strings.Join(command.args, " ") != "npm install react@latest" {
		t.Fatalf("expected pre npm install react@latest, got %v", command.args)
	}
}

func TestManageUIRunSelectedActionErrors(t *testing.T) {
	inv := packageInventory{Packages: []installedPackage{
		{Manager: "missing", Ecosystem: "unknown", Name: "thing", Version: "1.0.0"},
	}}
	ui := newManageUI(inv)
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
	tests := brewPackageArgCases(t)
	tests = append(tests, javascriptAndGoPackageArgCases(t)...)
	tests = append(tests, cargoPackageArgCases(t)...)
	tests = append(tests, pythonPackageArgCases(t)...)
	tests = append(tests, genericPackageArgCases()...)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertPackageManagerArgs(t, tt)
		})
	}
}

func TestPackageActionDowngradeRequest(t *testing.T) {
	req, err := packageActionRequest(actionDowngrade, []string{"pip", "urllib3", "1.24.1"})
	if err != nil {
		t.Fatalf("unexpected downgrade request error: %v", err)
	}
	wrongPackage := req.Manager.Name != "pip" || req.Package != "urllib3"
	unexpectedRequest := wrongPackage || req.Version != "1.24.1"
	if unexpectedRequest {
		t.Fatalf("unexpected downgrade request: %#v", req)
	}
}

func TestPackageActionDowngradeInvalidRequests(t *testing.T) {
	for _, args := range [][]string{
		{"npm"},
		{"missing", "react"},
		{"pip", "urllib3"},
	} {
		if _, err := packageActionRequest(actionDowngrade, args); err == nil {
			t.Fatalf("expected package action error for %v", args)
		}
	}
}

func TestManageFlagsVersionedUpgrade(t *testing.T) {
	req, err := packageActionRequestFromManageFlags([]string{"--manager", "npm", "--package", "react", "--upgrade", "18.3.1"})
	if err != nil {
		t.Fatalf("unexpected manage flag error: %v", err)
	}
	wrongAction := req.Action != actionUpdate || req.Manager.Name != "npm"
	wrongPackage := req.Package != "react" || req.Version != "18.3.1"
	unexpectedFlagRequest := wrongAction || wrongPackage
	if unexpectedFlagRequest {
		t.Fatalf("unexpected manage flag request: %#v", req)
	}
}

func TestManageFlagsInvalidRequests(t *testing.T) {
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

func TestInstalledBrewPackageParser(t *testing.T) {
	brew := parseBrewPackages(mustManager(t, "brew"), []byte("\nripgrep 14.1.1\nfoo 1.0 2.0\n"))
	assertPackage(t, brew, "ripgrep", "14.1.1")
	assertPackage(t, brew, "foo", "1.0 2.0")
}

func TestInstalledNPMPackageParser(t *testing.T) {
	npm := parseNPMJSONPackages(mustManager(t, "npm"), []byte(`{"dependencies":{"react":{"version":"18.2.0"}}}`))
	assertPackage(t, npm, "react", "18.2.0")
	if got := parseNPMJSONPackages(mustManager(t, "npm"), []byte(`{`)); got != nil {
		t.Fatalf("expected invalid npm json to return nil, got %#v", got)
	}
}

func TestInstalledPNPMPackageParser(t *testing.T) {
	pnpm := parsePNPMJSONPackages(mustManager(t, "pnpm"), []byte(`[{"dependencies":{"react":{"version":"18.2.0"}},"devDependencies":{"react":{"version":"18.2.0"},"vite":{"version":"5.0.0"}}}]`))
	if len(pnpm) != 2 {
		t.Fatalf("expected pnpm duplicates to collapse to 2 packages, got %#v", pnpm)
	}
	assertPackage(t, pnpm, "react", "18.2.0")
	assertPackage(t, pnpm, "vite", "5.0.0")
	fallback := parsePNPMJSONPackages(mustManager(t, "pnpm"), []byte(`{"dependencies":{"lodash":{"version":"4.17.21"}}}`))
	assertPackage(t, fallback, "lodash", "4.17.21")
}

func TestInstalledGoPackageParser(t *testing.T) {
	goPkgs := parseGoListPackages(mustManager(t, "go"), []byte(`{"Path":"example.com/app","Main":true}
{"Path":"golang.org/x/text","Version":"v0.14.0"}
{"Version":"v1.0.0"}
`))
	if len(goPkgs) != 1 {
		t.Fatalf("expected one go dependency, got %#v", goPkgs)
	}
	assertPackage(t, goPkgs, "golang.org/x/text", "v0.14.0")
}

func TestInstalledPipPackageParser(t *testing.T) {
	pip := parsePipJSONPackages(mustManager(t, "pip"), []byte(`[{"name":"urllib3","version":"2.2.0"},{"name":"","version":"skip"}]`))
	assertPackage(t, pip, "urllib3", "2.2.0")
	if got := parsePipJSONPackages(mustManager(t, "pip"), []byte(`{`)); got != nil {
		t.Fatalf("expected invalid pip json to return nil, got %#v", got)
	}
}

func TestInstalledPoetryPackageParser(t *testing.T) {
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

func TestListInstalledPackagesCargoUsesDirectManifestDependencies(t *testing.T) {
	dir := t.TempDir()
	manifest := "[dependencies]\nserde = \"1.0\"\n"
	manifestPath := filepath.Join(dir, "Cargo.toml")
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	writeCargoInventoryLockfile(t, dir)
	defer withWorkingDir(t, dir)()

	packages, err := listInstalledPackages(mustManager(t, "cargo"))
	if err != nil {
		t.Fatalf("unexpected list error: %v", err)
	}
	assertPackage(t, packages, "serde", "^1.0")
	if packageNamed(packages, "syn") {
		t.Fatal("expected transitive lockfile package to be excluded")
	}
}

func TestCargoManifestPackagesDiscoversParentManifest(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "src", "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "[dependencies]\nserde = \"1.0\"\n"
	if err := os.WriteFile(filepath.Join(dir, "Cargo.toml"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	defer withWorkingDir(t, nested)()

	packages, err := cargoManifestPackages(mustManager(t, "cargo"))
	if err != nil {
		t.Fatalf("read parent Cargo manifest: %v", err)
	}
	assertPackage(t, packages, "serde", "^1.0")
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
	invalidWarnings := len(inv.Errors) != 1 || !strings.Contains(inv.Errors[0], "package manager list failed")
	if invalidWarnings {
		t.Fatalf("expected fallback warning, got %#v", inv.Errors)
	}
}

func TestHomebrewPrefixDefaults(t *testing.T) {
	t.Setenv("HOMEBREW_PREFIX", "/tmp/homebrew-test")
	prefixes := defaultHomebrewPrefixes()
	invalidPrefixes := len(prefixes) < 3 || prefixes[0] != "/tmp/homebrew-test"
	if invalidPrefixes {
		t.Fatalf("expected env prefix first, got %#v", prefixes)
	}
}

func TestHomebrewPackageVersions(t *testing.T) {
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

func TestReadManageKey(t *testing.T) {
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
}

func TestReadByteRetries(t *testing.T) {
	got, err := readByteBlocking(&retryReader{data: []byte("z")})
	invalidRead := err != nil || got != 'z'
	if invalidRead {
		t.Fatalf("expected retrying blocking read to return z, got %q err=%v", got, err)
	}
	got, ok := readByteOptional(&retryReader{data: []byte("y")})
	invalidOptionalRead := !ok || got != 'y'
	if invalidOptionalRead {
		t.Fatalf("expected retrying optional read to return y, got %q ok=%v", got, ok)
	}
}

func TestRetryableReadErrors(t *testing.T) {
	for _, err := range []error{syscall.EAGAIN, syscall.EWOULDBLOCK, syscall.EINTR} {
		if !retryableReadError(err) {
			t.Fatalf("expected retryable syscall error: %v", err)
		}
	}
	if retryableReadError(io.EOF) {
		t.Fatal("did not expect EOF to be retryable")
	}
}

func TestManageThemeBranches(t *testing.T) {
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
}

func TestManageInputDialogRendering(t *testing.T) {
	ui := newManageUI(testPackageInventory())
	ui.mode = modeInput
	ui.inputLabel = "version"
	ui.inputValue = "1.2.3"
	lines := manageDialogLines(ui, 50)
	invalidInputDialog := len(lines) != 3 || !strings.Contains(lines[1], "1.2.3")
	if invalidInputDialog {
		t.Fatalf("expected input dialog lines, got %#v", lines)
	}
}

func TestManageEmptyManagerDialogRendering(t *testing.T) {
	got := managerDialogLines(manageUI{}, 50)
	invalidEmptyDialog := len(got) != 3 || !strings.Contains(got[2], "none found")
	if invalidEmptyDialog {
		t.Fatalf("expected empty manager dialog, got %#v", got)
	}
}

func TestManageWarningRendering(t *testing.T) {
	got := warningLines([]string{"one", "two", "three"}, 40)
	invalidWarnings := len(got) != 3 || !strings.Contains(got[2], "1 more")
	if invalidWarnings {
		t.Fatalf("expected capped warnings, got %#v", got)
	}
}

func TestManageSelectionBecomesVisible(t *testing.T) {
	ui := newManageUI(testPackageInventory())
	ui.selected = 8
	ui.offset = 0
	ui.ensureSelectionVisible(4)
	if ui.offset != 5 {
		t.Fatalf("expected selected row to become visible at offset 5, got %d", ui.offset)
	}
}

func TestTerminalSizeEnvironment(t *testing.T) {
	t.Setenv("COLUMNS", "120")
	t.Setenv("LINES", "40")
	width, height := detectTerminalSize()
	invalidSize := width != 120 || height != 40
	if invalidSize {
		t.Fatalf("expected env terminal size 120x40, got %dx%d", width, height)
	}
}

func TestTerminalSizeInvalidEnvironment(t *testing.T) {
	t.Setenv("COLUMNS", "bad")
	n, ok := envInt("COLUMNS")
	invalidEnv := ok || n != 0
	if invalidEnv {
		t.Fatalf("expected invalid env int to fail, got %d %v", n, ok)
	}
}

func TestTerminalSizeMinimum(t *testing.T) {
	width, height := normalizeTerminalSize(10, 5)
	invalidMinimum := width != 40 || height != 12
	if invalidMinimum {
		t.Fatalf("expected minimum size 40x12, got %dx%d", width, height)
	}
}

func TestPackageListTimeout(t *testing.T) {
	t.Setenv("PRE_MANAGE_LIST_TIMEOUT", "15ms")
	if got := packageListTimeout(); got != 15*time.Millisecond {
		t.Fatalf("expected 15ms timeout, got %s", got)
	}
	t.Setenv("PRE_MANAGE_LIST_TIMEOUT", "bad")
	if got := packageListTimeout(); got != 2*time.Second {
		t.Fatalf("expected default timeout, got %s", got)
	}
}

func TestExecutePackageActionFallbackExecutable(t *testing.T) {
	defer withExecutablePath(func() (string, error) { return "", errors.New("no executable") })()
	var gotName string
	var gotArgs []string
	defer withCommandRunner(func(name string, args []string, env []string, stdout, stderr io.Writer) error {
		gotName = name
		gotArgs = append([]string(nil), args...)
		return nil
	})()

	req := packageActionReq{Action: actionInstall, Manager: mustManager(t, "npm"), Package: "react"}
	if err := executePackageAction(req, io.Discard, io.Discard); err != nil {
		t.Fatalf("unexpected command error: %v", err)
	}
	unexpectedCommand := gotName != "pre" || strings.Join(gotArgs, " ") != "npm install react"
	if unexpectedCommand {
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

func packageNamed(packages []installedPackage, name string) bool {
	for _, pkg := range packages {
		if pkg.Name == name {
			return true
		}
	}
	return false
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

func TestHandleDialogKeyActions(t *testing.T) {
	stubManageActions(t)
	captureManageCommand(t)
	tests := []manageKeyCase{
		{key: 'u', mode: modeList},
		{key: 'd', mode: modeInput, pendingAction: actionDowngrade},
		{key: 'r', mode: modeList},
		{key: 'i', mode: modeList},
	}
	for _, tt := range tests {
		t.Run(string(rune(tt.key)), func(t *testing.T) {
			ui := newManageUI(reactPackageInventory())
			ui.mode = modeDialog
			handleDialogKey(tt.key, &ui, manageTerminal{}, io.Discard, io.Discard)
			assertManageKeyState(t, ui, tt)
		})
	}
}

func TestToggleSearchFromSearch(t *testing.T) {
	ui := newManageUI(testPackageInventory())
	ui.mode = modeSearch
	ui.toggleSearch()
	if ui.mode != modeList {
		t.Fatalf("expected list mode after toggleSearch from search, got %v", ui.mode)
	}
}

func TestToggleManagersFromManagers(t *testing.T) {
	ui := newManageUI(testPackageInventory())
	ui.mode = modeManagers
	ui.toggleManagers()
	if ui.mode != modeList {
		t.Fatalf("expected list mode after toggleManagers from managers, got %v", ui.mode)
	}
}

func TestToggleDialogFromDialog(t *testing.T) {
	ui := newManageUI(testPackageInventory())
	ui.mode = modeDialog
	ui.toggleDialog()
	if ui.mode != modeList {
		t.Fatalf("expected list mode after toggleDialog from dialog, got %v", ui.mode)
	}
}

func TestToggleDialogEmptyList(t *testing.T) {
	ui := manageUI{}
	ui.mode = modeList
	ui.toggleDialog()
	if ui.mode != modeList {
		t.Fatalf("expected mode to stay list with no packages, got %v", ui.mode)
	}
}

func TestMoveSelectionEmpty(t *testing.T) {
	empty := manageUI{}
	empty.moveSelection(1)
	if empty.selected != 0 {
		t.Fatalf("expected no-op on empty list, got %d", empty.selected)
	}
}

func TestMoveSelectionBoundaries(t *testing.T) {
	inv := packageInventory{Packages: []installedPackage{
		{Manager: "npm", Name: "a"},
		{Manager: "npm", Name: "b"},
	}}
	ui := newManageUI(inv)
	ui.selected = 0
	ui.moveSelection(-1)
	if ui.selected != 0 {
		t.Fatalf("expected selection to stay at 0, got %d", ui.selected)
	}

	ui.selected = len(ui.filtered) - 1
	ui.moveSelection(1)
	if ui.selected != len(ui.filtered)-1 {
		t.Fatalf("expected selection to stay at last, got %d", ui.selected)
	}
}

func TestMoveManagerSelectionEmpty(t *testing.T) {
	empty := manageUI{}
	empty.moveManagerSelection(1)
	if empty.managerSelected != 0 {
		t.Fatalf("expected no-op on empty managers, got %d", empty.managerSelected)
	}
}

func TestMoveManagerSelectionBoundaries(t *testing.T) {
	inv := packageInventory{Packages: []installedPackage{
		{Manager: "npm", Name: "a"},
		{Manager: "brew", Name: "b"},
	}}
	ui := newManageUI(inv)
	ui.managerSelected = 0
	ui.moveManagerSelection(-1)
	if ui.managerSelected != 0 {
		t.Fatalf("expected manager selection to stay at 0, got %d", ui.managerSelected)
	}

	ui.managerSelected = len(ui.managerOptions) - 1
	ui.moveManagerSelection(1)
	if ui.managerSelected != len(ui.managerOptions)-1 {
		t.Fatalf("expected manager selection to stay at last, got %d", ui.managerSelected)
	}
}

func TestBuildGoUVPoetryArgs(t *testing.T) {
	tests := versionedGoArgCases(t)
	tests = append(tests, uvInstallUpdateArgCases(t)...)
	tests = append(tests, poetryInstallUpdateArgCases(t)...)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assertPackageManagerArgs(t, tt)
		})
	}
}

func TestPackageActionUsage(t *testing.T) {
	actions := []packageAction{actionInstall, actionUpdate, actionUninstall, actionDowngrade, "unknown"}
	for _, action := range actions {
		got := packageActionUsage(action)
		if got == "" {
			t.Fatalf("expected non-empty usage for action %q", action)
		}
		missingAction := action != "unknown" && !strings.Contains(got, string(action))
		if missingAction {
			t.Fatalf("expected usage for %q to contain action name, got %q", action, got)
		}
	}
}

func TestEmptyDash(t *testing.T) {
	if got := emptyDash(""); got != "-" {
		t.Fatalf("expected - for empty string, got %q", got)
	}
	if got := emptyDash("val"); got != "val" {
		t.Fatalf("expected val for non-empty string, got %q", got)
	}
}

func TestHandleSearchKeyAcceptsQ(t *testing.T) {
	ui := newManageUI(testPackageInventory())
	if handleSearchKey('q', &ui) {
		t.Fatal("expected q to update the search")
	}
	if ui.search != "q" {
		t.Fatalf("expected q search, got %q", ui.search)
	}
}

func TestHandleManagerKeyQuit(t *testing.T) {
	ui := newManageUI(testPackageInventory())
	if !handleManagerKey('q', &ui) {
		t.Fatal("expected q to return true (quit)")
	}
}

func TestRunManageHelpFlag(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"manage", "--help"}, &out, &errOut)
	if code != 0 {
		t.Errorf("expected exit 0, got %d: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "usage:") {
		t.Errorf("expected usage in stdout, got: %s", out.String())
	}
}

func TestRunManageListFlag(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"manage", "--list"}, &out, &errOut)
	if code != 0 {
		t.Errorf("expected exit 0, got %d: %s", code, errOut.String())
	}
}

func TestRunManageUnknownSubcmd(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"manage", "badcmd"}, &out, &errOut)
	if code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
	if !strings.Contains(errOut.String(), "usage:") {
		t.Errorf("expected usage in stderr, got: %s", errOut.String())
	}
}

func TestRunManageFlagError(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"manage", "--unknown-flag"}, &out, &errOut)
	if code != 1 {
		t.Errorf("expected exit 1, got %d", code)
	}
	if !strings.Contains(errOut.String(), "pre manage:") {
		t.Errorf("expected pre manage: in stderr, got: %s", errOut.String())
	}
}

func TestRunManageInstallSubcmd(t *testing.T) {
	defer withExecutablePath(func() (string, error) { return "/tmp/pre", nil })()
	defer withCommandRunner(func(name string, args []string, env []string, stdout, stderr io.Writer) error {
		return nil
	})()
	var out, errOut bytes.Buffer
	code := run([]string{"manage", "install", "npm", "react"}, &out, &errOut)
	if code != 0 {
		t.Errorf("expected exit 0, got %d: %s", code, errOut.String())
	}
}

func TestHandleListKeyActionKeys(t *testing.T) {
	defer withExecutablePath(func() (string, error) { return "/tmp/pre", nil })()
	defer withManageActionPause(func() {})()
	defer withCommandOutput(func(string, []string) ([]byte, error) { return nil, os.ErrNotExist })()
	captureManageCommand(t)
	tests := []manageKeyCase{
		{key: 'u', mode: modeList},
		{key: 'd', mode: modeInput, pendingAction: actionDowngrade},
		{key: 'r', mode: modeList},
		{key: 'i', mode: modeInput},
	}
	for _, tt := range tests {
		t.Run(string(rune(tt.key)), func(t *testing.T) {
			ui := newManageUI(reactPackageInventory())
			handleListKey(tt.key, &ui, manageTerminal{}, io.Discard, io.Discard)
			assertManageKeyState(t, ui, tt)
		})
	}
}

func TestHandleManageKeyCtrlC(t *testing.T) {
	ui := newManageUI(testPackageInventory())
	if !handleManageKey(keyCtrlC, &ui, manageTerminal{}, io.Discard, io.Discard) {
		t.Fatal("expected ctrl-c to quit")
	}
}

func TestRunActionBuildError(t *testing.T) {
	readonly := &manager.Manager{Name: "readonly", Ecosystem: "npm", InstallCmds: []string{"install"}}
	req := packageActionReq{Action: actionUninstall, Manager: readonly, Package: "react"}
	ui := manageUI{}
	ui.runAction(req, manageTerminal{}, io.Discard, io.Discard)
	if ui.message == "" {
		t.Fatal("expected error message from unsupported action")
	}
	if ui.mode != modeList {
		t.Fatalf("expected list mode after build error, got %v", ui.mode)
	}
}

func TestEnsureSelectionVisibleNegative(t *testing.T) {
	inv := packageInventory{Packages: []installedPackage{
		{Manager: "npm", Name: "a"},
		{Manager: "npm", Name: "b"},
		{Manager: "npm", Name: "c"},
	}}
	ui := newManageUI(inv)
	ui.selected = -1
	ui.ensureSelectionVisible(2)
	if ui.selected != 0 {
		t.Fatalf("expected selected clamped to 0, got %d", ui.selected)
	}

	ui.offset = 5
	ui.selected = 1
	ui.ensureSelectionVisible(2)
	if ui.offset > ui.selected {
		t.Fatalf("expected offset <= selected, got offset=%d selected=%d", ui.offset, ui.selected)
	}
}

func TestBeginVersionInputNoPackage(t *testing.T) {
	ui := manageUI{}
	ui.beginVersionInput(actionDowngrade)
	if ui.mode == modeInput {
		t.Fatal("expected no input mode when no package selected")
	}
}

func TestCurrentPackageEdges(t *testing.T) {
	ui := manageUI{}
	if _, ok := ui.currentPackage(); ok {
		t.Fatal("expected no current package in empty ui")
	}

	ui2 := newManageUI(testPackageInventory())
	ui2.selected = -1
	if _, ok := ui2.currentPackage(); ok {
		t.Fatal("expected no current package when selected < 0")
	}
}

func TestFitLinePlainText(t *testing.T) {
	if got := fitLine("hi", 0); got != "" {
		t.Errorf("expected empty for width=0, got %q", got)
	}
	if got := fitLine("hello world", 5); got != "he..." {
		t.Errorf("expected truncation, got %q", got)
	}
	if got := fitLine("hi", 10); len(got) != 10 {
		t.Errorf("expected padding to width 10, got len %d", len(got))
	}
}

func TestTruncatePlainText(t *testing.T) {
	if got := truncate("abc", 0); got != "abc" {
		t.Errorf("expected original string for max=0, got %q", got)
	}
	if got := truncate("abc", 2); got != "ab" {
		t.Errorf("expected truncate to 2, got %q", got)
	}
	if got := truncate("abc", 3); got != "abc" {
		t.Errorf("expected no truncate at max=3, got %q", got)
	}
	if got := truncate("abcdef", 4); got != "a..." {
		t.Errorf("expected ellipsis truncate, got %q", got)
	}
}

func TestFitLineANSIPadding(t *testing.T) {
	styled := themed(manageDefaultTheme().selected, "hello")
	got := fitLine(styled, 10)
	invalidPadding := visibleWidth(got) != 10 || !strings.HasSuffix(got, ansiReset)
	if invalidPadding {
		t.Errorf("expected ANSI line padded to visible width 10 with reset, got %q width %d", got, visibleWidth(got))
	}
}

func TestFitLineANSITruncation(t *testing.T) {
	got := fitLine(themed(manageDefaultTheme().selected, "hello world"), 5)
	invalidANSI := visibleWidth(got) != 5 || !strings.HasSuffix(got, ansiReset)
	invalidTruncation := invalidANSI || !strings.Contains(got, "he...")
	if invalidTruncation {
		t.Errorf("expected ANSI line truncated visibly with reset, got %q width %d", got, visibleWidth(got))
	}
}

func TestManageColumnWidths(t *testing.T) {
	pkgW, verW := manageColumnWidths(100)
	if verW != 18 {
		t.Errorf("expected versionWidth=18 for wide terminal, got %d", verW)
	}
	if pkgW < 12 {
		t.Errorf("expected packageWidth>=12 for wide terminal, got %d", pkgW)
	}
	pkgW2, verW2 := manageColumnWidths(60)
	if verW2 != 12 {
		t.Errorf("expected versionWidth=12 for narrow terminal, got %d", verW2)
	}
	if pkgW2 < 12 {
		t.Errorf("expected packageWidth>=12 for narrow terminal, got %d", pkgW2)
	}
}

func TestManageFooterMessage(t *testing.T) {
	inv := testPackageInventory()
	ui := newManageUI(inv)
	ui.filtered = inv.Packages
	ui.message = "test msg"

	line := manageFooterLine(ui, 5, 80)
	if !strings.Contains(line, "test msg") {
		t.Errorf("expected message in footer, got %q", line)
	}
}

func TestManageFooterScrollIndicators(t *testing.T) {
	inv := testPackageInventory()
	ui2 := newManageUI(inv)
	ui2.filtered = make([]installedPackage, 20)
	ui2.offset = 10
	line2 := manageFooterLine(ui2, 5, 80)
	if !strings.Contains(line2, "↑ more") {
		t.Errorf("expected scroll indicator in footer, got %q", line2)
	}
	if !strings.Contains(line2, "↓ more") {
		t.Errorf("expected down indicator in footer, got %q", line2)
	}
}

func TestRenderPackageInventoryEmpty(t *testing.T) {
	var out bytes.Buffer
	renderPackageInventory(&out, packageInventory{})
	if !strings.Contains(out.String(), "none found") {
		t.Errorf("expected 'none found', got %q", out.String())
	}
}

func TestRenderPackageInventoryWithErrors(t *testing.T) {
	var out bytes.Buffer
	inv := packageInventory{
		Packages: []installedPackage{{Manager: "npm", Name: "react", Version: "18.0.0"}},
		Errors:   []string{"npm: failed"},
	}
	renderPackageInventory(&out, inv)
	if !strings.Contains(out.String(), "react") {
		t.Errorf("expected package in output, got %q", out.String())
	}
	if !strings.Contains(out.String(), "warning: npm: failed") {
		t.Errorf("expected warning in output, got %q", out.String())
	}
}

func TestParseManageFlagsInstallUpgradeConflict(t *testing.T) {
	req, err := parseManageFlags([]string{"--install", "1.0.0", "--upgrade"})
	missingConflict := err == nil || !strings.Contains(err.Error(), "choose only one")
	if missingConflict {
		t.Errorf("expected conflict error, got err=%v req=%+v", err, req)
	}
}

func TestParseManageFlagsUpgradeDowngradeConflict(t *testing.T) {
	req2, err2 := parseManageFlags([]string{"--upgrade", "2.0.0", "--downgrade"})
	missingUpgradeConflict := err2 == nil || !strings.Contains(err2.Error(), "choose only one")
	if missingUpgradeConflict {
		t.Errorf("expected conflict error on upgrade+downgrade, got err=%v req=%+v", err2, req2)
	}
}

func TestParseManageFlagsDowngradeVersion(t *testing.T) {
	req3, err3 := parseManageFlags([]string{"--downgrade", "1.0.0"})
	if err3 != nil {
		t.Fatalf("unexpected error: %v", err3)
	}
	if req3.version != "1.0.0" {
		t.Errorf("expected version 1.0.0, got %q", req3.version)
	}
}

func TestParseManageFlagsUninstallInstallConflict(t *testing.T) {
	req4, err4 := parseManageFlags([]string{"--uninstall", "--install"})
	missingUninstallConflict := err4 == nil || !strings.Contains(err4.Error(), "choose only one")
	if missingUninstallConflict {
		t.Errorf("expected conflict on uninstall+install, got err=%v req=%+v", err4, req4)
	}
}

func TestParseManageFlagsInstallWithoutVersion(t *testing.T) {
	req5, err5 := parseManageFlags([]string{"--install"})
	invalidInstall := req5.action != actionInstall || err5 != nil
	if invalidInstall {
		t.Errorf("expected install action, got err=%v req=%+v", err5, req5)
	}
}

func TestParseManageFlagsUnknown(t *testing.T) {
	_, err6 := parseManageFlags([]string{"--unknown-flag"})
	if err6 == nil {
		t.Error("expected error for unknown flag")
	}
}

func TestResolveManageManagerUnknown(t *testing.T) {
	_, err := resolveManageManager(manageFlagRequest{managerName: "notreal"})
	unexpectedError := err == nil || !strings.Contains(err.Error(), "unknown manager")
	if unexpectedError {
		t.Errorf("expected unknown manager error, got %v", err)
	}
}

func TestResolveManageManagerInstallNoManager(t *testing.T) {
	_, err := resolveManageManager(manageFlagRequest{action: actionInstall})
	unexpectedError := err == nil || !strings.Contains(err.Error(), "--manager is required")
	if unexpectedError {
		t.Errorf("expected --manager required error, got %v", err)
	}
}

func TestHandlePackageActionError(t *testing.T) {
	var errOut bytes.Buffer
	code := handlePackageAction(actionInstall, []string{}, &bytes.Buffer{}, &errOut)
	if code != 1 {
		t.Errorf("expected exit 1 on missing args, got %d", code)
	}
}

func TestHandlePackageActionExecuteError(t *testing.T) {
	orig := commandRunnerFn
	commandRunnerFn = func(string, []string, []string, io.Writer, io.Writer) error {
		return errors.New("exec failed")
	}
	defer func() { commandRunnerFn = orig }()

	var errOut bytes.Buffer
	code := handlePackageAction(actionInstall, []string{"npm", "react"}, &bytes.Buffer{}, &errOut)
	if code != 1 {
		t.Errorf("expected exit 1 on exec error, got %d", code)
	}
	if !strings.Contains(errOut.String(), "exec failed") {
		t.Errorf("expected error message, got %q", errOut.String())
	}
}

func TestBuildPipArgsMissingPackage(t *testing.T) {
	mgr := mustManager(t, "pip")

	got, err := buildPipArgs(packageActionReq{Action: actionUpdate, Manager: mgr, Package: ""}, "")
	unexpectedError := err == nil || !strings.Contains(err.Error(), "require a package name")
	if unexpectedError {
		t.Errorf("expected require package error, got err=%v got=%v", err, got)
	}
}

func TestBuildPipArgsVersionedUpdate(t *testing.T) {
	mgr := mustManager(t, "pip")
	got2, err2 := buildPipArgs(packageActionReq{Action: actionUpdate, Manager: mgr, Package: "requests", Version: "2.28.0"}, "requests")
	unexpectedUpgrade := err2 != nil || !reflect.DeepEqual(got2, []string{"install", "--upgrade", "requests==2.28.0"})
	if unexpectedUpgrade {
		t.Errorf("expected versioned upgrade, got err=%v got=%v", err2, got2)
	}
}

func TestBuildPipArgsDowngrade(t *testing.T) {
	mgr := mustManager(t, "pip")
	got3, err3 := buildPipArgs(packageActionReq{Action: actionDowngrade, Manager: mgr, Package: "requests", Version: "2.27.0"}, "requests")
	unexpectedDowngrade := err3 != nil || !reflect.DeepEqual(got3, []string{"install", "requests==2.27.0"})
	if unexpectedDowngrade {
		t.Errorf("expected downgrade args, got err=%v got=%v", err3, got3)
	}
}

func TestBuildPipArgsUnsupported(t *testing.T) {
	mgr := mustManager(t, "pip")
	got4, err4 := buildPipArgs(packageActionReq{Action: "invalid", Manager: mgr}, "")
	if err4 == nil {
		t.Errorf("expected unsupported action error, got %v", got4)
	}
}

func TestBuildUVArgsAllCases(t *testing.T) {
	mgr := mustManager(t, "uv")

	got, err := buildUVArgs(packageActionReq{Action: actionUpdate, Manager: mgr, Package: ""}, "")
	unexpectedError := err == nil || !strings.Contains(err.Error(), "require a package name")
	if unexpectedError {
		t.Errorf("expected require package error, got err=%v got=%v", err, got)
	}

	got2, err2 := buildUVArgs(packageActionReq{Action: actionDowngrade, Manager: mgr, Package: "requests", Version: "2.27.0"}, "requests")
	unexpectedDowngrade := err2 != nil || !reflect.DeepEqual(got2, []string{"pip", "install", "requests==2.27.0"})
	if unexpectedDowngrade {
		t.Errorf("expected downgrade args, got err=%v got=%v", err2, got2)
	}

	got3, err3 := buildUVArgs(packageActionReq{Action: "invalid", Manager: mgr}, "")
	if err3 == nil {
		t.Errorf("expected unsupported action error, got %v", got3)
	}
}

func TestBuildPoetryArgsUpdateAll(t *testing.T) {
	mgr := mustManager(t, "poetry")

	got, err := buildPoetryArgs(packageActionReq{Action: actionUpdate, Manager: mgr, Package: ""}, "")
	unexpectedUpdate := err != nil || !reflect.DeepEqual(got, []string{"update"})
	if unexpectedUpdate {
		t.Errorf("expected global update, got err=%v got=%v", err, got)
	}
}

func TestBuildPoetryArgsVersionedUpdate(t *testing.T) {
	mgr := mustManager(t, "poetry")
	got2, err2 := buildPoetryArgs(packageActionReq{Action: actionUpdate, Manager: mgr, Package: "django", Version: "4.2.0"}, "django")
	unexpectedVersionedUpdate := err2 != nil || !reflect.DeepEqual(got2, []string{"add", "django@4.2.0"})
	if unexpectedVersionedUpdate {
		t.Errorf("expected versioned update, got err=%v got=%v", err2, got2)
	}
}

func TestBuildPoetryArgsUninstall(t *testing.T) {
	mgr := mustManager(t, "poetry")
	got3, err3 := buildPoetryArgs(packageActionReq{Action: actionUninstall, Manager: mgr, Package: "django"}, "django")
	unexpectedUninstall := err3 != nil || !reflect.DeepEqual(got3, []string{"remove", "django"})
	if unexpectedUninstall {
		t.Errorf("expected uninstall args, got err=%v got=%v", err3, got3)
	}
}

func TestBuildPoetryArgsUnsupported(t *testing.T) {
	mgr := mustManager(t, "poetry")
	got4, err4 := buildPoetryArgs(packageActionReq{Action: "invalid", Manager: mgr}, "")
	if err4 == nil {
		t.Errorf("expected unsupported action error, got %v", got4)
	}
}

func TestBuildBrewArgsAllCases(t *testing.T) {
	mgr := mustManager(t, "brew")

	got, err := buildBrewArgs(packageActionReq{Action: actionUpdate, Manager: mgr, Package: ""}, "")
	unexpectedUpgrade := err != nil || !reflect.DeepEqual(got, []string{"upgrade"})
	if unexpectedUpgrade {
		t.Errorf("expected global upgrade, got err=%v got=%v", err, got)
	}

	got2, err2 := buildBrewArgs(packageActionReq{Action: actionUpdate, Manager: mgr, Package: "git", Version: "2.40.0"}, "git")
	unexpectedVersionedUpgrade := err2 != nil || !reflect.DeepEqual(got2, []string{"install", "git@2.40.0"})
	if unexpectedVersionedUpgrade {
		t.Errorf("expected versioned upgrade, got err=%v got=%v", err2, got2)
	}

	got3, err3 := buildBrewArgs(packageActionReq{Action: "invalid", Manager: mgr}, "")
	if err3 == nil {
		t.Errorf("expected unsupported action error, got %v", got3)
	}
}

func TestBuildNPMArgsAllCases(t *testing.T) {
	mgr := mustManager(t, "npm")

	got, err := buildNPMArgs(packageActionReq{Action: actionUpdate, Manager: mgr, Package: ""}, "", "install", "uninstall")
	unexpectedUpdate := err != nil || !reflect.DeepEqual(got, []string{"update"})
	if unexpectedUpdate {
		t.Errorf("expected global update, got err=%v got=%v", err, got)
	}

	got2, err2 := buildNPMArgs(packageActionReq{Action: actionUpdate, Manager: mgr, Package: "react", Version: "18.0.0"}, "react", "install", "uninstall")
	unexpectedVersionedUpdate := err2 != nil || !reflect.DeepEqual(got2, []string{"install", "react@18.0.0"})
	if unexpectedVersionedUpdate {
		t.Errorf("expected versioned update, got err=%v got=%v", err2, got2)
	}

	got3, err3 := buildNPMArgs(packageActionReq{Action: "invalid", Manager: mgr}, "", "install", "uninstall")
	if err3 == nil {
		t.Errorf("expected unsupported action error, got %v", got3)
	}
}

func TestManagerSupportsCommand(t *testing.T) {
	if managerSupportsCommand(nil, "install") {
		t.Error("expected false for nil manager")
	}
	mgr := &manager.Manager{InstallCmds: []string{"add", "install"}}
	if !managerSupportsCommand(mgr, "add") {
		t.Error("expected true for 'add' cmd")
	}
	if managerSupportsCommand(mgr, "remove") {
		t.Error("expected false for unsupported cmd")
	}
}

func TestActionDialogLinesNoPackage(t *testing.T) {
	ui := manageUI{}
	lines := actionDialogLines(ui, 80)
	if lines != nil {
		t.Errorf("expected nil when no current package, got %v", lines)
	}
}

func TestActionDialogLinesWithPackage(t *testing.T) {
	ui := newManageUI(testPackageInventory())
	ui.selected = 0
	lines := actionDialogLines(ui, 80)
	if len(lines) != 4 {
		t.Errorf("expected 4 dialog lines, got %d", len(lines))
	}
}

func TestManagerDialogLinesEmpty(t *testing.T) {
	ui := manageUI{}
	lines := managerDialogLines(ui, 80)
	found := false
	for _, l := range lines {
		if strings.Contains(l, "none found") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'none found' in dialog lines, got %v", lines)
	}
}

func TestManagerSummaryEmpty(t *testing.T) {
	ui := manageUI{}
	if got := ui.managerSummary(); got != "none" {
		t.Errorf("expected 'none' for empty ui, got %q", got)
	}
}

func TestManagerSummaryDisableEnableAll(t *testing.T) {
	ui2 := newManageUI(testPackageInventory())
	for k := range ui2.managerEnabled {
		ui2.managerEnabled[k] = false
	}
	if got := ui2.managerSummary(); got != "none" {
		t.Errorf("expected 'none' when all disabled, got %q", got)
	}

	for k := range ui2.managerEnabled {
		ui2.managerEnabled[k] = true
	}
	if got := ui2.managerSummary(); got != "all" {
		t.Errorf("expected 'all' when all enabled, got %q", got)
	}
}

func TestManagerSummaryManyEnabled(t *testing.T) {
	ui3 := manageUI{
		managerOptions: []string{"npm", "brew", "go", "pip", "uv"},
		managerEnabled: map[string]bool{"npm": true, "brew": true, "go": true, "pip": true, "uv": false},
	}
	got := ui3.managerSummary()
	if !strings.Contains(got, "/") {
		t.Errorf("expected N/M summary for many enabled, got %q", got)
	}
}

func TestExecutePackageActionError(t *testing.T) {
	mgr := mustManager(t, "npm")
	orig := commandRunnerFn
	commandRunnerFn = func(string, []string, []string, io.Writer, io.Writer) error {
		return errors.New("command failed")
	}
	defer func() { commandRunnerFn = orig }()

	err := executePackageAction(packageActionReq{Action: actionInstall, Manager: mgr, Package: "react"}, &bytes.Buffer{}, &bytes.Buffer{})
	unexpectedError := err == nil || !strings.Contains(err.Error(), "command failed")
	if unexpectedError {
		t.Errorf("expected command failed error, got %v", err)
	}
}

func TestDebugManageTiming(t *testing.T) {
	t.Setenv("PRE_MANAGE_DEBUG", "1")
	debugManageTiming("npm", []string{"list"}, time.Now().Add(-time.Millisecond*5), nil)
	debugManageTiming("npm", []string{"list"}, time.Now().Add(-time.Millisecond*5), errors.New("fail"))
}

func TestBuildGoArgsUpdateAll(t *testing.T) {
	mgr := mustManager(t, "go")

	got, err := buildGoArgs(packageActionReq{Action: actionUpdate, Manager: mgr, Package: ""}, "")
	unexpectedUpdate := err != nil || !reflect.DeepEqual(got, []string{"get", "-u", "./..."})
	if unexpectedUpdate {
		t.Errorf("expected global update, got err=%v got=%v", err, got)
	}
}

func TestBuildGoArgsLatestUpdate(t *testing.T) {
	mgr := mustManager(t, "go")
	got2, err2 := buildGoArgs(packageActionReq{Action: actionUpdate, Manager: mgr, Package: "golang.org/x/text"}, "golang.org/x/text")
	unexpectedLatestUpdate := err2 != nil || !reflect.DeepEqual(got2, []string{"get", "golang.org/x/text@latest"})
	if unexpectedLatestUpdate {
		t.Errorf("expected latest update, got err=%v got=%v", err2, got2)
	}
}

func TestBuildGoArgsUninstall(t *testing.T) {
	mgr := mustManager(t, "go")
	got3, err3 := buildGoArgs(packageActionReq{Action: actionUninstall, Manager: mgr, Package: "golang.org/x/text"}, "golang.org/x/text")
	unexpectedUninstall := err3 != nil || !reflect.DeepEqual(got3, []string{"get", "golang.org/x/text@none"})
	if unexpectedUninstall {
		t.Errorf("expected uninstall args, got err=%v got=%v", err3, got3)
	}
}

func TestBuildGoArgsUnsupported(t *testing.T) {
	mgr := mustManager(t, "go")
	_, err4 := buildGoArgs(packageActionReq{Action: "invalid", Manager: mgr}, "")
	if err4 == nil {
		t.Error("expected unsupported action error")
	}
}

func TestBuildPipArgsUpdateNoVersion(t *testing.T) {
	mgr := mustManager(t, "pip")
	got, err := buildPipArgs(packageActionReq{Action: actionUpdate, Manager: mgr, Package: "requests"}, "requests")
	unexpectedUpdate := err != nil || !reflect.DeepEqual(got, []string{"install", "--upgrade", "requests"})
	if unexpectedUpdate {
		t.Errorf("expected upgrade without version, got err=%v got=%v", err, got)
	}

	got2, err2 := buildPipArgs(packageActionReq{Action: actionUninstall, Manager: mgr, Package: "requests"}, "requests")
	unexpectedUninstall := err2 != nil || !reflect.DeepEqual(got2, []string{"uninstall", "-y", "requests"})
	if unexpectedUninstall {
		t.Errorf("expected uninstall args, got err=%v got=%v", err2, got2)
	}
}

func TestBuildUVArgsUpdateNoVersion(t *testing.T) {
	mgr := mustManager(t, "uv")
	got, err := buildUVArgs(packageActionReq{Action: actionUpdate, Manager: mgr, Package: "requests"}, "requests")
	unexpectedUpdate := err != nil || !reflect.DeepEqual(got, []string{"pip", "install", "--upgrade", "requests"})
	if unexpectedUpdate {
		t.Errorf("expected update without version, got err=%v got=%v", err, got)
	}

	got2, err2 := buildUVArgs(packageActionReq{Action: actionUninstall, Manager: mgr, Package: "requests"}, "requests")
	unexpectedUninstall := err2 != nil || !reflect.DeepEqual(got2, []string{"pip", "uninstall", "requests"})
	if unexpectedUninstall {
		t.Errorf("expected remove args, got err=%v got=%v", err2, got2)
	}
}

func TestTruncateEdgeCases(t *testing.T) {
	if got := truncate("ab", 1); got != "a" {
		t.Errorf("expected single char for max=1, got %q", got)
	}
	if got := truncate("abc", 2); got != "ab" {
		t.Errorf("expected 2 chars for max=2, got %q", got)
	}
}

func TestPackageWithVersionEdgeCases(t *testing.T) {
	mgr := mustManager(t, "npm")
	if got := packageWithVersion(mgr, "", "1.0.0"); got != "" {
		t.Errorf("expected empty for empty spec, got %q", got)
	}
	if got := packageWithVersion(mgr, "react", ""); got != "react" {
		t.Errorf("expected spec unchanged when no version, got %q", got)
	}
}

func TestExecutePackageActionBuildError(t *testing.T) {
	mgr := mustManager(t, "npm")
	err := executePackageAction(packageActionReq{Action: "invalid", Manager: mgr, Package: "react"}, &bytes.Buffer{}, &bytes.Buffer{})
	unexpectedError := err == nil || !strings.Contains(err.Error(), "does not support")
	if unexpectedError {
		t.Errorf("expected unsupported action error, got %v", err)
	}
}

func TestApplyFilterOffsetClamping(t *testing.T) {
	ui := newManageUI(testPackageInventory())
	ui.search = "react"
	ui.applyFilter()
	ui.selected = 0
	ui.offset = 5
	ui.search = ""
	ui.applyFilter()
	if ui.offset > ui.selected {
		t.Errorf("expected offset <= selected after filter, got offset=%d selected=%d", ui.offset, ui.selected)
	}
}

func TestHandleInputKeyBackspaceEmpty(t *testing.T) {
	ui := newManageUI(testPackageInventory())
	ui.beginInput(inputInstallManager, "manager")
	ui.inputValue = ""
	handleInputKey(keyBackspace, &ui, manageTerminal{}, io.Discard, io.Discard)
	if ui.inputValue != "" {
		t.Errorf("expected empty input after backspace on empty, got %q", ui.inputValue)
	}
	handleInputKey(1, &ui, manageTerminal{}, io.Discard, io.Discard)
	if ui.inputValue != "" {
		t.Errorf("expected no change for control char, got %q", ui.inputValue)
	}
}

func TestHandleSearchKeyBackspaceAndInput(t *testing.T) {
	ui := newManageUI(testPackageInventory())
	ui.mode = modeSearch
	ui.search = ""
	handleSearchKey(keyBackspace, &ui)
	if ui.search != "" {
		t.Errorf("expected empty search after backspace on empty")
	}
	handleSearchKey('r', &ui)
	if ui.search != "r" {
		t.Errorf("expected search='r', got %q", ui.search)
	}
	handleSearchKey(1, &ui)
	if ui.search != "r" {
		t.Errorf("expected search unchanged for control char, got %q", ui.search)
	}
}

func TestHandleManagerKeyEnableAll(t *testing.T) {
	ui := newManageUI(testPackageInventory())
	ui.mode = modeManagers
	for k := range ui.managerEnabled {
		ui.managerEnabled[k] = false
	}
	handleManagerKey('a', &ui)
	for k, v := range ui.managerEnabled {
		if !v {
			t.Errorf("expected manager %s enabled after 'a', got false", k)
		}
	}
}

func TestHandleManageKeyInputMode(t *testing.T) {
	ui := newManageUI(testPackageInventory())
	ui.mode = modeInput
	ui.inputValue = ""
	handleManageKey('a', &ui, manageTerminal{}, io.Discard, io.Discard)
	if ui.inputValue != "a" {
		t.Errorf("expected 'a' typed in input mode, got %q", ui.inputValue)
	}
}

func TestReadEscapeKeyUp(t *testing.T) {
	upSeq := bytes.NewReader([]byte{'[', 'A'})
	key := readEscapeKey(upSeq)
	if key != keyUp {
		t.Errorf("expected keyUp, got %d", key)
	}
}

func TestReadEscapeKeyDown(t *testing.T) {
	downSeq := bytes.NewReader([]byte{'[', 'B'})
	key2 := readEscapeKey(downSeq)
	if key2 != keyDown {
		t.Errorf("expected keyDown, got %d", key2)
	}
}

func TestReadEscapeKeyNonBracket(t *testing.T) {
	notBracket := bytes.NewReader([]byte{'O'})
	key3 := readEscapeKey(notBracket)
	if key3 != keyEsc {
		t.Errorf("expected keyEsc for non-bracket, got %d", key3)
	}
}

func TestReadEscapeKeyUnknown(t *testing.T) {
	unknownSeq := bytes.NewReader([]byte{'[', 'Z'})
	key4 := readEscapeKey(unknownSeq)
	if key4 != keyEsc {
		t.Errorf("expected keyEsc for unknown sequence, got %d", key4)
	}
}

func TestEnsureSelectionVisibleScrollDown(t *testing.T) {
	pkgs := make([]installedPackage, 10)
	for i := range pkgs {
		pkgs[i] = installedPackage{Manager: "npm", Name: "pkg"}
	}
	ui := newManageUI(packageInventory{Packages: pkgs})
	ui.selected = 9
	ui.offset = 0
	ui.ensureSelectionVisible(5)
	if ui.offset+5 <= ui.selected {
		t.Errorf("expected selected to be visible, offset=%d selected=%d", ui.offset, ui.selected)
	}
}

func TestToggleSelectedManagerEmpty(t *testing.T) {
	ui := manageUI{}
	ui.toggleSelectedManager()
}

func TestSubmitInputVersionEmpty(t *testing.T) {
	ui := newManageUI(testPackageInventory())
	ui.inputKind = inputVersion
	ui.inputValue = ""
	ui.submitInput(manageTerminal{}, io.Discard, io.Discard)
	if ui.message != "version is required" {
		t.Errorf("expected 'version is required', got %q", ui.message)
	}
}

func TestCurrentManageTheme(t *testing.T) {
	t.Setenv("PRE_MANAGE_THEME", "mono")
	theme := currentManageTheme()
	if theme.title != "" {
		t.Errorf("expected empty mono theme, got %+v", theme)
	}

	t.Setenv("PRE_MANAGE_THEME", "contrast")
	theme2 := currentManageTheme()
	if theme2.title == "" {
		t.Errorf("expected non-empty contrast theme")
	}

	t.Setenv("PRE_MANAGE_THEME", "unknown-theme")
	theme3 := currentManageTheme()
	if theme3.title == "" {
		t.Errorf("expected default theme for unknown value")
	}
}

func TestHandleSearchKeyNavigation(t *testing.T) {
	ui := newManageUI(testPackageInventory())
	ui.mode = modeSearch
	ui.selected = 1
	handleSearchKey(keyUp, &ui)
	if ui.selected != 0 {
		t.Errorf("expected selected=0 after keyUp in search, got %d", ui.selected)
	}
	handleSearchKey(keyDown, &ui)
	if ui.selected != 1 {
		t.Errorf("expected selected=1 after keyDown in search, got %d", ui.selected)
	}
}

func TestHandleManagerKeyEnter(t *testing.T) {
	ui := newManageUI(testPackageInventory())
	ui.mode = modeManagers
	first := ui.managerOptions[0]
	before := ui.managerEnabled[first]
	handleManagerKey(keyEnter, &ui)
	if ui.managerEnabled[first] == before {
		t.Errorf("expected manager toggle on keyEnter")
	}
}

func TestHandleDialogKeyEsc(t *testing.T) {
	ui := newManageUI(testPackageInventory())
	ui.mode = modeDialog
	handleDialogKey(keyEsc, &ui, manageTerminal{}, io.Discard, io.Discard)
	if ui.mode != modeList {
		t.Errorf("expected modeList after keyEsc in dialog, got %v", ui.mode)
	}
}

func TestHandleInputKeyEnter(t *testing.T) {
	ui := newManageUI(testPackageInventory())
	ui.mode = modeInput
	ui.inputKind = inputInstallManager
	ui.inputValue = ""
	handleInputKey(keyEnter, &ui, manageTerminal{}, io.Discard, io.Discard)
	if ui.message != "manager is required" {
		t.Errorf("expected manager required message, got %q", ui.message)
	}
}

func TestEnsureSelectionVisibleSmallPageSize(t *testing.T) {
	inv := packageInventory{Packages: []installedPackage{
		{Manager: "npm", Name: "a"},
	}}
	ui := newManageUI(inv)
	ui.selected = 0
	ui.ensureSelectionVisible(5)
	if ui.offset != 0 {
		t.Errorf("expected offset=0 when page > list size, got %d", ui.offset)
	}
}

func TestResolveManageManagerNotFound(t *testing.T) {
	defer withLookPath(func(string) (string, error) { return "", os.ErrNotExist })()
	_, err := resolveManageManager(manageFlagRequest{action: actionUpdate, packageName: "missing-pkg"})
	unexpectedError := err == nil || !strings.Contains(err.Error(), "not found")
	if unexpectedError {
		t.Errorf("expected not found error, got %v", err)
	}
}

func TestPackageWithVersionEmptyName(t *testing.T) {
	mgr := mustManager(t, "npm")
	result := packageWithVersion(mgr, "   ", "1.0.0")
	if result != "" {
		t.Errorf("expected empty for whitespace-only spec, got %q", result)
	}
}

func TestBuildGenericPackageArgsUnsupportedUpdate(t *testing.T) {
	mgr := &manager.Manager{Name: "custom", InstallCmds: []string{"add"}}
	_, err := buildGenericPackageArgs(packageActionReq{Action: actionUpdate, Manager: mgr, Package: "thing"})
	unexpectedError := err == nil || !strings.Contains(err.Error(), "does not support")
	if unexpectedError {
		t.Errorf("expected unsupported error, got %v", err)
	}
}

func TestManagerDialogLinesScrolling(t *testing.T) {
	managers := []string{"npm", "brew", "go", "pip", "uv", "poetry", "pnpm", "bun", "cargo"}
	enabled := make(map[string]bool)
	for _, m := range managers {
		enabled[m] = true
	}
	ui := manageUI{
		managerOptions:  managers,
		managerEnabled:  enabled,
		managerSelected: 8,
	}
	lines := managerDialogLines(ui, 80)
	text := strings.Join(lines, "\n")
	if !strings.Contains(text, "↑ more") {
		t.Errorf("expected ↑ more scroll indicator for large list, got %v", lines)
	}
}

func assertVersionActionResult(t *testing.T, ui manageUI, output string, command *manageCommandCapture) {
	t.Helper()
	unexpectedCommand := command.name != "/tmp/pre" || strings.Join(command.args, " ") != "npm install react@17.0.0"
	if unexpectedCommand {
		t.Fatalf("expected pre npm install react@17.0.0, got %q %v", command.name, command.args)
	}
	unfinished := ui.mode != modeList || ui.inputValue != ""
	unexpectedState := unfinished || ui.message != "downgrade react"
	if unexpectedState {
		t.Fatalf("expected completed action state, got mode=%v value=%q message=%q", ui.mode, ui.inputValue, ui.message)
	}
	if !strings.Contains(output, "running: pre npm install react@17.0.0") {
		t.Fatalf("expected run banner, got %q", output)
	}
}

func stubManageActions(t *testing.T) {
	t.Helper()
	t.Cleanup(withExecutablePath(func() (string, error) { return "/tmp/pre", nil }))
	t.Cleanup(withLookPath(func(string) (string, error) { return "", os.ErrNotExist }))
	t.Cleanup(withCommandOutput(func(string, []string) ([]byte, error) { return nil, os.ErrNotExist }))
	t.Cleanup(withManageActionPause(func() {}))
}

type manageCommandCapture struct {
	name  string
	args  []string
	stdin io.Reader
}

func captureManageCommand(t *testing.T) *manageCommandCapture {
	t.Helper()
	command := &manageCommandCapture{}
	t.Cleanup(withCommandRunner(func(name string, args []string, env []string, stdout, stderr io.Writer) error {
		command.name = name
		command.args = append([]string(nil), args...)
		return nil
	}))
	return command
}

func captureManageInputCommand(t *testing.T) *manageCommandCapture {
	t.Helper()
	command := &manageCommandCapture{}
	t.Cleanup(withCommandRunnerWithInput(func(name string, args []string, env []string, streams commandStreams) error {
		command.stdin = streams.stdin
		command.args = append([]string(nil), args...)
		return nil
	}))
	t.Cleanup(withCommandRunner(func(string, []string, []string, io.Writer, io.Writer) error {
		t.Fatal("expected input-aware command runner")
		return nil
	}))
	return command
}

func openManageTestInput(t *testing.T) *os.File {
	t.Helper()
	input, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = input.Close() })
	return input
}

func reactPackageInventory() packageInventory {
	return packageInventory{Packages: []installedPackage{
		{Manager: "npm", Ecosystem: "npm", Name: "react", Version: "18.2.0"},
	}}
}

type manageKeyCase struct {
	key           int
	mode          manageMode
	pendingAction packageAction
}

func assertManageKeyState(t *testing.T, ui manageUI, want manageKeyCase) {
	t.Helper()
	if ui.mode != want.mode {
		t.Fatalf("key %q: expected mode %v, got %v", want.key, want.mode, ui.mode)
	}
	if want.pendingAction == "" {
		return
	}
	if ui.pendingAction != want.pendingAction {
		t.Fatalf("key %q: expected pending action %v, got %v", want.key, want.pendingAction, ui.pendingAction)
	}
}

func writeCargoInventoryLockfile(t *testing.T, dir string) {
	t.Helper()
	lockfile := `[[package]]
name = "serde"
version = "1.0.217"
source = "registry+https://index.crates.io/"

[[package]]
name = "syn"
version = "2.0.0"
source = "registry+https://index.crates.io/"
`
	lockPath := filepath.Join(dir, "Cargo.lock")
	if err := os.WriteFile(lockPath, []byte(lockfile), 0o644); err != nil {
		t.Fatal(err)
	}
}

type packageArgsCase struct {
	name    string
	req     packageActionReq
	want    []string
	wantErr string
}

func assertPackageManagerArgs(t *testing.T, tt packageArgsCase) {
	t.Helper()
	got, err := buildPackageManagerArgs(tt.req)
	if tt.wantErr != "" {
		unexpectedError := err == nil || !strings.Contains(err.Error(), tt.wantErr)
		if unexpectedError {
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
}

func brewPackageArgCases(t *testing.T) []packageArgsCase {
	t.Helper()
	return []packageArgsCase{
		{name: "brew install version", req: packageActionReq{Action: actionInstall, Manager: mustManager(t, "brew"), Package: "ripgrep", Version: "14.1.1"}, want: []string{"install", "ripgrep@14.1.1"}},
		{name: "brew update all", req: packageActionReq{Action: actionUpdate, Manager: mustManager(t, "brew")}, want: []string{"upgrade"}},
		{name: "brew update package", req: packageActionReq{Action: actionUpdate, Manager: mustManager(t, "brew"), Package: "ripgrep"}, want: []string{"upgrade", "ripgrep"}},
		{name: "brew downgrade", req: packageActionReq{Action: actionDowngrade, Manager: mustManager(t, "brew"), Package: "ripgrep", Version: "13.0.0"}, want: []string{"install", "ripgrep@13.0.0"}},
	}
}

func javascriptAndGoPackageArgCases(t *testing.T) []packageArgsCase {
	t.Helper()
	return []packageArgsCase{
		{name: "npm update latest", req: packageActionReq{Action: actionUpdate, Manager: mustManager(t, "npm"), Package: "react"}, want: []string{"install", "react@latest"}},
		{name: "pnpm remove", req: packageActionReq{Action: actionUninstall, Manager: mustManager(t, "pnpm"), Package: "react"}, want: []string{"remove", "react"}},
		{name: "bun install", req: packageActionReq{Action: actionInstall, Manager: mustManager(t, "bun"), Package: "react", Version: "18.2.0"}, want: []string{"add", "react@18.2.0"}},
		{name: "go update all", req: packageActionReq{Action: actionUpdate, Manager: mustManager(t, "go")}, want: []string{"get", "-u", "./..."}},
		{name: "go uninstall", req: packageActionReq{Action: actionUninstall, Manager: mustManager(t, "go"), Package: "golang.org/x/text"}, want: []string{"get", "golang.org/x/text@none"}},
	}
}

func cargoPackageArgCases(t *testing.T) []packageArgsCase {
	t.Helper()
	return []packageArgsCase{
		{name: "cargo install exact", req: packageActionReq{Action: actionInstall, Manager: mustManager(t, "cargo"), Package: "serde", Version: "1.0.217"}, want: []string{"add", "serde@=1.0.217"}},
		{name: "cargo update all", req: packageActionReq{Action: actionUpdate, Manager: mustManager(t, "cargo")}, want: []string{"update"}},
		{name: "cargo update exact", req: packageActionReq{Action: actionUpdate, Manager: mustManager(t, "cargo"), Package: "serde", Version: "1.0.217"}, want: []string{"update", "serde", "--precise", "1.0.217"}},
		{name: "cargo uninstall", req: packageActionReq{Action: actionUninstall, Manager: mustManager(t, "cargo"), Package: "serde"}, want: []string{"remove", "serde"}},
		{name: "cargo downgrade", req: packageActionReq{Action: actionDowngrade, Manager: mustManager(t, "cargo"), Package: "serde", Version: "1.0.200"}, want: []string{"update", "serde", "--precise", "1.0.200"}},
	}
}

func pythonPackageArgCases(t *testing.T) []packageArgsCase {
	t.Helper()
	return []packageArgsCase{
		{name: "pip update package", req: packageActionReq{Action: actionUpdate, Manager: mustManager(t, "pip"), Package: "urllib3", Version: "1.26.0"}, want: []string{"install", "--upgrade", "urllib3==1.26.0"}},
		{name: "pip update all error", req: packageActionReq{Action: actionUpdate, Manager: mustManager(t, "pip")}, wantErr: "pip updates require a package name"},
		{name: "uv downgrade", req: packageActionReq{Action: actionDowngrade, Manager: mustManager(t, "uv"), Package: "urllib3", Version: "1.26.0"}, want: []string{"pip", "install", "urllib3==1.26.0"}},
		{name: "uv uninstall", req: packageActionReq{Action: actionUninstall, Manager: mustManager(t, "uv"), Package: "urllib3"}, want: []string{"pip", "uninstall", "urllib3"}},
		{name: "uv update all error", req: packageActionReq{Action: actionUpdate, Manager: mustManager(t, "uv")}, wantErr: "uv updates require a package name"},
		{name: "poetry update all", req: packageActionReq{Action: actionUpdate, Manager: mustManager(t, "poetry")}, want: []string{"update"}},
		{name: "poetry downgrade", req: packageActionReq{Action: actionDowngrade, Manager: mustManager(t, "poetry"), Package: "django", Version: "4.2.0"}, want: []string{"add", "django@4.2.0"}},
	}
}

func genericPackageArgCases() []packageArgsCase {
	generic := &manager.Manager{Name: "custom", Ecosystem: "npm", InstallCmds: []string{"install", "update"}}
	readonly := &manager.Manager{Name: "readonly", Ecosystem: "npm", InstallCmds: []string{"install"}}
	return []packageArgsCase{
		{name: "generic install", req: packageActionReq{Action: actionInstall, Manager: generic, Package: "react@18.2.0"}, want: []string{"install", "react@18.2.0"}},
		{name: "generic update package", req: packageActionReq{Action: actionUpdate, Manager: generic, Package: "react"}, want: []string{"update", "react"}},
		{name: "generic unsupported", req: packageActionReq{Action: actionUninstall, Manager: readonly, Package: "react"}, wantErr: "readonly does not support uninstall"},
	}
}

func versionedGoArgCases(t *testing.T) []packageArgsCase {
	t.Helper()
	return []packageArgsCase{
		{
			name: "go install with version",
			req:  packageActionReq{Action: actionInstall, Manager: mustManager(t, "go"), Package: "golang.org/x/text", Version: "v0.14.0"},
			want: []string{"get", "golang.org/x/text@v0.14.0"},
		},
		{
			name: "go update with version",
			req:  packageActionReq{Action: actionUpdate, Manager: mustManager(t, "go"), Package: "golang.org/x/text", Version: "v0.15.0"},
			want: []string{"get", "golang.org/x/text@v0.15.0"},
		},
		{
			name: "go downgrade",
			req:  packageActionReq{Action: actionDowngrade, Manager: mustManager(t, "go"), Package: "golang.org/x/text", Version: "v0.13.0"},
			want: []string{"get", "golang.org/x/text@v0.13.0"},
		},
	}
}

func uvInstallUpdateArgCases(t *testing.T) []packageArgsCase {
	t.Helper()
	return []packageArgsCase{
		{
			name: "uv install",
			req:  packageActionReq{Action: actionInstall, Manager: mustManager(t, "uv"), Package: "requests"},
			want: []string{"pip", "install", "requests"},
		},
		{
			name: "uv update with version",
			req:  packageActionReq{Action: actionUpdate, Manager: mustManager(t, "uv"), Package: "requests", Version: "2.28.0"},
			want: []string{"pip", "install", "--upgrade", "requests==2.28.0"},
		},
	}
}

func poetryInstallUpdateArgCases(t *testing.T) []packageArgsCase {
	t.Helper()
	return []packageArgsCase{
		{
			name: "poetry install",
			req:  packageActionReq{Action: actionInstall, Manager: mustManager(t, "poetry"), Package: "django"},
			want: []string{"add", "django"},
		},
		{
			name: "poetry update with version",
			req:  packageActionReq{Action: actionUpdate, Manager: mustManager(t, "poetry"), Package: "django", Version: "4.2.0"},
			want: []string{"add", "django@4.2.0"},
		},
	}
}
