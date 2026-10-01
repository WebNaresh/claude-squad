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
}

// ListOpenIssues returns the open issues of the GitHub repo in dir, oldest
// first (the order they should be worked on), each with the open PR that
// already closes it, if any.
func ListOpenIssues(dir string) ([]Issue, error) {
	prs := make(chan map[int]int, 1)
	go func() { prs <- openPRIssues(dir) }()
	cmd := exec.Command("gh", "issue", "list", "--state", "open", "--limit", "100",
		"--json", "number,title,url,body,createdAt")
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
		issues[i].PR = inPR[issues[i].Number]
	}
	sort.Slice(issues, func(i, j int) bool { return issues[i].CreatedAt.Before(issues[j].CreatedAt) })
	return issues, nil
}

// openPRIssues maps each issue an open PR closes to that PR. On error it is
// empty: then no issue is treated as taken.
func openPRIssues(dir string) map[int]int {
	cmd := exec.Command("gh", "pr", "list", "--state", "open", "--limit", "100",
		"--json", "number,closingIssuesReferences")
	cmd.Dir = dir
	out, err := cmd.Output()
	m := map[int]int{}
	if err != nil {
		return m
	}
	var prs []struct {
		Number int `json:"number"`
		Closes []struct {
			Number int `json:"number"`
		} `json:"closingIssuesReferences"`
	}
	if json.Unmarshal(out, &prs) != nil {
		return m
	}
	for _, pr := range prs {
		for _, is := range pr.Closes {
			m[is.Number] = pr.Number
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

// StartIssueSession runs `gai issue <url>` in a new tmux session in dir. gai
// attaches the issue to the open PR (writing the PR text with the local AI)
// and then starts Claude with the issue thread, in the same session.
func StartIssueSession(dir string, issue Issue) (string, error) {
	name := IssueSessionName(dir, issue.Number)
	if exec.Command("tmux", "has-session", "-t", "="+name).Run() == nil {
		return name, nil // already running
	}
	if out, err := exec.Command("tmux", "new-session", "-d", "-s", name, "-c", dir, "-x", "200", "-y", "50",
		"gai", "issue", issue.URL).CombinedOutput(); err != nil {
		return "", fmt.Errorf("could not start issue #%d: %s", issue.Number, strings.TrimSpace(string(out)))
	}
	_ = exec.Command("tmux", "set-option", "-t", name, "window-size", "latest").Run()
	return name, nil
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
