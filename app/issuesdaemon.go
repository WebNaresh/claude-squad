package app

import (
	"claude-squad/config"
	"claude-squad/session"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
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
	nextScan := time.Time{}

	for {
		select {
		case <-stop:
			logEvent("daemon: stopped")
			return nil
		case <-time.After(time.Second):
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
				if is.PR == 0 && !busy[is.Number] && !skipped[is.Number] {
					offer++
				}
			}
			if key := fmt.Sprint(running, room, offer); said[p] != key {
				said[p] = key
				logEvent("daemon: %s: %d running, %d free, PR #%d room %d, %d issue(s) to start", filepath.Base(p), running, free, pr, room, offer)
			}
			n := min(free, room)
			for _, is := range issues[p] {
				if n == 0 {
					break
				}
				if is.PR > 0 || busy[is.Number] || skipped[is.Number] {
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

// stepDaemonJob answers one gai job's questions; true when it is done
// (Claude runs, gai stopped, or it timed out).
func stepDaemonJob(j *daemonJob, issues []session.Issue) bool {
	line, alive := session.PaneLastLine(j.name)
	switch {
	case !alive:
		logEvent("daemon: gai issue #%d stopped before Claude started", j.issue.Number)
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
