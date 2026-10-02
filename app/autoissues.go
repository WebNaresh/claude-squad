package app

import (
	"claude-squad/session"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Auto issue loop. For each project with auto on, every few seconds cs
// counts its Claude sessions; when fewer than autoMaxSessions run and the
// open PR has room (autoMaxPerPR issues), it opens the issue picker on that
// project with as many issues ticked as there are free slots (oldest first,
// never skipped ones). Only on the tab you're on: other tabs show "·N free"
// and ask when you open them. Enter starts them (the usual
// queued gai flow), Esc means "not now": it asks again only after another
// session of that project closes. It never opens while you type
// (typingPause), while another dialog is up, or while an issue queue runs.
// A full PR is said once, in the message line. It is on for every project;
// a in the picker switches it off (or back on) for that project.

const (
	autoMaxSessions = 6
	autoMaxPerPR    = 15
	autoEvery       = 5 * time.Second
	autoListMaxAge  = time.Minute
)

type autoTickMsg struct{}

func autoTick() tea.Cmd {
	return func() tea.Msg {
		time.Sleep(autoEvery)
		return autoTickMsg{}
	}
}

// The loop is on for every project unless switched off: a marker file next
// to the project's issue cache (not config.json, which a running cs keeps in
// memory and rewrites, so an edit from outside would be lost).
func autoOffFile(project string) string {
	if f := issueCacheFile(project); f != "" {
		return strings.TrimSuffix(f, ".json") + ".auto-off"
	}
	return ""
}

// autoOn reports whether the loop runs for project.
func (m *home) autoOn(project string) bool {
	f := autoOffFile(project)
	if f == "" {
		return false
	}
	_, err := os.Stat(f)
	return err != nil
}

// toggleAuto switches the loop for project.
func (m *home) toggleAuto(project string) {
	f := autoOffFile(project)
	if f == "" {
		return
	}
	if m.autoOn(project) {
		if err := os.MkdirAll(filepath.Dir(f), 0o755); err == nil {
			_ = os.WriteFile(f, []byte("auto issue loop off for "+project+"\n"), 0o644)
		}
	} else {
		_ = os.Remove(f)
	}
	logEvent("auto issues %s: %v", filepath.Base(project), m.autoOn(project))
}

// autoRunning counts the Claude sessions of project: cs agents, sessions
// with a Claude status, issue sessions (also while queued or still in gai),
// but not plain terminals.
func (m *home) autoRunning(project string) int {
	n := m.list.CountInProject(project)
	for _, e := range m.list.ExternalSessions() {
		if m.projectOf(e.Path) != project {
			continue
		}
		if e.Status != "" || session.IssueNumberOf(e.Name) > 0 ||
			strings.HasPrefix(e.Name, session.ExternalPrefix) {
			n++
		}
	}
	return n
}

// prRoom returns how many more issues the project's open PR takes, and the
// PR (0 when none is open). gai attaches every issue to the one open PR.
func prRoom(issues []session.Issue) (room, pr int) {
	count := map[int]int{}
	for _, is := range issues {
		if is.PR > 0 {
			count[is.PR]++
			if count[is.PR] > count[pr] || pr == 0 {
				pr = is.PR
			}
		}
	}
	return autoMaxPerPR - count[pr], pr
}

// stepAuto runs autoCheck and schedules the next one.
func (m *home) stepAuto() tea.Cmd {
	return tea.Batch(m.autoCheck(), autoTick())
}

// autoCheck works out, for every auto project, how many issues could start
// now. Other tabs get a "·N free" badge; the tab you're on gets the picker,
// once you've stopped typing and no other dialog or issue queue is busy.
func (m *home) autoCheck() tea.Cmd {
	if !m.sessionsLoaded {
		return nil
	}
	if m.autoDeclined == nil {
		m.autoDeclined, m.autoFetched, m.autoSaid = map[string]int{}, map[string]time.Time{}, map[string]string{}
	}
	var cmds []tea.Cmd
	fetching := false
	for _, p := range m.projectTabs.Projects() {
		m.projectTabs.SetFree(p, 0)
		if !m.autoOn(p) {
			continue
		}
		running := m.autoRunning(p)
		free := autoMaxSessions - running
		if free <= 0 {
			continue
		}
		issues, cached := m.issueCache[p]
		if (!cached || time.Since(m.autoFetched[p]) > autoListMaxAge) && !fetching {
			// One list load per check; the next check decides on it.
			fetching = true
			m.autoFetched[p] = time.Now()
			cmds = append(cmds, loadIssues(p))
		}
		if !cached {
			continue
		}
		room, pr := prRoom(issues)
		if room <= 0 {
			if key := fmt.Sprint(pr, "full"); m.autoSaid[p] != key && p == m.projectTabs.Active() {
				m.autoSaid[p] = key
				cmds = append(cmds, m.handleError(fmt.Errorf("auto: PR #%d of %s has %d issues (the limit): merge it to start more",
					pr, filepath.Base(p), autoMaxPerPR)))
			}
			continue
		}
		busy, skipped := m.busyIssues(p), readSkipped(p)
		offer := 0
		for _, is := range issues {
			if is.PR == 0 && !busy[is.Number] && !skipped[is.Number] {
				offer++
			}
		}
		n := min(free, room, offer)
		if n <= 0 {
			continue
		}
		if p != m.projectTabs.Active() {
			m.projectTabs.SetFree(p, n) // asked when you open that tab
			continue
		}
		// Esc on the last offer: quiet until a session here closes.
		if d, ok := m.autoDeclined[p]; ok {
			if running > d {
				m.autoDeclined[p] = running
			}
			if running >= m.autoDeclined[p] {
				continue
			}
			delete(m.autoDeclined, p)
		}
		if m.state != stateDefault || m.issuePicker != nil || m.issueJob != nil || len(m.issueQueue) > 0 ||
			time.Since(m.lastKey) < typingPause {
			continue
		}
		delete(m.autoSaid, p)
		cmds = append(cmds, m.offerIssues(p, n, running, room, pr))
	}
	return tea.Batch(cmds...)
}

// offerIssues opens the picker on project with n issues ticked.
func (m *home) offerIssues(project string, n, running, room, pr int) tea.Cmd {
	slots := "slot"
	if n > 1 {
		slots = "slots"
	}
	note := fmt.Sprintf("%d free %s (%d of %d sessions running)", autoMaxSessions-running, slots, running, autoMaxSessions)
	if pr > 0 {
		note += fmt.Sprintf(" · PR #%d has room for %d more", pr, room)
	}
	note += " · enter starts the ticked ones, esc: not now"
	logEvent("auto issues: offering %d issue(s) in %s (%d running, PR room %d)", n, filepath.Base(project), running, room)
	m.newIssuePickerTicking(project, n, note)
	m.state = stateIssuePicker
	m.autoPrompt = project
	return loadIssues(project)
}

// autoPickerClosed records how an auto offer ended: Esc declines until a
// session of the project closes.
func (m *home) autoPickerClosed(project string, started bool) {
	if m.autoPrompt != project {
		m.autoPrompt = ""
		return
	}
	m.autoPrompt = ""
	if started {
		delete(m.autoDeclined, project)
		return
	}
	m.autoDeclined[project] = m.autoRunning(project)
	logEvent("auto issues: declined in %s; asks again when a session closes", filepath.Base(project))
}
