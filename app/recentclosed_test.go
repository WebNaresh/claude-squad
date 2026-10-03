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
