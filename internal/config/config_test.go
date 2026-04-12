package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func withConfigDir(dir string) func() {
	orig := configDirFn
	configDirFn = func() (string, error) { return dir, nil }
	return func() { configDirFn = orig }
}

func writeConfig(t *testing.T, dir string, cfg *Config) {
	t.Helper()
	p := filepath.Join(dir, "pre")
	os.MkdirAll(p, 0755)
	data, _ := json.Marshal(cfg)
	os.WriteFile(filepath.Join(p, "config.json"), data, 0644)
}

func TestLoadDefaults(t *testing.T) {
	defer withConfigDir(t.TempDir())()
	cfg := Load()
	if cfg.API.Endpoint != DefaultEndpoint {
		t.Errorf("expected default endpoint, got %q", cfg.API.Endpoint)
	}
	if cfg.Cache.TTL != DefaultTTL {
		t.Errorf("expected default TTL, got %q", cfg.Cache.TTL)
	}
	if len(cfg.Managers) != 0 {
		t.Errorf("expected no managers, got %d", len(cfg.Managers))
	}
}

func TestLoadCustomEndpoint(t *testing.T) {
	dir := t.TempDir()
	defer withConfigDir(dir)()
	writeConfig(t, dir, &Config{
		API: APIConfig{Endpoint: "https://custom.example.com/api"},
	})
	cfg := Load()
	if cfg.API.Endpoint != "https://custom.example.com/api" {
		t.Errorf("expected custom endpoint, got %q", cfg.API.Endpoint)
	}
}

func TestLoadCustomTTL(t *testing.T) {
	dir := t.TempDir()
	defer withConfigDir(dir)()
	writeConfig(t, dir, &Config{
		Cache: CacheConfig{TTL: "1h"},
	})
	cfg := Load()
	if cfg.Cache.TTL != "1h" {
		t.Errorf("expected 1h, got %q", cfg.Cache.TTL)
	}
}

func TestLoadCustomManagers(t *testing.T) {
	dir := t.TempDir()
	defer withConfigDir(dir)()
	writeConfig(t, dir, &Config{
		Managers: []ManagerConfig{
			{Name: "yarn", Ecosystem: "npm", InstallCmds: []string{"add"}},
		},
	})
	cfg := Load()
	if len(cfg.Managers) != 1 || cfg.Managers[0].Name != "yarn" {
		t.Errorf("expected yarn manager, got %v", cfg.Managers)
	}
}

func TestLoadBadJSON(t *testing.T) {
	dir := t.TempDir()
	defer withConfigDir(dir)()
	p := filepath.Join(dir, "pre")
	os.MkdirAll(p, 0755)
	os.WriteFile(filepath.Join(p, "config.json"), []byte("not json"), 0644)
	cfg := Load()
	if cfg.API.Endpoint != DefaultEndpoint {
		t.Error("expected defaults on bad JSON")
	}
}

func TestLoadMissingFile(t *testing.T) {
	defer withConfigDir(t.TempDir())()
	cfg := Load()
	if cfg.API.Endpoint != DefaultEndpoint {
		t.Error("expected defaults when file missing")
	}
}

func TestSaveAndReload(t *testing.T) {
	dir := t.TempDir()
	defer withConfigDir(dir)()
	cfg := defaults()
	cfg.API.Endpoint = "https://custom.example.com"
	cfg.SystemScan = true
	cfg.SystemTTL = "48h"
	if err := Save(cfg); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	loaded := Load()
	if loaded.API.Endpoint != "https://custom.example.com" {
		t.Errorf("endpoint not persisted, got %q", loaded.API.Endpoint)
	}
	if !loaded.SystemScan {
		t.Error("systemScan not persisted")
	}
	if loaded.SystemTTL != "48h" {
		t.Errorf("systemTTL not persisted, got %q", loaded.SystemTTL)
	}
}

func TestLoadConfigDirError(t *testing.T) {
	orig := configDirFn
	configDirFn = func() (string, error) { return "", errors.New("no dir") }
	defer func() { configDirFn = orig }()

	cfg := Load()
	if cfg.API.Endpoint != DefaultEndpoint {
		t.Error("expected defaults when config dir fn errors")
	}
}

func TestSaveConfigDirError(t *testing.T) {
	orig := configDirFn
	configDirFn = func() (string, error) { return "", errors.New("no dir") }
	defer func() { configDirFn = orig }()

	err := Save(defaults())
	if err == nil {
		t.Error("expected error when config dir fn errors")
	}
}

func TestSaveConfigMkdirError(t *testing.T) {
	orig := configDirFn
	configDirFn = func() (string, error) { return "/dev/null", nil }
	defer func() { configDirFn = orig }()

	err := Save(defaults())
	if err == nil {
		t.Error("expected error when MkdirAll fails")
	}
}

func TestSaveCreatesDirectory(t *testing.T) {
	dir := t.TempDir()
	defer withConfigDir(dir)()
	if err := Save(defaults()); err != nil {
		t.Fatalf("Save failed: %v", err)
	}
	p := filepath.Join(dir, "pre", "config.json")
	if _, err := os.Stat(p); err != nil {
		t.Errorf("expected config file to exist at %s: %v", p, err)
	}
}
