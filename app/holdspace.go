package app

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Holding Space to talk to Claude.
//
// A terminal never says a key was released; a held key is only its repeats.
// Claude (2.1.332) starts listening after 5 spaces with no gap over 120ms, and
// stops when they end. macOS repeats every ~83ms, so on a busy Mac single
// gaps reached 300-560ms before cs even got the key: Claude's count started
// over and every late space was typed into the prompt (keys.log, 2026-11-25).
//
// So cs reads the hold itself, allowing for late repeats, and then gives Claude
// what it needs: the 5 spaces in one piece (it counts them all at once, and
// takes them out of the prompt again) and a steady beat until the key is let go.

const (
	// holdFirst: the longest wait before a held key's first repeat.
	holdFirst = 900 * time.Millisecond
	// holdDecide: how long repeats are kept back while it isn't clear yet
	// whether Space is held. Then they are typed: the cost for someone typing
	// two spaces is this delay on the second one.
	holdDecide = 200 * time.Millisecond
	// holdFast, holdFastNeeded: a held key always has gaps this short (late
	// repeats arrive in bunches); fingers tapping Space don't, twice in a row.
	holdFast       = 105 * time.Millisecond
	holdFastNeeded = 2
	// holdBeat is cs's own repeat, well under Claude's 120ms.
	holdBeat = 50 * time.Millisecond
	// holdRelease: no repeat for this long means the key was let go.
	holdRelease = 450 * time.Millisecond
	// claudeHold is how many spaces Claude wants before it listens.
	claudeHold = 5
)

// holdIO is what holdSpace needs from outside; tests replace it.
type holdIO struct {
	enabled func(target string) bool
	send    func(target, spaces string)
	log     func(format string, args ...any)
}

var realHold = holdIO{
	enabled: func(target string) bool {
		return strings.HasPrefix(target, "cc_") && voiceHoldOn()
	},
	send: func(target, spaces string) {
		if spaces == "" {
			return
		}
		logKey("hold", target, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(spaces)})
		_ = exec.Command("tmux", "send-keys", "-t", target, "-l", "--", spaces).Run()
	},
	log: logEvent,
}

// voiceHold caches the setting: it is asked for on every space typed.
var voiceHold struct {
	sync.Mutex
	at time.Time
	on bool
}

// voiceHoldOn reads Claude's own setting: hold-to-talk is on.
func voiceHoldOn() bool {
	voiceHold.Lock()
	defer voiceHold.Unlock()
	if time.Since(voiceHold.at) > 5*time.Second {
		voiceHold.at, voiceHold.on = time.Now(), readVoiceHold()
	}
	return voiceHold.on
}

func readVoiceHold() bool {
	dir := os.Getenv("CLAUDE_CONFIG_DIR")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return false
		}
		dir = filepath.Join(home, ".claude")
	}
	data, err := os.ReadFile(filepath.Join(dir, "settings.json"))
	if err != nil {
		return false
	}
	var s struct {
		Voice struct {
			Enabled bool   `json:"enabled"`
			Mode    string `json:"mode"`
		} `json:"voice"`
		VoiceEnabled bool `json:"voiceEnabled"`
	}
	if json.Unmarshal(data, &s) != nil {
		return false
	}
	return (s.Voice.Enabled || s.VoiceEnabled) && (s.Voice.Mode == "" || s.Voice.Mode == "hold")
}

// holdSpace handles a Space press. done is false when it isn't for it (voice
// off, not a Claude tile): the caller sends the key as usual. Otherwise it has
// sent everything that was due, and rest is the next key, if it took one.
func holdSpace(k queuedKey, keys <-chan queuedKey, io holdIO) (rest *queuedKey, done bool) {
	if !io.enabled(k.target) {
		return nil, false
	}
	io.send(k.target, " ") // the first one, as ever: no delay on normal typing
	same := func(n queuedKey) bool {
		return n.target == k.target && n.wheel == 0 && n.msg.Type == tea.KeySpace && !n.msg.Alt
	}
	last, kept, fast := k.at, 0, 0
	wait := holdFirst
	for fast < holdFastNeeded {
		timer := time.NewTimer(wait)
		select {
		case n, ok := <-keys:
			timer.Stop()
			if !ok || !same(n) {
				io.send(k.target, strings.Repeat(" ", kept))
				if !ok {
					return nil, true
				}
				return &n, true
			}
			if kept > 0 && n.at.Sub(last) <= holdFast {
				fast++
			}
			last = n.at
			kept++
			wait = holdDecide
			// Taps: type them. Never 5 at once, which is what starts Claude
			// listening. A hold this late is seen by its next repeats.
			if fast < holdFastNeeded && kept >= claudeHold-2+fast {
				io.send(k.target, strings.Repeat(" ", kept))
				return nil, true
			}
		case <-timer.C:
			io.send(k.target, strings.Repeat(" ", kept))
			return nil, true
		}
	}
	started := time.Now()
	io.log("space held in %s: telling Claude", k.target)
	io.send(k.target, strings.Repeat(" ", claudeHold))
	beat := time.NewTicker(holdBeat)
	defer beat.Stop()
	seen := time.Now() // when the last repeat reached us
	for {
		select {
		case n, ok := <-keys:
			if !ok {
				return nil, true
			}
			if !same(n) {
				io.log("space let go in %s after %s (another key)", k.target, time.Since(started).Round(10*time.Millisecond))
				return &n, true
			}
			seen = time.Now()
		case <-beat.C:
			if time.Since(seen) > holdRelease {
				io.log("space let go in %s after %s", k.target, time.Since(started).Round(10*time.Millisecond))
				return nil, true
			}
			io.send(k.target, " ")
		}
	}
}
