package proxy

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func withStatsCacheDir(dir string) func() {
	orig := statsCacheDirFn
	statsCacheDirFn = func() (string, error) { return dir, nil }
	return func() { statsCacheDirFn = orig }
}

func TestSetSystemScanTTLValid(t *testing.T) {
	orig := configuredSystemScanTTL
	defer func() { configuredSystemScanTTL = orig }()

	SetSystemScanTTL("48h")
	if configuredSystemScanTTL != 48*time.Hour {
		t.Errorf("expected 48h, got %v", configuredSystemScanTTL)
	}
}

func TestSetSystemScanTTLEmpty(t *testing.T) {
	orig := configuredSystemScanTTL
	defer func() { configuredSystemScanTTL = orig }()

	SetSystemScanTTL("")
	if configuredSystemScanTTL != orig {
		t.Error("empty string should not change TTL")
	}
}

func TestSetSystemScanTTLInvalid(t *testing.T) {
	orig := configuredSystemScanTTL
	defer func() { configuredSystemScanTTL = orig }()

	SetSystemScanTTL("notaduration")
	if configuredSystemScanTTL != orig {
		t.Error("invalid duration should not change TTL")
	}
}

func TestShouldRunSystemScanNeverRun(t *testing.T) {
	dir := t.TempDir()
	defer withStatsCacheDir(dir)()
	orig := loadSystemStatsFn
	loadSystemStatsFn = loadSystemStats
	defer func() { loadSystemStatsFn = orig }()

	if !shouldRunSystemScan() {
		t.Error("expected true when stats file missing (never run)")
	}
}

func TestShouldRunSystemScanRecent(t *testing.T) {
	dir := t.TempDir()
	defer withStatsCacheDir(dir)()
	orig := loadSystemStatsFn
	loadSystemStatsFn = loadSystemStats
	defer func() { loadSystemStatsFn = orig }()

	saveSystemStats(SystemStats{Total: 5})

	origTTL := configuredSystemScanTTL
	configuredSystemScanTTL = 24 * time.Hour
	defer func() { configuredSystemScanTTL = origTTL }()

	if shouldRunSystemScan() {
		t.Error("expected false when scan ran recently")
	}
}

func TestShouldRunSystemScanExpired(t *testing.T) {
	dir := t.TempDir()
	defer withStatsCacheDir(dir)()
	orig := loadSystemStatsFn
	loadSystemStatsFn = loadSystemStats
	defer func() { loadSystemStatsFn = orig }()

	s := SystemStats{Total: 5}
	saveSystemStats(s)

	origTTL := configuredSystemScanTTL
	configuredSystemScanTTL = 1 * time.Nanosecond
	defer func() { configuredSystemScanTTL = origTTL }()

	time.Sleep(2 * time.Millisecond)

	if !shouldRunSystemScan() {
		t.Error("expected true when scan TTL has expired")
	}
}

func TestSaveAndLoadSystemStats(t *testing.T) {
	dir := t.TempDir()
	defer withStatsCacheDir(dir)()

	saveSystemStats(SystemStats{Crit: 2, Warn: 5, Total: 10})
	s := loadSystemStats()

	if s.Crit != 2 || s.Warn != 5 || s.Total != 10 {
		t.Errorf("stats not persisted correctly: %+v", s)
	}
	if s.LastUpdated.IsZero() {
		t.Error("expected LastUpdated to be set")
	}
}

func TestLoadSystemStatsMissing(t *testing.T) {
	dir := t.TempDir()
	defer withStatsCacheDir(dir)()

	s := loadSystemStats()
	if s.Total != 0 || !s.LastUpdated.IsZero() {
		t.Errorf("expected zero stats for missing file, got %+v", s)
	}
}

func TestSystemStatsPathError(t *testing.T) {
	orig := statsCacheDirFn
	statsCacheDirFn = func() (string, error) { return "", errors.New("no dir") }
	defer func() { statsCacheDirFn = orig }()

	s := loadSystemStats()
	if s.Total != 0 || !s.LastUpdated.IsZero() {
		t.Errorf("expected empty stats on path error, got %+v", s)
	}
}

func TestSaveSystemStatsDirError(t *testing.T) {
	orig := statsCacheDirFn
	statsCacheDirFn = func() (string, error) { return "", errors.New("no dir") }
	defer func() { statsCacheDirFn = orig }()

	saveSystemStats(SystemStats{Total: 5})
}

func TestSaveSystemStatsMkdirError(t *testing.T) {
	orig := statsCacheDirFn
	statsCacheDirFn = func() (string, error) { return "/dev/null", nil }
	defer func() { statsCacheDirFn = orig }()

	saveSystemStats(SystemStats{Total: 5})
}

func TestLoadSystemStatsBadJSON(t *testing.T) {
	dir := t.TempDir()
	defer withStatsCacheDir(dir)()

	os.MkdirAll(filepath.Join(dir, "pre"), 0755)
	os.WriteFile(filepath.Join(dir, "pre", "system.json"), []byte("not json"), 0644)

	s := loadSystemStats()
	if s.Total != 0 {
		t.Errorf("expected empty stats on bad JSON, got %+v", s)
	}
}

func TestLoadSystemStatsPublic(t *testing.T) {
	dir := t.TempDir()
	defer withStatsCacheDir(dir)()

	saveSystemStats(SystemStats{Crit: 1, Total: 3})
	s := LoadSystemStats()
	if s.Crit != 1 || s.Total != 3 {
		t.Errorf("LoadSystemStats returned wrong values: %+v", s)
	}
}
