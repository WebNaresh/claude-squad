package session

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

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

// GlitchRecord's `claude -p` runs must not get a view-only tile of their own
// (issue log: "GlitchRecord runs opened their own tile").
func TestIsHeadless(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".claude", "sessions")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"1.json": `{"kind":"interactive","entrypoint":"sdk-cli"}`,
		"2.json": `{"kind":"interactive","entrypoint":"cli"}`,
		"3.json": `{"kind":"interactive","entrypoint":"claude-vscode"}`,
		"4.json": `not json`,
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if !isHeadless(1) {
		t.Error("sdk-cli run not seen as headless")
	}
	for _, pid := range []int{2, 3, 4, 5} {
		if isHeadless(pid) {
			t.Errorf("pid %d hidden, want it listed", pid)
		}
	}
}

// The session file is deleted when a run ends but the agents list stays cached,
// so the list must keep hiding the run with no file left to read.
func TestHeadlessStaysHiddenAfterFileGone(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	agentsMu.Lock()
	saved, savedAt := agentsCache, agentsFetched
	agentsCache = []agentInfo{{Pid: 999999, Kind: "interactive", Name: "headless-run", Cwd: "/tmp/x", Headless: true}}
	agentsFetched = time.Now()
	agentsMu.Unlock()
	defer func() {
		agentsMu.Lock()
		agentsCache, agentsFetched = saved, savedAt
		agentsMu.Unlock()
	}()
	sessions, _, err := ListExternalSessions()
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range sessions {
		if e.Label == "headless-run" {
			t.Error("finished headless run came back as a tile")
		}
	}
}
