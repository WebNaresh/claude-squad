package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"claude-squad/session"
)

// A tile stopped on "resets 6pm" gets continue 5s after 6pm (not 30–45s
// late), the daemon is told to look again right at that moment, and a
// continue Claude refused (sent before the server's reset) is sent once more.
// Live: a private tmux server whose pane shows the limit and counts what is typed.
func TestResumeAfterLimitTiming(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux")
	}
	sock := fmt.Sprintf("cstest-limit-%d", os.Getpid())
	real, _ := exec.LookPath("tmux")
	bin := t.TempDir()
	wrapper := fmt.Sprintf("#!/bin/sh\nexec %s -L %s \"$@\"\n", real, sock)
	if err := os.WriteFile(filepath.Join(bin, "tmux"), []byte(wrapper), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	defer exec.Command(real, "-L", sock, "kill-server").Run()

	// The pane keeps showing the limit (Claude refusing), and records each
	// line typed into it without echoing it.
	typed := filepath.Join(t.TempDir(), "typed")
	screenFile := filepath.Join(t.TempDir(), "screen")
	screen := "● Bash(go test)\n  ⎿  You've hit your session limit · resets 6pm (Asia/Calcutta)\n     /upgrade to increase your usage limit.\n\n❯ "
	if err := os.WriteFile(screenFile, []byte(screen), 0o644); err != nil {
		t.Fatal(err)
	}
	script := fmt.Sprintf(`cat %s; stty -echo; cat >> %s`, screenFile, typed)
	const name = "limittest"
	if out, err := exec.Command("tmux", "new-session", "-d", "-s", name, "-x", "100", "-y", "20", "sh", "-c", script).CombinedOutput(); err != nil {
		t.Fatalf("new-session: %v %s", err, out)
	}
	time.Sleep(300 * time.Millisecond)

	sessions := []*session.ExternalSession{{Kind: session.KindTmux, Name: name, Live: name, Status: "idle"}}
	done := map[string]bool{}
	day := time.Now()
	at := func(h, m, s int) time.Time { return time.Date(day.Year(), day.Month(), day.Day(), h, m, s, 0, time.Local) }
	count := func() int {
		time.Sleep(300 * time.Millisecond)
		b, _ := os.ReadFile(typed)
		return strings.Count(string(b), "continue")
	}

	if next := resumeAfterLimit(sessions, done, at(17, 59, 50)); !next.Equal(at(18, 0, 5)) || count() != 0 {
		t.Fatalf("before the reset: next=%v typed=%d, want next 18:00:05 and nothing typed", next, count())
	}
	if resumeAfterLimit(sessions, done, at(18, 0, 6)); count() != 1 {
		t.Fatalf("6s after the reset: typed %d continue, want 1", count())
	}
	if next := resumeAfterLimit(sessions, done, at(18, 0, 30)); !next.Equal(at(18, 1, 30)) || count() != 1 {
		t.Fatalf("still on the limit at 18:00:30: next=%v typed=%d, want retry due 18:01:30, nothing new", next, count())
	}
	resumeAfterLimit(sessions, done, at(18, 1, 31))
	resumeAfterLimit(sessions, done, at(18, 5, 0))
	if count() != 2 {
		t.Fatalf("after the retry window: typed %d continue, want exactly 2", count())
	}
}
