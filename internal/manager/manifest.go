package manager

import (
	"bufio"
	"encoding/json"
	"os"
	"strings"
)

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
	}
	return nil
}

func readPackageJSON(dir string) []string {
	data, err := os.ReadFile(dir + "/package.json")
	if err != nil {
		return nil
	}
	var pkg struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal(data, &pkg); err != nil {
		return nil
	}
	seen := make(map[string]bool, len(pkg.Dependencies)+len(pkg.DevDependencies))
	names := make([]string, 0, len(pkg.Dependencies)+len(pkg.DevDependencies))
	for name, spec := range pkg.Dependencies {
		if !seen[name] {
			seen[name] = true
			names = append(names, npmDependencySpec(name, spec))
		}
	}
	for name, spec := range pkg.DevDependencies {
		if !seen[name] {
			seen[name] = true
			names = append(names, npmDependencySpec(name, spec))
		}
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
	data, err := os.ReadFile(dir + "/requirements.txt")
	if err != nil {
		return nil
	}
	var names []string
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "-") {
			continue
		}
		if name, _ := parsePySpec(line); name != "" {
			names = append(names, line)
		}
	}
	return names
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

func npmDependencySpec(name, spec string) string {
	spec = strings.TrimSpace(spec)
	if spec == "" || !supportedNPMRegistrySpec(spec) {
		return name
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
