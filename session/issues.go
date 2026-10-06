package session

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Issue is an open GitHub issue of a project.
type Issue struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	URL       string    `json:"url"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"createdAt"`
	// PR is the open pull request that closes this issue (gai issue links
	// each issue it attaches), or 0.
	PR int `json:"-"`
	// PRBot: that PR was opened by a bot (the GitHub Claude app). Its issue
	// is taken, but it is never the PR this project's issues are gathered on.
	PRBot bool `json:"-"`
	// PRMerged: that PR is already merged; the issue still counts as taken.
	PRMerged bool `json:"-"`
	// QAFailed: testing failed after its PR was merged (label qa:failed, put
	// on after that merge). It is free again: PR stays 0 until a new PR
	// takes it.
	QAFailed bool `json:"-"`
	Labels   []struct {
		Name string `json:"name"`
	} `json:"labels"`
}

// qaFailedLabel is what testers put on an issue whose fix failed testing.
const qaFailedLabel = "qa:failed"

func (is Issue) hasLabel(name string) bool {
	for _, l := range is.Labels {
		if l.Name == name {
			return true
		}
	}
	return false
}

// ListOpenIssues returns the open issues of the GitHub repo in dir, oldest
// first (the order they should be worked on), each with the open PR that
// already closes it, if any.
func ListOpenIssues(dir string) ([]Issue, error) {
	prs := make(chan map[int]prRef, 1)
	go func() { prs <- openPRIssues(dir) }()
	cmd := exec.Command("gh", "issue", "list", "--state", "open", "--limit", "100",
		"--json", "number,title,url,body,createdAt,labels")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		if ee, ok := err.(*exec.ExitError); ok && len(ee.Stderr) > 0 {
			return nil, fmt.Errorf("gh issue list: %s", strings.TrimSpace(string(ee.Stderr)))
		}
		return nil, fmt.Errorf("gh issue list: %w", err)
	}
	var issues []Issue
	if err := json.Unmarshal(out, &issues); err != nil {
		return nil, fmt.Errorf("gh issue list: %w", err)
	}
	inPR := <-prs
	for i := range issues {
		// Some titles are pasted with line breaks; one line keeps the list readable.
		issues[i].Title = strings.Join(strings.Fields(issues[i].Title), " ")
		ref := inPR[issues[i].Number]
		// Failed testing after its PR merged: back in the queue, unless a
		// newer PR already took it again.
		if ref.merged && issues[i].hasLabel(qaFailedLabel) && qaFailedAfter(dir, issues[i].Number, ref.mergedAt) {
			issues[i].QAFailed = true
			continue
		}
		issues[i].PR, issues[i].PRBot, issues[i].PRMerged = ref.number, ref.bot, ref.merged
	}
	sort.Slice(issues, func(i, j int) bool { return issues[i].CreatedAt.Before(issues[j].CreatedAt) })
	return issues, nil
}

// prRef is the open PR closing an issue and whether a bot opened it.
type prRef struct {
	number   int
	bot      bool
	merged   bool // the PR is merged: the issue is taken, the PR has no room
	mergedAt time.Time
}

// qaFailedAfter reports whether the issue got the qa:failed label after
// mergedAt, i.e. the merged fix failed testing. An issue labelled before its
// latest merge (a re-fix merged since) stays taken until testing says again.
func qaFailedAfter(dir string, number int, mergedAt time.Time) bool {
	cmd := exec.Command("gh", "api", fmt.Sprintf("repos/{owner}/{repo}/issues/%d/events?per_page=100", number),
		"--jq", `[.[] | select(.event=="labeled" and .label.name=="`+qaFailedLabel+`") | .created_at] | last // ""`)
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return false
	}
	at, err := time.Parse(time.RFC3339, strings.TrimSpace(string(out)))
	return err == nil && at.After(mergedAt)
}

// openPRIssues maps each issue an open PR closes to that PR. On error it is
// empty: then no issue is treated as taken. PRs opened by bots (the GitHub
// Claude app's own fix PRs) are marked: their issues are taken, but cs must
// never gather issues on them (it once attached 14 to one).
func openPRIssues(dir string) map[int]prRef {
	m := map[int]prRef{}
	// Merged PRs first, so an open PR closing the same issue wins below. An
	// issue a merged PR lists but that is still open (reopened, or GitHub
	// didn't close it) was worked on already: it is taken too (#944).
	for _, state := range []string{"merged", "open"} {
		cmd := exec.Command("gh", "pr", "list", "--state", state, "--limit", "100",
			"--json", "number,closingIssuesReferences,author,mergedAt")
		cmd.Dir = dir
		out, err := cmd.Output()
		if err != nil {
			continue
		}
		var prs []struct {
			Number   int       `json:"number"`
			MergedAt time.Time `json:"mergedAt"`
			Closes   []struct {
				Number int `json:"number"`
			} `json:"closingIssuesReferences"`
			Author struct {
				IsBot bool   `json:"is_bot"`
				Login string `json:"login"`
			} `json:"author"`
		}
		if json.Unmarshal(out, &prs) != nil {
			continue
		}
		for _, pr := range prs {
			bot := pr.Author.IsBot || strings.HasPrefix(pr.Author.Login, "app/") || strings.HasSuffix(pr.Author.Login, "[bot]")
			for _, is := range pr.Closes {
				// The latest merge counts: a re-fix merged after a failed test.
				if old, ok := m[is.Number]; ok && old.merged && state == "merged" && old.mergedAt.After(pr.MergedAt) {
					continue
				}
				m[is.Number] = prRef{number: pr.Number, bot: bot, merged: state == "merged", mergedAt: pr.MergedAt}
			}
		}
	}
	return m
}

// OpenInBrowser opens a URL in the default browser.
func OpenInBrowser(url string) error {
	if !strings.HasPrefix(url, "https://") {
		return fmt.Errorf("not opening %q: not an https link", url)
	}
	opener := "xdg-open"
	if runtime.GOOS == "darwin" {
		opener = "open"
	}
	if out, err := exec.Command(opener, url).CombinedOutput(); err != nil {
		return fmt.Errorf("could not open %s: %s", url, strings.TrimSpace(string(out)))
	}
	return nil
}

// IssueSessionName is the tmux session for an issue's Claude session, so
// cs can tell which issues are already being worked on.
func IssueSessionName(dir string, number int) string {
	return ExternalPrefix + sessionNameRe.ReplaceAllString(filepath.Base(dir), "-") + "_i" + strconv.Itoa(number)
}

// IssueNumberOf returns the issue number of an issue session's tmux name, or 0.
func IssueNumberOf(name string) int {
	i := strings.LastIndex(name, "_i")
	if i < 0 || !strings.HasPrefix(name, ExternalPrefix) {
		return 0
	}
	n, _ := strconv.Atoi(name[i+2:])
	return n
}

// queuedOption marks an issue session that only shows "queued" so far.
const queuedOption = "@cs_queued"

// QueueIssueSession opens an issue's session at once, showing that it waits
// for its turn, so every picked issue has its tile right away. Its gai run
// starts later (StartIssueSession) in the same session.
func QueueIssueSession(dir string, issue Issue) (string, error) {
	name := IssueSessionName(dir, issue.Number)
	if exec.Command("tmux", "has-session", "-t", "="+name).Run() == nil {
		return name, nil // already there
	}
	msg := fmt.Sprintf("#%d %s\n\nQueued: gai attaches the picked issues to the PR one at a time,\n"+
		"so this starts after the ones before it. Claude starts here then.\n", issue.Number, issue.Title)
	if out, err := exec.Command("tmux", "new-session", "-d", "-s", name, "-c", dir, "-x", "200", "-y", "50",
		"sh", "-c", `printf "%b" "$1"; exec sleep 86400`, "sh", msg).CombinedOutput(); err != nil {
		return "", fmt.Errorf("could not open issue #%d: %s", issue.Number, strings.TrimSpace(string(out)))
	}
	_ = exec.Command("tmux", "set-option", "-t", name, "window-size", "latest").Run()
	_ = exec.Command("tmux", "set-option", "-t", name, queuedOption, "1").Run()
	return name, nil
}

// CloseStaleQueuedIssues ends "queued" issue sessions left by an earlier cs
// run: the queue lives in memory, so nothing would ever start them.
func CloseStaleQueuedIssues() {
	out, err := exec.Command("tmux", "list-sessions", "-F", "#{session_name} #{"+queuedOption+"}").Output()
	if err != nil {
		return
	}
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if name, flag, ok := strings.Cut(line, " "); ok && flag == "1" {
			_ = exec.Command("tmux", "kill-session", "-t", "="+name).Run()
		}
	}
}

// StartIssueSession runs `gai issue <url>` in the issue's tmux session in
// dir (replacing its "queued" screen, or in a new session). gai attaches the
// issue to the open PR (writing the PR text with the local AI) and then
// starts Claude with the issue thread, in the same session.
func StartIssueSession(dir string, issue Issue) (string, error) {
	name := IssueSessionName(dir, issue.Number)
	if exec.Command("tmux", "has-session", "-t", "="+name).Run() == nil {
		out, _ := exec.Command("tmux", "show-options", "-v", "-t", name, queuedOption).Output()
		if strings.TrimSpace(string(out)) != "1" {
			return name, nil // already running
		}
		_ = exec.Command("tmux", "set-option", "-u", "-t", name, queuedOption).Run()
		if out, err := exec.Command("tmux", "respawn-pane", "-k", "-t", name, "-c", dir,
			"gai", "issue", issue.URL).CombinedOutput(); err != nil {
			return "", fmt.Errorf("could not start issue #%d: %s", issue.Number, strings.TrimSpace(string(out)))
		}
		return name, nil
	}
	if out, err := exec.Command("tmux", "new-session", "-d", "-s", name, "-c", dir, "-x", "200", "-y", "50",
		"gai", "issue", issue.URL).CombinedOutput(); err != nil {
		return "", fmt.Errorf("could not start issue #%d: %s", issue.Number, strings.TrimSpace(string(out)))
	}
	_ = exec.Command("tmux", "set-option", "-t", name, "window-size", "latest").Run()
	return name, nil
}

// PressKeys types text and Enter into a tmux session.
func PressKeys(name, text string) error {
	return exec.Command("tmux", "send-keys", "-t", name, "-l", text+"\r").Run()
}

// PaneLastLine returns the last non-empty line on a tmux session's screen,
// with lines the pane wrapped joined back (-J): a narrow tile wraps gai's
// questions.
func PaneLastLine(name string) (string, bool) {
	out, err := exec.Command("tmux", "capture-pane", "-p", "-J", "-t", name).Output()
	if err != nil {
		return "", false
	}
	lines := strings.Split(strings.TrimRight(string(out), "\n "), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" {
			return l, true
		}
	}
	return "", true
}

// PressEnter sends Enter to a tmux session.
func PressEnter(name string) error {
	return exec.Command("tmux", "send-keys", "-t", name, "Enter").Run()
}

// PaneScreen returns a tmux session's visible screen as plain text, with
// wrapped lines joined.
func PaneScreen(name string) string {
	out, _ := exec.Command("tmux", "capture-pane", "-p", "-J", "-t", name).Output()
	return string(out)
}

// SendKeys sends tmux key names (Down, Enter…) to a session.
func SendKeys(name string, keys ...string) error {
	return exec.Command("tmux", append([]string{"send-keys", "-t", name}, keys...)...).Run()
}
