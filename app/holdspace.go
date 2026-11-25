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
// stops 200ms after the last. macOS repeats every ~83ms, but a busy Terminal
// delivers a few in a bunch and then none for up to a second (keys.log,
// 2026-11-25): Claude's count started over, and every space was typed. Guessing
// the hold from such repeats doesn't work (tried: it ended holds mid-sentence).
//
// So cs asks macOS whether the key is down (spaceDown). Held long enough, it
// gives Claude the 5 spaces in one piece (it counts them all at once, and takes
// them and the first one out of the prompt) and a steady beat until the key is
// really up.

const (
	// holdAfter: down this long is a hold. Taps measured 90-165ms; the first
	// macOS repeat comes after 375ms or more.
	holdAfter = 300 * time.Millisecond
	// holdPoll is how often the key is looked at.
	holdPoll = 15 * time.Millisecond
	// holdBeat is cs's own repeat, well under Claude's 120ms.
	holdBeat = 50 * time.Millisecond
	// holdLate: repeats the Terminal still delivers after the key is up are
	// dropped for this long; they would be typed as spaces.
	holdLate = 500 * time.Millisecond
	// holdMax ends a hold whose key-up macOS never reported; without it cs
	// would type spaces until the next key.
	holdMax = 5 * time.Minute
	// claudeHold is how many spaces Claude wants before it listens.
	claudeHold = 5
)

// holdIO is what holdSpace needs from outside; tests replace it.
type holdIO struct {
	enabled func(target string) bool
	down    func() bool
	max     time.Duration // 0: holdMax
	send    func(target, spaces string)
	log     func(format string, args ...any)
}

func (io holdIO) maxHold() time.Duration {
	if io.max > 0 {
		return io.max
	}
	return holdMax
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
	down: spaceDown,
	log:  logEvent,
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
	io.send(k.target, " ") // at once, as ever: no delay on normal typing
	same := func(n queuedKey) bool {
		return n.target == k.target && n.wheel == 0 && n.msg.Type == tea.KeySpace && !n.msg.Alt
	}
	poll := time.NewTicker(holdPoll)
	defer poll.Stop()
	// The press may be old by now (a busy cs): count from when it arrived.
	for time.Since(k.at) < holdAfter {
		if !io.down() {
			return nil, true // a tap
		}
		select {
		case n, ok := <-keys:
			if !ok {
				return nil, true
			}
			return &n, true // typing goes on
		case <-poll.C:
		}
	}
	if !io.down() {
		return nil, true
	}
	started := time.Now()
	io.log("space held in %s: telling Claude", k.target)
	io.send(k.target, strings.Repeat(" ", claudeHold))
	letGo := func(why string) {
		io.log("space let go in %s after %s%s", k.target, time.Since(started).Round(10*time.Millisecond), why)
	}
	beat := time.NewTicker(holdBeat)
	defer beat.Stop()
	for up := false; !up; {
		select {
		case n, ok := <-keys:
			if !ok {
				return nil, true
			}
			if !same(n) {
				letGo(" (another key)")
				return &n, true
			}
			continue // a repeat: cs sends its own
		case <-poll.C:
			up = !io.down() || time.Since(started) > io.maxHold()
		case <-beat.C:
			io.send(k.target, " ")
		}
	}
	letGo("")
	late := time.NewTimer(holdLate)
	defer late.Stop()
	for {
		select {
		case n, ok := <-keys:
			if !ok {
				return nil, true
			}
			// Down again: a new press, not a late repeat.
			if !same(n) || io.down() {
				return &n, true
			}
		case <-late.C:
			return nil, true
		}
	}
}
