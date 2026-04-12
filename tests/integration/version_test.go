//go:build integration

package integration

import (
	"os/exec"
	"testing"

	"github.com/yowainwright/pre/internal/manager"
)

func TestGoProxyVersionResolution(t *testing.T) {
	mgr := &manager.Manager{Name: "go", Ecosystem: "Go", InstallCmds: []string{"get", "install"}}
	version, err := manager.ResolveVersion(mgr, "golang.org/x/text")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version == "" {
		t.Error("expected non-empty version for golang.org/x/text")
	}
}

func TestPyPIVersionResolution(t *testing.T) {
	mgr := &manager.Manager{Name: "pip", Ecosystem: "PyPI", InstallCmds: []string{"install"}}
	version, err := manager.ResolveVersion(mgr, "requests")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version == "" {
		t.Error("expected non-empty version for requests")
	}
}

func TestNpmVersionResolution(t *testing.T) {
	if _, err := exec.LookPath("npm"); err != nil {
		t.Skip("npm not available")
	}
	mgr := &manager.Manager{Name: "npm", Ecosystem: "npm", InstallCmds: []string{"install"}}
	version, err := manager.ResolveVersion(mgr, "react")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if version == "" {
		t.Error("expected non-empty version for react")
	}
}
