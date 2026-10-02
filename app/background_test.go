package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestOnBlack(t *testing.T) {
	in := "\x1b[1mtitle\x1b[0m plain\n\x1b[38;5;49mgreen\x1b[49m\x1b[m end"
	out := onBlack(in, 20, 3)
	if again := onBlack(out, 20, 3); again != out {
		t.Errorf("not idempotent:\n%q\n%q", out, again)
	}
	lines := strings.Split(out, "\n")
	if len(lines) != 3 {
		t.Fatalf("%d lines, want 3", len(lines))
	}
	for i, l := range lines {
		if w := ansi.StringWidth(l); w != 20 {
			t.Errorf("line %d is %d wide, want 20", i, w)
		}
		if !strings.HasPrefix(l, "\x1b["+blackBG+"m") {
			t.Errorf("line %d doesn't start on black: %q", i, l)
		}
	}
	if strings.Contains(out, "\x1b[0m") || strings.Contains(out, "\x1b[49m") || strings.Contains(out, "\x1b[m") {
		t.Errorf("a reset back to the terminal's background is left: %q", out)
	}
	if !strings.Contains(out, "\x1b[38;5;49m") {
		t.Errorf("colour 38;5;49 (a foreground) was changed: %q", out)
	}
}

func TestSofterDiffColours(t *testing.T) {
	if out := onBlack("\x1b[48;5;228mx", 5, 1); !strings.Contains(out, "48;5;228m") {
		t.Errorf("colour 228 was changed: %q", out)
	}
	out := onBlack("\x1b[48;5;22m+added\x1b[0m", 10, 1)
	if strings.Contains(out, "48;5;22m") || !strings.Contains(out, "48;2;34;92;43") {
		t.Errorf("diff green not softened: %q", out)
	}
}
