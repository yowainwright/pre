package manager

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBrewVersionSuccess(t *testing.T) {
	orig := runCmd
	runCmd = func(name string, args ...string) ([]byte, error) {
		return []byte(`{"formulae":[{"versions":{"stable":"1.25.0"}}]}`), nil
	}
	defer func() { runCmd = orig }()

	ver, err := brewVersion("nginx")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ver != "1.25.0" {
		t.Errorf("expected '1.25.0', got %q", ver)
	}
}

func TestBrewVersionExecError(t *testing.T) {
	orig := runCmd
	runCmd = func(name string, args ...string) ([]byte, error) {
		return nil, errors.New("command not found")
	}
	defer func() { runCmd = orig }()

	_, err := brewVersion("nginx")
	if err == nil {
		t.Error("expected error")
	}
}

func TestBrewVersionInvalidJSON(t *testing.T) {
	orig := runCmd
	runCmd = func(name string, args ...string) ([]byte, error) {
		return []byte("not json"), nil
	}
	defer func() { runCmd = orig }()

	_, err := brewVersion("nginx")
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestBrewVersionEmptyFormulae(t *testing.T) {
	orig := runCmd
	runCmd = func(name string, args ...string) ([]byte, error) {
		return []byte(`{"formulae":[]}`), nil
	}
	defer func() { runCmd = orig }()

	_, err := brewVersion("nginx")
	if err == nil {
		t.Error("expected error for empty formulae")
	}
}

func TestNpmVersionSuccess(t *testing.T) {
	orig := runCmd
	runCmd = func(name string, args ...string) ([]byte, error) {
		return []byte("18.0.0\n"), nil
	}
	defer func() { runCmd = orig }()

	ver, err := npmVersion("react")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ver != "18.0.0" {
		t.Errorf("expected '18.0.0', got %q", ver)
	}
}

func TestNpmVersionError(t *testing.T) {
	orig := runCmd
	runCmd = func(name string, args ...string) ([]byte, error) {
		return nil, errors.New("npm not found")
	}
	defer func() { runCmd = orig }()

	_, err := npmVersion("react")
	if err == nil {
		t.Error("expected error")
	}
}

func TestNpmVersionEmpty(t *testing.T) {
	orig := runCmd
	runCmd = func(name string, args ...string) ([]byte, error) {
		return []byte("\n"), nil
	}
	defer func() { runCmd = orig }()

	_, err := npmVersion("react")
	if err == nil {
		t.Error("expected error for empty version")
	}
}

func TestGoVersionSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"Version":"v1.2.3"}`)
	}))
	defer srv.Close()

	origBase := goProxyBase
	goProxyBase = srv.URL
	defer func() { goProxyBase = origBase }()

	ver, err := goVersion("github.com/foo/bar")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ver != "v1.2.3" {
		t.Errorf("expected 'v1.2.3', got %q", ver)
	}
}

func TestGoVersionHTTPError(t *testing.T) {
	origBase := goProxyBase
	goProxyBase = "http://invalid.local.invalid"
	defer func() { goProxyBase = origBase }()

	_, err := goVersion("github.com/foo/bar")
	if err == nil {
		t.Error("expected error for invalid URL")
	}
}

func TestGoVersionInvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "not json")
	}))
	defer srv.Close()

	origBase := goProxyBase
	goProxyBase = srv.URL
	defer func() { goProxyBase = origBase }()

	_, err := goVersion("github.com/foo/bar")
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestGoVersionStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad gateway", http.StatusBadGateway)
	}))
	defer srv.Close()

	origBase := goProxyBase
	goProxyBase = srv.URL
	defer func() { goProxyBase = origBase }()

	_, err := goVersion("github.com/foo/bar")
	if err == nil {
		t.Error("expected error for non-2xx status")
	}
}

func TestGoVersionEmptyVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{}`)
	}))
	defer srv.Close()

	origBase := goProxyBase
	goProxyBase = srv.URL
	defer func() { goProxyBase = origBase }()

	_, err := goVersion("github.com/foo/bar")
	if err == nil {
		t.Error("expected error for empty version")
	}
}

func TestPypiVersionSuccess(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"info":{"version":"2.28.0"}}`)
	}))
	defer srv.Close()

	origBase := pypiBase
	pypiBase = srv.URL
	defer func() { pypiBase = origBase }()

	ver, err := pypiVersion("requests")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ver != "2.28.0" {
		t.Errorf("expected '2.28.0', got %q", ver)
	}
}

func TestPypiVersionHTTPError(t *testing.T) {
	origBase := pypiBase
	pypiBase = "http://invalid.local.invalid"
	defer func() { pypiBase = origBase }()

	_, err := pypiVersion("requests")
	if err == nil {
		t.Error("expected error for invalid URL")
	}
}

func TestPypiVersionInvalidJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, "not json")
	}))
	defer srv.Close()

	origBase := pypiBase
	pypiBase = srv.URL
	defer func() { pypiBase = origBase }()

	_, err := pypiVersion("requests")
	if err == nil {
		t.Error("expected error for invalid JSON")
	}
}

func TestPypiVersionStatusError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "too many requests", http.StatusTooManyRequests)
	}))
	defer srv.Close()

	origBase := pypiBase
	pypiBase = srv.URL
	defer func() { pypiBase = origBase }()

	_, err := pypiVersion("requests")
	if err == nil {
		t.Error("expected error for non-2xx status")
	}
}

func TestPypiVersionEmptyVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"info":{}}`)
	}))
	defer srv.Close()

	origBase := pypiBase
	pypiBase = srv.URL
	defer func() { pypiBase = origBase }()

	_, err := pypiVersion("requests")
	if err == nil {
		t.Error("expected error for empty version")
	}
}

func TestResolveVersionHomebrew(t *testing.T) {
	orig := runCmd
	runCmd = func(name string, args ...string) ([]byte, error) {
		return []byte(`{"formulae":[{"versions":{"stable":"1.25.0"}}]}`), nil
	}
	defer func() { runCmd = orig }()

	mgr := &Manager{Name: "brew", Ecosystem: "Homebrew", InstallCmds: []string{"install"}}
	ver, err := ResolveVersion(mgr, "nginx")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ver != "1.25.0" {
		t.Errorf("expected '1.25.0', got %q", ver)
	}
}

func TestResolveVersionNpm(t *testing.T) {
	orig := runCmd
	runCmd = func(name string, args ...string) ([]byte, error) {
		return []byte("18.0.0\n"), nil
	}
	defer func() { runCmd = orig }()

	mgr := &Manager{Name: "npm", Ecosystem: "npm", InstallCmds: []string{"install"}}
	ver, err := ResolveVersion(mgr, "react")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ver != "18.0.0" {
		t.Errorf("expected '18.0.0', got %q", ver)
	}
}

func TestResolveVersionGo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"Version":"v1.2.3"}`)
	}))
	defer srv.Close()

	origBase := goProxyBase
	goProxyBase = srv.URL
	defer func() { goProxyBase = origBase }()

	mgr := &Manager{Name: "go", Ecosystem: "Go", InstallCmds: []string{"get"}}
	ver, err := ResolveVersion(mgr, "github.com/foo/bar")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ver != "v1.2.3" {
		t.Errorf("expected 'v1.2.3', got %q", ver)
	}
}

func TestResolveVersionPyPI(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintln(w, `{"info":{"version":"2.28.0"}}`)
	}))
	defer srv.Close()

	origBase := pypiBase
	pypiBase = srv.URL
	defer func() { pypiBase = origBase }()

	mgr := &Manager{Name: "pip", Ecosystem: "PyPI", InstallCmds: []string{"install"}}
	ver, err := ResolveVersion(mgr, "requests")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ver != "2.28.0" {
		t.Errorf("expected '2.28.0', got %q", ver)
	}
}

func TestRunCmdDefault(t *testing.T) {
	out, err := runCmd("echo", "hello")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(out) == 0 {
		t.Error("expected non-empty output from echo")
	}
}

func TestResolveVersionUnknownEcosystem(t *testing.T) {
	mgr := &Manager{Name: "custom", Ecosystem: "Custom", InstallCmds: []string{"install"}}
	ver, err := ResolveVersion(mgr, "somepkg")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ver != "" {
		t.Errorf("expected empty version, got %q", ver)
	}
}
