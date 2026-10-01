//go:build !windows

package main

import (
	"claude-squad/app"
	"claude-squad/log"
	"fmt"
	"os"
	"strings"
	"syscall"

	"golang.org/x/term"
)

// restartSelf replaces this process with the (newly built) binary on disk.
// It first draws an "Updating" screen on the alternate screen, so the user
// sees that instead of the shell flashing up between the old and new screen;
// the new process takes the alternate screen over when it starts.
func restartSelf() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	showSplash("⟳  Updating cs…")
	log.CloseQuiet()
	return syscall.Exec(exe, os.Args, os.Environ())
}

// restartInPlace replaces the process without touching the screen: the
// alternate screen and the last picture stay up while the new build starts.
func restartInPlace() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	// The terminal stays in raw mode: keys typed during the switch are held,
	// unechoed, and read by the new process. It restores normal mode on quit.
	log.CloseQuiet()
	return syscall.Exec(exe, os.Args, append(os.Environ(), "CS_INPLACE_RESTART=1"))
}

func init() {
	app.RestartNow = restartInPlace
}

// showSplash clears the alternate screen and centres a message on it.
func showSplash(msg string) {
	w, h, err := term.GetSize(int(os.Stdout.Fd()))
	if err != nil || w <= 0 || h <= 0 {
		w, h = 80, 24
	}
	pad := max(0, (w-len([]rune(msg)))/2)
	fmt.Print("\x1b[?1049h\x1b[?25l\x1b[2J" + fmt.Sprintf("\x1b[%d;1H", h/2) + strings.Repeat(" ", pad) + "\x1b[1m" + msg + "\x1b[0m")
}
