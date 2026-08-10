package manager

type Manager struct {
	Name        string   `json:"name"`
	Ecosystem   string   `json:"ecosystem"`
	InstallCmds []string `json:"installCmds"`
}

var builtins = []Manager{
	{Name: "brew", Ecosystem: "Homebrew", InstallCmds: []string{"install", "reinstall", "upgrade"}},
	{Name: "npm", Ecosystem: "npm", InstallCmds: []string{"install", "add", "i", "update", "ci"}},
	{Name: "pnpm", Ecosystem: "npm", InstallCmds: []string{"install", "add", "i", "update"}},
	{Name: "bun", Ecosystem: "npm", InstallCmds: []string{"install", "add", "i", "update"}},
	{Name: "go", Ecosystem: "Go", InstallCmds: []string{"get", "install"}},
	{Name: "cargo", Ecosystem: "crates.io", InstallCmds: []string{"add", "install", "update", "fetch"}},
	{Name: "pip", Ecosystem: "PyPI", InstallCmds: []string{"install"}},
	{Name: "pip3", Ecosystem: "PyPI", InstallCmds: []string{"install"}},
	{Name: "uv", Ecosystem: "PyPI", InstallCmds: []string{"add", "sync"}},
	{Name: "poetry", Ecosystem: "PyPI", InstallCmds: []string{"add", "update", "install"}},
}

var extraManagers []Manager

func SetUserManagers(mgrs []Manager) {
	extraManagers = mgrs
}

func All() []Manager {
	return mergeManagers(builtins, extraManagers)
}

func Get(name string) *Manager {
	for index := len(extraManagers) - 1; index >= 0; index-- {
		if extraManagers[index].Name == name {
			manager := extraManagers[index]
			return &manager
		}
	}
	for index := range builtins {
		if builtins[index].Name == name {
			manager := builtins[index]
			return &manager
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
