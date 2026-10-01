package app

import (
	"claude-squad/config"
	"claude-squad/log"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// ErrRestart is returned by Run when cs quit to restart itself on a new build.
type ErrRestart struct{}

func (ErrRestart) Error() string { return "restart for new build" }

const updateCheckInterval = 3 * time.Second

// updater rebuilds cs from its source folder (CS_SOURCE_DIR, set by the cs
// launcher) when the code changes, so a running cs never stays on an old
// build. Agents live in tmux, so restarting the UI doesn't touch them.
type updater struct {
	src string
	bin string
	// built is the time of the newest source file the running binary includes
	// (or that last failed to build, so a broken tree isn't rebuilt every tick).
	built time.Time
}

func newUpdater() *updater {
	src := os.Getenv("CS_SOURCE_DIR")
	bin, err := os.Executable()
	if src == "" || err != nil {
		return nil
	}
	st, err := os.Stat(bin)
	if err != nil {
		return nil
	}
	return &updater{src: src, bin: bin, built: st.ModTime()}
}

type updateReadyMsg struct{}
type updateCheckMsg struct{}

// newestSource returns the latest modification time of the Go sources.
func (u *updater) newestSource() time.Time {
	var newest time.Time
	_ = filepath.WalkDir(u.src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if name := d.Name(); name != "." && (strings.HasPrefix(name, ".") || name == "vendor" || name == "web") && path != u.src {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "go.mod") || strings.HasSuffix(path, "go.sum") {
			if info, err := d.Info(); err == nil && info.ModTime().After(newest) {
				newest = info.ModTime()
			}
		}
		return nil
	})
	return newest
}

// checkCmd waits, then rebuilds if the source is newer than the running
// binary. It reports updateReadyMsg when a new binary is in place.
func (u *updater) checkCmd() tea.Cmd {
	return func() tea.Msg {
		time.Sleep(updateCheckInterval)
		// While work is in progress, a hold file keeps half-finished builds out.
		if dir, err := config.GetConfigDir(); err == nil {
			if _, err := os.Stat(filepath.Join(dir, "hold-update")); err == nil {
				return updateCheckMsg{}
			}
		}
		// Someone else (the cs launcher) may already have built a newer binary.
		if st, err := os.Stat(u.bin); err == nil && st.ModTime().After(u.built) {
			return updateReadyMsg{}
		}
		newest := u.newestSource()
		if !newest.After(u.built) {
			return updateCheckMsg{}
		}
		tmp := u.bin + ".new"
		logEvent("self-update: source changed, building")
		started := time.Now()
		out, err := exec.Command("go", "build", "-C", u.src, "-o", tmp, ".").CombinedOutput()
		if err != nil {
			logEvent("self-update: build FAILED after %s, staying on current build: %s", time.Since(started).Round(time.Millisecond), firstLineOf(string(out)))
			log.WarningLog.Printf("self-update build failed, staying on current build: %s", out)
			_ = os.Remove(tmp)
			u.built = newest
			return updateCheckMsg{}
		}
		if err := os.Rename(tmp, u.bin); err != nil {
			log.WarningLog.Printf("self-update: %v", err)
			u.built = newest
			return updateCheckMsg{}
		}
		logEvent("self-update: built in %s", time.Since(started).Round(time.Millisecond))
		return updateReadyMsg{}
	}
}

func firstLineOf(s string) string {
	line, _, _ := strings.Cut(strings.TrimSpace(s), "\n")
	return line
}
