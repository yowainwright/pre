package proxy

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/yowainwright/pre/internal/cache"
	"github.com/yowainwright/pre/internal/display"
	"github.com/yowainwright/pre/internal/manager"
	"github.com/yowainwright/pre/internal/security"
)

var (
	processExit                         = os.Exit
	stdinReader               io.Reader = os.Stdin
	ExecFn                              = execReal
	securityCheckFn                     = security.Check
	securityBatchCheckFn                = security.CheckBatch
	resolveVersionFn                    = manager.ResolveVersion
	loadCacheFn                         = cache.Load
	updateCacheFn                       = cache.Update
	readManifestFn                      = manager.ReadManifest
	readManifestDirFn                   = manager.ReadManifestDir
	validateManifestFn                  = manager.ValidateManifest
	readRequirementsFileFn              = manager.ReadRequirementsFile
	readCargoFetchPackagesFn            = manager.ReadCargoFetchPackages
	readCargoUpdatePackagesFn           = manager.ReadCargoUpdatePackages
)

func Intercept(mgr *manager.Manager, args []string) {
	start := time.Now()
	recordProxyEvent("pre.command.started", mgr, args, nil)
	if disableEnabled() {
		recordProxyEvent("pre.command.bypassed", mgr, args, map[string]any{
			"decision":    "bypassed",
			"reason":      "env_disabled",
			"duration_ms": durationMillis(start),
		})
		ExecFn(mgr.Name, args)
		return
	}

	packageArgs, isInstall := installPackageArgs(mgr, args)
	isPassthrough := !isInstall
	if isPassthrough {
		recordProxyEvent("pre.command.passthrough", mgr, args, map[string]any{
			"decision":    "passthrough",
			"reason":      "not_install_command",
			"duration_ms": durationMillis(start),
		})
		ExecFn(mgr.Name, args)
		return
	}
	if err := cargoInstallError(mgr, args); err != nil {
		recordProxyBlock(mgr, args, start, "cargo_policy", err)
		blockIncompleteInstall(err)
		return
	}
	if err := npmInstallError(mgr, packageArgs); err != nil {
		recordProxyBlock(mgr, args, start, "npm_policy", err)
		blockIncompleteInstall(err)
		return
	}

	fromProject := len(requirementFilePaths(mgr, packageArgs)) > 0
	packages, err := installPackages(mgr, packageArgs)
	if err != nil {
		recordProxyBlock(mgr, args, start, "package_resolution", err)
		blockIncompleteInstall(err)
		return
	}
	if err = validateCargoDirectPackages(mgr, args, packages); err != nil {
		recordProxyBlock(mgr, args, start, "cargo_direct_package", err)
		blockIncompleteInstall(err)
		return
	}
	if len(packages) == 0 {
		fromProject = true
		packages, err = installFallbackPackages(mgr, args)
	}
	if err != nil {
		recordProxyBlock(mgr, args, start, "project_resolution", err)
		blockIncompleteInstall(err)
		return
	}
	if len(packages) == 0 {
		recordProxyEvent("pre.command.approved", mgr, args, map[string]any{
			"decision":    "approved",
			"reason":      "no_packages",
			"duration_ms": durationMillis(start),
		})
		ExecFn(mgr.Name, args)
		return
	}
	if limit, exceeded := packageLimitExceeded(len(packages)); exceeded {
		recordProxyEvent("pre.scan.skipped", mgr, args, map[string]any{
			"decision":      "approved",
			"reason":        "package_limit",
			"package_count": len(packages),
			"package_limit": limit,
			"duration_ms":   durationMillis(start),
		})
		if !quietEnabled() {
			fmt.Print(display.Dim(fmt.Sprintf("pre: skipping scan for %d packages (PRE_MAX_PACKAGES=%d)\n", len(packages), limit)))
		}
		ExecFn(mgr.Name, args)
		return
	}

	c := loadCacheFn()

	uncachedCount := countUncached(mgr, packages, c)
	if uncachedCount > 0 && !quietEnabled() {
		fmt.Print(display.Dim(fmt.Sprintf("scanning %d package(s)...\n", uncachedCount)))
	}
	recordProxyEvent("pre.scan.started", mgr, args, map[string]any{
		"package_count":  len(packages),
		"uncached_count": uncachedCount,
		"from_project":   fromProject,
	})

	results := scanAllWithPolicy(mgr, packages, c, !fromProject)
	attrs := scanResultAttrs(results)
	attrs["duration_ms"] = durationMillis(start)
	recordProxyEvent("pre.scan.completed", mgr, args, attrs)

	fresh := make(cache.Cache)
	for _, r := range results {
		if shouldCacheScanResult(r) {
			cache.Set(fresh, cache.Key(mgr.Ecosystem, r.name, r.version))
		}
	}
	storeFreshScanResults(fresh)

	level := outputLevel(results)
	if quietEnabled() && level == outputQuiet {
		level = outputSilent
	}
	switch level {
	case outputSilent:
	case outputQuiet:
		fmt.Print(renderQuiet(len(results)))
	default:
		fmt.Print(renderTree(mgr.Ecosystem, results))
	}
	if hasScanErrors(results) {
		recordProxyEvent("pre.scan.blocked", mgr, args, map[string]any{
			"decision":    "blocked",
			"reason":      "scan_error",
			"error_count": countScanErrors(results),
			"duration_ms": durationMillis(start),
		})
		fmt.Print(display.Red("pre: scan incomplete; install blocked (use PRE_DISABLE=1 to bypass)\n"))
		processExit(1)
		return
	}

	var criticals []scanResult
	for _, r := range results {
		if hasCriticalVulns(r) {
			criticals = append(criticals, r)
		}
	}
	if len(criticals) > 0 {
		recordProxyEvent("pre.scan.prompted", mgr, args, map[string]any{
			"decision":       "prompted",
			"reason":         "high_or_critical",
			"critical_count": len(criticals),
			"duration_ms":    durationMillis(start),
		})
		fmt.Print(renderCriticalDetail(criticals))
		if !confirm("Proceed with install?") {
			recordProxyEvent("pre.scan.denied", mgr, args, map[string]any{
				"decision":       "denied",
				"reason":         "user_denied",
				"critical_count": len(criticals),
				"duration_ms":    durationMillis(start),
			})
			processExit(1)
			return
		}
		recordProxyEvent("pre.scan.approved", mgr, args, map[string]any{
			"decision":       "approved",
			"reason":         "user_confirmed_high_or_critical",
			"critical_count": len(criticals),
			"duration_ms":    durationMillis(start),
		})
	} else {
		recordProxyEvent("pre.scan.approved", mgr, args, map[string]any{
			"decision":    "approved",
			"reason":      scanApprovalReason(results),
			"duration_ms": durationMillis(start),
		})
	}

	ExecFn(mgr.Name, args)
	if !backgroundDisabled() {
		spawnBackgroundScanFn(mgr.Name)
		if systemScanEnabled && shouldRunSystemScan() {
			spawnSystemScanFn()
		}
	}
}

func blockIncompleteInstall(err error) {
	format := "pre: scan incomplete: %v; install blocked (use PRE_DISABLE=1 to bypass)\n"
	message := fmt.Sprintf(format, err)
	styled := display.Red(message)
	fmt.Print(styled)
	processExit(1)
}

func recordProxyBlock(mgr *manager.Manager, args []string, start time.Time, reason string, err error) {
	recordProxyEvent("pre.scan.blocked", mgr, args, map[string]any{
		"decision":    "blocked",
		"reason":      reason,
		"error_type":  diagnosticsErrorType(err),
		"duration_ms": durationMillis(start),
	})
}

func scanApprovalReason(results []scanResult) string {
	for _, result := range results {
		if len(result.vulns) > 0 {
			return "warning_only"
		}
	}
	return "clean"
}

func hasScanErrors(results []scanResult) bool {
	return slices.ContainsFunc(results, func(result scanResult) bool {
		return result.err != nil
	})
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
		if !hasExactCacheHit(mgr, c, name, version) {
			n++
		}
	}
	return n
}

func hasExactCacheHit(mgr *manager.Manager, c cache.Cache, name, version string) bool {
	return version != "" &&
		!shouldResolveVersion(mgr.Ecosystem, version) &&
		isExactVersion(mgr.Ecosystem, version) &&
		cache.Hit(c, cache.Key(mgr.Ecosystem, name, version))
}

func hasCriticalVulns(r scanResult) bool {
	for _, v := range r.vulns {
		switch v.Severity {
		case security.SeverityCritical, security.SeverityHigh:
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
	start := time.Now()
	recordManagerExecStarted(name, args)
	c := exec.Command(name, args...) // #nosec G204 -- proxy intentionally execs the requested package manager.
	c.Stdin = os.Stdin
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	if err := c.Run(); err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			code := exitErr.ExitCode()
			recordManagerExec(name, args, start, code)
			processExit(code)
			return
		}
		recordManagerExec(name, args, start, 1)
		processExit(1)
		return
	}
	recordManagerExec(name, args, start, 0)
}
