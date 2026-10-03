package app

import (
	"claude-squad/config"
	"claude-squad/session"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Recently closed: the last Claude sessions closed with ⌃Space W in the
// current project, in the left column above the terminal, so one closed by
// mistake (or forgotten) is one click away: ↺ resumes its conversation in a
// new tile. Kept in recent-closed.json across restarts.

const (
	recentShown = 3
	recentKept  = 30
	recentOpenW = 9 // " ↺ reopen"
)

type closedEntry struct {
	Project   string    `json:"project"`
	Name      string    `json:"name"`
	Title     string    `json:"title"`
	SessionID string    `json:"session_id"`
	Issue     int       `json:"issue"`
	At        time.Time `json:"at"`
}

var recentOpenStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#3fb950"))

func recentFile() string {
	dir, err := config.GetConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "recent-closed.json")
}

func loadRecent() []closedEntry {
	var out []closedEntry
	data, err := os.ReadFile(recentFile())
	if err != nil {
		// First run: fill it from the closes activity.log already recorded
		// ("session closed: cc_<folder>_i<N>"), so earlier closes show too.
		out = backfillRecent()
		saveRecent(out)
		return out
	}
	_ = json.Unmarshal(data, &out)
	return out
}

// backfillRecent rebuilds the list from activity.log(.old): issue sessions
// only, each matched to its conversation by the session-naming hook's title
// ("…-#<N>"), newest first.
func backfillRecent() []closedEntry {
	dir, err := config.GetConfigDir()
	if err != nil {
		return nil
	}
	home, _ := os.UserHomeDir()
	named := filepath.Join(home, ".claude", ".session-named")
	projects := config.LoadConfig().OpenProjects
	var out []closedEntry
	seen := map[string]bool{}
	for _, f := range []string{"activity.log", "activity.log.old"} { // newest file first
		path := filepath.Join(dir, f)
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		day := modTime(path)
		lines := strings.Split(string(data), "\n")
		for i := len(lines) - 1; i >= 0; i-- {
			l := lines[i]
			k := strings.Index(l, " session closed: ")
			if k < 0 {
				continue
			}
			name := strings.TrimSpace(l[k+len(" session closed: "):])
			n := session.IssueNumberOf(name)
			if n == 0 || seen[name] {
				continue
			}
			project := ""
			for _, p := range projects {
				if strings.HasPrefix(name, session.ExternalPrefix+filepath.Base(p)+"_i") {
					project = p
				}
			}
			if project == "" {
				continue
			}
			sid, title := conversationOfIssue(named, n)
			if sid == "" {
				continue
			}
			at := day
			if t, err := time.Parse("15:04:05.000", l[:min(12, len(l))]); err == nil {
				at = time.Date(day.Year(), day.Month(), day.Day(), t.Hour(), t.Minute(), t.Second(), 0, day.Location())
				if at.After(day) {
					at = at.AddDate(0, 0, -1)
				}
			}
			seen[name] = true
			out = append(out, closedEntry{Project: project, Name: name, Title: "🟢 " + title, SessionID: sid, Issue: n, At: at})
			if len(out) == recentKept {
				return out
			}
		}
	}
	return out
}

// conversationOfIssue finds the session named after issue n ("…-#n").
func conversationOfIssue(named string, n int) (sid, title string) {
	matches, _ := filepath.Glob(filepath.Join(named, "*.title"))
	var newest time.Time
	for _, f := range matches {
		data, err := os.ReadFile(f)
		t := strings.TrimSpace(string(data))
		if err != nil || !strings.HasSuffix(t, fmt.Sprintf("-#%d", n)) {
			continue
		}
		if mt := modTime(f); mt.After(newest) {
			newest, sid, title = mt, strings.TrimSuffix(filepath.Base(f), ".title"), t
		}
	}
	return sid, title
}

func saveRecent(list []closedEntry) {
	if inTest {
		return
	}
	if data, err := json.Marshal(list); err == nil {
		_ = os.WriteFile(recentFile(), data, 0o600)
	}
}

// noteClosed records a closed Claude session (newest first).
func (m *home) noteClosed(e closedEntry) {
	if e.SessionID == "" {
		return
	}
	list := []closedEntry{e}
	for _, x := range m.recent {
		if x.SessionID != e.SessionID {
			list = append(list, x)
		}
	}
	if len(list) > recentKept {
		list = list[:recentKept]
	}
	m.recent = list
	saveRecent(list)
	m.layoutLeft()
}

// projectRecent is the active project's last closed sessions that aren't
// open again.
func (m *home) projectRecent() []closedEntry {
	project := m.projectTabs.Active()
	open := map[string]bool{}
	for _, e := range m.list.ExternalSessions() {
		if e.SessionID != "" {
			open[e.SessionID] = true
		}
	}
	var out []closedEntry
	for _, e := range m.recent {
		if e.Project == project && !open[e.SessionID] {
			out = append(out, e)
			if len(out) == recentShown {
				break
			}
		}
	}
	return out
}

// recentHeight is the rows the list takes: a heading and one per session.
func (m *home) recentHeight() int {
	if n := len(m.projectRecent()); n > 0 {
		return n + 1
	}
	return 0
}

// renderRecent draws the list, or "" when nothing was closed here.
func (m *home) renderRecent() string {
	list := m.projectRecent()
	if len(list) == 0 {
		return ""
	}
	w := m.leftWidth
	lines := []string{" " + serversHeadStyle.Render("Recently closed")}
	for _, e := range list {
		when := " · " + ago(e.At) // the title gives way, never the time
		text := " " + ansi.Truncate(e.Title, max(4, w-recentOpenW-1-ansi.StringWidth(when)), "…") + when
		pad := max(0, w-recentOpenW-ansi.StringWidth(text))
		lines = append(lines, serversDimStyle.Render(text)+strings.Repeat(" ", pad)+recentOpenStyle.Render(" ↺ reopen"))
	}
	return strings.Join(lines, "\n")
}

// recentTop is the screen row of the "Recently closed" heading.
func (m *home) recentTop() int { return m.dockTop() - m.recentHeight() }

// handleRecentMouse reopens the session whose row was clicked.
func (m *home) handleRecentMouse(msg tea.MouseMsg) (tea.Cmd, bool) {
	if m.state != stateDefault || msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft ||
		msg.X >= m.leftWidth {
		return nil, false
	}
	row := msg.Y - m.recentTop() - 1
	list := m.projectRecent()
	if row < 0 || row >= len(list) {
		return nil, false
	}
	if msg.X < m.leftWidth-recentOpenW {
		return nil, true // only "↺ reopen" reopens; a stray click on the row doesn't
	}
	return m.reopenClosed(list[row]), true
}

// reopenClosed resumes a closed session's conversation in a new tile.
func (m *home) reopenClosed(e closedEntry) tea.Cmd {
	program := m.program
	logEvent("reopening %s (%s)", e.Name, e.SessionID)
	return func() tea.Msg {
		name, err := session.ResumeSession(e.Project, program, e.SessionID, e.Issue)
		if err != nil {
			return err
		}
		list, _, _ := session.ListExternalSessions()
		return sessionStartedMsg{name: name, sessions: list}
	}
}

// ago says how long ago t was, briefly.
func ago(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}
