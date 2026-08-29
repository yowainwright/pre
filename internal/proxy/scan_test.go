package proxy

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yowainwright/pre/internal/cache"
	"github.com/yowainwright/pre/internal/manager"
	"github.com/yowainwright/pre/internal/security"
)

func withExecutableFn(fn func() (string, error)) func() {
	orig := executableFn
	executableFn = fn
	return func() { executableFn = orig }
}

func withSaveSystemStats(fn func(SystemStats)) func() {
	orig := saveSystemStatsFn
	saveSystemStatsFn = fn
	return func() { saveSystemStatsFn = orig }
}

func withSystemScanLock(fn func() (func(), bool)) func() {
	orig := acquireSystemScanLock
	acquireSystemScanLock = fn
	return func() { acquireSystemScanLock = orig }
}

func TestSetSystemScanEnabled(t *testing.T) {
	orig := systemScanEnabled
	defer func() { systemScanEnabled = orig }()

	SetSystemScanEnabled(true)
	if !systemScanEnabled {
		t.Error("expected systemScanEnabled to be true")
	}
	SetSystemScanEnabled(false)
	if systemScanEnabled {
		t.Error("expected systemScanEnabled to be false")
	}
}

func TestSpawnBackgroundScan(t *testing.T) {
	spawnBackgroundScan("npm")
}

func TestSpawnBackgroundScanError(t *testing.T) {
	withObsDir(t)
	defer withExecutableFn(func() (string, error) { return "", errors.New("no exec") })()
	spawnBackgroundScan("npm")

	failed := requireObsEvent(t, "pre.background_scan.spawn_failed")
	if failed["error_type"] == "" {
		t.Fatalf("expected spawn failure error type, got %#v", failed)
	}
}

func TestSpawnSystemScan(t *testing.T) {
	spawnSystemScan()
}

func TestSpawnSystemScanError(t *testing.T) {
	withObsDir(t)
	defer withExecutableFn(func() (string, error) { return "", errors.New("no exec") })()
	spawnSystemScan()

	failed := requireObsEvent(t, "pre.system_scan.spawn_failed")
	if failed["error_type"] == "" {
		t.Fatalf("expected spawn failure error type, got %#v", failed)
	}
}

func TestRunBackgroundScanEmpty(t *testing.T) {
	defer withReadManifest(func(*manager.Manager) []string { return nil })()

	mgr := &manager.Manager{Name: "npm", Ecosystem: "npm"}
	RunBackgroundScan(mgr)
}

func TestRunBackgroundScanUnsafePackageLockSkipsScan(t *testing.T) {
	dir := t.TempDir()
	lockfile := `{"packages":{"node_modules/lodash":{"version":"4.17.21","resolved":"https://attacker.example/lodash.tgz"}}}`
	if err := os.WriteFile(filepath.Join(dir, "package-lock.json"), []byte(lockfile), 0o644); err != nil {
		t.Fatal(err)
	}
	defer withWorkingDir(t, dir)()
	readCalled := false
	defer withReadManifest(func(*manager.Manager) []string {
		readCalled = true
		return []string{"lodash@4.17.21"}
	})()
	var savedStats SystemStats
	defer withSaveSystemStats(func(stats SystemStats) { savedStats = stats })()

	RunBackgroundScan(npmMgr())
	if readCalled || savedStats.Errors != 1 {
		t.Fatalf("expected unsafe lockfile to stop project scan, read=%t stats=%+v", readCalled, savedStats)
	}
}

func TestRunBackgroundScanDisabledSkipsWork(t *testing.T) {
	withObsDir(t)
	t.Setenv(envDisable, "1")

	readCalled := false
	defer withReadManifest(func(*manager.Manager) []string {
		readCalled = true
		return []string{"react@18.0.0"}
	})()

	RunBackgroundScan(npmMgr())

	if readCalled {
		t.Error("expected disabled project scan to skip manifest reading")
	}
	skipped := requireObsEvent(t, "pre.background_scan.skipped")
	if skipped["reason"] != "env_disabled" {
		t.Fatalf("unexpected skip event: %#v", skipped)
	}
}

func TestRunBackgroundScan(t *testing.T) {
	withObsDir(t)
	var savedStats SystemStats
	savedCache := make(cache.Cache)
	defer withSaveSystemStats(func(s SystemStats) { savedStats = s })()
	defer withUpdateCache(func(apply func(cache.Cache)) { apply(savedCache) })()
	defer withLoadCache(emptyCache)()
	defer withResolveVersion(func(*manager.Manager, string) (string, error) {
		return "4.17.21", nil
	})()
	defer withSecurityCheck(func(string, string, string) ([]security.Vulnerability, error) {
		return nil, nil
	})()
	defer withReadManifest(func(*manager.Manager) []string {
		return []string{"lodash@4.17.21"}
	})()

	mgr := &manager.Manager{Name: "npm", Ecosystem: "npm"}
	RunBackgroundScan(mgr)

	if savedStats.Total != 1 {
		t.Errorf("expected Total=1, got %d", savedStats.Total)
	}
	if !cache.Hit(savedCache, cache.Key("npm", "lodash", "4.17.21")) {
		t.Error("expected project scan to persist cache")
	}
	completed := requireObsEvent(t, "pre.background_scan.completed")
	if completed["package_count"] != float64(1) || completed["error_count"] != float64(0) {
		t.Fatalf("unexpected project scan event: %#v", completed)
	}
}

func TestRunBackgroundScanUsesBatch(t *testing.T) {
	batchCalls := 0
	defer withSaveSystemStats(func(SystemStats) {})()
	defer withUpdateCache(noopUpdate)()
	defer withLoadCache(emptyCache)()
	defer withSecurityBatchCheck(func(queries []security.Query) ([][]security.Vulnerability, error) {
		batchCalls++
		return make([][]security.Vulnerability, len(queries)), nil
	})()
	defer withReadManifest(func(*manager.Manager) []string {
		return []string{"react@18.0.0", "lodash@4.17.21"}
	})()

	RunBackgroundScan(npmMgr())

	if batchCalls != 1 {
		t.Errorf("expected one OSV batch request, got %d", batchCalls)
	}
}

func TestRunBackgroundScanPackageLimitSkipsWork(t *testing.T) {
	withObsDir(t)
	t.Setenv(envMaxPackages, "1")

	loadCalled := false
	securityCalled := false
	statsSaved := false
	defer withLoadCache(func() cache.Cache {
		loadCalled = true
		return emptyCache()
	})()
	defer withSecurityCheck(func(string, string, string) ([]security.Vulnerability, error) {
		securityCalled = true
		return nil, nil
	})()
	defer withSaveSystemStats(func(SystemStats) { statsSaved = true })()
	defer withReadManifest(func(*manager.Manager) []string {
		return []string{"react@18.0.0", "lodash@4.17.21"}
	})()

	RunBackgroundScan(npmMgr())

	if loadCalled || securityCalled || statsSaved {
		t.Error("expected package limit to skip project scan work")
	}
	skipped := requireObsEvent(t, "pre.background_scan.skipped")
	if skipped["reason"] != "package_limit" {
		t.Fatalf("unexpected package limit event: %#v", skipped)
	}
}

func TestRunBackgroundScanCritical(t *testing.T) {
	var savedStats SystemStats
	defer withSaveSystemStats(func(s SystemStats) { savedStats = s })()
	defer withLoadCache(emptyCache)()
	defer withSecurityCheck(func(string, string, string) ([]security.Vulnerability, error) {
		return []security.Vulnerability{{ID: "CVE-1234", Severity: "CRITICAL"}}, nil
	})()
	defer withReadManifest(func(*manager.Manager) []string {
		return []string{"lodash@4.17.21"}
	})()

	mgr := &manager.Manager{Name: "npm", Ecosystem: "npm"}
	RunBackgroundScan(mgr)

	if savedStats.Crit != 1 {
		t.Errorf("expected Crit=1, got %d", savedStats.Crit)
	}
}

func TestRunBackgroundScanWarn(t *testing.T) {
	var savedStats SystemStats
	defer withSaveSystemStats(func(s SystemStats) { savedStats = s })()
	defer withLoadCache(emptyCache)()
	defer withSecurityCheck(func(string, string, string) ([]security.Vulnerability, error) {
		return []security.Vulnerability{{ID: "CVE-1234", Severity: "MEDIUM"}}, nil
	})()
	defer withReadManifest(func(*manager.Manager) []string {
		return []string{"lodash@4.17.21"}
	})()

	mgr := &manager.Manager{Name: "npm", Ecosystem: "npm"}
	RunBackgroundScan(mgr)

	if savedStats.Warn != 1 {
		t.Errorf("expected Warn=1, got %d", savedStats.Warn)
	}
}

func TestRunSystemScan(t *testing.T) {
	withObsDir(t)
	var savedStats SystemStats
	securityCalled := false
	defer withSaveSystemStats(func(s SystemStats) { savedStats = s })()
	defer withUpdateCache(noopUpdate)()
	defer withSystemScanLock(func() (func(), bool) { return nil, true })()
	defer withSecurityCheck(func(string, string, string) ([]security.Vulnerability, error) {
		securityCalled = true
		return nil, nil
	})()

	c := make(cache.Cache)
	cache.Set(c, cache.Key("npm", "lodash", "4.17.21"))
	defer withLoadCache(func() cache.Cache { return c })()

	RunSystemScan()

	if savedStats.Total != 1 {
		t.Errorf("expected Total=1, got %d", savedStats.Total)
	}
	if !securityCalled {
		t.Error("expected system scan to query OSV for cached entry")
	}
	completed := requireObsEvent(t, "pre.system_scan.completed")
	if completed["package_count"] != float64(1) {
		t.Fatalf("unexpected system scan event: %#v", completed)
	}
	if completed["pending_count"] != float64(1) {
		t.Fatalf("unexpected system scan event: %#v", completed)
	}
}

func TestRunSystemScanDisabledSkipsWork(t *testing.T) {
	withObsDir(t)
	t.Setenv(envDisable, "1")

	lockCalled := false
	defer withSystemScanLock(func() (func(), bool) {
		lockCalled = true
		return nil, true
	})()

	RunSystemScan()

	if lockCalled {
		t.Error("expected disabled system scan to skip lock acquisition")
	}
	skipped := requireObsEvent(t, "pre.system_scan.skipped")
	if skipped["reason"] != "env_disabled" {
		t.Fatalf("unexpected system scan skip event: %#v", skipped)
	}
}

func TestRunSystemScanPackageLimitSkipsWork(t *testing.T) {
	withObsDir(t)
	t.Setenv(envMaxPackages, "1")

	securityCalled := false
	statsSaved := false
	defer withUpdateCache(noopUpdate)()
	defer withSystemScanLock(func() (func(), bool) { return nil, true })()
	defer withSecurityCheck(func(string, string, string) ([]security.Vulnerability, error) {
		securityCalled = true
		return nil, nil
	})()
	defer withSaveSystemStats(func(SystemStats) { statsSaved = true })()

	c := make(cache.Cache)
	cache.Set(c, cache.Key("npm", "react", "18.0.0"))
	cache.Set(c, cache.Key("npm", "lodash", "4.17.21"))
	defer withLoadCache(func() cache.Cache { return c })()

	RunSystemScan()

	if securityCalled || statsSaved {
		t.Error("expected package limit to skip system scan work")
	}
	skipped := requireObsEvent(t, "pre.system_scan.skipped")
	if skipped["reason"] != "package_limit" {
		t.Fatalf("unexpected package limit event: %#v", skipped)
	}
}

func TestRunSystemScanWithVulns(t *testing.T) {
	var savedStats SystemStats
	defer withSaveSystemStats(func(s SystemStats) { savedStats = s })()
	defer withUpdateCache(noopUpdate)()
	defer withSystemScanLock(func() (func(), bool) { return nil, true })()
	defer withSecurityCheck(func(string, string, string) ([]security.Vulnerability, error) {
		return []security.Vulnerability{{ID: "CVE-1234", Severity: "CRITICAL"}}, nil
	})()

	c := make(cache.Cache)
	cache.Set(c, cache.Key("npm", "lodash", "4.17.21"))
	defer withLoadCache(func() cache.Cache { return c })()

	RunSystemScan()

	if savedStats.Crit != 1 {
		t.Errorf("expected Crit=1, got %d", savedStats.Crit)
	}
	if savedStats.Total != 1 {
		t.Errorf("expected Total=1, got %d", savedStats.Total)
	}
}

func TestRunSystemScanSecurityError(t *testing.T) {
	var savedStats SystemStats
	defer withSaveSystemStats(func(s SystemStats) { savedStats = s })()
	defer withUpdateCache(noopUpdate)()
	defer withSystemScanLock(func() (func(), bool) { return nil, true })()
	defer withSecurityCheck(func(string, string, string) ([]security.Vulnerability, error) {
		return nil, errors.New("check failed")
	})()

	c := make(cache.Cache)
	cache.Set(c, cache.Key("npm", "lodash", "4.17.21"))
	defer withLoadCache(func() cache.Cache { return c })()

	RunSystemScan()

	if savedStats.Crit != 0 || savedStats.Warn != 0 {
		t.Errorf("expected no vulns when check errors, Crit=%d Warn=%d", savedStats.Crit, savedStats.Warn)
	}
	if savedStats.Errors != 1 {
		t.Errorf("expected Errors=1, got %d", savedStats.Errors)
	}
	if savedStats.Total != 1 {
		t.Errorf("expected Total=1, got %d", savedStats.Total)
	}
}

func TestRunSystemScanUsesBatch(t *testing.T) {
	batchCalls := 0
	defer withSaveSystemStats(func(SystemStats) {})()
	defer withUpdateCache(noopUpdate)()
	defer withSystemScanLock(func() (func(), bool) { return nil, true })()
	defer withSecurityBatchCheck(func(queries []security.Query) ([][]security.Vulnerability, error) {
		batchCalls++
		return make([][]security.Vulnerability, len(queries)), nil
	})()

	c := make(cache.Cache)
	cache.Set(c, cache.Key("npm", "react", "18.0.0"))
	cache.Set(c, cache.Key("npm", "lodash", "4.17.21"))
	defer withLoadCache(func() cache.Cache { return c })()

	RunSystemScan()

	if batchCalls != 1 {
		t.Errorf("expected one OSV batch request, got %d", batchCalls)
	}
}

func TestRunBackgroundScanSecurityError(t *testing.T) {
	var savedStats SystemStats
	defer withSaveSystemStats(func(s SystemStats) { savedStats = s })()
	defer withLoadCache(emptyCache)()
	defer withSecurityCheck(func(string, string, string) ([]security.Vulnerability, error) {
		return nil, errors.New("check failed")
	})()
	defer withReadManifest(func(*manager.Manager) []string {
		return []string{"lodash@4.17.21"}
	})()

	mgr := &manager.Manager{Name: "npm", Ecosystem: "npm"}
	RunBackgroundScan(mgr)

	if savedStats.Errors != 1 || savedStats.Warn != 0 {
		t.Errorf("expected Errors=1 Warn=0, got Warn=%d Errors=%d", savedStats.Warn, savedStats.Errors)
	}
	if savedStats.Total != 1 {
		t.Errorf("expected Total=1, got %d", savedStats.Total)
	}
}

func TestRunSystemScanWarn(t *testing.T) {
	var savedStats SystemStats
	defer withSaveSystemStats(func(s SystemStats) { savedStats = s })()
	defer withUpdateCache(noopUpdate)()
	defer withSystemScanLock(func() (func(), bool) { return nil, true })()
	defer withSecurityCheck(func(string, string, string) ([]security.Vulnerability, error) {
		return []security.Vulnerability{{ID: "CVE-1234", Severity: "MEDIUM"}}, nil
	})()

	c := make(cache.Cache)
	cache.Set(c, cache.Key("npm", "lodash", "4.17.21"))
	defer withLoadCache(func() cache.Cache { return c })()

	RunSystemScan()

	if savedStats.Warn != 1 {
		t.Errorf("expected Warn=1, got %d", savedStats.Warn)
	}
}

func TestRunSystemScanRefreshesCleanEntries(t *testing.T) {
	defer withSaveSystemStats(func(SystemStats) {})()
	defer withSystemScanLock(func() (func(), bool) { return nil, true })()
	defer withSecurityCheck(func(string, string, string) ([]security.Vulnerability, error) {
		return nil, nil
	})()

	c := make(cache.Cache)
	key := cache.Key("npm", "lodash", "4.17.21")
	c[key] = cache.Entry{Version: "4.17.21", CheckedAt: time.Now().Add(-48 * time.Hour)}
	defer withLoadCache(func() cache.Cache { return c })()
	defer withUpdateCache(func(apply func(cache.Cache)) { apply(c) })()

	RunSystemScan()

	if !cache.Hit(c, key) {
		t.Error("expected clean system scan to refresh cache TTL")
	}
}

func TestRunSystemScanSkipsBadKey(t *testing.T) {
	var savedStats SystemStats
	defer withSaveSystemStats(func(s SystemStats) { savedStats = s })()
	defer withUpdateCache(noopUpdate)()
	defer withSystemScanLock(func() (func(), bool) { return nil, true })()

	c := make(cache.Cache)
	c["noslash"] = cache.Entry{Version: "1.0.0"}
	defer withLoadCache(func() cache.Cache { return c })()

	RunSystemScan()

	if savedStats.Crit != 0 || savedStats.Warn != 0 {
		t.Errorf("expected no vulns for skipped key, Crit=%d Warn=%d", savedStats.Crit, savedStats.Warn)
	}
}

func TestRunSystemScanNilManager(t *testing.T) {
	var savedStats SystemStats
	defer withSaveSystemStats(func(s SystemStats) { savedStats = s })()
	defer withUpdateCache(noopUpdate)()
	defer withSystemScanLock(func() (func(), bool) { return nil, true })()
	defer withSecurityCheck(func(string, string, string) ([]security.Vulnerability, error) {
		return nil, nil
	})()

	c := make(cache.Cache)
	cache.Set(c, cache.Key("PyPI", "requests", "2.31.0"))
	defer withLoadCache(func() cache.Cache { return c })()

	RunSystemScan()

	if savedStats.Total != 1 {
		t.Errorf("expected Total=1, got %d", savedStats.Total)
	}
}

func TestScanPackageSecurityError(t *testing.T) {
	defer withSecurityCheck(func(eco, name, ver string) ([]security.Vulnerability, error) {
		return nil, errors.New("security check failed")
	})()

	r := scanSingleResult(npmMgr(), "react@18.0.0", make(cache.Cache), true)
	if r.err == nil {
		t.Error("expected error from security check in scanPackage")
	}
}

func TestIsExactVersionPyPI(t *testing.T) {
	if !isExactVersion("PyPI", "1.0.0") {
		t.Error("expected exact PyPI version")
	}
}

func TestIsExactVersionUnknownEcosystem(t *testing.T) {
	if isExactVersion("custom", "1.0.0") {
		t.Error("expected unknown ecosystem versions to remain unverified")
	}
	if isExactVersion("Homebrew", "1.0.0") {
		t.Error("expected Homebrew versions to remain unverified")
	}
}

func TestIsExactVersionEmpty(t *testing.T) {
	if isExactVersion("npm", "") {
		t.Error("expected false for empty version")
	}
}

func TestCanResolveConstraintNonNpm(t *testing.T) {
	if canResolveConstraint("pypi", "^1.0.0") {
		t.Error("expected false for non-npm ecosystem")
	}
}

func TestCanResolveConstraintEmpty(t *testing.T) {
	if canResolveConstraint("npm", "") {
		t.Error("expected false for empty version")
	}
}

func TestCanResolveConstraintSpecialPrefixes(t *testing.T) {
	prefixes := []string{
		"file:/path", "git+https://github.com/foo/bar", "github:foo/bar",
		"workspace:*", "catalog:default", "link:/path", "portal:/path", "patch:pkg", "npm:pkg",
		"http://example.com/pkg.tgz", "https://example.com/pkg.tgz",
		"package.tgz", "user/repository",
	}
	for _, v := range prefixes {
		if canResolveConstraint("npm", v) {
			t.Errorf("expected false for prefix %q", v)
		}
	}
}

func TestCanResolveConstraintPathPrefixes(t *testing.T) {
	for _, v := range []string{"./local", "../sibling", "/absolute"} {
		if canResolveConstraint("npm", v) {
			t.Errorf("expected false for path %q", v)
		}
	}
}

func TestCanResolveConstraintSemverRange(t *testing.T) {
	if !canResolveConstraint("npm", "^1.0.0") {
		t.Error("expected true for semver range ^1.0.0")
	}
	if !canResolveConstraint("npm", "~1.0.0") {
		t.Error("expected true for semver range ~1.0.0")
	}
}

func TestResolveScanVersionCargoRequirement(t *testing.T) {
	target := ""
	defer withResolveVersion(func(_ *manager.Manager, pkg string) (string, error) {
		target = pkg
		return "1.8.0", nil
	})()

	version, _, resolved, exact, err := resolveScanVersion(cargoMgr(), "serde", "^1.0", true)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if target != "serde@^1.0" || version != "1.8.0" || !resolved || !exact {
		t.Errorf("unexpected Cargo resolution: target=%q version=%q resolved=%v exact=%v", target, version, resolved, exact)
	}
}

func TestResolveScanVersionEmptyNoAllow(t *testing.T) {
	_, label, updated, exact, err := resolveScanVersion(npmMgr(), "react", "", false)
	if !errors.Is(err, errMissingVersion) || updated || exact {
		t.Errorf("expected skip: label=%q updated=%v exact=%v err=%v", label, updated, exact, err)
	}
}

func TestResolveScanVersionConstraintEmptyResolved(t *testing.T) {
	defer withResolveVersion(func(*manager.Manager, string) (string, error) {
		return "", nil
	})()
	_, _, updated, exact, err := resolveScanVersion(npmMgr(), "react", "^18.0.0", false)
	if !errors.Is(err, errMissingVersion) {
		t.Fatalf("expected missing-version error, got: %v", err)
	}
	if !updated {
		t.Error("expected updated=true when constraint resolves to empty")
	}
	if exact {
		t.Error("expected exact=false when resolved is empty")
	}
}

func TestResolveScanVersionDefault(t *testing.T) {
	_, _, updated, exact, err := resolveScanVersion(npmMgr(), "react", "file:/local/pkg", false)
	if !errors.Is(err, errMissingVersion) || updated || exact {
		t.Errorf("expected default skip: updated=%v exact=%v err=%v", updated, exact, err)
	}
}

func TestScanPackageParsesPyPIExtrasAndExactVersion(t *testing.T) {
	var checkedName, checkedVersion string
	defer withSecurityCheck(func(_ string, name, version string) ([]security.Vulnerability, error) {
		checkedName = name
		checkedVersion = version
		return nil, nil
	})()

	result := scanSingleResult(pipMgr(), "requests[socks]==2.19.0", make(cache.Cache), false)
	if result.err != nil {
		t.Fatalf("unexpected error: %v", result.err)
	}
	if checkedName != "requests" || checkedVersion != "2.19.0" {
		t.Errorf("expected requests 2.19.0, got %q %q", checkedName, checkedVersion)
	}
}

func TestScanPackageRejectsUnresolvedPyPIConstraint(t *testing.T) {
	securityCalled := false
	defer withSecurityCheck(func(string, string, string) ([]security.Vulnerability, error) {
		securityCalled = true
		return nil, nil
	})()

	result := scanSingleResult(pipMgr(), "urllib3<1.26", make(cache.Cache), false)
	if !errors.Is(result.err, errMissingVersion) {
		t.Fatalf("expected missing version error, got %v", result.err)
	}
	if securityCalled {
		t.Error("expected unresolved requirement to fail before querying OSV")
	}
}

func TestScanAllPostResolveCacheHit(t *testing.T) {
	defer withResolveVersion(func(*manager.Manager, string) (string, error) {
		return "18.0.0", nil
	})()
	defer withSecurityCheck(func(string, string, string) ([]security.Vulnerability, error) {
		return nil, nil
	})()

	c := make(cache.Cache)
	cache.Set(c, cache.Key("npm", "react", "18.0.0"))

	results := scanAllWithPolicy(npmMgr(), []string{"react"}, c, true)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !results[0].cached {
		t.Error("expected cached=true for post-resolve cache hit")
	}
}

func TestScanAllResolvesNPMSemverConstraint(t *testing.T) {
	resolveArg := ""
	defer withResolveVersion(func(_ *manager.Manager, pkg string) (string, error) {
		resolveArg = pkg
		return "18.2.0", nil
	})()
	defer withSecurityCheck(func(_, _, ver string) ([]security.Vulnerability, error) {
		if ver != "18.2.0" {
			t.Errorf("expected resolved npm version 18.2.0, got %q", ver)
		}
		return nil, nil
	})()

	results := scanAllWithPolicy(npmMgr(), []string{"react@^18.0.0"}, make(cache.Cache), false)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if resolveArg != "react@^18.0.0" {
		t.Errorf("expected range-aware resolution for semver constraint, got %q", resolveArg)
	}
	if results[0].version != "18.2.0" || !results[0].cacheable {
		t.Errorf("expected cacheable resolved result, got %+v", results[0])
	}
}

func TestScanAllIgnoresNonExactCacheHit(t *testing.T) {
	resolveArg := ""
	securityCalled := false
	defer withResolveVersion(func(_ *manager.Manager, pkg string) (string, error) {
		resolveArg = pkg
		return "18.2.0", nil
	})()
	defer withSecurityCheck(func(_, _, ver string) ([]security.Vulnerability, error) {
		securityCalled = true
		if ver != "18.2.0" {
			t.Errorf("expected resolved npm version 18.2.0, got %q", ver)
		}
		return nil, nil
	})()

	c := make(cache.Cache)
	cache.Set(c, cache.Key("npm", "react", "^18.0.0"))
	results := scanAllWithPolicy(npmMgr(), []string{"react@^18.0.0"}, c, false)

	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if resolveArg != "react@^18.0.0" {
		t.Errorf("expected range-aware resolution for semver constraint, got %q", resolveArg)
	}
	if !securityCalled {
		t.Error("expected security check after resolving non-exact cached spec")
	}
	if results[0].cached {
		t.Errorf("expected non-exact cache entry to be ignored, got %+v", results[0])
	}
}

func TestScanPackageWithoutVersionDoesNotResolveWhenDisabled(t *testing.T) {
	resolveCalled := false
	securityCalled := false

	defer withResolveVersion(func(*manager.Manager, string) (string, error) {
		resolveCalled = true
		return "18.0.0", nil
	})()
	defer withSecurityCheck(func(_, _, ver string) ([]security.Vulnerability, error) {
		securityCalled = true
		return nil, nil
	})()

	c := make(cache.Cache)
	r := scanSingleResult(npmMgr(), "react", c, false)

	if resolveCalled {
		t.Error("expected disabled missing-version resolution")
	}
	if securityCalled {
		t.Error("expected missing-version package to skip security check")
	}
	if !errors.Is(r.err, errMissingVersion) || r.version != "" || r.cacheable {
		t.Errorf("expected non-cacheable generic result, got %+v", r)
	}
	if len(c) != 0 {
		t.Errorf("expected cache to remain empty, got %v", c)
	}
}

func TestRunSystemScanSkipsWhenLocked(t *testing.T) {
	withObsDir(t)
	called := false
	defer withSystemScanLock(func() (func(), bool) { return nil, false })()
	defer withSaveSystemStats(func(SystemStats) { called = true })()

	RunSystemScan()

	if called {
		t.Error("expected locked system scan to skip work")
	}
	skipped := requireObsEvent(t, "pre.system_scan.skipped")
	if skipped["reason"] != "locked" {
		t.Fatalf("unexpected locked skip event: %#v", skipped)
	}
}

func TestRunSystemScanWithRelease(t *testing.T) {
	released := false
	defer withSaveSystemStats(func(SystemStats) {})()
	defer withUpdateCache(noopUpdate)()
	defer withSystemScanLock(func() (func(), bool) {
		return func() { released = true }, true
	})()
	defer withLoadCache(emptyCache)()

	RunSystemScan()

	if !released {
		t.Error("expected release to be called")
	}
}

func TestRunSystemScanLegacyKeyMigrated(t *testing.T) {
	var savedStats SystemStats
	defer withSaveSystemStats(func(s SystemStats) { savedStats = s })()
	defer withSystemScanLock(func() (func(), bool) { return nil, true })()
	defer withSecurityCheck(func(string, string, string) ([]security.Vulnerability, error) {
		return nil, nil
	})()

	c := make(cache.Cache)
	c["npm/lodash"] = cache.Entry{Version: "4.17.21"}
	defer withLoadCache(func() cache.Cache { return c })()

	var updated cache.Cache
	defer withUpdateCache(func(fn func(cache.Cache)) {
		cur := make(cache.Cache)
		fn(cur)
		updated = cur
	})()

	RunSystemScan()

	if savedStats.Total != 1 {
		t.Errorf("expected Total=1, got %d", savedStats.Total)
	}
	canonicalKey := cache.Key("npm", "lodash", "4.17.21")
	if !cache.Hit(updated, canonicalKey) {
		t.Errorf("expected canonical key %q in updated cache", canonicalKey)
	}
}

func TestRunSystemScanVersionFromEntry(t *testing.T) {
	var savedStats SystemStats
	defer withSaveSystemStats(func(s SystemStats) { savedStats = s })()
	defer withUpdateCache(noopUpdate)()
	defer withSystemScanLock(func() (func(), bool) { return nil, true })()
	defer withSecurityCheck(func(string, string, string) ([]security.Vulnerability, error) {
		return nil, nil
	})()

	c := make(cache.Cache)
	c["npm/lodash"] = cache.Entry{Version: "4.17.21"}
	defer withLoadCache(func() cache.Cache { return c })()

	RunSystemScan()

	if savedStats.Total != 1 {
		t.Errorf("expected Total=1, got %d", savedStats.Total)
	}
}

func TestSystemScanLockPath(t *testing.T) {
	dir := t.TempDir()
	defer withStatsCacheDir(dir)()

	path, err := systemScanLockPath()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != filepath.Join(dir, "pre", "system.lock") {
		t.Errorf("unexpected path: %s", path)
	}
}

func TestSystemScanLockPathError(t *testing.T) {
	orig := statsCacheDirFn
	statsCacheDirFn = func() (string, error) { return "", errors.New("no dir") }
	defer func() { statsCacheDirFn = orig }()

	_, err := systemScanLockPath()
	if err == nil {
		t.Error("expected error")
	}
}

func TestTryAcquireSystemScanLock(t *testing.T) {
	dir := t.TempDir()
	defer withStatsCacheDir(dir)()

	release, ok := tryAcquireSystemScanLock()
	if !ok {
		t.Fatal("expected ok=true for fresh lock")
	}
	if release == nil {
		t.Fatal("expected non-nil release function")
	}

	_, ok2 := tryAcquireSystemScanLock()
	if ok2 {
		t.Error("expected ok=false when lock is already held")
	}

	release()
}

func TestTryAcquireSystemScanLockPathError(t *testing.T) {
	orig := statsCacheDirFn
	statsCacheDirFn = func() (string, error) { return "", errors.New("no dir") }
	defer func() { statsCacheDirFn = orig }()

	_, ok := tryAcquireSystemScanLock()
	if !ok {
		t.Error("expected ok=true (fail-open) when path resolution fails")
	}
}

func TestTryAcquireSystemScanLockMkdirError(t *testing.T) {
	orig := statsCacheDirFn
	statsCacheDirFn = func() (string, error) { return "/dev/null", nil }
	defer func() { statsCacheDirFn = orig }()

	_, ok := tryAcquireSystemScanLock()
	if !ok {
		t.Error("expected ok=true (fail-open) when mkdir fails")
	}
}

func TestTryAcquireSystemScanLockStaleLock(t *testing.T) {
	dir := t.TempDir()
	defer withStatsCacheDir(dir)()

	lockDir := filepath.Join(dir, "pre")
	if err := os.MkdirAll(lockDir, 0755); err != nil {
		t.Fatal(err)
	}
	lockPath := filepath.Join(lockDir, "system.lock")
	if err := os.WriteFile(lockPath, []byte("stale"), 0644); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-2 * systemScanLockStaleAfter)
	if err := os.Chtimes(lockPath, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}

	release, ok := tryAcquireSystemScanLock()
	if !ok {
		t.Fatal("expected ok=true after evicting stale lock")
	}
	if release == nil {
		t.Fatal("expected non-nil release")
	}
	release()
}
