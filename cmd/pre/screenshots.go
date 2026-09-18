package main

import (
	"bytes"
	"fmt"
	"html"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	screenshotCols       = 112
	screenshotRows       = 30
	screenshotCharWidth  = 8.8
	screenshotLineHeight = 18.0
	screenshotPad        = 24.0
)

type screenshotCase struct {
	Name string
	UI   manageUI
}

type svgStyle struct {
	fg   string
	bg   string
	bold bool
}

type svgSpan struct {
	text  string
	start int
	style svgStyle
}

func handleScreenshots(args []string, stdout, stderr io.Writer) int {
	outDir := filepath.Join("dist", "screenshots")
	if len(args) > 0 {
		showHelp := args[0] == "--help" || args[0] == "-h"
		if showHelp {
			fmt.Fprintln(stdout, "usage: pre screenshots [output-dir]")
			return 0
		}
		outDir = args[0]
	}
	if err := writeManageScreenshots(outDir); err != nil {
		fmt.Fprintf(stderr, "pre screenshots: %v\n", err)
		return 1
	}
	fmt.Fprintf(stdout, "wrote TUI screenshots to %s\n", outDir)
	return 0
}

func writeManageScreenshots(outDir string) error {
	if err := os.MkdirAll(outDir, 0750); err != nil {
		return err
	}
	origTerminalSize := terminalSizeFn
	terminalSizeFn = func() (int, int) { return screenshotCols, screenshotRows }
	defer func() { terminalSizeFn = origTerminalSize }()

	for _, shot := range manageScreenshotCases() {
		var buf bytes.Buffer
		ui := shot.UI
		renderManageUI(&buf, &ui)
		svg := ansiToTerminalSVG(shot.Name, buf.String(), screenshotCols, screenshotRows)
		if err := os.WriteFile(filepath.Join(outDir, shot.Name+".svg"), []byte(svg), 0600); err != nil {
			return err
		}
	}
	return nil
}

func manageScreenshotCases() []screenshotCase {
	inv := screenshotInventory()
	list := newManageUI(inv)
	search := screenshotSearchUI(inv)
	managers := screenshotManagersUI(inv)
	actions := screenshotActionsUI(inv)
	install := screenshotInstallUI(inv)
	return []screenshotCase{
		{Name: "manage-list", UI: list},
		{Name: "manage-search", UI: search},
		{Name: "manage-managers", UI: managers},
		{Name: "manage-actions", UI: actions},
		{Name: "manage-install", UI: install},
	}
}

func screenshotSearchUI(inv packageInventory) manageUI {
	search := newManageUI(inv)
	search.mode = modeSearch
	search.search = "react"
	search.applyFilter()
	return search
}

func screenshotManagersUI(inv packageInventory) manageUI {
	managers := newManageUI(inv)
	managers.mode = modeManagers
	managers.managerSelected = managerIndex(managers.managerOptions, "npm")
	return managers
}

func screenshotActionsUI(inv packageInventory) manageUI {
	actions := newManageUI(inv)
	actions.selected = packageIndex(actions.filtered, "react")
	actions.mode = modeDialog
	return actions
}

func screenshotInstallUI(inv packageInventory) manageUI {
	install := newManageUI(inv)
	install.beginInput(inputInstallPackage, "package")
	install.installManager = "npm"
	install.inputValue = "react@latest"
	return install
}

func screenshotInventory() packageInventory {
	return packageInventory{Packages: []installedPackage{
		{Manager: "brew", Ecosystem: "Homebrew", Name: "ripgrep", Version: "14.1.1"},
		{Manager: "brew", Ecosystem: "Homebrew", Name: "go", Version: "1.24.2"},
		{Manager: "brew", Ecosystem: "Homebrew", Name: "node", Version: "23.11.0"},
		{Manager: "npm", Ecosystem: "npm", Name: "react", Version: "18.2.0"},
		{Manager: "npm", Ecosystem: "npm", Name: "vite", Version: "5.4.10"},
		{Manager: "pnpm", Ecosystem: "npm", Name: "@openai/codex", Version: "0.124.0"},
		{Manager: "go", Ecosystem: "Go", Name: "golang.org/x/text", Version: "v0.24.0"},
		{Manager: "pip3", Ecosystem: "PyPI", Name: "urllib3", Version: "2.4.0"},
		{Manager: "uv", Ecosystem: "PyPI", Name: "fastapi", Version: "0.115.12"},
		{Manager: "poetry", Ecosystem: "PyPI", Name: "cleo", Version: "2.1.0"},
		{Manager: "bun", Ecosystem: "npm", Name: "typescript", Version: "5.8.3"},
		{Manager: "npm", Ecosystem: "npm", Name: "eslint", Version: "9.25.1"},
	}}
}

func managerIndex(options []string, name string) int {
	for i, option := range options {
		if option == name {
			return i
		}
	}
	return 0
}

func packageIndex(pkgs []installedPackage, name string) int {
	for i, pkg := range pkgs {
		if pkg.Name == name {
			return i
		}
	}
	return 0
}

func ansiToTerminalSVG(title, content string, cols, rows int) string {
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	if rows < len(lines) {
		rows = len(lines)
	}
	width := screenshotPad*2 + float64(cols)*screenshotCharWidth
	height := screenshotPad*2 + float64(rows)*screenshotLineHeight
	var out strings.Builder
	writeSVGHeader(&out, title, width, height)
	writeSVGRows(&out, lines, rows)
	fmt.Fprintln(&out, `</svg>`)
	return out.String()
}

func writeSVGHeader(out *strings.Builder, title string, width, height float64) {
	fmt.Fprintf(out, `<svg xmlns="http://www.w3.org/2000/svg" width="%.0f" height="%.0f" viewBox="0 0 %.0f %.0f" role="img" aria-label="%s" xml:space="preserve">`+"\n",
		width, height, width, height, html.EscapeString(title))
	fmt.Fprintln(out, `<rect width="100%" height="100%" fill="#1e1e2e"/>`)
	fmt.Fprintln(out, `<style>text{font-family:"SFMono-Regular","Menlo","Consolas","Liberation Mono",monospace;font-size:14px;dominant-baseline:text-before-edge}</style>`)
}

func writeSVGRows(out *strings.Builder, lines []string, rows int) {
	for row := 0; row < rows; row++ {
		line := ""
		if row < len(lines) {
			line = lines[row]
		}
		spans := ansiLineSpans(line)
		y := screenshotPad + float64(row)*screenshotLineHeight
		writeSVGBackgrounds(out, spans, y)
		writeSVGText(out, spans, y)
	}
}

func writeSVGBackgrounds(out *strings.Builder, spans []svgSpan, y float64) {
	for _, span := range spans {
		x := screenshotPad + float64(span.start)*screenshotCharWidth
		cells := utf8.RuneCountInString(span.text)
		hasBackground := span.style.bg != "" && cells > 0
		if hasBackground {
			fmt.Fprintf(out, `<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="%s"/>`+"\n",
				x, y-2, float64(cells)*screenshotCharWidth, screenshotLineHeight, span.style.bg)
		}
	}
}

func writeSVGText(out *strings.Builder, spans []svgSpan, y float64) {
	for _, span := range spans {
		if span.text == "" {
			continue
		}
		x := screenshotPad + float64(span.start)*screenshotCharWidth
		weight := "400"
		if span.style.bold {
			weight = "700"
		}
		fmt.Fprintf(out, `<text x="%.1f" y="%.1f" fill="%s" font-weight="%s">%s</text>`+"\n",
			x, y, span.style.fg, weight, html.EscapeString(span.text))
	}
}

type ansiLineParser struct {
	style svgStyle
	spans []svgSpan
	text  strings.Builder
	start int
	col   int
}

func ansiLineSpans(line string) []svgSpan {
	parser := ansiLineParser{style: svgStyle{fg: "#cdd6f4"}}
	for i := 0; i < len(line); {
		if strings.HasPrefix(line[i:], "\x1b[") {
			i = parser.readEscape(line, i)
			continue
		}
		size := parser.readRune(line[i:])
		if size == 0 {
			break
		}
		i += size
	}
	parser.flush()
	return parser.spans
}

func (p *ansiLineParser) flush() {
	if p.text.Len() == 0 {
		return
	}
	p.spans = append(p.spans, svgSpan{text: p.text.String(), start: p.start, style: p.style})
	p.text.Reset()
}

func (p *ansiLineParser) readEscape(line string, start int) int {
	end, ok := ansiSequenceEnd(line, start)
	if !ok {
		return len(line)
	}
	final := end - 1
	if line[final] == 'm' {
		p.flush()
		p.style = applyANSIStyle(p.style, line[start+2:final])
		p.start = p.col
	}
	return end
}

func (p *ansiLineParser) readRune(text string) int {
	r, size := utf8.DecodeRuneInString(text)
	if size == 0 {
		return 0
	}
	if p.text.Len() == 0 {
		p.start = p.col
	}
	p.text.WriteRune(r)
	p.col++
	return size
}

func applyANSIStyle(style svgStyle, seq string) svgStyle {
	if seq == "" {
		return svgStyle{fg: "#cdd6f4"}
	}
	parts := strings.Split(seq, ";")
	for i := 0; i < len(parts); i++ {
		code, err := strconv.Atoi(parts[i])
		if err != nil {
			continue
		}
		nextStyle, consumed := applyANSIStyleCode(style, code, parts[i+1:])
		style = nextStyle
		i += consumed
	}
	return style
}

func applyANSIStyleCode(style svgStyle, code int, parts []string) (svgStyle, int) {
	switch code {
	case 0:
		style = svgStyle{fg: "#cdd6f4"}
	case 1:
		style.bold = true
	case 22:
		style.bold = false
	case 38, 48:
		return applyANSITrueColor(style, code, parts)
	case 39:
		style.fg = "#cdd6f4"
	case 49:
		style.bg = ""
	}
	return style, 0
}

func applyANSITrueColor(style svgStyle, code int, parts []string) (svgStyle, int) {
	hasRGB := len(parts) >= 4 && parts[0] == "2"
	if !hasRGB {
		return style, 0
	}
	color, ok := trueColor(parts[1], parts[2], parts[3])
	if !ok {
		return style, 4
	}
	if code == 48 {
		style.bg = color
		return style, 4
	}
	style.fg = color
	return style, 4
}

func trueColor(r, g, b string) (string, bool) {
	red, validRed := colorComponent(r)
	green, validGreen := colorComponent(g)
	blue, validBlue := colorComponent(b)
	validRGB := validRed && validGreen && validBlue
	if !validRGB {
		return "", false
	}
	return fmt.Sprintf("#%02x%02x%02x", red, green, blue), true
}

func colorComponent(text string) (int, bool) {
	value, err := strconv.Atoi(text)
	inRange := value >= 0 && value <= 255
	valid := err == nil && inRange
	return value, valid
}
