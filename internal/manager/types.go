package manager

type Manager struct {
	Name        string   `json:"name"`
	Ecosystem   string   `json:"ecosystem"`
	InstallCmds []string `json:"installCmds"`
}
