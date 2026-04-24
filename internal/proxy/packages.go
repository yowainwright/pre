package proxy

import (
	"strings"

	"github.com/yowainwright/pre/internal/manager"
)

func extractPackages(mgr *manager.Manager, args []string) []string {
	result := make([]string, 0, len(args))
	skipNext := false
	afterTerminator := false

	for _, arg := range args {
		if skipNext {
			skipNext = false
			continue
		}
		if arg == "--" {
			afterTerminator = true
			continue
		}
		if !afterTerminator && packageFlagConsumesValue(mgr, arg) {
			if !strings.Contains(arg, "=") {
				skipNext = true
			}
			continue
		}
		if !afterTerminator && strings.HasPrefix(arg, "-") {
			continue
		}
		if isPackageArg(mgr, arg) {
			result = append(result, arg)
		}
	}

	return result
}

func packageFlagConsumesValue(mgr *manager.Manager, arg string) bool {
	if mgr == nil {
		return false
	}
	flag := arg
	if idx := strings.Index(flag, "="); idx != -1 {
		flag = flag[:idx]
	}

	switch mgr.Ecosystem {
	case "npm":
		switch flag {
		case "--workspace", "-w", "--prefix", "--tag", "--registry", "--userconfig", "--cache",
			"--omit", "--include", "--install-strategy", "--save-prefix", "--otp", "--before", "--scope":
			return true
		}
	case "PyPI":
		switch flag {
		case "-r", "--requirement", "-c", "--constraint", "-i", "--index-url", "--index", "--default-index",
			"--extra-index-url", "-f", "--find-links", "--trusted-host", "--python", "--platform",
			"--python-version", "--implementation", "--abi", "--target", "--root", "--prefix", "--src",
			"--upgrade-strategy", "--config-settings", "-C", "--global-option", "--build-option",
			"--only-binary", "--no-binary", "--report", "-e", "--editable":
			return true
		}
		switch mgr.Name {
		case "poetry", "uv":
			switch flag {
			case "--group", "-G", "--source", "--extras":
				return true
			}
		}
	case "Go":
		switch flag {
		case "-C", "-mod", "-modfile", "-overlay", "-pgo", "-asmflags", "-gcflags", "-ldflags", "-tags", "-toolexec", "-pkgdir":
			return true
		}
	case "Homebrew":
		switch flag {
		case "--appdir", "--cc":
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
	case strings.HasPrefix(arg, "-"),
		strings.HasPrefix(arg, "."),
		strings.HasPrefix(arg, "/"),
		strings.HasPrefix(arg, "~/"),
		strings.HasPrefix(arg, "file:"),
		strings.HasPrefix(arg, "link:"),
		strings.HasPrefix(arg, "git+"),
		strings.HasPrefix(arg, "git://"),
		strings.HasPrefix(arg, "ssh://"),
		strings.HasPrefix(arg, "git@"),
		strings.HasPrefix(arg, "github:"),
		strings.HasPrefix(arg, "http://"),
		strings.HasPrefix(arg, "https://"):
		return false
	}
	if mgr != nil && mgr.Ecosystem == "npm" && strings.Contains(arg, "@npm:") {
		return false
	}

	if mgr != nil && mgr.Ecosystem == "PyPI" {
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
	case "npm":
		switch strings.ToLower(version) {
		case "*", "alpha", "beta", "canary", "head", "latest", "main", "master", "next", "stable", "tip":
			return true
		}
	case "Go":
		return strings.ToLower(version) == "latest"
	}

	return false
}
