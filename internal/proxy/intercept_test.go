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

func pipMgr() *manager.Manager {
	return &manager.Manager{Name: "pip", Ecosystem: "PyPI", InstallCmds: []string{"install"}}
}

func goMgr() *manager.Manager {
	return &manager.Manager{Name: "go", Ecosystem: "Go", InstallCmds: []string{"install"}}
}

func brewMgr() *manager.Manager {
	return &manager.Manager{Name: "brew", Ecosystem: "Homebrew", InstallCmds: []string{"install"}}
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
	defer withUpdateCache(noopUpdate)()
	defer withSpawnBackgroundScan(func(string) {})()
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

func TestInterceptManifestFallbackSkipsLatestGuess(t *testing.T) {
	resolveCalled := false
	checkedVersion := "unset"

	defer withExecFn(noopExec)()
	defer withLoadCache(emptyCache)()
	defer withUpdateCache(noopUpdate)()
	defer withSpawnBackgroundScan(func(string) {})()
	defer withSecurityCheck(func(eco, name, ver string) ([]security.Vulnerability, error) {
		checkedVersion = ver
		return nil, nil
	})()
	defer withResolveVersion(func(*manager.Manager, string) (string, error) {
		resolveCalled = true
		return "18.0.0", nil
	})()
	defer withReadManifest(func(*manager.Manager) []string { return []string{"react"} })()

	Intercept(npmMgr(), []string{"install"})

	if resolveCalled {
		t.Error("expected manifest fallback without an exact version to skip latest-version resolution")
	}
	if checkedVersion != "" {
		t.Errorf("expected package-level check without a guessed version, got %q", checkedVersion)
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
	checkedVersion := "unset"

	defer withResolveVersion(func(mgr *manager.Manager, pkg string) (string, error) {
		resolveCalled = true
		return "v1.2.3", nil
	})()
	defer withSecurityCheck(func(eco, name, ver string) ([]security.Vulnerability, error) {
		checkedVersion = ver
		return nil, nil
	})()

	c := make(cache.Cache)
	r := scanPackage(goMgr(), "golang.org/x/tools/gopls@master", c)
	if resolveCalled {
		t.Error("expected floating Go branch to avoid latest-version resolution")
	}
	if checkedVersion != "" {
		t.Errorf("expected package-level Go check, got version %q", checkedVersion)
	}
	if r.cacheable {
		t.Error("expected floating Go branch result to be non-cacheable")
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
