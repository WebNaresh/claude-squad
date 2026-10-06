package session

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"time"
)

// A session's colour, set with Claude's /color (by the user or by cs), is
// saved in its transcript as {"type":"agent-color","agentColor":"red",…}.
// AgentColor reads it back so cs can draw the tile in the same colour.

var agentColorRe = regexp.MustCompile(`"type":"agent-color","agentColor":"([a-z]+)"`)

type agentColorEntry struct {
	size    int64 // transcript bytes already read
	color   string
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
	if sessionID == "" {
		return ""
	}
	agentColorMu.Lock()
	c := agentColorCache[sessionID]
	agentColorMu.Unlock()
	if time.Since(c.checked) < 2*time.Second {
		return c.color
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
	return c.color
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
