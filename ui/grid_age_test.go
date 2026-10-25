package ui

import (
	"strings"
	"testing"
)

func TestRenderTileAgeDropsOnNarrowTile(t *testing.T) {
	tile := GridTile{Title: "hr-payroll", Status: "busy", Model: "opus", Age: "1h 5m"}
	if top := strings.Split(RenderTile(tile, false, 70, 2), "\n")[0]; !strings.Contains(top, "⏱ 1h 5m") {
		t.Errorf("wide tile should show the age: %q", top)
	}
	tile.Stage = "✓ done"
	if top := strings.Split(RenderTile(tile, true, 36, 2), "\n")[0]; strings.Contains(top, "⏱") {
		t.Errorf("narrow tile should drop the age: %q", top)
	}
}
