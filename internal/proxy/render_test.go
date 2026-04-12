package proxy

import (
	"errors"
	"strings"
	"testing"

	"github.com/yowainwright/pre/internal/security"
)

func TestRenderQuiet(t *testing.T) {
	out := renderQuiet(8)
	if !strings.Contains(out, "8") || !strings.Contains(out, "clean") {
		t.Errorf("unexpected renderQuiet output: %q", out)
	}
}

func TestRenderSystemLineEmpty(t *testing.T) {
	out := renderSystemLine(SystemStats{})
	if !strings.Contains(out, "pre setup") {
		t.Errorf("expected setup hint for empty stats, got %q", out)
	}
}

func TestRenderSystemLineWithStats(t *testing.T) {
	out := renderSystemLine(SystemStats{Crit: 2, Warn: 3, Total: 10})
	if !strings.Contains(out, "2") || !strings.Contains(out, "3") || !strings.Contains(out, "10") {
		t.Errorf("expected crit/warn/total in output, got %q", out)
	}
}

func TestNodeStatusClean(t *testing.T) {
	out := nodeStatus(scanResult{})
	if !strings.Contains(out, "clean") {
		t.Errorf("expected 'clean' for clean result, got %q", out)
	}
}

func TestNodeStatusCached(t *testing.T) {
	out := nodeStatus(scanResult{cached: true})
	if !strings.Contains(out, "cached") {
		t.Errorf("expected 'cached' for cached result, got %q", out)
	}
}

func TestNodeStatusError(t *testing.T) {
	out := nodeStatus(scanResult{err: errors.New("network timeout")})
	if !strings.Contains(out, "network timeout") {
		t.Errorf("expected error message in output, got %q", out)
	}
}

func TestNodeStatusVulns(t *testing.T) {
	out := nodeStatus(scanResult{vulns: []security.Vulnerability{{ID: "CVE-2021-1234"}}})
	if !strings.Contains(out, "vulnerabilit") {
		t.Errorf("expected 'vulnerabilit' in output, got %q", out)
	}
}

func TestRenderCriticalDetail(t *testing.T) {
	results := []scanResult{
		{
			label: "lodash@4.17.11",
			vulns: []security.Vulnerability{
				{ID: "CVE-2021-23337", Severity: "CRITICAL", Score: 9.8, Summary: "Prototype Pollution"},
				{ID: "CVE-2021-0001", Severity: "MEDIUM", Summary: "Minor issue"},
			},
		},
	}
	out := renderCriticalDetail(results)
	if !strings.Contains(out, "CVE-2021-23337") {
		t.Errorf("expected CVE ID in detail box, got %q", out)
	}
	if strings.Contains(out, "CVE-2021-0001") {
		t.Errorf("MEDIUM vuln should not appear in critical detail, got %q", out)
	}
	if !strings.Contains(out, "9.8") {
		t.Errorf("expected score in detail box, got %q", out)
	}
}

func TestRenderCriticalDetailNoScore(t *testing.T) {
	results := []scanResult{
		{
			label: "pkg@1.0.0",
			vulns: []security.Vulnerability{
				{ID: "GHSA-xxxx", Severity: "HIGH"},
			},
		},
	}
	out := renderCriticalDetail(results)
	if !strings.Contains(out, "GHSA-xxxx") {
		t.Errorf("expected vuln ID in output, got %q", out)
	}
}

func TestRenderSummaryCrit(t *testing.T) {
	results := []scanResult{
		{vulns: []security.Vulnerability{{ID: "CVE-1234", Severity: "CRITICAL"}}},
	}
	out := renderSummary(results)
	if !strings.Contains(out, "1 crit") {
		t.Errorf("expected '1 crit' in output, got %q", out)
	}
}

func TestRenderSummaryCached(t *testing.T) {
	results := []scanResult{
		{cached: true},
		{vulns: []security.Vulnerability{{ID: "CVE-1234", Severity: "MEDIUM"}}},
	}
	out := renderSummary(results)
	if !strings.Contains(out, "1 cached") {
		t.Errorf("expected '1 cached' in output, got %q", out)
	}
}

func TestRenderSummaryWarn(t *testing.T) {
	results := []scanResult{
		{vulns: []security.Vulnerability{{ID: "CVE-1234", Severity: "MEDIUM"}}},
	}
	out := renderSummary(results)
	if !strings.Contains(out, "1 warn") {
		t.Errorf("expected '1 warn' in output, got %q", out)
	}
}

func TestRenderSummaryUps(t *testing.T) {
	results := []scanResult{
		{updated: true},
	}
	out := renderSummary(results)
	if !strings.Contains(out, "1 ups") {
		t.Errorf("expected '1 ups' in output, got %q", out)
	}
}

func TestNodeChildrenWithScore(t *testing.T) {
	r := scanResult{
		vulns: []security.Vulnerability{
			{ID: "CVE-2021-1234", Summary: "test", Score: 9.8},
		},
	}
	children := nodeChildren(r)
	if len(children) != 1 {
		t.Fatalf("expected 1 child, got %d", len(children))
	}
	if !strings.Contains(children[0], "9.8") {
		t.Errorf("expected score in child, got %q", children[0])
	}
}
