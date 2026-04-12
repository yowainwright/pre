package security

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
)

var Endpoint = "https://api.osv.dev/v1/query"

type Vulnerability struct {
	ID       string `json:"id"`
	Summary  string `json:"summary"`
	Severity string
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
		ID               string `json:"id"`
		Summary          string `json:"summary"`
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

	resp, err := http.Post(Endpoint, "application/json", bytes.NewReader(body))
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
		vulns[i] = Vulnerability{ID: v.ID, Summary: v.Summary, Severity: v.DatabaseSpecific.Severity}
	}
	return vulns, nil
}
