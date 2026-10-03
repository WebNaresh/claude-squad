package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitT(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.email=t@t", "-c", "user.name=t", "-c", "commit.gpgsign=false"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
	return strings.TrimSpace(string(out))
}

// After a batch's PR is merged on GitHub, the next batch starts from an
// up-to-date main: merged branches go, unmerged ones stay, and nothing
// moves while there are uncommitted changes.
func TestFreshBaseAfterMerge(t *testing.T) {
	root := t.TempDir()
	origin := filepath.Join(root, "origin.git")
	gitT(t, root, "init", "-q", "--bare", "-b", "main", origin)
	repo := filepath.Join(root, "repo")
	gitT(t, root, "clone", "-q", origin, repo)
	os.WriteFile(filepath.Join(repo, "a.txt"), []byte("a"), 0o644)
	gitT(t, repo, "add", "a.txt")
	gitT(t, repo, "commit", "-q", "-m", "a")
	gitT(t, repo, "push", "-q", "origin", "main")
	gitT(t, root, "--git-dir", origin, "symbolic-ref", "HEAD", "refs/heads/main")
	gitT(t, repo, "remote", "set-head", "origin", "main")

	// The batch's dev branch, pushed and merged into main on "GitHub".
	gitT(t, repo, "checkout", "-q", "-b", "dev")
	os.WriteFile(filepath.Join(repo, "b.txt"), []byte("b"), 0o644)
	gitT(t, repo, "add", "b.txt")
	gitT(t, repo, "commit", "-q", "-m", "b")
	gitT(t, repo, "push", "-q", "origin", "dev:main")
	// An unrelated branch with work not in main.
	gitT(t, repo, "branch", "wip")
	gitT(t, repo, "checkout", "-q", "wip")
	os.WriteFile(filepath.Join(repo, "c.txt"), []byte("c"), 0o644)
	gitT(t, repo, "add", "c.txt")
	gitT(t, repo, "commit", "-q", "-m", "c")
	gitT(t, repo, "checkout", "-q", "dev")

	// Uncommitted changes: refuse, stay on dev.
	os.WriteFile(filepath.Join(repo, "dirty.txt"), []byte("x"), 0o644)
	if ok, why := freshBase(repo); ok || !strings.Contains(why, "uncommitted") {
		t.Fatalf("dirty tree: ok=%v why=%q", ok, why)
	}
	if b := gitT(t, repo, "branch", "--show-current"); b != "dev" {
		t.Fatalf("switched to %s over uncommitted work", b)
	}
	os.Remove(filepath.Join(repo, "dirty.txt"))

	if ok, why := freshBase(repo); !ok {
		t.Fatalf("clean tree refused: %s", why)
	}
	if b := gitT(t, repo, "branch", "--show-current"); b != "main" {
		t.Errorf("on %s, want main", b)
	}
	if gitT(t, repo, "rev-parse", "main") != gitT(t, repo, "rev-parse", "origin/main") {
		t.Error("main not up to date with origin/main")
	}
	branches := gitT(t, repo, "branch", "--format=%(refname:short)")
	if strings.Contains(branches, "dev") {
		t.Errorf("merged dev not deleted: %q", branches)
	}
	if !strings.Contains(branches, "wip") {
		t.Errorf("unmerged wip deleted: %q", branches)
	}
}
