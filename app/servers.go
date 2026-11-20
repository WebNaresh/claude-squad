package app

import (
	"claude-squad/session"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Servers: the current project's listening ports, between Source Control and
// the docked terminal, each with the tile that started it, so "port 3333 is
// already in use" has an answer and a one-click stop (✕). A server running
// in a tile gets Ctrl+C there; one Claude started, or one left running by a
// closed shell, is stopped directly (session.StopServer).

const (
	serversEvery   = 3 * time.Second
	serversMaxRows = 6
	serversStopW   = 7 // " ✕ stop"
)

var (
	serversHeadStyle = lipgloss.NewStyle().Bold(true)
	serversDimStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#888888"))
	serversStopStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#de613e"))
)

type serversTickMsg struct{}

type serversMsg struct {
	servers []session.Server
	blocked []int  // ports the dock terminal said were already in use
	dock    string // the dock terminal blocked was read from
	err     error  // the listing timed out: keep the last list
}

func serversTick(d time.Duration) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(d)
		return serversTickMsg{}
	}
}

// refreshServers lists the listening ports off the UI thread, and the ports
// the dock terminal (tmux session dock, "" for none) failed to bind.
func refreshServers(dock string) tea.Cmd {
	return func() tea.Msg {
		list, err := session.ListServers()
		return serversMsg{servers: list, blocked: dockBlockedPorts(dock), dock: dock, err: err}
	}
}

// addrInUse matches Node's and most servers' "address already in use :::4444"
// / "0.0.0.0:3000" / "port 3000".
var addrInUse = regexp.MustCompile(`(?i)address already in use[^0-9\n]*(?:[0-9.]*:)?(\d{2,5})`)

// dockBlockedPorts reads the dock terminal's recent lines for "address
// already in use" and returns those ports.
func dockBlockedPorts(dock string) []int {
	if dock == "" {
		return nil
	}
	out, err := session.ProcOutput("tmux", "capture-pane", "-p", "-J", "-S", "-200", "-t", "="+dock+":")
	if err != nil {
		return nil
	}
	return blockedPorts(string(out))
}

// blockedPorts returns each port text says was already in use, once.
func blockedPorts(text string) []int {
	var ports []int
	for _, m := range addrInUse.FindAllStringSubmatch(text, -1) {
		if n, _ := strconv.Atoi(m[1]); n > 0 && !slices.Contains(ports, n) {
			ports = append(ports, n)
		}
	}
	return ports
}

// applyServers stores the list and resizes the left column if the number of
// rows shown changed.
func (m *home) applyServers(msg serversMsg) tea.Cmd {
	if msg.err != nil {
		// lsof timed out: keep the last list rather than show every server gone.
		logEvent("servers: list failed, keeping the last one: %v", msg.err)
		return nil
	}
	before := m.serversHeight()
	m.servers, m.serversBlocked, m.serversBlockedDock = msg.servers, msg.blocked, msg.dock
	if m.serversHeight() != before {
		m.layoutLeft()
	}
	if m.serversFocused && len(m.projectServers()) == 0 {
		m.serversFocused = false
		return m.focusDock() // the last one stopped: back to typing
	}
	return nil
}

// projectServers are the servers of the active project: started in one of
// its tiles, or running in its folder. A server of another folder shows too
// when it holds a port the dock terminal failed to bind ("address already in
// use"), so the clash has an answer and a stop.
func (m *home) projectServers() []session.Server {
	project := m.projectTabs.Active()
	if project == "" {
		return nil
	}
	var out []session.Server
	for _, s := range m.servers {
		if m.tileProject(s.Tmux) == project || s.Cwd == project || strings.HasPrefix(s.Cwd, project+string(filepath.Separator)) ||
			m.blocksDock(s.Port) {
			out = append(out, s)
		}
	}
	if len(out) > serversMaxRows {
		out = out[:serversMaxRows]
	}
	return out
}

// tileProject returns the project of the tile running in tmux session name.
func (m *home) tileProject(name string) string {
	if name == "" {
		return ""
	}
	for _, e := range m.list.ExternalSessions() {
		if e.Name == name || e.Live == name {
			return m.projectOf(e.Path)
		}
	}
	for _, inst := range m.list.GetInstances() {
		if inst.TmuxName() == name {
			return m.projectOf(inst.Path)
		}
	}
	return ""
}

// serverOwner says who started a server, in the words of the screen.
func (m *home) serverOwner(s session.Server) string {
	who := ""
	switch {
	case m.blocksDock(s.Port) && m.tileProject(s.Tmux) != m.projectTabs.Active():
		return "blocks this port · " + homeShort(s.Cwd)
	case s.Tmux == "":
		return "no tile, left running"
	case s.Tmux == m.dockName():
		who = "terminal"
	default:
		who = s.Tmux
		for _, e := range m.list.ExternalSessions() {
			if e.Name == s.Tmux || e.Live == s.Tmux {
				who = e.Title()
			}
		}
		for _, inst := range m.list.GetInstances() {
			if inst.TmuxName() == s.Tmux {
				who = inst.Title
			}
		}
	}
	if s.ByClaude {
		return "Claude in " + who
	}
	return who
}

// blocksDock reports a port the active project's dock terminal failed to
// bind. Read from another project's dock (tab just switched): none.
func (m *home) blocksDock(port int) bool {
	return m.serversBlockedDock != "" && m.serversBlockedDock == m.dockName() && slices.Contains(m.serversBlocked, port)
}

// homeShort writes a folder with ~ for the home folder.
func homeShort(dir string) string {
	if h, err := os.UserHomeDir(); err == nil && h != "" && strings.HasPrefix(dir, h) {
		return "~" + dir[len(h):]
	}
	return dir
}

// serversHeight is the rows the list takes: a heading and one per server.
func (m *home) serversHeight() int {
	if n := len(m.projectServers()); n > 0 {
		return n + 1
	}
	return 0
}

// layoutLeft sizes Source Control to what the servers and the dock leave.
func (m *home) layoutLeft() {
	m.sourceControl.SetSize(m.leftWidth, m.contentHeight-dockBoxHeight(m.contentHeight, m.dockHidden)-m.serversHeight()-m.recentHeight())
}

// renderServers draws the list, or "" when the project runs none.
func (m *home) renderServers() string {
	list := m.projectServers()
	if len(list) == 0 {
		return ""
	}
	w := m.leftWidth
	lines := []string{" " + serversHeadStyle.Render("Servers")}
	for _, s := range list {
		port := fmt.Sprintf(":%d", s.Port)
		text := ansi.Truncate(" "+port+" "+m.serverOwner(s)+" · "+s.Command, w-serversStopW, "…")
		pad := max(0, w-serversStopW-ansi.StringWidth(text))
		row := serversDimStyle.Render(text) + strings.Repeat(" ", pad) + serversStopStyle.Render(" ✕ stop")
		if m.serversFocused && len(lines)-1 == m.serverCursor {
			row = "\x1b[7m" + text + strings.Repeat(" ", pad) + " ✕ stop\x1b[27m"
		}
		lines = append(lines, row)
	}
	return strings.Join(lines, "\n")
}

// serversTop is the screen row of the "Servers" heading.
func (m *home) serversTop() int { return m.recentTop() - m.serversHeight() }

// handleServersMouse stops the server whose "✕ stop" was clicked.
func (m *home) handleServersMouse(msg tea.MouseMsg) (tea.Cmd, bool) {
	if m.state != stateDefault || msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft ||
		msg.X >= m.leftWidth {
		return nil, false
	}
	row := msg.Y - m.serversTop() - 1
	list := m.projectServers()
	if row < 0 || row >= len(list) {
		return nil, false
	}
	if msg.X < m.leftWidth-serversStopW {
		// The row's text: select it (keyboard takes over from here).
		m.serverCursor = row
		return m.focusServers(), true
	}
	return m.stopServer(list[row]), true
}

// stopServer stops s and refreshes the list shortly after.
func (m *home) stopServer(s session.Server) tea.Cmd {
	owner, dock := m.serverOwner(s), m.dockName()
	logEvent("server stop: :%d pid=%d root=%d tmux=%s byClaude=%v", s.Port, s.PID, s.Root, s.Tmux, s.ByClaude)
	return tea.Batch(func() tea.Msg {
		if err := session.StopServer(s); err != nil {
			return err
		}
		return fmt.Errorf("stopping :%d (%s)", s.Port, owner)
	}, func() tea.Msg {
		time.Sleep(time.Second)
		return refreshServers(dock)()
	})
}

// nonEmpty drops empty blocks, so a missing list leaves no blank row.
func nonEmpty(blocks ...string) []string {
	var out []string
	for _, b := range blocks {
		if b != "" {
			out = append(out, b)
		}
	}
	return out
}

// Keyboard: ⇧↑ from the docked terminal moves here; ↑↓ pick a server, x or
// Enter asks to stop it; esc or ⇧↓ goes back to the terminal, ⇧↑ on to
// Source Control.

// focusServers gives the list the keyboard (nothing is typed into a session).
func (m *home) focusServers() tea.Cmd {
	if len(m.projectServers()) == 0 {
		return m.handleError(fmt.Errorf("no servers running in this project"))
	}
	logEvent("servers focused")
	m.sessionFocus = ""
	m.serversFocused = true
	m.serverCursor = min(m.serverCursor, len(m.projectServers())-1)
	return nil
}

// handleServersKey drives the focused list.
func (m *home) handleServersKey(msg tea.KeyMsg) tea.Cmd {
	list := m.projectServers()
	if len(list) == 0 {
		m.serversFocused = false
		return m.focusDock()
	}
	m.serverCursor = min(m.serverCursor, len(list)-1)
	switch msg.String() {
	case "up", "k":
		m.serverCursor = max(0, m.serverCursor-1)
	case "down", "j":
		m.serverCursor = min(len(list)-1, m.serverCursor+1)
	case "x", "enter", "delete", "backspace":
		s := list[m.serverCursor]
		cmd := m.confirmAction(fmt.Sprintf("Stop :%d (%s)?\n%s", s.Port, m.serverOwner(s), s.Command), m.stopServer(s))
		m.confirmationOverlay.ConfirmLabel = "Stop"
		return cmd
	case "esc", "shift+down", "alt+shift+down", "ctrl+q":
		m.serversFocused = false
		return m.focusDock()
	case "shift+up", "alt+shift+up":
		m.serversFocused = false
		return m.openSourceControl()
	case "shift+right", "alt+shift+right":
		m.serversFocused = false
		if rows := m.gridRows(); len(rows) > 0 {
			m.selectRow(rows[0].row)
		}
		return m.autoFocus()
	default:
		if cmd, ok := m.navKey(msg); ok {
			m.serversFocused = false
			return cmd
		}
	}
	return nil
}
