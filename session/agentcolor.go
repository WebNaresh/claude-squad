package session

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// A session's colour, set with Claude's /color (by the user or by cs), is
// saved in its transcript as {"type":"agent-color","agentColor":"red",…}.
// AgentColor reads it back so cs can draw the tile in the same colour.

var (
	agentColorRe = regexp.MustCompile(`"type":"agent-color","agentColor":"([a-z]+)"`)
	// Each of Claude's replies names its model: "model":"claude-opus-5-5".
	agentModelRe = regexp.MustCompile(`"model":"(claude-[a-z0-9.\-]+)"`)
)

type agentColorEntry struct {
	size    int64 // transcript bytes already read
	color   string
	model   string // the latest reply's model, e.g. claude-sonnet-5-5
	checked time.Time
}

var (
	agentColorMu    sync.Mutex
	agentColorCache = map[string]agentColorEntry{}
)

// AgentColor returns the colour last set with /color in a session, or "".
// Transcripts reach 40MB+: only the bytes added since the last call are
// read, and a file is looked at once every 2s at most.
func AgentColor(sessionID string) string {
	return readTranscriptTail(sessionID).color
}

// readTranscriptTail reads what was added to a session's transcript since
// the last call (at most every 2s) and returns the colour and model found.
func readTranscriptTail(sessionID string) agentColorEntry {
	if sessionID == "" {
		return agentColorEntry{}
	}
	agentColorMu.Lock()
	c := agentColorCache[sessionID]
	agentColorMu.Unlock()
	if time.Since(c.checked) < 2*time.Second {
		return c
	}
	c.checked = time.Now()
	if p := transcriptPath(sessionID); p != "" {
		if f, err := os.Open(p); err == nil {
			if st, err := f.Stat(); err == nil && st.Size() != c.size {
				if st.Size() < c.size {
					c.size = 0 // rewritten: read it again
				}
				if _, err := f.Seek(c.size, io.SeekStart); err == nil {
					sc := bufio.NewScanner(f)
					sc.Buffer(make([]byte, 1<<20), 64<<20)
					for sc.Scan() {
						if m := agentColorRe.FindSubmatch(sc.Bytes()); m != nil {
							c.color = string(m[1])
						}
						// A subagent's replies (sidechain) name its own model.
						if m := agentModelRe.FindSubmatch(sc.Bytes()); m != nil && !bytes.Contains(sc.Bytes(), []byte(`"isSidechain":true`)) {
							c.model = string(m[1])
						}
					}
				}
				c.size = st.Size()
			}
			f.Close()
		}
	}
	agentColorMu.Lock()
	agentColorCache[sessionID] = c
	agentColorMu.Unlock()
	return c
}

var (
	modelArgRe    = regexp.MustCompile(`--model[= ]+'?"?([^\s'"]+)`)
	modelArgMu    sync.Mutex
	modelArgCache = map[int]string{}
	sessionPid    = map[string]int{}
)

// AgentModel is the model a session runs, short: "sonnet", "opus", or
// "opus[1m]" when Claude was started with --model …[1m]. From the latest
// reply in its transcript; before the first reply, from the --model it was
// started with. "" when neither says.
func AgentModel(sessionID string, pid int) string {
	arg := ""
	if pid <= 0 {
		modelArgMu.Lock()
		p, ok := sessionPid[sessionID]
		modelArgMu.Unlock()
		if !ok {
			p = pidOfSession(sessionID) // reads ~/.claude/sessions once per session
			if p > 0 {                  // a new session's file may not be there yet: look again later
				modelArgMu.Lock()
				sessionPid[sessionID] = p
				modelArgMu.Unlock()
			}
		}
		pid = p
	}
	if pid > 0 {
		modelArgMu.Lock()
		a, ok := modelArgCache[pid]
		modelArgMu.Unlock()
		if !ok {
			out, _ := ProcOutput("ps", "-o", "args=", "-p", strconv.Itoa(pid))
			if m := modelArgRe.FindStringSubmatch(string(out)); m != nil {
				a = m[1]
			}
			modelArgMu.Lock()
			modelArgCache[pid] = a
			modelArgMu.Unlock()
		}
		arg = a
	}
	name := shortModel(readTranscriptTail(sessionID).model)
	if name == "" {
		name = shortModel(arg)
	}
	if name != "" && strings.Contains(arg, "[1m]") && !strings.Contains(name, "[1m]") {
		name += "[1m]"
	}
	return name
}

// shortModel turns "claude-sonnet-5-5" or "sonnet" into "sonnet".
func shortModel(m string) string {
	m = strings.TrimPrefix(strings.ToLower(m), "claude-")
	for _, f := range []string{"opus", "sonnet", "haiku", "fable"} {
		if strings.HasPrefix(m, f) {
			if strings.Contains(m, "[1m]") {
				return f + "[1m]"
			}
			return f
		}
	}
	return ""
}

// SessionStartedAt is when the Claude process running a session started
// (zero if unknown yet). pid may be 0: tmux tiles don't record it, so it is
// looked up by session ID (a zero pid once made every new session look "not
// new" and no issue session got its /color).
func SessionStartedAt(sessionID string, pid int) time.Time {
	if pid <= 0 {
		pid = pidOfSession(sessionID)
	}
	return StartedAt(pid)
}

// StartedAt is when the Claude process with this pid started, from
// ~/.claude/sessions/<pid>.json (zero if unknown).
func StartedAt(pid int) time.Time {
	if pid <= 0 {
		return time.Time{}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return time.Time{}
	}
	data, err := os.ReadFile(filepath.Join(home, ".claude", "sessions", strconv.Itoa(pid)+".json"))
	if err != nil {
		return time.Time{}
	}
	var s struct {
		StartedAt int64 `json:"startedAt"`
	}
	if json.Unmarshal(data, &s) != nil || s.StartedAt == 0 {
		return time.Time{}
	}
	return time.UnixMilli(s.StartedAt)
}

// pidOfSession finds the Claude process running a session, from
// ~/.claude/sessions/<pid>.json (tmux tiles don't record it).
func pidOfSession(sessionID string) int {
	if sessionID == "" {
		return 0
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return 0
	}
	files, _ := filepath.Glob(filepath.Join(home, ".claude", "sessions", "*.json"))
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil || !strings.Contains(string(data), sessionID) {
			continue
		}
		var s struct {
			Pid       int    `json:"pid"`
			SessionID string `json:"sessionId"`
		}
		if json.Unmarshal(data, &s) == nil && s.SessionID == sessionID {
			return s.Pid
		}
	}
	return 0
}
