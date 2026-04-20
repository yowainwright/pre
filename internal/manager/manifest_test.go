package manager

import (
	"os"
	"sort"
	"testing"
)

func TestReadPackageJSON(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/package.json", []byte(`{
		"dependencies": {"lodash": "^4.17.21", "react": "^18.0.0"},
		"devDependencies": {"typescript": "^5.0.0"}
	}`), 0644)

	names := readPackageJSON(dir)
	sort.Strings(names)
	if len(names) != 3 {
		t.Fatalf("expected 3 packages, got %d: %v", len(names), names)
	}
	if names[0] != "lodash@^4.17.21" || names[1] != "react@^18.0.0" || names[2] != "typescript@^5.0.0" {
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
	os.WriteFile(dir+"/requirements.txt", []byte(`# comment
requests==2.28.0
flask>=2.0
-r other.txt
`), 0644)

	names := readRequirementsTxt(dir)
	if len(names) != 2 {
		t.Fatalf("expected 2 packages, got %d: %v", len(names), names)
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

func TestReadPackageJSONSkipsLocalSpecs(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(dir+"/package.json", []byte(`{
		"dependencies": {
			"localpkg": "file:../localpkg",
			"workspacepkg": "workspace:*"
		}
	}`), 0644)

	names := readPackageJSON(dir)
	sort.Strings(names)
	if len(names) != 2 {
		t.Fatalf("expected 2 packages, got %d: %v", len(names), names)
	}
	if names[0] != "localpkg" || names[1] != "workspacepkg" {
		t.Errorf("expected unsupported specs to fall back to names, got %v", names)
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
