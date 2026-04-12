package proxy

import (
	"fmt"
	"strings"

	"github.com/yowainwright/pre/internal/display"
)

func renderTree(ecosystem string, results []scanResult) string {
	maxLen := 0
	for _, r := range results {
		if n := len(r.label); n > maxLen {
			maxLen = n
		}
	}

	nodes := make([]display.TreeNode, len(results))
	for i, r := range results {
		nodes[i] = display.TreeNode{
			Label:    nodeLabel(r, maxLen),
			Children: nodeChildren(r),
		}
	}

	logo := display.Logo()
	header := display.Cyan(display.IconInfo) + " " + display.Cyan(fmt.Sprintf("checking %d package(s) (%s)", len(results), ecosystem))
	sys := loadSystemStatsFn()
	return logo + "\n" + header + "\n" + display.Tree(nodes) + display.HRule(20) + "\n" + renderSummary(results) + "\n" + renderSystemLine(sys) + "\n"
}

func renderQuiet(count int) string {
	return display.Dim(fmt.Sprintf("%s %d packages clean", display.IconSuccess, count)) + "\n"
}

func renderCriticalDetail(results []scanResult) string {
	var lines []string
	for _, r := range results {
		for _, v := range r.vulns {
			if v.Severity != "CRITICAL" && v.Severity != "HIGH" {
				continue
			}
			score := ""
			if v.Score > 0 {
				score = fmt.Sprintf(" %.1f", v.Score)
			}
			lines = append(lines, fmt.Sprintf("%-30s %s%s  %s", r.label, v.ID, score, v.Severity))
		}
	}
	return display.Box(display.Red("Critical"), lines) + "\n"
}

func nodeLabel(r scanResult, maxLen int) string {
	padded := display.Pad(r.label, maxLen)
	return display.Bold(padded) + "  " + nodeStatus(r)
}

func nodeStatus(r scanResult) string {
	switch {
	case r.err != nil:
		return display.Yellow(display.IconWarning) + " " + r.err.Error()
	case len(r.vulns) > 0:
		return display.Red(display.IconError) + " " + display.Red(fmt.Sprintf("%d vulnerabilit(ies)", len(r.vulns)))
	case r.cached:
		return display.Green(display.IconSuccess) + " " + display.Dim("clean (cached)")
	default:
		return display.Green(display.IconSuccess) + " clean"
	}
}

func renderSummary(results []scanResult) string {
	var crit, warn, ups, cached int
	for _, r := range results {
		switch {
		case hasCriticalVulns(r):
			crit++
		case len(r.vulns) > 0 || r.err != nil:
			warn++
		case r.cached:
			cached++
		case r.updated:
			ups++
		}
	}
	tots := len(results)
	sep := display.Dim(" · ")
	return strings.Join([]string{
		display.Red(display.IconError) + fmt.Sprintf(" %d crit", crit),
		display.Yellow(display.IconWarning) + fmt.Sprintf(" %d warn", warn),
		display.Cyan(display.IconUp) + fmt.Sprintf(" %d ups", ups),
		display.Green(display.IconSuccess) + " " + display.BrightWhite(fmt.Sprintf("%d cached", cached)),
		fmt.Sprintf("%d tots", tots),
	}, sep)
}

func renderSystemLine(s SystemStats) string {
	if s.Total == 0 {
		return display.Dim("run 'pre setup' to enable weekly system scans")
	}
	sep := display.Dim(" · ")
	return strings.Join([]string{
		display.Red(display.IconError) + fmt.Sprintf(" %d syscrit", s.Crit),
		display.Yellow(display.IconWarning) + fmt.Sprintf(" %d syswarn", s.Warn),
		fmt.Sprintf("%d tots", s.Total),
	}, sep)
}

func nodeChildren(r scanResult) []string {
	children := make([]string, len(r.vulns))
	for i, v := range r.vulns {
		score := ""
		if v.Score > 0 {
			score = fmt.Sprintf(" %.1f", v.Score)
		}
		children[i] = fmt.Sprintf("%-20s%s  %s", v.ID, score, v.Summary)
	}
	return children
}
