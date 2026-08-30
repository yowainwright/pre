package proxy

import (
	"fmt"
	"io"
	"os"
	"os/exec"
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
	run := proxyRun{mgr: mgr, args: args, start: start}
	run.record("pre.command.started", nil)
	if disableEnabled() {
		run.recordDecision("pre.command.bypassed", "bypassed", "env_disabled", nil)
		ExecFn(mgr.Name, args)
		return
	}

	packageArgs, isInstall := installPackageArgs(mgr, args)
	isPassthrough := !isInstall
	if isPassthrough {
		run.recordDecision("pre.command.passthrough", "passthrough", "not_install_command", nil)
		ExecFn(mgr.Name, args)
		return
	}
	if err := cargoInstallError(mgr, args); err != nil {
		run.recordBlock("cargo_policy", err)
		blockIncompleteInstall(err)
		return
	}
	if err := npmInstallError(mgr, packageArgs); err != nil {
		run.recordBlock("npm_policy", err)
		blockIncompleteInstall(err)
		return
	}

	fromProject := len(requirementFilePaths(mgr, packageArgs)) > 0
	packages, err := installPackages(mgr, packageArgs)
	if err != nil {
		run.recordBlock("package_resolution", err)
		blockIncompleteInstall(err)
		return
	}
	if err = validateCargoDirectPackages(mgr, args, packages); err != nil {
		run.recordBlock("cargo_direct_package", err)
		blockIncompleteInstall(err)
		return
	}
	if len(packages) == 0 {
		fromProject = true
		packages, err = installFallbackPackages(mgr, args)
	}
	if err != nil {
		run.recordBlock("project_resolution", err)
		blockIncompleteInstall(err)
		return
	}
	if len(packages) == 0 {
		run.recordDecision("pre.command.approved", "approved", "no_packages", nil)
		ExecFn(mgr.Name, args)
		return
	}
	if limit, exceeded := packageLimitExceeded(len(packages)); exceeded {
		run.recordDecision("pre.scan.blocked", "blocked", "package_limit", map[string]any{
			"package_count": len(packages),
			"package_limit": limit,
		})
		fmt.Print(display.Red(fmt.Sprintf(
			"pre: %d package(s) exceeds PRE_MAX_PACKAGES=%d; install blocked (raise PRE_MAX_PACKAGES or use PRE_DISABLE=1 to bypass)\n",
			len(packages), limit,
		)))
		processExit(1)
		return
	}

	c := loadCacheFn()

	uncachedCount := countUncached(mgr, packages, c)
	if uncachedCount > 0 && !quietEnabled() {
		fmt.Print(display.Dim(fmt.Sprintf("scanning %d package(s)...\n", uncachedCount)))
	}
	run.record("pre.scan.started", map[string]any{
		"package_count":  len(packages),
		"uncached_count": uncachedCount,
		"from_project":   fromProject,
	})

	results := scanBatchWithPolicy(mgr, packages, c, !fromProject)
	counts := countScanResults(results)
	attrs := scanResultAttrs(results, counts)
	attrs["duration_ms"] = durationMillis(start)
	run.record("pre.scan.completed", attrs)

	approvalRequired := needsApproval(results)
	level := outputLevel(results)
	if approvalRequired {
		if level == outputQuiet {
			level = outputFull
		}
	}
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
	if counts.errors > 0 {
		run.recordDecision("pre.scan.blocked", "blocked", "scan_error", map[string]any{
			"error_count": counts.errors,
		})
		fmt.Print(display.Red("pre: scan incomplete; install blocked (use PRE_DISABLE=1 to bypass)\n"))
		processExit(1)
		return
	}

	if approvalRequired {
		criticalAttrs := approvalAttrs(results)
		run.recordDecision("pre.scan.prompted", "prompted", "approval_required", criticalAttrs)
		criticals := criticalResults(results)
		if len(criticals) > 0 {
			fmt.Print(renderCriticalDetail(criticals))
		}
		if !confirm("Approve install?") {
			run.recordDecision("pre.scan.denied", "denied", "user_denied", criticalAttrs)
			processExit(1)
			return
		}
		run.recordDecision("pre.scan.approved", "approved", scanApprovalReason(results), criticalAttrs)
		storeApprovedScanResults(mgr, results)
	} else {
		run.recordDecision("pre.scan.approved", "approved", "cache_hit", nil)
	}

	ExecFn(mgr.Name, args)
}

func blockIncompleteInstall(err error) {
	format := "pre: scan incomplete: %v; install blocked (use PRE_DISABLE=1 to bypass)\n"
	message := fmt.Sprintf(format, err)
	styled := display.Red(message)
	fmt.Print(styled)
	processExit(1)
}

func scanApprovalReason(results []scanResult) string {
	if hasCriticalResults(results) {
		return "user_approved_high_or_critical"
	}
	for _, result := range results {
		if len(result.vulns) > 0 {
			return "user_approved_warning"
		}
	}
	return "user_approved_clean"
}

func needsApproval(results []scanResult) bool {
	for _, result := range results {
		if !result.cached {
			return true
		}
	}
	return false
}

func approvalAttrs(results []scanResult) map[string]any {
	counts := countScanResults(results)
	return map[string]any{
		"package_count":       len(results),
		"cached_count":        counts.cached,
		"critical_count":      counts.criticals,
		"vulnerability_count": counts.vulnerabilities,
	}
}

func hasCriticalResults(results []scanResult) bool {
	return len(criticalResults(results)) > 0
}

func criticalResults(results []scanResult) []scanResult {
	criticals := make([]scanResult, 0)
	for _, result := range results {
		if hasCriticalVulns(result) {
			criticals = append(criticals, result)
		}
	}
	return criticals
}

func storeApprovedScanResults(mgr *manager.Manager, results []scanResult) {
	fresh := make(cache.Cache)
	for _, result := range results {
		if shouldCacheApprovedResult(result) {
			cache.Set(fresh, cache.Key(mgr.Ecosystem, result.name, result.version))
		}
	}
	storeFreshScanResults(fresh)
}

func shouldCacheApprovedResult(result scanResult) bool {
	if result.err != nil {
		return false
	}
	if result.version == "" {
		return false
	}
	if !result.cacheable {
		return false
	}
	return !result.cached
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
