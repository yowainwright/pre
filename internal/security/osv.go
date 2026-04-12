package security

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

var (
	Endpoint   = "https://api.osv.dev/v1/query"
	httpClient = &http.Client{Timeout: 10 * time.Second}
)

type Vulnerability struct {
	ID       string  `json:"id"`
	Summary  string  `json:"summary"`
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
	type osvVuln struct {
		ID       string `json:"id"`
		Summary  string `json:"summary"`
		Severity []struct {
			Type  string `json:"type"`
			Score string `json:"score"`
		} `json:"severity"`
		DatabaseSpecific struct {
			Severity string `json:"severity"`
		} `json:"database_specific"`
	}
	type response struct {
		Vulns []osvVuln `json:"vulns"`
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

func extractSeverity(dbSeverity string, cvssEntries []struct {
	Type  string `json:"type"`
	Score string `json:"score"`
}) (string, float64) {
	if dbSeverity != "" {
		return dbSeverity, 0
	}
	for _, s := range cvssEntries {
		if rating, score := severityFromVector(s.Score); rating != "" {
			return rating, score
		}
	}
	return "", 0
}
