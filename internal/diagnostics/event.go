package diagnostics

import (
	"encoding/json"
	"os"
	"runtime/metrics"
	"strings"
	"sync"
	"time"
)

const (
	SchemaVersion   = 1
	DefaultMaxBytes = 5 << 20
)

type Event map[string]any

var (
	appendMu       sync.Mutex
	currentVersion string
	maxBytes       int64 = DefaultMaxBytes
	nowFn                = time.Now
)

func SetVersion(version string) {
	currentVersion = strings.TrimSpace(version)
}

func Enabled() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("PRE_DIAGNOSTICS"))) {
	case "0", "false", "no", "off":
		return false
	}
	isTest := strings.HasSuffix(os.Args[0], ".test")
	return !isTest || os.Getenv("PRE_DIAGNOSTICS_DIR") != "" || os.Getenv("PRE_DIAGNOSTICS") != ""
}

func Record(name string, attrs map[string]any) {
	if name == "" || !Enabled() {
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

func Marshal(event Event) ([]byte, error) {
	return json.Marshal(event)
}
