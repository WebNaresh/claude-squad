package session

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
)

// A starting cs (the background runner restarts on each build) must leave the
// waiting tiles of a cs that still runs; only a dead one's are stale.
func TestCloseStaleQueuedKeepsLiveOwners(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux")
	}
	// Never the user's tmux server: a private socket folder, and TMUX unset.
	dir, err := os.MkdirTemp("/tmp", "csq")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMUX_TMPDIR", dir)
	t.Setenv("TMUX", "")
	defer os.RemoveAll(dir)
	defer exec.Command("tmux", "kill-server").Run()

	// "Another cs": this test binary again, sleeping.
	other := exec.Command(os.Args[0], "-test.run=TestQueueOwnerHelper")
	other.Env = append(os.Environ(), "QUEUE_OWNER_HELPER=1")
	stdin, _ := other.StdinPipe()
	if err := other.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { stdin.Close(); _ = other.Wait() }()
	dead := exec.Command("true")
	_ = dead.Run()

	for name, owner := range map[string]int{"live": other.Process.Pid, "dead": dead.Process.Pid, "own": os.Getpid(), "old": 1, "reused": os.Getppid()} {
		if out, err := exec.Command("tmux", "new-session", "-d", "-s", name, "sleep 100").CombinedOutput(); err != nil {
			t.Fatalf("%v %s", err, out)
		}
		_ = exec.Command("tmux", "set-option", "-t", name, queuedOption, strconv.Itoa(owner)).Run()
	}
	_ = exec.Command("tmux", "new-session", "-d", "-s", "plain", "sleep 100").Run()
	CloseStaleQueuedIssues()
	out, _ := exec.Command("tmux", "list-sessions", "-F", "#{session_name}").Output()
	if got := strings.Join(strings.Fields(string(out)), " "); got != "live old plain" {
		t.Fatalf("left: %q, want live old plain", got)
	}
}

func TestQueueOwnerHelper(t *testing.T) {
	if os.Getenv("QUEUE_OWNER_HELPER") == "" {
		t.Skip("helper")
	}
	_, _ = os.Stdin.Read(make([]byte, 1)) // until the parent closes it
}
