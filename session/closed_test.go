package session

import "testing"

// A background session closed in cs must not come back from the cached
// agents list (issue log: "Closed session opens again").
func TestMarkClosedHidesUntilBusy(t *testing.T) {
	agentsMu.Lock()
	agentsCache = []agentInfo{{ID: "d0d20006", Kind: "background", Status: "idle"}, {ID: "other", Kind: "background", Status: "idle"}}
	agentsMu.Unlock()
	MarkClosed("d0d20006")
	agentsMu.Lock()
	n := len(agentsCache)
	agentsMu.Unlock()
	if n != 1 {
		t.Fatalf("cache still has %d sessions, want 1", n)
	}
	if !isClosed(agentInfo{ID: "d0d20006", Status: "idle"}) {
		t.Error("closed session shown again while idle")
	}
	if isClosed(agentInfo{ID: "d0d20006", Status: "busy"}) {
		t.Error("session resumed by the user (busy) still hidden")
	}
	e := &ExternalSession{Kind: KindBackground, Name: "x1"}
	MarkClosed("x1")
	if _, err := e.EnsureLive(80, 24); err == nil {
		t.Error("EnsureLive re-attached a closed session")
	}
}

func TestCloseAttachWrappersIsNotExit(t *testing.T) {
	closedMu.Lock()
	attached["y1"] = true
	closedMu.Unlock()
	t.Setenv("TMUX_TMPDIR", t.TempDir()) // no tmux server: nothing to kill
	CloseAttachWrappers()
	closedMu.Lock()
	defer closedMu.Unlock()
	if attached["y1"] {
		t.Error("wrappers cs ended itself would count as the user's /exit")
	}
}
