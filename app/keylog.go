package app

import (
	"bufio"
	"claude-squad/config"
	"claude-squad/session"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
)

// keyLog records every key forwarded to a session in
// ~/.claude-squad/keys.log, to diagnose input problems (e.g. a held Space
// that Claude's hold-to-talk takes as released). Typed text is never
// written: letters are logged only as a count; Space, Enter and other
// special keys by name. Each line has the time since the previous key, so
// gaps in a held key are visible. The file is cut back at 1MB.

const keyLogMax = 1 << 20

var keyLog struct {
	sync.Mutex
	last time.Time
}

// inTest is true under `go test`, so tests never write the user's logs.
var inTest = strings.HasSuffix(os.Args[0], ".test")

func logKey(stage, target string, msg tea.KeyMsg) {
	if inTest {
		return
	}
	keyLog.Lock()
	now := time.Now()
	gap := "-"
	if !keyLog.last.IsZero() {
		gap = fmt.Sprintf("+%dms", now.Sub(keyLog.last).Milliseconds())
	}
	keyLog.last = now
	keyLog.Unlock()
	writeLog("keys.log", fmt.Sprintf("%s %-7s %-6s %-14s -> %s\n", now.Format("15:04:05.000"), gap, stage, describeKey(msg), target))
}

// describeKey names a key without revealing typed text.
func describeKey(msg tea.KeyMsg) string {
	if msg.Paste {
		return fmt.Sprintf("paste(%d)", len(msg.Runes))
	}
	if msg.Type == tea.KeyRunes {
		r := string(msg.Runes)
		if strings.Trim(r, " ") == "" {
			return fmt.Sprintf("space×%d", len(msg.Runes))
		}
		if msg.Alt {
			return "alt+text(1)"
		}
		// A single punctuation mark is shown as itself (it helps find which
		// character a shortcut produced); letters and digits stay hidden.
		if len(msg.Runes) == 1 && unicode.IsPunct(msg.Runes[0]) || len(msg.Runes) == 1 && unicode.IsSymbol(msg.Runes[0]) {
			return fmt.Sprintf("char(%q)", string(msg.Runes))
		}
		return fmt.Sprintf("text(%d)", len(msg.Runes))
	}
	if msg.Type == tea.KeySpace {
		return "space"
	}
	return msg.String()
}

// logEvent records what cs did and why in ~/.claude-squad/activity.log
// (view changes, focus changes, auto-jumps, sessions coming and going,
// messages shown), so odd behaviour can be traced afterwards. Same privacy
// rule as the key log: never typed text. Cut back at 1MB.
func logEvent(format string, args ...any) {
	if inTest {
		return
	}
	writeLog("activity.log", time.Now().Format("15:04:05.000")+" "+fmt.Sprintf(format, args...)+"\n")
}

// Logs are written through buffered, kept-open files flushed every 500ms:
// opening and closing a file per key made fast typing and wheel scrolling
// cost a lot of CPU.
var logFiles struct {
	sync.Mutex
	open map[string]*bufferedLog
}

type bufferedLog struct {
	f    *os.File
	w    *bufio.Writer
	size int64
	path string
}

func writeLog(name, line string) {
	logFiles.Lock()
	defer logFiles.Unlock()
	if logFiles.open == nil {
		logFiles.open = map[string]*bufferedLog{}
		go flushLogsForever()
	}
	l := logFiles.open[name]
	if l == nil {
		dir, err := config.GetConfigDir()
		if err != nil {
			return
		}
		path := filepath.Join(dir, name)
		f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
		if err != nil {
			return
		}
		st, _ := f.Stat()
		l = &bufferedLog{f: f, w: bufio.NewWriterSize(f, 32<<10), path: path}
		if st != nil {
			l.size = st.Size()
		}
		logFiles.open[name] = l
	}
	n, _ := l.w.WriteString(line)
	l.size += int64(n)
	if l.size > keyLogMax {
		// Cut back: keep the previous file as .old and start fresh.
		_ = l.w.Flush()
		_ = l.f.Close()
		_ = os.Rename(l.path, l.path+".old")
		delete(logFiles.open, name)
	}
}

// flushLogs writes out buffered log lines now (before a restart).
func flushLogs() {
	logFiles.Lock()
	defer logFiles.Unlock()
	for _, l := range logFiles.open {
		_ = l.w.Flush()
	}
}

func flushLogsForever() {
	for range time.Tick(500 * time.Millisecond) {
		logFiles.Lock()
		for _, l := range logFiles.open {
			_ = l.w.Flush()
		}
		logFiles.Unlock()
	}
}

// stateName describes where keys go right now, for the activity log.
func (m *home) stateName() string {
	var where string
	switch {
	case m.state == stateIssuePicker:
		where = "issue-picker"
	case m.state == stateCommit:
		where = "commit-box"
	case m.state == stateConfirm:
		where = "confirm-dialog"
	case m.state != stateDefault:
		where = fmt.Sprintf("state-%d", m.state)
	case m.sessionFocus != "":
		where = "typing:" + m.sessionFocus
	case m.sourceControl.Focused():
		where = "source-control"
	default:
		where = "list"
		if e := m.list.GetSelectedExternal(); e != nil && e.Kind == session.KindTerminal {
			where = "view-only:" + e.Name
		}
	}
	return fmt.Sprintf("[%s tab=%s sel=%s]", where, filepath.Base(m.projectTabs.Active()), m.selectedRowKey())
}
