package app

import (
	"os/exec"
	"path/filepath"
	"strings"
)

// Before the first issue of a new batch (no open PR of the project's own:
// the last one was merged), the project is brought back to an up-to-date
// default branch, the same as the user's `git cleanup`: fetch, switch to
// main, fast-forward it, delete local branches already merged into it. gai
// then cuts a fresh dev from there. Without it a new batch started on the
// old dev, or on a main the user's own cleanup had failed to pull.
//
// It never runs over uncommitted work, never deletes a branch with commits
// that aren't in main, and on any failure (no network, can't fast-forward)
// the batch waits for the next scan instead of starting on stale code.

// freshBase prepares project for a new batch; ok=false says why it can't yet.
func freshBase(project string) (ok bool, why string) {
	git := func(args ...string) (string, error) {
		out, err := exec.Command("git", append([]string{"-C", project}, args...)...).CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
	if out, _ := git("status", "--porcelain"); out != "" {
		return false, "uncommitted changes (sessions still finishing)"
	}
	if out, err := git("fetch", "-q", "origin", "--prune"); err != nil {
		return false, "fetch failed: " + firstLineOf(out)
	}
	def := "main"
	if out, err := git("symbolic-ref", "--short", "refs/remotes/origin/HEAD"); err == nil && strings.HasPrefix(out, "origin/") {
		def = strings.TrimPrefix(out, "origin/")
	}
	if cur, _ := git("branch", "--show-current"); cur != def {
		if out, err := git("checkout", def); err != nil {
			return false, "checkout " + def + " failed: " + firstLineOf(out)
		}
	}
	if out, err := git("pull", "--ff-only", "-q", "origin", def); err != nil {
		return false, "pull failed: " + firstLineOf(out)
	}
	branches, _ := git("for-each-ref", "--format=%(refname:short)", "refs/heads/")
	var deleted []string
	for _, b := range strings.Fields(branches) {
		if b == def {
			continue
		}
		if _, err := git("merge-base", "--is-ancestor", b, "origin/"+def); err == nil {
			if _, err := git("branch", "-d", b); err == nil {
				deleted = append(deleted, b)
			}
		}
	}
	logEvent("daemon: %s: new batch starts on an up-to-date %s (deleted merged: %s)", filepath.Base(project), def, strings.Join(deleted, ", "))
	return true, ""
}
