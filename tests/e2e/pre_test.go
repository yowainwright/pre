//go:build e2e

package e2e

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
)

type shellHookSession struct {
	home, profile, shell, pre string
	env                       []string
}

const shellProfileBefore = "export PRE_E2E_BEFORE=before\n"
const shellProfileAfter = "export PRE_E2E_AFTER=after\n"

const shellHookLifecycleCommands = `
printf 'preserved:%s:%s\n' "$PRE_E2E_BEFORE" "$PRE_E2E_AFTER"
npm --version
npm install --registry=https://example.invalid react; printf 'blocked:%s\n' "$?"
exit
`

const shellHookUpgradeCommands = `
printf 'before:%s\n' "$_pre_hook_version"
/bin/ln -sf "$PRE_E2E_NEXT" "$PRE_E2E_LINK"
printf 'after:%s\n' "$_pre_hook_version"
exit
`

var preBin string

func TestMain(m *testing.M) {
	tmp, err := e2eBuildDir()
	if err != nil {
		panic(err)
	}
	preBin = filepath.Join(tmp, "pre")
	if err := buildE2EBinary(preBin, "e2e-current"); err != nil {
		_ = os.RemoveAll(tmp)
		panic(err)
	}
	code := m.Run()
	_ = os.RemoveAll(tmp)
	os.Exit(code)
}

func e2eBuildDir() (string, error) {
	root, err := filepath.Abs("../../")
	if err != nil {
		return "", err
	}
	dir := filepath.Join(root, "bin")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return os.MkdirTemp(dir, "pre-e2e-*")
}

func run(env []string, args ...string) (stdout, stderr string, code int) {
	cmd := exec.Command(preBin, args...)
	cmd.Env = env
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	err := cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			code = 1
		}
	}
	return out.String(), errOut.String(), code
}

func baseEnv(home string) []string {
	configEnv := "XDG_CONFIG_HOME=" + filepath.Join(home, ".config")
	cacheEnv := "XDG_CACHE_HOME=" + filepath.Join(home, ".cache")
	return append(os.Environ(), "HOME="+home, configEnv, cacheEnv,
		"NO_COLOR=1", "PRE_CACHE_TTL=0", "PRE_DISABLE=0", "PRE_OBS=0")
}

func TestVersion(t *testing.T) {
	stdout, _, code := run(baseEnv(t.TempDir()), "--version")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if stdout == "" {
		t.Error("expected version output")
	}
}

func TestNoArgs(t *testing.T) {
	_, stderr, code := run(baseEnv(t.TempDir()))
	if code != 1 {
		t.Fatalf("expected exit 1, got %d", code)
	}
	if !strings.Contains(stderr, "usage") {
		t.Errorf("expected usage in stderr, got %q", stderr)
	}
}

func TestUnknownManager(t *testing.T) {
	_, stderr, code := run(baseEnv(t.TempDir()), "notamanager", "install", "foo")
	if code != 1 {
		t.Fatalf("expected exit 1, got %d", code)
	}
	if !strings.Contains(stderr, "unknown manager") {
		t.Errorf("expected unknown manager in stderr, got %q", stderr)
	}
}

func TestSetupWritesBashrc(t *testing.T) {
	home := t.TempDir()
	env := append(baseEnv(home), "SHELL=/bin/bash")
	_, _, code := run(env, "setup")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d", code)
	}
	content, err := os.ReadFile(filepath.Join(home, ".bashrc"))
	if err != nil {
		t.Fatalf("expected .bashrc to exist: %v", err)
	}
	if !strings.Contains(string(content), "# pre security proxy") {
		t.Error("expected shell hook in .bashrc")
	}
}

func TestSetupIdempotent(t *testing.T) {
	home := t.TempDir()
	env := append(baseEnv(home), "SHELL=/bin/bash")
	run(env, "setup")
	run(env, "setup")

	content, _ := os.ReadFile(filepath.Join(home, ".bashrc"))
	count := strings.Count(string(content), "# pre security proxy")
	if count != 1 {
		t.Errorf("expected hook written exactly once, found %d times", count)
	}
}

func TestShellHookLifecycle(t *testing.T) {
	cases := []struct{ shell, command string }{
		{"bash", "setup"},
		{"zsh", "setup"},
		{"bash", "--version"},
		{"zsh", "--version"},
	}
	for _, tc := range cases {
		t.Run(tc.shell+"/"+tc.command, func(t *testing.T) {
			checkShellHookLifecycle(t, tc.shell, tc.command)
		})
	}
}

func TestShellHookTerminalIsolation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	args := shellHookTTYArgs(binary)
	cmd := exec.CommandContext(ctx, "script", args...)
	output, err := cmd.CombinedOutput()
	passed := strings.Contains(string(output), "--- PASS: TestShellHookLifecycle")
	failed := err != nil || !passed
	if failed {
		t.Fatalf("shell-hook test with parent terminal: %v\n%s", err, output)
	}
}

func checkShellHookLifecycle(t *testing.T, shell, command string) {
	t.Helper()
	session := newShellHookSession(t, shell)
	if command == "--version" {
		inline := runShellHookCLI(t, session, "shell-hook")
		writeShellHookFixture(t, session.profile, shellProfileBefore+inline+shellProfileAfter, 0o600)
	}
	runShellHookCLI(t, session, command)
	assertSourcedShellHookProfile(t, session)
	output := runShellHookSession(t, session, shellHookLifecycleCommands)
	want := []string{"preserved:before:after\n", "manager:--version\n", "blocked:1\n"}
	assertShellHookOutput(t, output, want)
	runShellHookCLI(t, session, "teardown")
	got := readShellHookProfile(t, session)
	profile := shellProfileBefore + shellProfileAfter
	if got != profile {
		t.Fatalf("teardown changed unrelated profile content: %q", got)
	}
}

func TestShellHookReloadsAfterUpgrade(t *testing.T) {
	oldBinary := filepath.Join(filepath.Dir(preBin), "pre-old")
	if err := buildE2EBinary(oldBinary, "e2e-old"); err != nil {
		t.Fatal(err)
	}
	for _, shell := range []string{"bash", "zsh"} {
		t.Run(shell, func(t *testing.T) {
			checkShellHookUpgrade(t, shell, oldBinary)
		})
	}
}

func buildE2EBinary(path, version string) error {
	root, err := filepath.Abs("../../")
	if err != nil {
		return err
	}
	flags := "-X main.version=" + version
	build := exec.Command("go", "build", "-ldflags", flags, "-o", path, "./cmd/pre")
	build.Dir = root
	if out, err := build.CombinedOutput(); err != nil {
		return fmt.Errorf("build failed: %w\n%s", err, out)
	}
	return nil
}

func checkShellHookUpgrade(t *testing.T, shell, oldBinary string) {
	t.Helper()
	session := newShellHookSession(t, shell)
	pointShellHookBinary(t, session, oldBinary)
	runShellHookCLI(t, session, "setup")
	before := readShellHookProfile(t, session)
	nextEnv, linkEnv := "PRE_E2E_NEXT="+preBin, "PRE_E2E_LINK="+session.pre
	session.env = append(session.env, nextEnv, linkEnv)
	output := runShellHookSession(t, session, shellHookUpgradeCommands)
	want := []string{"before:e2e-old\n", "after:e2e-current\n"}
	assertShellHookOutput(t, output, want)
	if got := readShellHookProfile(t, session); got != before {
		t.Fatalf("upgrade rewrote the profile: %q", got)
	}
}

func pointShellHookBinary(t *testing.T, session shellHookSession, binary string) {
	t.Helper()
	if err := os.Remove(session.pre); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(binary, session.pre); err != nil {
		t.Fatal(err)
	}
}

func newShellHookSession(t *testing.T, shell string) shellHookSession {
	t.Helper()
	shellPath, err := exec.LookPath(shell)
	if err != nil {
		t.Fatalf("%s is required for shell-hook E2E tests: %v", shell, err)
	}
	home := filepath.Join(t.TempDir(), "home with ' quote")
	profileName := fmt.Sprintf(".%src", shell)
	profile := filepath.Join(home, profileName)
	pre := filepath.Join(home, "bin", "pre")
	env := shellHookEnv(home, shellPath)
	session := shellHookSession{home: home, profile: profile, shell: shellPath, pre: pre, env: env}
	prepareShellHookSession(t, session)
	return session
}

func shellHookEnv(home, shell string) []string {
	bin := filepath.Join(home, "bin")
	path := fmt.Sprintf("PATH=%s%c%s", bin, os.PathListSeparator, os.Getenv("PATH"))
	shellEnv := "SHELL=" + shell
	zdotEnv := "ZDOTDIR=" + home
	return append(baseEnv(home), path, shellEnv, zdotEnv,
		"PS1=", "PS2=", "PROMPT=", "BASH_ENV=", "ENV=")
}

func prepareShellHookSession(t *testing.T, session shellHookSession) {
	t.Helper()
	bin := filepath.Dir(session.pre)
	if err := os.MkdirAll(bin, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(preBin, session.pre); err != nil {
		t.Fatal(err)
	}
	npm := filepath.Join(bin, "npm")
	writeShellHookFixture(t, npm, "#!/bin/sh\nprintf 'manager:%s\\n' \"$*\"\n", 0o755)
	writeShellHookFixture(t, session.profile, shellProfileBefore+shellProfileAfter, 0o600)
}

func writeShellHookFixture(t *testing.T, path, content string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
}

func runShellHookCLI(t *testing.T, session shellHookSession, args ...string) string {
	t.Helper()
	cmd := exec.Command(session.pre, args...)
	cmd.Env = session.env
	cmd.Dir = session.home
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("pre %v: %v\n%s", args, err, output)
	}
	return string(output)
}

func runShellHookSession(t *testing.T, session shellHookSession, script string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, session.shell, "-i")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Env = session.env
	cmd.Dir = session.home
	cmd.Stdin = strings.NewReader(script)
	var out, errOut bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errOut
	if err := cmd.Run(); err != nil {
		t.Fatalf("shell session: %v\nstdout: %s\nstderr: %s", err, out.String(), errOut.String())
	}
	return out.String()
}

func assertSourcedShellHookProfile(t *testing.T, session shellHookSession) {
	t.Helper()
	content := readShellHookProfile(t, session)
	if strings.Count(content, "\nsource ") != 1 {
		t.Fatalf("expected one sourced hook file: %q", content)
	}
	if strings.Contains(content, "function ") {
		t.Fatalf("profile still contains inline hook functions: %q", content)
	}
}

func readShellHookProfile(t *testing.T, session shellHookSession) string {
	t.Helper()
	content, err := os.ReadFile(session.profile)
	if err != nil {
		t.Fatal(err)
	}
	return string(content)
}

func assertShellHookOutput(t *testing.T, output string, want []string) {
	t.Helper()
	for _, marker := range want {
		if !strings.Contains(output, marker) {
			t.Errorf("missing %q in shell output:\n%s", marker, output)
		}
	}
}

func TestInterceptPassthrough(t *testing.T) {
	if _, err := exec.LookPath("npm"); err != nil {
		t.Skip("npm not available")
	}
	home := t.TempDir()
	stdout, _, _ := run(baseEnv(home), "npm", "run", "--help")
	if !strings.Contains(stdout+"-", "-") {
		t.Error("expected passthrough to npm")
	}
}

func TestInterceptChecksPackage(t *testing.T) {
	if _, err := exec.LookPath("npm"); err != nil {
		t.Skip("npm not available")
	}
	home := t.TempDir()
	stdout, _, _ := run(baseEnv(home), "npm", "install", "--dry-run", "react@18.0.0")
	if !strings.Contains(stdout, "react") {
		t.Errorf("expected react in output, got: %s", stdout)
	}
	hasCheck := strings.Contains(stdout, "checking") || strings.Contains(stdout, "clean") || strings.Contains(stdout, "vulnerabilit")
	if !hasCheck {
		t.Errorf("expected security check output, got: %s", stdout)
	}
}

func runInDir(dir string, env []string, args ...string) (stdout, stderr string, code int) {
	cmd := exec.Command(preBin, args...)
	cmd.Dir = dir
	cmd.Env = env
	var out, errOut bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &errOut
	err := cmd.Run()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code = exitErr.ExitCode()
		} else {
			code = 1
		}
	}
	return out.String(), errOut.String(), code
}

func TestBunLockNewFormatReadsExactVersions(t *testing.T) {
	if _, err := exec.LookPath("npm"); err != nil {
		t.Skip("npm not available")
	}
	home := t.TempDir()
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"test","version":"1.0.0","dependencies":{"react":"^18.0.0"}}`), 0644)
	os.WriteFile(filepath.Join(dir, "bun.lock"), []byte(`# Bun Lockfile v1

{
  "lockfileVersion": 1,
  "packages": {
    "react": ["react@18.2.0", {}, "sha512-abc"]
  }
}
`), 0644)

	stdout, _, _ := runInDir(dir, baseEnv(home), "npm", "install", "--dry-run")

	if strings.Contains(stdout, "exit status 1") {
		t.Errorf("got npm view error — bun.lock new format not parsed correctly:\n%s", stdout)
	}
	if strings.Contains(stdout, "resolution failed") {
		t.Errorf("got resolution error — bun.lock new format not parsed correctly:\n%s", stdout)
	}
	hasOutput := strings.Contains(stdout, "packages clean") || strings.Contains(stdout, "checking")
	if !hasOutput {
		t.Errorf("expected scan output, got:\n%s", stdout)
	}
}

func TestSemverConstraintResolvesWithoutError(t *testing.T) {
	if _, err := exec.LookPath("npm"); err != nil {
		t.Skip("npm not available")
	}
	home := t.TempDir()
	dir := t.TempDir()

	os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"test","version":"1.0.0","dependencies":{"react":"^18.0.0"}}`), 0644)

	stdout, _, _ := runInDir(dir, baseEnv(home), "npm", "install", "--dry-run")

	if strings.Contains(stdout, "exit status 1") {
		t.Errorf("npm view called with range — semver constraint not handled correctly:\n%s", stdout)
	}
	if strings.Contains(stdout, "resolution failed") {
		t.Errorf("got resolution error for semver constraint:\n%s", stdout)
	}
}

func TestCargoLockScansBeforeFetch(t *testing.T) {
	home := t.TempDir()
	dir := t.TempDir()
	binDir := t.TempDir()
	lockfile := `version = 4

[[package]]
name = "serde"
version = "1.0.217"
source = "registry+https://index.crates.io/"
`
	if err := os.WriteFile(filepath.Join(dir, "Cargo.lock"), []byte(lockfile), 0644); err != nil {
		t.Fatal(err)
	}
	fakeCargo := "#!/bin/sh\nprintf 'fake cargo %s\\n' \"$*\"\n"
	cargoPath := filepath.Join(binDir, "cargo")
	if err := os.WriteFile(cargoPath, []byte(fakeCargo), 0755); err != nil {
		t.Fatal(err)
	}

	pathValue := "PATH=" + binDir + string(os.PathListSeparator) + os.Getenv("PATH")
	env := append(baseEnv(home), pathValue)
	stdout, stderr, code := runInDir(dir, env, "cargo", "fetch", "--locked")
	if code != 0 {
		t.Fatalf("expected exit 0, got %d: %s%s", code, stdout, stderr)
	}
	if !strings.Contains(stdout, "fake cargo fetch --locked") {
		t.Errorf("expected Cargo execution after scan, got: %s", stdout)
	}
}

func shellHookTTYArgs(binary string) []string {
	selector := "-test.run=^TestShellHookLifecycle$/^zsh$/^setup$"
	if runtime.GOOS == "darwin" {
		return []string{"-q", "/dev/null", binary, selector, "-test.v"}
	}
	quotedBinary := strings.ReplaceAll(binary, "'", "'\\''")
	command := fmt.Sprintf("'%s' '%s' -test.v", quotedBinary, selector)
	return []string{"-q", "-e", "-c", command, "/dev/null"}
}
