package git

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
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
		lines := strings.Split(strings.TrimSpace(string(out)), "\n")
		return fmt.Errorf("push failed: %s", lines[len(lines)-1])
	}
	return nil
}

// PullRequest is the open pull request of the current branch.
type PullRequest struct {
	Number int    `json:"number"`
	URL    string `json:"url"`
	State  string `json:"state"`
}

// CurrentPR asks GitHub (gh) for the current branch's pull request; ok is
// false when there is none or gh can't tell.
func CurrentPR(root string) (PullRequest, bool) {
	cmd := exec.Command("gh", "pr", "view", "--json", "number,url,state")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return PullRequest{}, false
	}
	var pr PullRequest
	if json.Unmarshal(out, &pr) != nil || pr.Number == 0 {
		return PullRequest{}, false
	}
	return pr, true
}
