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
	_, shrinkwrap, err := readOptionalProjectFile(filepath.Join(dir, "npm-shrinkwrap.json"))
	if err != nil {
		return err
	}
	if shrinkwrap {
		return errors.New("npm-shrinkwrap.json overrides package-lock.json and cannot be scanned")
	}
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
	if err := validateNPMOptionalLock(dir); err != nil {
		return err
	}
	return runNPMLockCheck(dir, args)
}

func validateNPMOptionalLock(dir string) error {
	manifestData, _, err := readOptionalProjectFile(filepath.Join(dir, "package.json"))
	if err != nil {
		return err
	}
	lockfile, _, err := readOptionalProjectFile(filepath.Join(dir, npmPackageLockFilename))
	if err != nil {
		return err
	}
	var manifest npmPackageManifest
	if err := json.Unmarshal(manifestData, &manifest); err != nil {
		return fmt.Errorf("parse package.json optional dependencies: %w", err)
	}
	return validateNPMOptionalDeclarations(dir, manifest, lockfile)
}

func validateNPMOptionalDeclarations(dir string, manifest npmPackageManifest, lockData []byte) error {
	var lockfile struct {
		LockfileVersion int                           `json:"lockfileVersion"`
		Packages        map[string]npmPackageManifest `json:"packages"`
	}
	if err := json.Unmarshal(lockData, &lockfile); err != nil {
		return fmt.Errorf("parse package-lock.json optional dependencies: %w", err)
	}
	if lockfile.LockfileVersion == 1 {
		return validateNPMLegacyOptionalLock(dir, manifest.OptionalDependencies)
	}
	locked := lockfile.Packages[""].OptionalDependencies
	// Offline npm ci can silently drop unresolved optional dependencies.
	matches := maps.Equal(manifest.OptionalDependencies, locked)
	if !matches {
		return errors.New("package.json optionalDependencies differ from package-lock.json; refresh the lockfile before installing")
	}
	return nil
}

func validateNPMLegacyOptionalLock(dir string, requirements map[string]string) error {
	if len(requirements) == 0 {
		return nil
	}
	names := make([]string, 0, len(requirements))
	for name := range requirements {
		names = append(names, name)
	}
	slices.Sort(names)
	projectDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return err
	}
	args := []string{"ls", "--prefix", projectDir, "--package-lock-only", "--json", "--all", "--depth=Infinity", "--include=prod", "--include=dev", "--include=optional", "--omit=peer", "--global=false", "--link=false", "--ignore-scripts", "--offline"}
	output, err := runCmd("npm", args...)
	if err != nil {
		return fmt.Errorf("validate legacy package-lock.json optional dependencies: %w", err)
	}
	return validateNPMLegacyOptionalResult(output, names)
}

func validateNPMLegacyOptionalResult(output []byte, names []string) error {
	var result struct {
		Dependencies map[string]struct {
			Version string `json:"version"`
			Invalid string `json:"invalid"`
			Missing bool   `json:"missing"`
		} `json:"dependencies"`
	}
	if err := json.Unmarshal(output, &result); err != nil {
		return fmt.Errorf("read legacy package-lock.json validation: %w", err)
	}
	for _, name := range names {
		entry := result.Dependencies[name]
		invalid := entry.Version == "" || entry.Invalid != "" || entry.Missing
		if invalid {
			return fmt.Errorf("cannot validate optional dependency %s in legacy package-lock.json; refresh the lockfile before installing", name)
		}
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
	lockUnavailable := err != nil || lockfile == nil
	if lockUnavailable {
		return err
	}
	if err := validatePackageLockPackages(lockfile.Packages, path); err != nil {
		return err
	}
	return validatePackageLockDependencies(lockfile.Dependencies, path, 0)
}

func readPackageLock(path string) (*packageLock, error) {
	data, exists, err := readOptionalProjectFile(path)
	fileUnavailable := err != nil || !exists
	if fileUnavailable {
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
		identityMismatch := entry.Name != "" && entry.Name != name
		if identityMismatch {
			return fmt.Errorf("package identity mismatch for %q in %s", name, path)
		}
		if err := validatePackageLockSource(name, entry.Version, entry.Resolved, entry.Link, path); err != nil {
			return err
		}
	}
	return nil
}

func validatePackageLockDependencies(dependencies map[string]packageLockDependency, path string, depth int) error {
	depthExceeded := depth >= maxPackageLockDependencyDepth && len(dependencies) > 0
	if depthExceeded {
		return fmt.Errorf("package dependency depth exceeds %d in %s", maxPackageLockDependencyDepth, path)
	}
	for name, dependency := range dependencies {
		identityMismatch := dependency.Name != "" && dependency.Name != name
		if identityMismatch {
			return fmt.Errorf("package identity mismatch for %q in %s", name, path)
		}
		const linked = false
		if err := validatePackageLockSource(name, dependency.Version, dependency.Resolved, linked, path); err != nil {
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
	unsupportedSource := link || hasUnsupportedVersion || hasUnsupportedSource
	if unsupportedSource {
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
	hasSecureURL := parsed.Scheme == "https" && parsed.User == nil
	supported := hasSecureURL && hasRegistryHost && hasPackagePath
	return supported
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
	manifestUnavailable := err != nil || !pyprojectExists
	if manifestUnavailable {
		return err
	}
	lockPath := filepath.Join(dir, lockName)
	_, lockExists, err := readOptionalProjectFile(lockPath)
	lockCheckComplete := err != nil || lockExists
	if lockCheckComplete {
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
	fileUnavailable := err != nil || !exists
	if fileUnavailable {
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
	fileUnavailable := err != nil || !exists
	if fileUnavailable {
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
	fileUnavailable := err != nil || !exists
	if fileUnavailable {
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
		if err := validateNPMDependencyGroup(dependencies, path); err != nil {
			return err
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
	fileUnavailable := err != nil || !exists
	if fileUnavailable {
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
	fileUnavailable := err != nil || !exists
	if fileUnavailable {
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

func validateNPMDependencyGroup(dependencies map[string]string, path string) error {
	for name, spec := range dependencies {
		if !IsSupportedNPMRegistrySpec(strings.TrimSpace(spec)) {
			return fmt.Errorf("unsupported npm dependency source for %q in %s", name, path)
		}
	}
	return nil
}
