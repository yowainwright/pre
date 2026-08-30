package obs

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func withObsTestState(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("PRE_OBS_DIR", dir)
	t.Setenv("PRE_OBS", "1")
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
	dir := withObsTestState(t)
	SetVersion(" 1.2.3 ")

	Record("pre.scan.completed", sanitizedEventAttrs())

	event := readSingleEvent(t, filepath.Join(dir, eventsFile))
	assertSanitizedEvent(t, event)
}

func sanitizedEventAttrs() map[string]any {
	return map[string]any{
		"manager":       "npm",
		"package_count": 2,
		"bad key":       "dropped",
		"raw":           "line\nbreak",
	}
}

func assertSanitizedEvent(t *testing.T, event Event) {
	t.Helper()
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
	t.Setenv("PRE_OBS_DIR", dir)
	t.Setenv("PRE_OBS", "0")

	Record("pre.scan.completed", nil)

	assertNoEventLog(t, dir)
	assertDisabledSummary(t)
}

func assertNoEventLog(t *testing.T, dir string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(dir, eventsFile)); !os.IsNotExist(err) {
		t.Fatalf("expected no event log, got %v", err)
	}
}

func assertDisabledSummary(t *testing.T) {
	t.Helper()
	summary, err := Status()
	if err != nil {
		t.Fatal(err)
	}
	if summary.Enabled {
		t.Fatalf("expected disabled empty summary, got %+v", summary)
	}
	if summary.Events != 0 {
		t.Fatalf("expected disabled empty summary, got %+v", summary)
	}
}

func TestEventsFiltersSinceAndLimit(t *testing.T) {
	withObsTestState(t)
	writeFilterFixture(t)

	since := time.Date(2026, 8, 26, 0, 0, 0, 0, time.UTC)
	events, summary, err := Events(since, 1)
	if err != nil {
		t.Fatal(err)
	}
	assertFilterResult(t, events, summary)
}

func writeFilterFixture(t *testing.T) {
	t.Helper()
	writeTimedEvent(t, "pre.old", time.Date(2026, 8, 25, 10, 0, 0, 0, time.UTC))
	writeTimedEvent(t, "pre.first", time.Date(2026, 8, 26, 10, 0, 0, 0, time.UTC))
	writeTimedEvent(t, "pre.second", time.Date(2026, 8, 26, 11, 0, 0, 0, time.UTC))
}

func assertFilterResult(t *testing.T, events []Event, summary Summary) {
	t.Helper()
	if summary.Events != 3 {
		t.Fatalf("expected total event count, got %d", summary.Events)
	}
	if len(events) != 1 {
		t.Fatalf("expected newest filtered event, got %#v", events)
	}
	if events[0]["event.name"] != "pre.second" {
		t.Fatalf("expected newest filtered event, got %#v", events)
	}
}

func TestEventsSkipsInvalidLines(t *testing.T) {
	dir := withObsTestState(t)
	writeInvalidEventFixture(t, dir)

	events, summary, err := Events(time.Time{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	assertValidEventOnly(t, events)
	assertInvalidEventSummary(t, summary)
}

func writeInvalidEventFixture(t *testing.T, dir string) {
	t.Helper()
	data := "not json\n{\"time\":\"2026-08-26T10:00:00Z\",\"event.name\":\"pre.valid\"}\n"
	if err := os.WriteFile(filepath.Join(dir, eventsFile), []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
}

func assertValidEventOnly(t *testing.T, events []Event) {
	t.Helper()
	if len(events) != 1 {
		t.Fatalf("expected one valid event, got %#v", events)
	}
	if events[0]["event.name"] != "pre.valid" {
		t.Fatalf("expected one valid event, got %#v", events)
	}
}

func assertInvalidEventSummary(t *testing.T, summary Summary) {
	t.Helper()
	if summary.Events != 1 {
		t.Fatalf("expected one valid and one invalid line, got %+v", summary)
	}
	if summary.Invalid != 1 {
		t.Fatalf("expected one valid and one invalid line, got %+v", summary)
	}
	if summary.Oldest != "2026-08-26T10:00:00Z" {
		t.Fatalf("unexpected event range: %+v", summary)
	}
	if summary.Newest != "2026-08-26T10:00:00Z" {
		t.Fatalf("unexpected event range: %+v", summary)
	}
}

func TestRotateKeepsLogBounded(t *testing.T) {
	dir := withObsTestState(t)
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

func TestErrorTypeOmitsErrorMessageDetails(t *testing.T) {
	got := ErrorType(&os.PathError{Op: "open", Path: "/secret/path", Err: os.ErrNotExist})
	if got == "" {
		t.Fatal("expected error type")
	}
	if strings.Contains(got, "secret") {
		t.Fatalf("expected type only, got %q", got)
	}
	if strings.Contains(got, "not exist") {
		t.Fatalf("expected type only, got %q", got)
	}
	if ErrorType(nil) != "" {
		t.Fatal("expected nil error type to be empty")
	}
}

func TestStateDirUsesXDGStateHome(t *testing.T) {
	root := t.TempDir()
	t.Setenv("PRE_OBS_DIR", "")
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
