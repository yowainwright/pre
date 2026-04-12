package manager

type Manager struct {
	Name        string   `json:"name"`
	Ecosystem   string   `json:"ecosystem"`
	InstallCmds []string `json:"installCmds"`
}

var builtins = []Manager{
	{Name: "brew", Ecosystem: "Homebrew", InstallCmds: []string{"install", "reinstall"}},
	{Name: "npm", Ecosystem: "npm", InstallCmds: []string{"install", "add", "i"}},
	{Name: "pnpm", Ecosystem: "npm", InstallCmds: []string{"install", "add", "i"}},
	{Name: "bun", Ecosystem: "npm", InstallCmds: []string{"install", "add", "i"}},
	{Name: "go", Ecosystem: "Go", InstallCmds: []string{"get", "install"}},
	{Name: "pip", Ecosystem: "PyPI", InstallCmds: []string{"install"}},
	{Name: "pip3", Ecosystem: "PyPI", InstallCmds: []string{"install"}},
	{Name: "uv", Ecosystem: "PyPI", InstallCmds: []string{"add", "install"}},
	{Name: "poetry", Ecosystem: "PyPI", InstallCmds: []string{"add"}},
}

var extraManagers []Manager

func SetUserManagers(mgrs []Manager) {
	extraManagers = mgrs
}

func All() []Manager {
	return mergeManagers(builtins, extraManagers)
}

func Get(name string) *Manager {
	for _, m := range All() {
		if m.Name == name {
			return &m
		}
	}
	return nil
}

func mergeManagers(base, extra []Manager) []Manager {
	result := make([]Manager, len(base))
	copy(result, base)
	for _, e := range extra {
		replaced := false
		for i, b := range result {
			if b.Name == e.Name {
				result[i] = e
				replaced = true
				break
			}
		}
		if !replaced {
			result = append(result, e)
		}
	}
	return result
}
