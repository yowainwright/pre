package manager

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// npm: package-lock.json

const testReadPackageLockJSONFixture = `{
		"lockfileVersion": 3,
		"packages": {
			"": {"version": "1.0.0"},
			"node_modules/lodash": {"version": "4.17.21"},
			"node_modules/react": {"version": "18.2.0"},
			"node_modules/react-dom": {"version": "18.2.0"}
		}
	}`

func TestReadPackageLockJSON(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/package-lock.json", []byte(testReadPackageLockJSONFixture), 0644)

	pkgs := readPackageLockJSON(dir)
	if len(pkgs) != 3 {
		t.Fatalf("expected 3, got %d: %v", len(pkgs), pkgs)
	}
	m := toSet(pkgs)
	missingPackagesPrefix := !m["lodash@4.17.21"] || !m["react@18.2.0"]
	missingPackages := missingPackagesPrefix || !m["react-dom@18.2.0"]
	if missingPackages {
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
	unexpectedPackages := len(pkgs) != 1 || pkgs[0] != "express@4.18.0"
	if unexpectedPackages {
		t.Errorf("expected [express@4.18.0], got %v", pkgs)
	}
}

func TestReadPackageLockJSONPreservesMultipleVersions(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/package-lock.json", []byte(`{
		"packages": {
			"node_modules/lodash": {"version": "4.17.21"},
			"node_modules/pkg-a/node_modules/lodash": {"version": "4.17.20"}
		}
	}`), 0644)

	pkgs := readPackageLockJSON(dir)
	m := toSet(pkgs)
	missingPackages := !m["lodash@4.17.21"] || !m["lodash@4.17.20"]
	if missingPackages {
		t.Errorf("expected both lodash versions, got %v", pkgs)
	}
}

func TestReadPackageLockJSONUsesDeclaredPackageName(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/package-lock.json", []byte(`{
		"packages": {
			"node_modules/lodash": {"name": "evil-pkg", "version": "4.17.21"}
		}
	}`), 0o644)

	packages := readPackageLockJSON(dir)
	unexpectedPackages := len(packages) != 1 || packages[0] != "evil-pkg@4.17.21"
	if unexpectedPackages {
		t.Fatalf("unexpected package-lock packages: %v", packages)
	}
}

const testReadPackageLockJSONV1DependenciesFixture = `{
		"lockfileVersion": 1,
		"dependencies": {
			"lodash": {"version": "4.17.21"},
			"pkg-a": {
				"version": "1.0.0",
				"dependencies": {
					"lodash": {"version": "4.17.20"}
				}
			}
		}
	}`

func TestReadPackageLockJSONV1Dependencies(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/package-lock.json", []byte(testReadPackageLockJSONV1DependenciesFixture), 0644)

	pkgs := readPackageLockJSON(dir)
	m := toSet(pkgs)
	missingPackagesPrefix := !m["lodash@4.17.21"] || !m["pkg-a@1.0.0"]
	missingPackages := missingPackagesPrefix || !m["lodash@4.17.20"]
	if missingPackages {
		t.Errorf("expected v1 dependencies and nested dependencies, got %v", pkgs)
	}
	if len(pkgs) != 3 {
		t.Errorf("expected 3 packages, got %d: %v", len(pkgs), pkgs)
	}
}

func TestReadPackageLockJSONRootOnlyPackagesFallsBackToDependencies(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/package-lock.json", []byte(`{
		"lockfileVersion": 2,
		"packages": {
			"": {"version": "1.0.0"}
		},
		"dependencies": {
			"lodash": {"version": "4.17.21"}
		}
	}`), 0644)

	pkgs := readPackageLockJSON(dir)
	unexpectedPackages := len(pkgs) != 1 || pkgs[0] != "lodash@4.17.21"
	if unexpectedPackages {
		t.Errorf("expected dependency fallback for root-only packages map, got %v", pkgs)
	}
}

func TestReadPackageLockJSONV1DepthLimit(t *testing.T) {
	dir := t.TempDir()
	data := nestedPackageLockJSON(t)
	os.WriteFile(dir+"/package-lock.json", data, 0644)

	pkgs := readPackageLockJSON(dir)
	m := toSet(pkgs)
	lastAllowed := fmt.Sprintf("pkg-%02d@1.0.%d", maxPackageLockDependencyDepth-1, maxPackageLockDependencyDepth-1)
	firstSkipped := fmt.Sprintf("pkg-%02d@1.0.%d", maxPackageLockDependencyDepth, maxPackageLockDependencyDepth)
	missingPackages := !m["pkg-00@1.0.0"] || !m[lastAllowed]
	if missingPackages {
		t.Errorf("expected packages through depth limit, got %v", pkgs)
	}
	if m[firstSkipped] {
		t.Errorf("expected package beyond depth limit to be skipped, got %v", pkgs)
	}
	if len(pkgs) != maxPackageLockDependencyDepth {
		t.Errorf("expected %d packages, got %d: %v", maxPackageLockDependencyDepth, len(pkgs), pkgs)
	}
}

func nestedPackageLockDeps(depth, maxDepth int) map[string]packageLockDependency {
	if depth >= maxDepth {
		return nil
	}
	return map[string]packageLockDependency{
		fmt.Sprintf("pkg-%02d", depth): {
			Version:      fmt.Sprintf("1.0.%d", depth),
			Dependencies: nestedPackageLockDeps(depth+1, maxDepth),
		},
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

const testReadBunLockFixture = `# bun lockfile v1 (https://bun.sh)

{
  "lockfileVersion": 0,
  "packages": {
    "lodash@4.17.21": ["lodash@4.17.21", {}],
    "react@18.2.0": ["react@18.2.0", {}],
    "@scope/pkg@1.0.0": ["@scope/pkg@1.0.0", {}]
  }
}
`

func TestReadBunLock(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/bun.lock", []byte(testReadBunLockFixture), 0644)

	pkgs := readBunLock(dir)
	if len(pkgs) != 3 {
		t.Fatalf("expected 3, got %d: %v", len(pkgs), pkgs)
	}
	m := toSet(pkgs)
	missingPackagesPrefix := !m["lodash@4.17.21"] || !m["react@18.2.0"]
	missingPackages := missingPackagesPrefix || !m["@scope/pkg@1.0.0"]
	if missingPackages {
		t.Errorf("unexpected packages: %v", pkgs)
	}
}

func TestReadBunLockAllowsJSONC(t *testing.T) {
	dir := t.TempDir()
	lock := `# Bun Lockfile v1

{
  // Bun emits JSONC, not strict JSON.
  "lockfileVersion": 1,
  "packages": {
    "react": ["react@18.2.0", {}, "sha512-abc"],
  },
}
`
	os.WriteFile(dir+"/bun.lock", []byte(lock), 0o644)

	packages := readBunLock(dir)
	unexpectedPackages := len(packages) != 1 || packages[0] != "react@18.2.0"
	if unexpectedPackages {
		t.Fatalf("unexpected Bun packages: %v", packages)
	}
}

func TestReadBunLockBadJSON(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/bun.lock", []byte("not json"), 0644)
	if readBunLock(dir) != nil {
		t.Error("expected nil for bad JSON")
	}
}

func TestReadBunLockPreservesMultipleVersions(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/bun.lock", []byte(`{
  "lockfileVersion": 0,
  "packages": {
    "lodash@4.17.21": ["lodash@4.17.21", {}],
    "lodash@4.17.20": ["lodash@4.17.20", {}]
  }
}
`), 0644)

	pkgs := readBunLock(dir)
	m := toSet(pkgs)
	missingPackages := !m["lodash@4.17.21"] || !m["lodash@4.17.20"]
	if missingPackages {
		t.Errorf("expected both lodash versions, got %v", pkgs)
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

func TestBunPackageSpecBadValue(t *testing.T) {
	cases := []struct {
		key string
		raw json.RawMessage
	}{
		{"react", json.RawMessage(`not-json`)},
		{"react", json.RawMessage(`[]`)},
		{"react", json.RawMessage(`[123]`)},
		{"react", json.RawMessage(`["noatsign"]`)},
	}
	for _, c := range cases {
		if got := bunPackageSpec(c.key, c.raw); got != "" {
			t.Errorf("bunPackageSpec(%q, %s) = %q, want empty", c.key, c.raw, got)
		}
	}
}

const testReadBunLockNewFormatFixture = `# Bun Lockfile v1

{
  "lockfileVersion": 1,
  "packages": {
    "react": ["react@18.2.0", {}, "sha512-abc"],
    "@opentelemetry/api": ["@opentelemetry/api@1.9.0", {}, "sha512-def"],
    "next": ["next@15.2.3", {}, "sha512-ghi"]
  }
}
`

func TestReadBunLockNewFormat(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/bun.lock", []byte(testReadBunLockNewFormatFixture), 0644)

	pkgs := readBunLock(dir)
	if len(pkgs) != 3 {
		t.Fatalf("expected 3, got %d: %v", len(pkgs), pkgs)
	}
	m := toSet(pkgs)
	missingPackagesPrefix := !m["react@18.2.0"] || !m["@opentelemetry/api@1.9.0"]
	missingPackages := missingPackagesPrefix || !m["next@15.2.3"]
	if missingPackages {
		t.Errorf("unexpected packages: %v", pkgs)
	}
}

func TestReadBunLockNestedScopedPackage(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/bun.lock", []byte(`{
  "lockfileVersion": 1,
  "packages": {
    "next/@babel/core": ["@babel/core@7.26.10", {}, "sha512-abc"]
  }
}
`), 0o644)

	packages := readBunLock(dir)
	unexpectedPackages := len(packages) != 1 || packages[0] != "@babel/core@7.26.10"
	if unexpectedPackages {
		t.Fatalf("unexpected Bun packages: %v", packages)
	}
}

func TestReadBunLockMissing(t *testing.T) {
	if readBunLock(t.TempDir()) != nil {
		t.Error("expected nil for missing file")
	}
}

// npm: pnpm-lock.yaml

const testReadPNPMLockV6Fixture = `lockfileVersion: '6.0'

packages:
  /lodash@4.17.21:
    resolution: {integrity: sha512-abc}
  /@scope/pkg@1.0.0:
    resolution: {integrity: sha512-def}

snapshots: {}
`

func TestReadPNPMLockV6(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/pnpm-lock.yaml", []byte(testReadPNPMLockV6Fixture), 0644)

	pkgs := readPNPMLock(dir)
	if len(pkgs) != 2 {
		t.Fatalf("expected 2, got %d: %v", len(pkgs), pkgs)
	}
	m := toSet(pkgs)
	missingPackages := !m["lodash@4.17.21"] || !m["@scope/pkg@1.0.0"]
	if missingPackages {
		t.Errorf("unexpected packages: %v", pkgs)
	}
}

const testReadPNPMLockV9Fixture = `lockfileVersion: '9.0'

packages:
  lodash@4.17.21:
    resolution: {integrity: sha512-abc}
  react@18.2.0:
    resolution: {integrity: sha512-xyz}
  '@scope/pkg@1.0.0':
    resolution: {integrity: sha512-def}
`

func TestReadPNPMLockV9(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/pnpm-lock.yaml", []byte(testReadPNPMLockV9Fixture), 0644)

	pkgs := readPNPMLock(dir)
	if len(pkgs) != 3 {
		t.Fatalf("expected 3, got %d: %v", len(pkgs), pkgs)
	}
	m := toSet(pkgs)
	hasAllPackages := m["lodash@4.17.21"] && m["react@18.2.0"] && m["@scope/pkg@1.0.0"]
	if !hasAllPackages {
		t.Errorf("unexpected packages: %v", pkgs)
	}
}

func TestReadPNPMLockMissing(t *testing.T) {
	if readPNPMLock(t.TempDir()) != nil {
		t.Error("expected nil for missing file")
	}
}

func TestReadPNPMLockPreservesMultipleVersions(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/pnpm-lock.yaml", []byte(`lockfileVersion: '9.0'

packages:
  lodash@4.17.21:
    resolution: {integrity: sha512-a}
  lodash@4.17.20:
    resolution: {integrity: sha512-b}
`), 0644)

	pkgs := readPNPMLock(dir)
	m := toSet(pkgs)
	missingPackages := !m["lodash@4.17.21"] || !m["lodash@4.17.20"]
	if missingPackages {
		t.Errorf("expected both lodash versions, got %v", pkgs)
	}
}

func TestReadPNPMLockStripsPeerDependencySuffix(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/pnpm-lock.yaml", []byte(`lockfileVersion: '9.0'

packages:
  react-dom@18.2.0(react@18.2.0):
    resolution: {integrity: sha512-a}
  '@scope/pkg@1.0.0(react@18.2.0)':
    resolution: {integrity: sha512-b}
`), 0o644)

	packages := toSet(readPNPMLock(dir))
	unexpectedPackagesPrefix := len(packages) != 2 || !packages["react-dom@18.2.0"]
	unexpectedPackages := unexpectedPackagesPrefix || !packages["@scope/pkg@1.0.0"]
	if unexpectedPackages {
		t.Fatalf("unexpected pnpm packages: %v", packages)
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
	missingPackages := !m["github.com/pkg/errors@v0.9.1"] || !m["golang.org/x/sync@v0.1.0"]
	if missingPackages {
		t.Errorf("unexpected packages: %v", pkgs)
	}
}

func TestReadGoSumMissing(t *testing.T) {
	if readGoSum(t.TempDir()) != nil {
		t.Error("expected nil for missing file")
	}
}

// Python: uv.lock

const testReadUVLockFixture = `version = 1
requires-python = ">=3.11"

[[package]]
name = "requests"
version = "2.31.0"
source = { registry = "https://pypi.org/simple" }

[[package]]
name = "certifi"
version = "2024.2.2"
source = { registry = "https://pypi.org/simple" }
`

func TestReadUVLock(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/uv.lock", []byte(testReadUVLockFixture), 0644)

	pkgs := readUVLock(dir)
	if len(pkgs) != 2 {
		t.Fatalf("expected 2, got %d: %v", len(pkgs), pkgs)
	}
	m := toSet(pkgs)
	missingPackages := !m["requests==2.31.0"] || !m["certifi==2024.2.2"]
	if missingPackages {
		t.Errorf("unexpected packages: %v", pkgs)
	}
}

func TestReadUVLockMissing(t *testing.T) {
	if readUVLock(t.TempDir()) != nil {
		t.Error("expected nil for missing file")
	}
}

// Python: poetry.lock

const testReadPoetryLockFixture = `[[package]]
name = "requests"
version = "2.28.0"
description = "Python HTTP for Humans."

[[package]]
name = "flask"
version = "2.3.0"
description = "A simple framework."
`

func TestReadPoetryLock(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/poetry.lock", []byte(testReadPoetryLockFixture), 0644)

	pkgs := readPoetryLock(dir)
	if len(pkgs) != 2 {
		t.Fatalf("expected 2, got %d: %v", len(pkgs), pkgs)
	}
	m := toSet(pkgs)
	missingPackages := !m["requests==2.28.0"] || !m["flask==2.3.0"]
	if missingPackages {
		t.Errorf("unexpected packages: %v", pkgs)
	}
}

func TestReadPoetryLockMissing(t *testing.T) {
	if readPoetryLock(t.TempDir()) != nil {
		t.Error("expected nil for missing file")
	}
}

// Python: Pipfile.lock

const testReadPipfileLockFixture = `{
		"_meta": {"hash": {"sha256": "abc"}},
		"default": {
			"requests": {"version": "==2.31.0"},
			"certifi": {"version": "==2024.2.2"}
		},
		"develop": {
			"pytest": {"version": "==7.4.0"}
		}
	}`

func TestReadPipfileLock(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/Pipfile.lock", []byte(testReadPipfileLockFixture), 0644)

	pkgs := readPipfileLock(dir)
	if len(pkgs) != 3 {
		t.Fatalf("expected 3, got %d: %v", len(pkgs), pkgs)
	}
	m := toSet(pkgs)
	missingPackagesPrefix := !m["requests==2.31.0"] || !m["certifi==2024.2.2"]
	missingPackages := missingPackagesPrefix || !m["pytest==7.4.0"]
	if missingPackages {
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
	unexpectedPackages := len(pkgs) != 1 || pkgs[0] != "requests"
	if unexpectedPackages {
		t.Errorf("expected [requests] for empty version, got %v", pkgs)
	}
}

func TestReadPipfileLockMissing(t *testing.T) {
	if readPipfileLock(t.TempDir()) != nil {
		t.Error("expected nil for missing file")
	}
}

// Homebrew: Brewfile.lock.json

const testReadBrewfileLockJSONFixture = `{
		"entries": {
			"brew": {
				"git": {"version": "2.43.0", "full_name": "git"},
				"ripgrep": {"version": "14.0.3", "full_name": "ripgrep"}
			},
			"cask": {}
		}
	}`

func TestReadBrewfileLockJSON(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/Brewfile.lock.json", []byte(testReadBrewfileLockJSONFixture), 0644)

	pkgs := readBrewfileLockJSON(dir)
	if len(pkgs) != 2 {
		t.Fatalf("expected 2, got %d: %v", len(pkgs), pkgs)
	}
	m := toSet(pkgs)
	missingPackages := !m["git@@2.43.0"] || !m["ripgrep@@14.0.3"]
	if missingPackages {
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
	unexpectedPackages := len(pkgs) != 1 || pkgs[0] != "react@18.0.0"
	if unexpectedPackages {
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
	unexpectedPackages := len(pkgs) != 1 || pkgs[0] != "react@18.0.0"
	if unexpectedPackages {
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
	unexpectedPackages := len(pkgs) != 1 || pkgs[0] != "requests==2.31.0"
	if unexpectedPackages {
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
	unexpectedPackages := len(pkgs) != 1 || pkgs[0] != "flask==2.3.0"
	if unexpectedPackages {
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
	unexpectedPackages := len(pkgs) != 1 || pkgs[0] != "requests==2.31.0"
	if unexpectedPackages {
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
	unexpectedPackages := len(pkgs) != 1 || pkgs[0] != "lodash@4.17.21"
	if unexpectedPackages {
		t.Errorf("unexpected: %v", pkgs)
	}
}

func TestReadLockfileUsesNPMManagerLock(t *testing.T) {
	dir := writeNPMManagerLocks(t)

	tests := []struct {
		manager string
		want    string
	}{
		{manager: "npm", want: "from-npm@1.0.0"},
		{manager: "bun", want: "from-bun@2.0.0"},
		{manager: "pnpm", want: "from-pnpm@3.0.0"},
	}
	for _, test := range tests {
		mgr := &Manager{Name: test.manager, Ecosystem: "npm"}
		packages := ReadLockfile(mgr, dir)
		unexpectedPackages := len(packages) != 1 || packages[0] != test.want
		if unexpectedPackages {
			t.Errorf("%s: expected %q, got %v", test.manager, test.want, packages)
		}
	}
}

func TestReadLockfileUsesPythonManagerLock(t *testing.T) {
	dir := writePythonManagerLocks(t)

	uvPackages := ReadLockfile(&Manager{Name: "uv", Ecosystem: "PyPI"}, dir)
	poetryPackages := ReadLockfile(&Manager{Name: "poetry", Ecosystem: "PyPI"}, dir)
	pipPackages := ReadLockfile(&Manager{Name: "pip", Ecosystem: "PyPI"}, dir)
	unexpectedUvPackages := len(uvPackages) != 1 || uvPackages[0] != "from-uv==1.0.0"
	if unexpectedUvPackages {
		t.Fatalf("unexpected uv packages: %v", uvPackages)
	}
	unexpectedPoetryPackages := len(poetryPackages) != 1 || poetryPackages[0] != "from-poetry==2.0.0"
	if unexpectedPoetryPackages {
		t.Fatalf("unexpected Poetry packages: %v", poetryPackages)
	}
	unexpectedPipPackages := len(pipPackages) != 1 || pipPackages[0] != "from-pip==3.0.0"
	if unexpectedPipPackages {
		t.Fatalf("unexpected pip packages: %v", pipPackages)
	}
}

func TestReadLockfileHomebrew(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/Brewfile.lock.json", []byte(`{
		"entries": {"brew": {"git": {"version": "2.43.0"}}, "cask": {}}
	}`), 0644)
	mgr := &Manager{Ecosystem: "Homebrew"}
	pkgs := ReadLockfile(mgr, dir)
	unexpectedPackages := len(pkgs) != 1 || pkgs[0] != "git@@2.43.0"
	if unexpectedPackages {
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
	missingPackages := !m["bar@1.0.0"] || !m["bar@2.0.0"]
	if missingPackages {
		t.Errorf("expected both bar versions in result, got %v", pkgs)
	}
	if len(pkgs) != 2 {
		t.Errorf("expected 2 (both versions preserved), got %d: %v", len(pkgs), pkgs)
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

const testReadCargoLockFixture = `version = 4

[[package]]
name = "app"
version = "0.1.0"

[[package]]
name = "serde"
version = "1.0.217"
source = "registry+https://github.com/rust-lang/crates.io-index"

[[package]]
name = "regex"
version = "1.11.1"
source = "sparse+https://index.crates.io/"

[[package]]
name = "git-only"
version = "1.2.3"
source = "git+https://example.com/repo"
`

func TestReadCargoLock(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(dir+"/Cargo.lock", []byte(testReadCargoLockFixture), 0644)
	if err != nil {
		t.Fatalf("write Cargo.lock: %v", err)
	}

	mgr := &Manager{Ecosystem: "crates.io"}
	pkgs := ReadLockfile(mgr, dir)
	set := toSet(pkgs)
	unexpectedPackagesPrefix := len(pkgs) != 2 || !set["serde@1.0.217"]
	unexpectedPackages := unexpectedPackagesPrefix || !set["regex@1.11.1"]
	if unexpectedPackages {
		t.Errorf("unexpected Cargo.lock packages: %v", pkgs)
	}
}

const cargoLockFixture = "[[package]]\nname = \"serde\"\nversion = \"1.0.217\"\nsource = \"sparse+https://index.crates.io/\"\n"

func TestReadCargoLockTOMLSyntax(t *testing.T) {
	for name, input := range cargoLockSyntaxFixtures() {
		t.Run(name, func(t *testing.T) {
			want := []string{"serde@1.0.217"}
			assertCargoLockPackages(t, input, want)
		})
	}
}

func cargoLockSyntaxFixtures() map[string]string {
	quotedKey := strings.ReplaceAll(cargoLockFixture, "name =", `"name" =`)
	escapedSource := strings.ReplaceAll(cargoLockFixture, "index.crates.io", `index\u002ecrates.io`)
	commentedHeader := strings.ReplaceAll(cargoLockFixture, "[[package]]", "[[package]] # dependency")
	literalStrings := strings.ReplaceAll(cargoLockFixture, `"`, "'")
	quotedTable := strings.ReplaceAll(cargoLockFixture, "[[package]]", "[[ 'package' ]]")
	metadata := cargoLockFixture + "dependencies = ['other 1.0.0']\n[package.metadata]\nnote = 'ignored'\n"
	return map[string]string{
		"quoted key": quotedKey, "escaped source": escapedSource,
		"header comment": commentedHeader, "literal strings": literalStrings,
		"quoted table": quotedTable, "extra metadata": metadata,
	}
}

func TestReadCargoLockCaseSensitiveKeys(t *testing.T) {
	for _, key := range []string{"package", "name", "version", "source"} {
		t.Run(key, func(t *testing.T) {
			uppercase := strings.ToUpper(key)
			input := strings.ReplaceAll(cargoLockFixture, key, uppercase)
			assertCargoLockPackages(t, input, nil)
		})
	}
}

func TestReadCargoLockIgnoresUppercaseSource(t *testing.T) {
	input := cargoLockFixture + "SOURCE='git+https://example.com/repo'\n"
	want := []string{"serde@1.0.217"}
	assertCargoLockPackages(t, input, want)
}

func TestReadCargoLockUppercaseSourceCannotBypassRejection(t *testing.T) {
	input := strings.ReplaceAll(cargoLockFixture, "sparse+https://index.crates.io/", "git+https://example.com/repo")
	input += "SOURCE='sparse+https://index.crates.io/'\n"
	assertCargoLockError(t, input, "unsupported Cargo source")
}

func TestReadCargoLockQuotedSourceCannotBypassRejection(t *testing.T) {
	input := strings.ReplaceAll(cargoLockFixture, "sparse+https://index.crates.io/", "git+https://example.com/repo")
	input = strings.ReplaceAll(input, "source =", `"source" =`)
	assertCargoLockError(t, input, "unsupported Cargo source")
}

func TestReadCargoLockRejectsInvalidTOML(t *testing.T) {
	inputs := []string{
		cargoLockFixture + "source='git+https://example.com/repo'\n",
		cargoLockFixture + "broken = [\n",
		strings.ReplaceAll(cargoLockFixture, `"1.0.217"`, "42"),
		strings.ReplaceAll(cargoLockFixture, `"sparse+https://index.crates.io/"`, "false"),
		"package = 42\n",
	}
	for index, input := range inputs {
		name := fmt.Sprintf("case-%d", index)
		t.Run(name, func(t *testing.T) {
			assertCargoLockError(t, input, "parse ")
		})
	}
}

func TestReadCargoLockPreservesOrderAndDeduplicates(t *testing.T) {
	local := "[[package]]\nname='app'\nversion='1.0.0'\n"
	second := strings.ReplaceAll(cargoLockFixture, "serde", "regex")
	input := cargoLockFixture + local + second + cargoLockFixture
	want := []string{"serde@1.0.217", "regex@1.0.217"}
	assertCargoLockPackages(t, input, want)
}

func assertCargoLockPackages(t *testing.T, input string, want []string) {
	t.Helper()
	packages, err := readCargoLockFixture(t, input)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(packages, want) {
		t.Fatalf("expected packages %v, got %v", want, packages)
	}
}

func assertCargoLockError(t *testing.T, input, message string) {
	t.Helper()
	packages, err := readCargoLockFixture(t, input)
	if err == nil {
		t.Fatalf("expected %q error for %q", message, input)
	}
	valid := packages == nil && strings.Contains(err.Error(), message)
	if !valid {
		t.Fatalf("expected no packages and %q error, got packages=%v error=%v", message, packages, err)
	}
}

func readCargoLockFixture(t *testing.T, input string) ([]string, error) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "Cargo.lock")
	writeCargoTestFile(t, path, input)
	return readCargoLockFile(path)
}

func toSet(ss []string) map[string]bool {
	m := make(map[string]bool, len(ss))
	for _, s := range ss {
		m[s] = true
	}
	return m
}

func nestedPackageLockJSON(t *testing.T) []byte {
	t.Helper()
	lockfile := struct {
		LockfileVersion int                              `json:"lockfileVersion"`
		Dependencies    map[string]packageLockDependency `json:"dependencies"`
	}{
		LockfileVersion: 1,
		Dependencies:    nestedPackageLockDeps(0, maxPackageLockDependencyDepth+5),
	}
	data, err := json.Marshal(lockfile)
	if err != nil {
		t.Fatalf("marshal lockfile: %v", err)
	}
	return data
}

func writeNPMManagerLocks(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	packageLock := `{"packages":{"node_modules/from-npm":{"version":"1.0.0"}}}`
	bunLock := `{"packages":{"from-bun@2.0.0":["from-bun@2.0.0",{}]}}`
	pnpmLock := "packages:\n  from-pnpm@3.0.0:\n"
	os.WriteFile(dir+"/package-lock.json", []byte(packageLock), 0o644)
	os.WriteFile(dir+"/bun.lock", []byte(bunLock), 0o644)
	os.WriteFile(dir+"/pnpm-lock.yaml", []byte(pnpmLock), 0o644)

	return dir
}

func writePythonManagerLocks(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	uvLock := "[[package]]\nname = \"from-uv\"\nversion = \"1.0.0\"\n"
	poetryLock := "[[package]]\nname = \"from-poetry\"\nversion = \"2.0.0\"\n"
	pipfileLock := `{"default":{"from-pip":{"version":"==3.0.0"}}}`
	os.WriteFile(dir+"/uv.lock", []byte(uvLock), 0o644)
	os.WriteFile(dir+"/poetry.lock", []byte(poetryLock), 0o644)
	os.WriteFile(dir+"/Pipfile.lock", []byte(pipfileLock), 0o644)

	return dir
}
