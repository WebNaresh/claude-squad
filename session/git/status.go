package git

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// FileStatus is one entry of `git status`: X is the staged (index) state and
// Y the working-tree state, as in `git status --porcelain`.
type FileStatus struct {
	Path string
	// Orig is the old path of a rename or copy.
	Orig string
	X, Y byte
}

// Untracked reports a file git doesn't track yet.
func (f FileStatus) Untracked() bool { return f.X == '?' }

// Staged reports changes in the index (the "Staged" section).
func (f FileStatus) Staged() bool { return f.X != ' ' && f.X != '?' && f.X != '!' }

// Changed reports working-tree changes not yet staged (the "Changes" section).
// A file can be both staged and changed (e.g. "MM").
func (f FileStatus) Changed() bool { return f.Untracked() || (f.Y != ' ' && f.Y != '!') }

// Status lists changed files in the repository at root.
func Status(root string) ([]FileStatus, error) {
	out, err := exec.Command("git", "-C", root, "status", "--porcelain=v1", "-z", "-uall").Output()
	if err != nil {
		return nil, fmt.Errorf("git status: %w", err)
	}
	return parsePorcelainZ(string(out)), nil
}

// parsePorcelainZ parses `git status --porcelain=v1 -z`. Entries are
// NUL-separated "XY path"; renames and copies are followed by the old path.
func parsePorcelainZ(out string) []FileStatus {
	var files []FileStatus
	fields := strings.Split(out, "\x00")
	for i := 0; i < len(fields); i++ {
		e := fields[i]
		if len(e) < 4 {
			continue
		}
		f := FileStatus{X: e[0], Y: e[1], Path: e[3:]}
		if (f.X == 'R' || f.X == 'C') && i+1 < len(fields) {
			i++
			f.Orig = fields[i]
		}
		files = append(files, f)
	}
	return files
}

// Branch returns the current branch name, or "" when detached.
func Branch(root string) string {
	out, err := exec.Command("git", "-C", root, "branch", "--show-current").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func hasCommits(root string) bool {
	return exec.Command("git", "-C", root, "rev-parse", "--verify", "-q", "HEAD").Run() == nil
}

func run(root string, args ...string) error {
	out, err := exec.Command("git", append([]string{"-C", root}, args...)...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("git %s: %s", args[0], strings.TrimSpace(string(out)))
	}
	return nil
}

// Stage adds the given paths (or every change when none are given) to the index.
func Stage(root string, paths ...string) error {
	if len(paths) == 0 {
		return run(root, "add", "-A")
	}
	return run(root, append([]string{"add", "-A", "--"}, paths...)...)
}

// Unstage removes the given paths (or everything when none are given) from
// the index, keeping the working-tree changes.
func Unstage(root string, paths ...string) error {
	if !hasCommits(root) {
		// No HEAD to restore from yet: drop the files from the index instead.
		if len(paths) == 0 {
			paths = []string{"."}
		}
		return run(root, append([]string{"rm", "-r", "-q", "--cached", "--"}, paths...)...)
	}
	if len(paths) == 0 {
		return run(root, "restore", "--staged", ".")
	}
	return run(root, append([]string{"restore", "--staged", "--"}, paths...)...)
}

// Discard throws away a file's unstaged changes. An untracked file is deleted.
func Discard(root string, f FileStatus) error {
	if f.Untracked() {
		return os.Remove(filepath.Join(root, f.Path))
	}
	return run(root, "restore", "--", f.Path)
}

// Commit commits what is staged.
func Commit(root, message string) error {
	if strings.TrimSpace(message) == "" {
		return fmt.Errorf("the commit message is empty")
	}
	return run(root, "commit", "-q", "-m", message)
}

// FileDiff returns the diff of one file: the staged version when staged is
// true, otherwise the working-tree changes (the whole file for untracked).
func FileDiff(root string, f FileStatus, staged bool) (string, error) {
	var cmd *exec.Cmd
	switch {
	case staged:
		cmd = exec.Command("git", "-C", root, "diff", "--cached", "--", f.Path)
	case f.Untracked():
		cmd = exec.Command("git", "-C", root, "diff", "--no-index", "--", os.DevNull, f.Path)
	default:
		cmd = exec.Command("git", "-C", root, "diff", "--", f.Path)
	}
	out, err := cmd.Output()
	// `git diff --no-index` exits 1 when the files differ.
	if err != nil && !(f.Untracked() && len(out) > 0) {
		return "", fmt.Errorf("git diff: %w", err)
	}
	return string(out), nil
}

// AutoCommitWatched reports whether gai-watch (fswatch on <root>/.git) is
// running for this repo; it commits staged files on its own within seconds.
func AutoCommitWatched(root string) bool {
	out, err := exec.Command("pgrep", "-fl", "fswatch").Output()
	if err != nil {
		return false
	}
	want := realPath(filepath.Join(root, ".git"))
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		// gai-watch runs `fswatch … <repo>/.git`; compare real paths, since
		// it may record /tmp/x while git reports /private/tmp/x.
		if len(fields) > 0 && realPath(fields[len(fields)-1]) == want {
			return true
		}
	}
	return false
}

func realPath(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}
