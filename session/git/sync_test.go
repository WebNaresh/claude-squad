package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// twoClones makes a bare "GitHub" with a teammate's clone and yours, both on main.
func twoClones(t *testing.T) (mine, theirs string) {
	t.Helper()
	// Not the user's git setup: its global hooks and gai-watch stay out.
	t.Setenv("GIT_CONFIG_GLOBAL", "/dev/null")
	t.Setenv("GIT_CONFIG_NOSYSTEM", "1")
	dir := t.TempDir()
	bare := filepath.Join(dir, "hub.git")
	gitRun(t, "", "init", "-q", "--bare", "-b", "main", bare)
	mine, theirs = filepath.Join(dir, "mine"), filepath.Join(dir, "theirs")
	for i, c := range []string{theirs, mine} {
		gitRun(t, "", "clone", "-q", bare, c)
		gitRun(t, c, "config", "user.name", []string{"Tess", "Me"}[i])
		gitRun(t, c, "config", "user.email", "x@example.com")
		if i == 0 {
			gitRun(t, c, "checkout", "-q", "-B", "main")
			put(t, c, "base", "1\n")
			gitRun(t, c, "push", "-q", "-u", "origin", "main")
		}
	}
	return mine, theirs
}

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func put(t *testing.T, dir, name, text string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", name)
	gitRun(t, dir, "commit", "-q", "-m", name)
}

// A teammate pushed first: the push says why, the check says who and that it
// merges cleanly, and merge + push works.
func TestDivergedMergeAndPush(t *testing.T) {
	mine, theirs := twoClones(t)
	put(t, theirs, "a", "a\n")
	gitRun(t, theirs, "push", "-q")
	put(t, mine, "b", "b\n")
	if s := Sync(mine); s.Behind != 0 {
		t.Fatalf("before a fetch behind = %d", s.Behind)
	}
	err := Push(mine)
	if err == nil || !strings.Contains(err.Error(), "rejected") {
		t.Fatalf("push: %v, want the rejection", err)
	}
	in, err := CheckIncoming(mine)
	if err != nil || !in.Diverged() || in.Behind != 1 || strings.Join(in.Authors, ",") != "Tess" || len(in.Conflicts) != 0 {
		t.Fatalf("incoming %+v, %v", in, err)
	}
	if err := MergeUpstream(mine); err != nil {
		t.Fatal(err)
	}
	if err := Push(mine); err != nil {
		t.Fatal(err)
	}
	if s := Sync(mine); s.Ahead != 0 || s.Behind != 0 {
		t.Fatalf("after: %+v", s)
	}
}

// Both changed the same line: the check names the file, and a merge tried
// anyway leaves the tree as it was.
func TestDivergedConflictIsUndone(t *testing.T) {
	mine, theirs := twoClones(t)
	put(t, theirs, "base", "theirs\n")
	gitRun(t, theirs, "push", "-q")
	put(t, mine, "base", "mine\n")
	in, err := CheckIncoming(mine)
	if err != nil || strings.Join(in.Conflicts, ",") != "base" {
		t.Fatalf("conflicts %q, %v", in.Conflicts, err)
	}
	if err := MergeUpstream(mine); err == nil {
		t.Fatal("a conflicting merge passed")
	}
	if got, _ := os.ReadFile(filepath.Join(mine, "base")); string(got) != "mine\n" {
		t.Fatalf("file left as %q", got)
	}
	out, _ := exec.Command("git", "-C", mine, "status", "--porcelain").Output()
	if len(out) != 0 {
		t.Fatalf("tree left dirty: %s", out)
	}
}

// Uncommitted edits to the same file, in another place: merged and kept.
func TestDivergedKeepsUncommittedEdits(t *testing.T) {
	mine, theirs := twoClones(t)
	put(t, theirs, "doc", "top\n2\n3\n4\n5\n6\nend\n")
	gitRun(t, theirs, "push", "-q")
	gitRun(t, mine, "pull", "-q")
	put(t, theirs, "doc", "THEIRS\n2\n3\n4\n5\n6\nend\n")
	gitRun(t, theirs, "push", "-q")
	put(t, mine, "b", "b\n")
	_ = os.WriteFile(filepath.Join(mine, "doc"), []byte("top\n2\n3\n4\n5\n6\nMINE\n"), 0o644)
	_ = os.WriteFile(filepath.Join(mine, "new"), []byte("new\n"), 0o644)
	in, err := CheckIncoming(mine)
	if err != nil || len(in.Conflicts) != 0 {
		t.Fatalf("%+v %v", in, err)
	}
	if err := MergeUpstream(mine); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(filepath.Join(mine, "doc")); string(got) != "THEIRS\n2\n3\n4\n5\n6\nMINE\n" {
		t.Fatalf("doc: %q", got)
	}
	out, _ := exec.Command("git", "-C", mine, "status", "--porcelain").Output()
	if string(out) != " M doc\n?? new\n" {
		t.Fatalf("status %q: the edits must stay uncommitted", out)
	}
	if out, _ := exec.Command("git", "-C", mine, "stash", "list").Output(); len(out) != 0 {
		t.Fatalf("stash left behind: %s", out)
	}
	if err := Push(mine); err != nil {
		t.Fatal(err)
	}
}

// Uncommitted edits that clash, or are staged: not started, edits kept.
func TestDivergedUncommittedClash(t *testing.T) {
	mine, theirs := twoClones(t)
	put(t, theirs, "base", "theirs\n")
	gitRun(t, theirs, "push", "-q")
	put(t, mine, "b", "b\n")
	_ = os.WriteFile(filepath.Join(mine, "base"), []byte("unsaved\n"), 0o644)
	in, err := CheckIncoming(mine)
	if err != nil || strings.Join(in.Conflicts, ",") != "base" {
		t.Fatalf("conflicts %q %v", in.Conflicts, err)
	}
	for _, want := range []string{"clash in base", "base is staged"} {
		if err := MergeUpstream(mine); err == nil || !strings.Contains(err.Error(), want) {
			t.Fatalf("merge: %v, want %q", err, want)
		}
		if got, _ := os.ReadFile(filepath.Join(mine, "base")); string(got) != "unsaved\n" {
			t.Fatalf("edits lost: %q", got)
		}
		gitRun(t, mine, "add", "base")
	}
}

// The reason names the file, not only "…would be overwritten by merge:".
func TestGitReason(t *testing.T) {
	out := "Updating a..b\nerror: Your local changes to the following files would be overwritten by merge:\n\tdocs/x.md\nPlease commit\nAborting\n"
	if got := gitReason(out); !strings.HasSuffix(got, "by merge: docs/x.md") {
		t.Fatal(got)
	}
	if got := gitReason("! [rejected] main -> main (non-fast-forward)\nerror: failed to push some refs to x\nhint: See the 'Note'"); !strings.HasPrefix(got, "! [rejected]") {
		t.Fatal(got)
	}
}

// Only the teammate has commits, and you edited the same file: pulled, kept.
func TestPullKeepsUncommittedEdits(t *testing.T) {
	mine, theirs := twoClones(t)
	put(t, theirs, "doc", "top\n2\n3\n4\n5\n6\nend\n")
	gitRun(t, theirs, "push", "-q")
	gitRun(t, mine, "pull", "-q")
	put(t, theirs, "doc", "THEIRS\n2\n3\n4\n5\n6\nend\n")
	gitRun(t, theirs, "push", "-q")
	doc := filepath.Join(mine, "doc")
	_ = os.WriteFile(doc, []byte("top\n2\n3\n4\n5\n6\nMINE\n"), 0o644)
	if err := Pull(mine); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(doc); string(got) != "THEIRS\n2\n3\n4\n5\n6\nMINE\n" {
		t.Fatalf("doc: %q", got)
	}
	if s := Sync(mine); s.Ahead != 0 || s.Behind != 0 {
		t.Fatalf("not a fast-forward: %+v", s)
	}
	if err := Pull(mine); err != nil {
		t.Fatalf("nothing to pull: %v", err)
	}
	put(t, theirs, "doc", "AGAIN\n2\n3\n4\n5\n6\nTHEIRS\n")
	gitRun(t, theirs, "push", "-q")
	if err := Pull(mine); err == nil || !strings.Contains(err.Error(), "clash in doc") {
		t.Fatalf("clash: %v", err)
	}
	if got, _ := os.ReadFile(doc); !strings.Contains(string(got), "MINE") {
		t.Fatalf("edits lost: %q", got)
	}
}

// Both have commits: Pull refuses, so the caller can ask about a merge.
func TestPullDiverged(t *testing.T) {
	mine, theirs := twoClones(t)
	put(t, theirs, "a", "a\n")
	gitRun(t, theirs, "push", "-q")
	put(t, mine, "b", "b\n")
	if err := Pull(mine); err == nil || !strings.Contains(err.Error(), "both have new commits") {
		t.Fatal(err)
	}
}
