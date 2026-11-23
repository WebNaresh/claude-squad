package app

import (
	"claude-squad/session"
	"claude-squad/ui"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

// A project with no open issue, no session and no recent Claude use leaves the
// tab row. It stays in open_projects, so its issues are still read and a new
// one brings the tab back (closing a tab would stop that).
const (
	tabQuietAfter = 7 * 24 * time.Hour
	tabQuietEvery = 30 * time.Second
)

type quietTabsMsg struct {
	quiet map[string]bool
	used  map[string]time.Time
}

var transcriptDirRe = regexp.MustCompile(`[^a-zA-Z0-9]`)

// lastClaudeUse is when Claude last wrote a transcript for project: this sees
// sessions started outside cs too. Zero when it never ran there.
func lastClaudeUse(project string) time.Time {
	home, err := os.UserHomeDir()
	if err != nil {
		return time.Time{}
	}
	dir := filepath.Join(home, ".claude", "projects", transcriptDirRe.ReplaceAllString(project, "-"))
	entries, err := os.ReadDir(dir)
	if err != nil {
		return time.Time{}
	}
	var last time.Time
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".jsonl") {
			continue
		}
		if info, err := e.Info(); err == nil && info.ModTime().After(last) {
			last = info.ModTime()
		}
	}
	return last
}

// hasOpenIssue reports an open issue that wasn't skipped.
func hasOpenIssue(issues []session.Issue, skipped map[int]bool) bool {
	for _, is := range issues {
		if !skipped[is.Number] {
			return true
		}
	}
	return false
}

// readQuietTabs works out the quiet projects. loaded holds the issue lists the
// window has; other projects use the saved list. A project whose issues can't
// be read (no GitHub remote) counts as having none. added is when projects
// were added as tabs. Sessions are checked by the tab row itself, so a new one
// shows at once.
func readQuietTabs(projects []string, loaded map[string][]session.Issue, added map[string]time.Time, now time.Time) quietTabsMsg {
	msg := quietTabsMsg{quiet: map[string]bool{}, used: map[string]time.Time{}}
	for _, p := range projects {
		issues, ok := loaded[p]
		if !ok {
			issues, _ = readIssueCache(p)
		}
		if hasOpenIssue(issues, readSkipped(p)) {
			continue
		}
		msg.used[p] = lastClaudeUse(p)
		recent := msg.used[p]
		if added[p].After(recent) {
			recent = added[p]
		}
		msg.quiet[p] = now.Sub(recent) > tabQuietAfter
	}
	return msg
}

// refreshQuietTabs re-reads them at most every tabQuietEvery.
func (m *home) refreshQuietTabs() tea.Cmd {
	if time.Since(m.quietAt) < tabQuietEvery {
		return nil
	}
	m.quietAt = time.Now()
	projects := append([]string(nil), m.projectTabs.Projects()...)
	loaded := make(map[string][]session.Issue, len(m.issueCache))
	for p, issues := range m.issueCache {
		loaded[p] = issues
	}
	added := make(map[string]time.Time, len(m.appConfig.ProjectAdded))
	for p, t := range m.appConfig.ProjectAdded {
		added[p] = t
	}
	return func() tea.Msg { return readQuietTabs(projects, loaded, added, time.Now()) }
}

func (m *home) applyQuietTabs(msg quietTabsMsg) {
	m.projectTabs.SetQuiet(msg.quiet)
	m.tabLastUsed = msg.used
}

// tabPicker is the list behind the "⋯ N more" chip.
type tabPicker struct {
	projects []string
	names    []string
	used     []string
	at       int
}

var (
	tabPickerBox = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#0078d4")).Padding(1, 2)
	tabPickerSel  = lipgloss.NewStyle().Background(lipgloss.Color("#0078d4")).Foreground(lipgloss.Color("#ffffff")).Bold(true)
	tabPickerDim  = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#A49FA5", Dark: "#777777"})
	tabPickerHead = lipgloss.NewStyle().Bold(true)
)

// tabPickerFirstRow is the first project row inside the box: border, padding,
// title, blank line.
const tabPickerFirstRow = 4

func (p *tabPicker) render() string {
	nameW, usedW := 0, 0
	for i := range p.projects {
		nameW = max(nameW, runewidth.StringWidth(p.names[i]))
		usedW = max(usedW, runewidth.StringWidth(p.used[i]))
	}
	rows := []string{tabPickerHead.Render("Hidden projects"), ""}
	for i := range p.projects {
		name := runewidth.FillRight(p.names[i], nameW)
		used := fmt.Sprintf("%*s", usedW, p.used[i])
		if i == p.at {
			rows = append(rows, tabPickerSel.Render(" "+name+"   "+used+" "))
		} else {
			rows = append(rows, " "+name+"   "+tabPickerDim.Render(used)+" ")
		}
	}
	rows = append(rows, "", tabPickerDim.Render("A new issue brings a project back on its own."),
		tabPickerDim.Render("↑↓ choose · Enter open · Esc close"))
	return tabPickerBox.Render(strings.Join(rows, "\n"))
}

// openTabPicker lists the hidden projects, last used first.
func (m *home) openTabPicker() tea.Cmd {
	hidden := m.projectTabs.HiddenProjects()
	if len(hidden) == 0 {
		return m.handleError(fmt.Errorf("no hidden projects: every project is on the tab row"))
	}
	all := m.projectTabs.Projects()
	allNames := ui.TabNames(all)
	p := &tabPicker{}
	for _, h := range hidden {
		used := "never used"
		if t := m.tabLastUsed[h]; !t.IsZero() {
			used = ago(t)
		}
		p.projects = append(p.projects, h)
		p.names = append(p.names, allNames[indexOf(all, h)])
		p.used = append(p.used, used)
	}
	m.tabPicker = p
	m.state = stateTabPicker
	return nil
}

// pickTab closes the list and, for i >= 0, opens that project. Its tab stays
// until you leave it.
func (m *home) pickTab(i int) tea.Cmd {
	p := m.tabPicker
	m.tabPicker = nil
	m.state = stateDefault
	if i < 0 || i >= len(p.projects) {
		return nil
	}
	return m.switchProject(m.projectTabs.Select(indexOf(m.projectTabs.Projects(), p.projects[i])))
}

func (m *home) handleTabPickerKey(msg tea.KeyMsg) tea.Cmd {
	p := m.tabPicker
	if p == nil {
		m.state = stateDefault
		return nil
	}
	switch msg.String() {
	case "up", "k":
		p.at = (p.at - 1 + len(p.projects)) % len(p.projects)
	case "down", "j":
		p.at = (p.at + 1) % len(p.projects)
	case "enter":
		return m.pickTab(p.at)
	case "esc", "ctrl+q":
		return m.pickTab(-1)
	}
	return nil
}

// handleTabPickerMouse opens a clicked row; a click outside the box closes it.
func (m *home) handleTabPickerMouse(msg tea.MouseMsg) tea.Cmd {
	p := m.tabPicker
	if p == nil || msg.Action != tea.MouseActionPress || msg.Button != tea.MouseButtonLeft {
		return nil
	}
	box := p.render()
	h, w := lipgloss.Height(box), lipgloss.Width(box)
	top, left := max(0, (m.screenHeight-h)/2), max(0, (m.screenWidth-w)/2)
	if msg.X < left || msg.X >= left+w || msg.Y < top || msg.Y >= top+h {
		return m.pickTab(-1)
	}
	if i := msg.Y - top - tabPickerFirstRow; i >= 0 && i < len(p.projects) {
		return m.pickTab(i)
	}
	return nil
}
