package main

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestHandleScreenshotsWritesSVGs(t *testing.T) {
	dir := t.TempDir()
	var out, errOut bytes.Buffer
	code := handleScreenshots([]string{dir}, &out, &errOut)
	if code != 0 {
		t.Fatalf("expected screenshots exit 0, got %d: %s", code, errOut.String())
	}
	for _, name := range []string{"manage-list", "manage-search", "manage-managers", "manage-actions", "manage-install"} {
		assertScreenshotSVG(t, dir, name)
	}
	if !strings.Contains(out.String(), "wrote TUI screenshots") {
		t.Fatalf("expected output message, got %q", out.String())
	}
}

func assertScreenshotSVG(t *testing.T, dir, name string) {
	t.Helper()
	path := filepath.Join(dir, name+".svg")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("expected screenshot %s: %v", path, err)
	}
	text := string(data)
	validSVG := strings.Contains(text, "<svg") && strings.Contains(text, "pre manage")
	if !validSVG {
		t.Fatalf("expected terminal svg for %s, got %q", name, text)
	}
}

func TestHandleScreenshotsHelp(t *testing.T) {
	var out, errOut bytes.Buffer
	code := handleScreenshots([]string{"--help"}, &out, &errOut)
	if code != 0 {
		t.Fatalf("expected help exit 0, got %d: %s", code, errOut.String())
	}
	if !strings.Contains(out.String(), "usage: pre screenshots") {
		t.Fatalf("expected screenshot usage, got %q", out.String())
	}
}

func TestScreenshotANSIToSVGPreservesColors(t *testing.T) {
	svg := ansiToTerminalSVG("test", "\033[1;38;2;1;2;3;48;2;4;5;6mhi\033[0m\n", 10, 2)
	for _, want := range []string{"#010203", "#040506", "font-weight=\"700\"", ">hi</text>"} {
		if !strings.Contains(svg, want) {
			t.Fatalf("expected SVG to contain %q, got %s", want, svg)
		}
	}
	color, ok := trueColor("999", "2", "3")
	acceptedInvalid := ok || color != ""
	if acceptedInvalid {
		t.Fatalf("expected invalid truecolor to fail, got %q %v", color, ok)
	}
}

func TestManagerIndexNotFound(t *testing.T) {
	if got := managerIndex([]string{"brew", "npm"}, "missing"); got != 0 {
		t.Errorf("expected 0 for missing manager, got %d", got)
	}
}

func TestPackageIndexNotFound(t *testing.T) {
	pkgs := []installedPackage{{Name: "react"}, {Name: "vite"}}
	if got := packageIndex(pkgs, "missing"); got != 0 {
		t.Errorf("expected 0 for missing package, got %d", got)
	}
}

func TestApplyANSIStyleReset(t *testing.T) {
	reset := applyANSIStyle(svgStyle{fg: "#ff0000", bold: true}, "")
	wrongReset := reset.fg != "#cdd6f4" || reset.bold
	if wrongReset {
		t.Errorf("expected reset on empty seq, got %+v", reset)
	}
}

func TestApplyANSIStyleBold(t *testing.T) {
	base := svgStyle{fg: "#cdd6f4"}
	bold := applyANSIStyle(base, "1")
	if !bold.bold {
		t.Errorf("expected bold=true, got %+v", bold)
	}
	unbold := applyANSIStyle(bold, "22")
	if unbold.bold {
		t.Errorf("expected bold=false after 22, got %+v", unbold)
	}
}

func TestApplyANSIStyleDefaultColors(t *testing.T) {
	defaultFG := applyANSIStyle(svgStyle{fg: "#ff0000"}, "39")
	if defaultFG.fg != "#cdd6f4" {
		t.Errorf("expected default fg after 39, got %q", defaultFG.fg)
	}

	withBG := applyANSIStyle(svgStyle{bg: "#ff0000"}, "49")
	if withBG.bg != "" {
		t.Errorf("expected cleared bg after 49, got %q", withBG.bg)
	}
}

func TestApplyANSIStyleTrueColors(t *testing.T) {
	base := svgStyle{fg: "#cdd6f4"}
	fgTrue := applyANSIStyle(base, "38;2;10;20;30")
	if fgTrue.fg != "#0a141e" {
		t.Errorf("expected truecolor fg, got %q", fgTrue.fg)
	}

	bgTrue := applyANSIStyle(base, "48;2;10;20;30")
	if bgTrue.bg != "#0a141e" {
		t.Errorf("expected truecolor bg, got %q", bgTrue.bg)
	}
}

func TestApplyANSIStyleInvalidCode(t *testing.T) {
	base := svgStyle{fg: "#cdd6f4"}
	unchanged := applyANSIStyle(base, "notanumber")
	if unchanged != base {
		t.Errorf("expected invalid code to leave style unchanged, got %+v", unchanged)
	}
}

func TestApplyANSIStyleMalformedColors(t *testing.T) {
	base := svgStyle{fg: "#cdd6f4"}
	want := svgStyle{fg: "#cdd6f4", bold: true}
	for _, seq := range []string{"38;2;bad;2;3;1", "48;2;-1;2;3;1", "38;2;1"} {
		if got := applyANSIStyle(base, seq); got != want {
			t.Errorf("%q: got %+v, want %+v", seq, got, want)
		}
	}
}

func TestANSILineSpansControlSequences(t *testing.T) {
	base := svgStyle{fg: "#cdd6f4"}
	bold := svgStyle{fg: "#cdd6f4", bold: true}
	cases := []struct {
		name, line string
		want       []svgSpan
	}{
		{"unicode", "hé", []svgSpan{{text: "hé", style: base}}},
		{"incomplete CSI", "a\x1b[38;2", []svgSpan{{text: "a", style: base}}},
		{"nonstyle CSI", "a\x1b[2Kb", []svgSpan{{text: "ab", style: base}}},
		{"bare ESC", "a\x1b", []svgSpan{{text: "a\x1b", style: base}}},
		{"invalid UTF8", "\xff", []svgSpan{{text: "\ufffd", style: base}}},
		{"reset", "\x1b[1mé\x1b[0mx", []svgSpan{{text: "é", style: bold}, {text: "x", start: 1, style: base}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ansiLineSpans(tc.line); !slices.Equal(got, tc.want) {
				t.Errorf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestHandleScreenshotsMkdirError(t *testing.T) {
	var out, errOut bytes.Buffer
	code := handleScreenshots([]string{"/dev/null/cannot"}, &out, &errOut)
	if code != 1 {
		t.Errorf("expected exit 1 on mkdir error, got %d", code)
	}
}
