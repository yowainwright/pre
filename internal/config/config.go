package config

import (
	"encoding/json"
	"os"
	"path/filepath"

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
		return cfg
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return cfg
	}
	if err := json.Unmarshal(data, cfg); err != nil {
		return cfg
	}
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
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return err
	}
	data, err := marshalIndentFn(cfg, "", "  ")
	if err != nil {
		return err
	}
	return fileutil.AtomicWriteFile(p, data, 0644)
}

func configPath() (string, error) {
	dir, err := configDirFn()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "pre", "config.json"), nil
}
