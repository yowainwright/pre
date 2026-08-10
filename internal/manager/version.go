package manager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

var (
	goProxyBase = "https://proxy.golang.org"
	pypiBase    = "https://pypi.org"
	cratesBase  = "https://crates.io"
	versionHTTP = &http.Client{Timeout: 10 * time.Second}
	crateNameRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,63}$`)
	runCmd      = func(name string, args ...string) ([]byte, error) {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return exec.CommandContext(ctx, name, args...).Output() // #nosec G204 -- executable names are package manager tools.
	}
)

type crateVersionResponse struct {
	Versions []crateVersionInfo `json:"versions"`
}

type brewFormulaInfo struct {
	Versions struct {
		Stable string `json:"stable"`
	} `json:"versions"`
}

type brewCaskInfo struct {
	Version string `json:"version"`
}

type brewInfo struct {
	Formulae []brewFormulaInfo `json:"formulae"`
	Casks    []brewCaskInfo    `json:"casks"`
}

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
	case "crates.io":
		return crateVersion(pkg)
	default:
		return "", nil
	}
}

func crateVersion(spec string) (string, error) {
	name, requirement := parseAtSeparator(spec)
	if !IsValidCrateName(name) {
		return "", fmt.Errorf("crates.io: invalid crate name %q", name)
	}
	url := fmt.Sprintf("%s/api/v1/crates/%s", cratesBase, name)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	if err != nil {
		return "", fmt.Errorf("crates.io: %w", err)
	}
	req.Header.Set("User-Agent", "pre (https://github.com/yowainwright/pre)")
	resp, err := versionHTTP.Do(req)
	if err != nil {
		return "", fmt.Errorf("crates.io: %w", err)
	}
	defer resp.Body.Close()
	return decodeCrateVersion(resp, name, requirement)
}

func IsValidCrateName(name string) bool {
	return crateNameRE.MatchString(name)
}

func decodeCrateVersion(resp *http.Response, name, requirement string) (string, error) {
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("crates.io: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var result crateVersionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("parse crates.io: %w", err)
	}
	if requirement != "" {
		version, ok := selectCargoVersion(result.Versions, requirement)
		if !ok {
			return "", fmt.Errorf("crates.io: no version of %q matches %q", name, requirement)
		}
		return version, nil
	}
	if version, ok := selectCargoVersion(result.Versions, "*"); ok {
		return version, nil
	}
	return "", fmt.Errorf("crates.io: no non-yanked stable version for %q", name)
}

func brewVersion(name string) (string, error) {
	out, err := runCmd("brew", "info", "--json=v2", "--", name)
	if err != nil {
		return "", fmt.Errorf("brew info: %w", err)
	}
	var info brewInfo
	if err := json.Unmarshal(out, &info); err != nil {
		return "", fmt.Errorf("parse brew info: %w", err)
	}
	version := brewPackageVersion(info)
	if version == "" {
		return "", fmt.Errorf("Homebrew package %q not found", name)
	}
	return version, nil
}

func brewPackageVersion(info brewInfo) string {
	if len(info.Formulae) > 0 && info.Formulae[0].Versions.Stable != "" {
		return info.Formulae[0].Versions.Stable
	}
	if len(info.Casks) > 0 && info.Casks[0].Version != "" {
		return info.Casks[0].Version
	}
	return ""
}

func npmVersion(pkg string) (string, error) {
	out, err := runCmd("npm", "view", "--json", "--", pkg, "version")
	if err != nil {
		return "", fmt.Errorf("npm: version unavailable")
	}
	version := decodeNPMVersion(out)
	if version == "" {
		return "", fmt.Errorf("npm: version unavailable")
	}
	return version, nil
}

func decodeNPMVersion(out []byte) string {
	var versions []string
	if err := json.Unmarshal(out, &versions); err == nil {
		if len(versions) == 0 {
			return ""
		}
		return versions[len(versions)-1]
	}
	var version string
	if err := json.Unmarshal(out, &version); err == nil {
		return strings.TrimSpace(version)
	}
	if json.Valid(out) {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func goVersion(modulePath string) (string, error) {
	escapedPath, err := escapeGoModulePath(modulePath)
	if err != nil {
		return "", fmt.Errorf("go proxy: %w", err)
	}
	url := fmt.Sprintf("%s/%s/@latest", goProxyBase, escapedPath)
	resp, err := versionHTTP.Get(url)
	if err != nil {
		return "", fmt.Errorf("go proxy: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("go proxy: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var result struct {
		Version string `json:"Version"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("parse go proxy: %w", err)
	}
	if result.Version == "" {
		return "", fmt.Errorf("go proxy: empty version for %q", modulePath)
	}
	return result.Version, nil
}

func escapeGoModulePath(modulePath string) (string, error) {
	if modulePath == "" {
		return "", errors.New("empty module path")
	}
	var escaped strings.Builder
	for _, char := range modulePath {
		if char == '!' {
			return "", fmt.Errorf("invalid module path %q", modulePath)
		}
		if char >= 'A' && char <= 'Z' {
			escaped.WriteByte('!')
			char += 'a' - 'A'
		}
		escaped.WriteRune(char)
	}
	return escaped.String(), nil
}

func pypiVersion(pkg string) (string, error) {
	resp, err := versionHTTP.Get(fmt.Sprintf("%s/pypi/%s/json", pypiBase, pkg))
	if err != nil {
		return "", fmt.Errorf("pypi: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("pypi: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var result struct {
		Info struct {
			Version string `json:"version"`
		} `json:"info"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("parse pypi: %w", err)
	}
	if result.Info.Version == "" {
		return "", fmt.Errorf("pypi: empty version for %q", pkg)
	}
	return result.Info.Version, nil
}
