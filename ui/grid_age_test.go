package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
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

// Time and model both stay on a narrow tile, at every /stage step, selected
// or not, for the widest time and model: the stage tag shortens instead.
func TestTileAgeAndModelDuringStage(t *testing.T) {
	top := func(tile GridTile, focused bool, w int) string {
		return ansi.Strip(strings.Split(RenderTile(tile, focused, w, 2), "\n")[0])
	}
	stages := []string{"", "⋯ staging", "✓ staged", "⇡ posting proof", "✓ proof posted", "? guide update", "📖 publishing guide", "✓ done · close session"}
	for _, model := range []string{"opus[1m]", "sonnet[1m]"} {
		for _, age := range []string{"1h 44m", "12h 44m"} {
			for _, stage := range stages {
				tile := GridTile{Title: "fee-bearer-by-firm-#2980", Status: "busy", Model: model, Age: age, Stage: stage}
				for _, focused := range []bool{false, true} {
					got := top(tile, focused, 47)
					t.Log(got)
					if !strings.Contains(got, "⏱ "+age) || !strings.Contains(got, strings.TrimSuffix(model, "[1m]")) || !strings.Contains(got, "#2980") {
						t.Errorf("%s %s %q focused=%v: %q", model, age, stage, focused, got)
					}
				}
				if bottom := strings.Split(ansi.Strip(RenderTile(tile, true, 47, 2)), "\n"); !strings.Contains(bottom[len(bottom)-1], "SELECTED") {
					t.Errorf("stage %q: bottom label gone", stage)
				}
			}
		}
	}
	// Room to spare: nothing shortens, and the top SELECTED stays.
	tile := GridTile{Title: "fee-bearer-by-firm-#2980", Status: "busy", Model: "opus[1m]", Age: "1h 44m", Stage: "✓ done · close session"}
	if got := top(tile, true, 90); !strings.Contains(got, "opus[1m]") || !strings.Contains(got, "close it") || !strings.Contains(got, "SELECTED") {
		t.Errorf("wide: %q", got)
	}
}
