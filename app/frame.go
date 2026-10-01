package app

import (
	"claude-squad/config"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Seamless self-update: the old cs saves the screen it last drew and execs
// the new build without leaving the alternate screen, so that picture stays
// up; the new cs shows the saved picture until its sessions are loaded. To
// the user it looks like a short pause, nothing vanishes.

// RestartNow, set by main on systems that support it, replaces this process
// with the new build without restoring the terminal (so the screen stays).
// It only returns on failure.
var RestartNow func() error

const frameFile = "last-frame"

// frameMaxAge: a saved screen older than this is not shown (it's from a
// normal quit, not an update).
const frameMaxAge = 30 * time.Second

// restartedEnv tells the new process it was started by an in-place update.
const restartedEnv = "CS_INPLACE_RESTART"

func saveFrame(view string, w, h int, focus, focusRow string) {
	dir, err := config.GetConfigDir()
	if err != nil || view == "" {
		return
	}
	if focus == "" {
		focus = "-"
	}
	if focusRow == "" {
		focusRow = "-"
	}
	header := fmt.Sprintf("%d %d %d %s %s\n", w, h, time.Now().UnixNano(), focus, focusRow)
	_ = os.WriteFile(filepath.Join(dir, frameFile), []byte(header+view), 0600)
}

// loadFrame returns the screen saved by the previous process if it is fresh,
// the window size it was drawn for, and the session being typed into.
func loadFrame() (view string, w, h int, focus, focusRow string) {
	dir, err := config.GetConfigDir()
	if err != nil {
		return "", 0, 0, "", ""
	}
	path := filepath.Join(dir, frameFile)
	data, err := os.ReadFile(path)
	_ = os.Remove(path) // one use only
	if err != nil {
		return "", 0, 0, "", ""
	}
	header, body, ok := strings.Cut(string(data), "\n")
	if !ok {
		return "", 0, 0, "", ""
	}
	var saved int64
	if _, err := fmt.Sscanf(header, "%d %d %d %s %s", &w, &h, &saved, &focus, &focusRow); err != nil {
		return "", 0, 0, "", ""
	}
	if time.Since(time.Unix(0, saved)) > frameMaxAge {
		return "", 0, 0, "", ""
	}
	if focus == "-" {
		focus = ""
	}
	if focusRow == "-" {
		focusRow = ""
	}
	return body, w, h, focus, focusRow
}

// selectRowKey selects the row with this key (see selectedRowKey).
func (m *home) selectRowKey(key string) {
	if name, ok := strings.CutPrefix(key, "session:"); ok {
		m.list.SelectExternal(name)
		return
	}
	if name, ok := strings.CutPrefix(key, "agent:"); ok {
		for _, inst := range m.list.GetInstances() {
			if inst.TmuxName() == name {
				m.list.SelectInstance(inst)
				return
			}
		}
	}
}
