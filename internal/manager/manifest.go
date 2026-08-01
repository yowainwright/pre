package manager

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

const maxRequirementsDepth = 20

var (
	cargoVersionFieldRE = regexp.MustCompile(`(?:^|[{,]\s*)version\s*=\s*["']([^"']+)["']`)
	cargoPackageFieldRE = regexp.MustCompile(`(?:^|[{,]\s*)package\s*=\s*["']([^"']+)["']`)
	cargoSourceFieldRE  = regexp.MustCompile(`(?:[{,.]\s*)(?:path|git|registry)\s*=`)
	cargoWorkspaceRE    = regexp.MustCompile(`(?:[{,.]\s*)workspace\s*=\s*true(?:\s*[,}]|\s*$)`)
)

type cargoManifestDependency struct {
	name        string
	packageName string
	version     string
	external    bool
	inherited   bool
}

type cargoManifestState struct {
	packages            []string
	seen                map[string]bool
	workspaceSpecs      map[string]string
	inheritedNames      []string
	inheritedSeen       map[string]bool
	section             string
	inDependencies      bool
	inWorkspaceDeps     bool
	inSourceOverrides   bool
	table               cargoManifestDependency
	unsupportedSource   bool
	unsupportedSyntax   bool
	inheritedDependency bool
	hasWorkspace        bool
}

func ReadManifest(mgr *Manager) []string {
	if pkgs := ReadLockfile(mgr, "."); len(pkgs) > 0 {
		return pkgs
	}
	return readManifestDir(mgr, ".")
}

func readManifestDir(mgr *Manager, dir string) []string {
	switch mgr.Ecosystem {
	case "npm":
		return readPackageJSON(dir)
	case "Go":
		return readGoMod(dir)
	case "PyPI":
		return readRequirementsTxt(dir)
	case "Homebrew":
		return readBrewfile(dir)
	case "crates.io":
		return readCargoToml(dir)
	}
	return nil
}

func readPackageJSON(dir string) []string {
	data, err := os.ReadFile(dir + "/package.json")
	if err != nil {
		return nil
	}
	var pkg struct {
		Dependencies         map[string]string `json:"dependencies"`
		DevDependencies      map[string]string `json:"devDependencies"`
		OptionalDependencies map[string]string `json:"optionalDependencies"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil
	}
	count := len(pkg.Dependencies) + len(pkg.DevDependencies) + len(pkg.OptionalDependencies)
	seen := make(map[string]bool, count)
	names := make([]string, 0, count)
	names = appendNPMDependencySpecs(names, seen, pkg.Dependencies)
	names = appendNPMDependencySpecs(names, seen, pkg.DevDependencies)
	names = appendNPMDependencySpecs(names, seen, pkg.OptionalDependencies)
	return names
}

func appendNPMDependencySpecs(names []string, seen map[string]bool, deps map[string]string) []string {
	for name, spec := range deps {
		dependency := npmDependencySpec(name, spec)
		if dependency == "" || seen[name] {
			continue
		}
		seen[name] = true
		names = append(names, dependency)
	}
	return names
}

func readGoMod(dir string) []string {
	data, err := os.ReadFile(dir + "/go.mod")
	if err != nil {
		return nil
	}
	var names []string
	inRequire := false
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "require (" {
			inRequire = true
			continue
		}
		if inRequire && line == ")" {
			inRequire = false
			continue
		}
		var spec string
		if inRequire {
			spec = line
		} else if strings.HasPrefix(line, "require ") {
			spec = strings.TrimPrefix(line, "require ")
		} else {
			continue
		}
		if idx := strings.Index(spec, "//"); idx != -1 {
			spec = strings.TrimSpace(spec[:idx])
		}
		parts := strings.Fields(spec)
		if len(parts) >= 2 {
			names = append(names, parts[0]+"@"+parts[1])
		}
	}
	return names
}

func readRequirementsTxt(dir string) []string {
	names, err := ReadRequirementsFile(filepath.Join(dir, "requirements.txt"))
	if err != nil {
		return nil
	}
	return names
}

func ReadRequirementsFile(path string) ([]string, error) {
	return readRequirementsFile(path, make(map[string]bool), 0)
}

func readRequirementsFile(path string, active map[string]bool, depth int) ([]string, error) {
	if depth >= maxRequirementsDepth {
		return nil, fmt.Errorf("requirements include depth exceeds %d", maxRequirementsDepth)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	if active[absolute] {
		return nil, fmt.Errorf("cyclic requirements include: %s", path)
	}
	data, err := os.ReadFile(absolute)
	if err != nil {
		return nil, err
	}
	active[absolute] = true
	defer delete(active, absolute)
	return parseRequirements(data, filepath.Dir(absolute), active, depth)
}

func parseRequirements(data []byte, dir string, active map[string]bool, depth int) ([]string, error) {
	var names []string
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := normalizeRequirement(scanner.Text())
		updated, err := appendRequirementLine(names, line, dir, active, depth)
		if err != nil {
			return nil, err
		}
		names = updated
	}
	return names, scanner.Err()
}

func appendRequirementLine(names []string, line, dir string, active map[string]bool, depth int) ([]string, error) {
	if include, ok := requirementInclude(line); ok {
		nestedPath := requirementIncludePath(dir, include)
		nested, err := readRequirementsFile(nestedPath, active, depth+1)
		if err != nil {
			return nil, err
		}
		return append(names, nested...), nil
	}
	ignored := line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "-")
	name, _ := parsePySpec(line)
	if ignored || name == "" {
		return names, nil
	}
	return append(names, line), nil
}

func requirementIncludePath(dir, include string) string {
	if filepath.IsAbs(include) {
		return include
	}
	return filepath.Join(dir, include)
}

func normalizeRequirement(line string) string {
	line = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(line), `\`))
	for _, separator := range []string{" #", " ;", " --hash="} {
		if index := strings.Index(line, separator); index != -1 {
			line = line[:index]
		}
	}
	return strings.TrimSpace(line)
}

func requirementInclude(line string) (string, bool) {
	for _, prefix := range []string{"-r ", "--requirement ", "--requirements "} {
		if path, ok := strings.CutPrefix(line, prefix); ok {
			return strings.TrimSpace(path), true
		}
	}
	for _, prefix := range []string{"-r", "--requirement=", "--requirements="} {
		if path, ok := strings.CutPrefix(line, prefix); ok && path != "" {
			return strings.TrimSpace(path), true
		}
	}
	return "", false
}

func readBrewfile(dir string) []string {
	data, err := os.ReadFile(dir + "/Brewfile")
	if err != nil {
		return nil
	}
	var names []string
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		rest, ok := strings.CutPrefix(line, `brew "`)
		if !ok {
			continue
		}
		if name, _, found := strings.Cut(rest, `"`); found && name != "" {
			names = append(names, name)
		}
	}
	return names
}

func readCargoToml(dir string) []string {
	manifestPath := filepath.Join(dir, "Cargo.toml")
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return nil
	}
	return parseCargoToml(data)
}

func parseCargoToml(data []byte) []string {
	state, err := parseCargoTomlState(data)
	if err != nil {
		return nil
	}
	return state.packages
}

func parseCargoTomlState(data []byte) (cargoManifestState, error) {
	seen := make(map[string]bool)
	workspaceSpecs := make(map[string]string)
	inheritedSeen := make(map[string]bool)
	state := cargoManifestState{
		seen:           seen,
		workspaceSpecs: workspaceSpecs,
		inheritedSeen:  inheritedSeen,
	}
	reader := strings.NewReader(string(data))
	scanner := bufio.NewScanner(reader)
	for scanner.Scan() {
		line := normalizeCargoTomlLine(scanner.Text())
		state.consume(line)
	}
	state.flushTable()
	return state, scanner.Err()
}

func normalizeCargoTomlLine(line string) string {
	commentIndex := strings.Index(line, "#")
	if commentIndex >= 0 {
		line = line[:commentIndex]
	}
	return strings.TrimSpace(line)
}

func (s *cargoManifestState) consume(line string) {
	section, isSection := cargoTomlSection(line)
	if isSection {
		s.startSection(section)
		return
	}
	if s.table.name != "" {
		s.consumeTableLine(line)
		return
	}
	if s.consumeSpecialLine(line) {
		return
	}
	name, spec := cargoDependencyLine(line)
	s.appendDependency(name, spec)
}

func (s *cargoManifestState) consumeSpecialLine(line string) bool {
	if s.section == "" && isUnsupportedCargoRootAssignment(line) {
		s.unsupportedSyntax = true
		return true
	}
	if s.inSourceOverrides {
		if _, _, ok := strings.Cut(line, "="); ok {
			s.unsupportedSource = true
		}
		return true
	}
	if !s.inDependencies {
		return true
	}
	if cargoSourceFieldRE.MatchString(line) {
		s.unsupportedSource = true
		return true
	}
	if cargoWorkspaceRE.MatchString(line) {
		s.appendInherited(cargoInheritedDependencyName(line))
		return true
	}
	return false
}

func (s *cargoManifestState) startSection(section string) {
	s.flushTable()
	s.section = section
	s.inDependencies = isCargoDependencySection(section)
	s.inWorkspaceDeps = isCargoWorkspaceDependencySection(section)
	s.inSourceOverrides = isCargoSourceOverrideSection(section)
	s.hasWorkspace = s.hasWorkspace || section == "workspace"
	name := cargoDependencyTableName(section)
	if name != "" {
		s.table = cargoManifestDependency{name: name}
	}
}

func isUnsupportedCargoRootAssignment(line string) bool {
	key, _, ok := strings.Cut(line, "=")
	if !ok {
		return false
	}
	root, _, _ := strings.Cut(strings.TrimSpace(key), ".")
	root = trimCargoDependencyName(root)
	structuralRoots := []string{
		"dependencies", "dev-dependencies", "build-dependencies",
		"target", "workspace", "patch", "replace",
	}
	for _, candidate := range structuralRoots {
		if root == candidate {
			return true
		}
	}
	return false
}

func isCargoSourceOverrideSection(section string) bool {
	isReplace := section == "replace" || strings.HasPrefix(section, "replace.")
	isPatch := section == "patch" || strings.HasPrefix(section, "patch.")
	return isReplace || isPatch
}

func (s *cargoManifestState) consumeTableLine(line string) {
	key, value, ok := strings.Cut(line, "=")
	if !ok {
		return
	}
	key = strings.TrimSpace(key)
	stringValue := cargoSimpleVersion(value)
	switch key {
	case "version":
		s.table.version = stringValue
	case "package":
		s.table.packageName = stringValue
	case "path", "git", "registry":
		s.table.external = true
	case "workspace":
		s.table.inherited = strings.TrimSpace(value) == "true"
	}
}

func (s *cargoManifestState) flushTable() {
	dependency := s.table
	s.table = cargoManifestDependency{}
	if dependency.external {
		s.unsupportedSource = true
		return
	}
	if dependency.inherited {
		s.appendInherited(dependency.name)
		return
	}
	if dependency.name == "" || dependency.version == "" {
		return
	}
	name := dependency.name
	if dependency.packageName != "" {
		name = dependency.packageName
	}
	version := cargoRequirementVersion(dependency.version)
	s.appendDependency(dependency.name, name+"@"+version)
}

func (s *cargoManifestState) appendDependency(name, spec string) {
	if spec == "" {
		return
	}
	s.appendSpec(spec)
	if s.inWorkspaceDeps {
		s.workspaceSpecs[name] = spec
	}
}

func (s *cargoManifestState) appendInherited(name string) {
	s.inheritedDependency = true
	if name == "" || s.inheritedSeen[name] {
		return
	}
	s.inheritedSeen[name] = true
	s.inheritedNames = append(s.inheritedNames, name)
}

func (s *cargoManifestState) appendSpec(spec string) {
	if spec == "" || s.seen[spec] {
		return
	}
	s.seen[spec] = true
	s.packages = append(s.packages, spec)
}

func cargoTomlSection(line string) (string, bool) {
	hasBrackets := strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]")
	if !hasBrackets {
		return "", false
	}
	section := strings.Trim(line, "[]")
	return strings.TrimSpace(section), true
}

func isCargoDependencySection(section string) bool {
	sections := []string{"dependencies", "dev-dependencies", "build-dependencies", "workspace.dependencies"}
	for _, candidate := range sections {
		if section == candidate {
			return true
		}
	}
	if !strings.HasPrefix(section, "target.") {
		return false
	}
	for _, candidate := range sections[:3] {
		if strings.HasSuffix(section, "."+candidate) {
			return true
		}
	}
	return false
}

func isCargoWorkspaceDependencySection(section string) bool {
	return section == "workspace.dependencies" || strings.HasPrefix(section, "workspace.dependencies.")
}

func cargoDependencyTableName(section string) string {
	name := directCargoDependencyTableName(section)
	if name != "" {
		return name
	}
	return targetCargoDependencyTableName(section)
}

func directCargoDependencyTableName(section string) string {
	sections := []string{"dependencies", "dev-dependencies", "build-dependencies", "workspace.dependencies"}
	for _, candidate := range sections {
		prefix := candidate + "."
		if strings.HasPrefix(section, prefix) {
			name := strings.TrimPrefix(section, prefix)
			return trimCargoDependencyName(name)
		}
	}
	return ""
}

func targetCargoDependencyTableName(section string) string {
	if !strings.HasPrefix(section, "target.") {
		return ""
	}
	sections := []string{"dependencies", "dev-dependencies", "build-dependencies"}
	for _, candidate := range sections {
		marker := "." + candidate + "."
		index := strings.LastIndex(section, marker)
		if index >= 0 {
			name := section[index+len(marker):]
			return trimCargoDependencyName(name)
		}
	}
	return ""
}

func trimCargoDependencyName(name string) string {
	trimmed := strings.TrimSpace(name)
	return strings.Trim(trimmed, `"'`)
}

func cargoDependencyLine(line string) (string, string) {
	key, value, ok := strings.Cut(line, "=")
	if !ok {
		return "", ""
	}
	name := cargoVersionDependencyName(key)
	if name == "" {
		return "", ""
	}
	return name, cargoDependencySpec(name, value)
}

func cargoVersionDependencyName(key string) string {
	trimmed := strings.TrimSpace(key)
	if strings.HasSuffix(trimmed, ".version") {
		trimmed = strings.TrimSuffix(trimmed, ".version")
	}
	return trimCargoDependencyName(trimmed)
}

func cargoInheritedDependencyName(line string) string {
	name, _, ok := strings.Cut(line, "=")
	if !ok {
		return ""
	}
	name = strings.TrimSpace(name)
	name = strings.TrimSuffix(name, ".workspace")
	return trimCargoDependencyName(name)
}

func cargoDependencySpec(name, value string) string {
	if cargoSourceFieldRE.MatchString(value) {
		return ""
	}
	packageMatch := cargoPackageFieldRE.FindStringSubmatch(value)
	if len(packageMatch) == 2 {
		name = packageMatch[1]
	}
	version := cargoSimpleVersion(value)
	versionMatch := cargoVersionFieldRE.FindStringSubmatch(value)
	if len(versionMatch) == 2 {
		version = versionMatch[1]
	}
	if version == "" {
		return ""
	}
	version = cargoRequirementVersion(version)
	return name + "@" + version
}

func cargoRequirementVersion(version string) string {
	if version == "" {
		return ""
	}
	startsWithDigit := version[0] >= '0' && version[0] <= '9'
	if startsWithDigit {
		return "^" + version
	}
	return version
}

func cargoSimpleVersion(value string) string {
	trimmed := strings.TrimSpace(value)
	if len(trimmed) < 2 {
		return ""
	}
	quote := trimmed[0]
	lastQuote := trimmed[len(trimmed)-1]
	isQuoted := quote == lastQuote && (quote == '"' || quote == '\'')
	if !isQuoted {
		return ""
	}
	return trimmed[1 : len(trimmed)-1]
}

func npmDependencySpec(name, spec string) string {
	spec = strings.TrimSpace(spec)
	if spec == "" || !supportedNPMRegistrySpec(spec) {
		return ""
	}
	return name + "@" + spec
}

func supportedNPMRegistrySpec(spec string) bool {
	for _, prefix := range []string{
		"file:", "git+", "github:", "workspace:", "link:", "npm:",
		"http://", "https://",
	} {
		if strings.HasPrefix(spec, prefix) {
			return false
		}
	}
	return !strings.HasPrefix(spec, "./") && !strings.HasPrefix(spec, "../") && !strings.HasPrefix(spec, "/")
}
