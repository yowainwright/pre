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
		return readNPMLockfile(dir)
	case "Go":
		return readGoSum(dir)
	case "PyPI":
		return readPyLockfile(dir)
	case "Homebrew":
		return readBrewfileLockJSON(dir)
	}
	return nil
}

// npm: package-lock.json → bun.lock → pnpm-lock.yaml

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
	}
	if err := json.Unmarshal(data, &lockfile); err != nil {
		return nil
	}
	seen := make(map[string]bool, len(lockfile.Packages))
	var result []string
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
	return result
}

func readBunLock(dir string) []string {
	data, err := os.ReadFile(filepath.Join(dir, "bun.lock"))
	if err != nil {
		return nil
	}
	content := string(data)
	if idx := strings.Index(content, "{"); idx != -1 {
		content = content[idx:]
	}
	var lockfile struct {
		Packages map[string]json.RawMessage `json:"packages"`
	}
	if err := json.Unmarshal([]byte(content), &lockfile); err != nil {
		return nil
	}
	seen := make(map[string]bool, len(lockfile.Packages))
	var result []string
	for key := range lockfile.Packages {
		atIdx := strings.LastIndex(key, "@")
		if atIdx <= 0 {
			continue
		}
		name := key[:atIdx]
		version := key[atIdx+1:]
		spec := name + "@" + version
		if !seen[spec] {
			seen[spec] = true
			result = append(result, spec)
		}
	}
	return result
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
			result = append(result, name+"@"+pkg.Version)
		} else {
			result = append(result, name)
		}
	}
	return result
}
