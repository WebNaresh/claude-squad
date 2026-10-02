package app

import (
	"claude-squad/session"
	"claude-squad/session/tmux"
	"claude-squad/ui"
	"fmt"
	"os/exec"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// The dock: every project has one terminal (a shell in its folder, a
// csterm_ tmux session kept between runs) docked under Source Control in the
// left column, at a fixed size so long prompts wrap instead of being cut.
// Ctrl+] t / ⌥T shows it and types into it; pressed again it hides, and a
// one-line status (idle / running bun…) stays in its place. When Claude is
// started in it, that session moves to the grid as a normal tile and the
// dock opens a fresh shell.

// dockShare is the part of the left column's height the dock takes.
const dockShare = 55

// leftColumnWidth is the width of the Source Control + dock column.
func leftColumnWidth(screenW int) int { return max(32, min(64, screenW/4)) }

// dockBoxHeight is the dock's height (border included) in a column of
// height h: the box when shown, one status row when hidden.
func dockBoxHeight(h int, hidden bool) int {
	if hidden {
		return 1
	}
	return max(8, h*dockShare/100)
}

// dockSize is the dock shell's screen size (inside border and padding).
func (m *home) dockSize() (w, h int) {
	return max(m.leftWidth-4, 10), max(dockBoxHeight(m.contentHeight, false)-2, 3)
}

// dockName returns the active project's dock shell, or "" if none yet.
func (m *home) dockName() string {
	name := m.dockNames[m.projectTabs.Active()]
	if name == "" || m.claudeRunningIn(name) {
		return ""
	}
	return name
}

// isDock reports whether an external session is the active project's dock.
func (m *home) isDock(e *session.ExternalSession) bool {
	return e != nil && e.Name != "" && e.Name == m.dockName()
}

// ensureDock starts the active project's dock shell if needed and returns
// its name. It reuses a project terminal that isn't running Claude.
func (m *home) ensureDock() (string, error) {
	project := m.projectTabs.Active()
	if project == "" {
		return "", fmt.Errorf("no project open")
	}
	// Listed means alive (the list is refreshed from tmux); this runs every
	// frame, so no tmux call here.
	if name := m.dockName(); name != "" && m.listHas(name) {
		return name, nil
	}
	w, h := m.dockSize()
	name, err := session.EnsureProjectShell(project, w, h, m.claudeRunningIn)
	if err != nil {
		return "", err
	}
	m.dockNames[project] = name
	delete(m.fitted, name) // size it to the dock on the next capture
	if !m.listHas(name) {
		if list, _, err := session.ListExternalSessions(); err == nil {
			m.setExternalSessions(list)
		}
	}
	logEvent("dock terminal: %s", name)
	return name, nil
}

// openTerminal shows the dock and types into it; pressed while typing into
// the dock it hides it and gives the keyboard back to the grid.
func (m *home) openTerminal() tea.Cmd {
	if m.projectTabs.Active() == "" {
		return nil
	}
	name, err := m.ensureDock()
	if err != nil {
		return m.handleError(err)
	}
	if !m.dockHidden && m.sessionFocus == name {
		m.setDockHidden(true)
		if rows := m.gridRows(); len(rows) > 0 {
			m.selectRow(rows[0].row)
			return m.autoFocus()
		}
		m.sessionFocus = ""
		return m.instanceChanged()
	}
	m.setDockHidden(false)
	m.sourceControl.SetFocused(false)
	m.tabbedWindow.ClearFileDiff()
	m.list.SelectExternal(name)
	return m.autoFocus()
}

// setDockHidden shows or hides the dock and resizes the column to match.
func (m *home) setDockHidden(hidden bool) {
	if m.dockHidden == hidden {
		return
	}
	m.dockHidden = hidden
	logEvent("dock terminal hidden=%v", hidden)
	m.layoutLeft()
}

// selectRow selects a grid row's session.
func (m *home) selectRow(r ui.Row) {
	if r.Instance != nil {
		m.list.SelectInstance(r.Instance)
	} else if r.External != nil {
		m.list.SelectExternal(r.External.Name)
	}
}

// focusDock selects the dock and types into it (Shift+← from the grid, a click).
func (m *home) focusDock() tea.Cmd {
	name, err := m.ensureDock()
	if err != nil {
		return m.handleError(err)
	}
	m.setDockHidden(false)
	m.sourceControl.SetFocused(false)
	m.tabbedWindow.ClearFileDiff()
	m.list.SelectExternal(name)
	return tea.Batch(m.autoFocus(), m.refreshDock())
}

// dockCapturedMsg is the dock's screen, captured off the UI thread.
type dockCapturedMsg struct {
	name    string
	content string
	command string
}

// refreshDock re-captures the dock (and creates it for a project that has
// none). The tmux calls run on a background goroutine.
func (m *home) refreshDock() tea.Cmd {
	if m.dockCapturing || m.projectTabs.Active() == "" || !m.sessionsLoaded {
		return nil
	}
	name, err := m.ensureDock()
	if err != nil {
		return nil
	}
	w, h := m.dockSize()
	fit := m.needsFit(name, w, h)
	if fit {
		logEvent("resize %s to %dx%d (dock)", name, w, h)
	}
	focus, hidden := m.sessionFocus, m.dockHidden
	m.dockCapturing = true
	return func() tea.Msg {
		if fit {
			_ = exec.Command("tmux", "resize-window", "-t", name, "-x", fmt.Sprint(w), "-y", fmt.Sprint(h)).Run()
		}
		msg := dockCapturedMsg{name: name, command: tmux.CurrentCommand(name)}
		if !hidden {
			msg.content, _ = tmux.CaptureScreen(name)
			msg.content = cursorInto(name, focus, msg.content, 0)
		}
		return msg
	}
}

// applyDockCapture stores the dock's capture (UI thread).
func (m *home) applyDockCapture(msg dockCapturedMsg) {
	m.dockCapturing = false
	if msg.content == "" {
		msg.content = m.dockShots[msg.name].content // hidden: keep the last picture
	}
	m.dockShots[msg.name] = msg
}

// dockStatus says what the dock shell is doing.
func (m *home) dockStatus() string {
	command := m.dockShots[m.dockName()].command
	switch command {
	case "", "zsh", "-zsh", "bash", "-bash", "fish", "sh":
		return "idle"
	default:
		return "running " + command
	}
}

// renderDock draws the dock box, or its one-line status when hidden.
func (m *home) renderDock() string {
	status := m.dockStatus()
	if m.dockHidden {
		line := " ▸ terminal · " + status + " · ⌃Space T shows it"
		return lipgloss.NewStyle().Width(m.leftWidth).MaxWidth(m.leftWidth).Foreground(lipgloss.Color("#888888")).Render(line)
	}
	w, h := m.dockSize()
	name := m.dockName()
	return ui.RenderTile(ui.GridTile{Title: "terminal", Status: status, Content: m.dockShots[name].content},
		name != "" && m.sessionFocus == name, w, h)
}

// dockTop is the screen row where the dock starts (tab row, gap, Source Control).
func (m *home) dockTop() int {
	return 2 + m.contentHeight - dockBoxHeight(m.contentHeight, m.dockHidden)
}

// handleDockMouse focuses the dock on a click and scrolls it with the wheel.
func (m *home) handleDockMouse(msg tea.MouseMsg) (tea.Cmd, bool) {
	if m.state != stateDefault || msg.Action != tea.MouseActionPress ||
		msg.X >= m.leftWidth || msg.Y < m.dockTop() || msg.Y >= 2+m.contentHeight {
		return nil, false
	}
	switch msg.Button {
	case tea.MouseButtonLeft:
		// Focus it, and start a drag-select like in the tiles (selection.go):
		// letting go after a drag copies the dock's text.
		cmd := m.focusDock()
		if m.dockHidden {
			return cmd, true
		}
		_, h := m.dockSize()
		top := m.dockTop() + 1 // below the box's top border
		hit := gridHit{idx: -1, x0: 0, w0: m.leftWidth, textTop: top, row: msg.Y - top}
		hit.lines = make([]string, h)
		m.sel = tileSelection{}
		if hit.row >= 0 && hit.row < h {
			m.startSelection(msg.X, msg.Y, hit)
		}
		return cmd, true
	case tea.MouseButtonWheelUp, tea.MouseButtonWheelDown:
		name := m.dockName()
		if name == "" || m.dockHidden {
			return nil, true
		}
		key := tea.KeyMsg{Type: tea.KeyUp}
		if msg.Button == tea.MouseButtonWheelDown {
			key = tea.KeyMsg{Type: tea.KeyDown}
		}
		select {
		case keyQueue <- queuedKey{target: name, msg: key, at: timeNow(), wheel: 1}:
		default:
		}
		m.lastKey = timeNow()
		m.skipRender = true // the next preview tick shows it (see handleGridMouse)
		return nil, true
	}
	return nil, false
}

// externalCount is how many external sessions run in a project, not
// counting its docked terminal.
func (m *home) externalCount(project string) int {
	n := m.list.CountExternalInProject(project)
	if name := m.dockNames[project]; name != "" {
		for _, e := range m.list.ExternalSessions() {
			if e.Name == name && !m.claudeRunningIn(name) {
				n--
			}
		}
	}
	return n
}
