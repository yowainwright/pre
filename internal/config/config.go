package config

import (
	"encoding/json"
	"os"
	"path/filepath"

	"github.com/yowainwright/pre/internal/diagnostics"
	"github.com/yowainwright/pre/internal/fileutil"
)

const (
	DefaultEndpoint      = "https://api.osv.dev/v1/query"
	DefaultTTL           = "24h"
	DefaultSystemScanTTL = "168h"
)

type Config struct {
	API        APIConfig       `json:"api"`
	Cache      CacheConfig     `json:"cache"`
	Managers   []ManagerConfig `json:"managers"`
	SystemScan bool            `json:"systemScan"`
	SystemTTL  string          `json:"systemTTL"`
}

type APIConfig struct {
	Endpoint string `json:"endpoint"`
}

type CacheConfig struct {
	TTL string `json:"ttl"`
}

type ManagerConfig struct {
	Name        string   `json:"name"`
	Ecosystem   string   `json:"ecosystem"`
	InstallCmds []string `json:"installCmds"`
}

var (
	configDirFn     = os.UserConfigDir
	marshalIndentFn = json.MarshalIndent
)

func Load() *Config {
	cfg := defaults()
	p, err := configPath()
	if err != nil {
		recordConfigEvent("pre.config.load_failed", err, 0)
		return cfg
	}
	data, err := os.ReadFile(p)
	if err != nil {
		if !os.IsNotExist(err) {
			recordConfigEvent("pre.config.load_failed", err, 0)
		}
		return cfg
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		recordConfigEvent("pre.config.load_failed", err, len(data))
		return cfg
	}
	diagnostics.Record("pre.config.loaded", map[string]any{"config_bytes": len(data)})
	return cfg
}

func defaults() *Config {
	return &Config{
		API:       APIConfig{Endpoint: DefaultEndpoint},
		Cache:     CacheConfig{TTL: DefaultTTL},
		SystemTTL: DefaultSystemScanTTL,
	}
}

func Save(cfg *Config) error {
	p, err := configPath()
	if err != nil {
		recordConfigEvent("pre.config.write_failed", err, 0)
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		recordConfigEvent("pre.config.write_failed", err, 0)
		return err
	}
	data, err := marshalIndentFn(cfg, "", "  ")
	if err != nil {
		recordConfigEvent("pre.config.write_failed", err, 0)
		return err
	}
	if err := fileutil.AtomicWriteFile(p, data, 0600); err != nil {
		recordConfigEvent("pre.config.write_failed", err, len(data))
		return err
	}
	diagnostics.Record("pre.config.written", map[string]any{"config_bytes": len(data)})
	return nil
}

func Path() (string, error) {
	return configPath()
}

func configPath() (string, error) {
	dir, err := configDirFn()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "pre", "config.json"), nil
}

func recordConfigEvent(name string, err error, bytes int) {
	diagnostics.Record(name, map[string]any{
		"config_bytes": bytes,
		"error_type":   diagnostics.ErrorType(err),
	})
}
