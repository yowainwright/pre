package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"time"

	"github.com/yowainwright/pre/internal/cache"
	"github.com/yowainwright/pre/internal/obs"
)

const obsUsage = "usage: pre obs [--json] [--events [query]]"

type obsOptions struct {
	asJSON     bool
	eventsOnly bool
	query      string
}

type obsReadResponse struct {
	Status  string         `json:"status"`
	Cache   cache.Stats    `json:"cache"`
	Process obsProcessInfo `json:"process"`
	Scans   obsScanInfo    `json:"scans"`
	Runtime obsRuntimeInfo `json:"runtime"`
	Events  []obs.Event    `json:"events"`
	Obs     obs.Summary    `json:"obs"`
}

type obsProcessInfo struct {
	Background string `json:"background"`
}

type obsScanInfo struct {
	Allowed int `json:"allowed"`
	Blocked int `json:"blocked"`
	Failed  int `json:"failed"`
}

type obsRuntimeInfo struct {
	HeapObjectsBytes uint64 `json:"heap_objects_bytes,omitempty"`
	Goroutines       uint64 `json:"goroutines,omitempty"`
}

func handleObs(args []string, stdout, stderr io.Writer) int {
	options, err := parseObsOptions(args)
	if err != nil {
		return writeObsError(stderr, err)
	}
	response, err := readObsResponse(options.query)
	if err != nil {
		return writeObsError(stderr, err)
	}
	return writeObsResponse(options, stdout, stderr, response)
}

func readObsResponse(query string) (obsReadResponse, error) {
	events, summary, err := obs.Events(time.Time{}, 0)
	if err != nil {
		return obsReadResponse{}, err
	}
	events = filterObsEvents(events, query)
	summary.Included = len(events)
	return buildObsResponse(summary, events), nil
}

func writeObsResponse(options obsOptions, stdout, stderr io.Writer, response obsReadResponse) int {
	if options.asJSON {
		return writeObsJSON(stdout, stderr, response)
	}
	if options.eventsOnly {
		writeObsEvents(stdout, response.Events)
		return 0
	}
	writeObsText(stdout, response)
	return 0
}

func writeObsError(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "pre obs: %v\n", err)
	return 1
}

func parseObsOptions(args []string) (obsOptions, error) {
	options := obsOptions{}
	for i := 0; i < len(args); i++ {
		if err := parseObsOption(args, &i, &options); err != nil {
			return options, err
		}
	}
	return options, nil
}

func parseObsOption(args []string, index *int, options *obsOptions) error {
	arg := args[*index]
	switch {
	case arg == "--json":
		options.asJSON = true
	case arg == "--events":
		parseObsEventsOption(args, index, options)
	case strings.HasPrefix(arg, "--events="):
		options.eventsOnly = true
		options.query = strings.TrimSpace(strings.TrimPrefix(arg, "--events="))
	default:
		return errors.New(obsUsage)
	}
	return nil
}

func parseObsEventsOption(args []string, index *int, options *obsOptions) {
	options.eventsOnly = true
	next := *index + 1
	if next >= len(args) {
		return
	}
	if strings.HasPrefix(args[next], "-") {
		return
	}
	*index = next
	options.query = args[next]
}

func buildObsResponse(summary obs.Summary, events []obs.Event) obsReadResponse {
	return obsReadResponse{
		Status:  "ok",
		Cache:   cache.FileStats(),
		Process: obsProcessInfo{Background: "none"},
		Scans:   countObsScans(events),
		Runtime: newestRuntime(events),
		Events:  events,
		Obs:     summary,
	}
}

func writeObsJSON(stdout, stderr io.Writer, response obsReadResponse) int {
	data, err := json.MarshalIndent(response, "", "  ")
	if err != nil {
		fmt.Fprintf(stderr, "pre obs: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, string(data))
	return 0
}

func writeObsText(stdout io.Writer, response obsReadResponse) {
	fmt.Fprintf(stdout, "status: %s\n", response.Status)
	fmt.Fprintf(stdout, "cache:\n")
	fmt.Fprintf(stdout, "  entries: %d\n", response.Cache.Entries)
	fmt.Fprintf(stdout, "  bytes: %d\n", response.Cache.Bytes)
	fmt.Fprintf(stdout, "  max_entries: %d\n", response.Cache.MaxEntries)
	fmt.Fprintf(stdout, "  max_bytes: %d\n", response.Cache.MaxBytes)
	fmt.Fprintf(stdout, "process:\n")
	fmt.Fprintf(stdout, "  background: %s\n", response.Process.Background)
	fmt.Fprintf(stdout, "scans:\n")
	fmt.Fprintf(stdout, "  allowed: %d\n", response.Scans.Allowed)
	fmt.Fprintf(stdout, "  blocked: %d\n", response.Scans.Blocked)
	fmt.Fprintf(stdout, "  failed: %d\n", response.Scans.Failed)
	fmt.Fprintf(stdout, "runtime:\n")
	fmt.Fprintf(stdout, "  heap_objects_bytes: %d\n", response.Runtime.HeapObjectsBytes)
	fmt.Fprintf(stdout, "  goroutines: %d\n", response.Runtime.Goroutines)
	fmt.Fprintf(stdout, "events:\n")
	writeObsEventLines(stdout, response.Events)
}

func writeObsEvents(stdout io.Writer, events []obs.Event) {
	writeObsEventLines(stdout, events)
}

func writeObsEventLines(stdout io.Writer, events []obs.Event) {
	if len(events) == 0 {
		fmt.Fprintln(stdout, "  none")
		return
	}
	for _, event := range events {
		fmt.Fprintf(stdout, "  - %s %s\n", eventTimeLabel(event), eventName(event))
	}
}

func filterObsEvents(events []obs.Event, query string) []obs.Event {
	query = strings.ToLower(strings.TrimSpace(query))
	if query == "" {
		return events
	}
	filtered := make([]obs.Event, 0, len(events))
	for _, event := range events {
		data, _ := json.Marshal(event)
		if strings.Contains(strings.ToLower(string(data)), query) {
			filtered = append(filtered, event)
		}
	}
	return filtered
}

func countObsScans(events []obs.Event) obsScanInfo {
	info := obsScanInfo{}
	for _, event := range events {
		addObsScanEvent(&info, event)
	}
	return info
}

func addObsScanEvent(info *obsScanInfo, event obs.Event) {
	name := eventName(event)
	decision, _ := event["decision"].(string)
	if isAllowedObsEvent(name, decision) {
		info.Allowed++
	}
	if isBlockedObsEvent(name, decision) {
		info.Blocked++
	}
	if isFailedObsEvent(name, event) {
		info.Failed++
	}
}

func isFailedObsEvent(name string, event obs.Event) bool {
	if strings.Contains(name, "failed") {
		return true
	}
	return numberAttr(event, "error_count") > 0
}

func isAllowedObsEvent(name, decision string) bool {
	allowed := []string{"approved", "passthrough", "bypassed"}
	if slices.Contains(allowed, decision) {
		return true
	}
	return name == "pre.manager.exec.completed"
}

func isBlockedObsEvent(name, decision string) bool {
	blockedDecision := decision == "blocked" || decision == "denied"
	return blockedDecision || strings.Contains(name, "blocked")
}

func newestRuntime(events []obs.Event) obsRuntimeInfo {
	for index := len(events) - 1; index >= 0; index-- {
		heap := numberAttr(events[index], "go.heap_objects_bytes")
		goroutines := numberAttr(events[index], "go.goroutines")
		hasRuntime := heap > 0 || goroutines > 0
		if hasRuntime {
			return obsRuntimeInfo{HeapObjectsBytes: heap, Goroutines: goroutines}
		}
	}
	return obsRuntimeInfo{}
}

func eventName(event obs.Event) string {
	name, _ := event["event.name"].(string)
	return name
}

func eventTimeLabel(event obs.Event) string {
	value, _ := event["time"].(string)
	if value == "" {
		return "unknown"
	}
	return value
}

func numberAttr(event obs.Event, key string) uint64 {
	switch value := event[key].(type) {
	case float64:
		if value > 0 {
			return uint64(value)
		}
	case int:
		if value > 0 {
			return uint64(value)
		}
	case int64:
		if value > 0 {
			return uint64(value)
		}
	case uint64:
		return value
	}
	return 0
}
