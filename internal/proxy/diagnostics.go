package proxy

import (
	"slices"
	"strings"
	"time"

	"github.com/yowainwright/pre/internal/diagnostics"
	"github.com/yowainwright/pre/internal/manager"
)

func recordProxyEvent(name string, mgr *manager.Manager, args []string, attrs map[string]any) {
	base := baseProxyAttrs(mgr, args)
	for key, value := range attrs {
		base[key] = value
	}
	diagnostics.Record(name, base)
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

func scanResultAttrs(results []scanResult) map[string]any {
	attrs := map[string]any{"package_count": len(results)}
	attrs["cached_count"] = countScanCached(results)
	attrs["vulnerability_count"] = countScanVulnerabilities(results)
	attrs["critical_count"] = countScanCriticals(results)
	attrs["error_count"] = countScanErrors(results)
	return attrs
}

func countScanCached(results []scanResult) int {
	count := 0
	for _, result := range results {
		if result.cached {
			count++
		}
	}
	return count
}

func countScanVulnerabilities(results []scanResult) int {
	count := 0
	for _, result := range results {
		count += len(result.vulns)
	}
	return count
}

func countScanCriticals(results []scanResult) int {
	count := 0
	for _, result := range results {
		if hasCriticalVulns(result) {
			count++
		}
	}
	return count
}

func countScanErrors(results []scanResult) int {
	count := 0
	for _, result := range results {
		if result.err != nil {
			count++
		}
	}
	return count
}

func recordManagerExec(name string, args []string, start time.Time, exitCode int) {
	diagnostics.Record("pre.manager.exec.completed", map[string]any{
		"manager":     name,
		"arg_count":   len(args),
		"duration_ms": durationMillis(start),
		"exit_code":   exitCode,
		"success":     exitCode == 0,
	})
}

func recordManagerExecStarted(name string, args []string) {
	diagnostics.Record("pre.manager.exec.started", map[string]any{
		"manager":   name,
		"arg_count": len(args),
	})
}

func diagnosticsErrorType(err error) string {
	return diagnostics.ErrorType(err)
}
