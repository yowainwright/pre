package proxy

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/yowainwright/pre/internal/cache"
	"github.com/yowainwright/pre/internal/manager"
	"github.com/yowainwright/pre/internal/security"
)

func noopExec(name string, args []string) {}

func npmMgr() *manager.Manager {
	return &manager.Manager{Name: "npm", Ecosystem: "npm", InstallCmds: []string{"install", "add", "i", "ci"}}
}

func pipMgr() *manager.Manager {
	return &manager.Manager{Name: "pip", Ecosystem: "PyPI", InstallCmds: []string{"install"}}
}

func goMgr() *manager.Manager {
	return &manager.Manager{Name: "go", Ecosystem: "Go", InstallCmds: []string{"install"}}
}

func brewMgr() *manager.Manager {
	return &manager.Manager{Name: "brew", Ecosystem: "Homebrew", InstallCmds: []string{"install"}}
}

func uvMgr() *manager.Manager {
	return &manager.Manager{Name: "uv", Ecosystem: "PyPI", InstallCmds: []string{"add", "sync"}}
}

func poetryMgr() *manager.Manager {
	return &manager.Manager{Name: "poetry", Ecosystem: "PyPI", InstallCmds: []string{"add", "update", "install"}}
}

func cargoMgr() *manager.Manager {
	commands := []string{"add", "install", "update", "fetch"}
	return &manager.Manager{Name: "cargo", Ecosystem: "crates.io", InstallCmds: commands}
}

func withExecFn(fn func(string, []string)) func() {
	orig := ExecFn
	ExecFn = fn
	return func() { ExecFn = orig }
}

func withSecurityCheck(fn func(string, string, string) ([]security.Vulnerability, error)) func() {
	orig := securityCheckFn
	securityCheckFn = fn
	return func() { securityCheckFn = orig }
}

func withResolveVersion(fn func(*manager.Manager, string) (string, error)) func() {
	orig := resolveVersionFn
	resolveVersionFn = fn
	return func() { resolveVersionFn = orig }
}

func withLoadCache(fn func() cache.Cache) func() {
	orig := loadCacheFn
	loadCacheFn = fn
	return func() { loadCacheFn = orig }
}

func withSaveCache(fn func(cache.Cache)) func() {
	orig := saveCacheFn
	saveCacheFn = fn
	return func() { saveCacheFn = orig }
}

func withUpdateCache(fn func(func(cache.Cache))) func() {
	orig := updateCacheFn
	updateCacheFn = fn
	return func() { updateCacheFn = orig }
}

func withReadManifest(fn func(*manager.Manager) []string) func() {
	orig := readManifestFn
	readManifestFn = fn
	return func() { readManifestFn = orig }
}

func withValidateManifest(fn func(*manager.Manager, string) error) func() {
	orig := validateManifestFn
	validateManifestFn = fn
	return func() { validateManifestFn = orig }
}

func withReadRequirementsFile(fn func(string) ([]string, error)) func() {
	orig := readRequirementsFileFn
	readRequirementsFileFn = fn
	return func() { readRequirementsFileFn = orig }
}

func withReadCargoFetchPackages(fn func(string) ([]string, error)) func() {
	orig := readCargoFetchPackagesFn
	readCargoFetchPackagesFn = fn
	return func() { readCargoFetchPackagesFn = orig }
}

func withReadCargoUpdatePackages(fn func(string, string) ([]string, error)) func() {
	orig := readCargoUpdatePackagesFn
	readCargoUpdatePackagesFn = fn
	return func() { readCargoUpdatePackagesFn = orig }
}

func withSpawnBackgroundScan(fn func(string)) func() {
	orig := spawnBackgroundScanFn
	spawnBackgroundScanFn = fn
	return func() { spawnBackgroundScanFn = orig }
}

func withSpawnSystemScan(fn func()) func() {
	orig := spawnSystemScanFn
	spawnSystemScanFn = fn
	return func() { spawnSystemScanFn = orig }
}

func withStdinInput(input string) func() {
	orig := stdinReader
	stdinReader = strings.NewReader(input)
	return func() { stdinReader = orig }
}

func emptyCache() cache.Cache      { return make(cache.Cache) }
func noopSave(cache.Cache)         {}
func noopUpdate(func(cache.Cache)) {}

func captureStdout(t *testing.T, fn func()) string {
	t.Helper()
	orig := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe stdout: %v", err)
	}
	os.Stdout = w
	defer func() { os.Stdout = orig }()

	fn()

	if err := w.Close(); err != nil {
		t.Fatalf("close stdout writer: %v", err)
	}
	out, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read stdout: %v", err)
	}
	return string(out)
}

func expectProcessExit(t *testing.T, want int, fn func()) {
	t.Helper()
	orig := processExit
	code := -1
	processExit = func(got int) { code = got; panic("exit") }
	defer func() {
		processExit = orig
		if recover() == nil {
			t.Error("expected processExit to be called")
		}
		if code != want {
			t.Errorf("expected exit code %d, got %d", want, code)
		}
	}()
	fn()
}

// Intercept flow tests

func TestInterceptDisabledBypassesScan(t *testing.T) {
	t.Setenv(envDisable, "1")

	execCalled := false
	loadCalled := false
	securityCalled := false
	defer withExecFn(func(name string, args []string) { execCalled = true })()
	defer withLoadCache(func() cache.Cache {
		loadCalled = true
		return emptyCache()
	})()
	defer withSecurityCheck(func(string, string, string) ([]security.Vulnerability, error) {
		securityCalled = true
		return nil, nil
	})()

	Intercept(npmMgr(), []string{"install", "react"})

	if !execCalled {
		t.Error("expected disabled intercept to run the package manager")
	}
	if loadCalled || securityCalled {
		t.Error("expected disabled intercept to bypass cache loading and security checks")
	}
}

func TestInterceptNonInstallSubcommand(t *testing.T) {
	called := false
	defer withExecFn(func(name string, args []string) { called = true })()

	Intercept(npmMgr(), []string{"run", "build"})
	if !called {
		t.Error("expected ExecFn to be called")
	}
}

func TestInterceptEmptyArgs(t *testing.T) {
	called := false
	defer withExecFn(func(name string, args []string) { called = true })()

	Intercept(npmMgr(), []string{})
	if !called {
		t.Error("expected ExecFn to be called for empty args")
	}
}

func TestInterceptNPMCI(t *testing.T) {
	securityCalled := false
	defer withExecFn(noopExec)()
	defer withLoadCache(emptyCache)()
	defer withUpdateCache(noopUpdate)()
	defer withSpawnBackgroundScan(func(string) {})()
	defer withReadManifest(func(*manager.Manager) []string { return []string{"react@18.0.0"} })()
	defer withSecurityCheck(func(string, string, string) ([]security.Vulnerability, error) {
		securityCalled = true
		return nil, nil
	})()

	Intercept(npmMgr(), []string{"ci"})

	if !securityCalled {
		t.Error("expected npm ci to scan the lockfile")
	}
}

func TestInterceptInvalidManifestBlocks(t *testing.T) {
	execCalled := false
	defer withExecFn(func(string, []string) { execCalled = true })()
	defer withValidateManifest(func(*manager.Manager, string) error {
		return errors.New("invalid package-lock.json")
	})()

	expectProcessExit(t, 1, func() {
		Intercept(npmMgr(), []string{"ci"})
	})
	if execCalled {
		t.Error("expected invalid manifest to block npm ci")
	}
}

func TestInterceptUVPipInstall(t *testing.T) {
	securityCalled := false
	defer withExecFn(noopExec)()
	defer withLoadCache(emptyCache)()
	defer withUpdateCache(noopUpdate)()
	defer withSpawnBackgroundScan(func(string) {})()
	defer withSecurityCheck(func(ecosystem, name, version string) ([]security.Vulnerability, error) {
		securityCalled = ecosystem == "PyPI" && name == "requests" && version == "2.32.0"
		return nil, nil
	})()

	Intercept(uvMgr(), []string{"pip", "install", "requests==2.32.0"})

	if !securityCalled {
		t.Error("expected uv pip install to scan the requested package")
	}
}

func TestInterceptUVPipListPassesThrough(t *testing.T) {
	execCalled := false
	securityCalled := false
	defer withExecFn(func(string, []string) { execCalled = true })()
	defer withSecurityCheck(func(string, string, string) ([]security.Vulnerability, error) {
		securityCalled = true
		return nil, nil
	})()

	Intercept(uvMgr(), []string{"pip", "list"})

	if !execCalled || securityCalled {
		t.Error("expected uv pip list to pass through without scanning")
	}
}

func TestInterceptCargoAddResolvesRequirement(t *testing.T) {
	resolvedTarget := ""
	scanned := false
	executed := false
	defer withExecFn(func(name string, args []string) {
		executed = name == "cargo" && slices.Equal(args, []string{"add", "serde@1.0.0"})
	})()
	defer withLoadCache(emptyCache)()
	defer withUpdateCache(noopUpdate)()
	defer withSpawnBackgroundScan(func(string) {})()
	defer withResolveVersion(func(_ *manager.Manager, pkg string) (string, error) {
		resolvedTarget = pkg
		return "1.0.217", nil
	})()
	defer withSecurityCheck(func(ecosystem, name, version string) ([]security.Vulnerability, error) {
		scanned = ecosystem == "crates.io" && name == "serde" && version == "1.0.217"
		return nil, nil
	})()

	Intercept(cargoMgr(), []string{"add", "serde@1.0.0"})

	if resolvedTarget != "serde@^1.0.0" || !scanned || !executed {
		t.Errorf("unexpected Cargo interception: target=%q scanned=%v executed=%v", resolvedTarget, scanned, executed)
	}
}

func TestInterceptCargoUpdateUsesSelectedManifestRequirement(t *testing.T) {
	var gotPath, gotTarget string
	defer withExecFn(noopExec)()
	defer withLoadCache(emptyCache)()
	defer withUpdateCache(noopUpdate)()
	defer withSpawnBackgroundScan(func(string) {})()
	defer withReadCargoUpdatePackages(func(path, target string) ([]string, error) {
		gotPath = path
		gotTarget = target
		return []string{"serde@^1.0"}, nil
	})()
	defer withResolveVersion(func(_ *manager.Manager, pkg string) (string, error) {
		return "1.0.217", nil
	})()
	defer withSecurityCheck(func(string, string, string) ([]security.Vulnerability, error) {
		return nil, nil
	})()

	args := []string{"update", "--manifest-path", "nested/Cargo.toml", "serde"}
	Intercept(cargoMgr(), args)

	if gotPath != "nested/Cargo.toml" || gotTarget != "serde" {
		t.Fatalf("unexpected Cargo project request: path=%q target=%q", gotPath, gotTarget)
	}
}

func TestInterceptCargoUpdateReadsEveryTargetRequirement(t *testing.T) {
	var targets []string
	defer withExecFn(noopExec)()
	defer withLoadCache(emptyCache)()
	defer withUpdateCache(noopUpdate)()
	defer withSpawnBackgroundScan(func(string) {})()
	defer withReadCargoUpdatePackages(func(_ string, target string) ([]string, error) {
		targets = append(targets, target)
		return []string{target + "@^1"}, nil
	})()
	defer withResolveVersion(func(_ *manager.Manager, _ string) (string, error) {
		return "1.0.0", nil
	})()
	defer withSecurityCheck(func(string, string, string) ([]security.Vulnerability, error) {
		return nil, nil
	})()

	args := []string{"update", "--manifest-path", "Cargo.toml", "serde", "regex"}
	Intercept(cargoMgr(), args)

	if !slices.Equal(targets, []string{"serde", "regex"}) {
		t.Fatalf("unexpected update targets: %v", targets)
	}
}

func TestInterceptCargoFetchReadFailureBlocks(t *testing.T) {
	execCalled := false
	defer withExecFn(func(string, []string) { execCalled = true })()
	defer withReadCargoFetchPackages(func(string) ([]string, error) {
		return nil, errors.New("invalid Cargo.lock")
	})()

	expectProcessExit(t, 1, func() {
		Intercept(cargoMgr(), []string{"fetch", "--manifest-path", "Cargo.toml"})
	})
	if execCalled {
		t.Error("expected unreadable Cargo project to block fetch")
	}
}

func TestInterceptCargoExternalSourceBlocks(t *testing.T) {
	execCalled := false
	defer withExecFn(func(string, []string) { execCalled = true })()

	expectProcessExit(t, 1, func() {
		Intercept(cargoMgr(), []string{"add", "--git", "https://example.com/repo", "private"})
	})
	if execCalled {
		t.Error("expected external Cargo source to block install")
	}
}

func TestCargoRepeatedRegistryFlagsBlockCustomSource(t *testing.T) {
	t.Setenv("CARGO_HOME", t.TempDir())
	args := []string{"install", "--registry", "crates-io", "--registry=internal", "tool"}

	err := cargoInstallError(cargoMgr(), args)
	if err == nil || !strings.Contains(err.Error(), "custom-registry") {
		t.Fatalf("expected custom registry error, got %v", err)
	}
}

func TestInterceptCargoDefaultRegistryEnvironmentBlocks(t *testing.T) {
	t.Setenv("CARGO_REGISTRY_DEFAULT", "internal")
	t.Setenv("CARGO_HOME", t.TempDir())
	args := []string{"add", "serde"}

	err := cargoInstallError(cargoMgr(), args)
	if err == nil || !strings.Contains(err.Error(), "default registry") {
		t.Fatalf("expected default registry error, got %v", err)
	}
}

func TestCargoConfigDefaultRegistryBlocks(t *testing.T) {
	t.Setenv("CARGO_HOME", t.TempDir())
	projectDir := t.TempDir()
	writeCargoConfig(t, projectDir, "[registry]\ndefault = \"internal\"\n")
	args := []string{"-C", projectDir, "add", "serde"}

	err := cargoInstallError(cargoMgr(), args)
	if err == nil || !strings.Contains(err.Error(), "default registry") {
		t.Fatalf("expected default registry error, got %v", err)
	}
}

func TestCargoInlineDefaultRegistryBlocks(t *testing.T) {
	t.Setenv("CARGO_HOME", t.TempDir())
	projectDir := t.TempDir()
	writeCargoConfig(t, projectDir, "registry = { default = \"internal\" }\n")
	args := []string{"-C", projectDir, "add", "serde"}

	err := cargoInstallError(cargoMgr(), args)
	if err == nil || !strings.Contains(err.Error(), "default registry") {
		t.Fatalf("expected inline default registry error, got %v", err)
	}
}

func TestCargoRegistryEnvironmentOverridesConfig(t *testing.T) {
	t.Setenv("CARGO_HOME", t.TempDir())
	t.Setenv("CARGO_REGISTRY_DEFAULT", "crates-io")
	projectDir := t.TempDir()
	writeCargoConfig(t, projectDir, "[registry]\ndefault = \"internal\"\n")
	args := []string{"-C", projectDir, "add", "serde"}

	if err := cargoInstallError(cargoMgr(), args); err != nil {
		t.Fatalf("expected crates.io override to pass, got %v", err)
	}
}

func TestCargoExplicitRegistryOverridesDefault(t *testing.T) {
	t.Setenv("CARGO_HOME", t.TempDir())
	t.Setenv("CARGO_REGISTRY_DEFAULT", "internal")
	args := []string{"add", "--registry", "crates-io", "serde"}

	if err := cargoInstallError(cargoMgr(), args); err != nil {
		t.Fatalf("expected explicit crates.io registry to pass, got %v", err)
	}
}

func TestCargoConfigSourceOverrideBlocks(t *testing.T) {
	t.Setenv("CARGO_HOME", t.TempDir())
	projectDir := t.TempDir()
	config := "[patch.crates-io]\nserde = { path = \"../serde\" }\n"
	writeCargoConfig(t, projectDir, config)
	args := []string{"-C", projectDir, "fetch"}

	err := cargoInstallError(cargoMgr(), args)
	if err == nil || !strings.Contains(err.Error(), "resolution override") {
		t.Fatalf("expected resolution override error, got %v", err)
	}
}

func TestCargoInlinePatchConfigBlocks(t *testing.T) {
	t.Setenv("CARGO_HOME", t.TempDir())
	projectDir := t.TempDir()
	config := "patch = { crates-io = { serde = { path = \"../serde\" } } }\n"
	writeCargoConfig(t, projectDir, config)
	args := []string{"-C", projectDir, "fetch"}

	err := cargoInstallError(cargoMgr(), args)
	if err == nil || !strings.Contains(err.Error(), "resolution override") {
		t.Fatalf("expected inline patch error, got %v", err)
	}
}

func TestCargoResolverLockfileConfigBlocks(t *testing.T) {
	t.Setenv("CARGO_HOME", t.TempDir())
	projectDir := t.TempDir()
	config := "[resolver]\nlockfile-path = \"other.lock\"\n"
	writeCargoConfig(t, projectDir, config)
	args := []string{"-C", projectDir, "fetch"}

	err := cargoInstallError(cargoMgr(), args)
	if err == nil || !strings.Contains(err.Error(), "resolution override") {
		t.Fatalf("expected lockfile override error, got %v", err)
	}
}

func TestCargoCommandConfigOverrideBlocks(t *testing.T) {
	t.Setenv("CARGO_HOME", t.TempDir())
	args := []string{"--config", "registry.default='internal'", "add", "serde"}

	err := cargoInstallError(cargoMgr(), args)
	if err == nil || !strings.Contains(err.Error(), "--config") {
		t.Fatalf("expected command config error, got %v", err)
	}
}

func TestCargoCommandLockfileOverrideBlocks(t *testing.T) {
	t.Setenv("CARGO_HOME", t.TempDir())
	args := []string{"update", "--lockfile-path", "other.lock"}

	err := cargoInstallError(cargoMgr(), args)
	if err == nil || !strings.Contains(err.Error(), "--lockfile-path") {
		t.Fatalf("expected lockfile path error, got %v", err)
	}
}

func TestCargoInstallUsesCargoHomeDefaultRegistry(t *testing.T) {
	cargoHome := t.TempDir()
	t.Setenv("CARGO_HOME", cargoHome)
	configPath := filepath.Join(cargoHome, "config.toml")
	config := "[registry]\ndefault = \"internal\"\n"
	if err := os.WriteFile(configPath, []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}

	err := cargoInstallError(cargoMgr(), []string{"install", "ripgrep"})
	if err == nil || !strings.Contains(err.Error(), "default registry") {
		t.Fatalf("expected Cargo home registry error, got %v", err)
	}
}

func TestCargoInstallIgnoresProjectRegistryConfig(t *testing.T) {
	t.Setenv("CARGO_HOME", t.TempDir())
	projectDir := t.TempDir()
	writeCargoConfig(t, projectDir, "[registry]\ndefault = \"internal\"\n")
	args := []string{"-C", projectDir, "install", "ripgrep"}

	if err := cargoInstallError(cargoMgr(), args); err != nil {
		t.Fatalf("expected project config to be ignored, got %v", err)
	}
}

func TestCargoCratesIOIndexEnvironmentBlocks(t *testing.T) {
	t.Setenv("CARGO_HOME", t.TempDir())
	t.Setenv("CARGO_REGISTRIES_CRATES_IO_INDEX", "https://registry.example/index")

	err := cargoInstallError(cargoMgr(), []string{"add", "serde"})
	if err == nil || !strings.Contains(err.Error(), "index override") {
		t.Fatalf("expected index override error, got %v", err)
	}
}

func TestCargoCratesIOIndexConfigBlocks(t *testing.T) {
	t.Setenv("CARGO_HOME", t.TempDir())
	projectDir := t.TempDir()
	config := "[registries.crates-io]\nindex = \"https://registry.example/index\"\n"
	writeCargoConfig(t, projectDir, config)

	err := cargoInstallError(cargoMgr(), []string{"-C", projectDir, "add", "serde"})
	if err == nil || !strings.Contains(err.Error(), "resolution override") {
		t.Fatalf("expected registry index error, got %v", err)
	}
}

func TestCargoCratesIOSourceRegistryConfigBlocks(t *testing.T) {
	t.Setenv("CARGO_HOME", t.TempDir())
	projectDir := t.TempDir()
	config := "[source.crates-io]\nregistry = \"https://registry.example/index\"\n"
	writeCargoConfig(t, projectDir, config)

	err := cargoInstallError(cargoMgr(), []string{"-C", projectDir, "fetch"})
	if err == nil || !strings.Contains(err.Error(), "resolution override") {
		t.Fatalf("expected source registry error, got %v", err)
	}
}

func TestCargoOfflineFlagBlocks(t *testing.T) {
	t.Setenv("CARGO_HOME", t.TempDir())

	err := cargoInstallError(cargoMgr(), []string{"add", "--offline", "serde"})
	if err == nil || !strings.Contains(err.Error(), "offline") {
		t.Fatalf("expected offline error, got %v", err)
	}
}

func TestCargoOfflineConfigBlocks(t *testing.T) {
	t.Setenv("CARGO_HOME", t.TempDir())
	projectDir := t.TempDir()
	writeCargoConfig(t, projectDir, "[net]\noffline = true\n")

	err := cargoInstallError(cargoMgr(), []string{"-C", projectDir, "add", "serde"})
	if err == nil || !strings.Contains(err.Error(), "offline") {
		t.Fatalf("expected offline config error, got %v", err)
	}
}

func TestCargoOfflineEnvironmentOverridesConfig(t *testing.T) {
	t.Setenv("CARGO_HOME", t.TempDir())
	t.Setenv("CARGO_NET_OFFLINE", "false")
	projectDir := t.TempDir()
	writeCargoConfig(t, projectDir, "[net]\noffline = true\n")

	if err := cargoInstallError(cargoMgr(), []string{"-C", projectDir, "add", "serde"}); err != nil {
		t.Fatalf("expected online environment override to pass, got %v", err)
	}
}

func TestCargoOfflineEnvironmentBlocks(t *testing.T) {
	t.Setenv("CARGO_HOME", t.TempDir())
	t.Setenv("CARGO_NET_OFFLINE", "true")

	err := cargoInstallError(cargoMgr(), []string{"install", "ripgrep"})
	if err == nil || !strings.Contains(err.Error(), "offline") {
		t.Fatalf("expected offline environment error, got %v", err)
	}
}

func TestCargoResolutionChangingUnstableOptionBlocks(t *testing.T) {
	t.Setenv("CARGO_HOME", t.TempDir())

	err := cargoInstallError(cargoMgr(), []string{"-Zminimal-versions", "update"})
	if err == nil || !strings.Contains(err.Error(), "unstable option") {
		t.Fatalf("expected unstable option error, got %v", err)
	}
}

func TestCargoUnstableOptionsGatePasses(t *testing.T) {
	t.Setenv("CARGO_HOME", t.TempDir())
	projectDir := t.TempDir()
	args := []string{"-Z", "unstable-options", "-C", projectDir, "add", "serde"}

	if err := cargoInstallError(cargoMgr(), args); err != nil {
		t.Fatalf("expected unstable-options gate to pass, got %v", err)
	}
}

func writeCargoConfig(t *testing.T, dir, content string) {
	t.Helper()
	configDir := filepath.Join(dir, ".cargo")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(configDir, "config.toml")
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestInterceptCargoPreciseUpdateBlocksManifestExternalSource(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "Cargo.toml")
	manifest := "[dependencies]\nprivate = { registry = \"internal\", version = \"1\" }\n"
	if err := os.WriteFile(manifestPath, []byte(manifest), 0o644); err != nil {
		t.Fatalf("write Cargo.toml: %v", err)
	}
	execCalled := false
	defer withExecFn(func(string, []string) { execCalled = true })()
	defer withLoadCache(emptyCache)()
	defer withUpdateCache(noopUpdate)()
	defer withSpawnBackgroundScan(func(string) {})()
	defer withSecurityCheck(func(string, string, string) ([]security.Vulnerability, error) {
		return nil, nil
	})()

	args := []string{"update", "--manifest-path", manifestPath, "private", "--precise", "1.0.0"}
	expectProcessExit(t, 1, func() {
		Intercept(cargoMgr(), args)
	})
	if execCalled {
		t.Error("expected manifest source to block precise update")
	}
}

func TestCargoManifestPathRejectsMissingValue(t *testing.T) {
	_, _, err := cargoManifestPath([]string{"fetch", "--manifest-path"})
	if err == nil {
		t.Fatal("expected missing manifest path error")
	}
}

func TestCargoWorkingDirectoryRejectsEmptyAttachedValue(t *testing.T) {
	_, _, err := cargoManifestPath([]string{"-C=", "update"})
	if err == nil {
		t.Fatal("expected empty -C value error")
	}
}

func TestCargoManifestPathAppliesCargoWorkingDirectory(t *testing.T) {
	args := []string{"-C", "workspace", "update", "--manifest-path", "member/Cargo.toml"}
	path, explicit, err := cargoManifestPath(args)
	want := filepath.Join("workspace", "member", "Cargo.toml")
	if err != nil || !explicit || path != want {
		t.Fatalf("cargoManifestPath() = %q, %v, %v; want %q, true, nil", path, explicit, err, want)
	}
}

func TestDiscoverCargoManifestSearchesParents(t *testing.T) {
	projectDir := t.TempDir()
	nestedDir := filepath.Join(projectDir, "src", "nested")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatalf("create nested directory: %v", err)
	}
	manifestPath := filepath.Join(projectDir, "Cargo.toml")
	if err := os.WriteFile(manifestPath, []byte("[package]\nname = \"demo\"\n"), 0o644); err != nil {
		t.Fatalf("write Cargo.toml: %v", err)
	}

	startPath := filepath.Join(nestedDir, "Cargo.toml")
	got, err := discoverCargoManifest(startPath)
	if err != nil || got != manifestPath {
		t.Fatalf("discoverCargoManifest() = %q, %v; want %q, nil", got, err, manifestPath)
	}
}

func TestInterceptPoetryInstallScansManifestInsteadOfFlagValues(t *testing.T) {
	var scannedName string
	defer withExecFn(noopExec)()
	defer withLoadCache(emptyCache)()
	defer withUpdateCache(noopUpdate)()
	defer withSpawnBackgroundScan(func(string) {})()
	defer withReadManifest(func(*manager.Manager) []string { return []string{"requests==2.32.0"} })()
	defer withSecurityCheck(func(_, name, _ string) ([]security.Vulnerability, error) {
		scannedName = name
		return nil, nil
	})()

	Intercept(poetryMgr(), []string{"install", "--with", "dev"})

	if scannedName != "requests" {
		t.Errorf("expected manifest package, got %q", scannedName)
	}
}

func TestInterceptRequirementFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "beta.txt")
	if err := os.WriteFile(path, []byte("requests==2.32.0\n"), 0644); err != nil {
		t.Fatalf("write requirements file: %v", err)
	}

	execCalled := false
	securityCalled := false
	defer withExecFn(func(string, []string) { execCalled = true })()
	defer withLoadCache(emptyCache)()
	defer withUpdateCache(noopUpdate)()
	defer withSpawnBackgroundScan(func(string) {})()
	defer withSecurityCheck(func(ecosystem, name, version string) ([]security.Vulnerability, error) {
		securityCalled = ecosystem == "PyPI" && name == "requests" && version == "2.32.0"
		return nil, nil
	})()

	Intercept(pipMgr(), []string{"install", "-r", path})

	if !execCalled || !securityCalled {
		t.Error("expected requirements file packages to be scanned before install")
	}
}

func TestInterceptMissingRequirementFileBlocks(t *testing.T) {
	execCalled := false
	defer withExecFn(func(string, []string) { execCalled = true })()
	defer withReadRequirementsFile(func(string) ([]string, error) {
		return nil, errors.New("not found")
	})()

	expectProcessExit(t, 1, func() {
		Intercept(pipMgr(), []string{"install", "--requirement", "missing.txt"})
	})
	if execCalled {
		t.Error("expected unreadable requirements file to block the install")
	}
}

func TestInterceptInstallManifestFallback(t *testing.T) {
	execCalled := false
	defer withExecFn(func(name string, args []string) { execCalled = true })()
	defer withSecurityCheck(func(eco, name, ver string) ([]security.Vulnerability, error) {
		return nil, nil
	})()
	defer withResolveVersion(func(mgr *manager.Manager, pkg string) (string, error) {
		return "1.0.0", nil
	})()
	defer withLoadCache(emptyCache)()
	defer withUpdateCache(noopUpdate)()
	defer withSpawnBackgroundScan(func(string) {})()
	defer withReadManifest(func(mgr *manager.Manager) []string {
		return []string{"lodash", "react"}
	})()
	Intercept(npmMgr(), []string{"install"})
	if !execCalled {
		t.Error("expected ExecFn called after scanning manifest packages")
	}
}

func TestInterceptInstallManifestEmpty(t *testing.T) {
	execCalled := false
	defer withExecFn(func(name string, args []string) { execCalled = true })()
	defer withReadManifest(func(mgr *manager.Manager) []string { return nil })()

	Intercept(npmMgr(), []string{"install"})
	if !execCalled {
		t.Error("expected ExecFn called when manifest is empty")
	}
}

func TestInterceptInstallAllFlags(t *testing.T) {
	called := false
	defer withExecFn(func(name string, args []string) { called = true })()

	Intercept(npmMgr(), []string{"install", "--save-dev", "--legacy-peer-deps"})
	if !called {
		t.Error("expected ExecFn to be called when no packages to check")
	}
}

func TestInterceptInstallCleanPackage(t *testing.T) {
	execCalled := false
	defer withExecFn(func(name string, args []string) { execCalled = true })()
	defer withSecurityCheck(func(eco, name, ver string) ([]security.Vulnerability, error) {
		return nil, nil
	})()
	defer withResolveVersion(func(mgr *manager.Manager, pkg string) (string, error) {
		return "18.0.0", nil
	})()
	defer withLoadCache(emptyCache)()
	defer withUpdateCache(noopUpdate)()
	defer withSpawnBackgroundScan(func(string) {})()
	Intercept(npmMgr(), []string{"install", "react@18.0.0"})
	if !execCalled {
		t.Error("expected ExecFn to be called for clean package")
	}
}

func TestInterceptInstallVersionResolutionFailure(t *testing.T) {
	execCalled := false
	defer withExecFn(func(name string, args []string) { execCalled = true })()
	defer withSecurityCheck(func(eco, name, ver string) ([]security.Vulnerability, error) {
		return nil, nil
	})()
	defer withResolveVersion(func(mgr *manager.Manager, pkg string) (string, error) {
		return "", errors.New("resolution failed")
	})()
	defer withLoadCache(emptyCache)()
	defer withUpdateCache(noopUpdate)()
	defer withSpawnBackgroundScan(func(string) {})()
	expectProcessExit(t, 1, func() {
		Intercept(npmMgr(), []string{"install", "react"})
	})
	if execCalled {
		t.Error("expected version resolution failure to block the install")
	}
}

func TestInterceptInstallSecurityCheckFailure(t *testing.T) {
	execCalled := false
	defer withExecFn(func(name string, args []string) { execCalled = true })()
	defer withSecurityCheck(func(eco, name, ver string) ([]security.Vulnerability, error) {
		return nil, errors.New("network error")
	})()
	defer withResolveVersion(func(mgr *manager.Manager, pkg string) (string, error) {
		return "1.0.0", nil
	})()
	defer withLoadCache(emptyCache)()
	defer withUpdateCache(noopUpdate)()
	defer withSpawnBackgroundScan(func(string) {})()
	expectProcessExit(t, 1, func() {
		Intercept(npmMgr(), []string{"install", "lodash"})
	})
	if execCalled {
		t.Error("expected security check failure to block the install")
	}
}

func TestInterceptInstallVulnsUserYes(t *testing.T) {
	for _, answer := range []string{"y", "yes"} {
		t.Run(answer, func(t *testing.T) {
			execCalled := false
			defer withExecFn(func(name string, args []string) { execCalled = true })()
			defer withSecurityCheck(func(eco, name, ver string) ([]security.Vulnerability, error) {
				return []security.Vulnerability{{ID: "CVE-2021-1234", Summary: "test vuln", Severity: "CRITICAL"}}, nil
			})()
			defer withResolveVersion(func(mgr *manager.Manager, pkg string) (string, error) {
				return "1.0.0", nil
			})()
			defer withLoadCache(emptyCache)()
			defer withUpdateCache(noopUpdate)()
			defer withSpawnBackgroundScan(func(string) {})()
			defer withStdinInput(answer + "\n")()

			Intercept(npmMgr(), []string{"install", "lodash"})
			if !execCalled {
				t.Errorf("expected ExecFn called when user answers %q", answer)
			}

		})
	}
}

func TestInterceptInstallVulnsUserNo(t *testing.T) {
	defer withExecFn(noopExec)()
	defer withSecurityCheck(func(eco, name, ver string) ([]security.Vulnerability, error) {
		return []security.Vulnerability{{ID: "CVE-2021-1234", Summary: "test vuln", Severity: "CRITICAL"}}, nil
	})()
	defer withResolveVersion(func(mgr *manager.Manager, pkg string) (string, error) {
		return "1.0.0", nil
	})()
	defer withLoadCache(emptyCache)()
	defer withUpdateCache(noopUpdate)()

	origStdin := stdinReader
	stdinReader = strings.NewReader("N\n")
	defer func() { stdinReader = origStdin }()

	exited := false
	origExit := processExit
	processExit = func(code int) { exited = true; panic("exit") }
	defer func() {
		recover()
		processExit = origExit
		if !exited {
			t.Error("expected processExit to be called")
		}
	}()

	Intercept(npmMgr(), []string{"install", "lodash"})
}

func TestInterceptInstallCacheHit(t *testing.T) {
	execCalled := false
	securityCalled := false
	defer withExecFn(func(name string, args []string) { execCalled = true })()
	defer withSecurityCheck(func(eco, name, ver string) ([]security.Vulnerability, error) {
		securityCalled = true
		return nil, nil
	})()
	defer withUpdateCache(noopUpdate)()
	defer withSpawnBackgroundScan(func(string) {})()

	c := make(cache.Cache)
	cache.Set(c, cache.Key("npm", "react", "18.0.0"))
	defer withLoadCache(func() cache.Cache { return c })()
	Intercept(npmMgr(), []string{"install", "react@18.0.0"})
	if !execCalled {
		t.Error("expected ExecFn to be called on cache hit")
	}
	if securityCalled {
		t.Error("expected security check to be skipped on cache hit")
	}
}

func TestInterceptSilentWhenAllCached(t *testing.T) {
	execCalled := false
	defer withExecFn(func(name string, args []string) { execCalled = true })()
	defer withUpdateCache(noopUpdate)()
	defer withSpawnBackgroundScan(func(string) {})()

	c := make(cache.Cache)
	cache.Set(c, cache.Key("npm", "react", "18.0.0"))
	defer withLoadCache(func() cache.Cache { return c })()

	Intercept(npmMgr(), []string{"install", "react@18.0.0"})
	if !execCalled {
		t.Error("expected ExecFn to be called on all-cached install")
	}
}

func TestOutputLevelSilent(t *testing.T) {
	results := []scanResult{
		{cached: true},
		{cached: true},
	}
	if outputLevel(results) != outputSilent {
		t.Error("expected outputSilent when all cached")
	}
}

func TestOutputLevelQuiet(t *testing.T) {
	results := []scanResult{
		{cached: true},
		{updated: true},
	}
	if outputLevel(results) != outputQuiet {
		t.Error("expected outputQuiet when clean but not all cached")
	}
}

func TestOutputLevelFull(t *testing.T) {
	results := []scanResult{
		{vulns: []security.Vulnerability{{ID: "CVE-1234"}}},
	}
	if outputLevel(results) != outputFull {
		t.Error("expected outputFull when vulns present")
	}
}

func TestOutputLevelFullOnError(t *testing.T) {
	results := []scanResult{{err: errors.New("timeout")}}
	if outputLevel(results) != outputFull {
		t.Error("expected outputFull when error present")
	}
}

func TestCountUncached(t *testing.T) {
	mgr := npmMgr()
	c := make(cache.Cache)
	cache.Set(c, cache.Key("npm", "react", "18.0.0"))

	n := countUncached(mgr, []string{"react@18.0.0", "lodash@4.17.21"}, c)
	if n != 1 {
		t.Errorf("expected 1 uncached, got %d", n)
	}
}

func TestCountUncachedTreatsFloatingVersionsAsUncached(t *testing.T) {
	mgr := npmMgr()
	c := make(cache.Cache)
	cache.Set(c, cache.Key("npm", "react", "18.0.0"))
	cache.Set(c, cache.Key("npm", "react", "latest"))

	n := countUncached(mgr, []string{"react@latest"}, c)
	if n != 1 {
		t.Errorf("expected 1 uncached for floating version, got %d", n)
	}
}

func TestCountUncachedTreatsConstraintsAsUncached(t *testing.T) {
	mgr := npmMgr()
	c := make(cache.Cache)
	cache.Set(c, cache.Key("npm", "react", "^18.0.0"))

	n := countUncached(mgr, []string{"react@^18.0.0"}, c)
	if n != 1 {
		t.Errorf("expected 1 uncached for semver constraint, got %d", n)
	}
}

func TestInterceptQuietWhenClean(t *testing.T) {
	execCalled := false
	defer withExecFn(func(name string, args []string) { execCalled = true })()
	defer withSecurityCheck(func(eco, name, ver string) ([]security.Vulnerability, error) {
		return nil, nil
	})()
	defer withResolveVersion(func(mgr *manager.Manager, pkg string) (string, error) {
		return "18.0.0", nil
	})()
	defer withLoadCache(emptyCache)()
	defer withUpdateCache(noopUpdate)()
	defer withSpawnBackgroundScan(func(string) {})()

	Intercept(npmMgr(), []string{"install", "react"})
	if !execCalled {
		t.Error("expected ExecFn called after quiet clean scan")
	}
}

func TestInterceptQuietSuppressesCleanScanOutput(t *testing.T) {
	t.Setenv(envQuiet, "1")

	defer withExecFn(noopExec)()
	defer withSecurityCheck(func(eco, name, ver string) ([]security.Vulnerability, error) {
		return nil, nil
	})()
	defer withResolveVersion(func(mgr *manager.Manager, pkg string) (string, error) {
		return "18.0.0", nil
	})()
	defer withLoadCache(emptyCache)()
	defer withUpdateCache(noopUpdate)()
	defer withSpawnBackgroundScan(func(string) {})()

	out := captureStdout(t, func() {
		Intercept(npmMgr(), []string{"install", "react"})
	})

	if strings.Contains(out, "scanning") || strings.Contains(out, "all clean") {
		t.Errorf("expected quiet clean scan output to be suppressed, got %q", out)
	}
}

func TestInterceptQuietStillShowsVulnerabilities(t *testing.T) {
	t.Setenv(envQuiet, "1")

	defer withExecFn(noopExec)()
	defer withSecurityCheck(func(eco, name, ver string) ([]security.Vulnerability, error) {
		return []security.Vulnerability{{ID: "CVE-2026-0001", Summary: "test vuln", Severity: "LOW"}}, nil
	})()
	defer withResolveVersion(func(mgr *manager.Manager, pkg string) (string, error) {
		return "18.0.0", nil
	})()
	defer withLoadCache(emptyCache)()
	defer withUpdateCache(noopUpdate)()
	defer withSpawnBackgroundScan(func(string) {})()

	out := captureStdout(t, func() {
		Intercept(npmMgr(), []string{"install", "react"})
	})

	if !strings.Contains(out, "CVE-2026-0001") {
		t.Errorf("expected quiet mode to keep vulnerability output, got %q", out)
	}
}

func TestInterceptPackageLimitBypassesScan(t *testing.T) {
	t.Setenv(envMaxPackages, "1")

	execCalled := false
	loadCalled := false
	securityCalled := false
	defer withExecFn(func(name string, args []string) { execCalled = true })()
	defer withLoadCache(func() cache.Cache {
		loadCalled = true
		return emptyCache()
	})()
	defer withSecurityCheck(func(string, string, string) ([]security.Vulnerability, error) {
		securityCalled = true
		return nil, nil
	})()

	Intercept(npmMgr(), []string{"install", "react", "lodash"})

	if !execCalled {
		t.Error("expected package-limit bypass to run the package manager")
	}
	if loadCalled || securityCalled {
		t.Error("expected package-limit bypass to skip cache loading and security checks")
	}
}

func TestInterceptManifestFallbackResolvesMissingVersion(t *testing.T) {
	resolveCalled := false
	securityCalled := false

	defer withExecFn(noopExec)()
	defer withLoadCache(emptyCache)()
	defer withUpdateCache(noopUpdate)()
	defer withSpawnBackgroundScan(func(string) {})()
	defer withSecurityCheck(func(eco, name, ver string) ([]security.Vulnerability, error) {
		securityCalled = true
		return nil, nil
	})()
	defer withResolveVersion(func(*manager.Manager, string) (string, error) {
		resolveCalled = true
		return "18.0.0", nil
	})()
	defer withReadManifest(func(*manager.Manager) []string { return []string{"react"} })()

	Intercept(npmMgr(), []string{"install"})

	if !resolveCalled {
		t.Error("expected manifest fallback to resolve the install version")
	}
	if !securityCalled {
		t.Error("expected resolved manifest package to be scanned")
	}
}

// scanPackage tests

func TestScanPackageVersionInSpec(t *testing.T) {
	defer withSecurityCheck(func(eco, name, ver string) ([]security.Vulnerability, error) {
		if ver != "18.0.0" {
			t.Errorf("expected version 18.0.0, got %s", ver)
		}
		return nil, nil
	})()

	r := scanPackage(npmMgr(), "react@18.0.0", make(cache.Cache))
	if r.err != nil || len(r.vulns) != 0 {
		t.Errorf("expected clean result, got err=%v vulns=%d", r.err, len(r.vulns))
	}
}

func TestScanPackageResolvesVersion(t *testing.T) {
	defer withSecurityCheck(func(eco, name, ver string) ([]security.Vulnerability, error) {
		return nil, nil
	})()
	defer withResolveVersion(func(mgr *manager.Manager, pkg string) (string, error) {
		return "17.0.0", nil
	})()

	r := scanPackage(npmMgr(), "react", make(cache.Cache))
	if r.version != "17.0.0" {
		t.Errorf("expected resolved version 17.0.0, got %q", r.version)
	}
}

func TestScanPackageResolvesLatestVersionTag(t *testing.T) {
	defer withSecurityCheck(func(eco, name, ver string) ([]security.Vulnerability, error) {
		if name != "react" || ver != "18.3.1" {
			t.Errorf("expected resolved react@18.3.1, got %s@%s", name, ver)
		}
		return nil, nil
	})()
	defer withResolveVersion(func(mgr *manager.Manager, pkg string) (string, error) {
		if pkg != "react" {
			t.Errorf("expected package react, got %q", pkg)
		}
		return "18.3.1", nil
	})()

	r := scanPackage(npmMgr(), "react@latest", make(cache.Cache))
	if !r.updated {
		t.Error("expected latest tag to trigger version resolution")
	}
	if r.version != "18.3.1" {
		t.Errorf("expected resolved version 18.3.1, got %q", r.version)
	}
}

func TestScanPackageResolvesNPMDistTag(t *testing.T) {
	defer withSecurityCheck(func(eco, name, ver string) ([]security.Vulnerability, error) {
		if name != "react" || ver != "19.0.0-rc.1" {
			t.Errorf("expected resolved react@19.0.0-rc.1, got %s@%s", name, ver)
		}
		return nil, nil
	})()
	defer withResolveVersion(func(mgr *manager.Manager, pkg string) (string, error) {
		if pkg != "react@next" {
			t.Errorf("expected package spec react@next, got %q", pkg)
		}
		return "19.0.0-rc.1", nil
	})()

	r := scanPackage(npmMgr(), "react@next", make(cache.Cache))
	if !r.updated {
		t.Error("expected dist-tag to trigger version resolution")
	}
	if r.version != "19.0.0-rc.1" {
		t.Errorf("expected resolved version 19.0.0-rc.1, got %q", r.version)
	}
}

func TestScanPackageGoBranchDoesNotResolveAsLatest(t *testing.T) {
	resolveCalled := false
	securityCalled := false

	defer withResolveVersion(func(mgr *manager.Manager, pkg string) (string, error) {
		resolveCalled = true
		return "v1.2.3", nil
	})()
	defer withSecurityCheck(func(eco, name, ver string) ([]security.Vulnerability, error) {
		securityCalled = true
		return nil, nil
	})()

	c := make(cache.Cache)
	r := scanPackage(goMgr(), "golang.org/x/tools/gopls@master", c)
	if resolveCalled {
		t.Error("expected floating Go branch to avoid latest-version resolution")
	}
	if securityCalled {
		t.Error("expected floating Go branch to skip security check")
	}
	if !errors.Is(r.err, errMissingVersion) || r.cacheable {
		t.Errorf("expected floating Go branch result to be non-cacheable skip, got %+v", r)
	}
	if cache.Hit(c, cache.Key("Go", "golang.org/x/tools/gopls", "master")) {
		t.Error("expected floating Go branch not to be cached as an exact version")
	}
}

func TestScanPackageResolvesHomebrewVersionedFormula(t *testing.T) {
	defer withSecurityCheck(func(eco, name, ver string) ([]security.Vulnerability, error) {
		if name != "openssl@3" || ver != "3.3.1" {
			t.Errorf("expected resolved openssl@3 3.3.1, got %s@%s", name, ver)
		}
		return nil, nil
	})()
	defer withResolveVersion(func(mgr *manager.Manager, pkg string) (string, error) {
		if pkg != "openssl@3" {
			t.Errorf("expected formula name openssl@3, got %q", pkg)
		}
		return "3.3.1", nil
	})()

	r := scanPackage(brewMgr(), "openssl@3", make(cache.Cache))
	if !r.updated {
		t.Error("expected versioned formula name to resolve via brew info")
	}
	if r.version != "3.3.1" {
		t.Errorf("expected resolved version 3.3.1, got %q", r.version)
	}
}

func TestScanPackageResolutionError(t *testing.T) {
	defer withResolveVersion(func(mgr *manager.Manager, pkg string) (string, error) {
		return "", errors.New("resolution failed")
	})()

	r := scanPackage(npmMgr(), "react", make(cache.Cache))
	if r.err == nil {
		t.Error("expected error on resolution failure")
	}
}

func TestScanPackageCacheHit(t *testing.T) {
	securityCalled := false
	defer withSecurityCheck(func(eco, name, ver string) ([]security.Vulnerability, error) {
		securityCalled = true
		return nil, nil
	})()

	c := make(cache.Cache)
	cache.Set(c, cache.Key("npm", "react", "18.0.0"))

	r := scanPackage(npmMgr(), "react@18.0.0", c)
	if !r.cached {
		t.Error("expected cached=true on cache hit")
	}
	if securityCalled {
		t.Error("expected security check skipped on cache hit")
	}
}

func TestScanPackageSetsCache(t *testing.T) {
	defer withSecurityCheck(func(eco, name, ver string) ([]security.Vulnerability, error) {
		return nil, nil
	})()
	defer withResolveVersion(func(mgr *manager.Manager, pkg string) (string, error) {
		return "18.0.0", nil
	})()

	c := make(cache.Cache)
	scanPackage(npmMgr(), "react", c)

	if !cache.Hit(c, cache.Key("npm", "react", "18.0.0")) {
		t.Error("expected cache populated after clean scan")
	}
}

func TestScanPackageEmptyResolvedVersion(t *testing.T) {
	securityCalled := false
	defer withSecurityCheck(func(eco, name, ver string) ([]security.Vulnerability, error) {
		securityCalled = true
		return nil, nil
	})()
	defer withResolveVersion(func(mgr *manager.Manager, pkg string) (string, error) {
		return "", nil
	})()

	c := make(cache.Cache)
	r := scanPackage(npmMgr(), "react", c)
	if !errors.Is(r.err, errMissingVersion) {
		t.Errorf("expected missing-version error, got %v", r.err)
	}
	if securityCalled {
		t.Error("expected empty resolved version to skip security check")
	}
	if cache.Hit(c, cache.Key("npm", "react", "")) {
		t.Error("empty version should not be cached")
	}
}

func TestScanPackageVulnsNotCached(t *testing.T) {
	defer withSecurityCheck(func(eco, name, ver string) ([]security.Vulnerability, error) {
		return []security.Vulnerability{{ID: "CVE-2021-1234", Summary: "vuln"}}, nil
	})()
	defer withResolveVersion(func(mgr *manager.Manager, pkg string) (string, error) {
		return "4.17.4", nil
	})()

	c := make(cache.Cache)
	scanPackage(npmMgr(), "lodash", c)

	if cache.Hit(c, cache.Key("npm", "lodash", "4.17.4")) {
		t.Error("expected vulnerable package NOT cached")
	}
}

func TestInterceptSpawnsSystemScan(t *testing.T) {
	dir := t.TempDir()
	orig := statsCacheDirFn
	statsCacheDirFn = func() (string, error) { return dir, nil }
	defer func() { statsCacheDirFn = orig }()

	origLFn := loadSystemStatsFn
	loadSystemStatsFn = loadSystemStats
	defer func() { loadSystemStatsFn = origLFn }()

	origEnabled := systemScanEnabled
	systemScanEnabled = true
	defer func() { systemScanEnabled = origEnabled }()

	systemSpawned := false
	defer withExecFn(noopExec)()
	defer withLoadCache(emptyCache)()
	defer withUpdateCache(noopUpdate)()
	defer withSpawnBackgroundScan(func(string) {})()
	defer withSpawnSystemScan(func() { systemSpawned = true })()
	defer withSecurityCheck(func(eco, name, ver string) ([]security.Vulnerability, error) {
		return nil, nil
	})()
	defer withResolveVersion(func(*manager.Manager, string) (string, error) {
		return "18.0.0", nil
	})()
	defer withReadManifest(func(*manager.Manager) []string { return []string{"react"} })()

	Intercept(npmMgr(), []string{"install"})
	if !systemSpawned {
		t.Error("expected system scan to be spawned")
	}
}

func TestInterceptUpdateCacheCallback(t *testing.T) {
	var updated cache.Cache
	defer withExecFn(noopExec)()
	defer withSecurityCheck(func(string, string, string) ([]security.Vulnerability, error) {
		return nil, nil
	})()
	defer withResolveVersion(func(*manager.Manager, string) (string, error) {
		return "18.0.0", nil
	})()
	defer withLoadCache(emptyCache)()
	defer withUpdateCache(func(fn func(cache.Cache)) {
		c := make(cache.Cache)
		fn(c)
		updated = c
	})()
	defer withSpawnBackgroundScan(func(string) {})()

	Intercept(npmMgr(), []string{"install", "react"})

	if !cache.Hit(updated, cache.Key("npm", "react", "18.0.0")) {
		t.Error("expected update callback to populate cache with clean package")
	}
}

func TestInterceptSpawnsBackgroundScan(t *testing.T) {
	backgroundMgr := ""

	defer withExecFn(noopExec)()
	defer withLoadCache(emptyCache)()
	defer withUpdateCache(noopUpdate)()
	defer withSpawnBackgroundScan(func(name string) { backgroundMgr = name })()
	defer withSpawnSystemScan(func() {})()
	defer withSecurityCheck(func(string, string, string) ([]security.Vulnerability, error) {
		return nil, nil
	})()
	defer withResolveVersion(func(*manager.Manager, string) (string, error) {
		return "18.0.0", nil
	})()

	Intercept(npmMgr(), []string{"install", "react"})

	if backgroundMgr != "npm" {
		t.Errorf("expected background scan for npm, got %q", backgroundMgr)
	}
}

func TestInterceptNoBackgroundSkipsDetachedScans(t *testing.T) {
	t.Setenv(envNoBackground, "1")

	origEnabled := systemScanEnabled
	systemScanEnabled = true
	defer func() { systemScanEnabled = origEnabled }()

	backgroundSpawned := false
	systemSpawned := false
	defer withExecFn(noopExec)()
	defer withLoadCache(emptyCache)()
	defer withUpdateCache(noopUpdate)()
	defer withSpawnBackgroundScan(func(string) { backgroundSpawned = true })()
	defer withSpawnSystemScan(func() { systemSpawned = true })()
	defer withSecurityCheck(func(string, string, string) ([]security.Vulnerability, error) {
		return nil, nil
	})()
	defer withResolveVersion(func(*manager.Manager, string) (string, error) {
		return "18.0.0", nil
	})()

	Intercept(npmMgr(), []string{"install", "react"})

	if backgroundSpawned || systemSpawned {
		t.Error("expected PRE_NO_BACKGROUND to skip detached scans")
	}
}

// confirm / extractPackages / execReal tests

func TestExtractPackagesStripsFlags(t *testing.T) {
	result := extractPackages(npmMgr(), []string{"--save-dev", "./local", "react", "--legacy-peer-deps", "lodash"})
	if len(result) != 2 {
		t.Errorf("expected 2 packages, got %d: %v", len(result), result)
	}
	if result[0] != "react" || result[1] != "lodash" {
		t.Errorf("unexpected packages: %v", result)
	}
}

func TestExtractPackagesSkipsWorkspaceValue(t *testing.T) {
	result := extractPackages(npmMgr(), []string{"react", "--workspace", "app"})
	if len(result) != 1 || result[0] != "react" {
		t.Errorf("expected only react, got %v", result)
	}
}

func TestExtractPackagesKeepsArgsAfterTerminator(t *testing.T) {
	result := extractPackages(npmMgr(), []string{"--", "react", "--save-dev", "lodash"})
	if len(result) != 2 || result[0] != "react" || result[1] != "lodash" {
		t.Errorf("expected react and lodash after --, got %v", result)
	}
}

func TestExtractPackagesSkipsCustomNPMManagerWorkspaceValue(t *testing.T) {
	mgr := &manager.Manager{Name: "custom-npm", Ecosystem: "npm", InstallCmds: []string{"install"}}
	result := extractPackages(mgr, []string{"--workspace", "app", "react"})
	if len(result) != 1 || result[0] != "react" {
		t.Errorf("expected only react, got %v", result)
	}
}

func TestExtractPackagesSkipsNPMValueFlags(t *testing.T) {
	result := extractPackages(npmMgr(), []string{"--save-prefix", "~", "--tag=next", "react"})
	if len(result) != 1 || result[0] != "react" {
		t.Errorf("expected only react, got %v", result)
	}
}

func TestExtractPackagesSkipsRequirementFile(t *testing.T) {
	result := extractPackages(pipMgr(), []string{"-r", "requirements.txt", "requests"})
	if len(result) != 1 || result[0] != "requests" {
		t.Errorf("expected only requests, got %v", result)
	}
}

func TestRequirementFilePaths(t *testing.T) {
	args := []string{"-r", "base.txt", "--requirements=dev.txt", "requests"}
	paths := requirementFilePaths(uvMgr(), args)
	if len(paths) != 2 || paths[0] != "base.txt" || paths[1] != "dev.txt" {
		t.Errorf("unexpected requirement paths: %v", paths)
	}
}

func TestInstallPackageArgsManifestOnlyCommands(t *testing.T) {
	tests := []struct {
		manager *manager.Manager
		args    []string
	}{
		{npmMgr(), []string{"ci", "--workspace", "app"}},
		{uvMgr(), []string{"sync", "--group", "dev"}},
		{poetryMgr(), []string{"install", "--with", "dev"}},
	}
	for _, test := range tests {
		packageArgs, intercepted := installPackageArgs(test.manager, test.args)
		if !intercepted || len(packageArgs) != 0 {
			t.Errorf("expected manifest-only install for %s, got %v", test.manager.Name, packageArgs)
		}
	}
}

func TestInstallPackageArgsCargoCommands(t *testing.T) {
	tests := []struct {
		args        []string
		want        []string
		intercepted bool
	}{
		{args: []string{"fetch", "--locked"}, intercepted: true},
		{args: []string{"--color", "always", "fetch", "--locked"}, intercepted: true},
		{args: []string{"update"}, intercepted: true},
		{args: []string{"update", "serde", "--precise", "1.0.217"}, want: []string{"serde@1.0.217"}, intercepted: true},
		{args: []string{"update", "serde", "--precise", "abc123"}, intercepted: true},
		{args: []string{"add", "serde@1.0.217"}, want: []string{"serde@^1.0.217"}, intercepted: true},
		{args: []string{"+nightly", "add", "serde@1.0.217"}, want: []string{"serde@^1.0.217"}, intercepted: true},
		{args: []string{"add", "--git", "https://example.com/repo", "custom"}, intercepted: true},
		{args: []string{"install", "ripgrep", "--version", "14.1.1"}, want: []string{"ripgrep@14.1.1"}, intercepted: true},
		{args: []string{"install", "ripgrep@14.1.1"}, want: []string{"ripgrep@14.1.1"}, intercepted: true},
		{args: []string{"install", "ripgrep", "cargo-edit", "--version", "14.1.1"}, want: []string{"ripgrep@14.1.1", "cargo-edit@14.1.1"}, intercepted: true},
		{args: []string{"install", "--registry", "internal", "tool"}, intercepted: true},
		{args: []string{"install", "--registry", "crates-io", "tool"}, want: []string{"tool"}, intercepted: true},
		{args: []string{"install", "--list"}, intercepted: false},
		{args: []string{"add", "--help"}, intercepted: false},
		{args: []string{"--help", "update"}, intercepted: false},
		{args: []string{"--explain", "update"}, intercepted: false},
		{args: []string{"fetch", "-h"}, intercepted: false},
	}
	for _, test := range tests {
		got, intercepted := installPackageArgs(cargoMgr(), test.args)
		if intercepted != test.intercepted || !slices.Equal(got, test.want) {
			t.Errorf("installPackageArgs(%v) = %v, %v; want %v, %v", test.args, got, intercepted, test.want, test.intercepted)
		}
	}
}

func TestCargoUpdateTargetsIncludesRepeatedPackageFlags(t *testing.T) {
	args := []string{"--package", "serde", "-p=regex", "--package", "anyhow"}
	want := []string{"serde", "regex", "anyhow"}
	got := cargoUpdateTargets(cargoMgr(), args)
	if !slices.Equal(got, want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
}

func TestExtractPackagesSkipsPythonManagerValueFlags(t *testing.T) {
	mgr := &manager.Manager{Name: "poetry", Ecosystem: "PyPI", InstallCmds: []string{"add"}}
	result := extractPackages(mgr, []string{"--group", "dev", "--source", "internal", "requests"})
	if len(result) != 1 || result[0] != "requests" {
		t.Errorf("expected only requests, got %v", result)
	}
}

func TestExtractPackagesSkipsEditablePath(t *testing.T) {
	result := extractPackages(pipMgr(), []string{"-e", ".", "requests"})
	if len(result) != 1 || result[0] != "requests" {
		t.Errorf("expected only requests, got %v", result)
	}
}

func TestExtractPackagesSkipsUnsupportedSources(t *testing.T) {
	result := extractPackages(npmMgr(), []string{"github:user/repo", "git@github.com:user/repo.git", "alias@npm:react@18", "react"})
	if len(result) != 1 || result[0] != "react" {
		t.Errorf("expected only react, got %v", result)
	}
}

func TestExtractPackagesRejectsInvalidCrateNames(t *testing.T) {
	result := extractPackages(cargoMgr(), []string{"valid-crate", "bad/name", "serde@1.0.0"})
	if !slices.Equal(result, []string{"valid-crate", "serde@1.0.0"}) {
		t.Errorf("unexpected Cargo packages: %v", result)
	}
}

func TestExtractPackagesEmpty(t *testing.T) {
	result := extractPackages(npmMgr(), []string{})
	if len(result) != 0 {
		t.Errorf("expected empty result, got %v", result)
	}
}

func TestConfirmYes(t *testing.T) {
	origStdin := stdinReader
	stdinReader = strings.NewReader("y\n")
	defer func() { stdinReader = origStdin }()

	if !confirm("Install?") {
		t.Error("expected true for 'y'")
	}
}

func TestConfirmYesFull(t *testing.T) {
	origStdin := stdinReader
	stdinReader = strings.NewReader("yes\n")
	defer func() { stdinReader = origStdin }()

	if !confirm("Install?") {
		t.Error("expected true for 'yes'")
	}
}

func TestConfirmNo(t *testing.T) {
	origStdin := stdinReader
	stdinReader = strings.NewReader("n\n")
	defer func() { stdinReader = origStdin }()

	if confirm("Install?") {
		t.Error("expected false for 'n'")
	}
}

func TestConfirmEmpty(t *testing.T) {
	origStdin := stdinReader
	stdinReader = strings.NewReader("\n")
	defer func() { stdinReader = origStdin }()

	if confirm("Install?") {
		t.Error("expected false for empty input")
	}
}

func TestExecRealSuccess(t *testing.T) {
	exited := false
	origExit := processExit
	processExit = func(code int) { exited = true }
	defer func() { processExit = origExit }()

	execReal("echo", []string{"hello"})
	if exited {
		t.Error("expected no exit for successful command")
	}
}

func TestExecRealExitError(t *testing.T) {
	exitCode := -1
	origExit := processExit
	processExit = func(code int) { exitCode = code; panic("exit") }
	defer func() {
		recover()
		processExit = origExit
		if exitCode != 2 {
			t.Errorf("expected exit code 2, got %d", exitCode)
		}
	}()

	execReal("sh", []string{"-c", "exit 2"})
}

func TestExecRealNonexistentCommand(t *testing.T) {
	exitCode := -1
	origExit := processExit
	processExit = func(code int) { exitCode = code; panic("exit") }
	defer func() {
		recover()
		processExit = origExit
		if exitCode != 1 {
			t.Errorf("expected exit code 1, got %d", exitCode)
		}
	}()

	execReal("nonexistent-command-xyz-abc", []string{})
}
