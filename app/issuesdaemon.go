package app

import (
	"claude-squad/config"
	"claude-squad/session"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Background issue runner: `cs --issues-daemon`, started at login by a
// LaunchAgent (InstallIssuesDaemon) and kept alive by launchd, so issues get
// worked on whenever the Mac is on, with or without a cs window.
//
// Every daemonEvery, for each open project with auto on (not switched off
// with a in the issue picker): count its Claude sessions; while fewer than
// autoMaxSessions run and its open PR has fewer than autoMaxPerPR issues,
// open sessions for the oldest open issues that aren't skipped, in a PR, or
// already running. Each runs `gai issue` (one at a time across all projects,
// since gai rewrites the shared PR), and the runner answers gai's questions:
// Enter for yes/no, n to the PR rewrite while more of the project's issues
// wait, and the project's PR in gai's "Multiple open PRs" list. When the PR is
// merged or closed its issues stop counting and work starts again; gai opens
// the next PR. It logs to activity.log ("daemon: …") and exits to restart
// when a newer build is installed.

const daemonEvery = 20 * time.Second

type daemonJob struct {
	project  string
	issue    session.Issue
	name     string
	started  time.Time
	answered string
	more     bool // more of this project's issues wait after it
}

// RunIssuesDaemon runs the background loop until killed.
func RunIssuesDaemon() error {
	// launchd starts us with no locale, and tmux then prints the tab in
	// list-panes formats as "_": the session list came back empty, so the
	// runner never saw its own tiles (Claude never "started", counts were
	// off). Every tmux call below inherits this.
	if os.Getenv("LANG") == "" && os.Getenv("LC_ALL") == "" {
		_ = os.Setenv("LANG", "en_US.UTF-8")
	}
	release, err := config.AcquireNamedLock(config.IssuesDaemonLock, "the background issue runner is already running (process %d)")
	if err != nil {
		return err
	}
	defer release()
	session.CloseStaleQueuedIssues()
	logEvent("daemon: started pid=%d", os.Getpid())
	defer flushLogs()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGTERM, syscall.SIGINT)
	exe, _ := os.Executable()
	built := modTime(exe)

	var queue []daemonJob
	var job *daemonJob
	issuesAt := map[string]time.Time{}
	issues := map[string][]session.Issue{}
	said := map[string]string{}
	ready := map[string]bool{}      // projects already on a fresh base for their new batch
	saidBase := map[string]string{} // last "not starting a new batch yet" reason logged
	limitDone := map[string]bool{}  // sessions already resumed after a usage limit
	colored := loadColors()         // the /color cs gave each issue session ("" = the user's own)
	comments := loadCommentState()  // GitHub comments already typed into issue tiles (issuecomments.go)
	nextLimitCheck := time.Time{}
	nextScan := time.Time{}

	for {
		select {
		case <-stop:
			logEvent("daemon: stopped")
			return nil
		case <-time.After(time.Second):
		}

		// Usage-limit resumes on their own clock: the issue scan below waits
		// while a gai job runs (up to 5 min), which left tiles stopped long
		// after the reset.
		if time.Now().After(nextLimitCheck) {
			nextLimitCheck = time.Now().Add(15 * time.Second)
			if list, _, err := session.ListExternalSessions(); err == nil {
				if at := resumeAfterLimit(list, limitDone, time.Now()); !at.IsZero() && at.Before(nextLimitCheck) {
					nextLimitCheck = at // a reset is due before the next scan
				}
				colorIssueSessions(list, colored)
				deliverIssueComments(list, comments, time.Now())
			}
		}

		// The current gai job: answer its questions, finish when Claude runs.
		if job != nil {
			if done := stepDaemonJob(job, issues[job.project]); done {
				job = nil
			}
			continue
		}
		if len(queue) > 0 {
			next := queue[0]
			queue = queue[1:]
			name, err := session.StartIssueSession(next.project, next.issue)
			if err != nil {
				logEvent("daemon: could not start #%d in %s: %v", next.issue.Number, filepath.Base(next.project), err)
				continue
			}
			next.name, next.started = name, time.Now()
			next.more = false
			for _, q := range queue {
				if q.project == next.project {
					next.more = true
				}
			}
			logEvent("daemon: starting #%d in %s", next.issue.Number, filepath.Base(next.project))
			// Its colour now, so the new tile is drawn in it from the start.
			if list, _, err := session.ListExternalSessions(); err == nil {
				colorIssueSessions(list, colored)
			}
			job = &next
			continue
		}

		// Nothing running: a newer build replaces this process (launchd
		// starts it again through the launcher, which builds it).
		if built != (time.Time{}) && modTime(exe).After(built) {
			logEvent("daemon: newer build installed, restarting")
			return nil
		}
		if time.Now().Before(nextScan) {
			continue
		}
		nextScan = time.Now().Add(daemonEvery)

		cfg := config.LoadConfig()
		sessions, _, err := session.ListExternalSessions()
		if err != nil {
			continue
		}
		for _, p := range cfg.OpenProjects {
			if !autoEnabled(p) {
				continue
			}
			running := 0
			busy := map[int]bool{}
			for _, e := range sessions {
				if e.Path != p && !strings.HasPrefix(e.Path, p+string(filepath.Separator)) {
					continue
				}
				if n := session.IssueNumberOf(e.Name); n > 0 {
					busy[n] = true
				}
				if e.Status != "" || session.IssueNumberOf(e.Name) > 0 || strings.HasPrefix(e.Name, session.ExternalPrefix) {
					running++
				}
			}
			free := autoMaxSessions - running
			if free <= 0 {
				if key := fmt.Sprint("full", running); said[p] != key {
					said[p] = key
					logEvent("daemon: %s: %d sessions running (limit %d); waiting for one to close", filepath.Base(p), running, autoMaxSessions)
				}
				continue
			}
			if time.Since(issuesAt[p]) > autoListMaxAge {
				list, err := session.ListOpenIssues(p)
				if err != nil {
					logEvent("daemon: %s issues: %v", filepath.Base(p), err)
					continue
				}
				issues[p], issuesAt[p] = list, time.Now()
				writeIssueCache(p, list) // the window's strip reads it
			}
			room, pr := prRoom(issues[p])
			// A PR already open from the checked-out branch is this batch's
			// PR even when it lists no issues yet (gai's body can come out
			// without "Closes #…", like glitchgrab #577): keep going on it,
			// with no branch switch. Without this, the batch waited for the
			// cleanup, which waits for every session to close.
			if pr == 0 {
				if n := openPRHere(p); n > 0 {
					pr, room = n, autoMaxPerPR
				}
			}
			room -= joining(issues[p], busy)
			if room <= 0 {
				if key := fmt.Sprint(pr, "full"); said[p] != key {
					said[p] = key
					logEvent("daemon: %s PR #%d has %d issues (the limit); waiting for it to be merged", filepath.Base(p), pr, autoMaxPerPR)
				}
				continue
			}
			skipped := readSkipped(p)
			offer := 0
			for _, is := range issues[p] {
				if is.PR == 0 && !is.Waits() && !busy[is.Number] && !skipped[is.Number] && !gaiFailedRecently(p, is.Number) {
					offer++
				}
			}
			if key := fmt.Sprint(running, room, offer); said[p] != key {
				said[p] = key
				logEvent("daemon: %s: %d running, %d free, PR #%d room %d, %d issue(s) to start", filepath.Base(p), running, free, pr, room, offer)
			}
			if offer == 0 {
				continue
			}
			// A new batch (no open PR of the project's own): start it from an
			// up-to-date main, not the merged branch (batchbase.go). Only when
			// an issue is about to start; once its PR exists, the batch
			// continues on that branch.
			if pr == 0 && !ready[p] {
				// Switching branches under a working session moved its
				// commits onto main (practice-stack, 6 Oct 11:12: "1 running",
				// tree clean for that moment). The switch waits until no
				// Claude session is open in the project at all.
				// Already on the default branch: nothing to switch, so an open
				// session (glitchgrab #542, kept open for days) mustn't hold
				// the batch back. Start from main as it is; the pull waits
				// for a moment with no session open (it would change their files).
				onMain, why := running > 0 && onDefaultBranch(p), ""
				if onMain {
					why = staleDev(p)
				}
				if why != "" {
					if saidBase[p] != why {
						saidBase[p] = why
						logEvent("daemon: %s: not starting a new batch yet: %s", filepath.Base(p), why)
					}
					continue
				} else if onMain {
					ready[p] = true
					delete(saidBase, p)
					logEvent("daemon: %s: new batch starts on the current main (%d session(s) open, so no pull now)", filepath.Base(p), running)
				} else if running > 0 {
					why := fmt.Sprintf("%d session(s) still open; the switch to an up-to-date main waits until they're closed", running)
					if saidBase[p] != why {
						saidBase[p] = why
						logEvent("daemon: %s: not starting a new batch yet: %s", filepath.Base(p), why)
					}
					continue
				} else if ok, why := freshBase(p); !ok {
					if saidBase[p] != why {
						saidBase[p] = why
						logEvent("daemon: %s: not starting a new batch yet: %s", filepath.Base(p), why)
					}
					continue
				}
				ready[p] = true
				delete(saidBase, p)
			}
			if pr > 0 {
				delete(ready, p) // the next time no PR is open, clean up again
			}
			n := min(free, room)
			for _, is := range issues[p] {
				if n == 0 {
					break
				}
				if is.PR > 0 || is.Waits() || busy[is.Number] || skipped[is.Number] || gaiFailedRecently(p, is.Number) {
					continue
				}
				if _, err := session.QueueIssueSession(p, is); err != nil {
					logEvent("daemon: could not open #%d's tile: %v", is.Number, err)
					continue
				}
				queue = append(queue, daemonJob{project: p, issue: is})
				busy[is.Number] = true
				n--
			}
			if len(queue) > 0 {
				logEvent("daemon: %s: %d running, %d free, PR room %d: queued %d issue(s)", filepath.Base(p), running, free, room, len(queue))
				break // one project per scan; the rest next time
			}
		}
	}
}

// gaiFailed: when gai last stopped before Claude started, per project#issue.
// Such an issue isn't started again for gaiRetryAfter, so a gai that fails
// (a PR it can't create) doesn't run in a loop once its tile is closed.
var gaiFailed = map[string]time.Time{}

const gaiRetryAfter = 30 * time.Minute

func gaiFailedRecently(project string, n int) bool {
	at, ok := gaiFailed[fmt.Sprint(project, "#", n)]
	return ok && time.Since(at) < gaiRetryAfter
}

// gaiLastWords returns the last few lines gai printed, for the log.
func gaiLastWords(screen string) string {
	var keep []string
	lines := strings.Split(screen, "\n")
	for i := len(lines) - 1; i >= 0 && len(keep) < 4; i-- {
		if l := strings.TrimSpace(lines[i]); l != "" && !strings.HasPrefix(l, "Pane is dead") {
			keep = append([]string{l}, keep...)
		}
	}
	return strings.Join(keep, " | ")
}

// stepDaemonJob answers one gai job's questions; true when it is done
// (Claude runs, gai stopped, or it timed out).
func stepDaemonJob(j *daemonJob, issues []session.Issue) bool {
	line, alive := session.PaneLastLine(j.name)
	switch {
	case !alive || session.PaneDead(j.name):
		logEvent("daemon: gai issue #%d stopped before Claude started; its tile keeps what gai said: %s",
			j.issue.Number, gaiLastWords(session.PaneScreen(j.name)))
		gaiFailed[fmt.Sprint(j.project, "#", j.issue.Number)] = time.Now()
		return true
	case time.Since(j.started) > issueJobTimeout:
		logEvent("daemon: #%d taking long; leaving it in its tile", j.issue.Number)
		return true
	case strings.HasPrefix(line, "#") || strings.HasPrefix(line, "▶"):
		if j.answered != "pr" {
			j.answered = "pr"
			screen := session.PaneScreen(j.name)
			if !strings.Contains(screen, "Multiple open PRs") {
				return false
			}
			target := 0
			if room, pr := prRoom(issues); pr > 0 && room > 0 {
				target = pr
			}
			if keys, ok := prKeys(screen, target); ok {
				_ = session.SendKeys(j.name, keys...)
				logEvent("daemon: #%d picked PR #%d", j.issue.Number, target)
			} else {
				logEvent("daemon: #%d gai asks which PR; none of this project's in its list, left in its tile", j.issue.Number)
			}
		}
	case isGaiPrompt(line) && line != j.answered:
		j.answered = line
		if strings.Contains(line, "Rewrite the PR title and body") && j.more {
			_ = session.PressKeys(j.name, "n")
		} else {
			_ = session.PressEnter(j.name)
		}
		logEvent("daemon: #%d answered %q", j.issue.Number, line)
	}
	// Claude started in the tile: gai is done with the PR.
	if sessions, _, err := session.ListExternalSessions(); err == nil {
		for _, e := range sessions {
			if e.Name == j.name && e.Status != "" {
				logEvent("daemon: #%d: Claude is working", j.issue.Number)
				session.KeepOnExit(j.name, false)
				return true
			}
		}
	}
	return false
}

// autoEnabled reports whether the auto issue loop runs for project.
func autoEnabled(project string) bool {
	f := autoOffFile(project)
	if f == "" {
		return false
	}
	_, err := os.Stat(f)
	return err != nil
}

func modTime(path string) time.Time {
	if st, err := os.Stat(path); err == nil {
		return st.ModTime()
	}
	return time.Time{}
}

// openPRHere is the open PR from the project's checked-out branch, or 0.
func openPRHere(project string) int {
	out, err := exec.Command("git", "-C", project, "branch", "--show-current").Output()
	cur := strings.TrimSpace(string(out))
	if err != nil || cur == "" {
		return 0
	}
	n, err := openPRFrom(project, cur)
	if err != nil {
		return 0
	}
	v, _ := strconv.Atoi(n)
	return v
}

// onDefaultBranch reports whether project has its default branch (origin's
// HEAD, else main) checked out.
func onDefaultBranch(project string) bool {
	def := "main"
	if out, err := exec.Command("git", "-C", project, "symbolic-ref", "--short", "refs/remotes/origin/HEAD").Output(); err == nil {
		if d, ok := strings.CutPrefix(strings.TrimSpace(string(out)), "origin/"); ok {
			def = d
		}
	}
	out, err := exec.Command("git", "-C", project, "branch", "--show-current").Output()
	return err == nil && strings.TrimSpace(string(out)) == def
}

// staleDev says why an existing local "dev" would change files if gai
// switched to it (it has work that isn't in the checked-out main), or "".
// A dev that is only an older copy of main is deleted instead. gai cuts the
// batch's branch as dev and reuses one that exists; with sessions open that
// switch must not touch their files.
func staleDev(project string) string {
	if exec.Command("git", "-C", project, "rev-parse", "--verify", "-q", "refs/heads/dev").Run() != nil {
		return "" // no dev: gai makes it from main, nothing changes
	}
	if exec.Command("git", "-C", project, "diff", "--quiet", "HEAD", "dev").Run() == nil {
		return "" // same files as main
	}
	// Only an older copy of main (its files equal one of main's last 50
	// commits, e.g. before the release commits): nothing of its own, so it is
	// deleted (the user agreed, 9 Oct) and gai makes a fresh dev from main.
	out, _ := exec.Command("git", "-C", project, "rev-list", "--first-parent", "-50", "HEAD").Output()
	for _, c := range strings.Fields(string(out)) {
		if exec.Command("git", "-C", project, "diff", "--quiet", c, "dev").Run() == nil {
			if exec.Command("git", "-C", project, "branch", "-D", "dev").Run() == nil {
				logEvent("daemon: %s: deleted the old local dev (same files as main at %.8s, nothing of its own)", filepath.Base(project), c)
				return ""
			}
		}
	}
	return "an old local dev branch has work that isn't in main; gai would switch to it under the open sessions. Merge or delete it, or close the sessions"
}
