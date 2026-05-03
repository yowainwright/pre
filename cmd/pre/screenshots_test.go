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
