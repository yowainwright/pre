package proxy

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/yowainwright/pre/internal/cache"
	"github.com/yowainwright/pre/internal/manager"
	"github.com/yowainwright/pre/internal/obs"
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
	acquireSystemScanLock = tryAcquireSystemScanLock
)

var (
	npmExactVersionRE   = regexp.MustCompile(`^v?\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)
	goExactVersionRE    = regexp.MustCompile(`^v\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)
	crateExactVersionRE = regexp.MustCompile(`^\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)
)

const systemScanLockStaleAfter = 30 * time.Minute

var errMissingVersion = errors.New("version unavailable; skipped vulnerability check")

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

func RunSystemScan() {
	start := time.Now()
	obs.Record("pre.system_scan.started", nil)
	if disableEnabled() {
		recordSystemScanSkipped("env_disabled", start)
		return
	}
	release, ok := acquireSystemScanLock()
	if !ok {
		recordSystemScanSkipped("locked", start)
		return
	}
	if release != nil {
		defer release()
	}

	runSystemScanLocked(start)
}

func runSystemScanLocked(start time.Time) {
	c := loadCacheFn()
	if limit, exceeded := packageLimitExceeded(len(c)); exceeded {
		obs.Record("pre.system_scan.skipped", map[string]any{
			"reason":        "package_limit",
			"package_count": len(c),
			"package_limit": limit,
			"duration_ms":   durationMillis(start),
		})
		return
	}
	pending, total := pendingSystemScans(c)
	results, err := checkSystemScans(pending)
	stats, changes := systemScanOutcome(pending, results, err)
	stats.Total = total
	applySystemScanChanges(changes)
	saveSystemStatsFn(stats)
	recordSystemScanCompleted(start, stats, len(pending))
}

func recordSystemScanCompleted(start time.Time, stats SystemStats, pending int) {
	obs.Record("pre.system_scan.completed", map[string]any{
		"package_count":  stats.Total,
		"pending_count":  pending,
		"critical_count": stats.Crit,
		"warning_count":  stats.Warn,
		"error_count":    stats.Errors,
		"duration_ms":    durationMillis(start),
	})
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
	skipScan := result.err != nil || result.cached
	if skipScan {
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
	vulnerabilities, err := securityBatchCheckFn(queries)
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
		pending = append(pending, work)
	}
	return pending, total
}

func systemScanWorkFrom(key string, entry cache.Entry) (systemScanWork, bool) {
	ecosystem, name, version := cache.ParseKey(key)
	if version == "" {
		version = entry.Version
	}
	missingIdentity := ecosystem == "" || name == ""
	missingVersion := version == ""
	incomplete := missingIdentity || missingVersion
	if incomplete {
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
	return securityBatchCheckFn(queries)
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
	noChanges := len(changes.deleteKeys) == 0 && len(changes.refreshKeys) == 0
	if noChanges {
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
	if version != "" {
		return resolveRequestedVersion(mgr, name, version)
	}
	if !allowMissingVersionResolution {
		return "", name, false, false, errMissingVersion
	}
	return resolveScanTarget(mgr, name, name, name, name)
}

func resolveRequestedVersion(mgr *manager.Manager, name, version string) (string, string, bool, bool, error) {
	label := name + "@" + version
	if shouldResolveVersion(mgr.Ecosystem, version) {
		target := name
		useRequestedTag := mgr.Ecosystem == "npm" && strings.ToLower(version) != "latest"
		if useRequestedTag {
			target = label
		}
		return resolveScanTarget(mgr, name, target, label, name)
	}
	if isExactVersion(mgr.Ecosystem, version) {
		return version, label, false, true, nil
	}
	if !canResolveConstraint(mgr.Ecosystem, version) {
		return "", label, false, false, errMissingVersion
	}
	return resolveScanConstraint(mgr, name, label)
}

func resolveScanConstraint(mgr *manager.Manager, name, label string) (string, string, bool, bool, error) {
	target := name
	usesRequirement := mgr.Ecosystem == "npm" || mgr.Ecosystem == "crates.io"
	if usesRequirement {
		target = label
	}
	return resolveScanTarget(mgr, name, target, label, label)
}

func resolveScanTarget(mgr *manager.Manager, name, target, errorLabel, emptyLabel string) (string, string, bool, bool, error) {
	resolved, err := resolveVersionFn(mgr, target)
	if err != nil {
		return "", errorLabel, false, false, err
	}
	if resolved == "" {
		return "", emptyLabel, true, false, errMissingVersion
	}
	label := name + "@" + resolved
	return resolved, label, true, isExactVersion(mgr.Ecosystem, resolved), nil
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
	resolvable := manager.IsSupportedNPMRegistrySpec(version) && !isExactVersion(ecosystem, version)
	return resolvable
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

func recordSystemScanSkipped(reason string, start time.Time) {
	obs.Record("pre.system_scan.skipped", map[string]any{"reason": reason, "duration_ms": durationMillis(start)})
}
