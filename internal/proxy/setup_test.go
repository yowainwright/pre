package proxy

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yowainwright/pre/internal/manager"
)

const globalOptionCommands = `
cargo +nightly --color always update
cargo +nightly build
npm --prefix app install react
pnpm --dir app add react
go -C app get example.com/mod
`

const globalOptionOutput = `pre:cargo +nightly --color always update
cargo:+nightly build
pre:npm --prefix app install react
pre:pnpm --dir app add react
pre:go -C app get example.com/mod
`

const shellAliasSetup = `
if [ -n "${BASH_VERSION:-}" ]; then shopt -s expand_aliases; fi
alias "brew='$1/brew' --cask"
alias pip='python3 -m pip'
alias npm='command npm'
`

const shellAliasCommands = `
brew install example-package
brew --version
PRE_DISABLE=1 brew install example-package
PRE_DISABLE=1 pip --version
PRE_DISABLE=1 npm --version
PRE_DISABLE=true pip install example-package
PRE_DISABLE=on npm install example-package
`

const shellAliasOutput = `pre:brew --cask install example-package
alias:--cask --version
alias:--cask install example-package
python3:-m pip --version
npm:--version
python3:-m pip install example-package
npm:install example-package
`

const shellRefreshPre = `#!/bin/sh
case "$1" in
  --version)
    printf 'version\n' >> "$PRE_TEST_LOG"
    printf '%s\n' "$PRE_TEST_VERSION"
    ;;
  shell-hook)
    [ "${PRE_TEST_FAIL:-0}" = 0 ] || exit 1
    printf 'PRE_TEST_LOADS=$((PRE_TEST_LOADS + 1))\n'
    /bin/cat "$PRE_TEST_HOOK"
    ;;
esac
`

const shellRefreshSetup = `
function _pre_test_prompt() { printf 'prompt:%s\n' "$?"; }
function _pre_test_after_prompt() { printf 'after-prompt:%s\n' "$?"; }
if [ -n "${ZSH_VERSION:-}" ]; then
  precmd_functions=(_pre_test_prompt)
elif [ "$1" = array ]; then
  PROMPT_COMMAND=(_pre_test_prompt)
else
  PROMPT_COMMAND=_pre_test_prompt
fi
export PRE_TEST_VERSION=old
PRE_TEST_LOADS=0
`

const shellRefreshCommands = `
case "$1" in
  prepended) PROMPT_COMMAND="_pre_test_prompt; _pre_refresh_hooks" ;;
  multiline) PROMPT_COMMAND=$'_pre_test_prompt\n  _pre_refresh_hooks  ' ;;
esac
false
_pre_refresh_hooks
printf 'initial:%s\n' "$?"
export PRE_TEST_VERSION=new
false
_pre_refresh_hooks
printf 'updated:%s:%s\n' "$?" "$_pre_hook_version"
_pre_refresh_hooks
printf 'loads:%s\n' "$PRE_TEST_LOADS"
export PRE_TEST_VERSION=broken PRE_TEST_FAIL=1
false
_pre_refresh_hooks
printf 'failed:%s:%s\n' "$?" "$_pre_hook_version"
unset PRE_TEST_FAIL
export PRE_TEST_VERSION=new
: > "$PRE_TEST_LOG"
if [ -n "${ZSH_VERSION:-}" ]; then
  precmd_functions+=(_pre_test_after_prompt)
  for _pre_prompt in "${precmd_functions[@]}"; do
    (exit 7)
    "$_pre_prompt" || break
  done
elif [ "$1" = array ]; then
  for _pre_prompt in "${PROMPT_COMMAND[@]}"; do
    (exit 7)
    eval "$_pre_prompt"
  done
else
  (exit 7)
  eval "$PROMPT_COMMAND"
fi
true
`

const shellRefreshOutput = `initial:1
updated:1:new
loads:1
failed:1:new
prompt:7
`

const shellRefreshCachedPath = `
export PRE_TEST_VERSION=new
if [ -n "${ZSH_VERSION:-}" ]; then
  hash pre="$1"
else
  hash -p "$1" pre
fi
command pre --version
false
_pre_refresh_hooks
printf 'status:%s\n' "$?"
command pre --version
`

const brewBackgroundUpdateFixture = `#!/bin/sh
if read -r input; then exit 99; fi
printf '%s:%s:%s\n' "$*" "$HOMEBREW_NO_ASK" "$NONINTERACTIVE" > "$PRE_UPDATE_LOG.tmp"
/bin/mv "$PRE_UPDATE_LOG.tmp" "$PRE_UPDATE_LOG"
while [ ! -e "$PRE_UPDATE_LOG.release" ]; do
  [ -d "${PRE_UPDATE_LOG%/*}" ] || exit 0
  /bin/sleep 0.01
done
printf 'done\n' > "$PRE_UPDATE_LOG.done"
`

func TestBuildShellHookContents(t *testing.T) {
	hook := ShellHook("old")
	if !strings.Contains(hook, "# pre security proxy") {
		t.Error("expected hook to contain '# pre security proxy'")
	}
	if !strings.Contains(hook, "# end pre security proxy") {
		t.Error("expected hook to contain '# end pre security proxy'")
	}
	for _, m := range manager.All() {
		if !strings.Contains(hook, m.Name) {
			t.Errorf("expected hook to contain manager name %q", m.Name)
		}
	}
}

func TestBuildShellHookIncludesDisableBypass(t *testing.T) {
	hook := ShellHook("old")
	if !strings.Contains(hook, "PRE_DISABLE") {
		t.Error("expected hook to include PRE_DISABLE bypass")
	}
	if !strings.Contains(hook, `command npm "$@"`) {
		t.Error("expected hook bypass to call the original package manager")
	}
}

func TestBuildShellHookIncludesNestedUVInstall(t *testing.T) {
	hook := ShellHook("old")
	condition := `[[ "$_pre_command" == "pip" && "$_pre_subcommand" == "install" ]]`
	if !strings.Contains(hook, condition) {
		t.Errorf("expected uv pip install condition, got:\n%s", hook)
	}
}

func TestBuildShellHookParsesCargoGlobalOptions(t *testing.T) {
	hook := ShellHook("old")
	markers := []string{"_pre_cargo_command", "+*", "--color|--config|--explain|--manifest-path|--target-dir|-C|-Z", "add|install|update|fetch"}
	for _, marker := range markers {
		if !strings.Contains(hook, marker) {
			t.Errorf("expected Cargo hook marker %q", marker)
		}
	}
}

func TestBuildShellHookParsesManagerGlobalOptions(t *testing.T) {
	hook := ShellHook("old")
	markers := []string{"_pre_command", "--prefix", "--dir", "-C"}
	for _, marker := range markers {
		hasMarker := strings.Contains(hook, marker)
		if !hasMarker {
			t.Errorf("expected manager hook marker %q", marker)
		}
	}
}

func TestBuildShellHookHasValidBashSyntax(t *testing.T) {
	cmd := exec.Command("bash", "-n")
	cmd.Stdin = strings.NewReader(ShellHook("old"))
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("invalid Bash hook: %v\n%s", err, output)
	}
}

func TestShellHookQuotesVersion(t *testing.T) {
	version := "dev'$(printf unsafe)"
	script := ShellHook(version) + "printf '%s\\n' \"$_pre_hook_version\"\n"
	cmd := exec.Command("bash", "-c", script)
	output, err := cmd.CombinedOutput()
	unexpectedOutput := err != nil || string(output) != version+"\n"
	if unexpectedOutput {
		t.Fatalf("version was not preserved literally: %q, %v", output, err)
	}
}

func TestCargoShellHookRoutesGlobalOptions(t *testing.T) {
	binDir := t.TempDir()
	prePath := filepath.Join(binDir, "pre")
	cargoPath := filepath.Join(binDir, "cargo")
	writeShellFixture(t, prePath, "#!/bin/sh\nprintf 'pre:%s\\n' \"$*\"\n")
	writeShellFixture(t, cargoPath, "#!/bin/sh\nprintf 'cargo:%s\\n' \"$*\"\n")
	t.Setenv("PATH", binDir)

	hook := ShellHook("old")
	cmd := exec.Command("/bin/bash", "-c", hook+globalOptionCommands)
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("run Cargo hook: %v\n%s", err, output)
	}
	want := globalOptionOutput
	if string(output) != want {
		t.Fatalf("unexpected Cargo hook output: %q", output)
	}
}

func TestShellHookRoutesExistingAliases(t *testing.T) {
	for _, shell := range []string{"bash", "zsh"} {
		t.Run(shell, func(t *testing.T) {
			checkShellAliasRouting(t, shell)
		})
	}
}

func checkShellAliasRouting(t *testing.T, shell string) {
	t.Helper()
	shellPath, err := exec.LookPath(shell)
	if err != nil {
		t.Skipf("%s unavailable: %v", shell, err)
	}
	binDir, targetDir := shellAliasFixture(t)
	t.Setenv("PATH", binDir)
	t.Setenv("PRE_DISABLE", "")
	t.Setenv("BASH_ENV", "")
	hook := ShellHook("old")
	// Parse the fixed commands after alias setup, as an interactive shell does.
	script := shellAliasSetup + hook + hook + "eval \"$2\"\n"
	cmd := exec.Command(shellPath, "-f", "-c", script, "pre-alias-test", targetDir, shellAliasCommands)
	output, err := cmd.CombinedOutput()
	unexpectedResult := err != nil || string(output) != shellAliasOutput
	if unexpectedResult {
		t.Fatalf("expected %q, got %q (error: %v)", shellAliasOutput, output, err)
	}
}

func shellAliasFixture(t *testing.T) (string, string) {
	t.Helper()
	binDir := t.TempDir()
	targetDir := filepath.Join(t.TempDir(), "alternate bin")
	if err := os.Mkdir(targetDir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeShellFixture(t, filepath.Join(binDir, "pre"), "#!/bin/sh\nprintf 'pre:%s\\n' \"$*\"\n")
	writeShellFixture(t, filepath.Join(binDir, "brew"), "#!/bin/sh\nprintf 'default:%s\\n' \"$*\"\n")
	writeShellFixture(t, filepath.Join(binDir, "python3"), "#!/bin/sh\nprintf 'python3:%s\\n' \"$*\"\n")
	writeShellFixture(t, filepath.Join(binDir, "npm"), "#!/bin/sh\nprintf 'npm:%s\\n' \"$*\"\n")
	writeShellFixture(t, filepath.Join(targetDir, "brew"), "#!/bin/sh\nprintf 'alias:%s\\n' \"$*\"\n")
	return binDir, targetDir
}

func TestShellHookUpdatesPreAfterBrewUpdate(t *testing.T) {
	for _, shell := range []string{"bash", "zsh"} {
		t.Run(shell, func(t *testing.T) {
			checkBrewBackgroundUpdate(t, shell)
		})
	}
}

func TestShellHookSkipsBackgroundUpdate(t *testing.T) {
	cases := []struct{ command, status, disable string }{
		{"update", "7", ""},
		{"update", "0", "1"},
		{"update --help", "0", ""},
		{"update -h", "0", ""},
		{"--prefix update", "0", ""},
	}
	for _, tc := range cases {
		t.Run(tc.command+tc.status+tc.disable, func(t *testing.T) {
			checkBrewUpdateSkipped(t, tc.command, tc.status, tc.disable)
		})
	}
}

func checkBrewUpdateSkipped(t *testing.T, command, status, disable string) {
	t.Helper()
	cmd, logPath := brewBackgroundUpdateCommand(t, "bash")
	cmd.Args[len(cmd.Args)-1] = ShellHook("old") + "brew " + command + "\n"
	t.Setenv("PRE_BREW_UPDATE_STATUS", status)
	t.Setenv("PRE_DISABLE", disable)
	err := cmd.Run()
	if cmd.ProcessState == nil {
		t.Fatalf("brew update failed to start: %v", err)
	}
	got := fmt.Sprint(cmd.ProcessState.ExitCode())
	if got != status {
		t.Fatalf("brew exit status changed: got %s, want %s", got, status)
	}
	time.Sleep(100 * time.Millisecond)
	if _, err := os.Stat(logPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unexpected automatic update: %v", err)
	}
}

func checkBrewBackgroundUpdate(t *testing.T, shell string) {
	t.Helper()
	cmd, logPath := brewBackgroundUpdateCommand(t, shell)
	output, err := cmd.CombinedOutput()
	unexpected := err != nil || string(output) != "brew:update --verbose\n"
	if unexpected {
		t.Fatalf("brew update did not return independently: %v, %q", err, output)
	}
	got := awaitBrewUpdateFile(t, logPath)
	if got != "self update:1:1\n" {
		t.Fatalf("unexpected background updater: %q", got)
	}
	if err := os.WriteFile(logPath+".release", nil, 0o600); err != nil {
		t.Fatal(err)
	}
	awaitBrewUpdateFile(t, logPath+".done")
}

func brewBackgroundUpdateCommand(t *testing.T, shell string) (*exec.Cmd, string) {
	t.Helper()
	shellPath, err := exec.LookPath(shell)
	if err != nil {
		t.Skipf("%s unavailable: %v", shell, err)
	}
	dir := t.TempDir()
	logPath := filepath.Join(dir, "update.log")
	brewFixture := "#!/bin/sh\nprintf 'brew:%s\\n' \"$*\"\nexit \"${PRE_BREW_UPDATE_STATUS:-0}\"\n"
	writeShellFixture(t, filepath.Join(dir, "brew"), brewFixture)
	writeShellFixture(t, filepath.Join(dir, "pre"), brewBackgroundUpdateFixture)
	t.Setenv("PATH", dir)
	t.Setenv("BASH_ENV", "")
	t.Setenv("PRE_DISABLE", "")
	t.Setenv("PRE_UPDATE_LOG", logPath)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	cmd := exec.CommandContext(ctx, shellPath, "-f", "-c", ShellHook("old")+"brew update --verbose\n")
	return cmd, logPath
}

func writeShellFixture(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatalf("write shell fixture: %v", err)
	}
}

func awaitBrewUpdateFile(t *testing.T, path string) string {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		content, err := os.ReadFile(path)
		if err == nil {
			return string(content)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("background updater did not write %s", path)
	return ""
}

func TestShellHookReloadsAfterUpdate(t *testing.T) {
	zshOutput := shellRefreshOutput + "after-prompt:7\n"
	cases := []struct{ shell, prompt, want string }{
		{"bash", "string", shellRefreshOutput},
		{"bash", "array", shellRefreshOutput},
		{"bash", "prepended", shellRefreshOutput},
		{"bash", "multiline", shellRefreshOutput},
		{"zsh", "array", zshOutput},
	}
	for _, tc := range cases {
		t.Run(tc.shell+"/"+tc.prompt, func(t *testing.T) {
			checkShellHookRefresh(t, tc.shell, tc.prompt, tc.want)
		})
	}
}

func checkShellHookRefresh(t *testing.T, shell, prompt, want string) {
	t.Helper()
	shellPath, logPath := shellRefreshFixture(t, shell)
	hook := ShellHook("old")
	script := shellRefreshSetup + hook + hook + shellRefreshCommands
	cmd := exec.Command(shellPath, "-f", "-c", script, "pre-hook-test", prompt)
	output, err := cmd.CombinedOutput()
	unexpectedOutput := err != nil || string(output) != want
	if unexpectedOutput {
		t.Fatalf("refresh hooks: %v\nwant %q\ngot %q", err, want, output)
	}
	calls, err := os.ReadFile(logPath)
	unexpectedChecks := err != nil || string(calls) != "version\n"
	if unexpectedChecks {
		t.Fatalf("expected one refresh check per prompt, got %q: %v", calls, err)
	}
}

func shellRefreshFixture(t *testing.T, shell string) (string, string) {
	t.Helper()
	shellPath, err := exec.LookPath(shell)
	if err != nil {
		t.Skipf("%s unavailable: %v", shell, err)
	}
	binDir := t.TempDir()
	hookPath := filepath.Join(binDir, "hook.sh")
	logPath := filepath.Join(binDir, "calls")
	writeShellFixture(t, filepath.Join(binDir, "pre"), shellRefreshPre)
	writeShellFixture(t, hookPath, ShellHook("new"))
	t.Setenv("PATH", binDir)
	t.Setenv("BASH_ENV", "")
	t.Setenv("PRE_TEST_HOOK", hookPath)
	t.Setenv("PRE_TEST_LOG", logPath)
	return shellPath, logPath
}

func TestShellHookRefreshClearsCommandCache(t *testing.T) {
	for _, shell := range []string{"bash", "zsh"} {
		t.Run(shell, func(t *testing.T) {
			checkShellHookCachedPath(t, shell)
		})
	}
}

func checkShellHookCachedPath(t *testing.T, shell string) {
	t.Helper()
	shellPath, _ := shellRefreshFixture(t, shell)
	oldPath := filepath.Join(t.TempDir(), "pre")
	writeShellFixture(t, oldPath, "#!/bin/sh\nprintf 'old\\n'\n")
	script := ShellHook("old") + shellRefreshCachedPath
	cmd := exec.Command(shellPath, "-f", "-c", script, "pre-cache-test", oldPath)
	output, err := cmd.CombinedOutput()
	unexpectedOutput := err != nil || string(output) != "old\nstatus:1\nnew\n"
	if unexpectedOutput {
		t.Fatalf("cached executable was not refreshed: %q, %v", output, err)
	}
}

func TestDetectRCFileZsh(t *testing.T) {
	t.Setenv("SHELL", "/bin/zsh")
	t.Setenv("HOME", t.TempDir())
	rc, err := detectRCFile()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(rc, ".zshrc") {
		t.Errorf("expected .zshrc, got %s", rc)
	}
}

func TestDetectRCFileBash(t *testing.T) {
	t.Setenv("SHELL", "/bin/bash")
	t.Setenv("HOME", t.TempDir())
	rc, err := detectRCFile()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(rc, ".bashrc") {
		t.Errorf("expected .bashrc, got %s", rc)
	}
}

func TestDetectRCFileRequiresHome(t *testing.T) {
	t.Setenv("HOME", "")
	if _, err := detectRCFile(); err == nil {
		t.Fatal("expected missing home directory to fail")
	}
}

func TestSetupFresh(t *testing.T) {
	rcPath := setupTestRCPath(t)
	Setup("test")

	content, err := os.ReadFile(rcPath)
	if err != nil {
		t.Fatalf("expected .zshrc to be created: %v", err)
	}
	if !strings.Contains(string(content), "# pre security proxy") {
		t.Error("expected hook to be written to .zshrc")
	}
	if !strings.Contains(string(content), "_pre_hook_version='test'\n") {
		t.Error("expected setup to record the installed hook version")
	}
}

func TestRefreshShellHooks(t *testing.T) {
	prefix, suffix := "export FOO=bar\n\n", "\nexport BAR=baz\n"
	current := prefix + ShellHook("new") + suffix
	older := prefix + ShellHook("old") + suffix
	unmanaged := prefix + suffix
	legacy := "# pre security proxy\nfunction npm() {}\nexport BAR=baz\n"
	legacyWant := strings.TrimPrefix(ShellHook("new"), "\n") + "export BAR=baz\n"
	cases := []struct{ name, initial, want string }{
		{"legacy", legacy, legacyWant},
		{"older", older, current},
		{"current", current, current},
		{"unmanaged", unmanaged, unmanaged},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			checkHookMigration(t, tc.initial, tc.want)
		})
	}
}

func checkHookMigration(t *testing.T, initial, want string) {
	t.Helper()
	path := setupTestRCPath(t)
	if err := os.WriteFile(path, []byte(initial), 0o644); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := RefreshShellHooks("new"); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	unexpected := err != nil || string(content) != want
	if unexpected {
		t.Fatalf("unexpected migrated profile: %v\n%s", err, content)
	}
	assertMigrationMetadata(t, path, before, initial == want)
}

func assertMigrationMetadata(t *testing.T, path string, before os.FileInfo, unchanged bool) {
	t.Helper()
	after, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if after.Mode().Perm() != before.Mode().Perm() {
		t.Fatal("migration changed the profile permissions")
	}
	rewritten := unchanged && !os.SameFile(before, after)
	if rewritten {
		t.Fatal("migration rewrote an unchanged profile")
	}
}

func TestRefreshShellHooksDoesNotCreateProfile(t *testing.T) {
	path := setupTestRCPath(t)
	if err := RefreshShellHooks("new"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("migration created a profile: %v", err)
	}
}

func TestRefreshShellHooksPreservesUnreadableProfile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read files without read permission")
	}
	path := setupTestRCPath(t)
	original := []byte(ShellHook("old"))
	writeUnreadableRC(t, path, original)
	if err := RefreshShellHooks("new"); err == nil {
		t.Fatal("expected migration to report the unreadable profile")
	}
	assertRCUnchanged(t, path, original)
}

func TestSetupPreservesExistingRCFileMode(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("SHELL", "/bin/zsh")

	rcPath := filepath.Join(dir, ".zshrc")
	if err := os.WriteFile(rcPath, []byte("export FOO=bar\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	Setup("test")

	info, err := os.Stat(rcPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Errorf("expected rc file mode 0644, got %o", info.Mode().Perm())
	}
}

func TestWriteRCFileFollowsSymlinks(t *testing.T) {
	for _, kind := range []string{"relative", "absolute", "chain"} {
		t.Run(kind, func(t *testing.T) {
			path, target := profileSymlinkFixture(t, kind)
			assertRCWrite(t, path, target, 0o600)
			if err := os.Chmod(target, 0o644); err != nil {
				t.Fatal(err)
			}
			assertRCWrite(t, path, target, 0o644)
		})
	}
}

func profileSymlinkFixture(t *testing.T, kind string) (string, string) {
	t.Helper()
	dir := t.TempDir()
	target := filepath.Join(dir, "profile")
	linkTarget := target
	switch kind {
	case "relative":
		linkTarget = "profile"
	case "chain":
		linkTarget = filepath.Join(dir, "profile-link")
		createProfileSymlink(t, "profile", linkTarget)
	}
	path := filepath.Join(dir, ".zshrc")
	createProfileSymlink(t, linkTarget, path)
	return path, target
}

func createProfileSymlink(t *testing.T, target, path string) {
	t.Helper()
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
}

func assertRCWrite(t *testing.T, path, target string, perm os.FileMode) {
	t.Helper()
	link, err := os.Readlink(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeRCFile(path, []byte("updated profile\n")); err != nil {
		t.Fatal(err)
	}
	assertRCFile(t, target, perm)
	got, err := os.Readlink(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != link {
		t.Errorf("symlink target = %q, want %q", got, link)
	}
}

func assertRCFile(t *testing.T, path string, perm os.FileMode) {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != "updated profile\n" {
		t.Errorf("unexpected profile contents: %q", content)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != perm {
		t.Errorf("profile mode = %o, want %o", info.Mode().Perm(), perm)
	}
}

func TestWriteRCFileResolvesParentBeforeDotDot(t *testing.T) {
	dir := t.TempDir()
	nested := filepath.Join(dir, "actual", "nested")
	if err := os.MkdirAll(nested, 0o700); err != nil {
		t.Fatal(err)
	}
	alias := filepath.Join(dir, "alias")
	createProfileSymlink(t, nested, alias)
	path := filepath.Join(dir, ".zshrc")
	createProfileSymlink(t, "alias/../profile", path)
	target := filepath.Join(dir, "actual", "profile")
	assertRCWrite(t, path, target, 0o600)
}

func TestWriteRCFileRejectsInvalidSymlinks(t *testing.T) {
	for _, target := range []string{".zshrc", "missing/profile"} {
		t.Run(target, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, ".zshrc")
			createProfileSymlink(t, target, path)
			if err := writeRCFile(path, []byte("updated profile\n")); err == nil {
				t.Fatal("expected invalid symlink target to fail")
			}
			if _, err := os.Readlink(path); err != nil {
				t.Fatalf("profile symlink was replaced: %v", err)
			}
		})
	}
}

func TestSetupRefreshesExistingHooks(t *testing.T) {
	rcPath := setupTestRCPath(t)
	initial := "export FOO=bar\n# pre security proxy\nfunction npm() {}\n"
	os.WriteFile(rcPath, []byte(initial), 0644)

	Setup("test")

	content, _ := os.ReadFile(rcPath)
	if !strings.Contains(string(content), "export FOO=bar") {
		t.Error("expected setup to preserve content before existing hook")
	}
	if !strings.Contains(string(content), "# end pre security proxy") {
		t.Error("expected setup to refresh hook block")
	}
	contentText := string(content)
	containsUpdate := strings.Contains(contentText, `"$_pre_command" == "update"`)
	if !containsUpdate {
		t.Error("expected refreshed hooks to include update commands")
	}
}

func TestTeardownRemovesHooks(t *testing.T) {
	rcPath := setupTestRCPath(t)
	os.WriteFile(rcPath, []byte("export FOO=bar\n"+ShellHook("old")+"export BAR=baz\n"), 0644)

	Teardown()

	content, _ := os.ReadFile(rcPath)
	if strings.Contains(string(content), "# pre security proxy") {
		t.Error("expected hook marker to be removed")
	}
	if !strings.Contains(string(content), "export FOO=bar") {
		t.Error("expected content before marker to be preserved")
	}
	if !strings.Contains(string(content), "export BAR=baz") {
		t.Error("expected content after marker to be preserved")
	}
}

func TestTeardownRemovesLegacyHooksWithoutDeletingTrailingContent(t *testing.T) {
	rcPath := setupTestRCPath(t)
	os.WriteFile(rcPath, []byte("export FOO=bar\n# pre security proxy\nfunction bun() {}\nexport BAR=baz\n"), 0644)

	Teardown()

	content, _ := os.ReadFile(rcPath)
	if strings.Contains(string(content), "# pre security proxy") {
		t.Error("expected hook marker to be removed")
	}
	if !strings.Contains(string(content), "export FOO=bar") {
		t.Error("expected content before marker to be preserved")
	}
	if !strings.Contains(string(content), "export BAR=baz") {
		t.Error("expected content after legacy hook to be preserved")
	}
}

func TestShellHookStatus(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("SHELL", "/bin/zsh")

	rcPath := filepath.Join(dir, ".zshrc")
	os.WriteFile(rcPath, []byte(ShellHook("old")), 0644)

	path, installed := ShellHookStatus()
	if path != rcPath {
		t.Errorf("expected rc path %s, got %s", rcPath, path)
	}
	if !installed {
		t.Error("expected hooks to be installed")
	}
}

func TestTeardownNoHooks(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("SHELL", "/bin/zsh")

	rcPath := filepath.Join(dir, ".zshrc")
	os.WriteFile(rcPath, []byte("export FOO=bar\n"), 0644)

	Teardown()

	content, _ := os.ReadFile(rcPath)
	if string(content) != "export FOO=bar\n" {
		t.Error("expected file to be unchanged when no hooks present")
	}
}

func TestTeardownReadError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("SHELL", "/bin/zsh")

	Teardown()
}

func TestTeardownWriteError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("SHELL", "/bin/zsh")

	rcPath := filepath.Join(dir, ".zshrc")
	os.WriteFile(rcPath, []byte("# pre security proxy\nstuff\n"), 0444)
	defer os.Chmod(rcPath, 0644)

	Teardown()
}

func TestSetupWriteError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("SHELL", "/bin/zsh")

	os.Mkdir(filepath.Join(dir, ".zshrc"), 0755)

	exited := false
	origExit := processExit
	processExit = func(code int) { exited = true; panic("exit") }
	defer func() {
		recover()
		processExit = origExit
		if !exited {
			t.Error("expected processExit to be called on write error")
		}
	}()

	Setup("test")
}

func TestSetupPreservesUnreadableRCFile(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root can read files without read permission")
	}
	rcPath := setupTestRCPath(t)
	original := []byte("export IMPORTANT=value\n")
	writeUnreadableRC(t, rcPath, original)

	exitCode := 0
	origExit := processExit
	processExit = func(code int) { exitCode = code }
	defer func() { processExit = origExit }()

	Setup("test")
	if exitCode != 1 {
		t.Fatalf("expected setup to fail, got exit code %d", exitCode)
	}
	assertRCUnchanged(t, rcPath, original)
}

func TestShellHookStatusNotInstalled(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("SHELL", "/bin/zsh")

	rcPath := filepath.Join(dir, ".zshrc")
	os.WriteFile(rcPath, []byte("export FOO=bar\n"), 0644)

	_, installed := ShellHookStatus()
	if installed {
		t.Error("expected hooks not to be installed")
	}
}

func TestNextLineNoNewline(t *testing.T) {
	line, n := nextLine("hello")
	unexpectedLine := line != "hello" || n != 5
	if unexpectedLine {
		t.Errorf("expected (hello, 5), got (%q, %d)", line, n)
	}
}

func TestRemoveLegacyShellHookBlockMultiLineFunction(t *testing.T) {
	content := "# pre security proxy\nfunction npm() {\n  command pre npm \"$@\"\n}\nexport BAR=baz\n"
	result := removeLegacyShellHookBlock(content, 0)
	if strings.Contains(result, "function npm") {
		t.Errorf("expected function to be removed, got %q", result)
	}
	if !strings.Contains(result, "export BAR=baz") {
		t.Errorf("expected trailing content preserved, got %q", result)
	}
}

func TestRemoveLegacyShellHookBlockNoTrailing(t *testing.T) {
	content := "# pre security proxy\nfunction npm() { command pre npm \"$@\"; }\n"
	result := removeLegacyShellHookBlock(content, 0)
	if strings.Contains(result, "function npm") {
		t.Errorf("expected function removed when no trailing content, got %q", result)
	}
}

func TestManagerAllInHook(t *testing.T) {
	hook := ShellHook("old")
	for _, mgr := range manager.All() {
		if !strings.Contains(hook, "function "+mgr.Name+"()") {
			t.Errorf("expected hook for manager %s", mgr.Name)
		}
	}
}

func setupTestRCPath(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("SHELL", "/bin/zsh")
	return filepath.Join(dir, ".zshrc")
}

func writeUnreadableRC(t *testing.T, rcPath string, original []byte) {
	t.Helper()
	if err := os.WriteFile(rcPath, original, 0o200); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(rcPath, 0o600) })

}

func assertRCUnchanged(t *testing.T, rcPath string, original []byte) {
	t.Helper()
	if err := os.Chmod(rcPath, 0o600); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(rcPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(content) != string(original) {
		t.Fatalf("expected RC file to remain unchanged, got %q", content)
	}
}
