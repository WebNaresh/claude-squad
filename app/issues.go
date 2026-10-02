package app

import (
	"claude-squad/config"
	"claude-squad/session"
	"claude-squad/ui"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// ⌥N: pick a project and some of its open GitHub issues (oldest first) and
// start one Claude session per issue with `gai issue <url>`. Every picked
// issue gets its session (tile) at once, showing "queued"; the gai runs go
// one after another in them: each gai attaches its issue to the project's
// single open PR, so running them together would race on that PR. cs answers
// gai's [Y/n] questions with the default (Enter) and skips its optional
// "Extra instructions for Claude" prompt the same way, except the PR rewrite:
// each rewrite (~40s with the local AI) replaces the previous one, so only
// the batch's last issue in a project rewrites it; the others answer n.

const issueJobTimeout = 5 * time.Minute

type issuesLoadedMsg struct {
	project string
	issues  []session.Issue
	err     error
}

type issueTickMsg struct{}

type issueJob struct {
	project  string
	issue    session.Issue
	name     string
	started  time.Time
	answered string // the last [Y/n] question answered, so it is answered once
}

type queuedIssue struct {
	project string
	issue   session.Issue
}

func loadIssues(project string) tea.Cmd {
	return func() tea.Msg {
		issues, err := session.ListOpenIssues(project)
		return issuesLoadedMsg{project: project, issues: issues, err: err}
	}
}

// openIssuePicker shows the picker for the active project.
func (m *home) openIssuePicker() tea.Cmd {
	project := m.projectTabs.Active()
	if project == "" {
		return m.handleError(fmt.Errorf("open a project first"))
	}
	m.newIssuePicker(project)
	m.state = stateIssuePicker
	return loadIssues(project)
}

// newIssuePicker opens the picker on project, showing its last issue list
// at once (if any) while the fresh one loads.
func (m *home) newIssuePicker(project string) {
	m.issuePicker = ui.NewIssuePicker(project, projectNames(m.projectTabs.Projects())[project])
	m.issuePicker.SetSize(m.screenWidth, m.screenHeight)
	m.issuePicker.SetSkipped(readSkipped(project))
	m.issuePicker.NewestFirst = readPrefs(project).NewestFirst
	cached, ok := m.issueCache[project]
	if !ok {
		cached, ok = readIssueCache(project)
	}
	if ok {
		m.issuePicker.ShowCached(cached, m.busyIssues(project))
	}
}

// The last issue list is also kept on disk, so the picker isn't empty for
// the first seconds after cs restarts (it restarts itself on every build).
func issueCacheFile(project string) string {
	dir, err := config.GetConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "issues", galleryNameRe.ReplaceAllString(project, "-")+".json")
}

func readIssueCache(project string) ([]session.Issue, bool) {
	data, err := os.ReadFile(issueCacheFile(project))
	if err != nil {
		return nil, false
	}
	var c []cachedIssue
	if json.Unmarshal(data, &c) != nil {
		return nil, false
	}
	out := make([]session.Issue, len(c))
	for i, ci := range c {
		out[i] = ci.Issue
		out[i].PR = ci.PR
	}
	return out, true
}

func writeIssueCache(project string, issues []session.Issue) {
	f := issueCacheFile(project)
	if f == "" || os.MkdirAll(filepath.Dir(f), 0o755) != nil {
		return
	}
	c := make([]cachedIssue, len(issues))
	for i, is := range issues {
		c[i] = cachedIssue{is, is.PR}
	}
	if data, err := json.Marshal(c); err == nil {
		_ = os.WriteFile(f, data, 0o644)
	}
}

// Skipped issues (unticked in the picker) are kept per project, so the
// picker ticks the next ones instead next time.
func skippedFile(project string) string {
	if f := issueCacheFile(project); f != "" {
		return strings.TrimSuffix(f, ".json") + ".skipped.json"
	}
	return ""
}

func readSkipped(project string) map[int]bool {
	out := map[int]bool{}
	data, err := os.ReadFile(skippedFile(project))
	if err != nil {
		return out
	}
	var nums []int
	if json.Unmarshal(data, &nums) == nil {
		for _, n := range nums {
			out[n] = true
		}
	}
	return out
}

func writeSkipped(project string, nums []int) {
	f := skippedFile(project)
	if f == "" || os.MkdirAll(filepath.Dir(f), 0o755) != nil {
		return
	}
	if data, err := json.Marshal(nums); err == nil {
		_ = os.WriteFile(f, data, 0o644)
	}
}

// issuePrefs is how the picker lists a project's issues, kept per project.
type issuePrefs struct {
	NewestFirst bool `json:"newest_first,omitempty"`
}

func prefsFile(project string) string {
	if f := issueCacheFile(project); f != "" {
		return strings.TrimSuffix(f, ".json") + ".prefs.json"
	}
	return ""
}

func readPrefs(project string) issuePrefs {
	var p issuePrefs
	if data, err := os.ReadFile(prefsFile(project)); err == nil {
		_ = json.Unmarshal(data, &p)
	}
	return p
}

func writePrefs(project string, p issuePrefs) {
	f := prefsFile(project)
	if f == "" || os.MkdirAll(filepath.Dir(f), 0o755) != nil {
		return
	}
	if data, err := json.Marshal(p); err == nil {
		_ = os.WriteFile(f, data, 0o644)
	}
}

// cachedIssue stores PR too (Issue leaves it out of its JSON).
type cachedIssue struct {
	session.Issue
	PR int `json:"pr"`
}

// busyIssues returns the issue numbers that already have a session in project.
func (m *home) busyIssues(project string) map[int]bool {
	busy := map[int]bool{}
	for _, e := range m.list.ExternalSessions() {
		if n := session.IssueNumberOf(e.Name); n > 0 && m.projectOf(e.Path) == project {
			busy[n] = true
		}
	}
	for _, j := range m.issueQueue {
		if j.project == project {
			busy[j.issue.Number] = true
		}
	}
	if m.issueJob != nil && m.issueJob.project == project {
		busy[m.issueJob.issue.Number] = true
	}
	return busy
}

// handleIssuePickerKey drives the picker.
func (m *home) handleIssuePickerKey(msg tea.KeyMsg) tea.Cmd {
	p := m.issuePicker
	switch msg.String() {
	case "left", "right", "alt+left", "alt+right", "alt+b", "alt+f":
		projects := m.projectTabs.Projects()
		i := indexOf(projects, p.Project)
		if strings.HasSuffix(msg.String(), "left") || msg.String() == "alt+b" {
			i = (i - 1 + len(projects)) % len(projects)
		} else {
			i = (i + 1) % len(projects)
		}
		m.newIssuePicker(projects[i])
		return loadIssues(projects[i])
	case "o":
		is, ok := p.Current()
		if !ok {
			return nil
		}
		return func() tea.Msg {
			if err := session.OpenInBrowser(is.URL); err != nil {
				return err
			}
			return nil
		}
	}
	if msg.String() == "s" && !p.Loading {
		p.ToggleOrder()
		writePrefs(p.Project, issuePrefs{NewestFirst: p.NewestFirst})
		return nil
	}
	done, start := p.HandleKey(msg.String())
	if k := msg.String(); (k == " " || k == "x") && !p.Loading {
		writeSkipped(p.Project, p.Skipped())
	}
	if !done {
		return nil
	}
	m.issuePicker = nil
	m.state = stateDefault
	if !start {
		return nil
	}
	for _, is := range p.Selected() {
		if _, err := session.QueueIssueSession(p.Project, is); err != nil {
			logEvent("issue queue: could not open #%d's tile: %v", is.Number, err)
		}
		m.issueQueue = append(m.issueQueue, queuedIssue{project: p.Project, issue: is})
	}
	// Show every new tile now, not on the next session refresh.
	if list, _, err := session.ListExternalSessions(); err == nil {
		m.setExternalSessions(list)
	}
	return tea.Batch(m.handleError(fmt.Errorf("opened %d issue session(s); gai attaches them to the PR one at a time", len(p.Selected()))),
		m.issueTick(0), m.refreshGrid())
}

func (m *home) issueTick(d time.Duration) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(d)
		return issueTickMsg{}
	}
}

// stepIssues advances the issue queue: start the next gai issue, answer its
// [Y/n] questions, and move on once Claude is running in it.
func (m *home) stepIssues() tea.Cmd {
	if m.issueJob == nil {
		if len(m.issueQueue) == 0 {
			return nil
		}
		next := m.issueQueue[0]
		m.issueQueue = m.issueQueue[1:]
		logEvent("issue queue: starting #%d in %s", next.issue.Number, filepath.Base(next.project))
		name, err := session.StartIssueSession(next.project, next.issue)
		if err != nil {
			return tea.Batch(m.handleError(err), m.issueTick(time.Second))
		}
		m.issueJob = &issueJob{project: next.project, issue: next.issue, name: name, started: time.Now()}
		return m.issueTick(time.Second)
	}

	j := m.issueJob
	line, alive := session.PaneLastLine(j.name)
	switch {
	case !alive:
		m.issueJob = nil
		return tea.Batch(m.handleError(fmt.Errorf("gai issue #%d stopped before Claude started; see `gai --logs`", j.issue.Number)), m.issueTick(0))
	case m.claudeRunningIn(j.name):
		m.issueJob = nil
		return m.issueTick(0)
	case time.Since(j.started) > issueJobTimeout:
		m.issueJob = nil
		return tea.Batch(m.handleError(fmt.Errorf("issue #%d is taking long; it stays in its session, moving on", j.issue.Number)), m.issueTick(0))
	case isGaiPrompt(line) && line != j.answered:
		j.answered = line
		if strings.Contains(line, "Rewrite the PR title and body") && m.moreQueuedIn(j.project) {
			logEvent("issue queue: #%d answered n to %q (a later issue rewrites the PR)", j.issue.Number, line)
			_ = session.PressKeys(j.name, "n")
		} else {
			logEvent("issue queue: #%d answered Enter to %q", j.issue.Number, line)
			_ = session.PressEnter(j.name)
		}
	}
	return m.issueTick(time.Second)
}

// claudeRunningIn reports whether Claude has started in a tmux session
// (it then shows up in `claude agents` with a status).
func (m *home) claudeRunningIn(name string) bool {
	for _, e := range m.list.ExternalSessions() {
		if e.Name == name && e.Status != "" {
			return true
		}
	}
	return false
}

// isGaiPrompt reports whether a gai issue screen line waits for an answer
// whose default (Enter) is right for an unattended run.
func isGaiPrompt(line string) bool {
	return strings.Contains(line, "[Y/n]") || strings.HasPrefix(line, "Extra instructions for Claude")
}

// moreQueuedIn reports whether another picked issue of project waits its turn.
func (m *home) moreQueuedIn(project string) bool {
	for _, q := range m.issueQueue {
		if q.project == project {
			return true
		}
	}
	return false
}
