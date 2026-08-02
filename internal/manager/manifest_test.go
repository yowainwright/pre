package manager

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestReadPackageJSON(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/package.json", []byte(`{
		"dependencies": {"lodash": "^4.17.21", "react": "^18.0.0"},
		"devDependencies": {"typescript": "^5.0.0"},
		"optionalDependencies": {"fsevents": "^2.3.3"}
	}`), 0644)

	names := readPackageJSON(dir)
	sort.Strings(names)
	if len(names) != 4 {
		t.Fatalf("expected 4 packages, got %d: %v", len(names), names)
	}
	if names[0] != "fsevents@^2.3.3" || names[1] != "lodash@^4.17.21" || names[2] != "react@^18.0.0" || names[3] != "typescript@^5.0.0" {
		t.Errorf("unexpected packages: %v", names)
	}
}

func TestReadPackageJSONMissing(t *testing.T) {
	names := readPackageJSON(t.TempDir())
	if names != nil {
		t.Errorf("expected nil for missing file, got %v", names)
	}
}

func TestReadPackageJSONDeduplicates(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/package.json", []byte(`{
		"dependencies": {"lodash": "^4.0.0"},
		"devDependencies": {"lodash": "^4.0.0"}
	}`), 0644)

	names := readPackageJSON(dir)
	if len(names) != 1 {
		t.Errorf("expected 1 (deduped), got %d: %v", len(names), names)
	}
	if len(names) == 1 && names[0] != "lodash@^4.0.0" {
		t.Errorf("expected preserved npm spec, got %v", names)
	}
}

func TestReadGoMod(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/go.mod", []byte(`module example.com/app

go 1.22

require (
	github.com/some/pkg v1.2.3
	github.com/other/pkg v0.1.0 // indirect
)

require github.com/single/pkg v2.0.0
`), 0644)

	names := readGoMod(dir)
	if len(names) != 3 {
		t.Fatalf("expected 3 packages, got %d: %v", len(names), names)
	}
}

func TestReadGoModMissing(t *testing.T) {
	names := readGoMod(t.TempDir())
	if names != nil {
		t.Errorf("expected nil for missing file, got %v", names)
	}
}

func TestReadRequirementsTxt(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/other.txt", []byte("urllib3==2.2.3\n"), 0644); err != nil {
		t.Fatalf("write included requirements: %v", err)
	}
	err := os.WriteFile(dir+"/requirements.txt", []byte(`# comment
requests==2.28.0
flask>=2.0
-r other.txt
`), 0644)
	if err != nil {
		t.Fatalf("write requirements.txt: %v", err)
	}

	names := readRequirementsTxt(dir)
	if len(names) != 3 {
		t.Fatalf("expected 3 packages, got %d: %v", len(names), names)
	}
}

func TestReadRequirementsFileResolvesNestedFiles(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "nested.txt")
	root := filepath.Join(dir, "requirements.txt")
	if err := os.WriteFile(nested, []byte("urllib3==2.2.3\n"), 0644); err != nil {
		t.Fatalf("write nested requirements: %v", err)
	}
	if err := os.WriteFile(root, []byte("requests==2.32.0\n--requirement nested.txt\n"), 0644); err != nil {
		t.Fatalf("write root requirements: %v", err)
	}

	packages, err := ReadRequirementsFile(root)
	if err != nil {
		t.Fatalf("read requirements: %v", err)
	}
	if len(packages) != 2 || packages[0] != "requests==2.32.0" || packages[1] != "urllib3==2.2.3" {
		t.Errorf("unexpected packages: %v", packages)
	}
}

func TestReadRequirementsFileRejectsCycles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "requirements.txt")
	if err := os.WriteFile(path, []byte("-r requirements.txt\n"), 0644); err != nil {
		t.Fatalf("write cyclic requirements: %v", err)
	}

	if _, err := ReadRequirementsFile(path); err == nil {
		t.Error("expected cyclic requirements error")
	}
}

func TestReadBrewfile(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/Brewfile", []byte(`tap "homebrew/core"
brew "git"
brew "ripgrep"
cask "iterm2"
`), 0644)

	names := readBrewfile(dir)
	if len(names) != 2 {
		t.Fatalf("expected 2 packages, got %d: %v", len(names), names)
	}
	if names[0] != "git" || names[1] != "ripgrep" {
		t.Errorf("unexpected packages: %v", names)
	}
}

func TestReadManifestNpmEcosystem(t *testing.T) {
	mgr := &Manager{Ecosystem: "npm"}
	dir := t.TempDir()
	os.WriteFile(dir+"/package.json", []byte(`{"dependencies":{"lodash":"^4.0.0"}}`), 0644)

	names := readManifestDir(mgr, dir)
	if len(names) != 1 || names[0] != "lodash@^4.0.0" {
		t.Errorf("unexpected: %v", names)
	}
}

func TestReadManifestDirGo(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/go.mod", []byte("module example.com/app\ngo 1.22\nrequire github.com/some/pkg v1.2.3\n"), 0644)
	mgr := &Manager{Ecosystem: "Go"}
	names := readManifestDir(mgr, dir)
	if len(names) != 1 {
		t.Fatalf("expected 1 package, got %d: %v", len(names), names)
	}
}

func TestReadManifestDirPyPI(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/requirements.txt", []byte("requests==2.28.0\n"), 0644)
	mgr := &Manager{Ecosystem: "PyPI"}
	names := readManifestDir(mgr, dir)
	if len(names) != 1 {
		t.Fatalf("expected 1 package, got %d: %v", len(names), names)
	}
}

func TestReadManifestDirHomebrew(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/Brewfile", []byte("brew \"git\"\nbrew \"ripgrep\"\n"), 0644)
	mgr := &Manager{Ecosystem: "Homebrew"}
	names := readManifestDir(mgr, dir)
	if len(names) != 2 {
		t.Fatalf("expected 2 packages, got %d: %v", len(names), names)
	}
}

func TestReadManifestPrefersLockfile(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(orig)

	os.WriteFile("package-lock.json", []byte(`{
		"packages": {"node_modules/lodash": {"version": "4.17.21"}}
	}`), 0644)

	mgr := &Manager{Ecosystem: "npm"}
	pkgs := ReadManifest(mgr)
	if len(pkgs) != 1 || pkgs[0] != "lodash@4.17.21" {
		t.Errorf("expected lockfile result, got %v", pkgs)
	}
}

func TestReadManifestFallsBackToManifest(t *testing.T) {
	dir := t.TempDir()
	orig, _ := os.Getwd()
	os.Chdir(dir)
	defer os.Chdir(orig)

	os.WriteFile("package.json", []byte(`{"dependencies":{"react":"^18.0.0"}}`), 0644)

	mgr := &Manager{Ecosystem: "npm"}
	pkgs := ReadManifest(mgr)
	if len(pkgs) != 1 || pkgs[0] != "react@^18.0.0" {
		t.Errorf("expected manifest fallback result, got %v", pkgs)
	}
}

func TestReadPackageJSONKeepsUnsupportedSpecsForValidation(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/package.json", []byte(`{
		"dependencies": {
			"localpkg": "file:../localpkg",
			"workspacepkg": "workspace:*"
		}
	}`), 0644)

	names := readPackageJSON(dir)
	set := manifestSet(names)
	if len(names) != 2 || !set["localpkg"] || !set["workspacepkg"] {
		t.Errorf("expected unsupported specs to remain visible, got %v", names)
	}
}

func TestReadPackageJSONBadJSON(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/package.json", []byte("not json"), 0644)
	names := readPackageJSON(dir)
	if names != nil {
		t.Errorf("expected nil for bad JSON, got %v", names)
	}
}

func TestReadRequirementsTxtNoName(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/requirements.txt", []byte("==2.28.0\n"), 0644)
	names := readRequirementsTxt(dir)
	if len(names) != 0 {
		t.Errorf("expected 0 packages for no-name spec, got %v", names)
	}
}

func TestReadManifestUnknownEcosystem(t *testing.T) {
	mgr := &Manager{Ecosystem: "unknown"}
	names := readManifestDir(mgr, t.TempDir())
	if names != nil {
		t.Errorf("expected nil for unknown ecosystem")
	}
}

func TestReadRequirementsTxtMissing(t *testing.T) {
	if readRequirementsTxt(t.TempDir()) != nil {
		t.Error("expected nil for missing requirements.txt")
	}
}

func TestReadBrewfileMissing(t *testing.T) {
	if readBrewfile(t.TempDir()) != nil {
		t.Error("expected nil for missing Brewfile")
	}
}

func TestReadCargoToml(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(dir+"/Cargo.toml", []byte(`[dependencies]
serde = "1.0"
regex = { version = "1.11", features = ["unicode"] }
runtime = { package = "tokio", version = "1.42" }
local = { path = "../local", version = "1.0" }
git-only = { git = "https://example.com/repo", version = "2.0" }

[dev-dependencies]
tempfile = "3"

[target.'cfg(unix)'.dependencies]
nix = "0.29"

[workspace.dependencies]
anyhow = "1"
`), 0644)
	if err != nil {
		t.Fatalf("write Cargo.toml: %v", err)
	}

	mgr := &Manager{Ecosystem: "crates.io"}
	packages := readManifestDir(mgr, dir)
	set := manifestSet(packages)
	want := []string{"serde@^1.0", "regex@^1.11", "tokio@^1.42", "tempfile@^3", "nix@^0.29", "anyhow@^1"}
	for _, spec := range want {
		if !set[spec] {
			t.Errorf("missing %q from %v", spec, packages)
		}
	}
	if len(packages) != len(want) {
		t.Errorf("expected %d packages, got %d: %v", len(want), len(packages), packages)
	}
}

func TestReadCargoTomlDependencyTables(t *testing.T) {
	dir := t.TempDir()
	err := os.WriteFile(dir+"/Cargo.toml", []byte(`[dependencies.log]
version = "0.4"

[dev-dependencies.assertions]
package = "pretty_assertions"
version = "1.4"

[target.'cfg(windows)'.dependencies.windows-sys]
version = "0.59"

[dependencies.local]
version = "1.0"
path = "../local"
`), 0644)
	if err != nil {
		t.Fatalf("write Cargo.toml: %v", err)
	}

	packages := readCargoToml(dir)
	set := manifestSet(packages)
	want := []string{"log@^0.4", "pretty_assertions@^1.4", "windows-sys@^0.59"}
	for _, spec := range want {
		if !set[spec] {
			t.Errorf("missing %q from %v", spec, packages)
		}
	}
	if len(packages) != len(want) {
		t.Errorf("expected %d packages, got %d: %v", len(want), len(packages), packages)
	}
}

func TestCargoDependencySpecMarksImplicitCaret(t *testing.T) {
	spec := cargoDependencySpec("serde", `"1.0.217"`)
	if spec != "serde@^1.0.217" {
		t.Errorf("expected Cargo's implicit caret requirement, got %q", spec)
	}
}

func TestReadCargoTomlDottedVersion(t *testing.T) {
	dir := t.TempDir()
	manifest := "[dependencies]\nserde.version = \"1.0\"\n"
	if err := os.WriteFile(dir+"/Cargo.toml", []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	packages := readCargoToml(dir)
	if len(packages) != 1 || packages[0] != "serde@^1.0" {
		t.Fatalf("unexpected dotted dependency: %v", packages)
	}
}

func manifestSet(packages []string) map[string]bool {
	set := make(map[string]bool, len(packages))
	for _, spec := range packages {
		set[spec] = true
	}
	return set
}
