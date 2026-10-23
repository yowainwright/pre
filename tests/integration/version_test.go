//go:build integration

package integration

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/yowainwright/pre/internal/manager"
)

const brewIntegrationFormula = `class PreIntegration < Formula
  url "https://example.invalid/pre-integration-2.0.0.tar.gz"
  version "2.0.0"
  sha256 "0000000000000000000000000000000000000000000000000000000000000000"
end
`

const brewIntegrationNextFormula = `class PreIntegrationNext < Formula
  url "https://example.invalid/pre-integration-next-3.0.0.tar.gz"
  version "3.0.0"
  sha256 "0000000000000000000000000000000000000000000000000000000000000000"
end
`

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

func TestCratesIOVersionResolution(t *testing.T) {
	mgr := &manager.Manager{Name: "cargo", Ecosystem: "crates.io", InstallCmds: []string{"add"}}
	version, err := manager.ResolveVersion(mgr, "serde@^1.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.HasPrefix(version, "1.") {
		t.Errorf("expected a serde 1.x version, got %q", version)
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

func TestBrewUpgradeDiscoveryOutdated(t *testing.T) {
	prefix := setupBrewIntegration(t)
	writeBrewInstalledFixture(t, prefix, "pre-integration", "1.0.0")
	want := []string{"pre-integration@@2.0.0"}
	assertBrewUpgradeTargets(t, nil, want)
	flags := []string{"--formula", "--formulae", "--greedy", "--greedy-latest", "--greedy-auto-updates", "--fetch-HEAD", "--verbose"}
	for _, flag := range flags {
		t.Run(flag, func(t *testing.T) { assertBrewUpgradeTargets(t, []string{flag}, want) })
	}
	for _, flag := range []string{"--cask", "--casks"} {
		t.Run(flag, func(t *testing.T) { assertBrewUpgradeTargets(t, []string{flag}, nil) })
	}
}

func TestBrewUpgradeDiscoveryEmpty(t *testing.T) {
	prefix := setupBrewIntegration(t)
	writeBrewInstalledFixture(t, prefix, "pre-integration", "2.0.0")
	assertBrewUpgradeTargets(t, nil, nil)
	for _, flag := range []string{"--formula", "--cask", "--greedy"} {
		t.Run(flag, func(t *testing.T) { assertBrewUpgradeTargets(t, []string{flag}, nil) })
	}
}

func TestBrewUpgradeDiscoveryAliasRetarget(t *testing.T) {
	setupBrewAliasIntegration(t, "pre-integration-next")
	want := []string{"pre-integration-next@@3.0.0"}
	assertBrewUpgradeTargets(t, nil, want)
	assertBrewUpgradeTargets(t, []string{"--formula"}, want)
}

func TestBrewUpgradeDiscoveryAliasUnchanged(t *testing.T) {
	setupBrewAliasIntegration(t, "pre-integration")
	want := []string{"pre-integration@@2.0.0"}
	assertBrewUpgradeTargets(t, nil, want)
}

func TestBrewUpgradeDiscoveryAliasAmbiguousVersionBlocks(t *testing.T) {
	for _, version := range []string{"3.0@@1", "@3.0.0"} {
		t.Run(version, func(t *testing.T) {
			prefix := setupBrewAliasIntegration(t, "pre-integration-next")
			formula := filepath.Join(prefix, "Library", "Taps", "homebrew", "homebrew-core", "Formula", "pre-integration-next.rb")
			body := strings.ReplaceAll(brewIntegrationNextFormula, "3.0.0", version)
			writeBrewTestFile(t, formula, []byte(body), 0600)
			if _, err := manager.BrewUpgradePackages(nil); err == nil {
				t.Fatal("expected ambiguous alias target identity to block upgrade")
			}
		})
	}
}

func TestBrewUpgradeDiscoveryAliasTargetAlreadyInstalled(t *testing.T) {
	prefix := setupBrewAliasIntegration(t, "pre-integration-next")
	writeBrewInstalledFixture(t, prefix, "pre-integration-next", "3.0.0")
	want := []string{"pre-integration@@2.0.0"}
	assertBrewUpgradeTargets(t, nil, want)
}

func TestBrewUpgradeDiscoveryPinnedAlias(t *testing.T) {
	prefix := setupBrewAliasIntegration(t, "pre-integration-next")
	pinned := filepath.Join(prefix, "var", "homebrew", "pinned")
	if err := os.MkdirAll(pinned, 0700); err != nil {
		t.Fatal(err)
	}
	keg := filepath.Join(prefix, "Cellar", "pre-integration", "1.0.0")
	if err := os.Symlink(keg, filepath.Join(pinned, "pre-integration")); err != nil {
		t.Fatal(err)
	}
	assertBrewUpgradeTargets(t, nil, nil)
}

func assertBrewUpgradeTargets(t *testing.T, args, want []string) {
	t.Helper()
	got, err := manager.BrewUpgradePackages(args)
	if err != nil {
		t.Fatalf("real Homebrew discovery failed: %v", err)
	}
	if !slices.Equal(got, want) {
		t.Fatalf("upgrade targets = %v, want %v", got, want)
	}
}

func setupBrewAliasIntegration(t *testing.T, targetName string) string {
	t.Helper()
	prefix := setupBrewIntegration(t)
	writeBrewInstalledFixture(t, prefix, "pre-integration", "1.0.0")
	tap := filepath.Join(prefix, "Library", "Taps", "homebrew", "homebrew-core")
	nextFormula := filepath.Join(tap, "Formula", "pre-integration-next.rb")
	writeBrewTestFile(t, nextFormula, []byte(brewIntegrationNextFormula), 0600)
	aliases := filepath.Join(tap, "Aliases")
	if err := os.MkdirAll(aliases, 0700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(aliases, "pre-alias")
	target := filepath.Join(tap, "Formula", targetName+".rb")
	if err := os.Symlink(target, alias); err != nil {
		t.Fatal(err)
	}
	writeBrewAliasReceipt(t, prefix, alias)
	return prefix
}

func setupBrewIntegration(t *testing.T) string {
	t.Helper()
	brew, err := exec.LookPath("brew")
	if err != nil {
		if runtime.GOOS == "darwin" {
			t.Fatal("Homebrew is required for macOS integration tests")
		}
		t.Skip("Homebrew not available")
	}
	prefix := brewIntegrationPrefix(t)
	installBrewTestLauncher(t, brew, prefix)
	configureBrewIntegration(t, prefix)
	formula := filepath.Join(prefix, "Library", "Taps", "homebrew", "homebrew-core", "Formula", "pre-integration.rb")
	writeBrewTestFile(t, formula, []byte(brewIntegrationFormula), 0600)
	return prefix
}

func brewIntegrationPrefix(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", "tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root, 0700); err != nil {
		t.Fatal(err)
	}
	prefix, err := os.MkdirTemp(root, ".brew-integration-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(prefix); err != nil {
			t.Error(err)
		}
	})
	return prefix
}

func installBrewTestLauncher(t *testing.T, brew, prefix string) {
	t.Helper()
	source, err := filepath.EvalSymlinks(brew)
	if err != nil {
		t.Fatal(err)
	}
	launcher, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	writeBrewTestFile(t, filepath.Join(prefix, "bin", "brew"), launcher, 0700)
	library := filepath.Join(prefix, "Library")
	if err := os.MkdirAll(library, 0700); err != nil {
		t.Fatal(err)
	}
	repository := filepath.Dir(filepath.Dir(source))
	if err := os.Symlink(filepath.Join(repository, "Library", "Homebrew"), filepath.Join(library, "Homebrew")); err != nil {
		t.Fatal(err)
	}
}

func configureBrewIntegration(t *testing.T, prefix string) {
	t.Helper()
	for _, name := range []string{"HOMEBREW_NO_AUTO_UPDATE", "HOMEBREW_NO_ANALYTICS", "HOMEBREW_NO_INSTALL_FROM_API"} {
		t.Setenv(name, "1")
	}
	t.Setenv("HOMEBREW_CACHE", filepath.Join(prefix, "cache"))
	t.Setenv("HOMEBREW_LOGS", filepath.Join(prefix, "logs"))
	t.Setenv("HOMEBREW_TEMP", filepath.Join(prefix, "tmp"))
	t.Setenv("PRE_EXEC_MANAGER", "brew")
	t.Setenv("PRE_EXEC_PATH", filepath.Join(prefix, "bin", "brew"))
}

func writeBrewAliasReceipt(t *testing.T, prefix, alias string) {
	t.Helper()
	source := map[string]string{"tap": "homebrew/core", "spec": "stable", "path": alias}
	data := map[string]any{"source": source}
	receipt, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(prefix, "Cellar", "pre-integration", "1.0.0", "INSTALL_RECEIPT.json")
	writeBrewTestFile(t, path, receipt, 0600)
}

func writeBrewInstalledFixture(t *testing.T, prefix, name, version string) {
	t.Helper()
	keg := filepath.Join(prefix, "Cellar", name, version)
	receipt := []byte(`{"source":{"tap":"homebrew/core","spec":"stable"}}`)
	writeBrewTestFile(t, filepath.Join(keg, "INSTALL_RECEIPT.json"), receipt, 0600)
	writeBrewTestFile(t, filepath.Join(keg, "bin", name), nil, 0600)
	opt := filepath.Join(prefix, "opt")
	if err := os.MkdirAll(opt, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(keg, filepath.Join(opt, name)); err != nil {
		t.Fatal(err)
	}
}

func writeBrewTestFile(t *testing.T, path string, data []byte, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, mode); err != nil {
		t.Fatal(err)
	}
}
