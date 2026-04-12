package display

import (
	"strings"
	"testing"
)

func TestColorSupportedNoColor(t *testing.T) {
	t.Setenv("NO_COLOR", "1")
	if colorSupported() {
		t.Error("expected false when NO_COLOR is set")
	}
}

func TestColorizeDisabled(t *testing.T) {
	orig := ColorEnabled
	ColorEnabled = false
	defer func() { ColorEnabled = orig }()

	if Bold("x") != "x" {
		t.Error("expected plain text when color disabled")
	}
}

func TestColorizeEnabled(t *testing.T) {
	orig := ColorEnabled
	ColorEnabled = true
	defer func() { ColorEnabled = orig }()

	if Bold("x") == "x" {
		t.Error("expected ANSI codes when color enabled")
	}
	if !strings.Contains(Bold("x"), "x") {
		t.Error("expected original string to be present")
	}
}

func TestAllColorFunctions(t *testing.T) {
	orig := ColorEnabled
	ColorEnabled = true
	defer func() { ColorEnabled = orig }()

	for _, fn := range []func(string) string{Bold, Dim, Red, Green, Yellow, Cyan} {
		result := fn("test")
		if !strings.Contains(result, "test") {
			t.Errorf("color function dropped content")
		}
		if !strings.HasPrefix(result, "\033[") {
			t.Errorf("expected ANSI prefix")
		}
		if !strings.HasSuffix(result, cReset) {
			t.Errorf("expected reset suffix")
		}
	}
}

func TestWidthFromColumns(t *testing.T) {
	t.Setenv("COLUMNS", "120")
	if Width() != 120 {
		t.Errorf("expected 120, got %d", Width())
	}
}

func TestWidthInvalidColumns(t *testing.T) {
	t.Setenv("COLUMNS", "notanumber")
	if Width() != 80 {
		t.Errorf("expected default 80, got %d", Width())
	}
}

func TestWidthDefault(t *testing.T) {
	t.Setenv("COLUMNS", "")
	if Width() != 80 {
		t.Errorf("expected default 80, got %d", Width())
	}
}

func TestPadShort(t *testing.T) {
	result := Pad("hi", 10)
	if len(result) != 10 {
		t.Errorf("expected length 10, got %d", len(result))
	}
	if !strings.HasPrefix(result, "hi") {
		t.Error("expected original string at start")
	}
}

func TestPadExact(t *testing.T) {
	result := Pad("hello", 5)
	if result != "hello" {
		t.Errorf("expected no padding, got %q", result)
	}
}

func TestPadLonger(t *testing.T) {
	result := Pad("toolong", 3)
	if result != "toolong" {
		t.Errorf("expected untruncated string, got %q", result)
	}
}

func TestBoxStructure(t *testing.T) {
	orig := ColorEnabled
	ColorEnabled = false
	defer func() { ColorEnabled = orig }()

	result := Box("title", []string{"line one", "line two"})
	lines := strings.Split(result, "\n")

	if !strings.HasPrefix(lines[0], "┌─ title") {
		t.Errorf("unexpected top border: %q", lines[0])
	}
	if !strings.HasSuffix(lines[0], "┐") {
		t.Errorf("top border missing closing char: %q", lines[0])
	}
	if !strings.HasPrefix(lines[1], "│ ") {
		t.Errorf("content line missing left border: %q", lines[1])
	}
	if !strings.HasSuffix(lines[len(lines)-1], "┘") {
		t.Errorf("bottom border missing closing char: %q", lines[len(lines)-1])
	}
}

func TestBoxWidthConsistency(t *testing.T) {
	orig := ColorEnabled
	ColorEnabled = false
	defer func() { ColorEnabled = orig }()

	result := Box("header", []string{"short", "a much longer line here"})
	lines := strings.Split(result, "\n")

	width := len([]rune(lines[0]))
	for i, line := range lines {
		if len([]rune(line)) != width {
			t.Errorf("line %d has inconsistent width: got %d want %d: %q", i, len([]rune(line)), width, line)
		}
	}
}

func TestBoxLongLine(t *testing.T) {
	orig := ColorEnabled
	ColorEnabled = false
	defer func() { ColorEnabled = orig }()

	longLine := strings.Repeat("x", 60)
	result := Box("hdr", []string{longLine})
	if !strings.Contains(result, longLine) {
		t.Error("expected long line to be present in box")
	}
}

func TestTreeSingleNode(t *testing.T) {
	orig := ColorEnabled
	ColorEnabled = false
	defer func() { ColorEnabled = orig }()

	result := Tree([]TreeNode{{Label: "react@18.0.0  clean"}})
	if !strings.Contains(result, "└── ") {
		t.Errorf("expected last-node char, got %q", result)
	}
	if !strings.Contains(result, "react@18.0.0") {
		t.Error("expected label in output")
	}
}

func TestTreeMultipleNodes(t *testing.T) {
	orig := ColorEnabled
	ColorEnabled = false
	defer func() { ColorEnabled = orig }()

	nodes := []TreeNode{
		{Label: "react@18.0.0  clean"},
		{Label: "lodash@4.17.4  2 vulnerabilities"},
	}
	result := Tree(nodes)
	if !strings.Contains(result, "├── ") {
		t.Errorf("expected middle-node char, got %q", result)
	}
	if !strings.Contains(result, "└── ") {
		t.Errorf("expected last-node char, got %q", result)
	}
}

func TestTreeWithChildren(t *testing.T) {
	orig := ColorEnabled
	ColorEnabled = false
	defer func() { ColorEnabled = orig }()

	nodes := []TreeNode{
		{
			Label:    "lodash@4.17.4  2 vulnerabilities",
			Children: []string{"CVE-2021-23337  command injection", "CVE-2020-8203  prototype pollution"},
		},
	}
	result := Tree(nodes)
	if !strings.Contains(result, "CVE-2021-23337") {
		t.Error("expected first child in output")
	}
	if !strings.Contains(result, "CVE-2020-8203") {
		t.Error("expected last child in output")
	}
	if strings.Count(result, "└── ") < 1 {
		t.Error("expected at least one last-item char")
	}
}

func TestTreeEmpty(t *testing.T) {
	result := Tree([]TreeNode{})
	if result != "" {
		t.Errorf("expected empty output for empty nodes, got %q", result)
	}
}

func TestPromptContainsQuestionAndHint(t *testing.T) {
	orig := ColorEnabled
	ColorEnabled = false
	defer func() { ColorEnabled = orig }()

	result := Prompt("Install?")
	if !strings.Contains(result, "Install?") {
		t.Error("expected prompt to contain question")
	}
	if !strings.Contains(result, "?") {
		t.Error("expected prompt to contain question mark indicator")
	}
	if !strings.Contains(result, "[y/N]") {
		t.Error("expected prompt to contain [y/N] hint")
	}
}

func TestPromptPlainText(t *testing.T) {
	orig := ColorEnabled
	ColorEnabled = false
	defer func() { ColorEnabled = orig }()

	if Prompt("Install?") != "? Install? [y/N] " {
		t.Errorf("unexpected plain prompt: %q", Prompt("Install?"))
	}
}

func TestPromptColorEnabled(t *testing.T) {
	orig := ColorEnabled
	ColorEnabled = true
	defer func() { ColorEnabled = orig }()

	result := Prompt("Install?")
	if !strings.Contains(result, "Install?") {
		t.Error("expected question in colored prompt")
	}
	if !strings.HasPrefix(result, "\033[") {
		t.Error("expected ANSI prefix when color enabled")
	}
}
