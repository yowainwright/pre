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
	if d, err := time.ParseDuration(s); err == nil && d >= 0 {
		configuredSystemScanTTL = d
	}
}

func systemScanTTL() time.Duration {
	return configuredSystemScanTTL
}

type SystemStats struct {
	Crit          int       `json:"crit"`
	Warn          int       `json:"warn"`
	Errors        int       `json:"errors,omitempty"`
	Total         int       `json:"total"`
	LastUpdated   time.Time `json:"lastUpdated"`
	LastAttempted time.Time `json:"lastAttempted,omitempty"`
}

func shouldRunSystemScan() bool {
	s := loadSystemStatsFn()
	ttl := systemScanTTL()
	if ttl <= 0 {
		return true
	}

	now := time.Now()
	lastRun := s.LastUpdated
	if s.LastAttempted.After(lastRun) {
		lastRun = s.LastAttempted
	}
	return lastRun.IsZero() || lastRun.After(now) || now.Sub(lastRun) > ttl
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
	now := time.Now()
	s.LastAttempted = now
	if s.Errors == 0 {
		s.LastUpdated = now
	} else if s.LastUpdated.IsZero() {
		s.LastUpdated = loadSystemStats().LastUpdated
	}
	path, err := systemStatsPath()
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return
	}
	data, _ := json.Marshal(s)
	_ = fileutil.AtomicWriteFile(path, data, 0600)
}
