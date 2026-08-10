package proxy

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/yowainwright/pre/internal/cache"
	"github.com/yowainwright/pre/internal/manager"
	"github.com/yowainwright/pre/internal/security"
)

type scanResult struct {
	name      string
	version   string
	label     string
	vulns     []security.Vulnerability
	err       error
	cached    bool
	cacheable bool
	updated   bool
}

type batchScanWork struct {
	index int
	query security.Query
}

type systemScanWork struct {
	key          string
	canonicalKey string
	query        security.Query
}

type systemScanChanges struct {
	deleteKeys  map[string]struct{}
	refreshKeys cache.Cache
}

var (
	systemScanEnabled     bool
	spawnBackgroundScanFn = spawnBackgroundScan
	spawnSystemScanFn     = spawnSystemScan
	executableFn          = os.Executable
	acquireSystemScanLock = tryAcquireSystemScanLock
)

var (
	npmExactVersionRE   = regexp.MustCompile(`^v?\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)
	goExactVersionRE    = regexp.MustCompile(`^v\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)
	crateExactVersionRE = regexp.MustCompile(`^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)
)

const systemScanLockStaleAfter = 30 * time.Minute
const scanConcurrency = 10

var errMissingVersion = errors.New("version unavailable; skipped vulnerability check")

func SetSystemScanEnabled(v bool) {
	systemScanEnabled = v
}

func spawnBackgroundScan(mgrName string) {
	self, err := executableFn()
	if err != nil {
		return
	}
	cmd := exec.Command(self, "scan", mgrName) // #nosec G204 -- self is the current pre executable path.
	_ = cmd.Start()
}

func spawnSystemScan() {
	self, err := executableFn()
	if err != nil {
		return
	}
	cmd := exec.Command(self, "scan", "system") // #nosec G204 -- self is the current pre executable path.
	_ = cmd.Start()
}

func RunBackgroundScan(mgr *manager.Manager) {
	if disableEnabled() {
		return
	}
	packages, ok := backgroundScanPackages(mgr)
	if !ok {
		return
	}
	c := loadCacheFn()
	results := scanBatchWithPolicy(mgr, packages, c, false)
	stats, fresh := backgroundScanOutcome(mgr, results)
	stats.Total = len(packages)
	storeFreshScanResults(fresh)
	saveSystemStatsFn(stats)
}

func backgroundScanPackages(mgr *manager.Manager) ([]string, bool) {
	validationErr := validateManifestFn(mgr, ".")
	if validationErr != nil {
		saveSystemStatsFn(SystemStats{Errors: 1})
		return nil, false
	}
	packages := readManifestFn(mgr)
	if len(packages) == 0 {
		return nil, false
	}
	if _, exceeded := packageLimitExceeded(len(packages)); exceeded {
		return nil, false
	}
	return packages, true
}

func backgroundScanOutcome(mgr *manager.Manager, results []scanResult) (SystemStats, cache.Cache) {
	stats := SystemStats{}
	fresh := make(cache.Cache)
	for _, r := range results {
		switch {
		case hasCriticalVulns(r):
			stats.Crit++
		case len(r.vulns) > 0:
			stats.Warn++
		case r.err != nil:
			stats.Errors++
		}
		if shouldCacheScanResult(r) {
			cache.Set(fresh, cache.Key(mgr.Ecosystem, r.name, r.version))
		}
	}
	return stats, fresh
}

func storeFreshScanResults(fresh cache.Cache) {
	if len(fresh) == 0 {
		return
	}
	updateCacheFn(func(current cache.Cache) {
		for key := range fresh {
			cache.Set(current, key)
		}
	})
}

func shouldCacheScanResult(result scanResult) bool {
	isClean := len(result.vulns) == 0 && result.err == nil
	return isClean && result.version != "" && result.cacheable && !result.cached
}

func RunSystemScan() {
	if disableEnabled() {
		return
	}
	release, ok := acquireSystemScanLock()
	if !ok {
		return
	}
	if release != nil {
		defer release()
	}

	c := loadCacheFn()
	if _, exceeded := packageLimitExceeded(len(c)); exceeded {
		return
	}
	pending, total := pendingSystemScans(c)
	results, err := checkSystemScans(pending)
	stats, changes := systemScanOutcome(pending, results, err)
	stats.Total = total
	applySystemScanChanges(changes)
	saveSystemStatsFn(stats)
}

func scanBatchWithPolicy(mgr *manager.Manager, packages []string, c cache.Cache, allowMissingVersionResolution bool) []scanResult {
	results := make([]scanResult, len(packages))
	pending := make([]batchScanWork, 0, len(packages))
	for index, spec := range packages {
		result, query, scan := prepareBatchScan(mgr, spec, c, allowMissingVersionResolution)
		results[index] = result
		if scan {
			pending = append(pending, batchScanWork{index: index, query: query})
		}
	}
	applyBatchScanResults(results, pending)
	return results
}

func prepareBatchScan(mgr *manager.Manager, spec string, c cache.Cache, allowMissing bool) (scanResult, security.Query, bool) {
	name, requestedVersion := manager.ParseSpec(mgr.Ecosystem, spec)
	result := prepareScan(mgr, name, requestedVersion, c, allowMissing)
	if result.err != nil || result.cached {
		return result, security.Query{}, false
	}
	query := security.Query{Ecosystem: mgr.Ecosystem, Name: result.name, Version: result.version}
	return result, query, true
}

func applyBatchScanResults(results []scanResult, pending []batchScanWork) {
	if len(pending) == 0 {
		return
	}
	queries := make([]security.Query, len(pending))
	for index, work := range pending {
		queries[index] = work.query
	}
	vulnerabilities, err := checkBatchQueries(queries)
	for index, work := range pending {
		results[work.index].err = err
		if err == nil {
			results[work.index].vulns = vulnerabilities[index]
		}
	}
}

func pendingSystemScans(c cache.Cache) ([]systemScanWork, int) {
	pending := make([]systemScanWork, 0, len(c))
	total := 0
	for key, entry := range c {
		work, ok := systemScanWorkFrom(key, entry)
		if !ok {
			continue
		}
		total++
		if !cache.Hit(c, work.canonicalKey) {
			pending = append(pending, work)
		}
	}
	return pending, total
}

func systemScanWorkFrom(key string, entry cache.Entry) (systemScanWork, bool) {
	ecosystem, name, version := cache.ParseKey(key)
	if version == "" {
		version = entry.Version
	}
	if ecosystem == "" || name == "" || version == "" {
		return systemScanWork{}, false
	}
	mgr := manager.Get(strings.ToLower(ecosystem))
	if mgr != nil {
		ecosystem = mgr.Ecosystem
	}
	query := security.Query{Ecosystem: ecosystem, Name: name, Version: version}
	canonicalKey := cache.Key(ecosystem, name, version)
	return systemScanWork{key: key, canonicalKey: canonicalKey, query: query}, true
}

func checkSystemScans(pending []systemScanWork) ([][]security.Vulnerability, error) {
	if len(pending) == 0 {
		return nil, nil
	}
	queries := make([]security.Query, len(pending))
	for index, work := range pending {
		queries[index] = work.query
	}
	return checkBatchQueries(queries)
}

func checkBatchQueries(queries []security.Query) ([][]security.Vulnerability, error) {
	results, err := securityBatchCheckFn(queries)
	if err != nil {
		return nil, err
	}
	if len(results) != len(queries) {
		return nil, fmt.Errorf("expected %d batch results, got %d", len(queries), len(results))
	}
	return results, nil
}

func systemScanOutcome(pending []systemScanWork, results [][]security.Vulnerability, batchErr error) (SystemStats, systemScanChanges) {
	changes := systemScanChanges{deleteKeys: make(map[string]struct{}), refreshKeys: make(cache.Cache)}
	stats := SystemStats{}
	if batchErr != nil {
		stats.Errors = len(pending)
		return stats, changes
	}
	for index, work := range pending {
		applySystemScanResult(work, results[index], &stats, &changes)
	}
	return stats, changes
}

func applySystemScanResult(work systemScanWork, vulnerabilities []security.Vulnerability, stats *SystemStats, changes *systemScanChanges) {
	result := scanResult{vulns: vulnerabilities}
	if hasCriticalVulns(result) {
		stats.Crit++
		markSystemScanDeleted(work, changes)
		return
	}
	if len(vulnerabilities) > 0 {
		stats.Warn++
		markSystemScanDeleted(work, changes)
		return
	}
	cache.Set(changes.refreshKeys, work.canonicalKey)
	if work.key != work.canonicalKey {
		changes.deleteKeys[work.key] = struct{}{}
	}
}

func markSystemScanDeleted(work systemScanWork, changes *systemScanChanges) {
	changes.deleteKeys[work.key] = struct{}{}
	changes.deleteKeys[work.canonicalKey] = struct{}{}
}

func applySystemScanChanges(changes systemScanChanges) {
	if len(changes.deleteKeys) == 0 && len(changes.refreshKeys) == 0 {
		return
	}
	updateCacheFn(func(current cache.Cache) {
		for key := range changes.deleteKeys {
			delete(current, key)
		}
		for key := range changes.refreshKeys {
			cache.Set(current, key)
		}
	})
}

func scanAllWithPolicy(mgr *manager.Manager, packages []string, c cache.Cache, allowMissingVersionResolution bool) []scanResult {
	type work struct {
		idx     int
		name    string
		version string
	}

	results := make([]scanResult, len(packages))
	var pending []work

	for i, pkg := range packages {
		name, version := manager.ParseSpec(mgr.Ecosystem, pkg)
		if hasExactCacheHit(mgr, c, name, version) {
			label := name + "@" + version
			results[i] = scanResult{name: name, version: version, label: label, cached: true}
			continue
		}
		pending = append(pending, work{idx: i, name: name, version: version})
	}

	jobs := make(chan work)
	var wg sync.WaitGroup

	workers := scanConcurrency
	if len(pending) < workers {
		workers = len(pending)
	}
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for w := range jobs {
				results[w.idx] = scanPendingPackage(mgr, w.name, w.version, c, allowMissingVersionResolution)
			}
		}()
	}

	for _, w := range pending {
		jobs <- w
	}
	close(jobs)

	wg.Wait()
	return results
}

func scanPendingPackage(mgr *manager.Manager, name, version string, c cache.Cache, allowMissingVersionResolution bool) scanResult {
	result := prepareScan(mgr, name, version, c, allowMissingVersionResolution)
	if result.err != nil || result.cached {
		return result
	}
	vulns, err := securityCheckFn(mgr.Ecosystem, result.name, result.version)
	if err != nil {
		result.err = err
		return result
	}
	result.vulns = vulns
	return result
}

func prepareScan(mgr *manager.Manager, name, requestedVersion string, c cache.Cache, allowMissing bool) scanResult {
	version, label, updated, cacheable, err := resolveScanVersion(mgr, name, requestedVersion, allowMissing)
	result := scanResult{name: name, version: version, label: label, updated: updated, cacheable: cacheable, err: err}
	if err != nil {
		return result
	}
	key := cache.Key(mgr.Ecosystem, name, version)
	result.cached = cacheable && cache.Hit(c, key)
	return result
}

func tryAcquireSystemScanLock() (func(), bool) {
	path, err := systemScanLockPath()
	if err != nil {
		return nil, true
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return nil, true
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return handleSystemScanLockError(path, err)
	}
	_, _ = file.WriteString(time.Now().Format(time.RFC3339Nano))
	_ = file.Close()
	return func() { _ = os.Remove(path) }, true
}

func handleSystemScanLockError(path string, err error) (func(), bool) {
	if !errors.Is(err, os.ErrExist) {
		return nil, true
	}
	info, statErr := os.Stat(path)
	isStale := statErr == nil && time.Since(info.ModTime()) > systemScanLockStaleAfter
	if !isStale {
		return nil, false
	}
	_ = os.Remove(path)
	return tryAcquireSystemScanLock()
}

func systemScanLockPath() (string, error) {
	dir, err := statsCacheDirFn()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "pre", "system.lock"), nil
}

func resolveScanVersion(mgr *manager.Manager, name, version string, allowMissingVersionResolution bool) (string, string, bool, bool, error) {
	label := name
	if version != "" {
		label = name + "@" + version
	}

	switch {
	case version == "":
		if !allowMissingVersionResolution {
			return "", label, false, false, errMissingVersion
		}
		resolved, err := resolveVersionFn(mgr, name)
		if err != nil {
			return "", label, false, false, err
		}
		if resolved == "" {
			return "", name, true, false, errMissingVersion
		}
		return resolved, name + "@" + resolved, true, isExactVersion(mgr.Ecosystem, resolved), nil
	case shouldResolveVersion(mgr.Ecosystem, version):
		target := name
		if mgr.Ecosystem == "npm" && strings.ToLower(version) != "latest" {
			target = label
		}
		resolved, err := resolveVersionFn(mgr, target)
		if err != nil {
			return "", label, false, false, err
		}
		if resolved == "" {
			return "", name, true, false, errMissingVersion
		}
		return resolved, name + "@" + resolved, true, isExactVersion(mgr.Ecosystem, resolved), nil
	case isExactVersion(mgr.Ecosystem, version):
		return version, label, false, true, nil
	case canResolveConstraint(mgr.Ecosystem, version):
		target := name
		usesRequirement := mgr.Ecosystem == "npm" || mgr.Ecosystem == "crates.io"
		if usesRequirement {
			target = label
		}
		resolved, err := resolveVersionFn(mgr, target)
		if err != nil {
			return "", label, false, false, err
		}
		if resolved == "" {
			return "", label, true, false, errMissingVersion
		}
		return resolved, name + "@" + resolved, true, isExactVersion(mgr.Ecosystem, resolved), nil
	default:
		return "", label, false, false, errMissingVersion
	}
}

func canResolveConstraint(ecosystem, version string) bool {
	if version == "" {
		return false
	}
	if ecosystem == "crates.io" {
		return !isExactVersion(ecosystem, version)
	}
	if ecosystem != "npm" {
		return false
	}
	return manager.IsSupportedNPMRegistrySpec(version) && !isExactVersion(ecosystem, version)
}

func isExactVersion(ecosystem, version string) bool {
	if version == "" {
		return false
	}
	switch ecosystem {
	case "npm":
		return npmExactVersionRE.MatchString(version)
	case "Go":
		return goExactVersionRE.MatchString(version)
	case "PyPI":
		return !strings.ContainsAny(version, "<>=~*,@ ")
	case "crates.io":
		return crateExactVersionRE.MatchString(version)
	case "Homebrew":
		return false
	}
	return false
}
