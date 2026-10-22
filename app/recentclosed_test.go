package app

import (
	"strings"
	"testing"
	"time"

	"claude-squad/session"
	"claude-squad/ui"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestRecentlyClosed(t *testing.T) {
	m, project := autoHome(t, 0, 0, 0)
	m.leftWidth, m.contentHeight, m.sourceControl = 60, 40, ui.NewSourceControl()
	other := "/somewhere/else"
	for i, title := range []string{"oldest", "third", "second", "newest"} {
		m.noteClosed(closedEntry{Project: project, Name: "cc_p_i" + title, Title: title, SessionID: "sid-" + title, At: time.Now().Add(time.Duration(i) * time.Minute)})
	}
	m.noteClosed(closedEntry{Project: other, Title: "elsewhere", SessionID: "sid-x", At: time.Now()})
	// "second" is open again in a tile: not listed.
	m.list.SetExternal([]*session.ExternalSession{{Kind: session.KindTmux, Name: "cc_p_isecond", Path: project, SessionID: "sid-second", Status: "busy"}})

	got := ansi.Strip(m.renderRecent())
	want := []string{"Recently closed", "newest", "third", "oldest"}
	last := -1
	for _, w := range want {
		i := strings.Index(got, w)
		if i < 0 || i < last {
			t.Fatalf("list %q: %q missing or out of order", got, w)
		}
		last = i
	}
	if strings.Contains(got, "second") || strings.Contains(got, "elsewhere") {
		t.Errorf("list %q shows a reopened session or another project's", got)
	}
	if m.recentHeight() != 4 {
		t.Errorf("height %d, want 4", m.recentHeight())
	}
	// Only "↺ reopen" reopens; a stray click on the row's text doesn't.
	row := m.recentTop() + 1
	if cmd, _ := m.handleRecentMouse(tea.MouseMsg{X: 10, Y: row, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}); cmd != nil {
		t.Error("a click on the row's text reopened the session")
	}
	if cmd, ok := m.handleRecentMouse(tea.MouseMsg{X: m.leftWidth - 3, Y: row, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft}); !ok || cmd == nil {
		t.Error("click on ↺ reopen did nothing")
	}
}

// One click arriving as two mouse events reopened the conversation in two
// tiles; closing one of them then hid both (they share a session ID).
func TestReopenOnceAndCloseOne(t *testing.T) {
	m, project := autoHome(t, 0, 0, 0)
	e := closedEntry{Project: project, Name: "cc_p", Title: "t", SessionID: "sid-1", At: time.Now()}
	if m.reopenClosed(e) == nil {
		t.Fatal("first reopen did nothing")
	}
	if m.reopenClosed(e) != nil {
		t.Error("second click reopened it again")
	}

	a := &session.ExternalSession{Kind: session.KindTmux, Name: "cc_p", Path: project, SessionID: "sid-1"}
	b := &session.ExternalSession{Kind: session.KindTmux, Name: "cc_p_2", Path: project, SessionID: "sid-1"}
	m.setExternalSessions([]*session.ExternalSession{a, b})
	m.hideClosed(externalClosedMsg{name: "cc_p", sessionID: "sid-1"})
	// The next refresh still lists both, plus the closed Claude exiting.
	ghost := &session.ExternalSession{Kind: session.KindTerminal, Name: "pid-123", Path: project, SessionID: "sid-1", Pid: 123}
	m.setExternalSessions([]*session.ExternalSession{a, b, ghost})
	got := m.list.ExternalSessions()
	if len(got) != 1 || got[0].Name != "cc_p_2" {
		t.Errorf("after closing cc_p, tiles = %v, want only cc_p_2", names(got))
	}
	// Closed again: reopening it right away works.
	if m.reopenClosed(e) == nil {
		t.Error("reopen right after closing it did nothing")
	}
}

func names(l []*session.ExternalSession) []string {
	var out []string
	for _, e := range l {
		out = append(out, e.Name)
	}
	return out
}
