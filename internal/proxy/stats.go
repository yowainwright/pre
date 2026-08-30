package proxy

import (
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/yowainwright/pre/internal/fileutil"
	"github.com/yowainwright/pre/internal/obs"
)

type SystemStats struct {
	Crit          int       `json:"crit"`
	Warn          int       `json:"warn"`
	Errors        int       `json:"errors,omitempty"`
	Total         int       `json:"total"`
	LastUpdated   time.Time `json:"lastUpdated"`
	LastAttempted time.Time `json:"lastAttempted,omitempty"`
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
		recordSystemStatsEvent("pre.system_stats.load_failed", SystemStats{}, err)
		return SystemStats{}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) {
			recordSystemStatsEvent("pre.system_stats.load_failed", SystemStats{}, err)
		}
		return SystemStats{}
	}
	var s SystemStats
	if err := json.Unmarshal(data, &s); err != nil {
		recordSystemStatsEvent("pre.system_stats.load_failed", SystemStats{}, err)
		return SystemStats{}
	}
	recordSystemStatsEvent("pre.system_stats.loaded", s, nil)
	return s
}

func saveSystemStats(s SystemStats) {
	now := time.Now()
	s.LastAttempted = now
	if s.Errors == 0 {
		s.LastUpdated = now
	} else if s.LastUpdated.IsZero() {
		s.LastUpdated = loadSystemStatsFn().LastUpdated
	}
	path, err := systemStatsPath()
	if err != nil {
		recordSystemStatsEvent("pre.system_stats.write_failed", s, err)
		return
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		recordSystemStatsEvent("pre.system_stats.write_failed", s, err)
		return
	}
	data, _ := json.Marshal(s)
	if err := fileutil.AtomicWriteFile(path, data, 0600); err != nil {
		recordSystemStatsEvent("pre.system_stats.write_failed", s, err)
		return
	}
	recordSystemStatsEvent("pre.system_stats.written", s, nil)
}

func recordSystemStatsEvent(name string, stats SystemStats, err error) {
	attrs := map[string]any{
		"critical_count": stats.Crit,
		"warning_count":  stats.Warn,
		"error_count":    stats.Errors,
		"package_count":  stats.Total,
	}
	if err != nil {
		attrs["error_type"] = obs.ErrorType(err)
	}
	obs.Record(name, attrs)
}
