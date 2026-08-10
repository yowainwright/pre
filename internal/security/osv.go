package security

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

var (
	Endpoint   = "https://api.osv.dev/v1/query"
	httpClient = &http.Client{Timeout: 10 * time.Second}
)

const (
	SeverityCritical = "CRITICAL"
	SeverityHigh     = "HIGH"
	SeverityMedium   = "MEDIUM"
	SeverityLow      = "LOW"
	cvssTypeV3       = "CVSS_V3"
	cvssTypeV4       = "CVSS_V4"
)

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
	type pkg struct {
		Name      string `json:"name"`
		Ecosystem string `json:"ecosystem"`
	}
	type query struct {
		Version string `json:"version,omitempty"`
		Package pkg    `json:"package"`
	}
	type response struct {
		Vulns []osvVulnerability `json:"vulns"`
	}

	body, _ := json.Marshal(query{
		Version: version,
		Package: pkg{Name: name, Ecosystem: ecosystem},
	})

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "POST", Endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("request: status %d: %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	var result response
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}

	vulns := make([]Vulnerability, len(result.Vulns))
	for i, v := range result.Vulns {
		rating, score := extractSeverity(v.DatabaseSpecific.Severity, v.Severity)
		vulns[i] = Vulnerability{ID: v.ID, Summary: v.Summary, Severity: rating, Score: score}
	}
	return vulns, nil
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
