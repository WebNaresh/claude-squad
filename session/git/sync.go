package git

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"
)

// SyncState is how the branch stands against its upstream.
type SyncState struct {
	Upstream      string // e.g. "origin/dev"; "" when the branch has none
	Ahead, Behind int    // commits to push / to pull
}

// Sync returns the branch's ahead/behind counts against its upstream. It
// reads only local refs (no fetch), so it is cheap.
func Sync(root string) SyncState {
	up, err := exec.Command("git", "-C", root, "rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{upstream}").Output()
	if err != nil {
		return SyncState{}
	}
	s := SyncState{Upstream: strings.TrimSpace(string(up))}
	out, err := exec.Command("git", "-C", root, "rev-list", "--left-right", "--count", "HEAD...@{upstream}").Output()
	if err != nil {
		return s
	}
	if f := strings.Fields(string(out)); len(f) == 2 {
		s.Ahead, _ = strconv.Atoi(f[0])
		s.Behind, _ = strconv.Atoi(f[1])
	}
	return s
}

// Push pushes the current branch, setting its upstream on origin the first
// time.
func Push(root string) error {
	args := []string{"-C", root, "push"}
	if Sync(root).Upstream == "" {
		branch := Branch(root)
		if branch == "" {
			return fmt.Errorf("not on a branch")
		}
		args = append(args, "-u", "origin", branch)
	}
	out, err := exec.Command("git", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("push failed: %s", gitReason(string(out)))
	}
	return nil
}

// Pull fast-forwards the current branch from its upstream (fetches first),
// keeping uncommitted edits. A diverged branch is refused: it needs a merge,
// which the caller asks about.
func Pull(root string) error {
	in, err := CheckIncoming(root)
	switch {
	case err != nil:
		return err
	case in.Diverged():
		return fmt.Errorf("pull failed: %s and this branch both have new commits", in.Upstream)
	case in.Behind == 0:
		return nil
	case len(in.Conflicts) > 0:
		return fmt.Errorf("pull not started: your uncommitted changes clash in %s", in.Conflicts[0])
	}
	return MergeUpstream(root) // a fast-forward here
}

// gitReason picks the line of git's output that says what went wrong. The last
// line is often a hint ("See the 'Note about fast-forwards'…") that doesn't.
func gitReason(out string) string {
	var lines []string
	for _, l := range strings.Split(out, "\n") {
		l = strings.TrimSpace(l)
		if l != "" && !strings.HasPrefix(l, "hint:") && !strings.HasPrefix(l, "error: failed to push some refs") {
			lines = append(lines, l)
		}
	}
	for _, prefix := range []string{"remote: error", "fatal:", "error:", "! ["} {
		for i, l := range lines {
			if strings.HasPrefix(l, prefix) {
				if strings.HasSuffix(l, ":") && i+1 < len(lines) {
					l += " " + lines[i+1] // "…would be overwritten by merge:" and the file
				}
				return l
			}
		}
	}
	if len(lines) == 0 {
		return "git gave no reason"
	}
	return lines[len(lines)-1]
}

// fetchTimeout: a fetch on a dead network must not hang the bar's button.
const fetchTimeout = time.Minute

// Fetch updates the upstream's refs (nothing in the working tree). It never
// asks for a password: there is nobody to type it.
func Fetch(root string) error {
	ctx, cancel := context.WithTimeout(context.Background(), fetchTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", "-C", root, "fetch", "--quiet")
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0", "GIT_SSH_COMMAND=ssh -o BatchMode=yes")
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("fetch failed: %s", gitReason(string(out)))
	}
	return nil
}

// Incoming is what the upstream has that the branch doesn't, after a fetch.
type Incoming struct {
	SyncState
	Authors   []string // who wrote those commits
	Conflicts []string // files both sides changed in a way git can't merge
}

// Diverged: both sides have commits the other lacks, so neither a push nor a
// fast-forward pull can work; they have to be merged.
func (in Incoming) Diverged() bool { return in.Ahead > 0 && in.Behind > 0 }

// CheckIncoming fetches and says what merging the upstream would bring. It
// changes no file and no branch.
func CheckIncoming(root string) (Incoming, error) {
	if err := Fetch(root); err != nil {
		return Incoming{}, err
	}
	in := Incoming{SyncState: Sync(root)}
	if in.Behind == 0 {
		return in, nil
	}
	if out, err := exec.Command("git", "-C", root, "log", "--format=%an", "HEAD..@{upstream}").Output(); err == nil {
		for _, a := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if a != "" && !slices.Contains(in.Authors, a) {
				in.Authors = append(in.Authors, a)
			}
		}
	}
	// Exit 1: conflicts, listed after the tree's id. 2 or more: it couldn't
	// tell (an old git); the merge itself then decides, and is undone if it fails.
	// Uncommitted edits count as yours too: they must survive the merge.
	mine := "HEAD"
	if w, err := exec.Command("git", "-C", root, "stash", "create").Output(); err == nil && len(bytes.TrimSpace(w)) > 0 {
		mine = strings.TrimSpace(string(w)) // a loose commit of them; changes nothing
	}
	out, err := exec.Command("git", "-C", root, "merge-tree", "--write-tree", "--name-only", "--no-messages", "--merge-base", mergeBase(root), mine, "@{upstream}").Output()
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == 1 {
		if lines := strings.Split(strings.TrimSpace(string(out)), "\n"); len(lines) > 1 {
			in.Conflicts = lines[1:]
		}
	}
	return in, nil
}

func mergeBase(root string) string {
	out, err := exec.Command("git", "-C", root, "merge-base", "HEAD", "@{upstream}").Output()
	if err != nil {
		return "HEAD"
	}
	return strings.TrimSpace(string(out))
}

// nameList is the file names a git command prints, one per line.
func nameList(root string, args ...string) []string {
	out, _ := exec.Command("git", append([]string{"-C", root, "-c", "core.quotePath=false"}, args...)...).Output()
	return strings.FieldsFunc(string(out), func(r rune) bool { return r == '\n' })
}

// MergeUpstream merges the fetched upstream into the branch. A merge that
// fails is undone, so no half-merged files are left under open sessions.
// Uncommitted edits to a file the merge changes are set aside and put back on
// top (git's autostash); CheckIncoming has already seen that they fit.
func MergeUpstream(root string) error {
	merging := func() bool {
		return exec.Command("git", "-C", root, "rev-parse", "-q", "--verify", "MERGE_HEAD").Run() == nil
	}
	if merging() {
		return fmt.Errorf("merge failed: another merge is already in progress here")
	}
	args := []string{"-C", root, "merge", "--no-edit"}
	incoming := nameList(root, "diff", "--name-only", mergeBase(root), "@{upstream}")
	edited := nameList(root, "diff", "--name-only", "HEAD")
	if slices.ContainsFunc(edited, func(f string) bool { return slices.Contains(incoming, f) }) {
		// Setting edits aside unstages them, and gai-watch may be committing
		// them this second.
		if staged := nameList(root, "diff", "--name-only", "--cached"); len(staged) > 0 {
			return fmt.Errorf("merge not started: %s is staged and not committed yet; commit it, then try again", staged[0])
		}
		in, err := CheckIncoming(root) // they may have changed since the question
		if err != nil {
			return err
		}
		if len(in.Conflicts) > 0 {
			return fmt.Errorf("merge not started: your changes clash in %s", in.Conflicts[0])
		}
		args = append(args, "--autostash")
	}
	out, err := exec.Command("git", append(args, "@{upstream}")...).CombinedOutput()
	if err != nil {
		if merging() {
			_ = exec.Command("git", "-C", root, "merge", "--abort").Run()
		}
		return fmt.Errorf("merge failed, nothing changed: %s", gitReason(string(out)))
	}
	// git reports success even when the edits it set aside no longer fit (a
	// session wrote to the file in that second); they are then kept in the stash.
	if strings.Contains(string(out), "utostash resulted in conflicts") {
		return fmt.Errorf("merged, but your uncommitted edits didn't fit back; they are safe in `git stash`: run `git stash pop`")
	}
	return nil
}

// PullRequest is the open pull request of the current branch.
type PullRequest struct {
	Number int    `json:"number"`
	URL    string `json:"url"`
	State  string `json:"state"`
	// Checks is GitHub's rollup of the PR's CI checks (statusCheckRollup).
	Checks []CheckItem `json:"statusCheckRollup"`
}

type CheckItem struct {
	Status     string `json:"status"`     // CheckRun: QUEUED, IN_PROGRESS, COMPLETED
	Conclusion string `json:"conclusion"` // CheckRun, once COMPLETED
	State      string `json:"state"`      // StatusContext: SUCCESS, FAILURE, PENDING, ERROR
}

// CheckSummary counts the PR's checks: passed, failed, still running.
func (p PullRequest) CheckSummary() (pass, fail, pending int) {
	for _, c := range p.Checks {
		v := c.Conclusion
		if c.State != "" {
			v = c.State
		}
		switch {
		case c.Status != "" && c.Status != "COMPLETED":
			pending++
		case v == "SUCCESS" || v == "NEUTRAL" || v == "SKIPPED":
			pass++
		case v == "" || v == "PENDING" || v == "EXPECTED":
			pending++
		default: // FAILURE, ERROR, TIMED_OUT, CANCELLED, ACTION_REQUIRED, …
			fail++
		}
	}
	return
}

// CurrentPR asks GitHub (gh) for the current branch's pull request; ok is
// false when there is none or gh can't tell. failed is true when gh couldn't
// tell (GitHub not answering, login): callers keep what they showed before,
// or a network blip hides the branch's PR (practice-stack PR #3337, 2026-11-18).
func CurrentPR(root string) (pr PullRequest, ok, failed bool) {
	return viewPR(root)
}

// PRByNumber is CurrentPR for a given pull request number (the batch PR the
// progress strip shows, which need not be the checked-out branch's PR).
func PRByNumber(root string, number int) (pr PullRequest, ok, failed bool) {
	return viewPR(root, strconv.Itoa(number))
}

func viewPR(root string, number ...string) (PullRequest, bool, bool) {
	args := append(append([]string{"pr", "view"}, number...), "--json", "number,url,state,statusCheckRollup")
	cmd := exec.Command("gh", args...)
	if len(number) > 0 {
		// A number alone means the origin repo's PR, not upstream's. Without
		// one gh needs the local branch, which GH_REPO would switch off.
		cmd = GhCmd(root, args...)
	}
	cmd.Dir = root
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		return PullRequest{}, false, !noPR(stderr.String())
	}
	var pr PullRequest
	if json.Unmarshal(out, &pr) != nil || pr.Number == 0 {
		return PullRequest{}, false, false
	}
	return pr, true, false
}

// noPR reports whether gh's error means there is no pull request, as opposed
// to gh failing to ask.
func noPR(stderr string) bool {
	s := strings.ToLower(stderr)
	return strings.Contains(s, "no pull requests found") || strings.Contains(s, "could not resolve to a pullrequest")
}
