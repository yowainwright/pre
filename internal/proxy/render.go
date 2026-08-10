package proxy

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/yowainwright/pre/internal/display"
)

func renderTree(ecosystem string, results []scanResult) string {
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

	logo := display.Logo()
	safeEcosystem := terminalText(ecosystem)
	headerText := fmt.Sprintf("checking %d package(s) (%s)", len(results), safeEcosystem)
	header := display.Cyan(display.IconInfo) + " " + display.Cyan(headerText)
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
			label := terminalText(r.label)
			id := terminalText(v.ID)
			severity := terminalText(v.Severity)
			line := fmt.Sprintf("%-30s %s%s  %s", label, id, score, severity)
			lines = append(lines, line)
		}
	}
	return display.Box(display.Red("Critical"), lines) + "\n"
}

func nodeLabel(r scanResult, maxLen int) string {
	label := terminalText(r.label)
	padded := display.Pad(label, maxLen)
	return display.Bold(padded) + "  " + nodeStatus(r)
}

func nodeStatus(r scanResult) string {
	switch {
	case r.err != nil:
		icon := display.Yellow(display.IconWarning)
		message := terminalText(r.err.Error())
		return icon + " " + message
	case len(r.vulns) > 0:
		icon := display.Red(display.IconError)
		count := fmt.Sprintf("%d vulnerabilit(ies)", len(r.vulns))
		message := display.Red(count)
		return icon + " " + message
	case r.cached:
		icon := display.Green(display.IconSuccess)
		message := display.Dim("clean (cached)")
		return icon + " " + message
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
	if unicode.IsControl(char) || unicode.In(char, unicode.Cf) {
		return '�'
	}
	return char
}
