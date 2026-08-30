package obs

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"runtime/metrics"
	"strings"
	"sync"
	"time"
)

const (
	SchemaVersion   = 1
	DefaultMaxBytes = 5 << 20
)

const (
	eventsFile  = "obs.ndjson"
	rotatedFile = "obs.ndjson.1"
)

const maxStringBytes = 240

type Event map[string]any

type Summary struct {
	Enabled  bool   `json:"enabled"`
	Events   int    `json:"events"`
	Included int    `json:"included_events,omitempty"`
	Invalid  int    `json:"invalid"`
	Bytes    int64  `json:"bytes"`
	MaxBytes int64  `json:"max_bytes"`
	Rotated  bool   `json:"rotated"`
	Oldest   string `json:"oldest,omitempty"`
	Newest   string `json:"newest,omitempty"`
}

var (
	appendMu       sync.Mutex
	currentVersion string
	maxBytes       int64 = DefaultMaxBytes
	nowFn                = time.Now
	stateDirFn           = defaultStateDir
)

func SetVersion(version string) {
	currentVersion = strings.TrimSpace(version)
}

func Enabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("PRE_OBS"))) {
	case "0", "false", "no", "off":
		return false
	}
	isTest := strings.HasSuffix(os.Args[0], ".test")
	hasObsDir := os.Getenv("PRE_OBS_DIR") != ""
	hasObsEnv := os.Getenv("PRE_OBS") != ""
	if !isTest {
		return true
	}
	if hasObsDir {
		return true
	}
	return hasObsEnv
}

func Record(name string, attrs map[string]any) {
	if name == "" {
		return
	}
	if !Enabled() {
		return
	}
	event := newEvent(name, attrs)
	_ = appendEvent(event)
}

func ErrorType(err error) string {
	if err == nil {
		return ""
	}
	name := strings.TrimPrefix(strings.TrimPrefix(typeName(err), "*"), "fs.")
	if name == "" {
		return "error"
	}
	return sanitizeString(name, 80)
}

func Status() (Summary, error) {
	_, summary, err := readEvents(time.Time{}, 0)
	return summary, err
}

func Events(since time.Time, limit int) ([]Event, Summary, error) {
	events, summary, err := readEvents(since, limit)
	return events, summary, err
}

func newEvent(name string, attrs map[string]any) Event {
	event := Event{
		"time":               nowFn().UTC().Format(time.RFC3339Nano),
		"event.name":         sanitizeString(name, 120),
		"pre.schema_version": SchemaVersion,
	}
	if currentVersion != "" {
		event["pre.version"] = currentVersion
	}
	addRuntimeAttrs(event)
	for key, value := range attrs {
		addAttr(event, key, value)
	}
	return event
}

func addRuntimeAttrs(event Event) {
	samples := []metrics.Sample{
		{Name: "/memory/classes/heap/objects:bytes"},
		{Name: "/sched/goroutines:goroutines"},
	}
	metrics.Read(samples)
	addMetric(event, "go.heap_objects_bytes", samples[0])
	addMetric(event, "go.goroutines", samples[1])
}

func addMetric(event Event, key string, sample metrics.Sample) {
	if sample.Value.Kind() == metrics.KindUint64 {
		event[key] = sample.Value.Uint64()
	}
}

func appendEvent(event Event) error {
	appendMu.Lock()
	defer appendMu.Unlock()

	dir, err := stateDir()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	path := filepath.Join(dir, eventsFile)
	if err := rotateIfNeeded(path); err != nil {
		return err
	}
	return appendLine(path, event)
}

func appendLine(path string, event Event) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return err
	}
	defer file.Close()

	data, err := json.Marshal(event)
	if err != nil {
		return err
	}
	data = append(data, '\n')
	_, err = file.Write(data)
	return err
}

func rotateIfNeeded(path string) error {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if info.Size() <= maxBytes {
		return nil
	}
	rotated := filepath.Join(filepath.Dir(path), rotatedFile)
	_ = os.Remove(rotated)
	return os.Rename(path, rotated)
}

func readEvents(since time.Time, limit int) ([]Event, Summary, error) {
	dir, err := stateDir()
	summary := Summary{Enabled: Enabled(), MaxBytes: maxBytes}
	if err != nil {
		return nil, summary, err
	}
	events, err := readEventFiles(dir, since, &summary)
	events = limitEvents(events, limit)
	summary.Included = len(events)
	return events, summary, err
}

func readEventFiles(dir string, since time.Time, summary *Summary) ([]Event, error) {
	events := []Event{}
	for _, path := range eventPaths(dir) {
		fileEvents, err := readEventFile(path, since, summary)
		if err != nil {
			return events, err
		}
		events = append(events, fileEvents...)
	}
	return events, nil
}

func readEventFile(path string, since time.Time, summary *Summary) ([]Event, error) {
	info, err := os.Stat(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	summary.Bytes += info.Size()
	summary.Rotated = summary.Rotated || filepath.Base(path) == rotatedFile
	return scanEventFile(path, since, summary)
}

func scanEventFile(path string, since time.Time, summary *Summary) ([]Event, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	scanner := newEventScanner(file)
	return scanEvents(scanner, since, summary)
}

func newEventScanner(file *os.File) *bufio.Scanner {
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 4096), 1<<20)
	return scanner
}

func scanEvents(scanner *bufio.Scanner, since time.Time, summary *Summary) ([]Event, error) {
	events := []Event{}
	for scanner.Scan() {
		event, ok := decodeEvent(scanner.Bytes(), summary)
		if !ok {
			continue
		}
		if eventInRange(event, since) {
			events = append(events, event)
		}
	}
	return events, scanner.Err()
}

func decodeEvent(line []byte, summary *Summary) (Event, bool) {
	var event Event
	if err := json.Unmarshal(line, &event); err != nil {
		summary.Invalid++
		return nil, false
	}
	summary.Events++
	trackEventTimes(event, summary)
	return event, true
}

func eventInRange(event Event, since time.Time) bool {
	if since.IsZero() {
		return true
	}
	eventTime, ok := eventTime(event)
	if !ok {
		return false
	}
	return !eventTime.Before(since)
}

func eventTime(event Event) (time.Time, bool) {
	value, ok := event["time"].(string)
	if !ok {
		return time.Time{}, false
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	return parsed, err == nil
}

func trackEventTimes(event Event, summary *Summary) {
	eventTime, ok := eventTime(event)
	if !ok {
		return
	}
	formatted := eventTime.UTC().Format(time.RFC3339)
	if shouldReplaceOldest(summary.Oldest, formatted) {
		summary.Oldest = formatted
	}
	if shouldReplaceNewest(summary.Newest, formatted) {
		summary.Newest = formatted
	}
}

func shouldReplaceOldest(current, next string) bool {
	if current == "" {
		return true
	}
	return next < current
}

func shouldReplaceNewest(current, next string) bool {
	if current == "" {
		return true
	}
	return next > current
}

func limitEvents(events []Event, limit int) []Event {
	if limit <= 0 {
		return events
	}
	if len(events) <= limit {
		return events
	}
	return events[len(events)-limit:]
}

func stateDir() (string, error) {
	if dir := os.Getenv("PRE_OBS_DIR"); dir != "" {
		return dir, nil
	}
	return stateDirFn()
}

func defaultStateDir() (string, error) {
	if dir := os.Getenv("XDG_STATE_HOME"); dir != "" {
		return filepath.Join(dir, "pre"), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "state", "pre"), nil
}

func eventPaths(dir string) []string {
	return []string{
		filepath.Join(dir, rotatedFile),
		filepath.Join(dir, eventsFile),
	}
}

func addAttr(event Event, key string, value any) {
	if !safeKey(key) {
		return
	}
	if value == nil {
		return
	}
	if safe, ok := safeValue(value); ok {
		event[key] = safe
	}
}

func safeKey(key string) bool {
	if key == "" {
		return false
	}
	if len(key) > 80 {
		return false
	}
	for _, r := range key {
		if safeKeyRune(r) {
			continue
		}
		return false
	}
	return true
}

func safeKeyRune(r rune) bool {
	if r == '.' {
		return true
	}
	if r == '_' {
		return true
	}
	if r == '-' {
		return true
	}
	return safeKeyAlphaNumeric(r)
}

func safeKeyAlphaNumeric(r rune) bool {
	if runeInRange(r, 'a', 'z') {
		return true
	}
	if runeInRange(r, 'A', 'Z') {
		return true
	}
	return runeInRange(r, '0', '9')
}

func runeInRange(r, min, max rune) bool {
	if r < min {
		return false
	}
	return r <= max
}

func safeValue(value any) (any, bool) {
	switch v := value.(type) {
	case string:
		return sanitizeString(v, maxStringBytes), true
	case bool, int, int64, uint64, float64:
		return v, true
	case int32:
		return int64(v), true
	case uint:
		return uint64(v), true
	case time.Duration:
		return v.Milliseconds(), true
	default:
		return nil, false
	}
}

func sanitizeString(value string, limit int) string {
	value = strings.Map(safeRune, strings.TrimSpace(value))
	if len(value) <= limit {
		return value
	}
	return value[:limit]
}

func safeRune(r rune) rune {
	if r == '\n' {
		return -1
	}
	if r == '\r' {
		return -1
	}
	if r == '\t' {
		return -1
	}
	if r < 0x20 {
		return -1
	}
	if r == 0x7f {
		return -1
	}
	return r
}

func typeName(value any) string {
	t := reflect.TypeOf(value)
	if t == nil {
		return ""
	}
	if t.PkgPath() == "" {
		return t.String()
	}
	return fmt.Sprintf("%s.%s", t.PkgPath(), t.Name())
}
