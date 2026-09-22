package display

import (
	"os"
	"strings"
	"unicode/utf8"
)

var ColorEnabled = colorSupported()

func colorSupported() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	fi, _ := os.Stdout.Stat()
	isTerminal := fi != nil && (fi.Mode()&os.ModeCharDevice != 0)
	return isTerminal
}

const (
	cReset       = "\033[0m"
	cBold        = "\033[1m"
	cDim         = "\033[2m"
	cRed         = "\033[31m"
	cGreen       = "\033[32m"
	cYellow      = "\033[33m"
	cCyan        = "\033[36m"
	cFluoYellow  = "\033[38;5;226m"
	cBrightRed   = "\033[91m"
	cOrange      = "\033[38;5;208m"
	cLightGray   = "\033[37m"
	cBrightWhite = "\033[97m"

	IconSuccess = "●"
	IconError   = "■"
	IconWarning = "▲"
	IconInfo    = "◆"
	IconUp      = "⬆"
)

func colorize(code, s string) string {
	if !ColorEnabled {
		return s
	}
	colored := code + s + cReset
	return colored
}

func Bold(s string) string        { return colorize(cBold, s) }
func Dim(s string) string         { return colorize(cDim, s) }
func Red(s string) string         { return colorize(cRed, s) }
func Green(s string) string       { return colorize(cGreen, s) }
func Yellow(s string) string      { return colorize(cYellow, s) }
func Cyan(s string) string        { return colorize(cCyan, s) }
func FluoYellow(s string) string  { return colorize(cFluoYellow, s) }
func BrightRed(s string) string   { return colorize(cBrightRed, s) }
func Orange(s string) string      { return colorize(cOrange, s) }
func LightGray(s string) string   { return colorize(cLightGray, s) }
func BrightWhite(s string) string { return colorize(cBrightWhite, s) }

func Logo() string {
	logo := FluoYellow("PRE") + BrightRed("≋") + Orange("≈") + Yellow("~") + LightGray("∿")
	return logo
}

type TreeNode struct {
	Label    string
	Children []string
}

func Tree(nodes []TreeNode) string {
	var sb strings.Builder
	for i, node := range nodes {
		isLast := i == len(nodes)-1
		writeTreeNode(&sb, node, isLast)
	}
	return sb.String()
}

func writeTreeNode(sb *strings.Builder, node TreeNode, isLast bool) {
	branch := "├── "
	continuation := "│   "
	if isLast {
		branch = "└── "
		continuation = "    "
	}
	sb.WriteString(Dim(branch) + node.Label + "\n")
	writeTreeChildren(sb, node.Children, continuation)
}

func writeTreeChildren(sb *strings.Builder, children []string, continuation string) {
	for j, child := range children {
		branch := "├── "
		if j == len(children)-1 {
			branch = "└── "
		}
		sb.WriteString(Dim(continuation) + Dim(branch) + child + "\n")
	}
}

func HRule(width int) string {
	return Dim(strings.Repeat("─", width))
}

func Prompt(question string) string {
	parts := []string{Cyan("?"), Bold(question), Dim("[y/N]"), ""}
	return strings.Join(parts, " ")
}

func Pad(s string, width int) string {
	n := utf8.RuneCountInString(s)
	if n >= width {
		return s
	}
	padding := strings.Repeat(" ", width-n)
	return s + padding
}

func boxInnerWidth(header string, lines []string) int {
	inner := utf8.RuneCountInString(header) + 2
	for _, l := range lines {
		if n := utf8.RuneCountInString(l); n > inner {
			inner = n
		}
	}
	return inner
}

func boxTop(header string, inner int) string {
	dashCount := inner + 4 - utf8.RuneCountInString(header) - 5
	border := "┌─ " + header + " " + strings.Repeat("─", dashCount) + "┐"
	return border
}

func boxBottom(inner int) string {
	dashes := strings.Repeat("─", inner+2)
	border := "└" + dashes + "┘"
	return border
}

func Box(header string, lines []string) string {
	inner := boxInnerWidth(header, lines)

	var sb strings.Builder
	sb.WriteString(Yellow(boxTop(header, inner)) + "\n")
	for _, line := range lines {
		sb.WriteString(Yellow("│ "+Pad(line, inner)+" │") + "\n")
	}
	sb.WriteString(Yellow(boxBottom(inner)))

	return sb.String()
}
