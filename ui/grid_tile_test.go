package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

// Every line of a tile, the top border with its title included, must be
// exactly as wide as the tile, or the tiles beside it shift.
func TestRenderTileWidth(t *testing.T) {
	for _, focused := range []bool{false, true} {
		for _, tile := range []GridTile{
			{Title: "short", Status: "idle", Content: "x"},
			{Title: strings.Repeat("long title ", 20), Status: "view only", Content: "x"},
			{Title: "needs", Status: "busy", NeedsYou: true},
		} {
			out := renderTile(tile, focused, 50, 5)
			for i, l := range strings.Split(out, "\n") {
				if w := lipgloss.Width(l); w != 54 {
					t.Errorf("focused=%v %q line %d width %d, want 54", focused, tile.Title, i, w)
				}
			}
		}
	}
}

func TestBoxWidths(t *testing.T) {
	sc := NewSourceControl()
	sc.SetSize(40, 12)
	sc.branch = "main"
	tw := NewTabbedWindow(NewPreviewPane(), NewDiffPane(), NewTerminalPane())
	tw.SetSize(60, 12)
	tw.SetFileDiff("app/app.go · changes", "@@ -1 +1 @@\n-a\n+b")
	for name, c := range map[string]struct {
		out  string
		w, h int
	}{"source control": {sc.String(), 40, 12}, "diff": {tw.String(), 60, 12}} {
		lines := strings.Split(c.out, "\n")
		if len(lines) != c.h {
			t.Errorf("%s: %d lines, want %d", name, len(lines), c.h)
		}
		for i, l := range lines {
			if w := lipgloss.Width(l); w != c.w {
				t.Errorf("%s line %d width %d, want %d", name, i, w, c.w)
			}
		}
	}
}

// A key row never grows past its width, however many keys it is given.
func TestKeyRowFits(t *testing.T) {
	keys := []Key{{"enter", "start"}, {"esc", "cancel"}, {"space", "tick / skip"}, {"↑↓", "move"}, {"←→", "project"}, {"o", "open"}}
	for w := 10; w <= 120; w++ {
		if got := lipgloss.Width(KeyRow("lead", keys, w)); got > w && w >= 20 {
			t.Errorf("width %d: row is %d wide", w, got)
		}
	}
}

func TestEmptyTileSaysSo(t *testing.T) {
	out := renderTile(GridTile{Title: "x", Status: "idle"}, false, 40, 4)
	if !strings.Contains(out, "Nothing on screen yet") {
		t.Errorf("empty tile shows no hint:\n%s", out)
	}
}

func TestKeyBarKeepsHelpKey(t *testing.T) {
	keys := []Key{{"T", "terminal"}, {"N", "issues"}, {"S", "source control"}, {"←→", "project"}, {"?", "all keys"}}
	for _, w := range []int{60, 80, 200} {
		bar := KeyBar("⌨ some-session", nil, "⌃Space then", keys, w)
		if !strings.Contains(bar, "all keys") || lipgloss.Width(bar) > w {
			t.Errorf("width %d: %q (%d wide)", w, bar, lipgloss.Width(bar))
		}
	}
}
