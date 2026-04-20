package proxy

import (
	"strings"

	"github.com/yowainwright/pre/internal/manager"
)

func extractPackages(mgr *manager.Manager, args []string) []string {
	result := make([]string, 0, len(args))
	skipNext := false

	for _, arg := range args {
		if skipNext {
			skipNext = false
			continue
		}
		if arg == "--" {
			break
		}
		if packageFlagConsumesValue(mgr, arg) {
			if !strings.Contains(arg, "=") {
				skipNext = true
			}
			continue
		}
		if strings.HasPrefix(arg, "-") {
			continue
		}
		if isPackageArg(mgr, arg) {
			result = append(result, arg)
		}
	}

	return result
}

func packageFlagConsumesValue(mgr *manager.Manager, arg string) bool {
	flag := arg
	if idx := strings.Index(flag, "="); idx != -1 {
		flag = flag[:idx]
	}

	switch mgr.Name {
	case "npm", "pnpm", "bun":
		switch flag {
		case "--workspace", "-w", "--prefix", "--tag", "--registry", "--userconfig", "--cache", "--omit", "--include", "--install-strategy":
			return true
		}
	case "pip", "pip3", "uv", "poetry":
		switch flag {
		case "-r", "--requirement", "-c", "--constraint", "-i", "--index-url", "--extra-index-url", "-f", "--find-links", "--trusted-host", "--python", "--platform", "--target", "-e", "--editable":
			return true
		}
	case "go":
		switch flag {
		case "-modfile", "-overlay", "-pgo", "-asmflags", "-gcflags", "-ldflags", "-tags", "-toolexec", "-pkgdir":
			return true
		}
	}

	return false
}

func isPackageArg(mgr *manager.Manager, arg string) bool {
	if arg == "" {
		return false
	}
	switch {
	case strings.HasPrefix(arg, "."),
		strings.HasPrefix(arg, "/"),
		strings.HasPrefix(arg, "~/"),
		strings.HasPrefix(arg, "file:"),
		strings.HasPrefix(arg, "link:"),
		strings.HasPrefix(arg, "git+"),
		strings.HasPrefix(arg, "http://"),
		strings.HasPrefix(arg, "https://"):
		return false
	}

	if mgr.Ecosystem == "PyPI" {
		lower := strings.ToLower(arg)
		for _, suffix := range []string{".txt", ".whl", ".zip", ".egg", ".tar.gz", ".tgz"} {
			if strings.HasSuffix(lower, suffix) {
				return false
			}
		}
	}

	return true
}

func shouldResolveVersion(ecosystem, version string) bool {
	if version == "" {
		return true
	}

	switch ecosystem {
	case "npm", "Go":
		switch strings.ToLower(version) {
		case "*", "alpha", "beta", "canary", "head", "latest", "main", "master", "next", "stable", "tip":
			return true
		}
	}

	return false
}
