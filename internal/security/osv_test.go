package security

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
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
}

func (handler *osvBatchTestHandler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	handler.requests++
	var payload osvBatchRequest
	if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
		http.Error(writer, err.Error(), http.StatusBadRequest)
		return
	}
	handler.batchSizes = append(handler.batchSizes, len(payload.Queries))
	results := make([]osvResponse, len(payload.Queries))
	if handler.includeVulnerability && len(results) > 0 {
		vulnerability := osvVulnerability{ID: "CVE-2026-1234", Summary: "batch test"}
		results[0].Vulns = []osvVulnerability{vulnerability}
	}
	_ = json.NewEncoder(writer).Encode(osvBatchResponse{Results: results})
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
	if handler.requests != 1 || len(results) != 2 {
		t.Fatalf("expected one request and two results, got %d and %d", handler.requests, len(results))
	}
	if len(results[0]) != 1 || results[0][0].ID != "CVE-2026-1234" {
		t.Fatalf("unexpected first result: %+v", results[0])
	}
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
