package manager

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type cargoWorkspace struct {
	manifestPath string
	state        cargoManifestState
}

type cargoProject struct {
	manifestPath string
	state        cargoManifestState
	workspace    cargoWorkspace
}

func ReadCargoFetchPackages(manifestPath string) ([]string, error) {
	project, err := readCargoProject(manifestPath)
	if err != nil {
		return nil, err
	}
	if err := project.validateSources(); err != nil {
		return nil, err
	}
	packages, err := readCargoLockFile(project.lockPath())
	if err == nil {
		return packages, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if project.workspace.manifestPath != "" {
		return nil, errors.New("Cargo.lock is required to pre-scan workspace fetches; run cargo generate-lockfile first")
	}
	return project.directPackages()
}

func ReadCargoUpdatePackages(manifestPath, target string) ([]string, error) {
	project, err := readCargoProject(manifestPath)
	if err != nil {
		return nil, err
	}
	if err := project.validateSources(); err != nil {
		return nil, err
	}
	packages, err := project.directPackages()
	if err != nil || target == "" {
		return packages, err
	}
	return selectCargoTarget(packages, target, manifestPath)
}

func readCargoProject(manifestPath string) (cargoProject, error) {
	state, err := readCargoManifestState(manifestPath)
	if err != nil {
		return cargoProject{}, err
	}
	workspace, err := discoverCargoWorkspace(manifestPath)
	if err != nil {
		return cargoProject{}, err
	}
	project := cargoProject{manifestPath: manifestPath, state: state, workspace: workspace}
	return project, nil
}

func discoverCargoWorkspace(manifestPath string) (cargoWorkspace, error) {
	candidate := filepath.Clean(manifestPath)
	for {
		workspace, found, err := cargoWorkspaceAt(candidate)
		if err != nil || found {
			return workspace, err
		}
		parent, ok := parentCargoManifestPath(candidate)
		if !ok {
			return cargoWorkspace{}, nil
		}
		candidate = parent
	}
}

func cargoWorkspaceAt(path string) (cargoWorkspace, bool, error) {
	state, err := readCargoManifestState(path)
	if errors.Is(err, os.ErrNotExist) {
		return cargoWorkspace{}, false, nil
	}
	if err != nil {
		return cargoWorkspace{}, false, err
	}
	workspace := cargoWorkspace{manifestPath: path, state: state}
	return workspace, state.hasWorkspace, nil
}

func parentCargoManifestPath(path string) (string, bool) {
	dir := filepath.Dir(path)
	parent := filepath.Dir(dir)
	if parent == dir {
		return "", false
	}
	return filepath.Join(parent, "Cargo.toml"), true
}

func (p cargoProject) validateSources() error {
	if p.state.unsupportedSyntax {
		return unsupportedCargoManifestSyntaxError(p.manifestPath)
	}
	if p.state.unsupportedSource {
		return unsupportedCargoSourceError(p.manifestPath)
	}
	workspaceIsManifest := sameCargoManifest(p.workspace.manifestPath, p.manifestPath)
	if p.workspace.manifestPath == "" || workspaceIsManifest {
		return nil
	}
	if p.workspace.state.unsupportedSyntax {
		return unsupportedCargoManifestSyntaxError(p.workspace.manifestPath)
	}
	if p.workspace.state.unsupportedSource {
		return unsupportedCargoSourceError(p.workspace.manifestPath)
	}
	return nil
}

func unsupportedCargoManifestSyntaxError(path string) error {
	return fmt.Errorf("unsupported Cargo manifest syntax in %s", path)
}

func unsupportedCargoSourceError(path string) error {
	return fmt.Errorf("unsupported Cargo source in %s", path)
}

func sameCargoManifest(left, right string) bool {
	leftAbsolute, leftErr := filepath.Abs(left)
	rightAbsolute, rightErr := filepath.Abs(right)
	return leftErr == nil && rightErr == nil && leftAbsolute == rightAbsolute
}

func (p cargoProject) lockPath() string {
	manifestPath := p.manifestPath
	if p.workspace.manifestPath != "" {
		manifestPath = p.workspace.manifestPath
	}
	return filepath.Join(filepath.Dir(manifestPath), "Cargo.lock")
}

func (p cargoProject) directPackages() ([]string, error) {
	packages := append([]string(nil), p.state.packages...)
	if !p.state.inheritedDependency {
		return packages, nil
	}
	if p.workspace.manifestPath == "" || len(p.state.inheritedNames) == 0 {
		return nil, fmt.Errorf("inherited Cargo dependency in %s cannot be resolved", p.manifestPath)
	}
	return p.appendInheritedPackages(packages)
}

func (p cargoProject) appendInheritedPackages(packages []string) ([]string, error) {
	seen := packageSpecSet(packages)
	for _, name := range p.state.inheritedNames {
		spec, ok := p.workspace.state.workspaceSpecs[name]
		if !ok {
			return nil, fmt.Errorf("workspace dependency %q is not defined in %s", name, p.workspace.manifestPath)
		}
		if !seen[spec] {
			seen[spec] = true
			packages = append(packages, spec)
		}
	}
	return packages, nil
}

func packageSpecSet(packages []string) map[string]bool {
	seen := make(map[string]bool, len(packages))
	for _, spec := range packages {
		seen[spec] = true
	}
	return seen
}

func selectCargoTarget(packages []string, target, manifestPath string) ([]string, error) {
	for _, spec := range packages {
		name, _ := ParseSpec("crates.io", spec)
		if name == target {
			return []string{spec}, nil
		}
	}
	return nil, fmt.Errorf("crate %q is not a direct dependency in %s", target, manifestPath)
}

func readCargoLockFile(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	state, err := parseCargoLock(data)
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	if state.unsupportedSource {
		return nil, unsupportedCargoSourceError(path)
	}
	return state.result, nil
}

func readCargoManifestState(path string) (cargoManifestState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return cargoManifestState{}, fmt.Errorf("read %s: %w", path, err)
	}
	state, err := parseCargoTomlState(data)
	if err != nil {
		return cargoManifestState{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return state, nil
}
