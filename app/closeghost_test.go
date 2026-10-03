package app

import (
	"testing"
	"time"

	"claude-squad/ui"
)

func TestClosedTileKeepsItsPlace(t *testing.T) {
	m, project := autoHome(t, 0, 0, 0)
	m.paneWidth, m.contentHeight, m.screenWidth, m.sourceControl = 180, 40, 240, ui.NewSourceControl()
	// Five tiles a..e; "b" (index 1) is closed.
	m.gridKeys = "session:a|session:b|session:c|session:d|session:e|"
	m.gridTiles = make([]ui.GridTile, 5)
	m.noteGhost("b", "issue b", closedEntry{SessionID: "sid-b"})
	if m.ghost == nil || m.ghost.idx != 1 {
		t.Fatalf("ghost = %+v, want index 1", m.ghost)
	}
	// The grid now holds a, c, d, e; the placeholder goes back at 1.
	m.gridKeys = "session:a|session:c|session:d|session:e|"
	m.gridTiles = []ui.GridTile{{Title: "a"}, {Title: "c"}, {Title: "d"}, {Title: "e"}}
	m.gridFocus = 2 // "d"
	shown, focus := m.shownTiles()
	if len(shown) != 5 || !shown[1].Ghost || shown[2].Title != "c" || shown[3].Title != "d" {
		t.Fatalf("shown = %v", titles(shown))
	}
	if focus != 3 {
		t.Errorf("focus %d, want 3 (d stays in its place)", focus)
	}
	seen := map[string]bool{}
	for _, tl := range shown {
		if tl.Ghost {
			continue
		}
		if tl.Accent == "" || seen[string(tl.Accent)] {
			t.Errorf("tile %s: colour %q missing or shared", tl.Title, tl.Accent)
		}
		seen[string(tl.Accent)] = true
	}
	again, _ := m.shownTiles()
	if again[0].Accent != shown[0].Accent {
		t.Error("a tile's colour changed between frames")
	}
	// After ghostFor, the gap closes.
	m.ghost.at = time.Now().Add(-ghostFor - time.Second)
	if shown, _ := m.shownTiles(); len(shown) != 4 {
		t.Errorf("placeholder still shown after %s", ghostFor)
	}
	_ = project
}

func titles(ts []ui.GridTile) []string {
	var out []string
	for _, t := range ts {
		if t.Ghost {
			out = append(out, "GHOST")
		} else {
			out = append(out, t.Title)
		}
	}
	return out
}
