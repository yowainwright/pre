package security

import (
	"math"
	"testing"
)

func TestCVSSScore(t *testing.T) {
	cases := []struct {
		vector string
		want   float64
	}{
		{"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H", 9.8},
		{"CVSS:3.1/AV:N/AC:L/PR:L/UI:N/S:U/C:H/I:H/A:H", 8.8},
		{"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:N/I:N/A:H", 7.5},
		{"CVSS:3.1/AV:L/AC:L/PR:L/UI:N/S:U/C:H/I:N/A:N", 5.5},
		{"CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:C/C:H/I:H/A:H", 10.0},
	}
	for _, c := range cases {
		got := cvssScore(c.vector)
		if math.Abs(got-c.want) > 0.05 {
			t.Errorf("cvssScore(%q) = %.1f, want %.1f", c.vector, got, c.want)
		}
	}
}

func TestCVSSScoreInvalid(t *testing.T) {
	if cvssScore("") != -1 {
		t.Error("expected -1 for empty vector")
	}
	if cvssScore("notavector") != -1 {
		t.Error("expected -1 for garbage input")
	}
}

func TestCVSSScoreZeroImpact(t *testing.T) {
	score := cvssScore("CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:N/I:N/A:N")
	if score != 0 {
		t.Errorf("expected 0 for all-None CIA, got %.1f", score)
	}
}

func TestCVSSScoreInvalidPR(t *testing.T) {
	score := cvssScore("CVSS:3.1/AV:N/AC:L/PR:X/UI:N/S:U/C:H/I:H/A:H")
	if score != -1 {
		t.Errorf("expected -1 for invalid PR metric, got %.1f", score)
	}
}

func TestSeverityFromScore(t *testing.T) {
	cases := []struct {
		score float64
		want  string
	}{
		{9.8, "CRITICAL"},
		{9.0, "CRITICAL"},
		{8.8, "HIGH"},
		{7.0, "HIGH"},
		{6.9, "MEDIUM"},
		{4.0, "MEDIUM"},
		{3.9, "LOW"},
		{0.1, "LOW"},
		{0.0, ""},
	}
	for _, c := range cases {
		got := severityFromScore(c.score)
		if got != c.want {
			t.Errorf("severityFromScore(%.1f) = %q, want %q", c.score, got, c.want)
		}
	}
}

func TestSeverityFromVector(t *testing.T) {
	rating, score := severityFromVector("CVSS:3.1/AV:N/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H")
	if rating != "CRITICAL" {
		t.Errorf("expected CRITICAL, got %q", rating)
	}
	if math.Abs(score-9.8) > 0.05 {
		t.Errorf("expected score ~9.8, got %.1f", score)
	}
}

func TestSeverityFromVectorInvalid(t *testing.T) {
	rating, score := severityFromVector("notavector")
	if rating != "" || score != 0 {
		t.Errorf("expected empty rating and 0 score for invalid vector, got %q %.1f", rating, score)
	}
}

func TestCVSSScoreInvalidAV(t *testing.T) {
	score := cvssScore("CVSS:3.1/AV:X/AC:L/PR:N/UI:N/S:U/C:H/I:H/A:H")
	if score != -1 {
		t.Errorf("expected -1 for invalid AV metric, got %.1f", score)
	}
}
