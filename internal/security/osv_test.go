package security

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCheckWithVulns(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"vulns":[{"id":"CVE-2021-1234","summary":"test vuln"}]}`)
	}))
	defer srv.Close()

	origEndpoint := Endpoint
	Endpoint = srv.URL
	defer func() { Endpoint = origEndpoint }()

	vulns, err := Check("npm", "lodash", "4.17.20")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vulns) != 1 {
		t.Errorf("expected 1 vuln, got %d", len(vulns))
	}
	if vulns[0].ID != "CVE-2021-1234" {
		t.Errorf("expected ID 'CVE-2021-1234', got %q", vulns[0].ID)
	}
	if vulns[0].Summary != "test vuln" {
		t.Errorf("expected summary 'test vuln', got %q", vulns[0].Summary)
	}
}

func TestCheckEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"vulns":null}`)
	}))
	defer srv.Close()

	origEndpoint := Endpoint
	Endpoint = srv.URL
	defer func() { Endpoint = origEndpoint }()

	vulns, err := Check("npm", "react", "18.0.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vulns) != 0 {
		t.Errorf("expected 0 vulns, got %d", len(vulns))
	}
}

func TestCheckSeverityFromDatabaseSpecific(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"vulns":[{"id":"CVE-2021-1234","summary":"test","database_specific":{"severity":"CRITICAL"}}]}`)
	}))
	defer srv.Close()
	origEndpoint := Endpoint
	Endpoint = srv.URL
	defer func() { Endpoint = origEndpoint }()

	vulns, err := Check("npm", "lodash", "4.17.11")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if vulns[0].Severity != "CRITICAL" {
		t.Errorf("expected CRITICAL severity, got %q", vulns[0].Severity)
	}
}

func TestCheckSeverityFromDatabaseSpecificNormalizes(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"vulns":[{"id":"CVE-2021-1234","summary":"test","database_specific":{"severity":" high "}}]}`)
	}))
	defer srv.Close()
	origEndpoint := Endpoint
	Endpoint = srv.URL
	defer func() { Endpoint = origEndpoint }()

	vulns, err := Check("npm", "lodash", "4.17.11")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if vulns[0].Severity != "HIGH" {
		t.Errorf("expected HIGH severity, got %q", vulns[0].Severity)
	}
}

func TestCheckUnknownDatabaseSpecificSeverityFallsBackToCVSS(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"vulns":[{"id":"CVE-2021-5678","summary":"test","database_specific":{"severity":"IMPORTANT"},"severity":[{"type":"CVSS_V3","score":"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H"}]}]}`)
	}))
	defer srv.Close()
	origEndpoint := Endpoint
	Endpoint = srv.URL
	defer func() { Endpoint = origEndpoint }()

	vulns, err := Check("npm", "lodash", "4.17.11")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if vulns[0].Severity != "CRITICAL" {
		t.Errorf("expected CRITICAL from CVSS fallback, got %q", vulns[0].Severity)
	}
	if vulns[0].Score < 9.0 {
		t.Errorf("expected CVSS fallback score >= 9.0, got %.1f", vulns[0].Score)
	}
}

func TestNormalizeSeverity(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"critical", "CRITICAL"},
		{"HIGH", "HIGH"},
		{"moderate", "MEDIUM"},
		{" Medium ", "MEDIUM"},
		{"low", "LOW"},
		{"unknown", ""},
		{"", ""},
	}
	for _, tc := range tests {
		if got := normalizeSeverity(tc.input); got != tc.want {
			t.Errorf("normalizeSeverity(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestCheckSeverityFromCVSSVector(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"vulns":[{"id":"CVE-2021-5678","summary":"test","severity":[{"type":"CVSS_V3","score":"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H"}]}]}`)
	}))
	defer srv.Close()
	origEndpoint := Endpoint
	Endpoint = srv.URL
	defer func() { Endpoint = origEndpoint }()

	vulns, err := Check("npm", "lodash", "4.17.11")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if vulns[0].Severity != "CRITICAL" {
		t.Errorf("expected CRITICAL from CVSS vector, got %q", vulns[0].Severity)
	}
	if vulns[0].Score < 9.0 {
		t.Errorf("expected score >= 9.0, got %.1f", vulns[0].Score)
	}
}

func TestCheckFailsClosedForCVSSV4(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, `{"vulns":[{"id":"CVE-2026-1234","severity":[{"type":"CVSS_V4","score":"CVSS:4.0/AV:N/AC:L/AT:N/PR:N/UI:N/VC:H/VI:H/VA:H/SC:H/SI:H/SA:H"}]}]}`)
	}))
	defer srv.Close()
	origEndpoint := Endpoint
	Endpoint = srv.URL
	defer func() { Endpoint = origEndpoint }()

	vulns, err := Check("npm", "example", "1.0.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if vulns[0].Severity != SeverityCritical {
		t.Errorf("expected unsupported CVSS v4 to fail closed, got %q", vulns[0].Severity)
	}
}

func TestCheckHTTPError(t *testing.T) {
	origEndpoint := Endpoint
	Endpoint = "http://invalid.local.invalid"
	defer func() { Endpoint = origEndpoint }()

	_, err := Check("npm", "react", "18.0.0")
	if err == nil {
		t.Error("expected error for invalid Endpoint")
	}
}

func TestCheckInvalidURLScheme(t *testing.T) {
	origEndpoint := Endpoint
	Endpoint = "://bad"
	defer func() { Endpoint = origEndpoint }()

	_, err := Check("npm", "react", "18.0.0")
	if err == nil {
		t.Error("expected error for invalid URL scheme")
	}
}

func TestCheckInvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "not json")
	}))
	defer srv.Close()

	origEndpoint := Endpoint
	Endpoint = srv.URL
	defer func() { Endpoint = origEndpoint }()

	_, err := Check("npm", "react", "18.0.0")
	if err == nil {
		t.Error("expected error for invalid JSON response")
	}
}

func TestCheckRejectsOversizedResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Length", fmt.Sprint(maxOSVResponseBytes+1))
		_, _ = io.CopyN(w, zeroReader{}, maxOSVResponseBytes+1)
	}))
	defer srv.Close()
	origEndpoint := Endpoint
	Endpoint = srv.URL
	defer func() { Endpoint = origEndpoint }()

	if _, err := Check("npm", "react", "18.0.0"); err == nil {
		t.Fatal("expected oversized OSV response to fail")
	}
}

type zeroReader struct{}

func (zeroReader) Read(buffer []byte) (int, error) {
	for index := range buffer {
		buffer[index] = ' '
	}
	return len(buffer), nil
}

type osvBatchTestHandler struct {
	requests             int
	batchSizes           []int
	includeVulnerability bool
	detailStatus         int
}

func (handler *osvBatchTestHandler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	handler.requests++
	if request.URL.Path == "/v1/query" {
		handler.serveDetails(writer)
		return
	}
	var payload osvBatchRequest
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	handler.batchSizes = append(handler.batchSizes, len(payload.Queries))
	results := make([]osvResponse, len(payload.Queries))
	if handler.includeVulnerability && len(results) > 0 {
		vulnerability := osvVulnerability{ID: "CVE-2026-1234"}
		results[0].Vulns = []osvVulnerability{vulnerability}
	}
	_ = json.NewEncoder(writer).Encode(osvBatchResponse{Results: results})
}

func (handler *osvBatchTestHandler) serveDetails(writer http.ResponseWriter) {
	if handler.detailStatus != 0 {
		http.Error(writer, "detail lookup failed", handler.detailStatus)
		return
	}
	_, _ = fmt.Fprintln(writer, `{"vulns":[{"id":"CVE-2026-1234","summary":"batch test","database_specific":{"severity":"CRITICAL"}}]}`)
}

type osvSingleTestHandler struct {
	requests int
}

func (handler *osvSingleTestHandler) ServeHTTP(writer http.ResponseWriter, _ *http.Request) {
	handler.requests++
	_, _ = fmt.Fprintln(writer, `{"vulns":null}`)
}

func TestCheckBatch(t *testing.T) {
	handler := &osvBatchTestHandler{includeVulnerability: true}
	server := httptest.NewServer(handler)
	defer server.Close()
	originalEndpoint := Endpoint
	Endpoint = server.URL + "/v1/query"
	defer func() { Endpoint = originalEndpoint }()

	first := Query{Ecosystem: "npm", Name: "react", Version: "18.0.0"}
	second := Query{Ecosystem: "PyPI", Name: "requests", Version: "2.31.0"}
	results, err := CheckBatch([]Query{first, second})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected two results, got %d", len(results))
	}
	if len(results[0]) != 1 || results[0][0].ID != "CVE-2026-1234" {
		t.Fatalf("unexpected first result: %+v", results[0])
	}
	hasFullDetails := results[0][0].Severity == SeverityCritical && results[0][0].Summary == "batch test"
	if !hasFullDetails {
		t.Errorf("expected full vulnerability details, got %+v", results[0][0])
	}
	hasExpectedBatchResult := handler.requests == 2 && len(results[1]) == 0
	if !hasExpectedBatchResult {
		t.Errorf("expected one detail lookup and a clean second result, got %d requests and %+v", handler.requests, results[1])
	}
}

func TestCheckBatchDetailFailure(t *testing.T) {
	handler := &osvBatchTestHandler{includeVulnerability: true, detailStatus: http.StatusServiceUnavailable}
	server := httptest.NewServer(handler)
	defer server.Close()
	originalEndpoint := Endpoint
	Endpoint = server.URL + "/v1/query"
	defer func() { Endpoint = originalEndpoint }()

	query := Query{Ecosystem: "npm", Name: "lodash", Version: "4.17.20"}
	results, err := CheckBatch([]Query{query})
	if err == nil {
		t.Fatalf("expected failed detail lookup to fail the scan, got %+v", results)
	}
}

func TestCheckBatchDetailsConcurrent(t *testing.T) {
	for _, failure := range []string{"", "package-2"} {
		t.Run("failure="+failure, func(t *testing.T) {
			checkConcurrentOSVDetails(t, failure)
		})
	}
}

func checkConcurrentOSVDetails(t *testing.T, failure string) {
	t.Helper()
	const count = 9
	started := make(chan string, count)
	release := make(chan struct{}, count)
	setupOSVHeldDetails(t, started, release, failure)
	queries, matches := osvConcurrentDetailQueries(count)
	resultCh := make(chan [][]Vulnerability, 1)
	errorCh := make(chan error, 1)
	go func() {
		results, err := checkBatchDetails(queries, matches)
		resultCh <- results
		errorCh <- err
	}()
	assertOSVDetailOverlap(t, started)
	for index := 0; index < count; index++ {
		release <- struct{}{}
	}
	results := <-resultCh
	err := <-errorCh
	if len(started) != count-5 {
		t.Errorf("expected exactly %d detail requests, including the four held requests", count-1)
	}
	assertOSVConcurrentResults(t, queries, results, err, failure)
}

func setupOSVHeldDetails(t *testing.T, started chan string, release chan struct{}, failure string) {
	t.Helper()
	server := httptest.NewServer(osvHeldDetailHandler(started, release, failure))
	t.Cleanup(server.Close)
	t.Cleanup(func() { close(release) })
	original := Endpoint
	Endpoint = server.URL
	t.Cleanup(func() { Endpoint = original })
}

func assertOSVConcurrentResults(t *testing.T, queries []Query, results [][]Vulnerability, err error, failure string) {
	t.Helper()
	if failure != "" {
		unsafe := err == nil || results != nil
		if unsafe {
			t.Fatalf("failed lookup must fail the whole scan: results=%v error=%v", results, err)
		}
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	assertOSVDetailOrder(t, queries, results)
}

func assertOSVDetailOrder(t *testing.T, queries []Query, results [][]Vulnerability) {
	t.Helper()
	last := len(queries) - 1
	for index, query := range queries[:last] {
		misordered := len(results[index]) != 1 || results[index][0].ID != query.Name
		if misordered {
			t.Fatalf("misordered result for %s: %v", query.Name, results[index])
		}
	}
	if len(results[last]) != 0 {
		t.Fatal("clean query must remain clean without a detail lookup")
	}
}

func assertOSVDetailOverlap(t *testing.T, started <-chan string) {
	t.Helper()
	for index := 0; index < 4; index++ {
		select {
		case <-started:
		case <-time.After(2 * time.Second):
			t.Error("expected four overlapping detail requests")
			return
		}
	}
	select {
	case name := <-started:
		t.Errorf("concurrency limit exceeded by %s", name)
	case <-time.After(50 * time.Millisecond):
	}
}

func osvConcurrentDetailQueries(count int) ([]Query, []osvResponse) {
	queries := make([]Query, count)
	matches := make([]osvResponse, count)
	for index := range queries {
		name := fmt.Sprintf("package-%d", index)
		queries[index] = Query{Ecosystem: "npm", Name: name, Version: "1.0.0"}
		if index < count-1 {
			matches[index].Vulns = []osvVulnerability{{ID: name}}
		}
	}
	return queries, matches
}

func osvHeldDetailHandler(started chan<- string, release <-chan struct{}, failure string) http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var query osvQuery
		if err := json.NewDecoder(request.Body).Decode(&query); err != nil {
			http.Error(writer, err.Error(), http.StatusBadRequest)
			return
		}
		started <- query.Package.Name
		<-release
		if query.Package.Name == failure {
			http.Error(writer, "detail failed", http.StatusServiceUnavailable)
			return
		}
		response := osvResponse{Vulns: []osvVulnerability{{ID: query.Package.Name}}}
		_ = json.NewEncoder(writer).Encode(response)
	})
}

func TestCheckBatchChunksQueries(t *testing.T) {
	handler := &osvBatchTestHandler{}
	server := httptest.NewServer(handler)
	defer server.Close()
	originalEndpoint := Endpoint
	Endpoint = server.URL + "/v1/query"
	defer func() { Endpoint = originalEndpoint }()

	queries := make([]Query, maxOSVBatchQueries+1)
	for index := range queries {
		queries[index].Ecosystem = "npm"
		queries[index].Name = fmt.Sprintf("package-%d", index)
		queries[index].Version = "1.0.0"
	}
	results, err := CheckBatch(queries)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if handler.requests != 2 || len(results) != len(queries) {
		t.Fatalf("expected two requests and %d results, got %d and %d", len(queries), handler.requests, len(results))
	}
	if handler.batchSizes[0] != maxOSVBatchQueries || handler.batchSizes[1] != 1 {
		t.Fatalf("unexpected batch sizes: %v", handler.batchSizes)
	}
}

func TestCheckBatchFallsBackForCustomEndpoint(t *testing.T) {
	handler := &osvSingleTestHandler{}
	server := httptest.NewServer(handler)
	defer server.Close()
	originalEndpoint := Endpoint
	Endpoint = server.URL
	defer func() { Endpoint = originalEndpoint }()

	first := Query{Ecosystem: "npm", Name: "react", Version: "18.0.0"}
	second := Query{Ecosystem: "npm", Name: "lodash", Version: "4.17.21"}
	results, err := CheckBatch([]Query{first, second})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if handler.requests != 2 || len(results) != 2 {
		t.Fatalf("expected two requests and results, got %d and %d", handler.requests, len(results))
	}
}

func TestCheckStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	defer srv.Close()

	origEndpoint := Endpoint
	Endpoint = srv.URL
	defer func() { Endpoint = origEndpoint }()

	_, err := Check("npm", "react", "18.0.0")
	if err == nil {
		t.Error("expected error for non-2xx response")
	}
}
