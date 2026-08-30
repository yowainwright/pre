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

func TestSaveAndLoadSystemStats(t *testing.T) {
	withObsDir(t)
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
	if s.LastAttempted.IsZero() {
		t.Error("expected LastAttempted to be set")
	}
	written := requireObsEvent(t, "pre.system_stats.written")
	loaded := requireObsEvent(t, "pre.system_stats.loaded")
	if written["package_count"] != float64(10) || loaded["package_count"] != float64(10) {
		t.Fatalf("unexpected stats obs: written=%#v loaded=%#v", written, loaded)
	}
}

func TestSaveSystemStatsWithErrorsDoesNotAdvanceLastUpdated(t *testing.T) {
	dir := t.TempDir()
	defer withStatsCacheDir(dir)()

	saveSystemStats(SystemStats{Total: 1})
	first := loadSystemStats()
	if first.LastUpdated.IsZero() {
		t.Fatal("expected initial successful timestamp")
	}

	time.Sleep(2 * time.Millisecond)
	saveSystemStats(SystemStats{Errors: 1, Total: 1})
	failed := loadSystemStats()

	if !failed.LastUpdated.Equal(first.LastUpdated) {
		t.Errorf("expected failed scan to preserve LastUpdated, got %s want %s", failed.LastUpdated, first.LastUpdated)
	}
	if !failed.LastAttempted.After(first.LastAttempted) {
		t.Error("expected failed scan to update LastAttempted")
	}
}

func TestSaveSystemStatsWithErrorsUsesLoadSystemStatsFn(t *testing.T) {
	dir := t.TempDir()
	defer withStatsCacheDir(dir)()

	priorUpdated := time.Now().Add(-time.Hour).Round(0)
	called := false
	orig := loadSystemStatsFn
	loadSystemStatsFn = func() SystemStats {
		called = true
		return SystemStats{LastUpdated: priorUpdated}
	}
	defer func() { loadSystemStatsFn = orig }()

	saveSystemStats(SystemStats{Errors: 1, Total: 1})

	if !called {
		t.Fatal("expected saveSystemStats to load prior stats through loadSystemStatsFn")
	}
	saved := loadSystemStats()
	if !saved.LastUpdated.Equal(priorUpdated) {
		t.Errorf("expected injected LastUpdated to be preserved, got %s want %s", saved.LastUpdated, priorUpdated)
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
	withObsDir(t)
	orig := statsCacheDirFn
	statsCacheDirFn = func() (string, error) { return "", errors.New("no dir") }
	defer func() { statsCacheDirFn = orig }()

	s := loadSystemStats()
	if s.Total != 0 || !s.LastUpdated.IsZero() {
		t.Errorf("expected empty stats on path error, got %+v", s)
	}
	event := requireObsEvent(t, "pre.system_stats.load_failed")
	if event["error_type"] == "" {
		t.Fatalf("expected error type, got %#v", event)
	}
}

func TestSaveSystemStatsDirError(t *testing.T) {
	withObsDir(t)
	orig := statsCacheDirFn
	statsCacheDirFn = func() (string, error) { return "", errors.New("no dir") }
	defer func() { statsCacheDirFn = orig }()

	saveSystemStats(SystemStats{Total: 5})
	event := requireObsEvent(t, "pre.system_stats.write_failed")
	if event["package_count"] != float64(5) || event["error_type"] == "" {
		t.Fatalf("unexpected write failure event: %#v", event)
	}
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
