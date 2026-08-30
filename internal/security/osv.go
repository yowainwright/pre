package security

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

var (
	Endpoint   = "https://api.osv.dev/v1/query"
	httpClient = &http.Client{Timeout: 10 * time.Second}
)

const (
	maxOSVResponseBytes      = 4 << 20
	maxOSVBatchResponseBytes = 64 << 20
	maxOSVBatchQueries       = 1000
	SeverityCritical         = "CRITICAL"
	SeverityHigh             = "HIGH"
	SeverityMedium           = "MEDIUM"
	SeverityLow              = "LOW"
	cvssTypeV3               = "CVSS_V3"
	cvssTypeV4               = "CVSS_V4"
)

type Query struct {
	Ecosystem string
	Name      string
	Version   string
}

type osvPackage struct {
	Name      string `json:"name"`
	Ecosystem string `json:"ecosystem"`
}

type osvQuery struct {
	Version string     `json:"version,omitempty"`
	Package osvPackage `json:"package"`
}

type osvResponse struct {
	Vulns []osvVulnerability `json:"vulns"`
}

type osvBatchRequest struct {
	Queries []osvQuery `json:"queries"`
}

type osvBatchResponse struct {
	Results []osvResponse `json:"results"`
}

type severityEntry struct {
	Type  string `json:"type"`
	Score string `json:"score"`
}

type osvVulnerability struct {
	ID               string          `json:"id"`
	Summary          string          `json:"summary"`
	Severity         []severityEntry `json:"severity"`
	DatabaseSpecific struct {
		Severity string `json:"severity"`
	} `json:"database_specific"`
}

type Vulnerability struct {
	ID       string `json:"id"`
	Summary  string `json:"summary"`
	Severity string
	Score    float64
}

func Check(ecosystem, name, version string) ([]Vulnerability, error) {
	query := Query{Ecosystem: ecosystem, Name: name, Version: version}
	request := osvQueryFrom(query)
	var response osvResponse
	if err := postOSV(Endpoint, request, &response, maxOSVResponseBytes); err != nil {
		return nil, err
	}
	return vulnerabilitiesFrom(response.Vulns), nil
}

func CheckBatch(queries []Query) ([][]Vulnerability, error) {
	if len(queries) == 0 {
		return nil, nil
	}
	endpoint, ok := osvBatchEndpoint(Endpoint)
	if !ok {
		return checkIndividually(queries)
	}
	results := make([][]Vulnerability, 0, len(queries))
	for start := 0; start < len(queries); start += maxOSVBatchQueries {
		end := min(start+maxOSVBatchQueries, len(queries))
		batch, err := checkBatchChunk(endpoint, queries[start:end])
		if err != nil {
			return nil, err
		}
		results = append(results, batch...)
	}
	return results, nil
}

func checkBatchChunk(endpoint string, queries []Query) ([][]Vulnerability, error) {
	requestQueries := make([]osvQuery, len(queries))
	for index, query := range queries {
		requestQueries[index] = osvQueryFrom(query)
	}
	request := osvBatchRequest{Queries: requestQueries}
	var response osvBatchResponse
	if err := postOSV(endpoint, request, &response, maxOSVBatchResponseBytes); err != nil {
		return nil, err
	}
	if len(response.Results) != len(queries) {
		return nil, fmt.Errorf("decode: expected %d batch results, got %d", len(queries), len(response.Results))
	}
	results := make([][]Vulnerability, len(response.Results))
	for index, result := range response.Results {
		results[index] = vulnerabilitiesFrom(result.Vulns)
	}
	return results, nil
}

func checkIndividually(queries []Query) ([][]Vulnerability, error) {
	results := make([][]Vulnerability, len(queries))
	for index, query := range queries {
		vulnerabilities, err := Check(query.Ecosystem, query.Name, query.Version)
		if err != nil {
			return nil, err
		}
		results[index] = vulnerabilities
	}
	return results, nil
}

func osvQueryFrom(query Query) osvQuery {
	packageInfo := osvPackage{Name: query.Name, Ecosystem: query.Ecosystem}
	return osvQuery{Version: query.Version, Package: packageInfo}
}

func osvBatchEndpoint(endpoint string) (string, bool) {
	parsed, err := url.Parse(endpoint)
	if err != nil || !strings.HasSuffix(parsed.Path, "/query") {
		return "", false
	}
	parsed.Path = strings.TrimSuffix(parsed.Path, "/query") + "/querybatch"
	return parsed.String(), true
}

func postOSV(endpoint string, payload, target any, limit int64) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	request, cancel, err := newOSVRequest(endpoint, body)
	if err != nil {
		return err
	}
	defer cancel()
	response, err := httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("request: %w", err)
	}
	defer response.Body.Close()
	return decodeOSVHTTPResponse(response, target, limit)
}

func newOSVRequest(endpoint string, body []byte) (*http.Request, context.CancelFunc, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		cancel()
		return nil, nil, fmt.Errorf("request: %w", err)
	}
	request.Header.Set("Content-Type", "application/json")
	return request, cancel, nil
}

func decodeOSVHTTPResponse(response *http.Response, target any, limit int64) error {
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 512))
		return fmt.Errorf("request: status %d: %s", response.StatusCode, strings.TrimSpace(string(body)))
	}
	if err := decodeOSVResponse(response.Body, target, limit); err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	return nil
}

func vulnerabilitiesFrom(source []osvVulnerability) []Vulnerability {
	vulnerabilities := make([]Vulnerability, len(source))
	for index, vulnerability := range source {
		rating, score := extractSeverity(vulnerability.DatabaseSpecific.Severity, vulnerability.Severity)
		vulnerabilities[index] = Vulnerability{
			ID: vulnerability.ID, Summary: vulnerability.Summary, Severity: rating, Score: score,
		}
	}
	return vulnerabilities
}

func decodeOSVResponse(body io.Reader, target any, limit int64) error {
	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return err
	}
	if int64(len(data)) > limit {
		return fmt.Errorf("response exceeds %d bytes", limit)
	}
	return json.Unmarshal(data, target)
}

func extractSeverity(dbSeverity string, cvssEntries []severityEntry) (string, float64) {
	if dbSeverity != "" {
		if rating := normalizeSeverity(dbSeverity); rating != "" {
			return rating, 0
		}
	}
	for _, s := range cvssEntries {
		isSupportedCVSS := s.Type == "" || s.Type == cvssTypeV3
		isUnsupportedCVSS := strings.HasPrefix(s.Type, "CVSS_") && !isSupportedCVSS
		if s.Type == cvssTypeV4 || isUnsupportedCVSS {
			return SeverityCritical, 0
		}
		if rating, score := severityFromVector(s.Score); rating != "" {
			return rating, score
		}
	}
	return "", 0
}

func normalizeSeverity(severity string) string {
	switch strings.ToUpper(strings.TrimSpace(severity)) {
	case SeverityCritical:
		return SeverityCritical
	case SeverityHigh:
		return SeverityHigh
	case "MEDIUM", "MODERATE":
		return SeverityMedium
	case SeverityLow:
		return SeverityLow
	default:
		return ""
	}
}
