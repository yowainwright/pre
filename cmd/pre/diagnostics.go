package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/yowainwright/pre/internal/diagnostics"
)

const diagnosticsUsage = "usage: pre diagnostics status | events [--since 24h] [--limit 50] | export [--since 24h] | clear"

type diagnosticsOptions struct {
	since time.Time
	limit int
}

func handleDiagnostics(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, diagnosticsUsage)
		return 1
	}
	switch args[0] {
	case "status":
		return diagnosticsStatus(stdout, stderr)
	case "events":
		return diagnosticsEvents(args[1:], stdout, stderr)
	case "export":
		return diagnosticsExport(args[1:], stdout, stderr)
	case "clear":
		return diagnosticsClear(stdout, stderr)
	default:
		fmt.Fprintln(stderr, diagnosticsUsage)
		return 1
	}
}

func diagnosticsStatus(stdout, stderr io.Writer) int {
	summary, err := diagnostics.Status()
	if err != nil {
		fmt.Fprintf(stderr, "pre diagnostics: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "diagnostics: %s\n", enabledLabel(summary.Enabled))
	fmt.Fprintf(stdout, "events: %d\n", summary.Events)
	fmt.Fprintf(stdout, "invalid: %d\n", summary.Invalid)
	fmt.Fprintf(stdout, "bytes: %d / %d\n", summary.Bytes, summary.MaxBytes)
	fmt.Fprintf(stdout, "rotated: %s\n", boolLabel(summary.Rotated))
	return 0
}

func diagnosticsEvents(args []string, stdout, stderr io.Writer) int {
	opts, err := parseDiagnosticsOptions(args, 24*time.Hour, 50)
	if err != nil {
		fmt.Fprintf(stderr, "pre diagnostics: %v\n", err)
		return 1
	}
	events, summary, err := diagnostics.Events(opts.since, opts.limit)
	if err != nil {
		fmt.Fprintf(stderr, "pre diagnostics: %v\n", err)
		return 1
	}
	writeDiagnosticsEvents(stdout, events)
	warnInvalidDiagnostics(stderr, summary)
	return 0
}

func diagnosticsExport(args []string, stdout, stderr io.Writer) int {
	opts, err := parseDiagnosticsOptions(args, 24*time.Hour, 0)
	if err != nil {
		fmt.Fprintf(stderr, "pre diagnostics: %v\n", err)
		return 1
	}
	path, summary, err := diagnostics.Export(opts.since)
	if err != nil {
		fmt.Fprintf(stderr, "pre diagnostics: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "diagnostics report: %s\n", path)
	fmt.Fprintf(stdout, "events: %d\n", summary.Included)
	warnInvalidDiagnostics(stderr, summary)
	return 0
}

func diagnosticsClear(stdout, stderr io.Writer) int {
	if err := diagnostics.Clear(); err != nil {
		fmt.Fprintf(stderr, "pre diagnostics: %v\n", err)
		return 1
	}
	fmt.Fprintln(stdout, "diagnostics cleared")
	return 0
}

func writeDiagnosticsEvents(stdout io.Writer, events []diagnostics.Event) {
	if len(events) == 0 {
		fmt.Fprintln(stdout, "no diagnostics events")
		return
	}
	for _, event := range events {
		data, _ := json.Marshal(event)
		fmt.Fprintln(stdout, string(data))
	}
}

func warnInvalidDiagnostics(stderr io.Writer, summary diagnostics.Summary) {
	if summary.Invalid > 0 {
		fmt.Fprintf(stderr, "pre diagnostics: skipped %d invalid event line(s)\n", summary.Invalid)
	}
}

func boolLabel(value bool) string {
	if value {
		return "yes"
	}
	return "no"
}

func parseDiagnosticsOptions(args []string, defaultSince time.Duration, defaultLimit int) (diagnosticsOptions, error) {
	opts := diagnosticsOptions{since: sinceCutoff(defaultSince), limit: defaultLimit}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch {
		case arg == "--since" && i+1 < len(args):
			i++
			since, err := parseSinceCutoff(args[i])
			if err != nil {
				return opts, err
			}
			opts.since = since
		case strings.HasPrefix(arg, "--since="):
			since, err := parseSinceCutoff(strings.TrimPrefix(arg, "--since="))
			if err != nil {
				return opts, err
			}
			opts.since = since
		case arg == "--limit" && i+1 < len(args):
			i++
			limit, err := parseDiagnosticsLimit(args[i])
			if err != nil {
				return opts, err
			}
			opts.limit = limit
		case strings.HasPrefix(arg, "--limit="):
			limit, err := parseDiagnosticsLimit(strings.TrimPrefix(arg, "--limit="))
			if err != nil {
				return opts, err
			}
			opts.limit = limit
		default:
			return opts, errors.New(diagnosticsUsage)
		}
	}
	return opts, nil
}

func parseSinceCutoff(value string) (time.Time, error) {
	if strings.EqualFold(strings.TrimSpace(value), "all") {
		return time.Time{}, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil || duration <= 0 {
		return time.Time{}, fmt.Errorf("invalid --since %q", value)
	}
	return sinceCutoff(duration), nil
}

func sinceCutoff(duration time.Duration) time.Time {
	if duration <= 0 {
		return time.Time{}
	}
	return time.Now().Add(-duration)
}

func parseDiagnosticsLimit(value string) (int, error) {
	limit, err := strconv.Atoi(value)
	if err != nil || limit < 0 {
		return 0, fmt.Errorf("invalid --limit %q", value)
	}
	return limit, nil
}
