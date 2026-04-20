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
	defer withExecutableFn(func() (string, error) { return "", errors.New("no exec") })()
	spawnBackgroundScan("npm")
}

func TestSpawnSystemScan(t *testing.T) {
	spawnSystemScan()
}

func TestSpawnSystemScanError(t *testing.T) {
	defer withExecutableFn(func() (string, error) { return "", errors.New("no exec") })()
	spawnSystemScan()
}

func TestRunBackgroundScanEmpty(t *testing.T) {
	defer withReadManifest(func(*manager.Manager) []string { return nil })()

	mgr := &manager.Manager{Name: "npm", Ecosystem: "npm"}
	RunBackgroundScan(mgr)
}

func TestRunBackgroundScan(t *testing.T) {
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
		t.Error("expected background scan to persist cache")
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
	var savedStats SystemStats
	defer withSaveSystemStats(func(s SystemStats) { savedStats = s })()
	defer withUpdateCache(noopUpdate)()
	defer withSystemScanLock(func() (func(), bool) { return nil, true })()
	defer withSecurityCheck(func(string, string, string) ([]security.Vulnerability, error) {
		return nil, nil
	})()

	c := make(cache.Cache)
	cache.Set(c, cache.Key("npm", "lodash", "4.17.21"))
	defer withLoadCache(func() cache.Cache { return c })()

	RunSystemScan()

	if savedStats.Total != 1 {
		t.Errorf("expected Total=1, got %d", savedStats.Total)
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

	r := scanPackage(npmMgr(), "react@18.0.0", make(cache.Cache))
	if r.err == nil {
		t.Error("expected error from security check in scanPackage")
	}
}

func TestIsExactVersionNonNpm(t *testing.T) {
	if !isExactVersion("pypi", "1.0.0") {
		t.Error("expected true for non-npm ecosystem with non-empty version")
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
		"workspace:*", "link:/path", "npm:pkg",
		"http://example.com/pkg.tgz", "https://example.com/pkg.tgz",
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

func TestResolveScanVersionEmptyNoAllow(t *testing.T) {
	_, label, updated, exact, err := resolveScanVersion(npmMgr(), "react", "", false)
	if err != nil || updated || exact {
		t.Errorf("expected skip: label=%q updated=%v exact=%v err=%v", label, updated, exact, err)
	}
}

func TestResolveScanVersionConstraintEmptyResolved(t *testing.T) {
	defer withResolveVersion(func(*manager.Manager, string) (string, error) {
		return "", nil
	})()
	_, _, updated, exact, err := resolveScanVersion(npmMgr(), "react", "^18.0.0", false)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
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
	if err != nil || updated || exact {
		t.Errorf("expected default skip: updated=%v exact=%v err=%v", updated, exact, err)
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

	results := scanAll(npmMgr(), []string{"react"}, c)

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
		t.Errorf("expected constraint-aware resolution, got %q", resolveArg)
	}
	if results[0].version != "18.2.0" || !results[0].cacheable {
		t.Errorf("expected cacheable resolved result, got %+v", results[0])
	}
}

func TestScanPackageWithoutVersionDoesNotResolveWhenDisabled(t *testing.T) {
	resolveCalled := false
	checkedVersion := "unset"

	defer withResolveVersion(func(*manager.Manager, string) (string, error) {
		resolveCalled = true
		return "18.0.0", nil
	})()
	defer withSecurityCheck(func(_, _, ver string) ([]security.Vulnerability, error) {
		checkedVersion = ver
		return nil, nil
	})()

	c := make(cache.Cache)
	r := scanPackageWithPolicy(npmMgr(), "react", c, false)

	if resolveCalled {
		t.Error("expected disabled missing-version resolution")
	}
	if checkedVersion != "" {
		t.Errorf("expected generic package check without a guessed version, got %q", checkedVersion)
	}
	if r.version != "" || r.cacheable {
		t.Errorf("expected non-cacheable generic result, got %+v", r)
	}
	if len(c) != 0 {
		t.Errorf("expected cache to remain empty, got %v", c)
	}
}

func TestRunSystemScanSkipsWhenLocked(t *testing.T) {
	called := false
	defer withSystemScanLock(func() (func(), bool) { return nil, false })()
	defer withSaveSystemStats(func(SystemStats) { called = true })()

	RunSystemScan()

	if called {
		t.Error("expected locked system scan to skip work")
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
