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
	openPR := ""
	old := openPRFrom
	openPRFrom = func(string, string) (string, error) { return openPR, nil }
	defer func() { openPRFrom = old }()

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

	// A PR still open from dev (even one listing no issues): refuse, stay on dev.
	openPR = "2355"
	if ok, why := freshBase(repo); ok || !strings.Contains(why, "#2355") {
		t.Fatalf("open PR from dev: ok=%v why=%q", ok, why)
	}
	if b := gitT(t, repo, "branch", "--show-current"); b != "dev" {
		t.Fatalf("switched to %s while PR #2355 is open from dev", b)
	}
	openPR = ""

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

func TestIsGaiPromptBranchName(t *testing.T) {
	for line, want := range map[string]bool{
		"Branch name [dev] (n to abort):":                                    true,
		"Create a PR for #585? [Y/n]":                                        true,
		"Extra instructions for Claude (optional — Ctrl+V pastes an image):": true,
		"On 'main' — a PR needs a feature branch.":                           false,
		"Branch name is dev":                                                 false,
	} {
		if got := isGaiPrompt(line); got != want {
			t.Errorf("isGaiPrompt(%q) = %v, want %v", line, got, want)
		}
	}
}

// A dev that is only an older main is deleted so gai cuts a fresh one; a dev
// with its own work holds the batch back instead of being switched to.
func TestStaleDev(t *testing.T) {
	repo := t.TempDir()
	gitT(t, repo, "init", "-q", "-b", "main")
	os.WriteFile(filepath.Join(repo, "a.txt"), []byte("a"), 0o644)
	gitT(t, repo, "add", "a.txt")
	gitT(t, repo, "commit", "-q", "-m", "a")
	gitT(t, repo, "branch", "dev")
	os.WriteFile(filepath.Join(repo, "b.txt"), []byte("b"), 0o644)
	gitT(t, repo, "add", "b.txt")
	gitT(t, repo, "commit", "-q", "-m", "b")

	if why := staleDev(repo); why != "" {
		t.Fatalf("older-main dev: why=%q", why)
	}
	if strings.Contains(gitT(t, repo, "branch", "--format=%(refname:short)"), "dev") {
		t.Fatal("older-main dev not deleted")
	}

	gitT(t, repo, "checkout", "-q", "-b", "dev")
	os.WriteFile(filepath.Join(repo, "c.txt"), []byte("c"), 0o644)
	gitT(t, repo, "add", "c.txt")
	gitT(t, repo, "commit", "-q", "-m", "c")
	gitT(t, repo, "checkout", "-q", "main")
	if why := staleDev(repo); why == "" {
		t.Fatal("dev with its own work: want a reason")
	}
	if !strings.Contains(gitT(t, repo, "branch", "--format=%(refname:short)"), "dev") {
		t.Fatal("dev with its own work was deleted")
	}
}
