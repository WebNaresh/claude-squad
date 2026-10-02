//go:build darwin || linux

package session

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// A Claude pane left in line mode (issue log: "Enter does nothing") is put
// back into raw mode, but only after the grace period.
func TestRepairClaudeKeyboards(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux")
	}
	dir, err := os.MkdirTemp("/tmp", "kb") // short: tmux socket paths are length-limited
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	t.Setenv("TMUX_TMPDIR", dir) // a private tmux server
	t.Setenv("TMUX", "")
	fake := filepath.Join(dir, "claude") // looks like Claude to ps; keeps the default line mode
	if err := os.Symlink("/bin/sleep", fake); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("tmux", "new-session", "-d", "-s", "kb", fake, "30").CombinedOutput(); err != nil {
		t.Fatalf("tmux: %s", out)
	}
	defer exec.Command("tmux", "kill-server").Run()
	out, _ := exec.Command("tmux", "display", "-p", "-t", "kb", "#{pane_tty}").Output()
	tty := strings.TrimSpace(string(out))
	time.Sleep(300 * time.Millisecond)

	cookedGrace = 200 * time.Millisecond
	defer func() { cookedGrace = 4 * time.Second }()
	if got := RepairClaudeKeyboards(); len(got) != 0 {
		t.Fatalf("repaired on first sight: %v", got)
	}
	if cooked, _ := lineMode(tty); !cooked {
		t.Fatal("test pane should start in line mode")
	}
	time.Sleep(300 * time.Millisecond)
	if got := RepairClaudeKeyboards(); len(got) != 1 || got[0] != "kb" {
		t.Fatalf("repaired %v, want [kb]", got)
	}
	if cooked, _ := lineMode(tty); cooked {
		t.Error("still in line mode after repair")
	}
}
