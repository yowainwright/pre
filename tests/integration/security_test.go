//go:build integration

package integration

import (
	"testing"

	"github.com/yowainwright/pre/internal/security"
)

func TestOSVAPIReachable(t *testing.T) {
	_, err := security.Check("npm", "react", "18.0.0")
	if err != nil {
		t.Fatalf("OSV API unreachable: %v", err)
	}
}

func TestOSVKnownVulnerablePackage(t *testing.T) {
	vulns, err := security.Check("npm", "lodash", "4.17.4")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(vulns) == 0 {
		t.Error("expected vulnerabilities for lodash@4.17.4")
	}
}

func TestOSVGoEcosystem(t *testing.T) {
	_, err := security.Check("Go", "golang.org/x/net", "0.0.0-20210226172049-4dc4b3a7a6e")
	if err != nil {
		t.Fatalf("unexpected error for Go ecosystem: %v", err)
	}
}

func TestOSVPyPIEcosystem(t *testing.T) {
	_, err := security.Check("PyPI", "requests", "2.25.0")
	if err != nil {
		t.Fatalf("unexpected error for PyPI ecosystem: %v", err)
	}
}
