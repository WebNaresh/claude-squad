package app

import (
	"claude-squad/session"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Unlink: ⌃Space then U on an issue's tile takes that issue off its PR
// (`gai issue <n> --remove`: drops its "Closes #N" and rewrites the PR text
// from what is left) and marks it skipped, so the loop doesn't offer it
// again. Before this the user did both by hand: asked the Claude in the tile
// to edit the PR body, then pressed x on it in the picker (practice-stack
// #2510 in PR #2519).
//
// gai needs a terminal for its questions, so it runs in a short-lived tmux
// session that cs answers the same way as the issue queue does: Enter on the
// [Y/n] rewrite question, the cursor onto the issue's PR in the PR list.

const unlinkTimeout = 3 * time.Minute

type unlinkPollMsg struct {
	name, project string
	issue, pr     int
	started       time.Time
	answered      string
	last          string // last screen seen, for the result once gai has exited
}

type unlinkDoneMsg struct {
	project string
	issue   int
	text    string
}

// unlinkIssue asks, then removes the selected issue tile's issue from its PR
// and skips it.
func (m *home) unlinkIssue() tea.Cmd {
	e := m.list.GetSelectedExternal()
	if e == nil {
		return m.handleError(fmt.Errorf("select an issue's tile first"))
	}
	n := session.IssueNumberOf(e.Name)
	if n == 0 {
		return m.handleError(fmt.Errorf("%s is not an issue session: select an issue's tile", e.Title()))
	}
	project := m.projectOf(e.Path)
	if project == "" {
		return m.handleError(fmt.Errorf("could not tell which project #%d belongs to", n))
	}
	pr := 0
	for _, is := range m.issueCache[project] {
		if is.Number == n {
			pr = is.PR
		}
	}
	what := fmt.Sprintf("Remove #%d from its PR", n)
	if pr > 0 {
		what = fmt.Sprintf("Remove #%d from PR #%d", n, pr)
	}
	return m.confirmAction(what+" and skip it? The PR title and body are rewritten from the issues that remain.",
		func() tea.Msg { return startUnlink(project, n, pr) })
}

// startUnlink marks the issue skipped at once and starts gai in tmux.
func startUnlink(project string, n, pr int) tea.Msg {
	skipped := readSkipped(project)
	skipped[n] = true
	var nums []int
	for k := range skipped {
		nums = append(nums, k)
	}
	writeSkipped(project, nums)

	name := fmt.Sprintf("csunlink_%s_%d", filepath.Base(project), n)
	if exec.Command("tmux", "has-session", "-t", "="+name).Run() == nil {
		return fmt.Errorf("#%d is already being taken off its PR; wait for the result", n)
	}
	if out, err := exec.Command("tmux", "new-session", "-d", "-s", name, "-c", project, "-x", "200", "-y", "50",
		"gai", "issue", fmt.Sprint(n), "--remove").CombinedOutput(); err != nil {
		return fmt.Errorf("#%d skipped, but gai could not start: %s", n, strings.TrimSpace(string(out)))
	}
	logEvent("unlink: #%d off PR #%d in %s, skipped", n, pr, filepath.Base(project))
	return unlinkPollMsg{name: name, project: project, issue: n, pr: pr, started: time.Now()}
}

// pollUnlink looks at gai's screen once a second and answers its questions.
func pollUnlink(p unlinkPollMsg) tea.Cmd {
	return func() tea.Msg {
		time.Sleep(time.Second)
		screen := session.PaneScreen(p.name)
		if strings.TrimSpace(screen) != "" {
			p.last = screen
		}
		if exec.Command("tmux", "has-session", "-t", "="+p.name).Run() != nil {
			return unlinkDoneMsg{project: p.project, issue: p.issue, text: unlinkResult(p)}
		}
		if time.Since(p.started) > unlinkTimeout {
			_ = exec.Command("tmux", "kill-session", "-t", "="+p.name).Run()
			p.last = screen
			return unlinkDoneMsg{project: p.project, issue: p.issue,
				text: fmt.Sprintf("#%d skipped; gai took too long removing it from the PR (see `gai --logs`)", p.issue)}
		}
		line, _ := session.PaneLastLine(p.name)
		switch {
		case strings.Contains(screen, "Multiple open PRs") && p.answered != "pr":
			keys, ok := prKeys(screen, p.pr)
			if !ok {
				// The issue's PR isn't in gai's list (or isn't known): don't sit
				// on the menu for 3 minutes, say so and stop.
				_ = exec.Command("tmux", "kill-session", "-t", "="+p.name).Run()
				return unlinkDoneMsg{project: p.project, issue: p.issue,
					text: fmt.Sprintf("#%d skipped; gai asks which PR and none is #%d's: run `gai issue %d --remove` in the project terminal",
						p.issue, p.issue, p.issue)}
			}
			p.answered = "pr"
			_ = session.SendKeys(p.name, keys...)
		case isGaiPrompt(line) && line != p.answered:
			p.answered = line
			_ = session.PressEnter(p.name)
		}
		return p
	}
}

// unlinkResult says what happened, from gai's last screen.
func unlinkResult(p unlinkPollMsg) string {
	switch {
	case strings.Contains(p.last, "not referenced in PR"):
		return fmt.Sprintf("#%d skipped; it was not on an open PR", p.issue)
	case strings.Contains(p.last, "error:"):
		for _, l := range strings.Split(p.last, "\n") {
			if strings.Contains(l, "error:") {
				return fmt.Sprintf("#%d skipped; gai: %s", p.issue, strings.TrimSpace(l))
			}
		}
	}
	return fmt.Sprintf("#%d removed from its PR and skipped", p.issue)
}
