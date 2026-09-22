package proxy

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/yowainwright/pre/internal/display"
)

func scanTreeNodes(results []scanResult) []display.TreeNode {
	maxLen := 0
	for _, r := range results {
		label := terminalText(r.label)
		if n := utf8.RuneCountInString(label); n > maxLen {
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

	return nodes
}

func renderTree(ecosystem string, results []scanResult) string {
	nodes := scanTreeNodes(results)
	logo := display.Logo()
	safeEcosystem := terminalText(ecosystem)
	headerText := fmt.Sprintf("checking %d package(s) (%s)", len(results), safeEcosystem)
	header := display.Cyan(display.IconInfo) + " " + display.Cyan(headerText)
	treeWithRule := display.Tree(nodes) + display.HRule(20)
	summary := renderSummary(results)
	return strings.Join([]string{logo, header, treeWithRule, summary, ""}, "\n")
}

func renderQuiet(count int) string {
	return display.Dim(fmt.Sprintf("%s %d packages clean", display.IconSuccess, count)) + "\n"
}

func renderCriticalDetail(results []scanResult) string {
	var lines []string
	for _, result := range results {
		lines = appendCriticalLines(lines, result)
	}
	return display.Box(display.Red("Critical"), lines) + "\n"
}

func appendCriticalLines(lines []string, r scanResult) []string {
	for _, v := range r.vulns {
		notCritical := v.Severity != "CRITICAL" && v.Severity != "HIGH"
		if notCritical {
			continue
		}
		score := ""
		if v.Score > 0 {
			score = fmt.Sprintf(" %.1f", v.Score)
		}
		label := terminalText(r.label)
		id := terminalText(v.ID)
		severity := terminalText(v.Severity)
		line := fmt.Sprintf("%-30s %s%s  %s", label, id, score, severity)
		lines = append(lines, line)
	}
	return lines
}

func nodeLabel(r scanResult, maxLen int) string {
	label := terminalText(r.label)
	padded := display.Pad(label, maxLen)
	labelWithStatus := display.Bold(padded) + "  " + nodeStatus(r)
	return labelWithStatus
}

func nodeStatus(r scanResult) string {
	switch {
	case r.err != nil:
		icon := display.Yellow(display.IconWarning)
		message := terminalText(r.err.Error())
		return strings.Join([]string{icon, message}, " ")
	case len(r.vulns) > 0:
		icon := display.Red(display.IconError)
		count := fmt.Sprintf("%d vulnerabilit(ies)", len(r.vulns))
		message := display.Red(count)
		return strings.Join([]string{icon, message}, " ")
	case r.cached:
		icon := display.Green(display.IconSuccess)
		message := display.Dim("clean (cached)")
		return strings.Join([]string{icon, message}, " ")
	default:
		icon := display.Green(display.IconSuccess)
		return icon + " clean"
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
	return formatScanSummary(crit, warn, ups, cached, len(results))
}

func formatScanSummary(crit, warn, ups, cached, tots int) string {
	sep := display.Dim(" · ")
	cachedText := display.Green(display.IconSuccess) + " " + display.BrightWhite(fmt.Sprintf("%d cached", cached))
	parts := []string{
		display.Red(display.IconError) + fmt.Sprintf(" %d crit", crit),
		display.Yellow(display.IconWarning) + fmt.Sprintf(" %d warn", warn),
		display.Cyan(display.IconUp) + fmt.Sprintf(" %d ups", ups),
		cachedText,
		fmt.Sprintf("%d tots", tots),
	}
	return strings.Join(parts, sep)
}

func nodeChildren(r scanResult) []string {
	children := make([]string, len(r.vulns))
	for i, v := range r.vulns {
		score := ""
		if v.Score > 0 {
			score = fmt.Sprintf(" %.1f", v.Score)
		}
		id := terminalText(v.ID)
		summary := terminalText(v.Summary)
		children[i] = fmt.Sprintf("%-20s%s  %s", id, score, summary)
	}
	return children
}

func terminalText(value string) string {
	return strings.Map(terminalRune, value)
}

func terminalRune(char rune) rune {
	unsafeControl := unicode.IsControl(char) || unicode.In(char, unicode.Cf)
	if unsafeControl {
		return '�'
	}
	return char
}
