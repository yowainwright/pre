package proxy

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"

	"github.com/yowainwright/pre/internal/cache"
	"github.com/yowainwright/pre/internal/display"
	"github.com/yowainwright/pre/internal/manager"
	"github.com/yowainwright/pre/internal/security"
)

var (
	processExit                = os.Exit
	stdinReader      io.Reader = os.Stdin
	ExecFn                     = execReal
	securityCheckFn            = security.Check
	resolveVersionFn           = manager.ResolveVersion
	loadCacheFn                = cache.Load
	saveCacheFn                = cache.Save
	readManifestFn             = manager.ReadManifest
)

func Intercept(mgr *manager.Manager, args []string) {
	isPassthrough := len(args) == 0 || !slices.Contains(mgr.InstallCmds, args[0])
	if isPassthrough {
		ExecFn(mgr.Name, args)
		return
	}

	packages := extractPackages(args[1:])
	if len(packages) == 0 {
		packages = readManifestFn(mgr)
	}
	if len(packages) == 0 {
		ExecFn(mgr.Name, args)
		return
	}

	c := loadCacheFn()

	results := make([]scanResult, len(packages))
	for i, pkg := range packages {
		results[i] = scanPackage(mgr, pkg, c)
	}

	fmt.Print(renderTree(mgr.Ecosystem, results))

	for _, r := range results {
		if hasCriticalVulns(r) {
			if !confirm("Critical vulnerabilities found. Proceed with install?") {
				processExit(1)
				return
			}
			break
		}
	}

	saveCacheFn(c)
	ExecFn(mgr.Name, args)
	if systemScanEnabled && shouldRunSystemScan() {
		spawnSystemScan()
	}
}

func hasCriticalVulns(r scanResult) bool {
	for _, v := range r.vulns {
		switch v.Severity {
		case "CRITICAL", "HIGH":
			return true
		}
	}
	return false
}

func confirm(prompt string) bool {
	fmt.Print(display.Prompt(prompt))
	var line []byte
	buf := make([]byte, 1)
	for {
		n, err := stdinReader.Read(buf)
		if n > 0 {
			if buf[0] == '\n' {
				break
			}
			line = append(line, buf[0])
		}
		if err != nil {
			break
		}
	}
	answer := strings.ToLower(strings.TrimSpace(string(line)))
	isYes := answer == "y" || answer == "yes"
	return isYes
}

func extractPackages(args []string) []string {
	isPackage := func(a string) bool {
		return !strings.HasPrefix(a, "-") && !strings.HasPrefix(a, ".")
	}
	result := make([]string, 0, len(args))
	for _, a := range args {
		if isPackage(a) {
			result = append(result, a)
		}
	}
	return result
}

func execReal(name string, args []string) {
	c := exec.Command(name, args...)
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			processExit(exitErr.ExitCode())
			return
		}
		processExit(1)
		return
	}
}
