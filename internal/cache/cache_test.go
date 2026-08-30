package cache

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/yowainwright/pre/internal/obs"
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

func resetConfiguredMaxEntries() func() {
	orig := configuredMaxEntries
	return func() { configuredMaxEntries = orig }
}

func resetConfiguredMaxBytes() func() {
	orig := configuredMaxBytes
	return func() { configuredMaxBytes = orig }
}

func withObsDir(t *testing.T) {
	t.Helper()
	t.Setenv("PRE_OBS_DIR", t.TempDir())
	t.Setenv("PRE_OBS", "1")
}

func requireObsEvent(t *testing.T, name string) obs.Event {
	t.Helper()
	for _, event := range obsEvents(t) {
		if event["event.name"] == name {
			return event
		}
	}
	t.Fatalf("missing obs event %q", name)
	return nil
}

func obsEvents(t *testing.T) []obs.Event {
	t.Helper()
	events, _, err := obs.Events(time.Time{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	return events
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

func TestSetSkipsVersionlessKey(t *testing.T) {
	c := make(Cache)
	Set(c, Key("npm", "react", ""))
	if len(c) != 0 {
		t.Fatalf("expected versionless key not to be cached, got %#v", c)
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
	withObsDir(t)
	defer withCacheDir(t.TempDir())()

	c := make(Cache)
	Set(c, Key("npm", "react", "18.0.0"))
	Save(c)
	Load()

	written := requireObsEvent(t, "pre.cache.written")
	loaded := requireObsEvent(t, "pre.cache.loaded")
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

func TestConcurrentUpdatesPreserveApprovals(t *testing.T) {
	defer withCacheDir(t.TempDir())()

	keys := []string{
		Key("npm", "react", "18.0.0"),
		Key("npm", "lodash", "4.17.21"),
	}
	var wg sync.WaitGroup
	for _, key := range keys {
		wg.Add(1)
		go func() {
			defer wg.Done()
			Update(func(current Cache) { Set(current, key) })
		}()
	}
	wg.Wait()

	loaded := Load()
	for _, key := range keys {
		if !Hit(loaded, key) {
			t.Fatalf("expected cache hit for %s after concurrent updates, got %#v", key, loaded)
		}
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
	withObsDir(t)
	orig := cacheDirFn
	cacheDirFn = func() (string, error) { return "", errors.New("no dir") }
	defer func() { cacheDirFn = orig }()

	c := Load()
	if len(c) != 0 {
		t.Error("expected empty cache when dir fn errors")
	}
	event := requireObsEvent(t, "pre.cache.load_failed")
	if event["error_type"] == "" {
		t.Fatalf("expected error type, got %#v", event)
	}
}

func TestSaveCacheDirError(t *testing.T) {
	withObsDir(t)
	orig := cacheDirFn
	cacheDirFn = func() (string, error) { return "", errors.New("no dir") }
	defer func() { cacheDirFn = orig }()

	c := make(Cache)
	Save(c)
	event := requireObsEvent(t, "pre.cache.write_failed")
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
	withObsDir(t)
	dir := t.TempDir()
	defer withCacheDir(dir)()
	os.MkdirAll(filepath.Join(dir, "pre"), 0755)
	path := filepath.Join(dir, "pre", "versions.json")
	os.WriteFile(path, []byte("not json"), 0644)
	c := Load()
	if len(c) != 0 {
		t.Error("expected empty cache on bad JSON")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("expected bad cache to be quarantined, got %v", err)
	}
	event := requireObsEvent(t, "pre.cache.load_failed")
	if event["error_type"] == "" {
		t.Fatalf("expected load failure obs event, got %#v", event)
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

func TestSavePrunesMaxEntries(t *testing.T) {
	defer resetConfiguredMaxEntries()()
	configuredMaxEntries = 2
	defer withCacheDir(t.TempDir())()

	c := make(Cache)
	c[Key("npm", "old", "1.0.0")] = Entry{Version: "1.0.0", CheckedAt: time.Now().Add(-3 * time.Hour)}
	c[Key("npm", "mid", "1.0.0")] = Entry{Version: "1.0.0", CheckedAt: time.Now().Add(-2 * time.Hour)}
	c[Key("npm", "new", "1.0.0")] = Entry{Version: "1.0.0", CheckedAt: time.Now().Add(-1 * time.Hour)}

	Save(c)
	loaded := Load()

	if len(loaded) != 2 {
		t.Fatalf("expected two newest entries, got %#v", loaded)
	}
	if Hit(loaded, Key("npm", "old", "1.0.0")) {
		t.Fatal("expected oldest entry to be pruned")
	}
}

func TestSavePrunesMaxBytes(t *testing.T) {
	defer resetConfiguredMaxBytes()()
	configuredMaxBytes = 160
	defer withCacheDir(t.TempDir())()

	c := cacheWithOrderedEntries([]string{"old", "mid", "new"})

	Save(c)
	stats := FileStats()

	if stats.Bytes > MaxBytes() {
		t.Fatalf("expected cache bytes <= %d, got %d", MaxBytes(), stats.Bytes)
	}
	if stats.Entries >= 3 {
		t.Fatalf("expected byte pruning to remove entries, got %d", stats.Entries)
	}
}

func cacheWithOrderedEntries(names []string) Cache {
	c := make(Cache)
	for _, name := range names {
		Set(c, Key("npm", name, "1.0.0"))
		time.Sleep(time.Millisecond)
	}
	return c
}

func TestLoadPrunesExpiredEntries(t *testing.T) {
	dir := t.TempDir()
	defer withCacheDir(dir)()
	writeExpiredCacheFixture(t, dir)

	c := Load()
	assertOnlyFreshCacheEntry(t, c)
}

func TestLoadPersistsPrunedEntries(t *testing.T) {
	dir := t.TempDir()
	defer withCacheDir(dir)()
	writeExpiredCacheFixture(t, dir)

	Load()
	c := readCacheFixture(t, dir)
	assertOnlyFreshCacheEntry(t, c)
}

func readCacheFixture(t *testing.T, dir string) Cache {
	t.Helper()
	path := filepath.Join(dir, "pre", "versions.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var c Cache
	if err := json.Unmarshal(data, &c); err != nil {
		t.Fatal(err)
	}
	return c
}

func writeExpiredCacheFixture(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "pre"), 0755); err != nil {
		t.Fatal(err)
	}
	data := expiredCacheFixture()
	path := filepath.Join(dir, "pre", "versions.json")
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatal(err)
	}
}

func expiredCacheFixture() []byte {
	fresh := formattedCacheTime(time.Now())
	expired := formattedCacheTime(time.Now().Add(-48 * time.Hour))
	fixture := `{
		"npm/fresh@1.0.0":{"version":"1.0.0","checkedAt":"` + fresh + `"},
		"npm/expired@1.0.0":{"version":"1.0.0","checkedAt":"` + expired + `"}
	}`
	return []byte(fixture)
}

func assertOnlyFreshCacheEntry(t *testing.T, c Cache) {
	t.Helper()
	if len(c) != 1 {
		t.Fatalf("expected only fresh entry, got %#v", c)
	}
	if !Hit(c, Key("npm", "fresh", "1.0.0")) {
		t.Fatalf("expected only fresh entry, got %#v", c)
	}
}

func formattedCacheTime(value time.Time) string {
	utc := value.UTC()
	return utc.Format(time.RFC3339)
}

func TestMaxEntriesEnv(t *testing.T) {
	defer resetConfiguredMaxEntries()()
	configuredMaxEntries = 7
	t.Setenv("PRE_CACHE_MAX_ENTRIES", "3")
	if MaxEntries() != 3 {
		t.Errorf("expected env max entries, got %d", MaxEntries())
	}
}

func TestMaxEntriesInvalidEnvFallsBack(t *testing.T) {
	defer resetConfiguredMaxEntries()()
	configuredMaxEntries = 7
	for _, value := range []string{"bad", "-1"} {
		t.Setenv("PRE_CACHE_MAX_ENTRIES", value)
		if MaxEntries() != 7 {
			t.Fatalf("expected fallback for %q, got %d", value, MaxEntries())
		}
	}
}

func TestMaxBytesEnv(t *testing.T) {
	defer resetConfiguredMaxBytes()()
	configuredMaxBytes = 700
	t.Setenv("PRE_CACHE_MAX_BYTES", "300")
	if MaxBytes() != 300 {
		t.Errorf("expected env max bytes, got %d", MaxBytes())
	}
}

func TestMaxBytesInvalidEnvFallsBack(t *testing.T) {
	defer resetConfiguredMaxBytes()()
	configuredMaxBytes = 700
	for _, value := range []string{"bad", "-1"} {
		t.Setenv("PRE_CACHE_MAX_BYTES", value)
		if MaxBytes() != 700 {
			t.Fatalf("expected fallback for %q, got %d", value, MaxBytes())
		}
	}
}

func TestFileStatsCacheDirError(t *testing.T) {
	withObsDir(t)
	orig := cacheDirFn
	cacheDirFn = func() (string, error) { return "", errors.New("no dir") }
	defer func() { cacheDirFn = orig }()

	stats := FileStats()
	if stats.Entries != 0 {
		t.Fatalf("expected empty stats, got %#v", stats)
	}
	event := requireObsEvent(t, "pre.cache.stat_failed")
	if event["error_type"] == "" {
		t.Fatalf("expected stat failure obs event, got %#v", event)
	}
}

func TestMigrateCacheKeepsNewestDuplicate(t *testing.T) {
	now := time.Now()
	key := Key("npm", "react", "18.0.0")
	c := Cache{
		"npm/react": Entry{Version: "18.0.0", CheckedAt: now.Add(-time.Hour)},
		key:         Entry{Version: "18.0.0", CheckedAt: now},
	}

	migrated, changed := migrateCache(c)
	if !changed {
		t.Fatal("expected duplicate migration to report change")
	}
	if !migrated[key].CheckedAt.Equal(now) {
		t.Fatalf("expected newest duplicate to survive, got %#v", migrated[key])
	}
}

func TestShouldPruneEntryRejectsInvalidKeys(t *testing.T) {
	now := time.Now()
	entry := Entry{Version: "1.0.0", CheckedAt: now}
	for _, key := range []string{"", "npm", "npm/react"} {
		if !shouldPruneEntry(key, entry, now, defaultTTL, "") {
			t.Fatalf("expected invalid key %q to be pruned", key)
		}
	}
}

func TestShouldPruneEntryRejectsVersionMismatch(t *testing.T) {
	now := time.Now()
	key := Key("npm", "react", "1.0.0")
	entry := Entry{Version: "2.0.0", CheckedAt: now}
	if !shouldPruneEntry(key, entry, now, defaultTTL, "") {
		t.Fatal("expected version mismatch to be pruned")
	}
}

func TestShouldPruneEntryAcceptsValidEntry(t *testing.T) {
	now := time.Now()
	key := Key("npm", "react", "1.0.0")
	entry := Entry{Version: "1.0.0", CheckedAt: now}
	if shouldPruneEntry(key, entry, now, defaultTTL, "") {
		t.Fatal("expected valid entry to survive pruning")
	}
}

func TestPruneMaxEntriesNegativeDisabled(t *testing.T) {
	defer resetConfiguredMaxEntries()()
	configuredMaxEntries = -1
	c := cacheWithOrderedEntries([]string{"old", "new"})
	if pruneMaxEntries(c) {
		t.Fatal("expected negative max entries to disable pruning")
	}
}

func TestPruneMaxBytesNegativeDisabled(t *testing.T) {
	defer resetConfiguredMaxBytes()()
	configuredMaxBytes = -1
	c := cacheWithOrderedEntries([]string{"old", "new"})
	if pruneMaxBytes(c) {
		t.Fatal("expected negative max bytes to disable pruning")
	}
}

func TestCacheFileSizeStatError(t *testing.T) {
	withObsDir(t)
	parent := filepath.Join(t.TempDir(), "parent")
	if err := os.WriteFile(parent, []byte("file"), 0644); err != nil {
		t.Fatal(err)
	}

	size := cacheFileSize(filepath.Join(parent, "versions.json"))
	if size != 0 {
		t.Fatalf("expected zero size on stat error, got %d", size)
	}
	requireObsEvent(t, "pre.cache.stat_failed")
}

func TestWriteCacheAtomicError(t *testing.T) {
	withObsDir(t)
	parent := filepath.Join(t.TempDir(), "parent")
	if err := os.WriteFile(parent, []byte("file"), 0644); err != nil {
		t.Fatal(err)
	}

	writeCache(filepath.Join(parent, "versions.json"), make(Cache))
	requireObsEvent(t, "pre.cache.write_failed")
}

func TestRepairCacheFileLockError(t *testing.T) {
	withObsDir(t)
	preDir := filepath.Join(t.TempDir(), "pre")
	if err := os.MkdirAll(filepath.Join(preDir, "versions.lock"), 0755); err != nil {
		t.Fatal(err)
	}

	repairCacheFile(filepath.Join(preDir, "versions.json"))
	requireObsEvent(t, "pre.cache.write_failed")
}
