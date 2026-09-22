package main

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/yowainwright/pre/internal/cache"
	"github.com/yowainwright/pre/internal/config"
	"github.com/yowainwright/pre/internal/manager"
	"github.com/yowainwright/pre/internal/obs"
	"github.com/yowainwright/pre/internal/proxy"
	"github.com/yowainwright/pre/internal/security"
)

var version = "dev"

func run(args []string, stdout, stderr io.Writer) int {
	cfg := configureCommand()
	if len(args) < 1 {
		printUsage(stderr)
		return 1
	}
	return dispatchCommand(args, cfg, stdout, stderr)
}

func configureCommand() *config.Config {
	obs.SetVersion(version)
	cfg := config.Load()
	security.Endpoint = cfg.API.Endpoint
	cache.SetTTL(cfg.Cache.TTL)
	cache.SetSource(cfg.API.Endpoint)

	mgrs := make([]manager.Manager, len(cfg.Managers))
	for i, m := range cfg.Managers {
		mgrs[i] = manager.Manager{Name: m.Name, Ecosystem: m.Ecosystem, InstallCmds: m.InstallCmds}
	}
	manager.SetUserManagers(mgrs)
	return cfg
}

func printUsage(stderr io.Writer) {
	fmt.Fprintln(stderr, "usage: pre <manager> <command> [args]")
	fmt.Fprintln(stderr, "       pre manage | m | installed | install | update | downgrade | uninstall")
	fmt.Fprintln(stderr, "       pre obs [--json] [--events [query]]")
	fmt.Fprintln(stderr, "       pre setup | teardown | status | config [set <key> <value>]")
}

func dispatchCommand(args []string, cfg *config.Config, stdout, stderr io.Writer) int {
	switch args[0] {
	case "setup", "teardown", "scan", "status", "--version", "-v":
		return handleSystemCommand(args, stdout, stderr)
	case "config":
		return handleConfig(args[1:], cfg, stdout, stderr)
	case "obs", "observability":
		return handleObs(args[1:], stdout, stderr)
	case "self":
		return handleSelf(args[1:], stdout, stderr)
	case "screenshots":
		return handleScreenshots(args[1:], stdout, stderr)
	default:
		return dispatchPackages(args, stdout, stderr)
	}
}

func handleSystemCommand(args []string, stdout, stderr io.Writer) int {
	switch args[0] {
	case "setup":
		proxy.Setup()
	case "teardown":
		if err := proxy.Teardown(); err != nil {
			return 1
		}
	case "scan":
		return handleSystemScan(args[1:], stderr)
	case "status":
		handleStatus(stdout)
	case "--version", "-v":
		fmt.Fprintln(stdout, version)
	}
	return 0
}

func handleSystemScan(args []string, stderr io.Writer) int {
	invalidTarget := len(args) != 1 || args[0] != "system"
	if invalidTarget {
		fmt.Fprintln(stderr, "usage: pre scan system")
		return 1
	}
	proxy.RunSystemScan()
	return 0
}

func dispatchPackages(args []string, stdout, stderr io.Writer) int {
	switch args[0] {
	case "installed":
		return handlePackageInventory(stdout, stderr)
	case "manage", "m", "tui", "install", "downgrade":
		return handlePackages(args, stdout, stderr)
	case "update", "upgrade", "uninstall":
		return handleRequiredPackageAction(args, stdout, stderr)
	case "packages":
		return handlePackages(args[1:], stdout, stderr)
	default:
		return interceptManager(args, stderr)
	}
}

func handleRequiredPackageAction(args []string, stdout, stderr io.Writer) int {
	if len(args) > 1 {
		return handlePackages(args, stdout, stderr)
	}
	if args[0] == "uninstall" {
		fmt.Fprintln(stderr, "usage: pre uninstall <manager> <package>")
		return 1
	}
	fmt.Fprintln(stderr, "usage: pre update <manager> [package]")
	return 1
}

func interceptManager(args []string, stderr io.Writer) int {
	mgr := manager.Get(args[0])
	if mgr == nil {
		fmt.Fprintf(stderr, "pre: unknown manager: %s\n", args[0])
		return 1
	}
	proxy.Intercept(mgr, args[1:])
	return 0
}

func handlePackages(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		return handlePackageInventory(stdout, stderr)
	}
	switch args[0] {
	case "manage", "m", "tui":
		return handleManage(args[1:], stdout, stderr)
	case "install":
		return handlePackageAction(actionInstall, args[1:], stdout, stderr)
	case "update", "upgrade":
		return handlePackageAction(actionUpdate, args[1:], stdout, stderr)
	case "downgrade":
		return handlePackageAction(actionDowngrade, args[1:], stdout, stderr)
	case "uninstall", "remove":
		return handlePackageAction(actionUninstall, args[1:], stdout, stderr)
	default:
		fmt.Fprintln(stderr, "usage: pre packages [manage|install|update|downgrade|uninstall]")
		return 1
	}
}

func handleConfig(args []string, cfg *config.Config, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprintf(stdout, "api.endpoint  %s\n", cfg.API.Endpoint)
		fmt.Fprintf(stdout, "cache.ttl     %s\n", cfg.Cache.TTL)
		return 0
	}
	if args[0] != "set" {
		fmt.Fprintln(stderr, "usage: pre config [set <key> <value>]")
		return 1
	}
	if len(args) < 3 {
		fmt.Fprintln(stderr, "usage: pre config set <key> <value>")
		return 1
	}
	return handleConfigSet(args[1:], cfg, stdout, stderr)
}

func handleConfigSet(args []string, cfg *config.Config, stdout, stderr io.Writer) int {
	key, val := args[0], strings.Join(args[1:], " ")
	if err := setConfigValue(cfg, key, val); err != nil {
		fmt.Fprintf(stderr, "pre config: %v\n", err)
		return 1
	}
	if err := config.Save(cfg); err != nil {
		fmt.Fprintf(stderr, "pre config: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "%s = %s\n", key, val)
	return 0
}

func setConfigValue(cfg *config.Config, key, val string) error {
	switch normalizeConfigKey(key) {
	case "endpoint":
		cfg.API.Endpoint = val
	case "ttl":
		if err := validateNonNegativeDuration(val); err != nil {
			return fmt.Errorf("invalid duration for %s: %q", key, val)
		}
		cfg.Cache.TTL = val
	default:
		return fmt.Errorf("unknown key %q (api.endpoint, cache.ttl)", key)
	}
	return nil
}

func validateNonNegativeDuration(s string) error {
	d, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	if d < 0 {
		return fmt.Errorf("duration must be non-negative")
	}
	return nil
}

func normalizeConfigKey(key string) string {
	switch key {
	case "api.endpoint", "endpoint":
		return "endpoint"
	case "cache.ttl", "ttl":
		return "ttl"
	default:
		return key
	}
}

func handleStatus(stdout io.Writer) {
	info := collectInstallInfo()
	renderInstallInfo(stdout, info)
	fmt.Fprintln(stdout)
	renderManagerStatus(stdout)
	renderCacheStatus(stdout)
	renderSystemStatus(stdout, proxy.LoadSystemStats())
	renderObsStatus(stdout)
}

func renderManagerStatus(stdout io.Writer) {
	mgrs := manager.All()
	fmt.Fprintf(stdout, "managers (%d):\n", len(mgrs))
	for _, m := range mgrs {
		fmt.Fprintf(stdout, "  %-8s %s\n", m.Name, m.Ecosystem)
	}
}

func renderCacheStatus(stdout io.Writer) {
	c := cache.Load()
	fmt.Fprintf(stdout, "cached: %d packages\n", len(c))
}

func renderSystemStatus(stdout io.Writer, sys proxy.SystemStats) {
	if sys.Total == 0 {
		fmt.Fprintf(stdout, "system scan: no manual scan yet\n")
		return
	}
	if sys.Errors > 0 {
		renderSystemErrorStatus(stdout, sys)
		return
	}
	fmt.Fprintf(stdout, "system scan: %d total · %d crit · %d warn · last run %s\n",
		sys.Total, sys.Crit, sys.Warn, sys.LastUpdated.Format("2006-01-02 15:04"))
}

func renderSystemErrorStatus(stdout io.Writer, sys proxy.SystemStats) {
	if sys.LastUpdated.IsZero() {
		fmt.Fprintf(stdout, "system scan: %d total · %d crit · %d warn · %d errors · no successful run\n",
			sys.Total, sys.Crit, sys.Warn, sys.Errors)
		return
	}
	fmt.Fprintf(stdout, "system scan: %d total · %d crit · %d warn · %d errors · last successful %s\n",
		sys.Total, sys.Crit, sys.Warn, sys.Errors, sys.LastUpdated.Format("2006-01-02 15:04"))
}

func renderObsStatus(stdout io.Writer) {
	obsSummary, err := obs.Status()
	if err == nil {
		fmt.Fprintf(stdout, "obs: %s · %d events · %d bytes\n",
			enabledLabel(obsSummary.Enabled), obsSummary.Events, obsSummary.Bytes)
	}
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
