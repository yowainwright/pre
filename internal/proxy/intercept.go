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
	run := proxyRun{mgr: mgr, args: args, start: time.Now()}
	run.record("pre.command.started", nil)
	if run.bypassDisabled() {
		return
	}
	packageArgs, isInstall := installPackageArgs(mgr, args)
	if !isInstall {
		run.recordDecision("pre.command.passthrough", "passthrough", "not_install_command", nil)
		ExecFn(mgr.Name, args)
		return
	}
	run.interceptInstall(packageArgs)
}

func (run proxyRun) bypassDisabled() bool {
	if !disableEnabled() {
		return false
	}
	run.recordDecision("pre.command.bypassed", "bypassed", "env_disabled", nil)
	ExecFn(run.mgr.Name, run.args)
	return true
}

func (run proxyRun) interceptInstall(packageArgs []string) {
	if !run.allowInstall(packageArgs) {
		return
	}
	packages, fromProject, ok := run.resolvePackages(packageArgs)
	if !ok {
		return
	}
	if len(packages) == 0 {
		run.recordDecision("pre.command.approved", "approved", "no_packages", nil)
		ExecFn(run.mgr.Name, run.args)
		return
	}
	run.interceptPackages(packageArgs, packages, fromProject)
}

func (run proxyRun) interceptPackages(packageArgs, packages []string, fromProject bool) {
	if !run.allowPackageCount(len(packages)) {
		return
	}
	results := run.scanPackages(packages, fromProject)
	if !run.approveResults(results) {
		return
	}
	args := run.resolvedInstallArgs(packageArgs, packages, results)
	ExecFn(run.mgr.Name, args)
}

func (run proxyRun) resolvedInstallArgs(packageArgs, packages []string, results []scanResult) []string {
	if run.mgr.Name != "poetry" {
		return run.args
	}
	versions := make(map[string]string)
	for index, spec := range packages {
		_, requested := manager.ParseSpec(run.mgr.Ecosystem, spec)
		if requested == "@latest" {
			versions[spec] = results[index].version
		}
	}
	if len(versions) == 0 {
		return run.args
	}
	args := slices.Clone(run.args)
	offset := len(args) - len(packageArgs)
	_ = walkPackageArgs(run.mgr, packageArgs, func(index int, option bool) error {
		arg := packageArgs[index]
		version := versions[arg]
		pin := !option && version != ""
		if pin {
			args[offset+index] = strings.Replace(arg, "@latest", "=="+version, 1)
		}
		return nil
	})
	return args
}

func (run proxyRun) allowInstall(packageArgs []string) bool {
	if run.blockInstall("cargo_policy", cargoInstallError(run.mgr, run.args)) {
		return false
	}
	policyArgs := npmPolicyArgs(run.mgr, run.args, packageArgs)
	if run.blockInstall("npm_policy", npmInstallError(run.mgr, policyArgs)) {
		return false
	}
	targetErr := unknownInstallTargetError(run.mgr, run.args, packageArgs)
	return !run.blockInstall("install_target_policy", targetErr)
}

func (run proxyRun) blockInstall(reason string, err error) bool {
	if err == nil {
		return false
	}
	run.recordBlock(reason, err)
	blockIncompleteInstall(err)
	return true
}

func (run proxyRun) resolvePackages(packageArgs []string) ([]string, bool, bool) {
	fromProject := len(requirementFilePaths(run.mgr, packageArgs)) > 0
	packages, err := installPackages(run.mgr, packageArgs)
	if run.blockInstall("package_resolution", err) {
		return nil, fromProject, false
	}
	err = validateCargoDirectPackages(run.mgr, run.args, packages)
	if run.blockInstall("cargo_direct_package", err) {
		return nil, fromProject, false
	}
	if len(packages) == 0 {
		fromProject = true
		packages, err = installFallbackPackages(run.mgr, run.args)
	}
	if run.blockInstall("project_resolution", err) {
		return nil, fromProject, false
	}
	return packages, fromProject, true
}

func (run proxyRun) allowPackageCount(count int) bool {
	limit, exceeded := packageLimitExceeded(count)
	if !exceeded {
		return true
	}
	run.recordDecision("pre.scan.blocked", "blocked", "package_limit", map[string]any{
		"package_count": count, "package_limit": limit,
	})
	message := fmt.Sprintf("pre: %d package(s) exceeds PRE_MAX_PACKAGES=%d; install blocked (raise PRE_MAX_PACKAGES or use PRE_DISABLE=1 to bypass)\n", count, limit)
	fmt.Print(display.Red(message))
	processExit(1)
	return false
}

func (run proxyRun) scanPackages(packages []string, fromProject bool) []scanResult {
	c := loadCacheFn()
	uncachedCount := countUncached(run.mgr, packages, c)
	showProgress := uncachedCount > 0 && !quietEnabled()
	if showProgress {
		fmt.Print(display.Dim(fmt.Sprintf("scanning %d package(s)...\n", uncachedCount)))
	}
	run.record("pre.scan.started", map[string]any{
		"package_count": len(packages), "uncached_count": uncachedCount, "from_project": fromProject,
	})
	results := scanBatchWithPolicy(run.mgr, packages, c, !fromProject)
	counts := countScanResults(results)
	attrs := scanResultAttrs(results, counts)
	attrs["duration_ms"] = durationMillis(run.start)
	run.record("pre.scan.completed", attrs)
	return results
}

func (run proxyRun) approveResults(results []scanResult) bool {
	approvalRequired := needsApproval(results)
	printScanResults(run.mgr.Ecosystem, results, approvalRequired)
	counts := countScanResults(results)
	if counts.errors > 0 {
		run.recordDecision("pre.scan.blocked", "blocked", "scan_error", map[string]any{"error_count": counts.errors})
		fmt.Print(display.Red("pre: scan incomplete; install blocked (use PRE_DISABLE=1 to bypass)\n"))
		processExit(1)
		return false
	}
	if approvalRequired {
		return run.requestScanApproval(results)
	}
	run.recordDecision("pre.scan.approved", "approved", "cache_hit", nil)
	return true
}

func printScanResults(ecosystem string, results []scanResult, approvalRequired bool) {
	level := outputLevel(results)
	needsFullOutput := approvalRequired && level == outputQuiet
	if needsFullOutput {
		level = outputFull
	}
	suppressQuietOutput := quietEnabled() && level == outputQuiet
	if suppressQuietOutput {
		level = outputSilent
	}
	switch level {
	case outputSilent:
	case outputQuiet:
		fmt.Print(renderQuiet(len(results)))
	default:
		fmt.Print(renderTree(ecosystem, results))
	}
}

func (run proxyRun) requestScanApproval(results []scanResult) bool {
	attrs := approvalAttrs(results)
	run.recordDecision("pre.scan.prompted", "prompted", "approval_required", attrs)
	criticals := criticalResults(results)
	if len(criticals) > 0 {
		fmt.Print(renderCriticalDetail(criticals))
	}
	if !confirm("Approve install?") {
		run.recordDecision("pre.scan.denied", "denied", "user_denied", attrs)
		processExit(1)
		return false
	}
	run.recordDecision("pre.scan.approved", "approved", scanApprovalReason(results), attrs)
	storeApprovedScanResults(run.mgr, results)
	return true
}

func blockIncompleteInstall(err error) {
	format := "pre: scan incomplete: %v; install blocked (use PRE_DISABLE=1 to bypass)\n"
	message := fmt.Sprintf(format, err)
	styled := display.Red(message)
	fmt.Print(styled)
	processExit(1)
}

func npmPolicyArgs(mgr *manager.Manager, args, packageArgs []string) []string {
	skipNPMPolicy := mgr == nil || mgr.Ecosystem != "npm"
	if skipNPMPolicy {
		return packageArgs
	}
	sourceArgs := npmSourceArgs(args)
	return append(sourceArgs, packageArgs...)
}

func npmSourceArgs(args []string) []string {
	var sourceArgs []string
	for index := 0; index < len(args); index++ {
		_, value, found := npmSourceFlagAt(args, index)
		if !found {
			continue
		}
		sourceArgs = append(sourceArgs, args[index])
		if !strings.Contains(args[index], "=") {
			sourceArgs = append(sourceArgs, value)
			index++
		}
	}
	return sourceArgs
}

func unknownInstallTargetError(mgr *manager.Manager, args, packageArgs []string) error {
	if mgr == nil {
		return nil
	}
	commandIndex := managerCommandIndex(mgr, args)
	if commandIndex < 0 {
		return nil
	}
	command := args[commandIndex]
	commandArgs := args[commandIndex+1:]
	if commandInstallsUnknownVersions(mgr, command, commandArgs, packageArgs) {
		return fmt.Errorf("%s %s cannot be scanned before it resolves new versions", mgr.Name, command)
	}
	return nil
}

func commandInstallsUnknownVersions(mgr *manager.Manager, command string, commandArgs, packageArgs []string) bool {
	switch mgr.Name {
	case "brew":
		return brewInstallsUnknownVersions(mgr, command, packageArgs)
	case "npm", "pnpm", "bun":
		return command == "update"
	case "go":
		return goInstallsUnknownVersions(command, packageArgs)
	case "poetry":
		return command == "update"
	case "cargo":
		return cargoInstallsUnknownVersions(mgr, command, commandArgs, packageArgs)
	default:
		return false
	}
}

func brewInstallsUnknownVersions(mgr *manager.Manager, command string, packageArgs []string) bool {
	if command != "upgrade" {
		return false
	}
	return len(extractPackages(mgr, packageArgs)) == 0
}

func goInstallsUnknownVersions(command string, packageArgs []string) bool {
	if command != "get" {
		return false
	}
	return hasGoUpdateFlag(packageArgs)
}

func cargoInstallsUnknownVersions(mgr *manager.Manager, command string, commandArgs, packageArgs []string) bool {
	if command != "update" {
		return false
	}
	hasPrecisePackage := len(cargoUpdatePackages(mgr, packageArgs)) > 0
	if hasPrecisePackage {
		return false
	}
	targets := cargoUpdateTargets(mgr, commandArgs)
	return len(targets) == 0
}

func hasGoUpdateFlag(args []string) bool {
	for _, arg := range args {
		isShortUpdate := arg == "-u"
		isLongUpdate := strings.HasPrefix(arg, "-u=")
		if isShortUpdate {
			return true
		}
		if isLongUpdate {
			return true
		}
	}
	return false
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
	return slices.ContainsFunc(results, hasCriticalVulns)
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
	if len(result.vulns) > 0 {
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
		needsFullOutput := len(r.vulns) > 0 || r.err != nil
		if needsFullOutput {
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
	unresolved := version == "" || shouldResolveVersion(mgr.Ecosystem, version)
	if unresolved {
		return false
	}
	if !isExactVersion(mgr.Ecosystem, version) {
		return false
	}
	return cache.Hit(c, cache.Key(mgr.Ecosystem, name, version))
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
	confirmed := answer == "y" || answer == "yes"
	return confirmed
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
