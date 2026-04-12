package proxy

import (
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

	key := cache.Key(mgr.Ecosystem, name)
	if version != "" && cache.Hit(c, key, version) {
		return scanResult{name: name, version: version, label: label, cached: true}
	}

	vulns, err := securityCheckFn(mgr.Ecosystem, name, version)
	if err != nil {
		return scanResult{name: name, version: version, label: label, err: err}
	}

	if len(vulns) == 0 && version != "" {
		cache.Set(c, key, version)
	}

	return scanResult{name: name, version: version, label: label, vulns: vulns, updated: resolved}
}
