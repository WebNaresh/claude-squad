package app

import (
	"claude-squad/session"
	"os"
	"strings"
	"testing"
	"time"
)

func TestCurrentTilesKeepsKnownConversation(t *testing.T) {
	before := []liveTile{{Project: "/p", Name: "cc_p_i12", SessionID: "s12", Issue: 12}}
	sessions := []*session.ExternalSession{
		{Kind: session.KindTmux, Name: "cc_p_i12", Path: "/p"},                   // id not known this round
		{Kind: session.KindTmux, Name: "cc_p", Path: "/p", SessionID: "s1"},      // general tile
		{Kind: session.KindTmux, Name: "cc_p_2", Path: "/p"},                     // never known: skipped
		{Kind: session.KindTmux, Name: session.TermPrefix + "p", Path: "/p"},     // docked terminal
		{Kind: session.KindBackground, Name: "abc", Path: "/p", SessionID: "bg"}, // not in this server
	}
	got := currentTiles(sessions, before)
	if len(got) != 2 {
		t.Fatalf("got %d tiles, want 2: %+v", len(got), got)
	}
	if got[0].SessionID != "s12" || got[0].Issue != 12 {
		t.Errorf("issue tile = %+v, want s12 / #12", got[0])
	}
	if got[1].SessionID != "s1" || got[1].Issue != 0 {
		t.Errorf("general tile = %+v", got[1])
	}
}

func TestLostTilesSkipsOpenAndUserClosed(t *testing.T) {
	at := time.Now()
	saved := liveTiles{Server: "1 1", At: at, Tiles: []liveTile{
		{Name: "a", SessionID: "a"}, // still open
		{Name: "b", SessionID: "b"}, // closed by the user after the save
		{Name: "c", SessionID: "c"}, // lost
		{Name: "d", SessionID: "d"}, // closed before the save, open again, then lost
	}}
	open := []*session.ExternalSession{{SessionID: "a"}}
	closed := []closedEntry{{SessionID: "b", At: at.Add(time.Second)}, {SessionID: "d", At: at.Add(-time.Hour)}}
	got := lostTiles(saved, open, closed)
	if len(got) != 2 || got[0].SessionID != "c" || got[1].SessionID != "d" {
		t.Fatalf("lost = %+v, want c and d", got)
	}
}

func TestGoneImageClickSaysWhy(t *testing.T) {
	lines := []string{
		"  ›       /private/tmp/claude-501/-Users-webnaresh-coding (148.8K",
		"  [image] -line-practice-stack/de415537-c2c7-4e49-aab7-63B)",
		"          94d811b38b/scratchpad/after-search-highlighted.",
		"          png",
		"",
		"  plain text naming /tmp/x/foo.png",
	}
	err := goneImageAt(lines, 1)
	if err == nil || !strings.Contains(err.Error(), "after-search-highlighted.png is gone") || !strings.Contains(err.Error(), "restart") {
		t.Fatalf("got %v", err)
	}
	dir := t.TempDir()
	_ = os.WriteFile(dir+"/still-here.png", []byte("x"), 0o600)
	if err := goneImageAt([]string{"  ›       " + dir + "/still-here.png (1B)"}, 0); err != nil {
		t.Errorf("existing image called gone: %v", err)
	}
	if err := goneImageAt(lines, 5); err != nil {
		t.Errorf("plain text named a file, got %v", err)
	}
}
