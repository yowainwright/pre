package display

import (
	"os"
	"strconv"
	"strings"
	"unicode/utf8"
)

var ColorEnabled = colorSupported()

func colorSupported() bool {
	if os.Getenv("NO_COLOR") != "" {
		return false
	}
	fi, _ := os.Stdout.Stat()
	return fi != nil && (fi.Mode()&os.ModeCharDevice != 0)
}

const (
	cReset       = "\033[0m"
	cBold        = "\033[1m"
	cDim         = "\033[2m"
	cRed         = "\033[31m"
	cGreen       = "\033[32m"
	cYellow      = "\033[33m"
	cCyan        = "\033[36m"
	cSkyBlue     = "\033[96m"
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
	return code + s + cReset
}

func Bold(s string) string        { return colorize(cBold, s) }
func Dim(s string) string         { return colorize(cDim, s) }
func Red(s string) string         { return colorize(cRed, s) }
func Green(s string) string       { return colorize(cGreen, s) }
func Yellow(s string) string      { return colorize(cYellow, s) }
func Cyan(s string) string        { return colorize(cCyan, s) }
func SkyBlue(s string) string     { return colorize(cSkyBlue, s) }
func FluoYellow(s string) string  { return colorize(cFluoYellow, s) }
func BrightRed(s string) string   { return colorize(cBrightRed, s) }
func Orange(s string) string      { return colorize(cOrange, s) }
func LightGray(s string) string   { return colorize(cLightGray, s) }
func BrightWhite(s string) string { return colorize(cBrightWhite, s) }

func Logo() string {
	return FluoYellow("PRE") + BrightRed("≋") + Orange("≈") + Yellow("~") + LightGray("∿")
}

type TreeNode struct {
	Label    string
	Children []string
}

func Tree(nodes []TreeNode) string {
	var sb strings.Builder
	for i, node := range nodes {
		isLast := i == len(nodes)-1
		branch := "├── "
		continuation := "│   "
		if isLast {
			branch = "└── "
			continuation = "    "
		}
		sb.WriteString(Dim(branch) + node.Label + "\n")
		for j, child := range node.Children {
			childBranch := "├── "
			if j == len(node.Children)-1 {
				childBranch = "└── "
			}
			sb.WriteString(Dim(continuation) + Dim(childBranch) + child + "\n")
		}
	}
	return sb.String()
}

func HRule(width int) string {
	return Dim(strings.Repeat("─", width))
}

func Prompt(question string) string {
	return Cyan("?") + " " + Bold(question) + " " + Dim("[y/N]") + " "
}

func Width() int {
	if cols := os.Getenv("COLUMNS"); cols != "" {
		if n, err := strconv.Atoi(cols); err == nil && n > 0 {
			return n
		}
	}
	return 80
}

func Pad(s string, width int) string {
	n := utf8.RuneCountInString(s)
	if n >= width {
		return s
	}
	return s + strings.Repeat(" ", width-n)
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
	return "┌─ " + header + " " + strings.Repeat("─", dashCount) + "┐"
}

func boxBottom(inner int) string {
	return "└" + strings.Repeat("─", inner+2) + "┘"
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
