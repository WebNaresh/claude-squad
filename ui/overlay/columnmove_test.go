package overlay

import (
	"strings"
	"testing"
)

// A dialog over a tile row must land at its own column: the row's column
// moves from beyond the dialog used to survive the cut and throw it sideways.
func TestPlaceOverlayKeepsDialogOnPinnedRow(t *testing.T) {
	row := "ab की\x1b[12G     |tile2 की\x1b[30G   |"
	got := strings.Split(PlaceOverlay(8, 1, "XXXX", row+"\n"+row+"\n"+row, false, false), "\n")[1]
	if !strings.Contains(got, ColumnMove(8)) || !strings.Contains(got, ColumnMove(12)) {
		t.Fatalf("dialog not pinned: %q", got)
	}
	if strings.Contains(got, "\x1b[30G") {
		t.Fatalf("column move from past the dialog kept: %q", got)
	}
	if !strings.Contains(got, "XXXX") {
		t.Fatalf("dialog missing: %q", got)
	}
}

func TestStripColumnMoves(t *testing.T) {
	if got, ok := StripColumnMoves("a\x1b[12Gb\x1b[0m"); got != "ab\x1b[0m" || !ok {
		t.Fatalf("got %q %v", got, ok)
	}
	if _, ok := StripColumnMoves("plain"); ok {
		t.Fatal("plain line reported as pinned")
	}
}
