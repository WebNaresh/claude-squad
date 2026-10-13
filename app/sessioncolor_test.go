package app

import (
	"testing"

	"claude-squad/session"
)

// A new issue tile gets its colour before Claude starts, so the tile is
// drawn in it from the start; the colour stays free of the project's other
// sessions, and a closed tile's pick goes.
func TestColorPickedAtTileStart(t *testing.T) {
	t.Setenv("CLAUDE_SQUAD_HOME", t.TempDir())
	a := &session.ExternalSession{Name: "cc_proj_i8", Kind: session.KindTmux, Live: "cc_proj_i8", Path: "/p"}
	b := &session.ExternalSession{Name: "cc_proj_i16", Kind: session.KindTmux, Live: "cc_proj_i16", Path: "/p"}
	typed := map[string]string{}

	colorIssueSessions([]*session.ExternalSession{a}, typed)
	ca := typed[tileColorKey(a.Name)]
	if ca == "" {
		t.Fatal("no colour picked for a new tile")
	}
	if got := pickedTileColor(a.Name); got != ca {
		t.Fatalf("window reads %q, runner picked %q", got, ca)
	}

	// 8 and 16 hash to the same first choice: the second must differ.
	colorIssueSessions([]*session.ExternalSession{a, b}, typed)
	if cb := typed[tileColorKey(b.Name)]; cb == "" || cb == ca {
		t.Fatalf("second tile got %q, first has %q", cb, ca)
	}
	// Picked once: a later pass keeps it.
	colorIssueSessions([]*session.ExternalSession{a, b}, typed)
	if typed[tileColorKey(a.Name)] != ca {
		t.Fatal("tile colour changed on a later pass")
	}

	colorIssueSessions([]*session.ExternalSession{b}, typed)
	if _, ok := typed[tileColorKey(a.Name)]; ok {
		t.Fatal("closed tile's colour kept")
	}
}
