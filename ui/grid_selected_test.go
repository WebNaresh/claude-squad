package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// The selected tile carries "◀ SELECTED" on its bottom border too, and stays
// the same size as any other tile.
func TestSelectedTileBottomLabel(t *testing.T) {
	tile := GridTile{Title: "all-and-all-time-filters-#2458", Status: "idle", Content: "hello", Accent: lipgloss.Color("#d7af00")}
	sel := strings.Split(RenderTile(tile, true, 60, 8), "\n")
	other := strings.Split(RenderTile(tile, false, 60, 8), "\n")
	if len(sel) != len(other) {
		t.Fatalf("selected tile has %d lines, others %d", len(sel), len(other))
	}
	for i := range sel {
		if ansi.StringWidth(sel[i]) != ansi.StringWidth(other[i]) {
			t.Errorf("line %d: width %d, want %d: %q", i, ansi.StringWidth(sel[i]), ansi.StringWidth(other[i]), ansi.Strip(sel[i]))
		}
	}
	last := ansi.Strip(sel[len(sel)-1])
	if !strings.HasSuffix(last, "◀ SELECTED ═╝") {
		t.Errorf("bottom border %q lacks the SELECTED label on the right", last)
	}
	if strings.Contains(ansi.Strip(other[len(other)-1]), "SELECTED") {
		t.Errorf("unselected tile shows SELECTED")
	}
}
