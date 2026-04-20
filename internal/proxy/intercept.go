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

	packages := extractPackages(mgr, args[1:])
	if len(packages) == 0 {
		packages = readManifestFn(mgr)
	}
	if len(packages) == 0 {
		ExecFn(mgr.Name, args)
		return
	}

	c := loadCacheFn()

	uncachedCount := countUncached(mgr, packages, c)
	if uncachedCount > 0 {
		fmt.Print(display.Dim(fmt.Sprintf("scanning %d package(s)...\n", uncachedCount)))
	}

	results := scanAll(mgr, packages, c)

	for _, r := range results {
		if len(r.vulns) == 0 && r.version != "" && r.err == nil && !r.cached {
			cache.Set(c, cache.Key(mgr.Ecosystem, r.name), r.version)
		}
	}
	saveCacheFn(c)

	switch outputLevel(results) {
	case outputSilent:
	case outputQuiet:
		fmt.Print(renderQuiet(len(results)))
	default:
		fmt.Print(renderTree(mgr.Ecosystem, results))
	}

	var criticals []scanResult
	for _, r := range results {
		if hasCriticalVulns(r) {
			criticals = append(criticals, r)
		}
	}
	if len(criticals) > 0 {
		fmt.Print(renderCriticalDetail(criticals))
		if !confirm("Proceed with install?") {
			processExit(1)
			return
		}
	}

	ExecFn(mgr.Name, args)
	if systemScanEnabled && shouldRunSystemScan() {
		spawnSystemScan()
	}
}

type outputMode int

const (
	outputSilent outputMode = iota
	outputQuiet
	outputFull
)

func outputLevel(results []scanResult) outputMode {
	for _, r := range results {
		if len(r.vulns) > 0 || r.err != nil {
			return outputFull
		}
	}
	for _, r := range results {
		if !r.cached {
			return outputQuiet
		}
	}
	return outputSilent
}

func countUncached(mgr *manager.Manager, packages []string, c cache.Cache) int {
	n := 0
	for _, pkg := range packages {
		name, version := manager.ParseSpec(mgr.Ecosystem, pkg)
		if shouldResolveVersion(mgr.Ecosystem, version) || !cache.Hit(c, cache.Key(mgr.Ecosystem, name), version) {
			n++
		}
	}
	return n
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
	return answer == "y" || answer == "yes"
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
