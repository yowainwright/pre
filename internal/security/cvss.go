package security

import (
	"math"
	"strings"
)

func cvssScore(vector string) float64 {
	parts := strings.Split(vector, "/")
	if len(parts) < 2 {
		return -1
	}
	m := make(map[string]string, len(parts))
	for _, p := range parts[1:] {
		k, v, ok := strings.Cut(p, ":")
		if ok {
			m[k] = v
		}
	}

	avMap := map[string]float64{"N": 0.85, "A": 0.62, "L": 0.55, "P": 0.20}
	acMap := map[string]float64{"L": 0.77, "H": 0.44}
	uiMap := map[string]float64{"N": 0.85, "R": 0.62}
	ciaMap := map[string]float64{"N": 0.0, "L": 0.22, "H": 0.56}
	prUMap := map[string]float64{"N": 0.85, "L": 0.62, "H": 0.27}
	prCMap := map[string]float64{"N": 0.85, "L": 0.68, "H": 0.50}

	av, avOK := avMap[m["AV"]]
	ac, acOK := acMap[m["AC"]]
	ui, uiOK := uiMap[m["UI"]]
	c, cOK := ciaMap[m["C"]]
	i, iOK := ciaMap[m["I"]]
	a, aOK := ciaMap[m["A"]]
	if !avOK || !acOK || !uiOK || !cOK || !iOK || !aOK {
		return -1
	}

	scopeChanged := m["S"] == "C"
	var pr float64
	var prOK bool
	if scopeChanged {
		pr, prOK = prCMap[m["PR"]]
	} else {
		pr, prOK = prUMap[m["PR"]]
	}
	if !prOK {
		return -1
	}

	iss := 1 - (1-c)*(1-i)*(1-a)
	var impact float64
	if scopeChanged {
		impact = 7.52*(iss-0.029) - 3.25*math.Pow(iss-0.02, 15)
	} else {
		impact = 6.42 * iss
	}
	if impact <= 0 {
		return 0
	}

	exploitability := 8.22 * av * ac * pr * ui
	var raw float64
	if scopeChanged {
		raw = math.Min(1.08*(impact+exploitability), 10)
	} else {
		raw = math.Min(impact+exploitability, 10)
	}
	return math.Ceil(raw*10) / 10
}

func severityFromScore(score float64) string {
	switch {
	case score >= 9.0:
		return "CRITICAL"
	case score >= 7.0:
		return "HIGH"
	case score >= 4.0:
		return "MEDIUM"
	case score > 0:
		return "LOW"
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
