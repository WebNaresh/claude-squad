package tmux

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Scrolled back while Claude writes: the rows being read stay put instead of
// sliding down a line per new line.
func TestScrolledViewHoldsWhileOutputGrows(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux")
	}
	dir, err := os.MkdirTemp("/tmp", "cs") // own tmux server; t.TempDir is too long for a socket path
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	t.Setenv("TMUX_TMPDIR", dir)
	t.Setenv("TMUX", "")
	name := "cs_scrollhold"
	script := `for i in $(seq 1 200); do echo line $i; done; sleep 1; for i in $(seq 201 260); do echo line $i; sleep 0.02; done; sleep 30`
	if out, err := exec.Command("tmux", "new-session", "-d", "-s", name, "-x", "80", "-y", "20", script).CombinedOutput(); err != nil {
		t.Fatalf("new-session: %v %s", err, out)
	}
	defer exec.Command("tmux", "kill-server").Run()
	time.Sleep(500 * time.Millisecond)
	_ = exec.Command("tmux", "copy-mode", "-e", "-t", name).Run()
	_ = exec.Command("tmux", "send-keys", "-t", name, "-X", "-N", "30", "scroll-up").Run()
	SetScrolled(name, true)
	defer SetScrolled(name, false)

	first := func() string {
		out, err := CaptureScreen(name)
		if err != nil {
			t.Fatal(err)
		}
		return strings.SplitN(out, "\n", 2)[0]
	}
	before := first()
	time.Sleep(3 * time.Second) // 60 more lines arrive
	if after := first(); after != before {
		t.Fatalf("view slid while output grew: top row %q, then %q", before, after)
	}
}
