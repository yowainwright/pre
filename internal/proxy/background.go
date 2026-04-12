package proxy

import (
	"os"
	"os/exec"
	"strings"

	"github.com/yowainwright/pre/internal/cache"
	"github.com/yowainwright/pre/internal/manager"
)

var (
	systemScanEnabled     bool
	spawnBackgroundScanFn = spawnBackgroundScan
)

func SetSystemScanEnabled(v bool) {
	systemScanEnabled = v
}

func spawnBackgroundScan(mgrName string) {
	self, err := os.Executable()
	if err != nil {
		return
	}
	cmd := exec.Command(self, "scan", mgrName)
	cmd.Start()
}

func spawnSystemScan() {
	self, err := os.Executable()
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
	saveSystemStatsFn(SystemStats{Crit: crit, Warn: warn, Total: len(packages)})
}

func RunSystemScan() {
	c := loadCacheFn()
	var crit, warn int
	for key, entry := range c {
		ecosystem, name := cache.ParseKey(key)
		if ecosystem == "" || name == "" {
			continue
		}
		mgr := manager.Get(strings.ToLower(ecosystem))
		if mgr == nil {
			mgr = &manager.Manager{Name: ecosystem, Ecosystem: ecosystem}
		}
		vulns, err := securityCheckFn(mgr.Ecosystem, name, entry.Version)
		if err != nil {
			continue
		}
		r := scanResult{name: name, version: entry.Version, vulns: vulns}
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
	saveSystemStatsFn(SystemStats{Crit: crit, Warn: warn, Total: len(c)})
}
