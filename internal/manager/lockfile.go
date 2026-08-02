package manager

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

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
	active            bool
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
	content := string(data)
	reader := strings.NewReader(content)
	scanner := bufio.NewScanner(reader)
	state := newCargoLockState()
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		state.consume(line)
	}
	state.flush()
	return state, scanner.Err()
}

func newCargoLockState() cargoLockState {
	seen := make(map[string]bool)
	return cargoLockState{seen: seen}
}

func (s *cargoLockState) consume(line string) {
	if isCargoLockPackageHeader(line) {
		s.flush()
		s.active = true
		return
	}
	if !s.active {
		return
	}
	if strings.HasPrefix(line, "[") {
		s.flush()
		s.active = false
		return
	}
	s.consumeField(line)
}

func isCargoLockPackageHeader(line string) bool {
	if !strings.HasPrefix(line, "[[") || !strings.HasSuffix(line, "]]") {
		return false
	}
	name := strings.TrimSpace(line[2 : len(line)-2])
	return strings.Trim(name, `"'`) == "package"
}

func (s *cargoLockState) consumeField(line string) {
	key, value, ok := strings.Cut(line, "=")
	if !ok {
		return
	}
	key = strings.TrimSpace(key)
	trimmedValue := cargoLockString(value)
	s.set(key, trimmedValue)
}

func cargoLockString(value string) string {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) < 2 || (trimmed[0] != '"' && trimmed[0] != '\'') {
		return ""
	}
	quote := trimmed[0]
	escaped := false
	for index := 1; index < len(trimmed); index++ {
		if trimmed[index] == quote && !escaped {
			return trimmed[1:index]
		}
		escaped = trimmed[index] == '\\' && !escaped
		if trimmed[index] != '\\' {
			escaped = false
		}
	}
	return ""
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

type packageLockDependency struct {
	Version      string                           `json:"version"`
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
	data, err := os.ReadFile(filepath.Join(dir, "package-lock.json"))
	if err != nil {
		return nil
	}
	var lockfile struct {
		Packages map[string]struct {
			Version string `json:"version"`
		} `json:"packages"`
		Dependencies map[string]packageLockDependency `json:"dependencies"`
	}
	if err := json.Unmarshal(data, &lockfile); err != nil {
		return nil
	}
	seen := make(map[string]bool, len(lockfile.Packages)+len(lockfile.Dependencies))
	var result []string
	if len(lockfile.Packages) > 0 {
		for path, pkg := range lockfile.Packages {
			if path == "" || pkg.Version == "" {
				continue
			}
			name := strings.TrimPrefix(path, "node_modules/")
			if idx := strings.LastIndex(name, "node_modules/"); idx != -1 {
				name = name[idx+len("node_modules/"):]
			}
			spec := name + "@" + pkg.Version
			if seen[spec] {
				continue
			}
			seen[spec] = true
			result = append(result, spec)
		}
		if len(result) > 0 {
			return result
		}
	}
	appendPackageLockDependencies(&result, seen, lockfile.Dependencies, 0)
	return result
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
	atIdx := strings.LastIndex(key, "@")
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
		trimmed = strings.TrimPrefix(trimmed, "/")
		atIdx := strings.LastIndex(trimmed, "@")
		if atIdx <= 0 {
			continue
		}
		name, version := trimmed[:atIdx], trimmed[atIdx+1:]
		version = strings.SplitN(version, "(", 2)[0]
		spec := name + "@" + version
		if !seen[spec] {
			seen[spec] = true
			result = append(result, spec)
		}
	}
	return result
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
