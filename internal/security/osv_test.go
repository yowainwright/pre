package security

import (
	"fmt"
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

func TestCheckHTTPError(t *testing.T) {
	origEndpoint := Endpoint
	Endpoint = "http://invalid.local.invalid"
	defer func() { Endpoint = origEndpoint }()

	_, err := Check("npm", "react", "18.0.0")
	if err == nil {
		t.Error("expected error for invalid Endpoint")
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
