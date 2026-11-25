package app

import (
	"claude-squad/session"
	"claude-squad/session/git"
	"claude-squad/ui"
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
	sync map[string]git.SyncState // per project root, from the Source Control refresh
	prs  map[string]prEntry
	// stripPRs: the checks of the PR the progress strip shows (the batch PR
	// issues gather on), which is not always the checked-out branch's PR.
	stripPRs map[string]prEntry
	pushing  map[string]bool
	pulling  map[string]bool
	fetched  map[string]time.Time // last background fetch, per project
	// Where the clickable parts were drawn on the bottom row (columns).
	syncSpan, pullSpan, prSpan, refreshSpan [2]int
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
	ok, failed   bool
}

type pushDoneMsg struct {
	root    string
	ahead   int
	err     error
	in      *git.Incoming // set when the push failed because both sides moved
	started time.Time
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

// prRefreshEvery is how often the PR and its checks are re-read: short, so a
// check turning red or green shows without opening the browser.
const prRefreshEvery = 10 * time.Second

// refreshPR looks up the branch's pull request in the background, at most
// every prRefreshEvery per project and again when the branch changes.
func (m *home) refreshPR(root, branch string) tea.Cmd {
	if m.bar.prs == nil {
		m.bar.prs = map[string]prEntry{}
	}
	e := m.bar.prs[root]
	if e.loading || (e.branch == branch && time.Since(e.fetched) < prRefreshEvery) || branch == "" {
		return nil
	}
	e.loading = true
	m.bar.prs[root] = e
	return func() tea.Msg {
		pr, ok, failed := git.CurrentPR(root)
		return prMsg{root: root, branch: branch, pr: pr, ok: ok, failed: failed}
	}
}

func (m *home) applyPR(msg prMsg) {
	// GitHub didn't answer: keep the branch's PR shown, try again next time.
	if old := m.bar.prs[msg.root]; msg.failed && old.ok && old.branch == msg.branch {
		old.loading, old.fetched = false, time.Now()
		m.bar.prs[msg.root] = old
		return
	}
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
	m.bar.syncSpan, m.bar.pullSpan, m.bar.prSpan, m.bar.refreshSpan = [2]int{}, [2]int{}, [2]int{}, [2]int{}

	// ↓N pulls (click), ↑N pushes (click); two buttons, two click spans.
	sy := m.bar.sync[root]
	switch {
	case m.bar.pushing[root]:
		sync := barBusyStyle.Render(" ⟳ pushing… ")
		m.bar.syncSpan = [2]int{col, col + lipgloss.Width(sync)}
		parts = append(parts, sync)
		col += lipgloss.Width(sync) + 2
	case m.bar.pulling[root]:
		sync := barBusyStyle.Render(" ⟳ pulling… ")
		m.bar.pullSpan = [2]int{col, col + lipgloss.Width(sync)}
		parts = append(parts, sync)
		col += lipgloss.Width(sync) + 2
	case sy.Upstream == "":
		sync := barSyncStyle.Render(" ⇡ publish branch ")
		m.bar.syncSpan = [2]int{col, col + lipgloss.Width(sync)}
		parts = append(parts, sync)
		col += lipgloss.Width(sync) + 2
	default:
		pull := barSyncStyle.Render(fmt.Sprintf(" ↓%d ", sy.Behind))
		push := barSyncStyle.Render(fmt.Sprintf(" ↑%d ", sy.Ahead))
		m.bar.pullSpan = [2]int{col, col + lipgloss.Width(pull)}
		col += lipgloss.Width(pull) + 1
		m.bar.syncSpan = [2]int{col, col + lipgloss.Width(push)}
		col += lipgloss.Width(push) + 2
		parts = append(parts, pull+barStyle.Render(" ")+push)
	}

	if e := m.bar.prs[root]; e.ok && e.branch == st.branch {
		label := fmt.Sprintf("PR #%d", e.pr.Number)
		if e.pr.State != "" && e.pr.State != "OPEN" {
			label += " · " + strings.ToLower(e.pr.State)
		}
		pr := barPRStyle.Render(label)
		if ck := checkLabel(e.pr); ck != "" && (e.pr.State == "" || e.pr.State == "OPEN") { // a merged PR's old checks say nothing
			pr += barStyle.Render(" ") + ck
		}
		m.bar.prSpan = [2]int{col, col + lipgloss.Width(pr)}
		icon, istyle := " ↻ ", barSyncStyle
		if e.loading {
			icon, istyle = " ⟳ ", barBusyStyle
		}
		ric := istyle.Render(icon)
		start := m.bar.prSpan[1] + 2
		m.bar.refreshSpan = [2]int{start, start + lipgloss.Width(ric)}
		parts = append(parts, pr, ric)
	}
	return strings.Join(parts, barStyle.Render("  "))
}

// checkLabel is the PR's CI result for the status bar: "✓ 5/5 checks",
// "✗ 1 failed", "● 2 of 5 running"; "" when the PR has no checks.
func checkLabel(pr git.PullRequest) string {
	text, c := checkText(pr)
	if text == "" {
		return ""
	}
	return lipgloss.NewStyle().Foreground(c).Background(barBG).Bold(true).Render(text)
}

// checkText is the check summary and its colour; "" when there are no checks.
func checkText(pr git.PullRequest) (string, lipgloss.Color) {
	pass, fail, pending := pr.CheckSummary()
	total := pass + fail + pending
	green, red, amber := lipgloss.Color("#3fb950"), lipgloss.Color("#f85149"), lipgloss.Color("#d29922")
	switch {
	case total == 0:
		return "", green
	case fail > 0:
		return fmt.Sprintf("✗ %d failed", fail), red
	case pending > 0:
		return fmt.Sprintf("● %d of %d running", pending, total), amber
	default:
		return fmt.Sprintf("✓ %d/%d checks", pass, total), green
	}
}

// stripPR is the open PR's checks for the progress strip: the status bar's
// own read when it is the same PR, else a read of its own, kept fresh by
// refreshStripPR.
func (m *home) stripPR(root string, number int) (git.PullRequest, bool) {
	if e, ok := m.bar.prs[root]; ok && e.ok && e.pr.Number == number && (e.pr.State == "" || e.pr.State == "OPEN") {
		return e.pr, true
	}
	if e, ok := m.bar.stripPRs[root]; ok && e.ok && e.pr.Number == number {
		return e.pr, true
	}
	return git.PullRequest{}, false
}

type stripPRMsg struct {
	root       string
	pr         git.PullRequest
	ok, failed bool
	num        int
}

// refreshStripPR reads the checks of the PR the strip shows, at most every
// prRefreshEvery per project, so a red or green check shows at the top too.
func (m *home) refreshStripPR(root string) tea.Cmd {
	issues, ok := m.issueCache[root]
	if !ok {
		return nil
	}
	_, number := prRoom(issues)
	if number == 0 {
		return nil
	}
	if m.bar.stripPRs == nil {
		m.bar.stripPRs = map[string]prEntry{}
	}
	e := m.bar.stripPRs[root]
	if e.loading || (e.pr.Number == number && time.Since(e.fetched) < prRefreshEvery) {
		return nil
	}
	e.loading = true
	m.bar.stripPRs[root] = e
	return func() tea.Msg {
		pr, ok, failed := git.PRByNumber(root, number)
		return stripPRMsg{root: root, pr: pr, ok: ok, failed: failed, num: number}
	}
}

func (m *home) applyStripPR(msg stripPRMsg) {
	if old := m.bar.stripPRs[msg.root]; msg.failed && old.ok && old.pr.Number == msg.num {
		old.loading, old.fetched = false, time.Now()
		m.bar.stripPRs[msg.root] = old
		return
	}
	m.bar.stripPRs[msg.root] = prEntry{pr: msg.pr, ok: msg.ok, fetched: time.Now()}
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

// pullProject asks, then fast-forwards the active project's branch.
func (m *home) pullProject() tea.Cmd {
	root := m.projectTabs.Active()
	st := m.scCache[root]
	sy := m.bar.sync[root]
	if m.bar.pulling[root] || st.branch == "" {
		return nil
	}
	if sy.Upstream == "" {
		return m.handleError(fmt.Errorf("%s has no upstream to pull from: publish it first", st.branch))
	}
	if sy.Behind > 0 && sy.Ahead > 0 {
		return m.startMergeCheck(root, false) // a plain pull would be refused
	}
	if sy.Behind == 0 {
		// The count comes from the last fetch: a pull fetches first, so
		// "up to date" here still means one real check against GitHub.
		return m.confirmAction(fmt.Sprintf("Fetch and pull %s from %s?", st.branch, sy.Upstream), func() tea.Msg {
			return pullStartMsg{root: root}
		})
	}
	return m.confirmAction(fmt.Sprintf("Pull %d commit(s) into %s from %s?", sy.Behind, st.branch, sy.Upstream), func() tea.Msg {
		return pullStartMsg{root: root}
	})
}

type pullStartMsg struct{ root string }

type pullDoneMsg struct {
	root    string
	err     error
	in      *git.Incoming // set when the pull failed because both sides moved
	started time.Time
}

// fetchEvery is how often the open project's upstream is fetched, so ↓N says
// what is on GitHub now. Without it ↓ stayed at 0 until someone fetched by hand.
const fetchEvery = 5 * time.Minute

// fetchUpstream fetches the open project in the background when it is due; the
// next status read shows the new count. A failure (no network) stays quiet.
func (m *home) fetchUpstream() tea.Cmd {
	root := m.projectTabs.Active()
	if root == "" || m.bar.sync[root].Upstream == "" || m.bar.pushing[root] || m.bar.pulling[root] ||
		time.Since(m.bar.fetched[root]) < fetchEvery {
		return nil
	}
	if m.bar.fetched == nil {
		m.bar.fetched = map[string]time.Time{}
	}
	m.bar.fetched[root] = time.Now()
	return func() tea.Msg {
		if err := git.Fetch(root); err != nil {
			logEvent("background fetch of %s: %v", root, err)
		}
		return nil
	}
}

func (m *home) markPulling(root string) {
	if m.bar.pulling == nil {
		m.bar.pulling = map[string]bool{}
	}
	m.bar.pulling[root] = true
}

func (m *home) startPull(root string) tea.Cmd {
	m.markPulling(root)
	logEvent("pull started: %s", root)
	started := time.Now()
	return func() tea.Msg {
		err := git.Pull(root)
		return pullDoneMsg{root: root, err: err, in: divergedNow(root, err), started: started}
	}
}

func (m *home) pullDone(msg pullDoneMsg) tea.Cmd {
	delete(m.bar.pulling, msg.root)
	if msg.err != nil {
		logEvent("pull failed: %v", msg.err)
		if msg.in != nil {
			return tea.Batch(m.offerMerge(msg.root, *msg.in, false, msg.started), m.refreshSourceControl())
		}
		return tea.Batch(m.handleError(msg.err), m.refreshSourceControl())
	}
	logEvent("pulled: %s", msg.root)
	return tea.Batch(m.handleError(fmt.Errorf("pulled: branch is up to date")), m.refreshSourceControl())
}

// pushProject asks, then pushes the active project's branch.
func (m *home) pushProject() tea.Cmd {
	root := m.projectTabs.Active()
	st := m.scCache[root]
	sy := m.bar.sync[root]
	if st.branch == "" {
		return m.handleError(fmt.Errorf("this project isn't on a branch"))
	}
	if m.bar.pushing[root] || m.bar.pulling[root] {
		return nil
	}
	if sy.Ahead > 0 && sy.Behind > 0 {
		return m.startMergeCheck(root, true) // GitHub would refuse the push
	}
	if sy.Upstream != "" && sy.Ahead == 0 {
		return m.handleError(fmt.Errorf("nothing to push: %s is up to date with %s", st.branch, sy.Upstream))
	}
	question := fmt.Sprintf("Push %d commit(s) on %s to %s?", sy.Ahead, st.branch, sy.Upstream)
	if sy.Upstream == "" {
		question = fmt.Sprintf("Publish branch %s to origin?", st.branch)
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
	started := time.Now()
	return func() tea.Msg {
		err := git.Push(root)
		return pushDoneMsg{root: root, ahead: ahead, err: err, in: divergedNow(root, err), started: started}
	}
}

// divergedNow says whether a pull or push failed because both sides have new
// commits. The bar's ↓ count can't: it is as old as the last fetch.
func divergedNow(root string, err error) *git.Incoming {
	if err == nil {
		return nil
	}
	if in, err := git.CheckIncoming(root); err == nil && in.Diverged() {
		return &in
	}
	return nil
}

type mergeCheckMsg struct {
	root    string
	push    bool
	in      git.Incoming
	err     error
	started time.Time
}

type mergeStartMsg struct {
	root string
	push bool
}

type mergeDoneMsg struct {
	root string
	push bool
	err  error
}

func (m *home) startMergeCheck(root string, push bool) tea.Cmd {
	m.markPulling(root)
	started := time.Now()
	return func() tea.Msg {
		in, err := git.CheckIncoming(root)
		return mergeCheckMsg{root: root, push: push, in: in, err: err, started: started}
	}
}

func (m *home) mergeChecked(msg mergeCheckMsg) tea.Cmd {
	delete(m.bar.pulling, msg.root)
	if msg.err != nil {
		return tea.Batch(m.handleError(msg.err), m.refreshSourceControl())
	}
	if !msg.in.Diverged() { // someone else merged or pushed meanwhile
		return tea.Batch(m.handleError(fmt.Errorf("the branch changed meanwhile: press it again")), m.refreshSourceControl())
	}
	return tea.Batch(m.offerMerge(msg.root, msg.in, msg.push, msg.started), m.refreshSourceControl())
}

// offerMerge asks to merge the upstream's commits (and push). It comes up by
// itself after a refused push, so it never opens over another dialog, another
// project, or keys typed since: a "y" meant for a tile would confirm it.
func (m *home) offerMerge(root string, in git.Incoming, push bool, started time.Time) tea.Cmd {
	newer := fmt.Sprintf("%s has %d newer commit(s)", in.Upstream, in.Behind)
	if len(in.Authors) > 0 {
		newer += " by " + strings.Join(in.Authors, ", ")
	}
	if n := len(in.Conflicts); n > 0 {
		files := in.Conflicts[0]
		if n > 1 {
			files += fmt.Sprintf(" and %d more", n-1)
		}
		logEvent("merge needed in %s, conflicts: %s", root, strings.Join(in.Conflicts, ", "))
		return m.handleError(fmt.Errorf("%s that clash with yours in %s: ask a Claude here to merge %s", newer, files, in.Upstream))
	}
	if m.state != stateDefault || m.projectTabs.Active() != root || m.lastKey.After(started) {
		button := "↓"
		if push {
			button = "↑"
		}
		return m.handleError(fmt.Errorf("%s: click %s again to merge them", newer, button))
	}
	logEvent("merge offered in %s: %d behind, %d ahead", root, in.Behind, in.Ahead)
	question := newer + ". No conflicts. Merge them into yours"
	if push {
		question += fmt.Sprintf(" and push your %d", in.Ahead)
	}
	return m.confirmAction(question+"?", func() tea.Msg { return mergeStartMsg{root: root, push: push} })
}

func (m *home) startMerge(root string, push bool) tea.Cmd {
	m.markPulling(root)
	logEvent("merge started: %s", root)
	return func() tea.Msg { return mergeDoneMsg{root: root, push: push, err: git.MergeUpstream(root)} }
}

func (m *home) mergeDone(msg mergeDoneMsg) tea.Cmd {
	delete(m.bar.pulling, msg.root)
	if msg.err != nil {
		logEvent("%v", msg.err)
		return tea.Batch(m.handleError(msg.err), m.refreshSourceControl())
	}
	logEvent("merged: %s", msg.root)
	if msg.push {
		if m.bar.sync == nil {
			m.bar.sync = map[string]git.SyncState{}
		}
		m.bar.sync[msg.root] = git.Sync(msg.root) // the merge is one commit more to push
		return m.startPush(msg.root)
	}
	return tea.Batch(m.handleError(fmt.Errorf("merged: nothing left to pull")), m.refreshSourceControl())
}

func (m *home) pushDone(msg pushDoneMsg) tea.Cmd {
	delete(m.bar.pushing, msg.root)
	if msg.err != nil {
		logEvent("push failed: %v", msg.err)
		if msg.in != nil {
			return tea.Batch(m.offerMerge(msg.root, *msg.in, true, msg.started), m.refreshSourceControl())
		}
		return tea.Batch(m.handleError(msg.err), m.refreshSourceControl())
	}
	logEvent("pushed %d commits: %s", msg.ahead, msg.root)
	if e, ok := m.bar.prs[msg.root]; ok {
		e.fetched = time.Time{} // a first push may have opened the way for a PR
		m.bar.prs[msg.root] = e
	}
	return tea.Batch(m.handleError(fmt.Errorf("pushed %d commit(s)", msg.ahead)), m.refreshSourceControl())
}

// refreshPRNow re-reads the PR and its checks right away (the ↻ button).
func (m *home) refreshPRNow() tea.Cmd {
	root := m.projectTabs.Active()
	st := m.scCache[root]
	if e, ok := m.bar.prs[root]; ok {
		e.fetched, e.loading = time.Time{}, false
		m.bar.prs[root] = e
	}
	logEvent("PR refresh clicked: %s", root)
	return tea.Batch(m.refreshPR(root, st.branch), m.handleError(fmt.Errorf("refreshing the PR and its checks…")))
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
	case m.bar.pullSpan[1] > m.bar.pullSpan[0] && x >= m.bar.pullSpan[0] && x < m.bar.pullSpan[1]:
		return m.pullProject(), true
	case x >= m.bar.refreshSpan[0] && x < m.bar.refreshSpan[1]:
		return m.refreshPRNow(), true
	case x >= m.bar.prSpan[0] && x < m.bar.prSpan[1]:
		return m.openPR(), true
	}
	return nil, false
}

// tabChecksText is the short checks chip for a project tab: "✓ 5/5",
// "● 2 running", "✗ 1 failed"; "" without checks.
func tabChecksText(pr git.PullRequest) (string, lipgloss.Color) {
	pass, fail, pending := pr.CheckSummary()
	green, red, amber := lipgloss.Color("#3fb950"), lipgloss.Color("#f85149"), lipgloss.Color("#d29922")
	switch total := pass + fail + pending; {
	case total == 0:
		return "", green
	case fail > 0:
		return fmt.Sprintf("✗ %d failed", fail), red
	case pending > 0:
		return fmt.Sprintf("● %d running", pending), amber
	default:
		return fmt.Sprintf("✓ %d/%d", pass, total), green
	}
}

type tabChecksMsg struct {
	root       string
	pr         git.PullRequest
	ok, failed bool
}

// tabChecksEvery is how often each project's checks are read: one gh call
// per project, so slower than the active project's own 10s.
const tabChecksEvery = 30 * time.Second

// refreshTabChecks reads, for every project tab, the checks of the PR it
// works on: its batch PR when issues have gathered on one, else the PR of
// its checked-out branch. Each project at most every tabChecksEvery.
func (m *home) refreshTabChecks() tea.Cmd {
	if m.tabChecksAt == nil {
		m.tabChecksAt = map[string]time.Time{}
	}
	var cmds []tea.Cmd
	for _, root := range m.projectTabs.Projects() {
		if time.Since(m.tabChecksAt[root]) < tabChecksEvery {
			continue
		}
		m.tabChecksAt[root] = time.Now()
		_, number := prRoom(m.issueCache[root])
		cmds = append(cmds, func() tea.Msg {
			var pr git.PullRequest
			var ok, failed bool
			if number > 0 {
				pr, ok, failed = git.PRByNumber(root, number)
			} else {
				pr, ok, failed = git.CurrentPR(root)
			}
			return tabChecksMsg{root: root, pr: pr, ok: ok, failed: failed}
		})
	}
	return tea.Batch(cmds...)
}

func (m *home) applyTabChecks(msg tabChecksMsg) {
	if msg.failed {
		return // GitHub didn't answer: keep the chip as it was
	}
	var c ui.TabChecks
	if msg.ok && (msg.pr.State == "" || msg.pr.State == "OPEN") { // a merged PR's old checks say nothing
		if text, col := tabChecksText(msg.pr); text != "" {
			c = ui.TabChecks{Text: text, Color: string(col)}
		}
	}
	m.projectTabs.SetChecks(msg.root, c)
}
