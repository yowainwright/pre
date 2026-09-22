package security

import (
	"math"
	"strings"
)

var (
	cvssAttackVectorWeights = map[string]float64{"N": 0.85, "A": 0.62, "L": 0.55, "P": 0.20}
	cvssComplexityWeights   = map[string]float64{"L": 0.77, "H": 0.44}
	cvssInteractionWeights  = map[string]float64{"N": 0.85, "R": 0.62}
	cvssImpactWeights       = map[string]float64{"N": 0.0, "L": 0.22, "H": 0.56}
	cvssPrivilegeWeights    = map[string]float64{"N": 0.85, "L": 0.62, "H": 0.27}
	cvssChangedWeights      = map[string]float64{"N": 0.85, "L": 0.68, "H": 0.50}
)

func cvssScore(vector string) float64 {
	parts := strings.Split(vector, "/")
	if len(parts) < 2 {
		return -1
	}
	metrics := cvssMetrics(parts[1:])
	exploitability, ok := cvssExploitability(metrics)
	if !ok {
		return -1
	}
	impact, ok := cvssImpact(metrics)
	if !ok {
		return -1
	}
	if impact <= 0 {
		return 0
	}
	return cvssBaseScore(impact, exploitability, metrics["S"] == "C")
}

func cvssMetrics(parts []string) map[string]string {
	metrics := make(map[string]string, len(parts))
	for _, part := range parts {
		key, value, ok := strings.Cut(part, ":")
		if ok {
			metrics[key] = value
		}
	}
	return metrics
}

func cvssExploitability(metrics map[string]string) (float64, bool) {
	av, avOK := cvssAttackVectorWeights[metrics["AV"]]
	ac, acOK := cvssComplexityWeights[metrics["AC"]]
	ui, uiOK := cvssInteractionWeights[metrics["UI"]]
	weights := cvssPrivilegeWeights
	if metrics["S"] == "C" {
		weights = cvssChangedWeights
	}
	pr, prOK := weights[metrics["PR"]]
	valid := avOK && acOK && uiOK && prOK
	score := 8.22 * av * ac * pr * ui
	return score, valid
}

func cvssImpact(metrics map[string]string) (float64, bool) {
	c, cOK := cvssImpactWeights[metrics["C"]]
	i, iOK := cvssImpactWeights[metrics["I"]]
	a, aOK := cvssImpactWeights[metrics["A"]]
	valid := cOK && iOK && aOK
	iss := 1 - (1-c)*(1-i)*(1-a)
	impact := 6.42 * iss
	if metrics["S"] == "C" {
		impact = 7.52*(iss-0.029) - 3.25*math.Pow(iss-0.02, 15)
	}
	return impact, valid
}

func cvssBaseScore(impact, exploitability float64, scopeChanged bool) float64 {
	raw := math.Min(impact+exploitability, 10)
	if scopeChanged {
		raw = math.Min(1.08*(impact+exploitability), 10)
	}
	rounded := math.Ceil(raw*10) / 10
	return rounded
}

func severityFromScore(score float64) string {
	switch {
	case score >= 9.0:
		return SeverityCritical
	case score >= 7.0:
		return SeverityHigh
	case score >= 4.0:
		return SeverityMedium
	case score > 0:
		return SeverityLow
	default:
		return ""
	}
}

func severityFromVector(vector string) (string, float64) {
	score := cvssScore(vector)
	if score < 0 {
		return "", 0
	}
	return severityFromScore(score), score
}
