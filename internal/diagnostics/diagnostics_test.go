package diagnostics

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func withDiagnosticsTestState(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("PRE_DIAGNOSTICS_DIR", dir)
	t.Setenv("PRE_DIAGNOSTICS", "1")
	origNow := nowFn
	origMaxBytes := maxBytes
	origVersion := currentVersion
	nowFn = func() time.Time { return time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC) }
	maxBytes = DefaultMaxBytes
	currentVersion = ""
	t.Cleanup(func() {
		nowFn = origNow
		maxBytes = origMaxBytes
		currentVersion = origVersion
	})
	return dir
}

func TestRecordWritesSanitizedEvent(t *testing.T) {
	dir := withDiagnosticsTestState(t)
	SetVersion(" 1.2.3 ")

	Record("pre.scan.completed", map[string]any{
		"manager":       "npm",
		"package_count": 2,
		"bad key":       "dropped",
		"raw":           "line\nbreak",
	})

	event := readSingleEvent(t, filepath.Join(dir, eventsFile))
	if event["event.name"] != "pre.scan.completed" {
		t.Fatalf("unexpected event: %#v", event)
	}
	if event["pre.version"] != "1.2.3" {
		t.Fatalf("expected version, got %#v", event["pre.version"])
	}
	if _, ok := event["bad key"]; ok {
		t.Fatal("unsafe key was recorded")
	}
	if strings.Contains(event["raw"].(string), "\n") {
		t.Fatal("newline was not removed")
	}
}

func TestRecordDisabledWritesNothing(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PRE_DIAGNOSTICS_DIR", dir)
	t.Setenv("PRE_DIAGNOSTICS", "0")

	Record("pre.scan.completed", nil)

	if _, err := os.Stat(filepath.Join(dir, eventsFile)); !os.IsNotExist(err) {
		t.Fatalf("expected no event log, got %v", err)
	}
	summary, err := Status()
	if err != nil {
		t.Fatal(err)
	}
	if summary.Enabled || summary.Events != 0 {
		t.Fatalf("expected disabled empty summary, got %+v", summary)
	}
}

func TestEventsFiltersSinceAndLimit(t *testing.T) {
	withDiagnosticsTestState(t)
	writeTimedEvent(t, "pre.old", time.Date(2026, 8, 25, 10, 0, 0, 0, time.UTC))
	writeTimedEvent(t, "pre.first", time.Date(2026, 8, 26, 10, 0, 0, 0, time.UTC))
	writeTimedEvent(t, "pre.second", time.Date(2026, 8, 26, 11, 0, 0, 0, time.UTC))

	since := time.Date(2026, 8, 26, 0, 0, 0, 0, time.UTC)
	events, summary, err := Events(since, 1)
	if err != nil {
		t.Fatal(err)
	}
	if summary.Events != 3 {
		t.Fatalf("expected total event count, got %d", summary.Events)
	}
	if len(events) != 1 || events[0]["event.name"] != "pre.second" {
		t.Fatalf("expected newest filtered event, got %#v", events)
	}
}

func TestEventsSkipsInvalidLines(t *testing.T) {
	dir := withDiagnosticsTestState(t)
	data := "not json\n{\"time\":\"2026-08-26T10:00:00Z\",\"event.name\":\"pre.valid\"}\n"
	if err := os.WriteFile(filepath.Join(dir, eventsFile), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}

	events, summary, err := Events(time.Time{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 || events[0]["event.name"] != "pre.valid" {
		t.Fatalf("expected one valid event, got %#v", events)
	}
	if summary.Events != 1 || summary.Invalid != 1 {
		t.Fatalf("expected one valid and one invalid line, got %+v", summary)
	}
	if summary.Oldest != "2026-08-26T10:00:00Z" || summary.Newest != "2026-08-26T10:00:00Z" {
		t.Fatalf("unexpected event range: %+v", summary)
	}
}

func TestRotateKeepsLogBounded(t *testing.T) {
	dir := withDiagnosticsTestState(t)
	maxBytes = 40
	Record("pre.one", nil)
	Record("pre.two", nil)

	if _, err := os.Stat(filepath.Join(dir, rotatedFile)); err != nil {
		t.Fatalf("expected rotated log: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, eventsFile)); err != nil {
		t.Fatalf("expected current log: %v", err)
	}
}

func TestExportWritesShareableReport(t *testing.T) {
	dir := withDiagnosticsTestState(t)
	Record("pre.scan.completed", map[string]any{"manager": "npm"})

	path, summary, err := Export(time.Time{})
	if err != nil {
		t.Fatal(err)
	}
	if summary.Events != 1 {
		t.Fatalf("expected one event, got %d", summary.Events)
	}
	if !strings.HasPrefix(path, filepath.Join(dir, reportsDir)) {
		t.Fatalf("unexpected report path: %s", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), dir) {
		t.Fatal("report leaked local diagnostics path")
	}
}

func TestExportIncludesSinceLabel(t *testing.T) {
	withDiagnosticsTestState(t)
	since := time.Date(2026, 8, 26, 8, 0, 0, 0, time.UTC)

	path, _, err := Export(since)
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var report Report
	if err := json.Unmarshal(data, &report); err != nil {
		t.Fatal(err)
	}
	if report.Since != "2026-08-26T08:00:00Z" {
		t.Fatalf("expected RFC3339 since label, got %q", report.Since)
	}
}

func TestClearRemovesEventLogs(t *testing.T) {
	dir := withDiagnosticsTestState(t)
	Record("pre.scan.completed", nil)

	if err := os.WriteFile(filepath.Join(dir, rotatedFile), []byte("{}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := Clear(); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{eventsFile, rotatedFile} {
		if _, err := os.Stat(filepath.Join(dir, name)); !os.IsNotExist(err) {
			t.Fatalf("expected %s removed, got %v", name, err)
		}
	}
}

func TestErrorTypeOmitsErrorMessageDetails(t *testing.T) {
	got := ErrorType(&os.PathError{Op: "open", Path: "/secret/path", Err: os.ErrNotExist})
	if got == "" {
		t.Fatal("expected error type")
	}
	if strings.Contains(got, "secret") || strings.Contains(got, "not exist") {
		t.Fatalf("expected type only, got %q", got)
	}
	if ErrorType(nil) != "" {
		t.Fatal("expected nil error type to be empty")
	}
}

func TestStateDirUsesXDGStateHome(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PRE_DIAGNOSTICS_DIR", "")
	t.Setenv("XDG_STATE_HOME", root)
	origStateDir := stateDirFn
	stateDirFn = defaultStateDir
	defer func() { stateDirFn = origStateDir }()

	got, err := stateDir()
	if err != nil {
		t.Fatal(err)
	}
	if got != filepath.Join(root, "pre") {
		t.Fatalf("unexpected state dir: %s", got)
	}
}

func readSingleEvent(t *testing.T, path string) Event {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var event Event
	if err := json.Unmarshal(data, &event); err != nil {
		t.Fatal(err)
	}
	return event
}

func writeTimedEvent(t *testing.T, name string, timestamp time.Time) {
	t.Helper()
	origNow := nowFn
	nowFn = func() time.Time { return timestamp }
	Record(name, nil)
	nowFn = origNow
}
