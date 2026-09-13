package manager

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

const npmRegistryHost = "registry.npmjs.org"

func ValidateManifest(mgr *Manager, dir string, args ...string) error {
	if mgr == nil {
		return errors.New("package manager is required")
	}
	switch mgr.Ecosystem {
	case "npm":
		return validateNPMProject(mgr.Name, dir, args)
	case "Go":
		return validateTextFiles(dir, "go.sum", "go.mod")
	case "PyPI":
		return validatePythonProject(mgr.Name, dir)
	case "Homebrew":
		return validateHomebrewProject(dir)
	case "crates.io":
		return validateCargoProject(dir)
	default:
		return nil
	}
}

func validateNPMProject(name, dir string, args []string) error {
	err := validateNPMProjectFiles(name, dir)
	if err != nil {
		return err
	}
	if name != "npm" {
		return nil
	}
	return validateNPMLockConsistency(dir, args)
}

func validateNPMProjectFiles(name, dir string) error {
	var err error
	switch name {
	case "npm":
		err = validatePackageLock(filepath.Join(dir, npmPackageLockFilename))
	case "bun":
		err = validateBunLock(filepath.Join(dir, "bun.lock"))
	case "pnpm":
		err = validateTextFiles(dir, "pnpm-lock.yaml")
	default:
		err = validateAllNPMLocks(dir)
	}
	if err != nil {
		return err
	}
	return validatePackageJSON(filepath.Join(dir, "package.json"))
}

func validateNPMLockConsistency(dir string, args []string) error {
	for _, name := range []string{"package.json", npmPackageLockFilename} {
		_, exists, err := readOptionalProjectFile(filepath.Join(dir, name))
		if err != nil {
			return err
		}
		if !exists {
			return nil
		}
	}
	return checkNPMLockConsistency(dir, args)
}

func checkNPMLockConsistency(dir string, args []string) error {
	_, shrinkwrap, err := readOptionalProjectFile(filepath.Join(dir, "npm-shrinkwrap.json"))
	if err != nil {
		return err
	}
	if shrinkwrap {
		return errors.New("npm-shrinkwrap.json overrides package-lock.json and cannot be scanned")
	}
	if err := validateNPMOptionalLock(dir); err != nil {
		return err
	}
	return runNPMLockCheck(dir, args)
}

func validateNPMOptionalLock(dir string) error {
	manifest, _, err := readOptionalProjectFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return err
	}
	lockfile, _, err := readOptionalProjectFile(filepath.Join(dir, npmPackageLockFilename))
	if err != nil {
		return err
	}
	return validateNPMOptionalDeclarations(manifest, lockfile)
}

func validateNPMOptionalDeclarations(manifestData, lockData []byte) error {
	var manifest npmPackageManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return fmt.Errorf("parse package.json optional dependencies: %w", err)
	}
	var lockfile struct {
		Packages map[string]npmPackageManifest `json:"packages"`
	}
	if err := json.Unmarshal(lockData, &lockfile); err != nil {
		return fmt.Errorf("parse package-lock.json optional dependencies: %w", err)
	}
	locked := lockfile.Packages[""].OptionalDependencies
	// Offline npm ci can silently drop unresolved optional dependencies.
	matches := maps.Equal(manifest.OptionalDependencies, locked)
	if !matches {
		return errors.New("package.json optionalDependencies differ from package-lock.json; refresh the lockfile before installing")
	}
	return nil
}

func runNPMLockCheck(dir string, args []string) error {
	projectDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return err
	}
	options, err := npmLockOptions(args)
	if err != nil {
		return err
	}
	command := append([]string{"ci"}, options...)
	command = append(command, "--prefix", projectDir, "--dry-run", "--ignore-scripts", "--no-audit", "--no-fund", "--offline")
	_, err = runCmd("npm", command...)
	if err != nil {
		return fmt.Errorf("validate package-lock.json against package.json with npm ci: %w", err)
	}
	return nil
}

func npmLockOptions(args []string) ([]string, error) {
	var options []string
	for index := 0; index < len(args); index++ {
		isTerminator := strings.HasPrefix(args[index], "--") && strings.Trim(args[index], "-") == ""
		if isTerminator {
			break
		}
		option, consumed, err := npmLockOption(args, index)
		if err != nil {
			return nil, err
		}
		if option != "" {
			options = append(options, option)
		}
		if consumed {
			index++
		}
	}
	return options, nil
}

func npmLockOption(args []string, index int) (string, bool, error) {
	flag, value, inline := strings.Cut(args[index], "=")
	flag = npmLockOptionName(flag)
	known, needsValue := npmLockOptionKind(flag)
	if !known {
		return "", false, nil
	}
	if inline {
		consumed := false
		return npmLockOptionResult(flag, value, consumed, needsValue)
	}
	value, consumed, err := npmLockOptionValue(args, index, needsValue)
	if err != nil {
		return "", false, err
	}
	return npmLockOptionResult(flag, value, consumed, needsValue)
}

func npmLockOptionResult(flag, value string, consumed, needsValue bool) (string, bool, error) {
	valid := value != "" && !strings.HasPrefix(value, "-")
	if !needsValue {
		valid = value == "true" || value == "false"
	}
	if !valid {
		return "", false, fmt.Errorf("%s has an invalid value for lockfile validation", flag)
	}
	option := flag + "=" + value
	return option, consumed, nil
}

func npmLockOptionName(flag string) string {
	switch flag {
	case "-f":
		return "--force"
	case "-w":
		return "--workspace"
	default:
		return flag
	}
}

func npmLockOptionValue(args []string, index int, needsValue bool) (string, bool, error) {
	if index+1 < len(args) {
		value := args[index+1]
		isBoolean := value == "true" || value == "false"
		isValue := !strings.HasPrefix(value, "-") && (needsValue || isBoolean)
		if isValue {
			return value, true, nil
		}
	}
	if needsValue {
		return "", false, fmt.Errorf("%s requires a value for lockfile validation", args[index])
	}
	return "true", false, nil
}

func npmLockOptionKind(flag string) (bool, bool) {
	valueFlags := []string{"--install-strategy", "--omit", "--include", "--only", "--also", "--cpu", "--os", "--libc", "--before", "--workspace"}
	if slices.Contains(valueFlags, flag) {
		return true, true
	}
	positive := "--" + strings.TrimPrefix(flag, "--no-")
	if strings.HasPrefix(flag, "--no-") {
		flag = positive
	}
	booleanFlags := []string{
		"--legacy-peer-deps", "--strict-peer-deps", "--install-links", "--legacy-bundling", "--global-style",
		"--prefer-dedupe", "--force", "--engine-strict", "--production", "--prod", "--workspaces", "--include-workspace-root",
	}
	return slices.Contains(booleanFlags, flag), false
}

func validateAllNPMLocks(dir string) error {
	if err := validatePackageLock(filepath.Join(dir, npmPackageLockFilename)); err != nil {
		return err
	}
	if err := validateBunLock(filepath.Join(dir, "bun.lock")); err != nil {
		return err
	}
	return validateTextFiles(dir, "pnpm-lock.yaml")
}

func validatePackageLock(path string) error {
	lockfile, err := readPackageLock(path)
	if err != nil || lockfile == nil {
		return err
	}
	if err := validatePackageLockPackages(lockfile.Packages, path); err != nil {
		return err
	}
	return validatePackageLockDependencies(lockfile.Dependencies, path, 0)
}

func readPackageLock(path string) (*packageLock, error) {
	data, exists, err := readOptionalProjectFile(path)
	if err != nil || !exists {
		return nil, err
	}
	var lockfile *packageLock
	if err := json.Unmarshal(data, &lockfile); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if lockfile == nil {
		return nil, fmt.Errorf("parse %s: expected JSON object", path)
	}
	return lockfile, nil
}

func validatePackageLockPackages(packages map[string]packageLockEntry, path string) error {
	for packagePath, entry := range packages {
		if packagePath == "" {
			continue
		}
		name := packageLockPackageName(packagePath)
		if entry.Name != "" && entry.Name != name {
			return fmt.Errorf("package identity mismatch for %q in %s", name, path)
		}
		if err := validatePackageLockSource(name, entry.Version, entry.Resolved, entry.Link, path); err != nil {
			return err
		}
	}
	return nil
}

func validatePackageLockDependencies(dependencies map[string]packageLockDependency, path string, depth int) error {
	if depth >= maxPackageLockDependencyDepth && len(dependencies) > 0 {
		return fmt.Errorf("package dependency depth exceeds %d in %s", maxPackageLockDependencyDepth, path)
	}
	for name, dependency := range dependencies {
		if dependency.Name != "" && dependency.Name != name {
			return fmt.Errorf("package identity mismatch for %q in %s", name, path)
		}
		if err := validatePackageLockSource(name, dependency.Version, dependency.Resolved, false, path); err != nil {
			return err
		}
		if err := validatePackageLockDependencies(dependency.Dependencies, path, depth+1); err != nil {
			return err
		}
	}
	return nil
}

func validatePackageLockSource(name, version, resolved string, link bool, path string) error {
	hasUnsupportedVersion := version != "" && !IsSupportedNPMRegistrySpec(strings.TrimSpace(version))
	hasUnsupportedSource := resolved != "" && !isNPMRegistryURL(resolved, name)
	if link || hasUnsupportedVersion || hasUnsupportedSource {
		return fmt.Errorf("unsupported npm lockfile source for %q in %s", name, path)
	}
	return nil
}

func isNPMRegistryURL(value, name string) bool {
	parsed, err := url.Parse(value)
	if err != nil {
		return false
	}
	hasRegistryHost := strings.EqualFold(parsed.Hostname(), npmRegistryHost) && parsed.Port() == ""
	hasPackagePath := strings.HasPrefix(parsed.Path, "/"+name+"/-/")
	return parsed.Scheme == "https" && parsed.User == nil && hasRegistryHost && hasPackagePath
}

func validatePythonProject(name, dir string) error {
	lockName, err := validatePythonManagerFiles(name, dir)
	if err != nil {
		return err
	}
	if lockName != "" {
		if err := requireLockForPyproject(dir, lockName); err != nil {
			return err
		}
	}
	return validateRequirements(filepath.Join(dir, "requirements.txt"))
}

func validatePythonManagerFiles(name, dir string) (string, error) {
	switch name {
	case "uv":
		return "uv.lock", validateTextFiles(dir, "uv.lock", "pyproject.toml")
	case "poetry":
		return "poetry.lock", validateTextFiles(dir, "poetry.lock", "pyproject.toml")
	case "pip", "pip3":
		return "", validateJSONFiles(dir, "Pipfile.lock")
	default:
		return "", validateAllPythonLocks(dir)
	}
}

func requireLockForPyproject(dir, lockName string) error {
	pyprojectPath := filepath.Join(dir, "pyproject.toml")
	_, pyprojectExists, err := readOptionalProjectFile(pyprojectPath)
	if err != nil || !pyprojectExists {
		return err
	}
	lockPath := filepath.Join(dir, lockName)
	_, lockExists, err := readOptionalProjectFile(lockPath)
	if err != nil || lockExists {
		return err
	}
	return fmt.Errorf("%s is required to pre-scan pyproject.toml", lockName)
}

func validateAllPythonLocks(dir string) error {
	if err := validateJSONFiles(dir, "Pipfile.lock"); err != nil {
		return err
	}
	return validateTextFiles(dir, "uv.lock", "poetry.lock", "pyproject.toml")
}

func validateHomebrewProject(dir string) error {
	if err := validateJSONFiles(dir, "Brewfile.lock.json"); err != nil {
		return err
	}
	return validateTextFiles(dir, "Brewfile")
}

func validateCargoProject(dir string) error {
	manifestPath := filepath.Join(dir, "Cargo.toml")
	_, err := os.Stat(manifestPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read %s: %w", manifestPath, err)
	}
	_, err = ReadCargoFetchPackages(manifestPath)
	return err
}

func validateJSONFiles(dir string, names ...string) error {
	for _, name := range names {
		path := filepath.Join(dir, name)
		if err := validateJSONFile(path); err != nil {
			return err
		}
	}
	return nil
}

func validateJSONFile(path string) error {
	data, exists, err := readOptionalProjectFile(path)
	if err != nil || !exists {
		return err
	}
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	if object == nil {
		return fmt.Errorf("parse %s: expected JSON object", path)
	}
	return nil
}

func validateBunLock(path string) error {
	data, exists, err := readOptionalProjectFile(path)
	if err != nil || !exists {
		return err
	}
	var object map[string]json.RawMessage
	if err := unmarshalBunLock(data, &object); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	if object == nil {
		return fmt.Errorf("parse %s: expected JSON object", path)
	}
	return nil
}

func validatePackageJSON(path string) error {
	data, exists, err := readOptionalProjectFile(path)
	if err != nil || !exists {
		return err
	}
	var manifest *npmPackageManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	if manifest == nil {
		return fmt.Errorf("parse %s: expected JSON object", path)
	}
	return validateNPMDependencySources(*manifest, path)
}

func validateNPMDependencySources(manifest npmPackageManifest, path string) error {
	groups := []map[string]string{
		manifest.Dependencies,
		manifest.DevDependencies,
		manifest.OptionalDependencies,
	}
	for _, dependencies := range groups {
		for name, spec := range dependencies {
			if !IsSupportedNPMRegistrySpec(strings.TrimSpace(spec)) {
				return fmt.Errorf("unsupported npm dependency source for %q in %s", name, path)
			}
		}
	}
	return nil
}

func validateTextFiles(dir string, names ...string) error {
	for _, name := range names {
		path := filepath.Join(dir, name)
		if err := validateTextFile(path); err != nil {
			return err
		}
	}
	return nil
}

func validateTextFile(path string) error {
	data, exists, err := readOptionalProjectFile(path)
	if err != nil || !exists {
		return err
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}

func validateRequirements(path string) error {
	_, exists, err := readOptionalProjectFile(path)
	if err != nil || !exists {
		return err
	}
	_, err = ReadRequirementsFile(path)
	if err != nil {
		return fmt.Errorf("parse %s: %w", path, err)
	}
	return nil
}

func readOptionalProjectFile(path string) ([]byte, bool, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, fmt.Errorf("read %s: %w", path, err)
	}
	return data, true, nil
}
