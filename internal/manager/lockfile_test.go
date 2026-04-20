package manager

import (
	"os"
	"testing"
)

// npm: package-lock.json

func TestReadPackageLockJSON(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/package-lock.json", []byte(`{
		"lockfileVersion": 3,
		"packages": {
			"": {"version": "1.0.0"},
			"node_modules/lodash": {"version": "4.17.21"},
			"node_modules/react": {"version": "18.2.0"},
			"node_modules/react-dom": {"version": "18.2.0"}
		}
	}`), 0644)

	pkgs := readPackageLockJSON(dir)
	if len(pkgs) != 3 {
		t.Fatalf("expected 3, got %d: %v", len(pkgs), pkgs)
	}
	m := toSet(pkgs)
	if !m["lodash@4.17.21"] || !m["react@18.2.0"] || !m["react-dom@18.2.0"] {
		t.Errorf("missing expected packages: %v", pkgs)
	}
}

func TestReadPackageLockJSONSkipsRoot(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/package-lock.json", []byte(`{
		"packages": {
			"": {"version": "1.0.0"},
			"node_modules/express": {"version": "4.18.0"}
		}
	}`), 0644)

	pkgs := readPackageLockJSON(dir)
	if len(pkgs) != 1 || pkgs[0] != "express@4.18.0" {
		t.Errorf("expected [express@4.18.0], got %v", pkgs)
	}
}

func TestReadPackageLockJSONBadJSON(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/package-lock.json", []byte("not json"), 0644)
	if readPackageLockJSON(dir) != nil {
		t.Error("expected nil for bad JSON")
	}
}

func TestReadPackageLockJSONMissing(t *testing.T) {
	if readPackageLockJSON(t.TempDir()) != nil {
		t.Error("expected nil for missing file")
	}
}

// npm: bun.lock

func TestReadBunLock(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/bun.lock", []byte(`# bun lockfile v1 (https://bun.sh)

{
  "lockfileVersion": 0,
  "packages": {
    "lodash@4.17.21": ["lodash@4.17.21", {}],
    "react@18.2.0": ["react@18.2.0", {}],
    "@scope/pkg@1.0.0": ["@scope/pkg@1.0.0", {}]
  }
}
`), 0644)

	pkgs := readBunLock(dir)
	if len(pkgs) != 3 {
		t.Fatalf("expected 3, got %d: %v", len(pkgs), pkgs)
	}
	m := toSet(pkgs)
	if !m["lodash@4.17.21"] || !m["react@18.2.0"] || !m["@scope/pkg@1.0.0"] {
		t.Errorf("unexpected packages: %v", pkgs)
	}
}

func TestReadBunLockBadJSON(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/bun.lock", []byte("not json"), 0644)
	if readBunLock(dir) != nil {
		t.Error("expected nil for bad JSON")
	}
}

func TestReadBunLockKeyNoAt(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/bun.lock", []byte(`{
  "lockfileVersion": 0,
  "packages": {
    "react@18.0.0": ["react@18.0.0", {}],
    "badkey": ["badkey", {}]
  }
}
`), 0644)
	pkgs := readBunLock(dir)
	m := toSet(pkgs)
	if !m["react@18.0.0"] {
		t.Errorf("expected react, got %v", pkgs)
	}
	if m["badkey"] {
		t.Error("expected badkey to be skipped")
	}
}

func TestReadBunLockMissing(t *testing.T) {
	if readBunLock(t.TempDir()) != nil {
		t.Error("expected nil for missing file")
	}
}

// npm: pnpm-lock.yaml

func TestReadPNPMLockV6(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/pnpm-lock.yaml", []byte(`lockfileVersion: '6.0'

packages:
  /lodash@4.17.21:
    resolution: {integrity: sha512-abc}
  /@scope/pkg@1.0.0:
    resolution: {integrity: sha512-def}

snapshots: {}
`), 0644)

	pkgs := readPNPMLock(dir)
	if len(pkgs) != 2 {
		t.Fatalf("expected 2, got %d: %v", len(pkgs), pkgs)
	}
	m := toSet(pkgs)
	if !m["lodash@4.17.21"] || !m["@scope/pkg@1.0.0"] {
		t.Errorf("unexpected packages: %v", pkgs)
	}
}

func TestReadPNPMLockV9(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/pnpm-lock.yaml", []byte(`lockfileVersion: '9.0'

packages:
  lodash@4.17.21:
    resolution: {integrity: sha512-abc}
  react@18.2.0:
    resolution: {integrity: sha512-xyz}
`), 0644)

	pkgs := readPNPMLock(dir)
	if len(pkgs) != 2 {
		t.Fatalf("expected 2, got %d: %v", len(pkgs), pkgs)
	}
	m := toSet(pkgs)
	if !m["lodash@4.17.21"] || !m["react@18.2.0"] {
		t.Errorf("unexpected packages: %v", pkgs)
	}
}

func TestReadPNPMLockMissing(t *testing.T) {
	if readPNPMLock(t.TempDir()) != nil {
		t.Error("expected nil for missing file")
	}
}

// Go: go.sum

func TestReadGoSum(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/go.sum", []byte(`github.com/pkg/errors v0.9.1 h1:FEBLx1zS214owpjy7qsBeixbURkuhQAwrK5UwLGTwt4=
github.com/pkg/errors v0.9.1/go.mod h1:bwawxfHBFNV+L2hUp1rHADufV3IMtnDRdf1r5NINEl0=
golang.org/x/sync v0.1.0 h1:wsuoTGHzEhffawBOhz5CYhcrV4IdKZbEyZjBMuTp12o=
golang.org/x/sync v0.1.0/go.mod h1:RxMgew5VJxzue5/jJTE5uejpjVlUs/hafntRnmEBH5A=
`), 0644)

	pkgs := readGoSum(dir)
	if len(pkgs) != 2 {
		t.Fatalf("expected 2 (deduped /go.mod entries), got %d: %v", len(pkgs), pkgs)
	}
	m := toSet(pkgs)
	if !m["github.com/pkg/errors@v0.9.1"] || !m["golang.org/x/sync@v0.1.0"] {
		t.Errorf("unexpected packages: %v", pkgs)
	}
}

func TestReadGoSumMissing(t *testing.T) {
	if readGoSum(t.TempDir()) != nil {
		t.Error("expected nil for missing file")
	}
}

// Python: uv.lock

func TestReadUVLock(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/uv.lock", []byte(`version = 1
requires-python = ">=3.11"

[[package]]
name = "requests"
version = "2.31.0"
source = { registry = "https://pypi.org/simple" }

[[package]]
name = "certifi"
version = "2024.2.2"
source = { registry = "https://pypi.org/simple" }
`), 0644)

	pkgs := readUVLock(dir)
	if len(pkgs) != 2 {
		t.Fatalf("expected 2, got %d: %v", len(pkgs), pkgs)
	}
	m := toSet(pkgs)
	if !m["requests==2.31.0"] || !m["certifi==2024.2.2"] {
		t.Errorf("unexpected packages: %v", pkgs)
	}
}

func TestReadUVLockMissing(t *testing.T) {
	if readUVLock(t.TempDir()) != nil {
		t.Error("expected nil for missing file")
	}
}

// Python: poetry.lock

func TestReadPoetryLock(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/poetry.lock", []byte(`[[package]]
name = "requests"
version = "2.28.0"
description = "Python HTTP for Humans."

[[package]]
name = "flask"
version = "2.3.0"
description = "A simple framework."
`), 0644)

	pkgs := readPoetryLock(dir)
	if len(pkgs) != 2 {
		t.Fatalf("expected 2, got %d: %v", len(pkgs), pkgs)
	}
	m := toSet(pkgs)
	if !m["requests==2.28.0"] || !m["flask==2.3.0"] {
		t.Errorf("unexpected packages: %v", pkgs)
	}
}

func TestReadPoetryLockMissing(t *testing.T) {
	if readPoetryLock(t.TempDir()) != nil {
		t.Error("expected nil for missing file")
	}
}

// Python: Pipfile.lock

func TestReadPipfileLock(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/Pipfile.lock", []byte(`{
		"_meta": {"hash": {"sha256": "abc"}},
		"default": {
			"requests": {"version": "==2.31.0"},
			"certifi": {"version": "==2024.2.2"}
		},
		"develop": {
			"pytest": {"version": "==7.4.0"}
		}
	}`), 0644)

	pkgs := readPipfileLock(dir)
	if len(pkgs) != 3 {
		t.Fatalf("expected 3, got %d: %v", len(pkgs), pkgs)
	}
	m := toSet(pkgs)
	if !m["requests==2.31.0"] || !m["certifi==2024.2.2"] || !m["pytest==7.4.0"] {
		t.Errorf("unexpected packages: %v", pkgs)
	}
}

func TestReadPipfileLockBadJSON(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/Pipfile.lock", []byte("not json"), 0644)
	if readPipfileLock(dir) != nil {
		t.Error("expected nil for bad JSON")
	}
}

func TestReadPipfileLockNoVersion(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/Pipfile.lock", []byte(`{
		"default": {"requests": {"version": ""}},
		"develop": {}
	}`), 0644)
	pkgs := readPipfileLock(dir)
	if len(pkgs) != 1 || pkgs[0] != "requests" {
		t.Errorf("expected [requests] for empty version, got %v", pkgs)
	}
}

func TestReadPipfileLockMissing(t *testing.T) {
	if readPipfileLock(t.TempDir()) != nil {
		t.Error("expected nil for missing file")
	}
}

// Homebrew: Brewfile.lock.json

func TestReadBrewfileLockJSON(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/Brewfile.lock.json", []byte(`{
		"entries": {
			"brew": {
				"git": {"version": "2.43.0", "full_name": "git"},
				"ripgrep": {"version": "14.0.3", "full_name": "ripgrep"}
			},
			"cask": {}
		}
	}`), 0644)

	pkgs := readBrewfileLockJSON(dir)
	if len(pkgs) != 2 {
		t.Fatalf("expected 2, got %d: %v", len(pkgs), pkgs)
	}
	m := toSet(pkgs)
	if !m["git@@2.43.0"] || !m["ripgrep@@14.0.3"] {
		t.Errorf("unexpected packages: %v", pkgs)
	}
}

func TestReadBrewfileLockJSONBadJSON(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/Brewfile.lock.json", []byte("not json"), 0644)
	if readBrewfileLockJSON(dir) != nil {
		t.Error("expected nil for bad JSON")
	}
}

func TestReadBrewfileLockJSONNoVersion(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/Brewfile.lock.json", []byte(`{
		"entries": {
			"brew": {
				"git": {"version": ""},
				"ripgrep": {"version": "14.0.3"}
			},
			"cask": {}
		}
	}`), 0644)
	pkgs := readBrewfileLockJSON(dir)
	m := toSet(pkgs)
	if !m["git"] {
		t.Errorf("expected git without version, got %v", pkgs)
	}
	if !m["ripgrep@@14.0.3"] {
		t.Errorf("expected ripgrep@@14.0.3, got %v", pkgs)
	}
}

func TestReadBrewfileLockJSONMissing(t *testing.T) {
	if readBrewfileLockJSON(t.TempDir()) != nil {
		t.Error("expected nil for missing file")
	}
}

// ReadLockfile dispatch

// readNPMLockfile fallback dispatch

func TestReadNPMLockfileFallbackToBun(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/bun.lock", []byte(`# bun lockfile v1
{
  "lockfileVersion": 0,
  "packages": {
    "react@18.0.0": ["react@18.0.0", {}]
  }
}
`), 0644)
	pkgs := readNPMLockfile(dir)
	if len(pkgs) != 1 || pkgs[0] != "react@18.0.0" {
		t.Errorf("expected fallback to bun.lock, got %v", pkgs)
	}
}

func TestReadNPMLockfileFallbackToPNPM(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/pnpm-lock.yaml", []byte(`lockfileVersion: '9.0'

packages:
  react@18.0.0:
    resolution: {integrity: sha512-abc}
`), 0644)
	pkgs := readNPMLockfile(dir)
	if len(pkgs) != 1 || pkgs[0] != "react@18.0.0" {
		t.Errorf("expected fallback to pnpm-lock.yaml, got %v", pkgs)
	}
}

// readPyLockfile dispatch

func TestReadPyLockfileUV(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/uv.lock", []byte(`[[package]]
name = "requests"
version = "2.31.0"
`), 0644)
	pkgs := readPyLockfile(dir)
	if len(pkgs) != 1 || pkgs[0] != "requests==2.31.0" {
		t.Errorf("expected uv.lock result, got %v", pkgs)
	}
}

func TestReadPyLockfilePoetryFallback(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/poetry.lock", []byte(`[[package]]
name = "flask"
version = "2.3.0"
`), 0644)
	pkgs := readPyLockfile(dir)
	if len(pkgs) != 1 || pkgs[0] != "flask==2.3.0" {
		t.Errorf("expected poetry.lock fallback, got %v", pkgs)
	}
}

func TestReadPyLockfilePipfileFallback(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/Pipfile.lock", []byte(`{
		"default": {"requests": {"version": "==2.31.0"}},
		"develop": {}
	}`), 0644)
	pkgs := readPyLockfile(dir)
	if len(pkgs) != 1 || pkgs[0] != "requests==2.31.0" {
		t.Errorf("expected Pipfile.lock fallback, got %v", pkgs)
	}
}

// ReadLockfile dispatch for Go and PyPI

func TestReadLockfileGo(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/go.sum", []byte(`github.com/pkg/errors v0.9.1 h1:abc=
`), 0644)
	mgr := &Manager{Ecosystem: "Go"}
	pkgs := ReadLockfile(mgr, dir)
	if len(pkgs) != 1 {
		t.Errorf("expected 1 package from go.sum, got %v", pkgs)
	}
}

func TestReadLockfilePyPI(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/uv.lock", []byte(`[[package]]
name = "requests"
version = "2.31.0"
`), 0644)
	mgr := &Manager{Ecosystem: "PyPI"}
	pkgs := ReadLockfile(mgr, dir)
	if len(pkgs) != 1 {
		t.Errorf("expected 1 package from uv.lock, got %v", pkgs)
	}
}

func TestReadLockfileNPM(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/package-lock.json", []byte(`{
		"packages": {"node_modules/lodash": {"version": "4.17.21"}}
	}`), 0644)
	mgr := &Manager{Ecosystem: "npm"}
	pkgs := ReadLockfile(mgr, dir)
	if len(pkgs) != 1 || pkgs[0] != "lodash@4.17.21" {
		t.Errorf("unexpected: %v", pkgs)
	}
}

func TestReadLockfileHomebrew(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/Brewfile.lock.json", []byte(`{
		"entries": {"brew": {"git": {"version": "2.43.0"}}, "cask": {}}
	}`), 0644)
	mgr := &Manager{Ecosystem: "Homebrew"}
	pkgs := ReadLockfile(mgr, dir)
	if len(pkgs) != 1 || pkgs[0] != "git@@2.43.0" {
		t.Errorf("unexpected: %v", pkgs)
	}
}

func TestReadLockfileUnknownEcosystem(t *testing.T) {
	mgr := &Manager{Ecosystem: "unknown"}
	if ReadLockfile(mgr, t.TempDir()) != nil {
		t.Error("expected nil for unknown ecosystem")
	}
}

func TestReadPackageLockJSONNested(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/package-lock.json", []byte(`{
		"packages": {
			"node_modules/foo/node_modules/bar": {"version": "1.0.0"},
			"node_modules/bar": {"version": "2.0.0"}
		}
	}`), 0644)
	pkgs := readPackageLockJSON(dir)
	m := toSet(pkgs)
	if !m["bar@1.0.0"] && !m["bar@2.0.0"] {
		t.Errorf("expected bar in result, got %v", pkgs)
	}
	if len(pkgs) != 1 {
		t.Errorf("expected 1 (deduped), got %d: %v", len(pkgs), pkgs)
	}
}

func TestReadPNPMLockNoAtKey(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/pnpm-lock.yaml", []byte(`lockfileVersion: '9.0'

packages:
  react@18.0.0:
    resolution: {integrity: sha512-abc}
  noversion:
    resolution: {integrity: sha512-xyz}
`), 0644)
	pkgs := readPNPMLock(dir)
	m := toSet(pkgs)
	if !m["react@18.0.0"] {
		t.Errorf("expected react, got %v", pkgs)
	}
	if m["noversion"] {
		t.Error("expected noversion to be skipped")
	}
}

func TestReadGoSumShortLine(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/go.sum", []byte(`github.com/pkg/errors v0.9.1 h1:abc=

garbage
`), 0644)
	pkgs := readGoSum(dir)
	if len(pkgs) != 1 {
		t.Errorf("expected 1 (skipping blank and short lines), got %d: %v", len(pkgs), pkgs)
	}
}

func TestReadPipfileLockDuplicate(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/Pipfile.lock", []byte(`{
		"default": {"requests": {"version": "==2.31.0"}},
		"develop": {"requests": {"version": "==2.31.0"}}
	}`), 0644)
	pkgs := readPipfileLock(dir)
	if len(pkgs) != 1 {
		t.Errorf("expected 1 (deduped), got %d: %v", len(pkgs), pkgs)
	}
}

func toSet(ss []string) map[string]bool {
	m := make(map[string]bool, len(ss))
	for _, s := range ss {
		m[s] = true
	}
	return m
}
