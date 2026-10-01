package app

import (
	"strings"
	"testing"

	"claude-squad/ui"
)

// Clicks must map to tiles by position: Source Control's box on the left has
// the same rounded corner as the tiles, so the grid can't be found by it.
func TestHitGridSkipsSourceControl(t *testing.T) {
	tiles := []ui.GridTile{{Title: "one", Content: "a1\na2"}, {Title: "two", Content: "b1\nb2"}}
	m := &home{gridTiles: tiles, paneWidth: 100, contentHeight: 10, screenWidth: 130}
	grid := strings.Split(ui.RenderGrid(tiles, 0, 100, 10), "\n")
	sc := strings.Repeat(" ", 30)
	lines := []string{"tabs", ""}
	for _, g := range grid {
		lines = append(lines, "╭"+sc[1:]+g)
	}
	m.lastView = strings.Join(lines, "\n")

	h, ok := m.hitGrid(30+50+5, 2+1) // second tile, first content row
	if !ok || h.idx != 1 || h.row != 0 {
		t.Fatalf("hit = %+v ok=%v, want tile 1 row 0", h, ok)
	}
	if got := strings.TrimSpace(h.lines[0]); got != "b1" {
		t.Errorf("content line %q, want b1", got)
	}
}
