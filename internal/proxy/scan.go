package proxy

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/yowainwright/pre/internal/cache"
	"github.com/yowainwright/pre/internal/manager"
	"github.com/yowainwright/pre/internal/security"
)

type scanResult struct {
	name    string
	version string
	label   string
	vulns   []security.Vulnerability
	err     error
	cached  bool
	updated bool
}

var (
	systemScanEnabled     bool
	spawnBackgroundScanFn = spawnBackgroundScan
	executableFn          = os.Executable
	acquireSystemScanLock = tryAcquireSystemScanLock
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
	var crit, warn int
	for _, pkg := range packages {
		r := scanPackage(mgr, pkg, c)
		switch {
		case hasCriticalVulns(r):
			crit++
		case len(r.vulns) > 0 || r.err != nil:
			warn++
		}
	}
	saveCacheFn(c)
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
		switch {
		case hasCriticalVulns(r):
			crit++
			delete(c, key)
		case len(vulns) > 0:
			warn++
			delete(c, key)
		}
	}
	saveCacheFn(c)
	saveSystemStatsFn(SystemStats{Crit: crit, Warn: warn, Total: total})
}

func scanAll(mgr *manager.Manager, packages []string, c cache.Cache) []scanResult {
	type work struct {
		idx     int
		name    string
		version string
	}

	results := make([]scanResult, len(packages))
	var pending []work

	for i, pkg := range packages {
		name, version := manager.ParseSpec(mgr.Ecosystem, pkg)
		if version != "" {
			label := name + "@" + version
			if cache.Hit(c, cache.Key(mgr.Ecosystem, name, version)) {
				results[i] = scanResult{name: name, version: version, label: label, cached: true}
				continue
			}
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

			version := w.version
			resolved := version == ""
			if resolved {
				var err error
				version, err = resolveVersionFn(mgr, w.name)
				if err != nil {
					mu.Lock()
					results[w.idx] = scanResult{name: w.name, label: w.name, err: err}
					mu.Unlock()
					return
				}
				if cache.Hit(c, cache.Key(mgr.Ecosystem, w.name, version)) {
					label := w.name + "@" + version
					mu.Lock()
					results[w.idx] = scanResult{name: w.name, version: version, label: label, cached: true}
					mu.Unlock()
					return
				}
			}

			label := w.name
			if version != "" {
				label = w.name + "@" + version
			}
			vulns, err := securityCheckFn(mgr.Ecosystem, w.name, version)
			mu.Lock()
			results[w.idx] = scanResult{
				name: w.name, version: version, label: label,
				vulns: vulns, err: err, updated: resolved,
			}
			mu.Unlock()
		}(w)
	}

	wg.Wait()
	return results
}

func scanPackage(mgr *manager.Manager, spec string, c cache.Cache) scanResult {
	name, version := manager.ParseSpec(mgr.Ecosystem, spec)

	resolved := version == ""
	if resolved {
		var err error
		version, err = resolveVersionFn(mgr, name)
		if err != nil {
			return scanResult{name: name, label: name, err: err}
		}
	}

	label := name
	if version != "" {
		label = name + "@" + version
	}

	key := cache.Key(mgr.Ecosystem, name, version)
	if version != "" && cache.Hit(c, key) {
		return scanResult{name: name, version: version, label: label, cached: true}
	}

	vulns, err := securityCheckFn(mgr.Ecosystem, name, version)
	if err != nil {
		return scanResult{name: name, version: version, label: label, err: err}
	}

	if len(vulns) == 0 && version != "" {
		cache.Set(c, key)
	}

	return scanResult{name: name, version: version, label: label, vulns: vulns, updated: resolved}
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
