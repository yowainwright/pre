package manager

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"
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

func TestValidateManifestRejectsNPMShrinkwrap(t *testing.T) {
	original := runCmd
	t.Cleanup(func() { runCmd = original })
	runCmd = func(string, ...string) ([]byte, error) {
		t.Fatal("shrinkwrap must be rejected before npm runs")
		return nil, nil
	}
	tests := map[string][]string{
		"shrinkwrap only":   nil,
		"with manifest":     {"package.json"},
		"with package lock": {"package-lock.json"},
		"with both":         {"package.json", "package-lock.json"},
	}
	for name, files := range tests {
		t.Run(name, func(t *testing.T) {
			dir := npmShrinkwrapValidationDir(t, files)
			err := ValidateManifest(&Manager{Name: "npm", Ecosystem: "npm"}, dir)
			if err == nil {
				t.Fatal("expected npm-shrinkwrap.json to block validation")
			}
			if !strings.Contains(err.Error(), "npm-shrinkwrap.json") {
				t.Fatalf("expected shrinkwrap error, got %v", err)
			}
		})
	}
}

func npmShrinkwrapValidationDir(t *testing.T, files []string) string {
	t.Helper()
	dir := t.TempDir()
	files = append(files, "npm-shrinkwrap.json")
	for _, name := range files {
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(`{}`), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
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

func TestValidateManifestChecksOptionalDeclarationsBeforeNPM(t *testing.T) {
	original := runCmd
	t.Cleanup(func() { runCmd = original })
	tests := []struct {
		name, manifest, lockfile string
		wantError                bool
	}{
		{"changed", `{"optionalDependencies":{"is-number":"^6.0.0"}}`, `{"packages":{"":{"optionalDependencies":{"is-number":"^7.0.0"}}}}`, true},
		{"equivalent range", `{"optionalDependencies":{"is-number":">=7.0.0 <8.0.0"}}`, `{"packages":{"":{"optionalDependencies":{"is-number":"^7.0.0"}}}}`, true},
		{"added", `{"optionalDependencies":{"is-number":"^7.0.0"}}`, `{"packages":{"":{}}}`, true},
		{"removed", `{}`, `{"packages":{"":{"optionalDependencies":{"is-number":"^7.0.0"}}}}`, true},
		{"missing lock root", `{"optionalDependencies":{"is-number":"^7.0.0"}}`, `{"lockfileVersion":3,"packages":{}}`, true},
		{"unchanged", `{"optionalDependencies":{"is-number":"^7.0.0"}}`, `{"packages":{"":{"optionalDependencies":{"is-number":"^7.0.0"}}}}`, false},
		{"empty", `{"optionalDependencies":{}}`, `{"packages":{"":{}}}`, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assertNPMOptionalDeclarations(t, test.manifest, test.lockfile, test.wantError)
		})
	}
}

func assertNPMOptionalDeclarations(t *testing.T, manifest, lockfile string, wantError bool) {
	t.Helper()
	dir := t.TempDir()
	for name, content := range map[string]string{"package.json": manifest, "package-lock.json": lockfile} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	called := false
	runCmd = func(string, ...string) ([]byte, error) { called = true; return nil, nil }
	err := ValidateManifest(&Manager{Name: "npm", Ecosystem: "npm"}, dir)
	failed := err != nil
	if failed != wantError {
		t.Fatalf("expected validation error=%t, got %v", wantError, err)
	}
	if called == wantError {
		t.Fatalf("npm called=%t; changed optional declarations must fail before npm runs", called)
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

func TestValidateManifestLegacyOptionalLock(t *testing.T) {
	t.Setenv("npm_config_cache", t.TempDir())
	tests := []struct {
		requirement string
		wantError   bool
	}{
		{"^7.0.0", false},
		{">=7.0.0 <8.0.0", false},
		{"^6.0.0", true},
	}
	for _, test := range tests {
		t.Run(test.requirement, func(t *testing.T) {
			dir := npmLegacyOptionalLockDir(t, test.requirement)
			err := ValidateManifest(&Manager{Name: "npm", Ecosystem: "npm"}, dir)
			failed := err != nil
			if failed != test.wantError {
				t.Fatalf("expected error=%t, got %v", test.wantError, err)
			}
		})
	}
}

func npmLegacyOptionalLockDir(t *testing.T, requirement string) string {
	t.Helper()
	dir := npmLockValidationDir(t, "optionalDependencies", requirement)
	lockfile := `{"name":"demo","lockfileVersion":1,"requires":true,"dependencies":{"is-number":{"version":"7.0.0","resolved":"https://registry.npmjs.org/is-number/-/is-number-7.0.0.tgz","optional":true}}}`
	path := filepath.Join(dir, "package-lock.json")
	if err := os.WriteFile(path, []byte(lockfile), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestValidateNPMLegacyOptionalResultRejectsIncomplete(t *testing.T) {
	outputs := []string{
		`not json`, `null`, `{}`, `{"dependencies":{"is-number":{}}}`,
		`{"dependencies":{"is-number":{"version":"7.0.0","invalid":"^6.0.0"}}}`,
		`{"dependencies":{"is-number":{"version":"7.0.0","missing":true}}}`,
	}
	for _, output := range outputs {
		err := validateNPMLegacyOptionalResult([]byte(output), []string{"is-number"})
		if err == nil {
			t.Errorf("expected incomplete legacy validation to fail: %s", output)
		}
	}
}

func TestValidateManifestLegacyOptionalOverride(t *testing.T) {
	t.Setenv("npm_config_cache", t.TempDir())
	dir := t.TempDir()
	files := map[string]string{
		"package.json":      `{"name":"demo","optionalDependencies":{"is-odd":"3.0.1"},"overrides":{"is-number":"^7.0.0"}}`,
		"package-lock.json": `{"name":"demo","lockfileVersion":1,"requires":true,"dependencies":{"is-odd":{"version":"3.0.1","resolved":"https://registry.npmjs.org/is-odd/-/is-odd-3.0.1.tgz","requires":{"is-number":"^6.0.0"},"optional":true},"is-number":{"version":"6.0.0","resolved":"https://registry.npmjs.org/is-number/-/is-number-6.0.0.tgz","optional":true}}}`,
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	err := ValidateManifest(&Manager{Name: "npm", Ecosystem: "npm"}, dir)
	if err == nil {
		t.Fatal("expected stale transitive optional dependency override to fail validation")
	}
}

func TestValidateManifestLegacyOptionalLockMissingPackage(t *testing.T) {
	t.Setenv("npm_config_cache", t.TempDir())
	dir := npmLegacyOptionalLockDir(t, "^7.0.0")
	lockfile := `{"name":"demo","lockfileVersion":1,"requires":true,"dependencies":{}}`
	if err := os.WriteFile(filepath.Join(dir, "package-lock.json"), []byte(lockfile), 0o644); err != nil {
		t.Fatal(err)
	}
	err := ValidateManifest(&Manager{Name: "npm", Ecosystem: "npm"}, dir)
	if err == nil {
		t.Fatal("expected missing optional dependency in legacy lockfile to fail validation")
	}
}

func TestValidateManifestLegacyOptionalPeerOptions(t *testing.T) {
	t.Setenv("npm_config_cache", t.TempDir())
	dir := npmLegacyOptionalLockDir(t, "^7.0.0")
	manifest := `{"name":"demo","optionalDependencies":{"is-number":"^7.0.0"},"peerDependencies":{"missing-peer":"1.0.0"}}`
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	err := ValidateManifest(&Manager{Name: "npm", Ecosystem: "npm"}, dir, "install", "--legacy-peer-deps")
	if err != nil {
		t.Fatalf("expected legacy peer option to remain supported: %v", err)
	}
}

func TestNPMLockOptions(t *testing.T) {
	tests := []struct{ args, want []string }{
		{[]string{"install", "--legacy-peer-deps", "--omit", "dev"}, []string{"--legacy-peer-deps=true", "--omit=dev"}},
		{[]string{"--install-links=false", "install", "--force"}, []string{"--install-links=false", "--force=true"}},
		{[]string{"install", "--legacy-peer-deps", "false", "--no-strict-peer-deps"}, []string{"--legacy-peer-deps=false", "--no-strict-peer-deps=true"}},
		{[]string{"install", "-f=false", "-w", "app"}, []string{"--force=false", "--workspace=app"}},
		{[]string{"ci", "--help", "--versions", "--no-dry-run", "--ignore-scripts=false", "--no-offline"}, nil},
		{[]string{"install", "--", "--force"}, nil},
		{[]string{"install", "---", "--force"}, nil},
	}
	for _, test := range tests {
		got, err := npmLockOptions(test.args)
		matches := slices.Equal(got, test.want)
		unexpected := err != nil || !matches
		if unexpected {
			t.Errorf("npmLockOptions(%v) = %v, %v; want %v", test.args, got, err, test.want)
		}
	}
}

func TestNPMLockOptionsRejectMissingValue(t *testing.T) {
	_, err := npmLockOptions([]string{"install", "--omit", "--help"})
	if err == nil {
		t.Fatal("expected missing option value to fail validation")
	}
}

func TestNPMLockCheckRejectsOptionInjection(t *testing.T) {
	original := runCmd
	runCmd = func(string, ...string) ([]byte, error) {
		t.Error("unsafe options must be rejected before starting npm")
		return nil, nil
	}
	defer func() { runCmd = original }()
	flags := []string{"--legacy-peer-deps=--", "--legacy-peer-deps=---", "--legacy-peer-deps=--help", "--force=--ignore-scripts=false", "--omit=--", "--install-strategy=--versions"}
	dir := t.TempDir()
	for _, flag := range flags {
		err := runNPMLockCheck(dir, []string{"install", flag})
		if err == nil {
			t.Errorf("expected %s to fail before npm executes", flag)
		}
	}
}

func TestValidateManifestPreservesLegacyPeerDeps(t *testing.T) {
	dir := npmPeerConflictDir(t)
	mgr := &Manager{Name: "npm", Ecosystem: "npm"}
	if err := ValidateManifest(mgr, dir); err == nil {
		t.Fatal("expected conflicting peers to fail without legacy-peer-deps")
	}
	err := ValidateManifest(mgr, dir, "install", "--legacy-peer-deps")
	if err != nil {
		t.Fatalf("expected legacy-peer-deps to preserve the valid install, got %v", err)
	}
}

func npmPeerConflictDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	manifest := `{"name":"demo","dependencies":{"host":"1.0.0","plugin":"1.0.0"}}`
	lockfile := `{"name":"demo","lockfileVersion":3,"packages":{"":{"name":"demo","dependencies":{"host":"1.0.0","plugin":"1.0.0"}},"node_modules/host":{"version":"1.0.0","resolved":"https://registry.npmjs.org/host/-/host-1.0.0.tgz"},"node_modules/plugin":{"version":"1.0.0","resolved":"https://registry.npmjs.org/plugin/-/plugin-1.0.0.tgz","peerDependencies":{"host":"^2.0.0"}}}}`
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

func TestValidateManifestCannotSkipLockCheck(t *testing.T) {
	flags := []string{"--help", "--version", "--versions", "--usage"}
	for _, flag := range flags {
		t.Run(flag, func(t *testing.T) {
			dir := npmLockValidationDir(t, "dependencies", "^6.0.0")
			mgr := &Manager{Name: "npm", Ecosystem: "npm"}
			err := ValidateManifest(mgr, dir, "install", flag)
			if err == nil {
				t.Fatal("expected stale lockfile to fail despite informational flag")
			}
		})
	}
}

func TestValidateManifestDoesNotInstallOrRunScripts(t *testing.T) {
	dir := npmGuardValidationDir(t)
	mgr := &Manager{Name: "npm", Ecosystem: "npm"}
	err := ValidateManifest(mgr, dir, "install", "--no-dry-run", "--ignore-scripts=false", "--no-offline")
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"script-ran", "node_modules/is-number"} {
		_, err := os.Stat(filepath.Join(dir, name))
		if !os.IsNotExist(err) {
			t.Errorf("validation unexpectedly created %s: %v", name, err)
		}
	}
	_, err = os.Stat(filepath.Join(dir, "node_modules/keep"))
	if err != nil {
		t.Fatalf("validation removed an existing node_modules entry: %v", err)
	}
}

func npmGuardValidationDir(t *testing.T) string {
	t.Helper()
	dir := npmLockValidationDir(t, "dependencies", "^7.0.0")
	manifest := `{"name":"demo","dependencies":{"is-number":"^7.0.0"},"scripts":{"preinstall":"touch script-ran"}}`
	path := filepath.Join(dir, "package.json")
	err := os.WriteFile(path, []byte(manifest), 0o644)
	if err != nil {
		t.Fatal(err)
	}
	path = filepath.Join(dir, "node_modules/keep")
	err = os.MkdirAll(path, 0o755)
	if err != nil {
		t.Fatal(err)
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
