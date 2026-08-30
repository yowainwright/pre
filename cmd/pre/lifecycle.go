package main

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/yowainwright/pre/internal/cache"
	"github.com/yowainwright/pre/internal/config"
	"github.com/yowainwright/pre/internal/proxy"
)

const (
	installScriptURL    = "https://github.com/yowainwright/pre/releases/latest/download/install.sh"
	installChecksumsURL = "https://github.com/yowainwright/pre/releases/latest/download/checksums.txt"
)

const (
	installSourceManual       = "manual"
	installSourceHomebrew     = "homebrew"
	installSourceHomebrewCask = "homebrew-cask"
	selfUsage                 = "usage: pre self installed | update | uninstall [--purge]"
)

var (
	executablePathFn         = os.Executable
	lookPathFn               = exec.LookPath
	commandRunnerFn          = runExternalCommand
	commandRunnerWithInputFn = runExternalCommandWithInput
	removeFileFn             = os.Remove
	removeAllFn              = os.RemoveAll
	httpGetBytesFn           = httpGetBytes
)

func handleSelf(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return printSelfUsage(stderr)
	}
	switch args[0] {
	case "installed", "status":
		return handleSelfInstalledCommand(args, stdout, stderr)
	case "update":
		return handleSelfUpdate(args[1:], stdout, stderr)
	case "uninstall":
		return handleSelfUninstall(args[1:], stdout, stderr)
	default:
		return printSelfUsage(stderr)
	}
}

func printSelfUsage(stderr io.Writer) int {
	fmt.Fprintln(stderr, selfUsage)
	return 1
}

func handleSelfInstalledCommand(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 {
		fmt.Fprintln(stderr, "usage: pre self installed")
		return 1
	}
	handleSelfInstalled(stdout)
	return 0
}

func handleSelfInstalled(stdout io.Writer) {
	renderInstallInfo(stdout, collectInstallInfo())
}

func handleSelfUpdate(args []string, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		fmt.Fprintln(stderr, "usage: pre self update")
		return 1
	}

	info := collectInstallInfo()
	if info.BinaryPath == "" {
		fmt.Fprintln(stderr, "pre update: could not locate the pre binary")
		return 1
	}
	if isHomebrewInstall(info.Source) {
		return updateHomebrewSelf(info.Source, stdout, stderr)
	}
	return updateManualSelf(info.BinaryPath, stdout, stderr)
}

func updateHomebrewSelf(source string, stdout, stderr io.Writer) int {
	brewArgs := homebrewLifecycleArgs(source, "upgrade")
	if _, err := lookPathFn("brew"); err != nil {
		fmt.Fprintln(stderr, "pre update: Homebrew install detected, but brew is not on PATH")
		fmt.Fprintf(stderr, "pre update: run: brew %s\n", strings.Join(brewArgs, " "))
		return 1
	}
	fmt.Fprintln(stdout, "pre: updating with Homebrew")
	if err := commandRunnerFn("brew", brewArgs, nil, stdout, stderr); err != nil {
		fmt.Fprintf(stderr, "pre update: %v\n", err)
		return 1
	}
	return 0
}

func updateManualSelf(binaryPath string, stdout, stderr io.Writer) int {
	binDir := filepath.Dir(binaryPath)
	missingBinDir := binDir == "." || binDir == ""
	if missingBinDir {
		fmt.Fprintf(stderr, "pre update: could not determine binary directory for %s\n", binaryPath)
		return 1
	}
	fmt.Fprintf(stdout, "pre: updating manual install in %s\n", binDir)
	if err := downloadVerifyAndRun(installScriptURL, installChecksumsURL, []string{"PRE_BIN_DIR=" + binDir}, stdout, stderr); err != nil {
		fmt.Fprintf(stderr, "pre update: %v\n", err)
		return 1
	}
	return 0
}

func downloadVerifyAndRun(scriptURL, checksumsURL string, env []string, stdout, stderr io.Writer) error {
	script, err := httpGetBytesFn(scriptURL)
	if err != nil {
		return fmt.Errorf("downloading install.sh: %w", err)
	}
	checksums, err := httpGetBytesFn(checksumsURL)
	if err != nil {
		return fmt.Errorf("downloading checksums.txt: %w", err)
	}
	if err := verifyInstallScript(script, checksums); err != nil {
		return err
	}
	tmp, err := os.CreateTemp("", "pre-install-*.sh")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(script); err != nil {
		if closeErr := tmp.Close(); closeErr != nil {
			return fmt.Errorf("writing install script: %w; closing temp file: %v", err, closeErr)
		}
		return err
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing install script: %w", err)
	}
	if err := os.Chmod(tmp.Name(), 0700); err != nil { // #nosec G302 -- executable script requires 0700
		return err
	}
	return commandRunnerFn("sh", []string{tmp.Name()}, env, stdout, stderr)
}

var httpClient = &http.Client{Timeout: 60 * time.Second}

func httpGetBytes(url string) ([]byte, error) {
	resp, err := httpClient.Get(url) // #nosec G107 -- URL is a package-level constant
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d for %s", resp.StatusCode, url)
	}
	return io.ReadAll(resp.Body)
}

func verifyInstallScript(script, checksums []byte) error {
	sum := sha256.Sum256(script)
	got := hex.EncodeToString(sum[:])
	for _, line := range strings.Split(string(checksums), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == "install.sh" && fields[0] == got {
			return nil
		}
	}
	return errors.New("install.sh SHA256 checksum does not match checksums.txt")
}

func handleSelfUninstall(args []string, stdout, stderr io.Writer) int {
	purge, valid := selfUninstallOptions(args)
	if !valid {
		fmt.Fprintln(stderr, "usage: pre self uninstall [--purge]")
		return 1
	}
	return uninstallSelf(collectInstallInfo(), purge, stdout, stderr)
}

func uninstallSelf(info installInfo, purge bool, stdout, stderr io.Writer) int {
	if !validateSelfUninstall(info, stderr) {
		return 1
	}
	if !removeSelfHooks(stdout, stderr) {
		return 1
	}
	if !uninstallSelfBinary(info, stdout, stderr) {
		return 1
	}
	if purge && !purgeInstallData(stdout, stderr) {
		return 1
	}
	return 0
}

func selfUninstallOptions(args []string) (bool, bool) {
	purge := false
	for _, arg := range args {
		if arg != "--purge" {
			return false, false
		}
		purge = true
	}
	return purge, true
}

func validateSelfUninstall(info installInfo, stderr io.Writer) bool {
	if isHomebrewInstall(info.Source) {
		brewArgs := homebrewLifecycleArgs(info.Source, "uninstall")
		if _, err := lookPathFn("brew"); err != nil {
			fmt.Fprintln(stderr, "pre uninstall: Homebrew install detected, but brew is not on PATH")
			fmt.Fprintf(stderr, "pre uninstall: run: brew %s\n", strings.Join(brewArgs, " "))
			return false
		}
		return true
	}
	return validateManualSelfUninstall(info, stderr)
}

func validateManualSelfUninstall(info installInfo, stderr io.Writer) bool {
	if info.BinaryPath == "" {
		fmt.Fprintln(stderr, "pre uninstall: could not locate the pre binary")
		return false
	}
	if filepath.Base(info.BinaryPath) != "pre" {
		fmt.Fprintf(stderr, "pre uninstall: refusing to remove %s because its filename is not pre\n", info.BinaryPath)
		return false
	}
	return true
}

func removeSelfHooks(stdout, stderr io.Writer) bool {
	rcFile, removed, err := proxy.RemoveShellHooks()
	if err != nil {
		fmt.Fprintf(stderr, "pre uninstall: %v\n", err)
		return false
	}
	if removed {
		fmt.Fprintf(stdout, "pre: removed hooks from %s\n", rcFile)
	} else {
		fmt.Fprintf(stdout, "pre: no hooks found in %s\n", rcFile)
	}
	return true
}

func uninstallSelfBinary(info installInfo, stdout, stderr io.Writer) bool {
	if isHomebrewInstall(info.Source) {
		brewArgs := homebrewLifecycleArgs(info.Source, "uninstall")
		fmt.Fprintln(stdout, "pre: uninstalling with Homebrew")
		if err := commandRunnerFn("brew", brewArgs, nil, stdout, stderr); err != nil {
			fmt.Fprintf(stderr, "pre uninstall: %v\n", err)
			return false
		}
		return true
	}
	if err := removeFileFn(info.BinaryPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(stderr, "pre uninstall: remove %s: %v\n", info.BinaryPath, err)
		return false
	}
	fmt.Fprintf(stdout, "pre: removed binary %s\n", info.BinaryPath)
	return true
}

type installInfo struct {
	Version       string
	BinaryPath    string
	Source        string
	HookPath      string
	HookInstalled bool
	ConfigPath    string
	ConfigExists  bool
	CachePath     string
	CacheExists   bool
	CachedCount   int
}

func collectInstallInfo() installInfo {
	binaryPath := currentExecutablePath()
	hookPath, hookInstalled := proxy.ShellHookStatus()
	configPath := currentConfigPath()
	cachePath := currentCachePath()

	return installInfo{
		Version:       version,
		BinaryPath:    binaryPath,
		Source:        detectInstallSource(binaryPath),
		HookPath:      hookPath,
		HookInstalled: hookInstalled,
		ConfigPath:    configPath,
		ConfigExists:  fileExists(configPath),
		CachePath:     cachePath,
		CacheExists:   fileExists(cachePath),
		CachedCount:   len(cache.Load()),
	}
}

func currentExecutablePath() string {
	binaryPath, err := executablePathFn()
	if err != nil {
		return ""
	}
	return binaryPath
}

func currentConfigPath() string {
	configPath, _ := config.Path()
	return configPath
}

func currentCachePath() string {
	cachePath, _ := cache.Path()
	return cachePath
}

func renderInstallInfo(stdout io.Writer, info installInfo) {
	fmt.Fprintf(stdout, "pre: %s\n", info.Version)
	if info.BinaryPath == "" {
		fmt.Fprintln(stdout, "binary: unknown")
	} else {
		fmt.Fprintf(stdout, "binary: %s (%s)\n", info.BinaryPath, sourceLabel(info.Source))
	}
	if info.HookInstalled {
		fmt.Fprintf(stdout, "shell hooks: installed in %s\n", info.HookPath)
	} else {
		fmt.Fprintf(stdout, "shell hooks: not installed (run 'pre setup')\n")
	}
	fmt.Fprintf(stdout, "config: %s (%s)\n", info.ConfigPath, existsLabel(info.ConfigExists))
	fmt.Fprintf(stdout, "cache: %s (%s, %d packages)\n", info.CachePath, existsLabel(info.CacheExists), info.CachedCount)
	fmt.Fprintln(stdout, "update pre: pre self update")
	fmt.Fprintln(stdout, "uninstall pre: pre self uninstall")
}

func detectInstallSource(binaryPath string) string {
	if binaryPath == "" {
		return installSourceManual
	}
	resolved := binaryPath
	if p, err := filepath.EvalSymlinks(binaryPath); err == nil {
		resolved = p
	}
	normalized := filepath.ToSlash(resolved)
	if strings.Contains(normalized, "/Caskroom/pre/") {
		return installSourceHomebrewCask
	}
	if strings.Contains(normalized, "/Cellar/pre/") {
		return installSourceHomebrew
	}
	return installSourceManual
}

func isHomebrewInstall(source string) bool {
	return source == installSourceHomebrew || source == installSourceHomebrewCask
}

func homebrewLifecycleArgs(source, action string) []string {
	args := []string{action}
	if source == installSourceHomebrewCask {
		args = append(args, "--cask")
	}
	return append(args, "pre")
}

func sourceLabel(source string) string {
	switch source {
	case installSourceHomebrew, installSourceHomebrewCask:
		return "Homebrew"
	default:
		return "manual"
	}
}

func existsLabel(exists bool) string {
	if exists {
		return "exists"
	}
	return "missing"
}

func enabledLabel(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "disabled"
}

func fileExists(path string) bool {
	if path == "" {
		return false
	}
	_, err := os.Stat(path)
	return err == nil
}

func purgeInstallData(stdout, stderr io.Writer) bool {
	ok := true
	if p, err := config.Path(); err == nil {
		ok = removeInstallDir("config", filepath.Dir(p), stdout, stderr) && ok
	}
	if p, err := cache.Path(); err == nil {
		ok = removeInstallDir("cache", filepath.Dir(p), stdout, stderr) && ok
	}
	return ok
}

func removeInstallDir(label, dir string, stdout, stderr io.Writer) bool {
	if dir == "" || dir == "." {
		return true
	}
	if err := removeAllFn(dir); err != nil && !errors.Is(err, os.ErrNotExist) {
		fmt.Fprintf(stderr, "pre uninstall: remove %s %s: %v\n", label, dir, err)
		return false
	}
	fmt.Fprintf(stdout, "pre: removed %s %s\n", label, dir)
	return true
}

func runExternalCommand(name string, args []string, env []string, stdout, stderr io.Writer) error {
	return runExternalCommandWithInput(name, args, env, os.Stdin, stdout, stderr)
}

func runExternalCommandWithInput(name string, args []string, env []string, stdin io.Reader, stdout, stderr io.Writer) error {
	cmd := exec.Command(name, args...) // #nosec G204,G702 -- lifecycle commands use fixed executables and argument builders.
	if stdin == nil {
		stdin = os.Stdin
	}
	cmd.Stdin = stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	return cmd.Run()
}
