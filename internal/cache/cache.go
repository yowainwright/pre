package cache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const defaultTTL = 24 * time.Hour

var configuredTTL = defaultTTL

func SetTTL(s string) {
	if s == "" {
		return
	}
	if d, err := time.ParseDuration(s); err == nil {
		configuredTTL = d
	}
}

type Entry struct {
	Version   string    `json:"version"`
	CheckedAt time.Time `json:"checkedAt"`
}

type Cache map[string]Entry

var cacheDirFn = os.UserCacheDir

func cacheFile() (string, error) {
	dir, err := cacheDirFn()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "pre", "versions.json"), nil
}

func Load() Cache {
	p, err := cacheFile()
	if err != nil {
		return make(Cache)
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return make(Cache)
	}
	var c Cache
	if err := json.Unmarshal(data, &c); err != nil {
		return make(Cache)
	}
	return c
}

func Save(c Cache) {
	p, err := cacheFile()
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return
	}
	data, _ := json.Marshal(c)
	os.WriteFile(p, data, 0644)
}

func TTL() time.Duration {
	if v := os.Getenv("PRE_CACHE_TTL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return configuredTTL
}

func Hit(c Cache, key, version string) bool {
	e, ok := c[key]
	if !ok {
		return false
	}
	return e.Version == version && time.Since(e.CheckedAt) < TTL()
}

func Set(c Cache, key, version string) {
	c[key] = Entry{Version: version, CheckedAt: time.Now()}
}

func Key(ecosystem, name string) string {
	return ecosystem + "/" + name
}

func ParseKey(key string) (ecosystem, name string) {
	parts := strings.SplitN(key, "/", 2)
	if len(parts) != 2 {
		return key, ""
	}
	return parts[0], parts[1]
}
