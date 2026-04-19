package cache

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func withCacheDir(dir string) func() {
	orig := cacheDirFn
	cacheDirFn = func() (string, error) { return dir, nil }
	return func() { cacheDirFn = orig }
}

func resetConfiguredTTL() func() {
	orig := configuredTTL
	return func() { configuredTTL = orig }
}

func TestTTLDefault(t *testing.T) {
	t.Setenv("PRE_CACHE_TTL", "")
	if TTL() != defaultTTL {
		t.Errorf("expected default TTL %v, got %v", defaultTTL, TTL())
	}
}

func TestTTLFromEnv(t *testing.T) {
	t.Setenv("PRE_CACHE_TTL", "1h")
	if TTL() != time.Hour {
		t.Errorf("expected 1h, got %v", TTL())
	}
}

func TestTTLInvalidEnv(t *testing.T) {
	t.Setenv("PRE_CACHE_TTL", "notaduration")
	if TTL() != defaultTTL {
		t.Errorf("expected default TTL on invalid value, got %v", TTL())
	}
}

func TestSetTTL(t *testing.T) {
	defer resetConfiguredTTL()()
	t.Setenv("PRE_CACHE_TTL", "")
	SetTTL("1h")
	if TTL() != time.Hour {
		t.Errorf("expected 1h, got %v", TTL())
	}
}

func TestSetTTLInvalid(t *testing.T) {
	defer resetConfiguredTTL()()
	SetTTL("notaduration")
	if configuredTTL != defaultTTL {
		t.Error("expected configuredTTL unchanged on invalid input")
	}
}

func TestSetTTLEmpty(t *testing.T) {
	defer resetConfiguredTTL()()
	SetTTL("")
	if configuredTTL != defaultTTL {
		t.Error("expected configuredTTL unchanged on empty string")
	}
}

func TestEnvOverridesSetTTL(t *testing.T) {
	defer resetConfiguredTTL()()
	t.Setenv("PRE_CACHE_TTL", "2h")
	SetTTL("1h")
	if TTL() != 2*time.Hour {
		t.Errorf("expected env to win, got %v", TTL())
	}
}

func TestTTLZero(t *testing.T) {
	t.Setenv("PRE_CACHE_TTL", "0s")
	if TTL() != 0 {
		t.Errorf("expected 0, got %v", TTL())
	}
}

func TestKey(t *testing.T) {
	if Key("npm", "react", "18.0.0") != "npm/react@18.0.0" {
		t.Errorf("unexpected key: %s", Key("npm", "react", "18.0.0"))
	}
}

func TestHitMiss(t *testing.T) {
	c := make(Cache)
	if Hit(c, "npm/react@18.0.0") {
		t.Error("expected miss on empty cache")
	}
}

func TestHitVersionMismatch(t *testing.T) {
	c := make(Cache)
	Set(c, Key("npm", "react", "17.0.0"))
	if Hit(c, Key("npm", "react", "18.0.0")) {
		t.Error("expected miss on version mismatch")
	}
}

func TestHitMatch(t *testing.T) {
	c := make(Cache)
	Set(c, Key("npm", "react", "18.0.0"))
	if !Hit(c, Key("npm", "react", "18.0.0")) {
		t.Error("expected hit on matching version within TTL")
	}
}

func TestHitExpired(t *testing.T) {
	c := make(Cache)
	c[Key("npm", "react", "18.0.0")] = Entry{Version: "18.0.0", CheckedAt: time.Now().Add(-25 * time.Hour)}
	if Hit(c, Key("npm", "react", "18.0.0")) {
		t.Error("expected miss on expired entry")
	}
}

func TestHitZeroTTL(t *testing.T) {
	t.Setenv("PRE_CACHE_TTL", "0s")
	c := make(Cache)
	Set(c, Key("npm", "react", "18.0.0"))
	if Hit(c, Key("npm", "react", "18.0.0")) {
		t.Error("expected miss when TTL is zero")
	}
}

func TestLoadEmpty(t *testing.T) {
	defer withCacheDir(t.TempDir())()
	c := Load()
	if len(c) != 0 {
		t.Errorf("expected empty cache, got %v", c)
	}
}

func TestSaveAndLoad(t *testing.T) {
	defer withCacheDir(t.TempDir())()

	c := make(Cache)
	Set(c, Key("npm", "react", "18.0.0"))
	Save(c)

	loaded := Load()
	if !Hit(loaded, Key("npm", "react", "18.0.0")) {
		t.Error("expected cache hit after save and load")
	}
}

func TestSaveBadDir(t *testing.T) {
	defer withCacheDir(filepath.Join(t.TempDir(), "nonexistent-parent"))()
	c := make(Cache)
	Set(c, Key("npm", "react", "18.0.0"))
	Save(c)
}

func TestParseKey(t *testing.T) {
	eco, name, version := ParseKey("npm/react@18.0.0")
	if eco != "npm" || name != "react" || version != "18.0.0" {
		t.Errorf("unexpected: eco=%q name=%q version=%q", eco, name, version)
	}
}

func TestParseKeyNoSlash(t *testing.T) {
	eco, name, version := ParseKey("noslash")
	if eco != "noslash" || name != "" || version != "" {
		t.Errorf("expected key as eco and empty name/version, got eco=%q name=%q version=%q", eco, name, version)
	}
}

func TestLoadCacheDirError(t *testing.T) {
	orig := cacheDirFn
	cacheDirFn = func() (string, error) { return "", errors.New("no dir") }
	defer func() { cacheDirFn = orig }()

	c := Load()
	if len(c) != 0 {
		t.Error("expected empty cache when dir fn errors")
	}
}

func TestSaveCacheDirError(t *testing.T) {
	orig := cacheDirFn
	cacheDirFn = func() (string, error) { return "", errors.New("no dir") }
	defer func() { cacheDirFn = orig }()

	c := make(Cache)
	Save(c)
}

func TestSaveMkdirAllError(t *testing.T) {
	orig := cacheDirFn
	cacheDirFn = func() (string, error) { return "/dev/null", nil }
	defer func() { cacheDirFn = orig }()

	c := make(Cache)
	Save(c)
}

func TestLoadBadJSON(t *testing.T) {
	dir := t.TempDir()
	defer withCacheDir(dir)()
	os.MkdirAll(filepath.Join(dir, "pre"), 0755)
	os.WriteFile(filepath.Join(dir, "pre", "versions.json"), []byte("not json"), 0644)
	c := Load()
	if len(c) != 0 {
		t.Error("expected empty cache on bad JSON")
	}
}

func TestLoadMigratesLegacyKeys(t *testing.T) {
	dir := t.TempDir()
	defer withCacheDir(dir)()
	if err := os.MkdirAll(filepath.Join(dir, "pre"), 0755); err != nil {
		t.Fatal(err)
	}
	data := []byte(`{"npm/react":{"version":"18.0.0","checkedAt":"` + time.Now().UTC().Format(time.RFC3339) + `"}}`)
	if err := os.WriteFile(filepath.Join(dir, "pre", "versions.json"), data, 0644); err != nil {
		t.Fatal(err)
	}

	c := Load()
	if !Hit(c, Key("npm", "react", "18.0.0")) {
		t.Error("expected migrated cache hit")
	}
}
