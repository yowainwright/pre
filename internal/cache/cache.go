package cache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yowainwright/pre/internal/fileutil"
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
	return migrate(c)
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
	_ = fileutil.AtomicWriteFile(p, data, 0644)
}

func TTL() time.Duration {
	if v := os.Getenv("PRE_CACHE_TTL"); v != "" {
		if d, err := time.ParseDuration(v); err == nil {
			return d
		}
	}
	return configuredTTL
}

func Hit(c Cache, key string) bool {
	e, ok := c[key]
	if !ok {
		return false
	}
	return time.Since(e.CheckedAt) < TTL()
}

func Set(c Cache, key string) {
	_, _, version := ParseKey(key)
	c[key] = Entry{Version: version, CheckedAt: time.Now()}
}

func Key(ecosystem, name, version string) string {
	key := ecosystem + "/" + name
	if version == "" {
		return key
	}
	return key + "@" + version
}

func ParseKey(key string) (ecosystem, name, version string) {
	parts := strings.SplitN(key, "/", 2)
	if len(parts) != 2 {
		return key, "", ""
	}
	ecosystem, rest := parts[0], parts[1]
	if idx := strings.LastIndex(rest, "@"); idx > 0 {
		return ecosystem, rest[:idx], rest[idx+1:]
	}
	return ecosystem, rest, ""
}

func migrate(c Cache) Cache {
	if len(c) == 0 {
		return c
	}

	migrated := make(Cache, len(c))
	for key, entry := range c {
		ecosystem, name, version := ParseKey(key)
		if version == "" && ecosystem != "" && name != "" && entry.Version != "" {
			key = Key(ecosystem, name, entry.Version)
			version = entry.Version
		}
		if version != "" && entry.Version == "" {
			entry.Version = version
		}
		current, exists := migrated[key]
		if !exists || entry.CheckedAt.After(current.CheckedAt) {
			migrated[key] = entry
		}
	}
	return migrated
}
