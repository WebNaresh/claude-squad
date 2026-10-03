//go:build darwin || linux

package session

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

// Claude reads keys one at a time with its terminal in raw mode. Something in
// a session can put the terminal back into line mode while Claude keeps
// running (seen 2026-10-02: Enter did nothing, typed text and "^[" were
// echoed under Claude's box). RepairClaudeKeyboards finds such panes and puts
// them back the way Claude set them, so the user can type again.

var (
	cookedMu    sync.Mutex
	cookedSince = map[string]time.Time{} // tty -> first seen in line mode under Claude
)

// cookedGrace is how long a Claude pane must stay in line mode before it is
// repaired, so Claude starting up or changing modes itself is left alone.
var cookedGrace = 4 * time.Second

// RepairClaudeKeyboards checks every tmux pane whose foreground program is
// Claude and restores raw mode where it was lost. It returns the tmux
// sessions it repaired.
func RepairClaudeKeyboards() []string {
	panes, err := exec.Command("tmux", "list-panes", "-a", "-F", "#{session_name}\t#{pane_tty}").Output()
	if err != nil {
		return nil
	}
	fg := foregroundCommands()
	var repaired []string
	seen := map[string]bool{}
	for _, line := range strings.Split(strings.TrimSpace(string(panes)), "\n") {
		name, tty, ok := strings.Cut(line, "\t")
		if !ok || !isClaude(fg[filepath.Base(tty)]) {
			continue
		}
		seen[tty] = true
		cooked, err := lineMode(tty)
		if err != nil || !cooked {
			cookedMu.Lock()
			delete(cookedSince, tty)
			cookedMu.Unlock()
			continue
		}
		cookedMu.Lock()
		first, known := cookedSince[tty]
		if !known {
			cookedSince[tty] = time.Now()
		}
		cookedMu.Unlock()
		if !known || time.Since(first) < cookedGrace {
			continue
		}
		if restoreRaw(tty) == nil {
			cookedMu.Lock()
			delete(cookedSince, tty)
			cookedMu.Unlock()
			// Ctrl+L makes Claude redraw over the lines echoed meanwhile.
			_ = exec.Command("tmux", "send-keys", "-t", "="+name+":", "C-l").Run()
			repaired = append(repaired, name)
		}
	}
	cookedMu.Lock()
	for tty := range cookedSince {
		if !seen[tty] {
			delete(cookedSince, tty)
		}
	}
	cookedMu.Unlock()
	return repaired
}

// foregroundCommands maps a tty name ("ttys012") to the command line of its
// foreground process.
func foregroundCommands() map[string]string {
	out, err := exec.Command("ps", "-A", "-o", "pid=,tpgid=,tty=,args=").Output()
	if err != nil {
		return nil
	}
	fg := map[string]string{}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) < 4 || f[0] != f[1] { // the foreground group's leader
			continue
		}
		if _, err := strconv.Atoi(f[0]); err == nil {
			fg[f[2]] = strings.Join(f[3:], " ")
		}
	}
	return fg
}

func isClaude(args string) bool {
	if args == "" {
		return false
	}
	bin := strings.Fields(args)[0]
	return filepath.Base(bin) == "claude" || strings.Contains(bin, "/claude/versions/")
}

func lineMode(tty string) (bool, error) {
	f, err := os.OpenFile(tty, os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		return false, err
	}
	defer f.Close()
	t, err := unix.IoctlGetTermios(int(f.Fd()), ioctlGetTermios)
	if err != nil {
		return false, err
	}
	return t.Lflag&unix.ICANON != 0, nil
}

// restoreRaw sets the terminal the way Node (libuv) does for raw mode, which
// is what Claude asked for.
func restoreRaw(tty string) error {
	f, err := os.OpenFile(tty, os.O_RDWR|unix.O_NOCTTY, 0)
	if err != nil {
		return err
	}
	defer f.Close()
	fd := int(f.Fd())
	t, err := unix.IoctlGetTermios(fd, ioctlGetTermios)
	if err != nil {
		return err
	}
	t.Iflag &^= unix.BRKINT | unix.ICRNL | unix.INPCK | unix.ISTRIP | unix.IXON
	t.Oflag |= unix.ONLCR
	t.Cflag |= unix.CS8
	t.Lflag &^= unix.ECHO | unix.ICANON | unix.IEXTEN | unix.ISIG
	t.Cc[unix.VMIN] = 1
	t.Cc[unix.VTIME] = 0
	return unix.IoctlSetTermios(fd, ioctlSetTermios, t)
}
