package tmux

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// A tile scrolled back with the wheel must still fill every row. With -J the
// wrapped history lines were joined, so the capture came back short and the
// tile went blank at the bottom, more the further back it scrolled.
func TestScrolledCaptureFillsTile(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("no tmux")
	}
	// Plain `tmux` calls in the package reach a private server.
	sock := fmt.Sprintf("cstest-scroll-%d", os.Getpid())
	real, _ := exec.LookPath("tmux")
	bin := t.TempDir()
	wrapper := fmt.Sprintf("#!/bin/sh\nexec %s -L %s \"$@\"\n", real, sock)
	if err := os.WriteFile(filepath.Join(bin, "tmux"), []byte(wrapper), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+":"+os.Getenv("PATH"))
	defer exec.Command(real, "-L", sock, "kill-server").Run()

	const name, w, h = "scrolltest", 40, 20
	// 120 lines of 100 characters: each wraps to 3 rows at width 40.
	script := `for i in $(seq 1 120); do printf "%03d-%097d\n" $i 0; done; sleep 60`
	if out, err := exec.Command("tmux", "new-session", "-d", "-s", name, "-x", fmt.Sprint(w), "-y", fmt.Sprint(h), "sh", "-c", script).CombinedOutput(); err != nil {
		t.Fatalf("new-session: %v %s", err, out)
	}
	for i := 0; i < 50; i++ {
		out, _ := exec.Command("tmux", "display-message", "-p", "-t", name, "#{history_size}").Output()
		if strings.TrimSpace(string(out)) > "300" {
			break
		}
		exec.Command("sleep", "0.1").Run()
	}
	exec.Command("tmux", "copy-mode", "-t", name).Run()
	exec.Command("tmux", "send-keys", "-t", name, "-X", "-N", "150", "scroll-up").Run()
	SetScrolled(name, true)
	defer SetScrolled(name, false)

	content, err := NewExternalTmuxSession(name).CapturePaneContent()
	if err != nil {
		t.Fatal(err)
	}
	rows := strings.Split(strings.TrimSuffix(content, "\n"), "\n")
	if len(rows) != h {
		t.Fatalf("scrolled capture has %d rows, want %d (tile goes blank below)", len(rows), h)
	}

	// All the way to the top: nothing further up to fill from.
	exec.Command("tmux", "send-keys", "-t", name, "-X", "history-top").Run()
	content, _ = NewExternalTmuxSession(name).CapturePaneContent()
	rows = strings.Split(strings.TrimSuffix(content, "\n"), "\n")
	if len(rows) != h || !strings.HasPrefix(rows[0], "001-") {
		t.Fatalf("at the top: %d rows starting %q, want %d rows from line 001", len(rows), rows[0], h)
	}
}
