package main

import (
	"fmt"
	"io"
	"os"

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
		fmt.Fprintln(stderr, "       pre setup")
		return 1
	}
	proxy.SetSystemScanEnabled(cfg.SystemScan)
	proxy.SetSystemScanTTL(cfg.SystemTTL)

	switch args[0] {
	case "setup":
		proxy.Setup()
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

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
