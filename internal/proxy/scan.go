package proxy

import (
	"errors"
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

var (
	systemScanEnabled     bool
	spawnBackgroundScanFn = spawnBackgroundScan
	spawnSystemScanFn     = spawnSystemScan
	executableFn          = os.Executable
	acquireSystemScanLock = tryAcquireSystemScanLock
)

var (
	npmExactVersionRE = regexp.MustCompile(`^v?\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)
	goExactVersionRE  = regexp.MustCompile(`^v\d+\.\d+\.\d+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)
)

const systemScanLockStaleAfter = 30 * time.Minute

func SetSystemScanEnabled(v bool) {
	systemScanEnabled = v
}

func spawnBackgroundScan(mgrName string) {
	self, err := executableFn()
	if err != nil {
		return
	}
	cmd := exec.Command(self, "scan", mgrName)
	cmd.Start()
}

func spawnSystemScan() {
	self, err := executableFn()
	if err != nil {
		return
	}
	cmd := exec.Command(self, "scan", "system")
	cmd.Start()
}

func RunBackgroundScan(mgr *manager.Manager) {
	packages := readManifestFn(mgr)
	if len(packages) == 0 {
		return
	}
	c := loadCacheFn()
	fresh := make(cache.Cache)
	var crit, warn int
	for _, pkg := range packages {
		r := scanPackageWithPolicy(mgr, pkg, c, false)
		switch {
		case hasCriticalVulns(r):
			crit++
		case len(r.vulns) > 0 || r.err != nil:
			warn++
		}
		if len(r.vulns) == 0 && r.version != "" && r.err == nil && r.cacheable && !r.cached {
			cache.Set(fresh, cache.Key(mgr.Ecosystem, r.name, r.version))
		}
	}
	if len(fresh) > 0 {
		updateCacheFn(func(current cache.Cache) {
			for key := range fresh {
				cache.Set(current, key)
			}
		})
	}
	saveSystemStatsFn(SystemStats{Crit: crit, Warn: warn, Total: len(packages)})
}

func RunSystemScan() {
	release, ok := acquireSystemScanLock()
	if !ok {
		return
	}
	if release != nil {
		defer release()
	}

	c := loadCacheFn()
	total := 0
	var crit, warn int
	deleteKeys := make(map[string]struct{})
	refreshKeys := make(cache.Cache)
	for key, entry := range c {
		ecosystem, name, version := cache.ParseKey(key)
		if ecosystem == "" || name == "" {
			continue
		}
		if version == "" {
			version = entry.Version
		}
		if version == "" {
			continue
		}
		total++
		mgr := manager.Get(strings.ToLower(ecosystem))
		if mgr == nil {
			mgr = &manager.Manager{Name: ecosystem, Ecosystem: ecosystem}
		}
		vulns, err := securityCheckFn(mgr.Ecosystem, name, version)
		if err != nil {
			continue
		}
		r := scanResult{name: name, version: version, vulns: vulns}
		canonicalKey := cache.Key(mgr.Ecosystem, name, version)
		switch {
		case hasCriticalVulns(r):
			crit++
			deleteKeys[key] = struct{}{}
			deleteKeys[canonicalKey] = struct{}{}
		case len(vulns) > 0:
			warn++
			deleteKeys[key] = struct{}{}
			deleteKeys[canonicalKey] = struct{}{}
		default:
			cache.Set(refreshKeys, canonicalKey)
			if key != canonicalKey {
				deleteKeys[key] = struct{}{}
			}
		}
	}
	updateCacheFn(func(current cache.Cache) {
		for key := range deleteKeys {
			delete(current, key)
		}
		for key := range refreshKeys {
			cache.Set(current, key)
		}
	})
	saveSystemStatsFn(SystemStats{Crit: crit, Warn: warn, Total: total})
}

func scanAll(mgr *manager.Manager, packages []string, c cache.Cache) []scanResult {
	return scanAllWithPolicy(mgr, packages, c, true)
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

	sem := make(chan struct{}, 10)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, w := range pending {
		wg.Add(1)
		go func(w work) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			version, label, resolved, cacheable, err := resolveScanVersion(mgr, w.name, w.version, allowMissingVersionResolution)
			if err != nil {
				mu.Lock()
				results[w.idx] = scanResult{name: w.name, label: label, err: err}
				mu.Unlock()
				return
			}
			if cacheable && cache.Hit(c, cache.Key(mgr.Ecosystem, w.name, version)) {
				mu.Lock()
				results[w.idx] = scanResult{name: w.name, version: version, label: label, cached: true, cacheable: true}
				mu.Unlock()
				return
			}

			vulns, err := securityCheckFn(mgr.Ecosystem, w.name, version)
			mu.Lock()
			results[w.idx] = scanResult{
				name: w.name, version: version, label: label,
				vulns: vulns, err: err, updated: resolved, cacheable: cacheable,
			}
			mu.Unlock()
		}(w)
	}

	wg.Wait()
	return results
}

func scanPackage(mgr *manager.Manager, spec string, c cache.Cache) scanResult {
	return scanPackageWithPolicy(mgr, spec, c, true)
}

func scanPackageWithPolicy(mgr *manager.Manager, spec string, c cache.Cache, allowMissingVersionResolution bool) scanResult {
	name, version := manager.ParseSpec(mgr.Ecosystem, spec)
	version, label, resolved, cacheable, err := resolveScanVersion(mgr, name, version, allowMissingVersionResolution)
	if err != nil {
		return scanResult{name: name, label: label, err: err}
	}

	key := cache.Key(mgr.Ecosystem, name, version)
	if cacheable && cache.Hit(c, key) {
		return scanResult{name: name, version: version, label: label, cached: true, cacheable: true}
	}

	vulns, err := securityCheckFn(mgr.Ecosystem, name, version)
	if err != nil {
		return scanResult{name: name, version: version, label: label, err: err}
	}

	if len(vulns) == 0 && cacheable {
		cache.Set(c, key)
	}

	return scanResult{name: name, version: version, label: label, vulns: vulns, updated: resolved, cacheable: cacheable}
}

func tryAcquireSystemScanLock() (func(), bool) {
	path, err := systemScanLockPath()
	if err != nil {
		return nil, true
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return nil, true
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			if info, statErr := os.Stat(path); statErr == nil && time.Since(info.ModTime()) > systemScanLockStaleAfter {
				_ = os.Remove(path)
				return tryAcquireSystemScanLock()
			}
			return nil, false
		}
		return nil, true
	}
	_, _ = file.WriteString(time.Now().Format(time.RFC3339Nano))
	_ = file.Close()
	return func() { _ = os.Remove(path) }, true
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
			return "", label, false, false, nil
		}
		resolved, err := resolveVersionFn(mgr, name)
		if err != nil {
			return "", label, false, false, err
		}
		if resolved == "" {
			return "", name, true, false, nil
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
			return "", name, true, false, nil
		}
		return resolved, name + "@" + resolved, true, isExactVersion(mgr.Ecosystem, resolved), nil
	case isExactVersion(mgr.Ecosystem, version):
		return version, label, false, true, nil
	case canResolveConstraint(mgr.Ecosystem, version):
		resolved, err := resolveVersionFn(mgr, name+"@"+version)
		if err != nil {
			return "", label, false, false, err
		}
		if resolved == "" {
			return "", label, true, false, nil
		}
		return resolved, name + "@" + resolved, true, isExactVersion(mgr.Ecosystem, resolved), nil
	default:
		return "", label, false, false, nil
	}
}

func canResolveConstraint(ecosystem, version string) bool {
	if ecosystem != "npm" || version == "" {
		return false
	}
	for _, prefix := range []string{
		"file:", "git+", "github:", "workspace:", "link:", "npm:",
		"http://", "https://",
	} {
		if strings.HasPrefix(version, prefix) {
			return false
		}
	}
	return !strings.HasPrefix(version, "./") &&
		!strings.HasPrefix(version, "../") &&
		!strings.HasPrefix(version, "/") &&
		!isExactVersion(ecosystem, version)
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
	}
	return true
}
