package proxy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/yowainwright/pre/internal/fileutil"
)

const defaultSystemScanTTL = 7 * 24 * time.Hour

var configuredSystemScanTTL = defaultSystemScanTTL

func SetSystemScanTTL(s string) {
	if s == "" {
		return
	}
	if d, err := time.ParseDuration(s); err == nil {
		configuredSystemScanTTL = d
	}
}

func systemScanTTL() time.Duration {
	return configuredSystemScanTTL
}

type SystemStats struct {
	Crit        int       `json:"crit"`
	Warn        int       `json:"warn"`
	Total       int       `json:"total"`
	LastUpdated time.Time `json:"lastUpdated"`
}

func shouldRunSystemScan() bool {
	s := loadSystemStatsFn()
	return s.LastUpdated.IsZero() || time.Since(s.LastUpdated) > systemScanTTL()
}

var (
	loadSystemStatsFn = loadSystemStats
	saveSystemStatsFn = saveSystemStats
	statsCacheDirFn   = os.UserCacheDir
)

func systemStatsPath() (string, error) {
	dir, err := statsCacheDirFn()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "pre", "system.json"), nil
}

func LoadSystemStats() SystemStats { return loadSystemStats() }

func loadSystemStats() SystemStats {
	path, err := systemStatsPath()
	if err != nil {
		return SystemStats{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return SystemStats{}
	}
	var s SystemStats
	if err := json.Unmarshal(data, &s); err != nil {
		return SystemStats{}
	}
	return s
}

func saveSystemStats(s SystemStats) {
	s.LastUpdated = time.Now()
	path, err := systemStatsPath()
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return
	}
	data, _ := json.Marshal(s)
	_ = fileutil.AtomicWriteFile(path, data, 0644)
}
