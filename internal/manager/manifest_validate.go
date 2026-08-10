package manager

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

func ValidateManifest(mgr *Manager, dir string) error {
	if mgr == nil {
		return errors.New("package manager is required")
	}
	switch mgr.Ecosystem {
	case "npm":
		return validateNPMProject(mgr.Name, dir)
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

func validateNPMProject(name, dir string) error {
	var err error
	switch name {
	case "npm":
		err = validateJSONFiles(dir, "package-lock.json")
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

func validateAllNPMLocks(dir string) error {
	if err := validateJSONFiles(dir, "package-lock.json"); err != nil {
		return err
	}
	if err := validateBunLock(filepath.Join(dir, "bun.lock")); err != nil {
		return err
	}
	return validateTextFiles(dir, "pnpm-lock.yaml")
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
