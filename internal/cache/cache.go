package cache

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yowainwright/pre/internal/fileutil"
)

const defaultTTL = 24 * time.Hour

const (
	cacheLockStaleAfter = 30 * time.Second
	cacheLockTimeout    = 2 * time.Second
	cacheLockRetry      = 10 * time.Millisecond
)

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
	return loadFromPath(p)
}

func Save(c Cache) {
	p, err := cacheFile()
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return
	}
	release, err := acquireLock(filepath.Join(filepath.Dir(p), "versions.lock"))
	if err != nil {
		return
	}
	defer release()
	writeCache(p, c)
}

func Update(fn func(Cache)) {
	if fn == nil {
		return
	}

	p, err := cacheFile()
	if err != nil {
		return
	}
	if err := os.MkdirAll(filepath.Dir(p), 0755); err != nil {
		return
	}

	release, err := acquireLock(filepath.Join(filepath.Dir(p), "versions.lock"))
	if err != nil {
		return
	}
	defer release()

	c := loadFromPath(p)
	fn(c)
	writeCache(p, c)
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

func loadFromPath(path string) Cache {
	data, err := os.ReadFile(path)
	if err != nil {
		return make(Cache)
	}
	var c Cache
	if err := json.Unmarshal(data, &c); err != nil {
		return make(Cache)
	}
	return migrate(c)
}

func writeCache(path string, c Cache) {
	data, _ := json.Marshal(migrate(c))
	_ = fileutil.AtomicWriteFile(path, data, 0644)
}

func acquireLock(path string) (func(), error) {
	deadline := time.Now().Add(cacheLockTimeout)
	for {
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
		if err == nil {
			_, _ = file.WriteString(time.Now().Format(time.RFC3339Nano))
			_ = file.Close()
			return func() { _ = os.Remove(path) }, nil
		}

		if !errors.Is(err, os.ErrExist) {
			return nil, err
		}
		if info, statErr := os.Stat(path); statErr == nil && time.Since(info.ModTime()) > cacheLockStaleAfter {
			_ = os.Remove(path)
			continue
		}
		if time.Now().After(deadline) {
			return nil, err
		}
		time.Sleep(cacheLockRetry)
	}
}
