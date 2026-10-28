package session

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
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
	// QAFailed: testing failed after its PR was merged (label qa:failed put
	// on, or the issue reopened, after that merge). It is free again: PR
	// stays 0 until a new PR takes it.
	QAFailed bool `json:"-"`
	// WaitsFor: a "[Follow-up] … after #N deploys" issue whose #N (issue or
	// PR) is still open. Its work can only happen once #N ships, so it is
	// not started until then (glitchgrab #627/#628 were run in #625's own
	// batch and closed with its PR, before anything was deployed).
	WaitsFor int `json:"-"`
	// WaitsRepo: the repo of WaitsFor when it lives in another repo
	// ("after glitchgrab#670 deploys"), else "". abhyasika #1028 waited on
	// glitchgrab#670 but was started at once and put into #1026's PR.
	WaitsRepo string `json:"-"`
	// NotBefore: a follow-up due on a later date ("Not before: 2026-10-23
	// 13:47 IST" in its body, or "[Follow-up] 2026-11-04: …" in its title).
	// Sessions don't live that long; cs starts it once the time comes.
	NotBefore time.Time `json:"-"`
	// Video: a video request (label gg:video), e.g. a demo script a tester
	// wrote. An editor makes it in GlitchRecord; it is never code to fix, so
	// cs never starts a session on it (glitchgrab #642 was triaged a feature).
	Video  bool `json:"-"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
}

// Waits reports whether the issue can't be worked on yet: a follow-up whose
// issue or PR isn't merged, or whose date hasn't come.
func (is Issue) Waits() bool {
	return is.Video || is.WaitsFor > 0 || time.Now().Before(is.NotBefore)
}

// WaitReason says what a waiting issue waits for, for the picker.
func (is Issue) WaitReason() string {
	if is.Video {
		return "video request, made in GlitchRecord"
	}
	if is.WaitsFor > 0 {
		return fmt.Sprintf("waits for %s#%d to merge", is.WaitsRepo, is.WaitsFor)
	}
	return "waits until " + is.NotBefore.Local().Format("2 Jan 15:04")
}

// issueListLimit caps the open issues cs reads. It was 100: practice-stack
// had 117 open, the 17 oldest were never seen, so PR #2435 showed 67 issues
// while GitHub linked 81 (and 3 free issues were missing from "open").
const issueListLimit = "1000"

// prListLimit is how many PRs per state are read to find taken issues. Merged
// needs to reach back as far as the oldest open issue (Aug-Sep on practice-stack):
// an old issue that a merged PR already fixed must not show as free.
var prListLimit = map[string]string{"merged": "300", "open": "100"}

// qaFailedLabel is what testers put on an issue whose fix failed testing.
const qaFailedLabel = "qa:failed"

// videoLabel marks a video request (Glitchgrab's report triage puts it on).
const videoLabel = "gg:video"

// isVideoRequest reports whether the issue carries videoLabel.
func isVideoRequest(is Issue) bool {
	for _, l := range is.Labels {
		if l.Name == videoLabel {
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
	openPRs := make(chan map[int]bool, 1)
	go func() { m, open := openPRIssues(dir); prs <- m; openPRs <- open }()
	cmd := exec.Command("gh", "issue", "list", "--state", "open", "--limit", issueListLimit,
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
	inPR, prOpen := <-prs, <-openPRs
	issueOpen := map[int]bool{}
	for _, is := range issues {
		issueOpen[is.Number] = true
	}
	// One gh call per issue with a merged PR. Run one after another they
	// made a refresh take 8–25s (r looked stuck on "refreshing…").
	failed := make([]bool, len(issues))
	var wg sync.WaitGroup
	slots := make(chan struct{}, 8)
	for i := range issues {
		if ref := inPR[issues[i].Number]; ref.merged {
			wg.Add(1)
			go func(i int, at time.Time) {
				defer wg.Done()
				slots <- struct{}{}
				defer func() { <-slots }()
				failed[i] = qaFailedAfter(dir, issues[i].Number, at)
			}(i, ref.mergedAt)
		}
	}
	wg.Wait()
	for i := range issues {
		// Some titles are pasted with line breaks; one line keeps the list readable.
		issues[i].Title = strings.Join(strings.Fields(issues[i].Title), " ")
		issues[i].NotBefore = dueDate(issues[i].Title, issues[i].Body)
		issues[i].Video = isVideoRequest(issues[i])
		for _, r := range followUpOf(issues[i].Title) {
			if r.repo == "" && (issueOpen[r.n] || prOpen[r.n]) || r.repo != "" && otherRepoOpen(dir, r.repo, r.n) {
				issues[i].WaitsFor, issues[i].WaitsRepo = r.n, r.repo
				break
			}
		}
		ref := inPR[issues[i].Number]
		// Failed testing after its PR merged: back in the queue, unless a
		// newer PR already took it again.
		if failed[i] {
			issues[i].QAFailed = true
			continue
		}
		issues[i].PR, issues[i].PRBot, issues[i].PRMerged = ref.number, ref.bot, ref.merged
	}
	sort.Slice(issues, func(i, j int) bool { return issues[i].CreatedAt.Before(issues[j].CreatedAt) })
	return issues, nil
}

// followUpRe matches a follow-up issue's title: "[Follow-up] <what> after
// #<issue> deploys" (the title the global rules give it). The issue may live
// in another repo: "after glitchgrab#670" or "after owner/repo#670".
var (
	followUpRe = regexp.MustCompile(`(?i)^\[follow-?up\].*?\bafter\s+(?:pr\s+)?((?:[\w.-]+(?:/[\w.-]+)?)?#\d+(?:\s*(?:,|and|&)\s*(?:[\w.-]+(?:/[\w.-]+)?)?#\d+)*)`)
	issueRefRe = regexp.MustCompile(`([\w.-]+(?:/[\w.-]+)?)?#(\d+)`)
)

// followRef is one issue or PR a follow-up waits for; repo is "" for this repo.
type followRef struct {
	repo string
	n    int
}

// followUpOf returns the issues or PRs a follow-up issue waits for, or nil
// when title isn't a follow-up. Only the refs right after "after" count:
// "(from #2651)" later in the title names the parent, not a wait.
func followUpOf(title string) []followRef {
	m := followUpRe.FindStringSubmatch(title)
	if m == nil {
		return nil
	}
	var out []followRef
	for _, r := range issueRefRe.FindAllStringSubmatch(m[1], -1) {
		if n, err := strconv.Atoi(r[2]); err == nil {
			repo := r[1]
			if strings.EqualFold(repo, "pr") { // "after PR#12" is this repo's PR
				repo = ""
			}
			out = append(out, followRef{repo, n})
		}
	}
	return out
}

// otherRepoOpen reports whether issue or PR n of repo is still open. A bare
// repo name ("glitchgrab") is taken under dir's own GitHub owner. When it
// can't be checked, it counts as open: starting too early is the worse miss.
func otherRepoOpen(dir, repo string, n int) bool {
	if !strings.Contains(repo, "/") {
		owner := githubOwner(dir)
		if owner == "" {
			return true
		}
		repo = owner + "/" + repo
	}
	cmd := exec.Command("gh", "api", fmt.Sprintf("repos/%s/issues/%d", repo, n), "--jq", ".state")
	cmd.Dir = dir
	out, err := cmd.Output()
	return err != nil || strings.TrimSpace(string(out)) != "closed"
}

var githubRemoteRe = regexp.MustCompile(`github\.com[:/]([^/]+)/`)

// githubOwner is the owner of dir's origin remote on GitHub, or "".
func githubOwner(dir string) string {
	out, err := exec.Command("git", "-C", dir, "remote", "get-url", "origin").Output()
	if err != nil {
		return ""
	}
	if m := githubRemoteRe.FindStringSubmatch(string(out)); m != nil {
		return m[1]
	}
	return ""
}

var (
	notBeforeRe = regexp.MustCompile(`(?im)^[\s*_>-]*not before[*_]*\s*:[*_]*\s*(.+?)[*_\s]*$`)
	titleDateRe = regexp.MustCompile(`\b(\d{4}-\d{2}-\d{2})\b`)
)

// dueDate is when a dated follow-up may start: the body's "Not before:" line
// (date, optional time, optional zone or IST), else a date in a follow-up's
// title (that day, 00:00 local). Zero when there's none.
func dueDate(title, body string) time.Time {
	if m := notBeforeRe.FindStringSubmatch(body); m != nil {
		if t, ok := parseDue(m[1]); ok {
			return t
		}
	}
	if strings.HasPrefix(strings.ToLower(title), "[follow") {
		if m := titleDateRe.FindStringSubmatch(title); m != nil {
			if t, ok := parseDue(m[1]); ok {
				return t
			}
		}
	}
	return time.Time{}
}

func parseDue(v string) (time.Time, bool) {
	v = strings.TrimSpace(v)
	loc := time.Local
	if u := strings.ToUpper(v); strings.HasSuffix(u, " IST") {
		if l, err := time.LoadLocation("Asia/Kolkata"); err == nil {
			loc = l
		}
		v = strings.TrimSpace(v[:len(v)-4])
	}
	for _, f := range []string{time.RFC3339, "2006-01-02 15:04 Z07:00", "2006-01-02 15:04 -0700", "2006-01-02T15:04", "2006-01-02 15:04", "2006-01-02"} {
		if t, err := time.ParseInLocation(f, v, loc); err == nil {
			return t, true
		}
	}
	return time.Time{}, false
}

// prRef is the open PR closing an issue and whether a bot opened it.
type prRef struct {
	number   int
	bot      bool
	merged   bool // the PR is merged: the issue is taken, the PR has no room
	mergedAt time.Time
}

// qaFailedAfter reports whether testing failed the issue after mergedAt
// (its latest merged fix): it got the qa:failed label, or was reopened, after
// that merge. A second failure often only reopens it: the label is still on
// from the first time (#2268: re-fix #2354 merged 6 Oct 05:41, reopened 07:56,
// no new label, and cs kept it as taken). Failed before the latest merge
// (a re-fix merged since) → stays taken until testing says again.
func qaFailedAfter(dir string, number int, mergedAt time.Time) bool {
	cmd := exec.Command("gh", "api", fmt.Sprintf("repos/{owner}/{repo}/issues/%d/events?per_page=100", number),
		"--jq", `[.[] | select(.event=="reopened" or (.event=="labeled" and .label.name=="`+qaFailedLabel+`")) | .created_at] | last // ""`)
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
// never gather issues on them (it once attached 14 to one). It also returns
// the numbers of the open PRs.
func openPRIssues(dir string) (map[int]prRef, map[int]bool) {
	// Merged and open lists load at the same time; merged ones go in first,
	// so an open PR closing the same issue wins below. An issue a merged PR
	// lists but that is still open (reopened, or GitHub didn't close it) was
	// worked on already: it is taken too (#944).
	var merged, open []prInfo
	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); merged = mergedPRs(dir) }()
	go func() { defer wg.Done(); open, _ = listPRs(dir, "open", prListLimit["open"]) }()
	wg.Wait()
	m, openNums := map[int]prRef{}, map[int]bool{}
	for _, pr := range open {
		openNums[pr.number] = true
	}
	for _, prs := range [][]prInfo{merged, open} {
		for _, pr := range prs {
			for _, is := range pr.closes {
				// The latest merge counts: a re-fix merged after a failed test.
				if old, ok := m[is]; ok && old.merged && pr.merged && old.mergedAt.After(pr.mergedAt) {
					continue
				}
				m[is] = prRef{number: pr.number, bot: pr.bot, merged: pr.merged, mergedAt: pr.mergedAt}
			}
		}
	}
	return m, openNums
}

// prInfo is one PR from gh pr list.
type prInfo struct {
	number   int
	bot      bool
	merged   bool
	mergedAt time.Time
	closes   []int
}

// mergedCache keeps each repo's merged PRs: listing 300 of them takes GitHub
// ~7s, which made every r in the issue picker look stuck. Merged PRs never
// change, so after the first full list only the newest mergedRecent are read
// and added, and the full list is read again after mergedFullEvery.
var (
	mergedMu    sync.Mutex
	mergedCache = map[string]mergedEntry{}
)

type mergedEntry struct {
	prs map[int]prInfo
	at  time.Time // last full list
}

const (
	mergedRecent    = "30"
	mergedFullEvery = 30 * time.Minute
)

func mergedPRs(dir string) []prInfo {
	mergedMu.Lock()
	e, ok := mergedCache[dir]
	mergedMu.Unlock()
	full := !ok || time.Since(e.at) > mergedFullEvery
	limit := mergedRecent
	if full {
		limit = prListLimit["merged"]
	}
	prs, err := listPRs(dir, "merged", limit)
	if err != nil {
		if ok {
			return e.list()
		}
		return nil
	}
	if full {
		e = mergedEntry{prs: map[int]prInfo{}, at: time.Now()}
	} else {
		cp := make(map[int]prInfo, len(e.prs)+len(prs))
		for k, v := range e.prs {
			cp[k] = v
		}
		e.prs = cp
	}
	for _, pr := range prs {
		e.prs[pr.number] = pr
	}
	mergedMu.Lock()
	mergedCache[dir] = e
	mergedMu.Unlock()
	return e.list()
}

func (e mergedEntry) list() []prInfo {
	out := make([]prInfo, 0, len(e.prs))
	for _, pr := range e.prs {
		out = append(out, pr)
	}
	return out
}

// listPRs reads up to limit PRs in state (open or merged).
func listPRs(dir, state, limit string) ([]prInfo, error) {
	cmd := exec.Command("gh", "pr", "list", "--state", state, "--limit", limit,
		"--json", "number,closingIssuesReferences,author,mergedAt")
	cmd.Dir = dir
	out, err := cmd.Output()
	if err != nil {
		return nil, err
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
	if err := json.Unmarshal(out, &prs); err != nil {
		return nil, err
	}
	var res []prInfo
	for _, pr := range prs {
		p := prInfo{number: pr.Number, merged: state == "merged", mergedAt: pr.MergedAt,
			bot: pr.Author.IsBot || strings.HasPrefix(pr.Author.Login, "app/") || strings.HasSuffix(pr.Author.Login, "[bot]")}
		for _, is := range pr.Closes {
			p.closes = append(p.closes, is.Number)
		}
		res = append(res, p)
	}
	return res, nil
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
	width, height := "200", "50"
	if exec.Command("tmux", "has-session", "-t", "="+name).Run() == nil {
		out, _ := exec.Command("tmux", "show-options", "-v", "-t", name, queuedOption).Output()
		if strings.TrimSpace(string(out)) != "1" {
			return name, nil // already running
		}
		// Replace the queued session with a new one. Not respawn-pane: on macOS
		// it failed ("fork failed: Device not configured") and left the pane
		// with no process, a blank tile forever; a second respawn on that pane
		// crashed the tmux server (tmux 3.7c), killing every session.
		// Keep the queued session's size: cs already fitted it to its tile
		// and fits a name only once, so a new 200x50 one stayed wider than
		// the tile and its lines ran off the right edge.
		if size, err := exec.Command("tmux", "display-message", "-p", "-t", "="+name+":", "#{window_width} #{window_height}").Output(); err == nil {
			if w, h, ok := strings.Cut(strings.TrimSpace(string(size)), " "); ok && w != "" && h != "" {
				width, height = w, h
			}
		}
		_ = exec.Command("tmux", "kill-session", "-t", "="+name).Run()
	}
	var out []byte
	var err error
	for try := 0; try < 3; try++ {
		if try > 0 {
			time.Sleep(500 * time.Millisecond)
		}
		out, err = exec.Command("tmux", "new-session", "-d", "-s", name, "-c", dir, "-x", width, "-y", height,
			"gai", "issue", issue.URL).CombinedOutput()
		if err == nil {
			break
		}
	}
	if err != nil {
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
