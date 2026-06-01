package main

import (
	"bytes"
	"os"
	"path/filepath"
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
		path := filepath.Join(dir, name+".svg")
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("expected screenshot %s: %v", path, err)
		}
		text := string(data)
		if !strings.Contains(text, "<svg") || !strings.Contains(text, "pre manage") {
			t.Fatalf("expected terminal svg for %s, got %q", name, text)
		}
	}
	if !strings.Contains(out.String(), "wrote TUI screenshots") {
		t.Fatalf("expected output message, got %q", out.String())
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
	if color, ok := trueColor("999", "2", "3"); ok || color != "" {
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

func TestApplyANSIStyleBranches(t *testing.T) {
	base := svgStyle{fg: "#cdd6f4"}

	reset := applyANSIStyle(svgStyle{fg: "#ff0000", bold: true}, "")
	if reset.fg != "#cdd6f4" || reset.bold {
		t.Errorf("expected reset on empty seq, got %+v", reset)
	}

	bold := applyANSIStyle(base, "1")
	if !bold.bold {
		t.Errorf("expected bold=true, got %+v", bold)
	}
	unbold := applyANSIStyle(bold, "22")
	if unbold.bold {
		t.Errorf("expected bold=false after 22, got %+v", unbold)
	}

	defaultFG := applyANSIStyle(svgStyle{fg: "#ff0000"}, "39")
	if defaultFG.fg != "#cdd6f4" {
		t.Errorf("expected default fg after 39, got %q", defaultFG.fg)
	}

	withBG := applyANSIStyle(svgStyle{bg: "#ff0000"}, "49")
	if withBG.bg != "" {
		t.Errorf("expected cleared bg after 49, got %q", withBG.bg)
	}

	fgTrue := applyANSIStyle(base, "38;2;10;20;30")
	if fgTrue.fg != "#0a141e" {
		t.Errorf("expected truecolor fg, got %q", fgTrue.fg)
	}

	bgTrue := applyANSIStyle(base, "48;2;10;20;30")
	if bgTrue.bg != "#0a141e" {
		t.Errorf("expected truecolor bg, got %q", bgTrue.bg)
	}

	unchanged := applyANSIStyle(base, "notanumber")
	if unchanged != base {
		t.Errorf("expected invalid code to leave style unchanged, got %+v", unchanged)
	}
}

func TestHandleScreenshotsMkdirError(t *testing.T) {
	var out, errOut bytes.Buffer
	code := handleScreenshots([]string{"/dev/null/cannot"}, &out, &errOut)
	if code != 1 {
		t.Errorf("expected exit 1 on mkdir error, got %d", code)
	}
}
