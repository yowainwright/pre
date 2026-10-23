package manager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

type brewUpgradePackage struct {
	Name    string `json:"name"`
	Version string `json:"current_version"`
	Pinned  bool   `json:"pinned"`
}

type brewUpgradeInfo struct {
	Formulae []brewUpgradePackage `json:"formulae"`
	Casks    []brewUpgradePackage `json:"casks"`
}

var (
	goProxyBase = "https://proxy.golang.org"
	pypiBase    = "https://pypi.org"
	cratesBase  = "https://crates.io"
	versionHTTP = &http.Client{Timeout: 10 * time.Second}
	crateNameRE = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]*$`)
	runCmd      = func(name string, args ...string) ([]byte, error) {
		path, err := Executable(name)
		if err != nil {
			return nil, err
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		command := exec.CommandContext(ctx, path, args...) // #nosec G204 -- executable names are package manager tools.
		command.Env = versionCommandEnvironment(name, args)
		return command.Output()
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

const brewUpgradeFormulaScript = `require "formula"
require "formulary"
require "json"
names = JSON.parse(ARGV.fetch(0))
installed = Formula.installed.to_h { |formula| [formula.full_name, formula] }
targets = names.map do |name|
  formula = installed.fetch(name)
  latest = formula.latest_formula
  target = latest.latest_version_installed? ? formula : latest
  { name: target.full_name, current_version: target.pkg_version.to_s }
end
puts JSON.generate(formulae: targets, casks: [])
`

func Executable(name string) (string, error) {
	if os.Getenv("PRE_EXEC_MANAGER") != name {
		return name, nil
	}
	path := os.Getenv("PRE_EXEC_PATH")
	invalidPath := path == "" || filepath.Base(path) != name
	if invalidPath {
		return "", fmt.Errorf("%s: invalid alias executable path", name)
	}
	return path, nil
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

func BrewUpgradePackages(args []string) ([]string, error) {
	options, err := brewUpgradeOptions(args)
	if err != nil {
		return nil, err
	}
	command := append([]string{"outdated", "--json=v2"}, options...)
	out, err := runCmd("brew", command...)
	if err != nil {
		return nil, fmt.Errorf("brew outdated: %w", err)
	}
	var info brewUpgradeInfo
	if err := json.Unmarshal(out, &info); err != nil {
		return nil, fmt.Errorf("parse brew outdated: %w", err)
	}
	return brewUpgradeSpecs(info)
}

func versionCommandEnvironment(name string, args []string) []string {
	if name != "brew" {
		return nil
	}
	if len(args) == 0 {
		return nil
	}
	if args[0] != "ruby" {
		return nil
	}
	return append(os.Environ(), "HOMEBREW_DEV_CMD_RUN=1")
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
	if len(name) > 64 {
		return false
	}
	return crateNameRE.MatchString(name)
}

func decodeCrateVersion(resp *http.Response, name, requirement string) (string, error) {
	unsuccessfulResponse := resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices
	if unsuccessfulResponse {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return "", fmt.Errorf("crates.io: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}
	var result crateVersionResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("parse crates.io: %w", err)
	}
	return selectCrateVersion(result.Versions, name, requirement)
}

func selectCrateVersion(versions []crateVersionInfo, name, requirement string) (string, error) {
	if requirement != "" {
		version, ok := selectCargoVersion(versions, requirement)
		if !ok {
			return "", fmt.Errorf("crates.io: no version of %q matches %q", name, requirement)
		}
		return version, nil
	}
	if version, ok := selectCargoVersion(versions, "*"); ok {
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
		return "", fmt.Errorf("homebrew package %q not found", name)
	}
	return version, nil
}

func brewPackageVersion(info brewInfo) string {
	if len(info.Formulae) > 0 {
		formula := info.Formulae[0]
		if formula.Versions.Stable != "" {
			return formula.Versions.Stable
		}
	}
	if len(info.Casks) > 0 {
		return info.Casks[0].Version
	}
	return ""
}

func brewUpgradeOptions(args []string) ([]string, error) {
	var options []string
	for _, arg := range args {
		switch arg {
		case "--formula", "--formulae", "--cask", "--casks", "-g", "--greedy", "--greedy-latest", "--greedy-auto-updates", "--fetch-HEAD":
			options = append(options, arg)
		case "-d", "--debug", "-v", "--verbose", "-q", "--quiet", "-f", "--force", "-n", "--dry-run", "--display-times":
		case "--no-ask", "--yes", "-y", "--ask", "--force-bottle", "--keep-tmp", "--overwrite":
		case "--skip-cask-deps", "--no-quit", "--binaries", "--no-binaries", "--require-sha", "--":
		default:
			return nil, fmt.Errorf("brew upgrade option %q cannot be scanned", arg)
		}
	}
	return options, nil
}

func brewUpgradeSpecs(info brewUpgradeInfo) ([]string, error) {
	missingLists := info.Formulae == nil || info.Casks == nil
	if missingLists {
		return nil, errors.New("brew outdated: missing formulae or casks")
	}
	formulae, err := resolveBrewUpgradeFormulae(info.Formulae)
	if err != nil {
		return nil, err
	}
	return brewUpgradePackageSpecs(append(formulae, info.Casks...))
}

func resolveBrewUpgradeFormulae(formulae []brewUpgradePackage) ([]brewUpgradePackage, error) {
	names, err := brewUpgradeFormulaNames(formulae)
	if err != nil {
		return nil, err
	}
	if len(names) == 0 {
		return nil, nil
	}
	encoded, err := json.Marshal(names)
	if err != nil {
		return nil, fmt.Errorf("encode brew upgrade formulae: %w", err)
	}
	out, err := runCmd("brew", "ruby", "-e", brewUpgradeFormulaScript, "--", string(encoded))
	if err != nil {
		return nil, fmt.Errorf("resolve brew upgrade formulae: %w", err)
	}
	return decodeBrewUpgradeFormulae(out, len(names))
}

func brewUpgradeFormulaNames(formulae []brewUpgradePackage) ([]string, error) {
	var names []string
	for _, pkg := range formulae {
		if pkg.Pinned {
			continue
		}
		if err := validateBrewUpgradePackage(pkg); err != nil {
			return nil, err
		}
		names = append(names, pkg.Name)
	}
	return names, nil
}

func decodeBrewUpgradeFormulae(out []byte, count int) ([]brewUpgradePackage, error) {
	var info brewUpgradeInfo
	if err := json.Unmarshal(out, &info); err != nil {
		return nil, fmt.Errorf("parse brew upgrade formulae: %w", err)
	}
	if len(info.Formulae) != count {
		return nil, errors.New("brew upgrade formula resolution returned incomplete targets")
	}
	for _, pkg := range info.Formulae {
		if pkg.Pinned {
			return nil, errors.New("brew upgrade formula resolution unexpectedly returned a pinned target")
		}
	}
	return info.Formulae, nil
}

func brewUpgradePackageSpecs(packages []brewUpgradePackage) ([]string, error) {
	var specs []string
	for _, pkg := range packages {
		if pkg.Pinned {
			continue
		}
		if err := validateBrewUpgradePackage(pkg); err != nil {
			return nil, err
		}
		spec := pkg.Name + brewLockVersionSeparator + pkg.Version
		specs = append(specs, spec)
	}
	return specs, nil
}

func validateBrewUpgradePackage(pkg brewUpgradePackage) error {
	incomplete := pkg.Name == "" || pkg.Version == ""
	if incomplete {
		return errors.New("brew outdated: missing package name or target version")
	}
	unresolved := strings.EqualFold(pkg.Version, "latest") || strings.HasPrefix(pkg.Version, "HEAD")
	if unresolved {
		return fmt.Errorf("brew upgrade target version for %q is unresolved: %s", pkg.Name, pkg.Version)
	}
	spec := pkg.Name + brewLockVersionSeparator + pkg.Version
	name, version := ParseSpec("Homebrew", spec)
	mismatch := name != pkg.Name || version != pkg.Version
	if mismatch {
		return fmt.Errorf("brew upgrade target identity for %q cannot be represented safely", pkg.Name)
	}
	return nil
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
	return decodeGoVersion(resp, modulePath)
}

func decodeGoVersion(resp *http.Response, modulePath string) (string, error) {
	unsuccessfulResponse := resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices
	if unsuccessfulResponse {
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
		uppercase := char >= 'A' && char <= 'Z'
		if uppercase {
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
	return decodePyPIVersion(resp, pkg)
}

func decodePyPIVersion(resp *http.Response, pkg string) (string, error) {
	unsuccessfulResponse := resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices
	if unsuccessfulResponse {
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
