package manager

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestValidateManifestRejectsInvalidPackageLock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "package-lock.json")
	if err := os.WriteFile(path, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := ValidateManifest(&Manager{Name: "npm", Ecosystem: "npm"}, dir)
	if err == nil || !strings.Contains(err.Error(), "package-lock.json") {
		t.Fatalf("expected package lock error, got %v", err)
	}
}

func TestValidateManifestRejectsInvalidBunLock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bun.lock")
	if err := os.WriteFile(path, []byte("# Bun lock\nnot json"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := ValidateManifest(&Manager{Name: "bun", Ecosystem: "npm"}, dir)
	if err == nil || !strings.Contains(err.Error(), "bun.lock") {
		t.Fatalf("expected Bun lock error, got %v", err)
	}
}

func TestValidateManifestAllowsBunJSONC(t *testing.T) {
	dir := t.TempDir()
	lock := `# Bun Lockfile v1
{
  "lockfileVersion": 1,
  "packages": {
    "react": ["react@18.2.0", {}],
  },
}
`
	if err := os.WriteFile(filepath.Join(dir, "bun.lock"), []byte(lock), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{}`), 0o644); err != nil {
		t.Fatal(err)
	}

	err := ValidateManifest(&Manager{Name: "bun", Ecosystem: "npm"}, dir)
	if err != nil {
		t.Fatalf("expected Bun JSONC to pass validation, got %v", err)
	}
}

func TestValidateManifestRejectsUnsupportedNPMDependencySource(t *testing.T) {
	specs := []string{
		"git+https://example.com/private.git",
		"file:../private",
		"link:../private",
		"npm:react@18",
		"workspace:*",
		"https://example.com/private.tgz",
		"catalog:default",
	}
	for _, spec := range specs {
		t.Run(spec, func(t *testing.T) {
			dir := t.TempDir()
			manifest := fmt.Sprintf(`{"dependencies":{"private":%q}}`, spec)
			path := filepath.Join(dir, "package.json")
			if err := os.WriteFile(path, []byte(manifest), 0o644); err != nil {
				t.Fatal(err)
			}

			err := ValidateManifest(&Manager{Name: "npm", Ecosystem: "npm"}, dir)
			if err == nil || !strings.Contains(err.Error(), "unsupported npm dependency source") {
				t.Fatalf("expected unsupported npm source error, got %v", err)
			}
		})
	}
}

func TestValidateManifestAllowsNPMRegistryDependency(t *testing.T) {
	dir := t.TempDir()
	manifest := `{"dependencies":{"react":"^18.2.0"}}`
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}

	err := ValidateManifest(&Manager{Name: "npm", Ecosystem: "npm"}, dir)
	if err != nil {
		t.Fatalf("expected registry dependency to pass, got %v", err)
	}
}

func TestValidateManifestRejectsRequirementsIncludeError(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "requirements.txt")
	if err := os.WriteFile(path, []byte("-r missing.txt\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := ValidateManifest(&Manager{Name: "pip", Ecosystem: "PyPI"}, dir)
	if err == nil || !strings.Contains(err.Error(), "missing.txt") {
		t.Fatalf("expected requirements include error, got %v", err)
	}
}

func TestValidateManifestRejectsLongTextLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pnpm-lock.yaml")
	content := strings.Repeat("x", 70_000)
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	err := ValidateManifest(&Manager{Name: "pnpm", Ecosystem: "npm"}, dir)
	if err == nil || !strings.Contains(err.Error(), "token too long") {
		t.Fatalf("expected scanner error, got %v", err)
	}
}

func TestValidateManifestAllowsMissingProjectFiles(t *testing.T) {
	err := ValidateManifest(&Manager{Name: "npm", Ecosystem: "npm"}, t.TempDir())
	if err != nil {
		t.Fatalf("expected missing project files to pass, got %v", err)
	}
}

func TestValidateManifestRequiresUVLockForPyproject(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "pyproject.toml")
	if err := os.WriteFile(path, []byte("[project]\nname = \"demo\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	err := ValidateManifest(&Manager{Name: "uv", Ecosystem: "PyPI"}, dir)
	if err == nil || !strings.Contains(err.Error(), "uv.lock") {
		t.Fatalf("expected uv.lock requirement, got %v", err)
	}
}

func TestValidateManifestAllowsLockedPyproject(t *testing.T) {
	dir := t.TempDir()
	pyprojectPath := filepath.Join(dir, "pyproject.toml")
	lockPath := filepath.Join(dir, "poetry.lock")
	os.WriteFile(pyprojectPath, []byte("[tool.poetry]\nname = \"demo\"\n"), 0o644)
	os.WriteFile(lockPath, []byte(""), 0o644)

	err := ValidateManifest(&Manager{Name: "poetry", Ecosystem: "PyPI"}, dir)
	if err != nil {
		t.Fatalf("expected locked pyproject to pass, got %v", err)
	}
}
