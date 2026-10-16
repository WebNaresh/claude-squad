package app

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// tileView draws pane rows (20 wide) inside a tile: border, padding, pane,
// padding, border, as the grid does.
func tileView(rows ...string) string {
	var out []string
	for _, r := range rows {
		out = append(out, "│ "+r+strings.Repeat(" ", 20-utf8.RuneCountInString(r))+" │")
	}
	return strings.Join(out, "\n")
}

func TestSelectedTextJoinsWrappedRows(t *testing.T) {
	m := &home{lastView: tileView(
		"  The local files is",
		"  for you:",
		"  /private/tmp/claud",
		"  e-501/scratch/ref/",
		"  a.html",
		"",
		"  done",
	)}
	m.sel = tileSelection{x0: 1, x1: 22, y0: 0, y1: 6, sx: 1, sy: 0, ex: 22, ey: 6, shown: true}
	want := "The local files is for you:\n/private/tmp/claude-501/scratch/ref/a.html\n\ndone"
	if got := m.selectedText(); got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestSelectedTextKeepsCodeIndent(t *testing.T) {
	m := &home{lastView: tileView(
		"  if x {",
		"      y()",
		"  }",
	)}
	m.sel = tileSelection{x0: 1, x1: 22, y0: 0, y1: 2, sx: 1, sy: 0, ex: 22, ey: 2, shown: true}
	if got, want := m.selectedText(), "if x {\n    y()\n}"; got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

// The user's case: Claude moved a long path to the next row because it
// didn't fit after ">", so the first row stops short of the edge.
func TestSelectedTextJoinsWordWrappedCommand(t *testing.T) {
	m := &home{lastView: tileView(
		"  ! echo a >",
		"  /Users/me/x/.env",
		"",
		"  Then tell me.",
	)}
	m.sel = tileSelection{x0: 1, x1: 22, y0: 0, y1: 3, sx: 1, sy: 0, ex: 22, ey: 3, shown: true}
	if got, want := m.selectedText(), "! echo a > /Users/me/x/.env\n\nThen tell me."; got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestSelectedTextKeepsListItems(t *testing.T) {
	m := &home{lastView: tileView(
		"  - one",
		"  - a long one here",
		"  ⎿  done",
	)}
	m.sel = tileSelection{x0: 1, x1: 22, y0: 0, y1: 2, sx: 1, sy: 0, ex: 22, ey: 2, shown: true}
	if got, want := m.selectedText(), "- one\n- a long one here\n⎿  done"; got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}

func TestSelectedTextKeepsTableRows(t *testing.T) {
	m := &home{lastView: tileView(
		"────────────────────",
		"│ a       │ b      │",
		"│ c       │ d      │",
	)}
	m.sel = tileSelection{x0: 1, x1: 22, y0: 0, y1: 2, sx: 1, sy: 0, ex: 22, ey: 2, shown: true}
	want := "────────────────────\n│ a       │ b      │\n│ c       │ d      │"
	if got := m.selectedText(); got != want {
		t.Errorf("got %q\nwant %q", got, want)
	}
}
