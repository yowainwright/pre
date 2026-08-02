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

type cargoManifestFile struct {
	path  string
	state cargoManifestState
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

func DiscoverCargoManifest(startPath string) (string, error) {
	candidate, err := filepath.Abs(startPath)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		next, ok := parentCargoManifestPath(candidate)
		if !ok {
			break
		}
		candidate = next
	}
	return "", fmt.Errorf("Cargo.toml not found from %s: %w", startPath, os.ErrNotExist)
}

func (p cargoProject) manifests() ([]cargoManifestFile, error) {
	seen := make(map[string]bool)
	var manifests []cargoManifestFile
	manifests, err := appendCargoManifest(manifests, seen, p.manifestPath, p.state)
	if err != nil || p.workspace.manifestPath == "" {
		return manifests, err
	}
	manifests, err = appendCargoManifest(manifests, seen, p.workspace.manifestPath, p.workspace.state)
	if err != nil {
		return nil, err
	}
	memberPaths, err := p.workspace.memberManifestPaths()
	if err != nil {
		return nil, err
	}
	for _, path := range memberPaths {
		state, err := readCargoManifestState(path)
		if err != nil {
			return nil, err
		}
		manifests, err = appendCargoManifest(manifests, seen, path, state)
		if err != nil {
			return nil, err
		}
	}
	return manifests, nil
}

func appendCargoManifest(files []cargoManifestFile, seen map[string]bool, path string, state cargoManifestState) ([]cargoManifestFile, error) {
	key, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if seen[key] {
		return files, nil
	}
	seen[key] = true
	return append(files, cargoManifestFile{path: path, state: state}), nil
}

func (w cargoWorkspace) memberManifestPaths() ([]string, error) {
	root := filepath.Dir(w.manifestPath)
	excluded, err := cargoWorkspaceExcludes(root, w.state.workspaceExcludes)
	if err != nil {
		return nil, err
	}
	var paths []string
	seen := make(map[string]bool)
	for _, pattern := range w.state.workspaceMembers {
		matches, err := cargoWorkspaceMatches(root, pattern, true)
		if err != nil {
			return nil, err
		}
		for _, match := range matches {
			path, err := cargoMemberManifestPath(match)
			if err != nil {
				return nil, err
			}
			if !excluded[path] && !seen[path] {
				seen[path] = true
				paths = append(paths, path)
			}
		}
	}
	return paths, nil
}

func cargoWorkspaceExcludes(root string, patterns []string) (map[string]bool, error) {
	excluded := make(map[string]bool)
	for _, pattern := range patterns {
		matches, err := cargoWorkspaceMatches(root, pattern, false)
		if err != nil {
			return nil, err
		}
		for _, match := range matches {
			path, err := cargoMemberManifestPath(match)
			if err != nil {
				return nil, err
			}
			excluded[path] = true
		}
	}
	return excluded, nil
}

func cargoWorkspaceMatches(root, pattern string, required bool) ([]string, error) {
	matches, err := filepath.Glob(filepath.Join(root, pattern))
	if err != nil {
		return nil, fmt.Errorf("invalid Cargo workspace member pattern %q: %w", pattern, err)
	}
	if required && len(matches) == 0 {
		return nil, fmt.Errorf("Cargo workspace member %q does not exist", pattern)
	}
	return matches, nil
}

func cargoMemberManifestPath(memberPath string) (string, error) {
	path := memberPath
	if filepath.Base(path) != "Cargo.toml" {
		path = filepath.Join(path, "Cargo.toml")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return absolute, nil
}

func (p cargoProject) validateSources() error {
	manifests, err := p.manifests()
	if err != nil {
		return err
	}
	for _, manifest := range manifests {
		if err := validateCargoManifestFile(manifest); err != nil {
			return err
		}
	}
	return nil
}

func validateCargoManifestFile(manifest cargoManifestFile) error {
	if manifest.state.unsupportedSyntax {
		return unsupportedCargoManifestSyntaxError(manifest.path)
	}
	if manifest.state.unsupportedSource {
		return unsupportedCargoSourceError(manifest.path)
	}
	return nil
}

func unsupportedCargoManifestSyntaxError(path string) error {
	return fmt.Errorf("unsupported Cargo manifest syntax in %s", path)
}

func unsupportedCargoSourceError(path string) error {
	return fmt.Errorf("unsupported Cargo source in %s", path)
}

func (p cargoProject) lockPath() string {
	manifestPath := p.manifestPath
	if p.workspace.manifestPath != "" {
		manifestPath = p.workspace.manifestPath
	}
	return filepath.Join(filepath.Dir(manifestPath), "Cargo.lock")
}

func (p cargoProject) directPackages() ([]string, error) {
	manifests, err := p.manifests()
	if err != nil {
		return nil, err
	}
	var packages []string
	seen := make(map[string]bool)
	for _, manifest := range manifests {
		manifestPackages, err := p.manifestPackages(manifest)
		if err != nil {
			return nil, err
		}
		packages = appendUniquePackageSpecs(packages, seen, manifestPackages)
	}
	return packages, nil
}

func (p cargoProject) manifestPackages(manifest cargoManifestFile) ([]string, error) {
	packages := append([]string(nil), manifest.state.packages...)
	if !manifest.state.inheritedDependency {
		return packages, nil
	}
	if p.workspace.manifestPath == "" || len(manifest.state.inheritedNames) == 0 {
		return nil, fmt.Errorf("inherited Cargo dependency in %s cannot be resolved", manifest.path)
	}
	return p.appendInheritedPackages(packages, manifest)
}

func (p cargoProject) appendInheritedPackages(packages []string, manifest cargoManifestFile) ([]string, error) {
	seen := packageSpecSet(packages)
	for _, name := range manifest.state.inheritedNames {
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

func appendUniquePackageSpecs(packages []string, seen map[string]bool, specs []string) []string {
	for _, spec := range specs {
		if seen[spec] {
			continue
		}
		seen[spec] = true
		packages = append(packages, spec)
	}
	return packages
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
