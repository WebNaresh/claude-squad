package app

import (
	"claude-squad/session"
	"claude-squad/ui"
	"fmt"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// Each project can have a terminal (a shell in its folder, tmux
// csterm_<folder>, kept between runs). It is a tile in the grid like any
// session: Ctrl+] t adds it (or jumps to it), `exit` closes it, and a project
// with no sessions gets one automatically. Running `claude` in it keeps the
// same tile, which then shows Claude's status; Ctrl+] t then opens another
// terminal (csterm_<folder>-2).

// ensureTerminal starts the project's terminal if needed, lists it and
// selects it. It returns the tmux session name.
func (m *home) ensureTerminal() (string, error) {
	rows := len(m.gridRows())
	w, h := ui.GridTileSize(rows+1, m.paneWidth, m.contentHeight)
	name, err := session.EnsureProjectShell(m.projectTabs.Active(), w, h, m.claudeRunningIn)
	if err != nil {
		return "", err
	}
	if !m.listHas(name) {
		if list, _, err := session.ListExternalSessions(); err == nil {
			m.setExternalSessions(list)
		}
		logEvent("terminal tile opened: %s", name)
	}
	m.list.SelectExternal(name)
	return name, nil
}

func (m *home) listHas(name string) bool {
	for _, e := range m.list.ExternalSessions() {
		if e.Name == name {
			return true
		}
	}
	return false
}

// openTerminal adds the project terminal tile (or goes to it) and types into it.
func (m *home) openTerminal() tea.Cmd {
	if m.projectTabs.Active() == "" {
		return nil
	}
	if _, err := m.ensureTerminal(); err != nil {
		return m.handleError(err)
	}
	return m.autoFocus()
}

// closeSession closes the selected tile after asking: a terminal's shell
// ends, a Claude session stops (its conversation stays resumable with
// claude --resume). Sessions running in another terminal window can only be
// closed there.
func (m *home) closeSession() tea.Cmd {
	e := m.list.GetSelectedExternal()
	if e == nil {
		return m.handleError(fmt.Errorf("select a session tile to close it"))
	}
	name := e.Title()
	var stop func() error
	var question string
	switch {
	case e.Kind == session.KindTerminal:
		return m.handleError(fmt.Errorf("%s runs in its own terminal window; close it there", name))
	case strings.HasPrefix(e.Name, session.TermPrefix):
		question = "Close this terminal? Anything running in it stops."
		stop = func() error { return exec.Command("tmux", "kill-session", "-t", "="+e.Name).Run() }
	case e.Kind == session.KindBackground:
		question = fmt.Sprintf("Close %s? Claude stops; the conversation can be resumed later.", name)
		id, live := e.Name, e.Live
		stop = func() error {
			if out, err := exec.Command(session.RealClaude(), "stop", id).CombinedOutput(); err != nil {
				return fmt.Errorf("%s", strings.TrimSpace(string(out)))
			}
			if live != "" {
				_ = exec.Command("tmux", "kill-session", "-t", "="+live).Run()
			}
			return nil
		}
	default:
		question = fmt.Sprintf("Close %s? Claude stops; the conversation can be resumed later.", name)
		stop = func() error { return exec.Command("tmux", "kill-session", "-t", "="+e.Name).Run() }
	}
	return m.confirmAction(question, func() tea.Msg {
		if err := stop(); err != nil {
			return fmt.Errorf("could not close %s: %w", name, err)
		}
		logEvent("session closed: %s", e.Name)
		list, _, err := session.ListExternalSessions()
		if err != nil {
			return err
		}
		return externalClosedMsg{list: list}
	})
}

// externalClosedMsg carries the session list after a tile was closed.
type externalClosedMsg struct{ list []*session.ExternalSession }
