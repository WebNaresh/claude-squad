package app

import (
	"claude-squad/session"
	"fmt"
	"os/exec"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Project terminals are csterm_<folder> tmux shells. One is docked under
// Source Control (dock.go); one running Claude shows in the grid as a tile.

func (m *home) listHas(name string) bool {
	for _, e := range m.list.ExternalSessions() {
		if e.Name == name {
			return true
		}
	}
	return false
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
	cmd := m.confirmAction(question, func() tea.Msg {
		if err := stop(); err != nil {
			return fmt.Errorf("could not close %s: %w", name, err)
		}
		logEvent("session closed: %s", e.Name)
		// Drop the tile now; reloading the session list first (claude agents)
		// kept the dead tile on screen for a second or more.
		return externalClosedMsg{name: e.Name}
	})
	m.confirmationOverlay.ConfirmLabel = "Close"
	return cmd
}

// externalClosedMsg names a tile that was just closed.
type externalClosedMsg struct{ name string }

// justClosedFor is how long a closed session is kept out of the list, so a
// status refresh that started before the close can't bring its tile back.
const justClosedFor = 5 * time.Second

// newClaudeSession starts a new Claude session in the active project's
// folder (no worktree, no branch); it shows up as a tile and gets focus.
func (m *home) newClaudeSession() tea.Cmd {
	if !m.activeProjectExists() {
		return m.handleError(fmt.Errorf("this project's folder no longer exists; close the tab with ⌃Space X"))
	}
	project := m.projectTabs.Active()
	program := m.program
	logEvent("new Claude session in %s", project)
	return func() tea.Msg {
		name, err := session.StartSession(project, program)
		if err != nil {
			return err
		}
		list, _, _ := session.ListExternalSessions()
		return sessionStartedMsg{name: name, sessions: list}
	}
}
