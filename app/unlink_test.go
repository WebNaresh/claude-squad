package app

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A fake gai asks the rewrite question and prints its result: startUnlink
// skips the issue at once, the poll answers Enter, and the run ends with the
// "removed" text. Isolated tmux server and config dir: nothing real is touched.
func TestUnlinkSkipsAndAnswersGai(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux")
	}
	tmp := t.TempDir()
	t.Setenv("CLAUDE_SQUAD_HOME", filepath.Join(tmp, "cs"))
	// $TMUX wins over TMUX_TMPDIR: left set, this test talked to the user's
	// real tmux server, and its kill-server cleanup closed every session.
	t.Setenv("TMUX", "")
	t.Setenv("TMUX_TMPDIR", tmp)
	bin := filepath.Join(tmp, "bin")
	_ = os.MkdirAll(bin, 0o755)
	script := "#!/bin/bash\necho \"→ Removing #77 from PR #5.\"\nread -rp \"Rewrite the PR title and body from the issues that remain? [Y/n] \" a\necho \"→ done\"\n"
	if err := os.WriteFile(filepath.Join(bin, "gai"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	project := filepath.Join(tmp, "proj")
	_ = os.MkdirAll(project, 0o755)
	// Never touch a tmux server outside tmp: check where tmux would connect
	// before starting anything, and clean up by session name, not kill-server.
	if out, err := exec.Command("tmux", "start-server", ";", "display-message", "-p", "#{socket_path}").Output(); err != nil ||
		!strings.HasPrefix(strings.TrimSpace(string(out)), tmp) {
		t.Skipf("tmux would not use the isolated socket (%q)", strings.TrimSpace(string(out)))
	}
	t.Cleanup(func() { _ = exec.Command("tmux", "kill-session", "-t", "=csunlink_proj_77").Run() })

	msg := startUnlink(project, 77, 5)
	p, ok := msg.(unlinkPollMsg)
	if !ok {
		t.Fatalf("startUnlink returned %v", msg)
	}
	if !readSkipped(project)[77] {
		t.Fatal("issue not skipped")
	}
	var done unlinkDoneMsg
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		switch r := pollUnlink(p)().(type) {
		case unlinkPollMsg:
			p = r
			continue
		case unlinkDoneMsg:
			done = r
		}
		break
	}
	if done.text != "#77 removed from its PR and skipped" {
		t.Fatalf("result = %q", done.text)
	}
}

func TestUnlinkResultNotOnPR(t *testing.T) {
	p := unlinkPollMsg{issue: 9, last: "→ #9 not referenced in PR #3 — nothing to remove there."}
	if got := unlinkResult(p); got != "#9 skipped; it was not on an open PR" {
		t.Fatal(got)
	}
}
