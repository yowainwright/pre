package proxy

import (
	"errors"
	"strings"
	"testing"

	"github.com/yowainwright/pre/internal/cache"
	"github.com/yowainwright/pre/internal/manager"
	"github.com/yowainwright/pre/internal/security"
)

func noopExec(name string, args []string) {}

func npmMgr() *manager.Manager {
	return &manager.Manager{Name: "npm", Ecosystem: "npm", InstallCmds: []string{"install", "add", "i"}}
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

func withReadManifest(fn func(*manager.Manager) []string) func() {
	orig := readManifestFn
	readManifestFn = fn
	return func() { readManifestFn = orig }
}

func withStdinInput(input string) func() {
	orig := stdinReader
	stdinReader = strings.NewReader(input)
	return func() { stdinReader = orig }
}

func emptyCache() cache.Cache { return make(cache.Cache) }
func noopSave(cache.Cache)    {}

// Intercept flow tests

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
	defer withSaveCache(noopSave)()
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
	defer withSaveCache(noopSave)()
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
	defer withSaveCache(noopSave)()
	Intercept(npmMgr(), []string{"install", "react"})
	if !execCalled {
		t.Error("expected ExecFn to be called even when version resolution fails")
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
	defer withSaveCache(noopSave)()
	Intercept(npmMgr(), []string{"install", "lodash"})
	if !execCalled {
		t.Error("expected ExecFn to be called when security check fails (don't block)")
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
			defer withSaveCache(noopSave)()
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
	defer withSaveCache(noopSave)()

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
	defer withSaveCache(noopSave)()

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
	defer withSaveCache(noopSave)()

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
	defer withSaveCache(noopSave)()

	Intercept(npmMgr(), []string{"install", "react"})
	if !execCalled {
		t.Error("expected ExecFn called after quiet clean scan")
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
	defer withSecurityCheck(func(eco, name, ver string) ([]security.Vulnerability, error) {
		return nil, nil
	})()
	defer withResolveVersion(func(mgr *manager.Manager, pkg string) (string, error) {
		return "", nil
	})()

	c := make(cache.Cache)
	r := scanPackage(npmMgr(), "react", c)
	if r.err != nil {
		t.Errorf("expected no error, got %v", r.err)
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

	defer withExecFn(noopExec)()
	defer withLoadCache(emptyCache)()
	defer withSaveCache(noopSave)()
	defer withSecurityCheck(func(eco, name, ver string) ([]security.Vulnerability, error) {
		return nil, nil
	})()
	defer withResolveVersion(func(*manager.Manager, string) (string, error) {
		return "18.0.0", nil
	})()
	defer withReadManifest(func(*manager.Manager) []string { return []string{"react"} })()

	Intercept(npmMgr(), []string{"install"})
}

// confirm / extractPackages / execReal tests

func TestExtractPackagesStripsFlags(t *testing.T) {
	result := extractPackages([]string{"--save-dev", "./local", "react", "--legacy-peer-deps", "lodash"})
	if len(result) != 2 {
		t.Errorf("expected 2 packages, got %d: %v", len(result), result)
	}
	if result[0] != "react" || result[1] != "lodash" {
		t.Errorf("unexpected packages: %v", result)
	}
}

func TestExtractPackagesEmpty(t *testing.T) {
	result := extractPackages([]string{})
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
