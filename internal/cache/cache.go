package cache

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/yowainwright/pre/internal/fileutil"
	"github.com/yowainwright/pre/internal/obs"
)

const defaultTTL = 24 * time.Hour

const cacheLockStaleAfter = 30 * time.Second
const defaultMaxEntries = 10000
const defaultMaxBytes int64 = 5 << 20

const (
	recordCacheLoadedEvent = true
	skipCacheLoadedEvent   = false
)

var (
	cacheLockTimeout = 2 * time.Second
	cacheLockRetry   = 10 * time.Millisecond
)

var (
	configuredTTL        = defaultTTL
	configuredMaxEntries = defaultMaxEntries
	configuredMaxBytes   = defaultMaxBytes
	configuredSource     string
)

func SetTTL(s string) {
	if s == "" {
		return
	}
	if d, err := time.ParseDuration(s); err == nil && d >= 0 {
		configuredTTL = d
	}
}

func SetSource(s string) {
	configuredSource = strings.TrimSpace(s)
}

type Entry struct {
	Version    string    `json:"version"`
	CheckedAt  time.Time `json:"checkedAt"`
	ApprovedAt time.Time `json:"approvedAt,omitempty"`
	Source     string    `json:"source,omitempty"`
}

type Cache map[string]Entry

type Stats struct {
	Entries    int   `json:"entries"`
	Bytes      int64 `json:"bytes"`
	MaxEntries int   `json:"max_entries"`
	MaxBytes   int64 `json:"max_bytes"`
}

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
		recordCacheEvent("pre.cache.load_failed", nil, err)
		return make(Cache)
	}
	return loadAndRepairPath(p)
}

func Path() (string, error) {
	return cacheFile()
}

func Save(c Cache) {
	p, err := cacheFile()
	if err != nil {
		recordCacheEvent("pre.cache.write_failed", c, err)
		return
	}
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		recordCacheEvent("pre.cache.write_failed", c, err)
		return
	}
	release, err := acquireLock(filepath.Join(filepath.Dir(p), "versions.lock"))
	if err != nil {
		recordCacheEvent("pre.cache.write_failed", c, err)
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
		recordCacheEvent("pre.cache.write_failed", nil, err)
		return
	}
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		recordCacheEvent("pre.cache.write_failed", nil, err)
		return
	}

	release, err := acquireLock(filepath.Join(filepath.Dir(p), "versions.lock"))
	if err != nil {
		recordCacheEvent("pre.cache.write_failed", nil, err)
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

func MaxEntries() int {
	if v := os.Getenv("PRE_CACHE_MAX_ENTRIES"); v != "" {
		return parseMaxEntries(v)
	}
	return configuredMaxEntries
}

func MaxBytes() int64 {
	if v := os.Getenv("PRE_CACHE_MAX_BYTES"); v != "" {
		return parseMaxBytes(v)
	}
	return configuredMaxBytes
}

func parseMaxEntries(value string) int {
	n, err := strconv.Atoi(value)
	if err != nil {
		return configuredMaxEntries
	}
	if n < 0 {
		return configuredMaxEntries
	}
	return n
}

func parseMaxBytes(value string) int64 {
	n, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return configuredMaxBytes
	}
	if n < 0 {
		return configuredMaxBytes
	}
	return n
}

func Source() string {
	return configuredSource
}

func FileStats() Stats {
	p, err := cacheFile()
	stats := Stats{MaxEntries: MaxEntries(), MaxBytes: MaxBytes()}
	if err != nil {
		recordCacheEvent("pre.cache.stat_failed", nil, err)
		return stats
	}
	cache := loadAndRepairPath(p)
	stats.Bytes = cacheFileSize(p)
	stats.Entries = len(cache)
	return stats
}

func Hit(c Cache, key string) bool {
	e, ok := c[key]
	if !ok {
		return false
	}
	ttl := TTL()
	currentSource := Source()
	sourceMismatch := currentSource != "" && e.Source != currentSource
	if ttl <= 0 || e.CheckedAt.IsZero() || sourceMismatch {
		return false
	}
	age := time.Since(e.CheckedAt)
	return age >= 0 && age < ttl
}

func Set(c Cache, key string) {
	_, _, version := ParseKey(key)
	if version == "" {
		return
	}
	now := time.Now()
	c[key] = Entry{Version: version, CheckedAt: now, ApprovedAt: now, Source: Source()}
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
	migrated, _ := migrateCache(c)
	return migrated
}

func migrateCache(c Cache) (Cache, bool) {
	if len(c) == 0 {
		return c, false
	}
	migration := cacheMigration{entries: make(Cache, len(c))}
	for key, entry := range c {
		migration.add(key, entry)
	}
	return migration.entries, migration.changed
}

type cacheMigration struct {
	entries Cache
	changed bool
}

func (m *cacheMigration) add(key string, entry Entry) {
	key, entry, changed := migrateCacheItem(key, entry)
	m.changed = m.changed || changed
	current, exists := m.entries[key]
	entryIsOlderOrSame := !entry.CheckedAt.After(current.CheckedAt)
	shouldDropEntry := exists && entryIsOlderOrSame
	if shouldDropEntry {
		m.changed = true
		return
	}
	if exists {
		m.changed = true
	}
	m.entries[key] = entry
}

func migrateCacheItem(key string, entry Entry) (string, Entry, bool) {
	ecosystem, name, version := ParseKey(key)
	changed := false
	canUseEntryVersion := version == ""
	hasPackageName := ecosystem != "" && name != ""
	hasEntryVersion := entry.Version != ""
	shouldUseEntryVersion := canUseEntryVersion && hasPackageName && hasEntryVersion
	if shouldUseEntryVersion {
		key = Key(ecosystem, name, entry.Version)
		version = entry.Version
		changed = true
	}
	shouldBackfillVersion := version != "" && entry.Version == ""
	if shouldBackfillVersion {
		entry.Version = version
		changed = true
	}
	return key, entry, changed
}

func prune(c Cache) Cache {
	pruned, _ := pruneCache(c)
	return pruned
}

func pruneCache(c Cache) (Cache, bool) {
	if len(c) == 0 {
		return c, false
	}
	changed := pruneInvalid(c)
	changed = pruneMaxEntries(c) || changed
	changed = pruneMaxBytes(c) || changed
	return c, changed
}

func pruneInvalid(c Cache) bool {
	now := time.Now()
	ttl := TTL()
	source := Source()
	changed := false
	for key, entry := range c {
		if shouldPruneEntry(key, entry, now, ttl, source) {
			delete(c, key)
			changed = true
		}
	}
	return changed
}

func shouldPruneEntry(key string, entry Entry, now time.Time, ttl time.Duration, source string) bool {
	ecosystem, name, version := ParseKey(key)
	if invalidCacheKey(ecosystem, name, version) {
		return true
	}
	if entry.Version != version {
		return true
	}
	if invalidCacheTime(entry.CheckedAt, now, ttl) {
		return true
	}
	if now.Sub(entry.CheckedAt) >= ttl {
		return true
	}
	return cacheSourceMismatch(entry.Source, source)
}

func invalidCacheKey(ecosystem, name, version string) bool {
	if ecosystem == "" {
		return true
	}
	if name == "" {
		return true
	}
	return version == ""
}

func invalidCacheTime(checkedAt, now time.Time, ttl time.Duration) bool {
	if ttl <= 0 {
		return true
	}
	if checkedAt.IsZero() {
		return true
	}
	return checkedAt.After(now)
}

func cacheSourceMismatch(entrySource, source string) bool {
	if source == "" {
		return false
	}
	return entrySource != source
}

func pruneMaxEntries(c Cache) bool {
	max := MaxEntries()
	if max < 0 {
		return false
	}
	if len(c) <= max {
		return false
	}
	replaceWithItems(c, newestItems(c)[:max])
	return true
}

func pruneMaxBytes(c Cache) bool {
	max := MaxBytes()
	if max < 0 {
		return false
	}
	if len(c) == 0 {
		return false
	}
	items := newestItems(c)
	keep := maxItemsUnderBytes(items, max)
	if keep == len(items) {
		return false
	}
	replaceWithItems(c, items[:keep])
	return true
}

func newestItems(c Cache) []cacheItem {
	items := make([]cacheItem, 0, len(c))
	for key, entry := range c {
		items = append(items, cacheItem{key: key, entry: entry})
	}
	sort.Slice(items, func(i, j int) bool {
		return itemTime(items[i]).After(itemTime(items[j]))
	})
	return items
}

func maxItemsUnderBytes(items []cacheItem, max int64) int {
	low, high := 0, len(items)
	best := 0
	for low <= high {
		mid := low + (high-low)/2
		size := serializedItemsBytes(items[:mid])
		if size <= max {
			best = mid
			low = mid + 1
			continue
		}
		high = mid - 1
	}
	return best
}

func serializedItemsBytes(items []cacheItem) int64 {
	c := make(Cache, len(items))
	for _, item := range items {
		c[item.key] = item.entry
	}
	data, err := json.Marshal(c)
	if err != nil {
		return 1<<63 - 1
	}
	return int64(len(data))
}

func replaceWithItems(c Cache, items []cacheItem) {
	clear(c)
	for _, item := range items {
		c[item.key] = item.entry
	}
}

func itemTime(item cacheItem) time.Time {
	if !item.entry.ApprovedAt.IsZero() {
		return item.entry.ApprovedAt
	}
	return item.entry.CheckedAt
}

type cacheItem struct {
	key   string
	entry Entry
}

type loadResult struct {
	cache   Cache
	changed bool
}

func loadAndRepairPath(path string) Cache {
	result := readCacheFile(path, recordCacheLoadedEvent)
	if result.changed {
		repairCacheFile(path)
	}
	return result.cache
}

func loadFromPath(path string) Cache {
	return readCacheFile(path, recordCacheLoadedEvent).cache
}

func readCacheFile(path string, recordLoaded bool) loadResult {
	data, err := os.ReadFile(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			recordCacheEvent("pre.cache.load_failed", nil, err)
		}
		return loadResult{cache: make(Cache)}
	}
	var c Cache
	if err := json.Unmarshal(data, &c); err != nil {
		recordCacheEvent("pre.cache.load_failed", nil, err)
		quarantineCache(path)
		return loadResult{cache: make(Cache)}
	}
	c, migrated := migrateCache(c)
	c, pruned := pruneCache(c)
	recordCacheLoaded(c, len(data), recordLoaded)
	return loadResult{cache: c, changed: migrated || pruned}
}

func recordCacheLoaded(c Cache, bytes int, enabled bool) {
	if !enabled {
		return
	}
	obs.Record("pre.cache.loaded", map[string]any{
		"cache_entries": len(c),
		"cache_bytes":   bytes,
	})
}

func repairCacheFile(path string) {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		recordCacheEvent("pre.cache.write_failed", nil, err)
		return
	}
	release, err := acquireLock(filepath.Join(filepath.Dir(path), "versions.lock"))
	if err != nil {
		recordCacheEvent("pre.cache.write_failed", nil, err)
		return
	}
	defer release()
	repairCacheFileLocked(path)
}

func repairCacheFileLocked(path string) {
	result := readCacheFile(path, skipCacheLoadedEvent)
	if !result.changed {
		return
	}
	writeCache(path, result.cache)
}

func cacheFileSize(path string) int64 {
	info, err := os.Stat(path)
	if err == nil {
		return info.Size()
	}
	if !errors.Is(err, os.ErrNotExist) {
		recordCacheEvent("pre.cache.stat_failed", nil, err)
	}
	return 0
}

func writeCache(path string, c Cache) {
	c = migrate(c)
	c = prune(c)
	data, err := json.Marshal(c)
	if err != nil {
		recordCacheEvent("pre.cache.write_failed", c, err)
		return
	}
	if err := fileutil.AtomicWriteFile(path, data, 0600); err != nil {
		recordCacheEvent("pre.cache.write_failed", c, err)
		return
	}
	obs.Record("pre.cache.written", map[string]any{
		"cache_entries": len(c),
		"cache_bytes":   len(data),
	})
}

func recordCacheEvent(name string, c Cache, err error) {
	attrs := map[string]any{"error_type": obs.ErrorType(err)}
	if c != nil {
		attrs["cache_entries"] = len(c)
	}
	obs.Record(name, attrs)
}

func quarantineCache(path string) {
	now := time.Now().UTC()
	suffix := now.Format("20060102T150405Z")
	_ = os.Rename(path, path+".invalid-"+suffix)
}

func acquireLock(path string) (func(), error) {
	deadline := time.Now().Add(cacheLockTimeout)
	for {
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
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
