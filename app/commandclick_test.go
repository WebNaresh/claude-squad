package app

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// The rows of glitchgrab #709's "Your action" (2026-10-28), 63 columns wide.
func TestCommandAtWrapped(t *testing.T) {
	lines := []string{
		"⚠ Your action:",
		"1. Run the login test (read-only):",
		"   ! /private/tmp/claude-501/-Users-webnaresh-coding-line-glitc",
		"   hgrab/985a9777-fd5d-4817-9b29-d690c9e0695a/scratchpad/check-",
		"   pgbouncer.sh",
		"   - Pass: it prints the pooler address, then max_connections",
	}
	want := "! /private/tmp/claude-501/-Users-webnaresh-coding-line-glitchgrab/985a9777-fd5d-4817-9b29-d690c9e0695a/scratchpad/check-pgbouncer.sh"
	for _, row := range []int{2, 3, 4} {
		if got := commandAt(lines, row, 63); got != want {
			t.Errorf("click on row %d: got %q", row, got)
		}
	}
	for _, row := range []int{0, 1, 5} {
		if got := commandAt(lines, row, 63); got != "" {
			t.Errorf("click on row %d (not the command): got %q", row, got)
		}
	}
	if got := commandAt([]string{"1. ! bun run x"}, 0, 63); got != "! bun run x" {
		t.Errorf("command after a list marker: got %q", got)
	}
}

// practice-stack #2884: Claude moved the path after "-f" to its own row; the
// copy split it there and zsh ran the path alone ("permission denied").
func TestCommandAtPathOnItsOwnRow(t *testing.T) {
	lines := []string{
		"2. Optional: run the read-only query and paste the output,",
		"   ! cd /Users/webnaresh/coding-line/practice-stack && psql",
		"   \"$(grep -E '^NEXT_DIRECT_URL' .env | cut -d'\"' -f2)\" -f",
		"   /private/tmp/claude-501/-Users-webnaresh-coding-line-practic",
		"   e-stack/c6a72472-3e3b/scratchpad/issue-2884-query.sql",
		"",
		"* Sauteed for 16s",
	}
	want := `! cd /Users/webnaresh/coding-line/practice-stack && psql "$(grep -E '^NEXT_DIRECT_URL' .env | cut -d'"' -f2)" -f /private/tmp/claude-501/-Users-webnaresh-coding-line-practice-stack/c6a72472-3e3b/scratchpad/issue-2884-query.sql`
	for _, row := range []int{1, 2, 3, 4} {
		if got := commandAt(lines, row, 63); got != want {
			t.Errorf("click on row %d:\ngot  %q\nwant %q", row, got, want)
		}
	}
	// The same rows dragged over copy as one line too.
	m := &home{lastView: strings.Join(func() []string {
		var out []string
		for _, l := range lines {
			out = append(out, "│ "+l+strings.Repeat(" ", 63-utf8.RuneCountInString(l))+" │")
		}
		return out
	}(), "\n")}
	m.sel = tileSelection{x0: 1, x1: 65, y0: 0, y1: 6, sx: 5, sy: 1, ex: 65, ey: 4, shown: true}
	if got := m.selectedText(); got != want {
		t.Errorf("drag:\ngot  %q\nwant %q", got, want)
	}
}
