package manager

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// ReadNPMProject validates npm project files and prefers locked package versions.
// When err is nil, the boolean reports whether a supported project file exists.
func ReadNPMProject(dir string, args ...string) ([]string, bool, error) {
	if err := validateNPMProject("npm", dir, args); err != nil {
		return nil, false, err
	}
	packages, lockExists, err := readPackageLockResult(dir)
	hasLockResult := err != nil || len(packages) > 0
	if hasLockResult {
		return packages, lockExists, err
	}
	packages, manifestExists, err := readPackageJSONResult(dir)
	return packages, lockExists || manifestExists, err
}

func ReadLockfile(mgr *Manager, dir string) []string {
	switch mgr.Ecosystem {
	case "npm":
		return readNPMManagerLockfile(mgr.Name, dir)
	case "Go":
		return readGoSum(dir)
	case "PyPI":
		return readPythonManagerLockfile(mgr.Name, dir)
	case "Homebrew":
		return readBrewfileLockJSON(dir)
	case "crates.io":
		return readCargoLock(dir)
	}
	return nil
}

func readNPMManagerLockfile(name, dir string) []string {
	switch name {
	case "npm":
		return readPackageLockJSON(dir)
	case "bun":
		return readBunLock(dir)
	case "pnpm":
		return readPNPMLock(dir)
	default:
		return readNPMLockfile(dir)
	}
}

type cargoLockPackage struct {
	name    string
	version string
	source  string
}

type cargoLockState struct {
	current           cargoLockPackage
	seen              map[string]bool
	result            []string
	unsupportedSource bool
}

func readCargoLock(dir string) []string {
	lockPath := filepath.Join(dir, "Cargo.lock")
	data, err := os.ReadFile(lockPath)
	if err != nil {
		return nil
	}
	state, err := parseCargoLock(data)
	if err != nil {
		return nil
	}
	return state.result
}

func parseCargoLock(data []byte) (cargoLockState, error) {
	state := newCargoLockState()
	if err := checkCargoLockLineLimit(data); err != nil {
		return state, err
	}
	err := state.decodeTOML(data)
	return state, err
}

func (s *cargoLockState) decodeTOML(data []byte) error {
	var document map[string]toml.Primitive
	metadata, err := toml.Decode(string(data), &document)
	if err != nil {
		return err
	}
	raw, exists := document["package"]
	if !exists {
		return nil
	}
	var entries []map[string]toml.Primitive
	if err := metadata.PrimitiveDecode(raw, &entries); err != nil {
		return err
	}
	return s.appendTOMLPackages(&metadata, entries)
}

func (s *cargoLockState) appendTOMLPackages(metadata *toml.MetaData, entries []map[string]toml.Primitive) error {
	for _, fields := range entries {
		if err := s.decodeTOMLFields(metadata, fields); err != nil {
			return err
		}
		s.flush()
	}
	return nil
}

func (s *cargoLockState) decodeTOMLFields(metadata *toml.MetaData, fields map[string]toml.Primitive) error {
	for _, key := range []string{"name", "version", "source"} {
		raw, exists := fields[key]
		if !exists {
			continue
		}
		var value string
		if err := metadata.PrimitiveDecode(raw, &value); err != nil {
			return err
		}
		s.set(key, value)
	}
	return nil
}

func checkCargoLockLineLimit(data []byte) error {
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
	}
	return scanner.Err()
}

func newCargoLockState() cargoLockState {
	seen := make(map[string]bool)
	return cargoLockState{seen: seen}
}

func (s *cargoLockState) set(key, value string) {
	switch key {
	case "name":
		s.current.name = value
	case "version":
		s.current.version = value
	case "source":
		s.current.source = value
	}
}

func (s *cargoLockState) flush() {
	current := s.current
	s.current = cargoLockPackage{}
	if current.name == "" || current.version == "" {
		return
	}
	if current.source != "" && !isCratesIOSource(current.source) {
		s.unsupportedSource = true
		return
	}
	if !isCratesIOSource(current.source) {
		return
	}
	spec := current.name + "@" + current.version
	if s.seen[spec] {
		return
	}
	s.seen[spec] = true
	s.result = append(s.result, spec)
}

func isCratesIOSource(source string) bool {
	trimmed := strings.TrimSuffix(source, "/")
	legacySource := trimmed == "registry+https://github.com/rust-lang/crates.io-index"
	registrySource := trimmed == "registry+https://index.crates.io"
	sparseSource := trimmed == "sparse+https://index.crates.io"
	return legacySource || registrySource || sparseSource
}

// npm: package-lock.json → bun.lock → pnpm-lock.yaml

const maxPackageLockDependencyDepth = 50

const npmPackageLockFilename = "package-lock.json"

type packageLock struct {
	Packages     map[string]packageLockEntry      `json:"packages"`
	Dependencies map[string]packageLockDependency `json:"dependencies"`
}

type packageLockEntry struct {
	Name     string `json:"name"`
	Version  string `json:"version"`
	Resolved string `json:"resolved"`
	Link     bool   `json:"link"`
}

type packageLockDependency struct {
	Name         string                           `json:"name"`
	Version      string                           `json:"version"`
	Resolved     string                           `json:"resolved"`
	Dependencies map[string]packageLockDependency `json:"dependencies"`
}

func readNPMLockfile(dir string) []string {
	if pkgs := readPackageLockJSON(dir); len(pkgs) > 0 {
		return pkgs
	}
	if pkgs := readBunLock(dir); len(pkgs) > 0 {
		return pkgs
	}
	return readPNPMLock(dir)
}

func readPackageLockJSON(dir string) []string {
	packages, _, _ := readPackageLockResult(dir)
	return packages
}

func readPackageLockResult(dir string) ([]string, bool, error) {
	lockfile, err := readPackageLock(filepath.Join(dir, npmPackageLockFilename))
	lockUnavailable := err != nil || lockfile == nil
	if lockUnavailable {
		return nil, false, err
	}
	return packageLockPackages(lockfile), true, nil
}

func packageLockPackages(lockfile *packageLock) []string {
	seen := make(map[string]bool, len(lockfile.Packages)+len(lockfile.Dependencies))
	result := packageLockEntries(lockfile.Packages, seen)
	if len(result) > 0 {
		return result
	}
	appendPackageLockDependencies(&result, seen, lockfile.Dependencies, 0)
	return result
}

func packageLockEntries(entries map[string]packageLockEntry, seen map[string]bool) []string {
	var result []string
	for path, pkg := range entries {
		spec := packageLockEntrySpec(path, pkg)
		skipEntry := spec == "" || seen[spec]
		if skipEntry {
			continue
		}
		seen[spec] = true
		result = append(result, spec)
	}
	return result
}

func packageLockEntrySpec(path string, pkg packageLockEntry) string {
	missingVersion := path == "" || pkg.Version == ""
	if missingVersion {
		return ""
	}
	name := pkg.Name
	if name == "" {
		name = packageLockPackageName(path)
	}
	spec := name + "@" + pkg.Version
	return spec
}

func packageLockPackageName(path string) string {
	name := strings.TrimPrefix(path, "node_modules/")
	if idx := strings.LastIndex(name, "node_modules/"); idx != -1 {
		return name[idx+len("node_modules/"):]
	}
	return name
}

func appendPackageLockDependencies(result *[]string, seen map[string]bool, deps map[string]packageLockDependency, depth int) {
	if depth >= maxPackageLockDependencyDepth {
		return
	}
	for name, dep := range deps {
		if dep.Version != "" {
			spec := name + "@" + dep.Version
			if !seen[spec] {
				seen[spec] = true
				*result = append(*result, spec)
			}
		}
		appendPackageLockDependencies(result, seen, dep.Dependencies, depth+1)
	}
}

func readBunLock(dir string) []string {
	data, err := os.ReadFile(filepath.Join(dir, "bun.lock"))
	if err != nil {
		return nil
	}
	var lockfile struct {
		Packages map[string]json.RawMessage `json:"packages"`
	}
	if err := unmarshalBunLock(data, &lockfile); err != nil {
		return nil
	}
	seen := make(map[string]bool, len(lockfile.Packages))
	var result []string
	for key, raw := range lockfile.Packages {
		spec := bunPackageSpec(key, raw)
		if spec == "" || seen[spec] {
			continue
		}
		seen[spec] = true
		result = append(result, spec)
	}
	return result
}

func bunPackageSpec(key string, raw json.RawMessage) string {
	lastSegment := key[strings.LastIndex(key, "/")+1:]
	atIdx := strings.LastIndex(lastSegment, "@")
	if atIdx > 0 {
		return key
	}
	var parts []json.RawMessage
	if json.Unmarshal(raw, &parts) != nil || len(parts) == 0 {
		return ""
	}
	var nameVersion string
	if json.Unmarshal(parts[0], &nameVersion) != nil {
		return ""
	}
	atIdx = strings.LastIndex(nameVersion, "@")
	if atIdx <= 0 {
		return ""
	}
	return nameVersion
}

func readPNPMLock(dir string) []string {
	data, err := os.ReadFile(filepath.Join(dir, "pnpm-lock.yaml"))
	if err != nil {
		return nil
	}
	seen := make(map[string]bool)
	var result []string
	inPackages := false
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := scanner.Text()
		if line == "packages:" {
			inPackages = true
			continue
		}
		if inPackages && len(line) > 0 && line[0] != ' ' && !strings.HasPrefix(line, "#") {
			inPackages = false
			continue
		}
		if !inPackages || !strings.HasPrefix(line, "  ") || strings.HasPrefix(line, "   ") {
			continue
		}
		trimmed := strings.TrimSuffix(strings.TrimSpace(line), ":")
		trimmed = trimYAMLKeyQuotes(trimmed)
		trimmed = strings.TrimPrefix(trimmed, "/")
		trimmed = strings.SplitN(trimmed, "(", 2)[0]
		atIdx := strings.LastIndex(trimmed, "@")
		if atIdx <= 0 {
			continue
		}
		name, version := trimmed[:atIdx], trimmed[atIdx+1:]
		spec := name + "@" + version
		if !seen[spec] {
			seen[spec] = true
			result = append(result, spec)
		}
	}
	return result
}

func trimYAMLKeyQuotes(key string) string {
	if len(key) < 2 {
		return key
	}
	first := key[0]
	last := key[len(key)-1]
	isQuoted := first == last && (first == '\'' || first == '"')
	if isQuoted {
		return key[1 : len(key)-1]
	}
	return key
}

// Go: go.sum

func readGoSum(dir string) []string {
	data, err := os.ReadFile(filepath.Join(dir, "go.sum"))
	if err != nil {
		return nil
	}
	seen := make(map[string]bool)
	var result []string
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 {
			continue
		}
		mod := fields[0]
		ver := strings.TrimSuffix(fields[1], "/go.mod")
		key := mod + "@" + ver
		if seen[key] {
			continue
		}
		seen[key] = true
		result = append(result, key)
	}
	return result
}

// Python: uv.lock → poetry.lock → Pipfile.lock

func readPyLockfile(dir string) []string {
	if pkgs := readUVLock(dir); len(pkgs) > 0 {
		return pkgs
	}
	if pkgs := readPoetryLock(dir); len(pkgs) > 0 {
		return pkgs
	}
	return readPipfileLock(dir)
}

func readPythonManagerLockfile(name, dir string) []string {
	switch name {
	case "uv":
		return readUVLock(dir)
	case "poetry":
		return readPoetryLock(dir)
	case "pip", "pip3":
		return readPipfileLock(dir)
	default:
		return readPyLockfile(dir)
	}
}

func readUVLock(dir string) []string {
	return parsePoetryFormat(filepath.Join(dir, "uv.lock"))
}

func readPoetryLock(dir string) []string {
	return parsePoetryFormat(filepath.Join(dir, "poetry.lock"))
}

func parsePoetryFormat(path string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var result []string
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	var name, version string
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "[[package]]" {
			if name != "" && version != "" {
				result = append(result, name+"=="+version)
			}
			name, version = "", ""
			continue
		}
		k, v, ok := strings.Cut(line, " = ")
		if !ok {
			continue
		}
		v = strings.Trim(v, "\"")
		switch k {
		case "name":
			name = v
		case "version":
			version = v
		}
	}
	if name != "" && version != "" {
		result = append(result, name+"=="+version)
	}
	return result
}

func readPipfileLock(dir string) []string {
	data, err := os.ReadFile(filepath.Join(dir, "Pipfile.lock"))
	if err != nil {
		return nil
	}
	var lockfile map[string]map[string]struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(data, &lockfile); err != nil {
		return nil
	}
	seen := make(map[string]bool)
	var result []string
	for section, pkgs := range lockfile {
		if section == "_meta" {
			continue
		}
		for name, pkg := range pkgs {
			ver := strings.TrimPrefix(pkg.Version, "==")
			spec := name
			if ver != "" {
				spec = name + "==" + ver
			}
			if seen[spec] {
				continue
			}
			seen[spec] = true
			result = append(result, spec)
		}
	}
	return result
}

// Homebrew: Brewfile.lock.json

func readBrewfileLockJSON(dir string) []string {
	data, err := os.ReadFile(filepath.Join(dir, "Brewfile.lock.json"))
	if err != nil {
		return nil
	}
	var lockfile struct {
		Entries struct {
			Brew map[string]struct {
				Version string `json:"version"`
			} `json:"brew"`
		} `json:"entries"`
	}
	if err := json.Unmarshal(data, &lockfile); err != nil {
		return nil
	}
	result := make([]string, 0, len(lockfile.Entries.Brew))
	for name, pkg := range lockfile.Entries.Brew {
		if pkg.Version != "" {
			result = append(result, name+brewLockVersionSeparator+pkg.Version)
		} else {
			result = append(result, name)
		}
	}
	return result
}
