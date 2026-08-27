package cache

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	prediagnostics "github.com/yowainwright/pre/internal/diagnostics"
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

func resetConfiguredSource() func() {
	orig := configuredSource
	return func() { configuredSource = orig }
}

func withDiagnosticsDir(t *testing.T) {
	t.Helper()
	t.Setenv("PRE_DIAGNOSTICS_DIR", t.TempDir())
	t.Setenv("PRE_DIAGNOSTICS", "1")
}

func requireDiagnosticEvent(t *testing.T, name string) prediagnostics.Event {
	t.Helper()
	events, _, err := prediagnostics.Events(time.Time{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		if event["event.name"] == name {
			return event
		}
	}
	t.Fatalf("missing diagnostic event %q in %#v", name, events)
	return nil
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

func TestSetTTLNegative(t *testing.T) {
	defer resetConfiguredTTL()()
	SetTTL("-1h")
	if configuredTTL != defaultTTL {
		t.Error("expected configuredTTL unchanged on negative input")
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

func TestSource(t *testing.T) {
	defer resetConfiguredSource()()
	SetSource(" https://api.example.test ")
	if Source() != "https://api.example.test" {
		t.Errorf("expected trimmed source, got %q", Source())
	}
}

func TestKey(t *testing.T) {
	if Key("npm", "react", "18.0.0") != "npm/react@18.0.0" {
		t.Errorf("unexpected key: %s", Key("npm", "react", "18.0.0"))
	}
}

func TestKeyNoVersion(t *testing.T) {
	if Key("npm", "react", "") != "npm/react" {
		t.Errorf("unexpected key: %s", Key("npm", "react", ""))
	}
}

func TestCachePath(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	p, err := Path()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if p == "" {
		t.Fatal("expected non-empty cache path")
	}
}

func TestMigrateVersionFromKey(t *testing.T) {
	c := Cache{
		"npm/react@18.0.0": Entry{Version: "", CheckedAt: time.Now()},
	}
	m := migrate(c)
	e := m["npm/react@18.0.0"]
	if e.Version != "18.0.0" {
		t.Errorf("expected version populated from key, got %q", e.Version)
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

func TestHitFutureTimestamp(t *testing.T) {
	c := make(Cache)
	c[Key("npm", "react", "18.0.0")] = Entry{Version: "18.0.0", CheckedAt: time.Now().Add(time.Hour)}
	if Hit(c, Key("npm", "react", "18.0.0")) {
		t.Error("expected miss on future timestamp")
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

func TestHitNegativeTTL(t *testing.T) {
	t.Setenv("PRE_CACHE_TTL", "-1h")
	c := make(Cache)
	Set(c, Key("npm", "react", "18.0.0"))
	if Hit(c, Key("npm", "react", "18.0.0")) {
		t.Error("expected miss when TTL is negative")
	}
}

func TestHitSourceMismatch(t *testing.T) {
	defer resetConfiguredSource()()
	SetSource("https://api.one.example")
	c := make(Cache)
	Set(c, Key("npm", "react", "18.0.0"))

	SetSource("https://api.two.example")
	if Hit(c, Key("npm", "react", "18.0.0")) {
		t.Error("expected miss when source changes")
	}
}

func TestHitBlankSourceMissWhenSourceConfigured(t *testing.T) {
	defer resetConfiguredSource()()
	SetSource("https://api.example.test")

	key := Key("npm", "react", "18.0.0")
	c := Cache{
		key: Entry{Version: "18.0.0", CheckedAt: time.Now()},
	}

	if Hit(c, key) {
		t.Error("expected source-less legacy entry to miss when source is configured")
	}
}

func TestSetStoresSource(t *testing.T) {
	defer resetConfiguredSource()()
	SetSource("https://api.example.test")
	c := make(Cache)
	key := Key("npm", "react", "18.0.0")
	Set(c, key)
	if c[key].Source != "https://api.example.test" {
		t.Errorf("expected source stored on entry, got %q", c[key].Source)
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

func TestSaveAndLoadRecordDiagnostics(t *testing.T) {
	withDiagnosticsDir(t)
	defer withCacheDir(t.TempDir())()

	c := make(Cache)
	Set(c, Key("npm", "react", "18.0.0"))
	Save(c)
	Load()

	written := requireDiagnosticEvent(t, "pre.cache.written")
	loaded := requireDiagnosticEvent(t, "pre.cache.loaded")
	if written["cache_entries"] != float64(1) || loaded["cache_entries"] != float64(1) {
		t.Fatalf("expected cache entry counts, got written=%#v loaded=%#v", written, loaded)
	}
}

func TestUpdateAddsToExistingCache(t *testing.T) {
	defer withCacheDir(t.TempDir())()

	c := make(Cache)
	Set(c, Key("npm", "react", "18.0.0"))
	Save(c)

	Update(func(current Cache) {
		Set(current, Key("npm", "lodash", "4.17.21"))
	})

	loaded := Load()
	if !Hit(loaded, Key("npm", "react", "18.0.0")) || !Hit(loaded, Key("npm", "lodash", "4.17.21")) {
		t.Errorf("expected update to preserve old entries and add new ones, got %v", loaded)
	}
}

func TestUpdateDeletesEntries(t *testing.T) {
	defer withCacheDir(t.TempDir())()

	c := make(Cache)
	key := Key("npm", "react", "18.0.0")
	Set(c, key)
	Save(c)

	Update(func(current Cache) {
		delete(current, key)
	})

	loaded := Load()
	if Hit(loaded, key) {
		t.Error("expected deleted entry to stay removed after update")
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
	withDiagnosticsDir(t)
	orig := cacheDirFn
	cacheDirFn = func() (string, error) { return "", errors.New("no dir") }
	defer func() { cacheDirFn = orig }()

	c := Load()
	if len(c) != 0 {
		t.Error("expected empty cache when dir fn errors")
	}
	event := requireDiagnosticEvent(t, "pre.cache.load_failed")
	if event["error_type"] == "" {
		t.Fatalf("expected error type, got %#v", event)
	}
}

func TestSaveCacheDirError(t *testing.T) {
	withDiagnosticsDir(t)
	orig := cacheDirFn
	cacheDirFn = func() (string, error) { return "", errors.New("no dir") }
	defer func() { cacheDirFn = orig }()

	c := make(Cache)
	Save(c)
	event := requireDiagnosticEvent(t, "pre.cache.write_failed")
	if event["error_type"] == "" {
		t.Fatalf("expected error type, got %#v", event)
	}
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

func TestUpdateCacheDirError(t *testing.T) {
	orig := cacheDirFn
	cacheDirFn = func() (string, error) { return "", errors.New("no dir") }
	defer func() { cacheDirFn = orig }()
	Update(func(c Cache) { Set(c, Key("npm", "react", "18.0.0")) })
}

func TestUpdateNilFn(t *testing.T) {
	defer withCacheDir(t.TempDir())()
	Update(nil)
}

func TestUpdateAcquireLockError(t *testing.T) {
	dir := t.TempDir()
	defer withCacheDir(dir)()
	preDir := filepath.Join(dir, "pre")
	if err := os.MkdirAll(preDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(preDir, "versions.lock"), 0755); err != nil {
		t.Fatal(err)
	}
	Update(func(c Cache) { Set(c, Key("npm", "react", "18.0.0")) })
}

func TestSaveAcquireLockError(t *testing.T) {
	dir := t.TempDir()
	defer withCacheDir(dir)()
	preDir := filepath.Join(dir, "pre")
	if err := os.MkdirAll(preDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(preDir, "versions.lock"), 0755); err != nil {
		t.Fatal(err)
	}
	c := make(Cache)
	Set(c, Key("npm", "react", "18.0.0"))
	Save(c)
}

func TestAcquireLockNonErrExist(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "versions.lock")
	if err := os.Mkdir(lockPath, 0755); err != nil {
		t.Fatal(err)
	}
	_, err := acquireLock(lockPath)
	if err == nil {
		t.Error("expected error when lock path is a directory")
	}
}

func TestAcquireLockDeadlineExceeded(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "versions.lock")

	orig := cacheLockTimeout
	cacheLockTimeout = 20 * time.Millisecond
	defer func() { cacheLockTimeout = orig }()

	release, err := acquireLock(lockPath)
	if err != nil {
		t.Fatalf("first lock unexpected error: %v", err)
	}

	_, err = acquireLock(lockPath)
	if err == nil {
		t.Error("expected deadline error when lock is held")
	}
	release()
}

func TestAcquireLockStaleLock(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "versions.lock")
	if err := os.WriteFile(lockPath, []byte("stale"), 0644); err != nil {
		t.Fatal(err)
	}
	oldTime := time.Now().Add(-2 * cacheLockStaleAfter)
	if err := os.Chtimes(lockPath, oldTime, oldTime); err != nil {
		t.Fatal(err)
	}
	release, err := acquireLock(lockPath)
	if err != nil {
		t.Fatalf("expected stale lock to be evicted, got: %v", err)
	}
	release()
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
