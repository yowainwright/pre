package config

import (
	"encoding/json"
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
