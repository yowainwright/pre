package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/yowainwright/pre/internal/cache"
	"github.com/yowainwright/pre/internal/config"
	"github.com/yowainwright/pre/internal/proxy"
)

const installScriptURL = "https://raw.githubusercontent.com/yowainwright/pre/main/install.sh"

const (
	installSourceManual   = "manual"
	installSourceHomebrew = "homebrew"
)

var (
	executablePathFn = os.Executable
	lookPathFn       = exec.LookPath
	commandRunnerFn  = runExternalCommand
	removeFileFn     = os.Remove
	removeAllFn      = os.RemoveAll
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
	if err := commandRunnerFn("sh", []string{"-c", "curl -fsSL " + installScriptURL + " | sh"}, []string{"PRE_BIN_DIR=" + binDir}, stdout, stderr); err != nil {
		fmt.Fprintf(stderr, "pre update: %v\n", err)
		return 1
	}
	return 0
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
		return 0
	}

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
	cmd := exec.Command(name, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	if len(env) > 0 {
		cmd.Env = append(os.Environ(), env...)
	}
	return cmd.Run()
}
