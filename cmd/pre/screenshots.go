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
		if args[0] == "--help" || args[0] == "-h" {
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

	search := newManageUI(inv)
	search.mode = modeSearch
	search.search = "react"
	search.applyFilter()

	managers := newManageUI(inv)
	managers.mode = modeManagers
	managers.managerSelected = managerIndex(managers.managerOptions, "npm")

	actions := newManageUI(inv)
	actions.selected = packageIndex(actions.filtered, "react")
	actions.mode = modeDialog

	install := newManageUI(inv)
	install.beginInput(inputInstallPackage, "package")
	install.installManager = "npm"
	install.inputValue = "react@latest"

	return []screenshotCase{
		{Name: "manage-list", UI: list},
		{Name: "manage-search", UI: search},
		{Name: "manage-managers", UI: managers},
		{Name: "manage-actions", UI: actions},
		{Name: "manage-install", UI: install},
	}
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
	fmt.Fprintf(&out, `<svg xmlns="http://www.w3.org/2000/svg" width="%.0f" height="%.0f" viewBox="0 0 %.0f %.0f" role="img" aria-label="%s" xml:space="preserve">`+"\n",
		width, height, width, height, html.EscapeString(title))
	fmt.Fprintln(&out, `<rect width="100%" height="100%" fill="#1e1e2e"/>`)
	fmt.Fprintln(&out, `<style>text{font-family:"SFMono-Regular","Menlo","Consolas","Liberation Mono",monospace;font-size:14px;dominant-baseline:text-before-edge}</style>`)
	for row := 0; row < rows; row++ {
		line := ""
		if row < len(lines) {
			line = lines[row]
		}
		spans := ansiLineSpans(line)
		y := screenshotPad + float64(row)*screenshotLineHeight
		for _, span := range spans {
			x := screenshotPad + float64(span.start)*screenshotCharWidth
			cells := utf8.RuneCountInString(span.text)
			if span.style.bg != "" && cells > 0 {
				fmt.Fprintf(&out, `<rect x="%.1f" y="%.1f" width="%.1f" height="%.1f" fill="%s"/>`+"\n",
					x, y-2, float64(cells)*screenshotCharWidth, screenshotLineHeight, span.style.bg)
			}
		}
		for _, span := range spans {
			if span.text == "" {
				continue
			}
			x := screenshotPad + float64(span.start)*screenshotCharWidth
			weight := "400"
			if span.style.bold {
				weight = "700"
			}
			fmt.Fprintf(&out, `<text x="%.1f" y="%.1f" fill="%s" font-weight="%s">%s</text>`+"\n",
				x, y, span.style.fg, weight, html.EscapeString(span.text))
		}
	}
	fmt.Fprintln(&out, `</svg>`)
	return out.String()
}

func ansiLineSpans(line string) []svgSpan {
	style := svgStyle{fg: "#cdd6f4"}
	var spans []svgSpan
	var text strings.Builder
	start := 0
	col := 0
	flush := func() {
		if text.Len() == 0 {
			return
		}
		spans = append(spans, svgSpan{text: text.String(), start: start, style: style})
		text.Reset()
	}

	for i := 0; i < len(line); {
		if line[i] == '\x1b' && i+1 < len(line) && line[i+1] == '[' {
			end := i + 2
			for end < len(line) && (line[end] < '@' || line[end] > '~') {
				end++
			}
			if end >= len(line) {
				break
			}
			final := line[end]
			seq := line[i+2 : end]
			i = end + 1
			if final == 'm' {
				flush()
				style = applyANSIStyle(style, seq)
				start = col
			}
			continue
		}
		r, size := utf8.DecodeRuneInString(line[i:])
		if r == utf8.RuneError && size == 0 {
			break
		}
		if text.Len() == 0 {
			start = col
		}
		text.WriteRune(r)
		col++
		i += size
	}
	flush()
	return spans
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
		switch code {
		case 0:
			style = svgStyle{fg: "#cdd6f4"}
		case 1:
			style.bold = true
		case 22:
			style.bold = false
		case 38, 48:
			isBG := code == 48
			if i+4 < len(parts) && parts[i+1] == "2" {
				color, ok := trueColor(parts[i+2], parts[i+3], parts[i+4])
				if ok {
					if isBG {
						style.bg = color
					} else {
						style.fg = color
					}
				}
				i += 4
			}
		case 39:
			style.fg = "#cdd6f4"
		case 49:
			style.bg = ""
		}
	}
	return style
}

func trueColor(r, g, b string) (string, bool) {
	red, errR := strconv.Atoi(r)
	green, errG := strconv.Atoi(g)
	blue, errB := strconv.Atoi(b)
	if errR != nil || errG != nil || errB != nil {
		return "", false
	}
	if red < 0 || red > 255 || green < 0 || green > 255 || blue < 0 || blue > 255 {
		return "", false
	}
	return fmt.Sprintf("#%02x%02x%02x", red, green, blue), true
}
