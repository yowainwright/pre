package main

import (
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/yowainwright/pre/internal/cache"
	"github.com/yowainwright/pre/internal/config"
	"github.com/yowainwright/pre/internal/manager"
	"github.com/yowainwright/pre/internal/proxy"
	"github.com/yowainwright/pre/internal/security"
)

var version = "dev"

func run(args []string, stdout, stderr io.Writer) int {
	cfg := config.Load()
	security.Endpoint = cfg.API.Endpoint
	cache.SetTTL(cfg.Cache.TTL)

	mgrs := make([]manager.Manager, len(cfg.Managers))
	for i, m := range cfg.Managers {
		mgrs[i] = manager.Manager{Name: m.Name, Ecosystem: m.Ecosystem, InstallCmds: m.InstallCmds}
	}
	manager.SetUserManagers(mgrs)

	if len(args) < 1 {
		fmt.Fprintln(stderr, "usage: pre <manager> <command> [args]")
		fmt.Fprintln(stderr, "       pre setup | teardown | status | config [set <key> <value>]")
		return 1
	}
	proxy.SetSystemScanEnabled(cfg.SystemScan)
	proxy.SetSystemScanTTL(cfg.SystemTTL)

	switch args[0] {
	case "setup":
		proxy.Setup()
	case "teardown":
		proxy.Teardown()
	case "scan":
		if len(args) < 2 {
			return 1
		}
		if args[1] == "system" {
			proxy.RunSystemScan()
			return 0
		}
		mgr := manager.Get(args[1])
		if mgr == nil {
			return 1
		}
		proxy.RunBackgroundScan(mgr)
	case "config":
		handleConfig(args[1:], cfg, stdout, stderr)
	case "status":
		handleStatus(cfg, stdout)
	case "--version", "-v":
		fmt.Fprintln(stdout, version)
	default:
		mgr := manager.Get(args[0])
		if mgr == nil {
			fmt.Fprintf(stderr, "pre: unknown manager: %s\n", args[0])
			return 1
		}
		proxy.Intercept(mgr, args[1:])
	}
	return 0
}

func handleConfig(args []string, cfg *config.Config, stdout, stderr io.Writer) {
	if len(args) == 0 {
		fmt.Fprintf(stdout, "endpoint    %s\n", cfg.API.Endpoint)
		fmt.Fprintf(stdout, "ttl         %s\n", cfg.Cache.TTL)
		fmt.Fprintf(stdout, "systemScan  %v\n", cfg.SystemScan)
		fmt.Fprintf(stdout, "systemTTL   %s\n", cfg.SystemTTL)
		return
	}
	if args[0] == "set" && len(args) >= 3 {
		key, val := args[1], strings.Join(args[2:], " ")
		switch key {
		case "endpoint", "api.endpoint":
			cfg.API.Endpoint = val
		case "ttl", "cache.ttl":
			cfg.Cache.TTL = val
		case "systemScan":
			cfg.SystemScan = val == "true"
		case "systemTTL":
			cfg.SystemTTL = val
		default:
			fmt.Fprintf(stderr, "pre config: unknown key %q (endpoint, api.endpoint, ttl, cache.ttl, systemScan, systemTTL)\n", key)
			return
		}
		if err := config.Save(cfg); err != nil {
			fmt.Fprintf(stderr, "pre config: %v\n", err)
			return
		}
		fmt.Fprintf(stdout, "%s = %s\n", key, val)
	}
}

func handleStatus(cfg *config.Config, stdout io.Writer) {
	mgrs := manager.All()
	fmt.Fprintf(stdout, "managers (%d):\n", len(mgrs))
	for _, m := range mgrs {
		fmt.Fprintf(stdout, "  %-8s %s\n", m.Name, m.Ecosystem)
	}

	c := cache.Load()
	fmt.Fprintf(stdout, "cached: %d packages\n", len(c))

	sys := proxy.LoadSystemStats()
	if sys.Total == 0 {
		fmt.Fprintf(stdout, "system scan: not configured (run 'pre setup')\n")
	} else {
		fmt.Fprintf(stdout, "system scan: %d total · %d crit · %d warn · last run %s\n",
			sys.Total, sys.Crit, sys.Warn, sys.LastUpdated.Format("2006-01-02 15:04"))
	}
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
