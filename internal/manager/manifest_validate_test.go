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

func TestValidateManifestRejectsUnsafePackageLockEntry(t *testing.T) {
	tests := map[string]string{
		"mismatched name":          `{"packages":{"node_modules/lodash":{"name":"evil-pkg","version":"4.17.21","resolved":"https://registry.npmjs.org/evil-pkg/-/evil-pkg-4.17.21.tgz"}}}`,
		"non-registry source":      `{"packages":{"node_modules/lodash":{"version":"4.17.21","resolved":"https://attacker.example/lodash.tgz"}}}`,
		"mismatched registry path": `{"packages":{"node_modules/lodash":{"version":"4.17.21","resolved":"https://registry.npmjs.org/evil-pkg/-/evil-pkg-4.17.21.tgz"}}}`,
	}
	for name, lockfile := range tests {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "package-lock.json")
			if err := os.WriteFile(path, []byte(lockfile), 0o644); err != nil {
				t.Fatal(err)
			}

			err := ValidateManifest(&Manager{Name: "npm", Ecosystem: "npm"}, dir)
			if err == nil || !strings.Contains(err.Error(), "package-lock.json") {
				t.Fatalf("expected unsafe package lock error, got %v", err)
			}
		})
	}
}

func TestValidateManifestAllowsRegistryPackageLockEntry(t *testing.T) {
	dir := t.TempDir()
	lockfile := `{"packages":{"node_modules/@scope/pkg":{"name":"@scope/pkg","version":"1.0.0","resolved":"https://registry.npmjs.org/@scope/pkg/-/pkg-1.0.0.tgz"}}}`
	if err := os.WriteFile(filepath.Join(dir, "package-lock.json"), []byte(lockfile), 0o644); err != nil {
		t.Fatal(err)
	}

	err := ValidateManifest(&Manager{Name: "npm", Ecosystem: "npm"}, dir)
	if err != nil {
		t.Fatalf("expected registry package lock to pass, got %v", err)
	}
}

func TestValidateManifestRejectsStalePackageLock(t *testing.T) {
	sections := []string{"dependencies", "devDependencies", "optionalDependencies"}
	for _, section := range sections {
		t.Run(section, func(t *testing.T) {
			dir := npmLockValidationDir(t, section, "^6.0.0")
			mgr := &Manager{Name: "npm", Ecosystem: "npm"}
			err := ValidateManifest(mgr, dir)
			if err == nil {
				t.Fatal("expected manifest requiring is-number 6.x to reject lockfile containing 7.0.0")
			}
		})
	}
}

func TestValidateManifestAllowsCompatiblePackageLock(t *testing.T) {
	requirements := []string{"^7.0.0", ">=7.0.0 <8.0.0"}
	for _, requirement := range requirements {
		t.Run(requirement, func(t *testing.T) {
			dir := npmLockValidationDir(t, "dependencies", requirement)
			mgr := &Manager{Name: "npm", Ecosystem: "npm"}
			err := ValidateManifest(mgr, dir)
			if err != nil {
				t.Fatalf("expected compatible lockfile to pass, got %v", err)
			}
		})
	}
}

func npmLockValidationDir(t *testing.T, section, requirement string) string {
	t.Helper()
	dir := t.TempDir()
	manifest := fmt.Sprintf(`{"name":"demo",%q:{"is-number":%q}}`, section, requirement)
	lockfile := fmt.Sprintf(`{"name":"demo","lockfileVersion":3,"packages":{"":{"name":"demo",%q:{"is-number":"^7.0.0"}},"node_modules/is-number":{"version":"7.0.0","resolved":"https://registry.npmjs.org/is-number/-/is-number-7.0.0.tgz"}}}`, section)
	files := map[string]string{"package.json": manifest, "package-lock.json": lockfile}
	for name, content := range files {
		path := filepath.Join(dir, name)
		data := []byte(content)
		err := os.WriteFile(path, data, 0o644)
		if err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestNPMRegistryURL(t *testing.T) {
	tests := map[string]struct {
		name     string
		expected bool
	}{
		"https://registry.npmjs.org/@scope/pkg/-/pkg-1.0.0.tgz":     {name: "@scope/pkg", expected: true},
		"https://registry.npmjs.org/%40scope%2Fpkg/-/pkg-1.0.0.tgz": {name: "@scope/pkg", expected: true},
		"https://registry.npmjs.org/evil/-/evil-1.0.0.tgz":          {name: "pkg", expected: false},
		"http://registry.npmjs.org/pkg/-/pkg-1.0.0.tgz":             {name: "pkg", expected: false},
		"https://registry.npmjs.org.evil.example/pkg.tgz":           {name: "pkg", expected: false},
		"https://user@registry.npmjs.org/pkg/-/pkg-1.0.0.tgz":       {name: "pkg", expected: false},
		"https://registry.example/pkg/-/pkg-1.0.0.tgz":              {name: "pkg", expected: false},
		"file:../pkg.tgz": {name: "pkg", expected: false},
	}
	for resolved, test := range tests {
		if actual := isNPMRegistryURL(resolved, test.name); actual != test.expected {
			t.Errorf("isNPMRegistryURL(%q, %q) = %t, want %t", resolved, test.name, actual, test.expected)
		}
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
