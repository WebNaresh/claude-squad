package app

import (
	"claude-squad/session"
	"claude-squad/ui"
	"fmt"
	"github.com/mattn/go-runewidth"
	"os/exec"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// Ctrl+] is the cs command key, like tmux's Ctrl+B: press it, then one key.
// It works in every terminal with no settings (unlike Option shortcuts,
// which need "Use Option as Meta key", or Ctrl+Shift+5, which Terminal.app
// never sends), and Claude doesn't use it.
const leaderKey = "ctrl+]"

// leaderSpace is Ctrl+Space (sent as NUL, "ctrl+@"): the easier command key,
// the one shown on screen. Ctrl+] still works.
const leaderSpace = "ctrl+@"

func isLeaderKey(k string) bool { return k == leaderKey || k == leaderSpace }

// commandKeys are the Ctrl+] commands, in the order shown in the key bar.
var commandKeys = []ui.Key{
	{"T", "terminal"}, {"A", "add project"}, {"N", "issues"},
	{"S", "source control"}, {"Y", "copy"}, {"W", "close session"}, {"X", "close tab"},
	{"!", "needs you"}, {"?", "help"}, {"Q", "quit"},
}

// handleLeader runs the command after Ctrl+].
func (m *home) handleLeader(msg tea.KeyMsg) tea.Cmd {
	m.leader = false
	key := msg.String()
	logEvent("command key: %s", key)
	switch key {
	case "a", "A", "+":
		return chooseProjectCmd
	case "x", "X":
		return m.closeActiveProject()
	case "!":
		return m.jumpToNeedsYou()
	case "?", "/":
		_, cmd := m.showHelpScreen(helpTypeGeneral{}, nil)
		return cmd
	case "t", "T":
		return m.openTerminal()
	case "w", "W":
		return m.closeSession()
	case "y", "Y":
		return m.copySelected()
	case "o", "O":
		return m.openSentFiles()
	case "m", "M":
		return m.toggleMouse()
	case "s", "S":
		return m.openSourceControl()
	case "n", "N":
		return m.openIssuePicker()
	case "left", "h":
		return m.moveProject(true)
	case "right", "l":
		return m.moveProject(false)
	case "up", "k":
		return m.moveSession(true)
	case "down", "j":
		return m.moveSession(false)
	case "i", "I":
		m.sourceControl.SetFocused(false)
		m.tabbedWindow.ClearFileDiff()
		return m.autoFocus()
	case "q", "Q":
		_, cmd := m.handleQuit()
		return cmd
	case leaderKey, leaderSpace:
		// Ctrl+] twice sends a real Ctrl+] to the session.
		if m.sessionFocus != "" {
			select {
			case keyQueue <- queuedKey{target: m.sessionFocus, msg: msg, at: timeNow()}:
			default:
			}
		}
		return nil
	}
	return nil // esc or anything else: cancel
}

// copySelected copies the selected tile's whole text, including what is
// scrolled out of view, to the clipboard. Terminal selection can't reach it:
// it only covers the screen, and across every tile on those rows.
func (m *home) copySelected() tea.Cmd {
	e := m.list.GetSelectedExternal()
	if e == nil {
		return m.handleError(fmt.Errorf("select a tile to copy"))
	}
	return func() tea.Msg {
		text, err := e.FullText()
		if err != nil {
			return err
		}
		cmd := exec.Command("pbcopy")
		cmd.Stdin = strings.NewReader(text)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("could not copy: %w", err)
		}
		logEvent("copied %d lines from %s", strings.Count(text, "\n")+1, e.Name)
		return fmt.Errorf("copied %d lines to the clipboard (⌘V to paste)", strings.Count(text, "\n")+1)
	}
}

// everydayKeys are the Ctrl+] commands shown in the key bar; the rest are
// in help (Ctrl+] ?) and appear in the bar once Ctrl+] is pressed.
var everydayKeys = []ui.Key{
	{"T", "terminal"}, {"N", "issues"}, {"S", "source control"},
	{"←→", "project"}, {"?", "all keys"},
}

// keyBar is the one-line shortcut bar at the bottom of the screen.
func (m *home) keyBar(width int) string {
	width = max(width, 60)
	var state, lead string
	var now, keys []ui.Key
	switch {
	case m.leader:
		state = "⌃Space pressed, now press"
		keys = commandKeys
	case m.sourceControl.Focused():
		state = "Source Control"
		keys = []ui.Key{{"↑↓", "file"}, {"space", "stage / unstage"}, {"a", "stage all"}, {"u", "unstage all"}, {"d", "discard"}, {"c", "commit"}, {"esc", "back"}}
	case m.sessionFocus != "":
		name := m.sessionFocus
		if e := m.list.GetSelectedExternal(); e != nil {
			name = e.Title()
		}
		state = "⌨ " + runewidth.Truncate(name, 40, "…")
		lead = "⌃Space then"
		keys = everydayKeys
	default:
		if e := m.list.GetSelectedExternal(); e != nil && e.Kind == session.KindTerminal {
			state = "👁 View only"
			now = []ui.Key{{"enter", "move it here to type"}}
			lead = "⌃Space then"
			keys = everydayKeys
		} else {
			state = "Not typing"
			lead = "⌃Space then"
			keys = append([]ui.Key{{"I", "type into the selected tile"}}, everydayKeys...)
		}
	}
	return ui.KeyBar(state, now, lead, keys, width)
}

// openSentFiles opens every file the selected Claude session sent on one
// page in the browser, at the newest batch. Terminal.app can't show images.
func (m *home) openSentFiles() tea.Cmd {
	e := m.list.GetSelectedExternal()
	if e == nil {
		return m.handleError(fmt.Errorf("select a Claude tile to open its images"))
	}
	return openGallery(e, "")
}
