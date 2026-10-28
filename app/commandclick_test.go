package app

import "testing"

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
