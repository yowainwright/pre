package proxy

import (
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/yowainwright/pre/internal/cache"
	"github.com/yowainwright/pre/internal/manager"
	preobs "github.com/yowainwright/pre/internal/obs"
	"github.com/yowainwright/pre/internal/security"
)

func noopExec(name string, args []string) {}

func npmMgr() *manager.Manager {
	return &manager.Manager{Name: "npm", Ecosystem: "npm", InstallCmds: []string{"install", "add", "i", "ci"}}
}

func bunMgr() *manager.Manager {
	return &manager.Manager{Name: "bun", Ecosystem: "npm", InstallCmds: []string{"install", "add", "i", "update"}}
}

func pnpmMgr() *manager.Manager {
	return &manager.Manager{Name: "pnpm", Ecosystem: "npm", InstallCmds: []string{"install", "add", "i", "update"}}
}

func pipMgr() *manager.Manager {
	return &manager.Manager{Name: "pip", Ecosystem: "PyPI", InstallCmds: []string{"install"}}
}

func goMgr() *manager.Manager {
	return &manager.Manager{Name: "go", Ecosystem: "Go", InstallCmds: []string{"get", "install"}}
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

func scanSingleResult(mgr *manager.Manager, spec string, c cache.Cache, allowMissing bool) scanResult {
	results := scanBatchWithPolicy(mgr, []string{spec}, c, allowMissing)
	return results[0]
}

func withExecFn(fn func(string, []string)) func() {
	orig := ExecFn
	ExecFn = fn
	return func() { ExecFn = orig }
}

func withSecurityCheck(fn func(string, string, string) ([]security.Vulnerability, error)) func() {
	origBatch := securityBatchCheckFn
	securityBatchCheckFn = func(queries []security.Query) ([][]security.Vulnerability, error) {
		results := make([][]security.Vulnerability, len(queries))
		for index, query := range queries {
			vulnerabilities, err := fn(query.Ecosystem, query.Name, query.Version)
			if err != nil {
				return nil, err
			}
			results[index] = vulnerabilities
		}
		return results, nil
	}
	return func() {
		securityBatchCheckFn = origBatch
	}
}

func withSecurityBatchCheck(fn func([]security.Query) ([][]security.Vulnerability, error)) func() {
	orig := securityBatchCheckFn
	securityBatchCheckFn = fn
	return func() { securityBatchCheckFn = orig }
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

func withReadManifestDir(fn func(*manager.Manager, string) []string) func() {
	orig := readManifestDirFn
	readManifestDirFn = fn
	return func() { readManifestDirFn = orig }
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

func withStdinInput(input string) func() {
	orig := stdinReader
	stdinReader = strings.NewReader(input)
	return func() { stdinReader = orig }
}

func withWorkingDir(t *testing.T, dir string) func() {
	t.Helper()
	original, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	return func() {
		if err := os.Chdir(original); err != nil {
			t.Error(err)
		}
	}
}

func emptyCache() cache.Cache      { return make(cache.Cache) }
func noopUpdate(func(cache.Cache)) {}

func withObsDir(t *testing.T) {
	t.Helper()
	t.Setenv("PRE_OBS_DIR", t.TempDir())
	t.Setenv("PRE_OBS", "1")
}

func requireObsEvent(t *testing.T, name string) preobs.Event {
	t.Helper()
	for _, event := range obsEvents(t) {
		if event["event.name"] == name {
			return event
		}
	}
	t.Fatalf("missing obs event %q", name)
	return nil
}

func obsEvents(t *testing.T) []preobs.Event {
	t.Helper()
	events, _, err := preobs.Events(time.Time{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	return events
}

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

func TestInterceptRecordsScanDecisionWithoutPackageName(t *testing.T) {
	t.Setenv("PRE_OBS_DIR", t.TempDir())
	t.Setenv("PRE_OBS", "1")
	defer withStdinInput("y\n")()
	defer withExecFn(noopExec)()
	defer withLoadCache(emptyCache)()
	defer withUpdateCache(noopUpdate)()
	defer withResolveVersion(func(*manager.Manager, string) (string, error) {
		return "18.2.0", nil
	})()
	defer withSecurityCheck(func(string, string, string) ([]security.Vulnerability, error) {
		return nil, nil
	})()

	Intercept(npmMgr(), []string{"install", "react"})

	events, _, err := preobs.Events(time.Time{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	names := eventNames(events)
	for _, want := range []string{"pre.scan.started", "pre.scan.completed", "pre.scan.approved"} {
		if !slices.Contains(names, want) {
			t.Fatalf("missing %s in %#v", want, names)
		}
	}
	data, _ := json.Marshal(events)
	if strings.Contains(string(data), "react") {
		t.Fatalf("obs leaked package name: %s", string(data))
	}
}

func eventNames(events []preobs.Event) []string {
	names := make([]string, 0, len(events))
	for _, event := range events {
		if name, ok := event["event.name"].(string); ok {
			names = append(names, name)
		}
	}
	return names
}

func requireNoObsLeak(t *testing.T, secret string) {
	t.Helper()
	data, _ := json.Marshal(obsEvents(t))
	if strings.Contains(string(data), secret) {
		t.Fatalf("obs leaked %q: %s", secret, string(data))
	}
}

func TestInterceptRecordsDisabledBypass(t *testing.T) {
	withObsDir(t)
	t.Setenv(envDisable, "1")
	defer withExecFn(noopExec)()

	Intercept(npmMgr(), []string{"install", "react"})

	started := requireObsEvent(t, "pre.command.started")
	bypassed := requireObsEvent(t, "pre.command.bypassed")
	if started["manager_command"] != "install" || bypassed["reason"] != "env_disabled" {
		t.Fatalf("unexpected bypass obs: started=%#v bypassed=%#v", started, bypassed)
	}
	requireNoObsLeak(t, "react")
}

func TestInterceptRecordsPassthrough(t *testing.T) {
	withObsDir(t)
	defer withExecFn(noopExec)()

	Intercept(npmMgr(), []string{"run", "build"})

	event := requireObsEvent(t, "pre.command.passthrough")
	if event["reason"] != "not_install_command" || event["manager_command"] != "run" {
		t.Fatalf("unexpected passthrough event: %#v", event)
	}
	requireNoObsLeak(t, "build")
}

func TestInterceptRecordsPolicyBlock(t *testing.T) {
	withObsDir(t)
	execCalled := false
	defer withExecFn(func(string, []string) { execCalled = true })()

	expectProcessExit(t, 1, func() {
		Intercept(npmMgr(), []string{"install", "private@file:../private"})
	})

	event := requireObsEvent(t, "pre.scan.blocked")
	if event["reason"] != "npm_policy" || event["error_type"] == "" {
		t.Fatalf("unexpected block event: %#v", event)
	}
	if execCalled {
		t.Fatal("expected blocked install not to execute")
	}
	requireNoObsLeak(t, "private")
}

func TestInterceptRecordsPackageLimitSkip(t *testing.T) {
	withObsDir(t)
	t.Setenv(envMaxPackages, "1")
	defer withExecFn(noopExec)()

	Intercept(npmMgr(), []string{"install", "react", "lodash"})

	event := requireObsEvent(t, "pre.scan.skipped")
	if event["reason"] != "package_limit" {
		t.Fatalf("unexpected skip reason: %#v", event)
	}
	if event["package_count"] != float64(2) || event["package_limit"] != float64(1) {
		t.Fatalf("unexpected package counts: %#v", event)
	}
	requireNoObsLeak(t, "react")
}

func TestInterceptRecordsScanErrorBlock(t *testing.T) {
	withObsDir(t)
	execCalled := false
	defer withExecFn(func(string, []string) { execCalled = true })()
	defer withLoadCache(emptyCache)()
	defer withUpdateCache(noopUpdate)()
	defer withResolveVersion(func(*manager.Manager, string) (string, error) {
		return "18.0.0", nil
	})()
	defer withSecurityCheck(func(string, string, string) ([]security.Vulnerability, error) {
		return nil, errors.New("check failed")
	})()

	expectProcessExit(t, 1, func() {
		Intercept(npmMgr(), []string{"install", "react"})
	})

	completed := requireObsEvent(t, "pre.scan.completed")
	blocked := requireObsEvent(t, "pre.scan.blocked")
	if completed["error_count"] != float64(1) || blocked["reason"] != "scan_error" {
		t.Fatalf("unexpected scan error obs: completed=%#v blocked=%#v", completed, blocked)
	}
	if execCalled {
		t.Fatal("expected scan error not to execute")
	}
}

func TestInterceptRecordsCriticalPromptDenied(t *testing.T) {
	withObsDir(t)
	defer withStdinInput("n\n")()
	defer withExecFn(noopExec)()
	defer withLoadCache(emptyCache)()
	defer withUpdateCache(noopUpdate)()
	defer withResolveVersion(func(*manager.Manager, string) (string, error) {
		return "18.0.0", nil
	})()
	defer withSecurityCheck(func(string, string, string) ([]security.Vulnerability, error) {
		return []security.Vulnerability{{ID: "CVE-1234", Severity: security.SeverityCritical}}, nil
	})()

	expectProcessExit(t, 1, func() {
		Intercept(npmMgr(), []string{"install", "react"})
	})

	prompted := requireObsEvent(t, "pre.scan.prompted")
	denied := requireObsEvent(t, "pre.scan.denied")
	if prompted["critical_count"] != float64(1) || denied["reason"] != "user_denied" {
		t.Fatalf("unexpected denied obs: prompted=%#v denied=%#v", prompted, denied)
	}
}

func TestInterceptRecordsCriticalPromptApproved(t *testing.T) {
	withObsDir(t)
	execCalled := false
	defer withStdinInput("y\n")()
	defer withExecFn(func(string, []string) { execCalled = true })()
	defer withLoadCache(emptyCache)()
	defer withUpdateCache(noopUpdate)()
	defer withResolveVersion(func(*manager.Manager, string) (string, error) {
		return "18.0.0", nil
	})()
	defer withSecurityCheck(func(string, string, string) ([]security.Vulnerability, error) {
		return []security.Vulnerability{{ID: "CVE-1234", Severity: security.SeverityHigh}}, nil
	})()

	Intercept(npmMgr(), []string{"install", "react"})

	event := requireObsEvent(t, "pre.scan.approved")
	if event["reason"] != "user_approved_high_or_critical" {
		t.Fatalf("unexpected approved obs: event=%#v exec=%t", event, execCalled)
	}
	if !execCalled {
		t.Fatalf("unexpected approved obs: event=%#v exec=%t", event, execCalled)
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
	defer withStdinInput("y\n")()
	defer withExecFn(noopExec)()
	defer withLoadCache(emptyCache)()
	defer withUpdateCache(noopUpdate)()
	defer withReadManifestDir(func(*manager.Manager, string) []string { return []string{"react@18.0.0"} })()
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

func TestInterceptUnsafePackageLockBlocks(t *testing.T) {
	dir := t.TempDir()
	lockfile := `{"packages":{"node_modules/lodash":{"name":"evil-pkg","version":"4.17.21","resolved":"https://attacker.example/evil.tgz"}}}`
	if err := os.WriteFile(filepath.Join(dir, "package-lock.json"), []byte(lockfile), 0o644); err != nil {
		t.Fatal(err)
	}
	defer withWorkingDir(t, dir)()
	execCalled := false
	defer withExecFn(func(string, []string) { execCalled = true })()

	expectProcessExit(t, 1, func() {
		Intercept(npmMgr(), []string{"ci"})
	})
	if execCalled {
		t.Error("expected unsafe package lock to block npm ci")
	}
}

func TestInterceptManifestCommandUsesSelectedProject(t *testing.T) {
	tests := []struct {
		name        string
		manager     *manager.Manager
		args        func(string) []string
		lockName    string
		lock        string
		packageName string
	}{
		{
			name: "npm prefix", manager: npmMgr(),
			args:        func(dir string) []string { return []string{"ci", "--prefix", dir} },
			lockName:    "package-lock.json",
			lock:        `{"packages":{"node_modules/react":{"version":"18.2.0"}}}`,
			packageName: "react",
		},
		{
			name: "bun cwd", manager: bunMgr(),
			args:        func(dir string) []string { return []string{"install", "--cwd", dir} },
			lockName:    "bun.lock",
			lock:        "{\n  \"packages\": {\n    \"react\": [\"react@18.2.0\", {}],\n  },\n}\n",
			packageName: "react",
		},
		{
			name: "pnpm dir", manager: pnpmMgr(),
			args:        func(dir string) []string { return []string{"install", "--dir", dir} },
			lockName:    "pnpm-lock.yaml",
			lock:        "packages:\n  react@18.2.0:\n    resolution: {integrity: sha512-abc}\n",
			packageName: "react",
		},
		{
			name: "uv project", manager: uvMgr(),
			args:        func(dir string) []string { return []string{"sync", "--project", dir} },
			lockName:    "uv.lock",
			lock:        "[[package]]\nname = \"requests\"\nversion = \"2.32.0\"\n",
			packageName: "requests",
		},
		{
			name: "poetry project", manager: poetryMgr(),
			args:        func(dir string) []string { return []string{"install", "-P", dir} },
			lockName:    "poetry.lock",
			lock:        "[[package]]\nname = \"requests\"\nversion = \"2.32.0\"\n",
			packageName: "requests",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			currentDir := t.TempDir()
			projectDir := t.TempDir()
			defer withWorkingDir(t, currentDir)()
			path := filepath.Join(projectDir, test.lockName)
			if err := os.WriteFile(path, []byte(test.lock), 0o644); err != nil {
				t.Fatal(err)
			}

			scanned := false
			defer withStdinInput("y\n")()
			defer withExecFn(noopExec)()
			defer withLoadCache(emptyCache)()
			defer withUpdateCache(noopUpdate)()
			defer withSecurityCheck(func(_ string, name, _ string) ([]security.Vulnerability, error) {
				scanned = scanned || name == test.packageName
				return nil, nil
			})()

			Intercept(test.manager, test.args(projectDir))
			if !scanned {
				t.Fatalf("expected %s package to be scanned", test.packageName)
			}
		})
	}
}

func TestInterceptNPMExternalSourceBlocks(t *testing.T) {
	specs := []string{
		"git+https://example.com/private.git",
		"private@file:../private",
		"alias@npm:react@18",
		"private@workspace:*",
		"https://example.com/private.tgz",
	}
	for _, spec := range specs {
		t.Run(spec, func(t *testing.T) {
			execCalled := false
			defer withExecFn(func(string, []string) { execCalled = true })()

			expectProcessExit(t, 1, func() {
				Intercept(npmMgr(), []string{"install", spec})
			})
			if execCalled {
				t.Error("expected external npm source to block install")
			}
		})
	}
}

func TestInterceptNPMManifestExternalSourceBlocks(t *testing.T) {
	dir := t.TempDir()
	manifest := `{"dependencies":{"private":"git+https://example.com/private.git"}}`
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	defer withWorkingDir(t, dir)()

	execCalled := false
	defer withExecFn(func(string, []string) { execCalled = true })()
	expectProcessExit(t, 1, func() {
		Intercept(npmMgr(), []string{"install"})
	})
	if execCalled {
		t.Error("expected manifest external source to block install")
	}
}

func TestInterceptUVPipInstall(t *testing.T) {
	securityCalled := false
	defer withStdinInput("y\n")()
	defer withExecFn(noopExec)()
	defer withLoadCache(emptyCache)()
	defer withUpdateCache(noopUpdate)()
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
	defer withStdinInput("y\n")()
	defer withExecFn(func(name string, args []string) {
		executed = name == "cargo" && slices.Equal(args, []string{"add", "serde@1.0.0"})
	})()
	defer withLoadCache(emptyCache)()
	defer withUpdateCache(noopUpdate)()
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
	defer withStdinInput("y\n")()
	defer withExecFn(noopExec)()
	defer withLoadCache(emptyCache)()
	defer withUpdateCache(noopUpdate)()
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
	defer withStdinInput("y\n")()
	defer withExecFn(noopExec)()
	defer withLoadCache(emptyCache)()
	defer withUpdateCache(noopUpdate)()
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
	defer withStdinInput("y\n")()
	defer withExecFn(noopExec)()
	defer withLoadCache(emptyCache)()
	defer withUpdateCache(noopUpdate)()
	defer withReadManifestDir(func(*manager.Manager, string) []string { return []string{"requests==2.32.0"} })()
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
	defer withStdinInput("y\n")()
	defer withExecFn(func(string, []string) { execCalled = true })()
	defer withLoadCache(emptyCache)()
	defer withUpdateCache(noopUpdate)()
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
	defer withStdinInput("y\n")()
	defer withExecFn(func(name string, args []string) { execCalled = true })()
	defer withSecurityCheck(func(eco, name, ver string) ([]security.Vulnerability, error) {
		return nil, nil
	})()
	defer withResolveVersion(func(mgr *manager.Manager, pkg string) (string, error) {
		return "1.0.0", nil
	})()
	defer withLoadCache(emptyCache)()
	defer withUpdateCache(noopUpdate)()
	defer withReadManifestDir(func(mgr *manager.Manager, _ string) []string {
		return []string{"lodash@1.0.0", "react@18.0.0"}
	})()
	Intercept(npmMgr(), []string{"install"})
	if !execCalled {
		t.Error("expected ExecFn called after scanning manifest packages")
	}
}

func TestInterceptInstallManifestEmpty(t *testing.T) {
	execCalled := false
	defer withExecFn(func(name string, args []string) { execCalled = true })()
	defer withReadManifestDir(func(mgr *manager.Manager, _ string) []string { return nil })()

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
	defer withStdinInput("y\n")()
	defer withExecFn(func(name string, args []string) { execCalled = true })()
	defer withSecurityCheck(func(eco, name, ver string) ([]security.Vulnerability, error) {
		return nil, nil
	})()
	defer withResolveVersion(func(mgr *manager.Manager, pkg string) (string, error) {
		return "18.0.0", nil
	})()
	defer withLoadCache(emptyCache)()
	defer withUpdateCache(noopUpdate)()
	Intercept(npmMgr(), []string{"install", "react@18.0.0"})
	if !execCalled {
		t.Error("expected ExecFn to be called for clean package")
	}
}

func TestInterceptBatchInstallScansOnlyCacheMisses(t *testing.T) {
	execCalled := false
	var queries []security.Query
	c := cacheWithApprovedReact()

	defer withStdinInput("y\n")()
	defer withExecFn(func(string, []string) { execCalled = true })()
	defer withLoadCache(func() cache.Cache { return c })()
	defer withUpdateCache(noopUpdate)()
	defer withSecurityBatchCheck(func(got []security.Query) ([][]security.Vulnerability, error) {
		queries = got
		return make([][]security.Vulnerability, len(got)), nil
	})()

	Intercept(npmMgr(), []string{"install", "react@18.0.0", "lodash@4.17.21"})

	assertBatchMissScan(t, execCalled, queries)
}

func cacheWithApprovedReact() cache.Cache {
	c := make(cache.Cache)
	cache.Set(c, cache.Key("npm", "react", "18.0.0"))
	return c
}

func assertBatchMissScan(t *testing.T, execCalled bool, queries []security.Query) {
	t.Helper()
	if !execCalled {
		t.Fatal("expected install to run after approval")
	}
	if len(queries) != 1 {
		t.Fatalf("expected one batch query for lodash, got %#v", queries)
	}
	if queries[0].Name != "lodash" {
		t.Fatalf("expected one batch query for lodash, got %#v", queries)
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

func TestInterceptDeniedInstallDoesNotCache(t *testing.T) {
	cacheUpdated := false
	defer withStdinInput("n\n")()
	defer withExecFn(noopExec)()
	defer withLoadCache(emptyCache)()
	defer withUpdateCache(func(func(cache.Cache)) { cacheUpdated = true })()
	defer withSecurityCheck(func(string, string, string) ([]security.Vulnerability, error) {
		return nil, nil
	})()
	defer withResolveVersion(func(*manager.Manager, string) (string, error) {
		return "18.0.0", nil
	})()

	expectProcessExit(t, 1, func() {
		Intercept(npmMgr(), []string{"install", "react"})
	})

	if cacheUpdated {
		t.Fatal("expected denied install not to update cache")
	}
}

func TestInterceptApprovedWarningWritesCache(t *testing.T) {
	updated := make(cache.Cache)
	defer withStdinInput("y\n")()
	defer withExecFn(noopExec)()
	defer withLoadCache(emptyCache)()
	defer withUpdateCache(func(fn func(cache.Cache)) { fn(updated) })()
	defer withSecurityCheck(func(string, string, string) ([]security.Vulnerability, error) {
		return []security.Vulnerability{{ID: "CVE-2026-0001", Severity: security.SeverityMedium}}, nil
	})()
	defer withResolveVersion(func(*manager.Manager, string) (string, error) {
		return "18.0.0", nil
	})()

	Intercept(npmMgr(), []string{"install", "react"})

	if !cache.Hit(updated, cache.Key("npm", "react", "18.0.0")) {
		t.Fatalf("expected approved warning to be cached, got %#v", updated)
	}
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
	defer withStdinInput("y\n")()
	defer withExecFn(func(name string, args []string) { execCalled = true })()
	defer withSecurityCheck(func(eco, name, ver string) ([]security.Vulnerability, error) {
		return nil, nil
	})()
	defer withResolveVersion(func(mgr *manager.Manager, pkg string) (string, error) {
		return "18.0.0", nil
	})()
	defer withLoadCache(emptyCache)()
	defer withUpdateCache(noopUpdate)()

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
	defer withStdinInput("y\n")()

	out := captureStdout(t, func() {
		Intercept(npmMgr(), []string{"install", "react"})
	})

	if !strings.Contains(out, "Approve install?") {
		t.Errorf("expected quiet first install to prompt, got %q", out)
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
	defer withStdinInput("y\n")()

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

func TestInterceptManifestFallbackBlocksMissingVersion(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "requirements.txt"), []byte("requests\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	defer withWorkingDir(t, dir)()

	execCalled := false
	resolveCalled := false
	securityCalled := false

	defer withExecFn(func(string, []string) { execCalled = true })()
	defer withLoadCache(emptyCache)()
	defer withUpdateCache(noopUpdate)()
	defer withSecurityCheck(func(eco, name, ver string) ([]security.Vulnerability, error) {
		securityCalled = true
		return nil, nil
	})()
	defer withResolveVersion(func(*manager.Manager, string) (string, error) {
		resolveCalled = true
		return "18.0.0", nil
	})()

	expectProcessExit(t, 1, func() {
		Intercept(pipMgr(), []string{"install"})
	})
	if resolveCalled || securityCalled || execCalled {
		t.Error("expected unversioned manifest dependency to block before resolution")
	}
}

func TestInterceptRequirementFileBlocksMissingVersion(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "requirements.txt")
	if err := os.WriteFile(path, []byte("requests\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	execCalled := false
	resolveCalled := false
	defer withExecFn(func(string, []string) { execCalled = true })()
	defer withLoadCache(emptyCache)()
	defer withResolveVersion(func(*manager.Manager, string) (string, error) {
		resolveCalled = true
		return "2.32.0", nil
	})()

	expectProcessExit(t, 1, func() {
		Intercept(pipMgr(), []string{"install", "-r", path})
	})
	if resolveCalled || execCalled {
		t.Error("expected unversioned requirements dependency to block before resolution")
	}
}

func TestInterceptDirectPackageResolvesMissingVersion(t *testing.T) {
	resolveCalled := false
	securityCalled := false
	defer withStdinInput("y\n")()
	defer withExecFn(noopExec)()
	defer withLoadCache(emptyCache)()
	defer withUpdateCache(noopUpdate)()
	defer withResolveVersion(func(*manager.Manager, string) (string, error) {
		resolveCalled = true
		return "18.2.0", nil
	})()
	defer withSecurityCheck(func(string, string, string) ([]security.Vulnerability, error) {
		securityCalled = true
		return nil, nil
	})()

	Intercept(npmMgr(), []string{"install", "react"})
	if !resolveCalled || !securityCalled {
		t.Error("expected direct unversioned package to resolve and scan")
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

	r := scanSingleResult(npmMgr(), "react@18.0.0", make(cache.Cache), true)
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

	r := scanSingleResult(npmMgr(), "react", make(cache.Cache), true)
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

	r := scanSingleResult(npmMgr(), "react@latest", make(cache.Cache), true)
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

	r := scanSingleResult(npmMgr(), "react@next", make(cache.Cache), true)
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
	r := scanSingleResult(goMgr(), "golang.org/x/tools/gopls@master", c, true)
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

	r := scanSingleResult(brewMgr(), "openssl@3", make(cache.Cache), true)
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

	r := scanSingleResult(npmMgr(), "react", make(cache.Cache), true)
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

	r := scanSingleResult(npmMgr(), "react@18.0.0", c, true)
	if !r.cached {
		t.Error("expected cached=true on cache hit")
	}
	if securityCalled {
		t.Error("expected security check skipped on cache hit")
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
	r := scanSingleResult(npmMgr(), "react", c, true)
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

func TestInterceptUpdateCacheCallback(t *testing.T) {
	var updated cache.Cache
	defer withStdinInput("y\n")()
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

	Intercept(npmMgr(), []string{"install", "react"})

	if !cache.Hit(updated, cache.Key("npm", "react", "18.0.0")) {
		t.Error("expected update callback to populate cache with clean package")
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

func TestInstallPackageArgsParsesGlobalOptions(t *testing.T) {
	tests := []struct {
		manager *manager.Manager
		args    []string
		want    []string
	}{
		{manager: npmMgr(), args: []string{"--prefix", "app", "install", "react"}, want: []string{"react"}},
		{manager: pnpmMgr(), args: []string{"--dir=app", "add", "react"}, want: []string{"react"}},
		{manager: goMgr(), args: []string{"-C", "app", "get", "example.com/mod"}, want: []string{"example.com/mod"}},
	}
	for _, test := range tests {
		packageArgs, intercepted := installPackageArgs(test.manager, test.args)
		matches := slices.Equal(packageArgs, test.want)
		if !intercepted || !matches {
			t.Errorf("expected %s install args %v, got %v", test.manager.Name, test.want, packageArgs)
		}
	}
}

func TestInstallPackageArgsBypassesGoRemoval(t *testing.T) {
	args := []string{"get", "example.com/mod@none"}
	packageArgs, intercepted := installPackageArgs(goMgr(), args)
	if intercepted || packageArgs != nil {
		t.Errorf("expected Go removal passthrough, got intercepted=%v args=%v", intercepted, packageArgs)
	}
}

func TestInstallPackagesSkipsGoRemovalInMixedGet(t *testing.T) {
	args := []string{"example.com/old@none", "example.com/new@v1.0.0"}
	packages, err := installPackages(goMgr(), args)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"example.com/new@v1.0.0"}
	if !slices.Equal(packages, want) {
		t.Errorf("expected %v, got %v", want, packages)
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
	withObsDir(t)
	exited := false
	origExit := processExit
	processExit = func(code int) { exited = true }
	defer func() { processExit = origExit }()

	execReal("echo", []string{"hello"})
	if exited {
		t.Error("expected no exit for successful command")
	}
	completed := requireObsEvent(t, "pre.manager.exec.completed")
	if completed["exit_code"] != float64(0) || completed["success"] != true {
		t.Fatalf("unexpected exec completion event: %#v", completed)
	}
	requireNoObsLeak(t, "hello")
}

func TestExecRealExitError(t *testing.T) {
	withObsDir(t)

	expectProcessExit(t, 2, func() {
		execReal("sh", []string{"-c", "exit 2"})
	})

	completed := requireObsEvent(t, "pre.manager.exec.completed")
	if completed["exit_code"] != float64(2) || completed["success"] != false {
		t.Fatalf("unexpected exec failure event: %#v", completed)
	}
}

func TestExecRealNonexistentCommand(t *testing.T) {
	withObsDir(t)

	expectProcessExit(t, 1, func() {
		execReal("nonexistent-command-xyz-abc", []string{})
	})

	completed := requireObsEvent(t, "pre.manager.exec.completed")
	if completed["exit_code"] != float64(1) || completed["success"] != false {
		t.Fatalf("unexpected exec command error event: %#v", completed)
	}
}
