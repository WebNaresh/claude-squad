package app

import (
	"claude-squad/session"
	"claude-squad/session/git"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// The last row is a status bar like VS Code's: on the left the branch,
// commits to pull / push (click to push) and the branch's pull request
// (click to open it in the browser); on the right cs's latest message.

type statusBar struct {
	sync    map[string]git.SyncState // per project root, from the Source Control refresh
	prs     map[string]prEntry
	pushing map[string]bool
	// Where the clickable parts were drawn on the bottom row (columns).
	syncSpan, prSpan [2]int
}

type prEntry struct {
	branch  string
	pr      git.PullRequest
	ok      bool
	fetched time.Time
	loading bool
}

type prMsg struct {
	root, branch string
	pr           git.PullRequest
	ok           bool
}

type pushDoneMsg struct {
	root  string
	ahead int
	err   error
}

// barBG is VS Code Dark Modern's status bar colour.
const barBG = lipgloss.Color("#181818")

var (
	barStyle     = lipgloss.NewStyle().Background(barBG)
	barTextStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#cccccc")).Background(barBG)
	barSyncStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#cccccc")).Background(lipgloss.Color("#313131"))
	barBusyStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#ffffff")).Background(lipgloss.Color("#0078d4"))
	barPRStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#4fc1ff")).Background(barBG).Underline(true)
)

// refreshPR looks up the branch's pull request in the background, at most
// once a minute per project and again when the branch changes.
func (m *home) refreshPR(root, branch string) tea.Cmd {
	if m.bar.prs == nil {
		m.bar.prs = map[string]prEntry{}
	}
	e := m.bar.prs[root]
	if e.loading || (e.branch == branch && time.Since(e.fetched) < time.Minute) || branch == "" {
		return nil
	}
	e.loading = true
	m.bar.prs[root] = e
	return func() tea.Msg {
		pr, ok := git.CurrentPR(root)
		return prMsg{root: root, branch: branch, pr: pr, ok: ok}
	}
}

func (m *home) applyPR(msg prMsg) {
	m.bar.prs[msg.root] = prEntry{branch: msg.branch, pr: msg.pr, ok: msg.ok, fetched: time.Now()}
}

// statusRight renders the right end of the bottom row; start is the column
// it is drawn at, to remember where the clickable parts are.
func (m *home) statusRight(start int) string {
	root := m.projectTabs.Active()
	st, ok := m.scCache[root]
	if !ok || st.branch == "" {
		return ""
	}
	parts := []string{barTextStyle.Render("⎇ " + st.branch)}
	col := start + lipgloss.Width(parts[0]) + 2
	m.bar.syncSpan, m.bar.prSpan = [2]int{}, [2]int{}

	var sync string
	sy := m.bar.sync[root]
	switch {
	case m.bar.pushing[root]:
		sync = barBusyStyle.Render(" ⟳ pushing… ")
	case sy.Upstream == "":
		sync = barSyncStyle.Render(" ⇡ publish branch ")
	default:
		sync = barSyncStyle.Render(fmt.Sprintf(" ↓%d ↑%d ", sy.Behind, sy.Ahead))
	}
	m.bar.syncSpan = [2]int{col, col + lipgloss.Width(sync)}
	parts = append(parts, sync)
	col += lipgloss.Width(sync) + 2

	if e := m.bar.prs[root]; e.ok && e.branch == st.branch {
		label := fmt.Sprintf("PR #%d", e.pr.Number)
		if e.pr.State != "" && e.pr.State != "OPEN" {
			label += " · " + strings.ToLower(e.pr.State)
		}
		pr := barPRStyle.Render(label)
		m.bar.prSpan = [2]int{col, col + lipgloss.Width(pr)}
		parts = append(parts, pr)
	}
	return strings.Join(parts, barStyle.Render("  "))
}

// statusRow is the last row: git status on the left, the latest message on
// the right, on the status bar colour across the whole width.
func (m *home) statusRow(width int) string {
	left := barStyle.Render(" ") + m.statusRight(1)
	lw := lipgloss.Width(left)
	msg := m.errBox.Text(max(0, width-lw-4), barBG)
	gap := max(1, width-lw-lipgloss.Width(msg)-1)
	return left + barStyle.Render(strings.Repeat(" ", gap)) + msg + barStyle.Render(" ")
}

// pushProject asks, then pushes the active project's branch.
func (m *home) pushProject() tea.Cmd {
	root := m.projectTabs.Active()
	st := m.scCache[root]
	sy := m.bar.sync[root]
	if st.branch == "" {
		return m.handleError(fmt.Errorf("this project isn't on a branch"))
	}
	if m.bar.pushing[root] {
		return nil
	}
	if sy.Upstream != "" && sy.Ahead == 0 {
		return m.handleError(fmt.Errorf("nothing to push: %s is up to date with %s", st.branch, sy.Upstream))
	}
	question := fmt.Sprintf("Push %d commit(s) on %s to %s?", sy.Ahead, st.branch, sy.Upstream)
	if sy.Upstream == "" {
		question = fmt.Sprintf("Publish branch %s to origin?", st.branch)
	}
	if sy.Behind > 0 {
		question += fmt.Sprintf(" (%d to pull first: the push may be refused)", sy.Behind)
	}
	return m.confirmAction(question, func() tea.Msg {
		return pushStartMsg{root: root}
	})
}

type pushStartMsg struct{ root string }

func (m *home) startPush(root string) tea.Cmd {
	if m.bar.pushing == nil {
		m.bar.pushing = map[string]bool{}
	}
	m.bar.pushing[root] = true
	ahead := m.bar.sync[root].Ahead
	logEvent("push started: %s (%d commits)", root, ahead)
	return func() tea.Msg {
		return pushDoneMsg{root: root, ahead: ahead, err: git.Push(root)}
	}
}

func (m *home) pushDone(msg pushDoneMsg) tea.Cmd {
	delete(m.bar.pushing, msg.root)
	if msg.err != nil {
		logEvent("push failed: %v", msg.err)
		return tea.Batch(m.handleError(msg.err), m.refreshSourceControl())
	}
	logEvent("pushed %d commits: %s", msg.ahead, msg.root)
	if e, ok := m.bar.prs[msg.root]; ok {
		e.fetched = time.Time{} // a first push may have opened the way for a PR
		m.bar.prs[msg.root] = e
	}
	return tea.Batch(m.handleError(fmt.Errorf("pushed %d commit(s)", msg.ahead)), m.refreshSourceControl())
}

// openPR opens the active project's pull request in the browser.
func (m *home) openPR() tea.Cmd {
	e := m.bar.prs[m.projectTabs.Active()]
	if !e.ok {
		return m.handleError(fmt.Errorf("this branch has no pull request"))
	}
	url := e.pr.URL
	return func() tea.Msg {
		if err := session.OpenInBrowser(url); err != nil {
			return err
		}
		return fmt.Errorf("opened PR #%d", e.pr.Number)
	}
}

// clickBottomRow handles a click on the status line; false when the click
// wasn't on it.
func (m *home) clickBottomRow(x int) (tea.Cmd, bool) {
	switch {
	case x >= m.bar.syncSpan[0] && x < m.bar.syncSpan[1]:
		return m.pushProject(), true
	case x >= m.bar.prSpan[0] && x < m.bar.prSpan[1]:
		return m.openPR(), true
	}
	return nil, false
}
