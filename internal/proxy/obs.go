package proxy

import (
	"slices"
	"strings"
	"time"

	"github.com/yowainwright/pre/internal/manager"
	"github.com/yowainwright/pre/internal/obs"
)

func recordProxyEvent(name string, mgr *manager.Manager, args []string, attrs map[string]any) {
	base := baseProxyAttrs(mgr, args)
	for key, value := range attrs {
		base[key] = value
	}
	obs.Record(name, base)
}

type proxyRun struct {
	mgr   *manager.Manager
	args  []string
	start time.Time
}

func (run proxyRun) record(name string, attrs map[string]any) {
	recordProxyEvent(name, run.mgr, run.args, attrs)
}

func (run proxyRun) recordDecision(name, decision, reason string, extra map[string]any) {
	attrs := map[string]any{
		"decision":    decision,
		"reason":      reason,
		"duration_ms": durationMillis(run.start),
	}
	for key, value := range extra {
		attrs[key] = value
	}
	run.record(name, attrs)
}

func (run proxyRun) recordBlock(reason string, err error) {
	run.recordDecision("pre.scan.blocked", "blocked", reason, map[string]any{
		"error_type": obs.ErrorType(err),
	})
}

func baseProxyAttrs(mgr *manager.Manager, args []string) map[string]any {
	return map[string]any{
		"manager":         mgr.Name,
		"ecosystem":       mgr.Ecosystem,
		"manager_command": managerCommandCategory(mgr, args),
		"arg_count":       len(args),
	}
}

func managerCommandCategory(mgr *manager.Manager, args []string) string {
	known := managerCommands(mgr)
	for _, arg := range args {
		if strings.HasPrefix(arg, "-") {
			continue
		}
		if slices.Contains(known, arg) {
			return arg
		}
		return "argument"
	}
	return "none"
}

func managerCommands(mgr *manager.Manager) []string {
	commands := []string{"run", "test", "publish", "remove", "uninstall"}
	return append(commands, mgr.InstallCmds...)
}

func durationMillis(start time.Time) int64 {
	return time.Since(start).Milliseconds()
}

type scanCounts struct {
	cached          int
	vulnerabilities int
	criticals       int
	errors          int
}

func countScanResults(results []scanResult) scanCounts {
	counts := scanCounts{}
	for _, result := range results {
		if result.cached {
			counts.cached++
		}
		counts.vulnerabilities += len(result.vulns)
		if hasCriticalVulns(result) {
			counts.criticals++
		}
		if result.err != nil {
			counts.errors++
		}
	}
	return counts
}

func scanResultAttrs(results []scanResult, counts scanCounts) map[string]any {
	return map[string]any{
		"package_count":       len(results),
		"cached_count":        counts.cached,
		"vulnerability_count": counts.vulnerabilities,
		"critical_count":      counts.criticals,
		"error_count":         counts.errors,
	}
}

func recordManagerExec(name string, args []string, start time.Time, exitCode int) {
	obs.Record("pre.manager.exec.completed", map[string]any{
		"manager":     name,
		"arg_count":   len(args),
		"duration_ms": durationMillis(start),
		"exit_code":   exitCode,
		"success":     exitCode == 0,
	})
}

func recordManagerExecStarted(name string, args []string) {
	obs.Record("pre.manager.exec.started", map[string]any{
		"manager":   name,
		"arg_count": len(args),
	})
}
