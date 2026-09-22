package manager

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestReadCargoFetchPackagesUsesAdjacentLockfile(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "Cargo.toml")
	writeCargoTestFile(t, manifestPath, "[dependencies]\nserde = \"1\"\n")
	lock := `[[package]]
name = "serde"
version = "1.0.217"
source = "registry+https://index.crates.io/"
`
	writeCargoTestFile(t, filepath.Join(dir, "Cargo.lock"), lock)

	packages, err := ReadCargoFetchPackages(manifestPath)
	if err != nil {
		t.Fatalf("read Cargo project: %v", err)
	}
	unexpectedPackages := len(packages) != 1 || packages[0] != "serde@1.0.217"
	if unexpectedPackages {
		t.Fatalf("unexpected packages: %v", packages)
	}
}

const testReadCargoFetchPackagesUsesWorkspaceLockfileFixture = `[workspace]
members = ["member"]
[workspace.dependencies]
serde = "1"
`

const testReadCargoFetchPackagesUsesWorkspaceLockfileFixture2 = `[dependencies]
serde = { workspace = true }
`

const testReadCargoFetchPackagesUsesWorkspaceLockfileFixture3 = `[[package]]
name = "serde"
version = "1.0.217"
source = "sparse+https://index.crates.io/"
`

func TestReadCargoFetchPackagesUsesWorkspaceLockfile(t *testing.T) {
	root := testReadCargoFetchPackagesUsesWorkspaceLockfileFixture
	member := testReadCargoFetchPackagesUsesWorkspaceLockfileFixture2
	rootPath, memberPath := writeCargoWorkspace(t, root, member)
	rootDir := filepath.Dir(rootPath)
	lockPath := filepath.Join(rootDir, "Cargo.lock")
	lock := testReadCargoFetchPackagesUsesWorkspaceLockfileFixture3
	writeCargoTestFile(t, lockPath, lock)

	packages, err := ReadCargoFetchPackages(memberPath)
	if err != nil {
		t.Fatalf("read workspace: %v", err)
	}
	want := []string{"serde@1.0.217"}
	if !slices.Equal(packages, want) {
		t.Fatalf("expected %v, got %v", want, packages)
	}
}

func TestReadCargoFetchPackagesRequiresWorkspaceLockfile(t *testing.T) {
	root := `[workspace]
members = ["member"]
`
	member := `[dependencies]
serde = "1"
`
	_, memberPath := writeCargoWorkspace(t, root, member)

	_, err := ReadCargoFetchPackages(memberPath)
	unexpectedError := err == nil || !strings.Contains(err.Error(), "Cargo.lock is required")
	if unexpectedError {
		t.Fatalf("expected workspace lockfile error, got %v", err)
	}
}

func TestReadCargoFetchPackagesBlocksExternalLockSource(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "Cargo.toml")
	writeCargoTestFile(t, manifestPath, "[dependencies]\n")
	lock := `[[package]]
name = "private"
version = "1.0.0"
source = "git+https://example.com/private"
`
	writeCargoTestFile(t, filepath.Join(dir, "Cargo.lock"), lock)

	_, err := ReadCargoFetchPackages(manifestPath)
	unexpectedError := err == nil || !strings.Contains(err.Error(), "unsupported Cargo source")
	if unexpectedError {
		t.Fatalf("expected unsupported source error, got %v", err)
	}
}

func TestReadCargoFetchPackagesParsesCompactLockFields(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "Cargo.toml")
	writeCargoTestFile(t, manifestPath, "[dependencies]\nserde = \"1\"\n")
	lock := `[[ package ]]
name="serde"
version="1.0.217"
source="sparse+https://index.crates.io/"
`
	writeCargoTestFile(t, filepath.Join(dir, "Cargo.lock"), lock)

	packages, err := ReadCargoFetchPackages(manifestPath)
	if err != nil {
		t.Fatalf("read compact Cargo.lock: %v", err)
	}
	want := []string{"serde@1.0.217"}
	if !slices.Equal(packages, want) {
		t.Fatalf("expected %v, got %v", want, packages)
	}
}

func TestReadCargoFetchPackagesBlocksCompactExternalSource(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "Cargo.toml")
	writeCargoTestFile(t, manifestPath, "[dependencies]\n")
	lock := `[[package]]
name="private"
version="1.0.0"
source="git+https://example.com/private"
`
	writeCargoTestFile(t, filepath.Join(dir, "Cargo.lock"), lock)

	_, err := ReadCargoFetchPackages(manifestPath)
	unexpectedError := err == nil || !strings.Contains(err.Error(), "unsupported Cargo source")
	if unexpectedError {
		t.Fatalf("expected compact source error, got %v", err)
	}
}

func TestReadCargoFetchPackagesBlocksManifestSourceWithLock(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "Cargo.toml")
	manifest := "[dependencies]\nlocal = { path = \"../local\" }\n"
	writeCargoTestFile(t, manifestPath, manifest)
	lock := `[[package]]
name = "serde"
version = "1.0.217"
source = "sparse+https://index.crates.io/"
`
	writeCargoTestFile(t, filepath.Join(dir, "Cargo.lock"), lock)

	_, err := ReadCargoFetchPackages(manifestPath)
	unexpectedError := err == nil || !strings.Contains(err.Error(), "unsupported Cargo source")
	if unexpectedError {
		t.Fatalf("expected manifest source error, got %v", err)
	}
}

func TestReadCargoFetchPackagesReturnsLockScannerError(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "Cargo.toml")
	writeCargoTestFile(t, manifestPath, "[dependencies]\n")
	longLine := "#" + strings.Repeat("x", 70_000)
	writeCargoTestFile(t, filepath.Join(dir, "Cargo.lock"), longLine)

	_, err := ReadCargoFetchPackages(manifestPath)
	unexpectedError := err == nil || !strings.Contains(err.Error(), "token too long")
	if unexpectedError {
		t.Fatalf("expected scanner error, got %v", err)
	}
}

func TestReadCargoUpdatePackagesFiltersTargetRequirement(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "Cargo.toml")
	manifest := `[dependencies]
serde = "1.0"
regex = "1.11"
`
	writeCargoTestFile(t, manifestPath, manifest)

	packages, err := ReadCargoUpdatePackages(manifestPath, "serde")
	if err != nil {
		t.Fatalf("read Cargo manifest: %v", err)
	}
	unexpectedPackages := len(packages) != 1 || packages[0] != "serde@^1.0"
	if unexpectedPackages {
		t.Fatalf("unexpected packages: %v", packages)
	}
}

func TestReadCargoUpdatePackagesRejectsUnknownTarget(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "Cargo.toml")
	writeCargoTestFile(t, manifestPath, "[dependencies]\nserde = \"1\"\n")

	_, err := ReadCargoUpdatePackages(manifestPath, "transitive-only")
	unexpectedError := err == nil || !strings.Contains(err.Error(), "not a direct dependency")
	if unexpectedError {
		t.Fatalf("expected unknown target error, got %v", err)
	}
}

func TestReadCargoUpdatePackagesBlocksExternalManifestSource(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "Cargo.toml")
	manifest := `[dependencies]
serde = "1"
private = { git = "https://example.com/private", version = "1" }
`
	writeCargoTestFile(t, manifestPath, manifest)

	_, err := ReadCargoUpdatePackages(manifestPath, "")
	unexpectedError := err == nil || !strings.Contains(err.Error(), "unsupported Cargo source")
	if unexpectedError {
		t.Fatalf("expected unsupported source error, got %v", err)
	}
}

func TestReadCargoUpdatePackagesBlocksQuotedExternalSourceKey(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "Cargo.toml")
	manifest := `[dependencies]
private = { version = "1", "git" = "https://example.com/private" }
`
	writeCargoTestFile(t, manifestPath, manifest)

	_, err := ReadCargoUpdatePackages(manifestPath, "")
	unexpectedError := err == nil || !strings.Contains(err.Error(), "unsupported Cargo source")
	if unexpectedError {
		t.Fatalf("expected quoted source key error, got %v", err)
	}
}

func TestReadCargoUpdatePackagesBlocksEscapedQuotedSourceKey(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "Cargo.toml")
	manifest := `[dependencies]
private = { version = "1", "g\u0069t" = "https://example.com/private" }
`
	writeCargoTestFile(t, manifestPath, manifest)

	_, err := ReadCargoUpdatePackages(manifestPath, "")
	unexpectedError := err == nil || !strings.Contains(err.Error(), "unsupported Cargo source")
	if unexpectedError {
		t.Fatalf("expected escaped source key error, got %v", err)
	}
}

func TestReadCargoUpdatePackagesBlocksQuotedTableSourceKey(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "Cargo.toml")
	manifest := `[dependencies.private]
version = "1"
"path" = "../private"
`
	writeCargoTestFile(t, manifestPath, manifest)

	_, err := ReadCargoUpdatePackages(manifestPath, "")
	unexpectedError := err == nil || !strings.Contains(err.Error(), "unsupported Cargo source")
	if unexpectedError {
		t.Fatalf("expected quoted table source key error, got %v", err)
	}
}

const testReadCargoUpdatePackagesAllowsCratesIORegistryFixture = `[dependencies]
serde = { version = "1", registry = "crates-io" }

[dependencies.regex]
version = "1.11"
"registry" = "crates-io"
`

func TestReadCargoUpdatePackagesAllowsCratesIORegistry(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "Cargo.toml")
	manifest := testReadCargoUpdatePackagesAllowsCratesIORegistryFixture
	writeCargoTestFile(t, manifestPath, manifest)

	packages, err := ReadCargoUpdatePackages(manifestPath, "")
	if err != nil {
		t.Fatalf("expected crates.io registry to pass, got %v", err)
	}
	want := []string{"serde@^1", "regex@^1.11"}
	if !slices.Equal(packages, want) {
		t.Fatalf("expected %v, got %v", want, packages)
	}
}

func TestReadCargoUpdatePackagesBlocksCustomQuotedRegistry(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "Cargo.toml")
	manifest := `[dependencies]
private = { version = "1", "registry" = "internal" }
`
	writeCargoTestFile(t, manifestPath, manifest)

	_, err := ReadCargoUpdatePackages(manifestPath, "")
	unexpectedError := err == nil || !strings.Contains(err.Error(), "unsupported Cargo source")
	if unexpectedError {
		t.Fatalf("expected custom registry error, got %v", err)
	}
}

func TestReadCargoUpdatePackagesBlocksPatchedExternalSource(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "Cargo.toml")
	manifest := `[dependencies]
serde = "1"

[patch.crates-io]
serde = { git = "https://example.com/serde" }
`
	writeCargoTestFile(t, manifestPath, manifest)

	_, err := ReadCargoUpdatePackages(manifestPath, "serde")
	unexpectedError := err == nil || !strings.Contains(err.Error(), "unsupported Cargo source")
	if unexpectedError {
		t.Fatalf("expected patched source error, got %v", err)
	}
}

func TestReadCargoUpdatePackagesBlocksRegistryPatch(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "Cargo.toml")
	manifest := `[dependencies]
serde = "1"

[patch]
crates-io.serde = "1.0.200"
`
	writeCargoTestFile(t, manifestPath, manifest)

	_, err := ReadCargoUpdatePackages(manifestPath, "serde")
	unexpectedError := err == nil || !strings.Contains(err.Error(), "unsupported Cargo source")
	if unexpectedError {
		t.Fatalf("expected registry patch error, got %v", err)
	}
}

func TestReadCargoUpdatePackagesBlocksDottedExternalSource(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "Cargo.toml")
	manifest := "[dependencies]\nlocal.path = \"../local\"\n"
	writeCargoTestFile(t, manifestPath, manifest)

	_, err := ReadCargoUpdatePackages(manifestPath, "")
	unexpectedError := err == nil || !strings.Contains(err.Error(), "unsupported Cargo source")
	if unexpectedError {
		t.Fatalf("expected dotted source error, got %v", err)
	}
}

func TestReadCargoUpdatePackagesBlocksRootDottedDependencies(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "Cargo.toml")
	manifest := "dependencies.serde = \"1\"\n"
	writeCargoTestFile(t, manifestPath, manifest)

	_, err := ReadCargoUpdatePackages(manifestPath, "")
	unexpectedError := err == nil || !strings.Contains(err.Error(), "unsupported Cargo manifest syntax")
	if unexpectedError {
		t.Fatalf("expected dotted manifest error, got %v", err)
	}
}

func TestReadCargoUpdatePackagesBlocksRootDottedPatch(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "Cargo.toml")
	manifest := "patch.crates-io.serde = { path = \"../serde\" }\n"
	writeCargoTestFile(t, manifestPath, manifest)

	_, err := ReadCargoUpdatePackages(manifestPath, "")
	unexpectedError := err == nil || !strings.Contains(err.Error(), "unsupported Cargo manifest syntax")
	if unexpectedError {
		t.Fatalf("expected dotted patch error, got %v", err)
	}
}

func TestReadCargoUpdatePackagesAllowsSourceWordCrateNames(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "Cargo.toml")
	manifest := "[dependencies]\ngit = \"1\"\npath = \"2\"\nregistry = \"3\"\n"
	writeCargoTestFile(t, manifestPath, manifest)

	packages, err := ReadCargoUpdatePackages(manifestPath, "")
	if err != nil {
		t.Fatalf("read Cargo dependencies: %v", err)
	}
	if len(packages) != 3 {
		t.Fatalf("expected source-word crates, got %v", packages)
	}
}

func TestReadCargoUpdatePackagesBlocksInheritedWorkspaceDependency(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "Cargo.toml")
	manifest := `[dependencies]
serde = { workspace = true }
`
	writeCargoTestFile(t, manifestPath, manifest)

	_, err := ReadCargoUpdatePackages(manifestPath, "")
	unexpectedError := err == nil || !strings.Contains(err.Error(), "inherited Cargo dependency")
	if unexpectedError {
		t.Fatalf("expected inherited dependency error, got %v", err)
	}
}

const testReadCargoUpdatePackagesResolvesInheritedWorkspaceDependencyFixture = `[workspace]
members = ["member"]

[workspace.dependencies]
serde = "1.0"
renamed = { package = "regex", version = "1.11" }
`

const testReadCargoUpdatePackagesResolvesInheritedWorkspaceDependencyFixture2 = `[dependencies]
serde = { workspace = true }
renamed.workspace = true
`

func TestReadCargoUpdatePackagesResolvesInheritedWorkspaceDependency(t *testing.T) {
	root := testReadCargoUpdatePackagesResolvesInheritedWorkspaceDependencyFixture
	member := testReadCargoUpdatePackagesResolvesInheritedWorkspaceDependencyFixture2
	_, memberPath := writeCargoWorkspace(t, root, member)
	packages, err := ReadCargoUpdatePackages(memberPath, "")
	if err != nil {
		t.Fatalf("read workspace dependencies: %v", err)
	}
	want := []string{"serde@^1.0", "regex@^1.11"}
	if !slices.Equal(packages, want) {
		t.Fatalf("expected %v, got %v", want, packages)
	}

	selected, err := ReadCargoUpdatePackages(memberPath, "regex")
	selectedWant := []string{"regex@^1.11"}
	unexpectedError := err != nil || !slices.Equal(selected, selectedWant)
	if unexpectedError {
		t.Fatalf("expected %v, got %v, %v", selectedWant, selected, err)
	}
}

func TestReadCargoUpdatePackagesBlocksWorkspaceSourceOverride(t *testing.T) {
	root := `[workspace]
members = ["member"]
[patch.crates-io]
serde = { git = "https://example.com/serde" }
`
	member := "[dependencies]\nserde = \"1\"\n"
	_, memberPath := writeCargoWorkspace(t, root, member)

	_, err := ReadCargoUpdatePackages(memberPath, "")
	unexpectedError := err == nil || !strings.Contains(err.Error(), "unsupported Cargo source")
	if unexpectedError {
		t.Fatalf("expected workspace source error, got %v", err)
	}
}

func TestReadCargoUpdatePackagesIncludesWorkspaceMembers(t *testing.T) {
	root := `[workspace]
members = [
  "member",
]
`
	member := "[dependencies]\nserde = \"1\"\n"
	rootPath, _ := writeCargoWorkspace(t, root, member)

	packages, err := ReadCargoUpdatePackages(rootPath, "")
	if err != nil {
		t.Fatalf("read workspace root: %v", err)
	}
	if !slices.Equal(packages, []string{"serde@^1"}) {
		t.Fatalf("expected member dependency, got %v", packages)
	}
}

func TestReadCargoUpdatePackagesBlocksWorkspaceMemberSource(t *testing.T) {
	root := `[workspace]
members = ["member"]
`
	member := `[dependencies]
private = { version = "1", git = "https://example.com/private" }
`
	rootPath, _ := writeCargoWorkspace(t, root, member)

	_, err := ReadCargoUpdatePackages(rootPath, "")
	unexpectedError := err == nil || !strings.Contains(err.Error(), "unsupported Cargo source")
	if unexpectedError {
		t.Fatalf("expected member source error, got %v", err)
	}
}

const testReadCargoUpdatePackagesHonorsWorkspaceExcludeFixture = `[workspace]
members = ["member", "excluded"]
exclude = ["excluded"]
`

func TestReadCargoUpdatePackagesHonorsWorkspaceExclude(t *testing.T) {
	root := testReadCargoUpdatePackagesHonorsWorkspaceExcludeFixture
	rootPath, _ := writeCargoWorkspace(t, root, "[dependencies]\nserde = \"1\"\n")
	excludedDir := filepath.Join(filepath.Dir(rootPath), "excluded")
	if err := os.MkdirAll(excludedDir, 0o755); err != nil {
		t.Fatal(err)
	}
	excluded := "[dependencies]\nprivate = { git = \"https://example.com/private\" }\n"
	writeCargoTestFile(t, filepath.Join(excludedDir, "Cargo.toml"), excluded)

	packages, err := ReadCargoUpdatePackages(rootPath, "")
	if err != nil {
		t.Fatalf("read workspace with exclusion: %v", err)
	}
	if !slices.Equal(packages, []string{"serde@^1"}) {
		t.Fatalf("unexpected workspace packages: %v", packages)
	}
}

func TestDiscoverCargoManifestSearchesParents(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "src", "nested")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(dir, "Cargo.toml")
	writeCargoTestFile(t, manifestPath, "[dependencies]\nserde = \"1\"\n")

	got, err := DiscoverCargoManifest(filepath.Join(nested, "Cargo.toml"))
	unexpectedError := err != nil || got != manifestPath
	if unexpectedError {
		t.Fatalf("DiscoverCargoManifest() = %q, %v; want %q, nil", got, err, manifestPath)
	}
}

func TestReadCargoUpdatePackagesReturnsScannerError(t *testing.T) {
	dir := t.TempDir()
	manifestPath := filepath.Join(dir, "Cargo.toml")
	longLine := "#" + strings.Repeat("x", 70_000)
	writeCargoTestFile(t, manifestPath, longLine)

	_, err := ReadCargoUpdatePackages(manifestPath, "")
	unexpectedError := err == nil || !strings.Contains(err.Error(), "token too long")
	if unexpectedError {
		t.Fatalf("expected scanner error, got %v", err)
	}
}

func writeCargoTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func writeCargoWorkspace(t *testing.T, root, member string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	memberDir := filepath.Join(dir, "member")
	if err := os.MkdirAll(memberDir, 0o755); err != nil {
		t.Fatal(err)
	}
	rootPath := filepath.Join(dir, "Cargo.toml")
	memberPath := filepath.Join(memberDir, "Cargo.toml")
	writeCargoTestFile(t, rootPath, root)
	writeCargoTestFile(t, memberPath, member)
	return rootPath, memberPath
}
