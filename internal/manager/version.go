package manager

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os/exec"
	"strings"
)

var (
	goProxyBase = "https://proxy.golang.org"
	pypiBase    = "https://pypi.org"
	runCmd      = func(name string, args ...string) ([]byte, error) {
		return exec.Command(name, args...).Output()
	}
)

func ResolveVersion(mgr *Manager, pkg string) (string, error) {
	switch mgr.Ecosystem {
	case "Homebrew":
		return brewVersion(pkg)
	case "npm":
		return npmVersion(pkg)
	case "Go":
		return goVersion(pkg)
	case "PyPI":
		return pypiVersion(pkg)
	default:
		return "", nil
	}
}

func brewVersion(name string) (string, error) {
	type brewInfo struct {
		Formulae []struct {
			Versions struct {
				Stable string `json:"stable"`
			} `json:"versions"`
		} `json:"formulae"`
	}
	out, err := runCmd("brew", "info", "--json=v2", name)
	if err != nil {
		return "", fmt.Errorf("brew info: %w", err)
	}
	var info brewInfo
	if err := json.Unmarshal(out, &info); err != nil {
		return "", fmt.Errorf("parse brew info: %w", err)
	}
	if len(info.Formulae) == 0 {
		return "", fmt.Errorf("formula %q not found", name)
	}
	return info.Formulae[0].Versions.Stable, nil
}

func npmVersion(pkg string) (string, error) {
	out, err := runCmd("npm", "view", pkg, "version")
	if err != nil {
		return "", fmt.Errorf("npm view: %w", err)
	}
	return strings.TrimSpace(string(out)), nil
}

func goVersion(module string) (string, error) {
	url := fmt.Sprintf("%s/%s/@latest", goProxyBase, module)
	resp, err := http.Get(url)
	if err != nil {
		return "", fmt.Errorf("go proxy: %w", err)
	}
	defer resp.Body.Close()
	var result struct {
		Version string `json:"Version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("parse go proxy: %w", err)
	}
	return result.Version, nil
}

func pypiVersion(pkg string) (string, error) {
	resp, err := http.Get(fmt.Sprintf("%s/pypi/%s/json", pypiBase, pkg))
	if err != nil {
		return "", fmt.Errorf("pypi: %w", err)
	}
	defer resp.Body.Close()
	var result struct {
		Info struct {
			Version string `json:"version"`
		} `json:"info"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("parse pypi: %w", err)
	}
	return result.Info.Version, nil
}
