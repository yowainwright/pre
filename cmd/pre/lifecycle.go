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
	installSourceManual   = "manual"
	installSourceHomebrew = "homebrew"
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

func handleSelf(args []string, cfg *config.Config, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintln(stderr, "usage: pre self installed | update | uninstall [--purge]")
		return 1
	}
	switch args[0] {
	case "installed", "status":
		if len(args) != 1 {
			fmt.Fprintln(stderr, "usage: pre self installed")
			return 1
		}
		handleSelfInstalled(cfg, stdout)
	case "update":
		return handleSelfUpdate(args[1:], cfg, stdout, stderr)
	case "uninstall":
		return handleSelfUninstall(args[1:], cfg, stdout, stderr)
	default:
		fmt.Fprintln(stderr, "usage: pre self installed | update | uninstall [--purge]")
		return 1
	}
	return 0
}

func handleSelfInstalled(cfg *config.Config, stdout io.Writer) {
	renderInstallInfo(stdout, collectInstallInfo(cfg))
}

func handleSelfUpdate(args []string, cfg *config.Config, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		fmt.Fprintln(stderr, "usage: pre self update")
		return 1
	}

	info := collectInstallInfo(cfg)
	if info.BinaryPath == "" {
		fmt.Fprintln(stderr, "pre update: could not locate the pre binary")
		return 1
	}

	if info.Source == installSourceHomebrew {
		if _, err := lookPathFn("brew"); err != nil {
			fmt.Fprintln(stderr, "pre update: Homebrew install detected, but brew is not on PATH")
			fmt.Fprintln(stderr, "pre update: run: brew upgrade pre")
			return 1
		}
		fmt.Fprintln(stdout, "pre: updating with Homebrew")
		if err := commandRunnerFn("brew", []string{"upgrade", "pre"}, nil, stdout, stderr); err != nil {
			fmt.Fprintf(stderr, "pre update: %v\n", err)
			return 1
		}
		return 0
	}

	binDir := filepath.Dir(info.BinaryPath)
	if binDir == "." || binDir == "" {
		fmt.Fprintf(stderr, "pre update: could not determine binary directory for %s\n", info.BinaryPath)
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

func handleSelfUninstall(args []string, cfg *config.Config, stdout, stderr io.Writer) int {
	purge := false
	for _, arg := range args {
		switch arg {
		case "--purge":
			purge = true
		default:
			fmt.Fprintln(stderr, "usage: pre self uninstall [--purge]")
			return 1
		}
	}

	info := collectInstallInfo(cfg)

	if info.Source == installSourceHomebrew {
		if _, err := lookPathFn("brew"); err != nil {
			fmt.Fprintln(stderr, "pre uninstall: Homebrew install detected, but brew is not on PATH")
			fmt.Fprintln(stderr, "pre uninstall: run: brew uninstall pre")
			return 1
		}
		fmt.Fprintln(stdout, "pre: uninstalling Homebrew formula")
		if err := commandRunnerFn("brew", []string{"uninstall", "pre"}, nil, stdout, stderr); err != nil {
			fmt.Fprintf(stderr, "pre uninstall: %v\n", err)
			return 1
		}
	} else {
		if info.BinaryPath == "" {
			fmt.Fprintln(stderr, "pre uninstall: could not locate the pre binary")
			return 1
		}
		if filepath.Base(info.BinaryPath) != "pre" {
			fmt.Fprintf(stderr, "pre uninstall: refusing to remove %s because its filename is not pre\n", info.BinaryPath)
			return 1
		}
		if err := removeFileFn(info.BinaryPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			fmt.Fprintf(stderr, "pre uninstall: remove %s: %v\n", info.BinaryPath, err)
			return 1
		}
		fmt.Fprintf(stdout, "pre: removed binary %s\n", info.BinaryPath)
	}

	rcFile, removedHooks, err := proxy.RemoveShellHooks()
	if err != nil {
		fmt.Fprintf(stderr, "pre uninstall: %v\n", err)
		return 1
	}
	if removedHooks {
		fmt.Fprintf(stdout, "pre: removed hooks from %s\n", rcFile)
	} else {
		fmt.Fprintf(stdout, "pre: no hooks found in %s\n", rcFile)
	}

	if purge && !purgeInstallData(stdout, stderr) {
		return 1
	}
	return 0
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
	SystemScan    bool
}

func collectInstallInfo(cfg *config.Config) installInfo {
	binaryPath, err := executablePathFn()
	if err != nil {
		binaryPath = ""
	}
	hookPath, hookInstalled := proxy.ShellHookStatus()
	configPath, _ := config.Path()
	cachePath, _ := cache.Path()

	info := installInfo{
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
	if cfg != nil {
		info.SystemScan = cfg.SystemScan
	}
	return info
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
	fmt.Fprintf(stdout, "background system scan: %s\n", enabledLabel(info.SystemScan))
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
	if strings.Contains(filepath.ToSlash(resolved), "/Cellar/pre/") {
		return installSourceHomebrew
	}
	return installSourceManual
}

func sourceLabel(source string) string {
	switch source {
	case installSourceHomebrew:
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
