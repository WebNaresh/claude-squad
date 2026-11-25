package app

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// feed plays key gaps (ms) into holdSpace and returns what was sent, in order,
// and the key handed back. Gaps are real time, so the cases stay short.
func feed(t *testing.T, gaps []int, then string) (sent []string, rest *queuedKey) {
	t.Helper()
	keys := make(chan queuedKey, 100)
	space := func() queuedKey {
		return queuedKey{target: "cc_x", msg: tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}, at: time.Now()}
	}
	first := space()
	go func() {
		for _, g := range gaps {
			time.Sleep(time.Duration(g) * time.Millisecond)
			keys <- space()
		}
		if then != "" {
			time.Sleep(30 * time.Millisecond)
			keys <- queuedKey{target: "cc_x", msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(then)}, at: time.Now()}
		}
	}()
	io := holdIO{
		enabled: func(string) bool { return true },
		send: func(_, s string) {
			if s != "" {
				sent = append(sent, s)
			}
		},
		log: func(string, ...any) {},
	}
	rest, done := holdSpace(first, keys, io)
	if !done {
		t.Fatal("not handled")
	}
	return sent, rest
}

func held(sent []string) bool {
	for _, s := range sent {
		if len(s) == claudeHold {
			return true
		}
	}
	return false
}

// The recorded gaps of holds that typed spaces (keys.log, 2026-11-25).
func TestHoldSpaceLateRepeats(t *testing.T) {
	for _, gaps := range [][]int{
		{500, 83, 83, 83},           // a calm Mac
		{444, 76, 97, 76, 360, 309}, // late repeats after the start
		{309, 33, 75, 82},
		{48, 22, 20, 300, 195},
	} {
		sent, _ := feed(t, gaps, "")
		if !held(sent) {
			t.Errorf("%v: not seen as held: %q", gaps, sent)
			continue
		}
		// Before the 5 in one piece only the first space: nothing typed late.
		if sent[0] != " " || len(sent[1]) != claudeHold {
			t.Errorf("%v: sent %q first", gaps, sent[:2])
		}
	}
}

// Through a gap of 360ms Claude gets a beat all along, so it keeps listening.
func TestHoldSpaceBridgesGaps(t *testing.T) {
	sent, _ := feed(t, []int{300, 80, 80, 360, 360}, "")
	beats := 0
	for _, s := range sent[2:] {
		if s == " " {
			beats++
		}
	}
	if beats < 15 {
		t.Fatalf("only %d beats over 720ms + release", beats)
	}
}

// Taps are typed, exactly as many as pressed, and never start listening.
func TestHoldSpaceTapsAreTyped(t *testing.T) {
	for _, gaps := range [][]int{{}, {150}, {160, 150}, {140, 130, 150}, {140, 90, 150, 140}} {
		sent, _ := feed(t, gaps, "")
		typed := len(strings.Join(sent, ""))
		// A run that ended early leaves the rest in the queue for the next call.
		if held(sent) || typed < 1 || typed > len(gaps)+1 || len(gaps) < 4 && typed != len(gaps)+1 {
			t.Errorf("%v: sent %q", gaps, sent)
		}
		for _, s := range sent {
			if len(s) >= claudeHold {
				t.Errorf("%v: %d spaces at once would start Claude listening", gaps, len(s))
			}
		}
	}
}

// A letter after a space is not delayed, and comes back to the caller.
func TestHoldSpaceThenLetter(t *testing.T) {
	start := time.Now()
	sent, rest := feed(t, nil, "a")
	if rest == nil || string(rest.msg.Runes) != "a" || len(sent) != 1 {
		t.Fatalf("sent %q, rest %v", sent, rest)
	}
	if d := time.Since(start); d > 150*time.Millisecond {
		t.Fatalf("took %s", d)
	}
	sent, rest = feed(t, []int{150}, "b") // "  b"
	if rest == nil || strings.Join(sent, "") != "  " {
		t.Fatalf("sent %q, rest %v", sent, rest)
	}
	// Enter right after the hold stops it.
	sent, rest = feed(t, []int{300, 80, 80, 80}, "x")
	if !held(sent) || rest == nil {
		t.Fatalf("sent %q, rest %v", sent, rest)
	}
}

func TestHoldSpaceOffWhenVoiceOff(t *testing.T) {
	k := queuedKey{target: "cc_x", msg: tea.KeyMsg{Type: tea.KeySpace}}
	if _, done := holdSpace(k, nil, holdIO{enabled: func(string) bool { return false }}); done {
		t.Fatal("handled a space with voice off")
	}
	if realHold.enabled("csterm_x") {
		t.Fatal("on in a terminal tile")
	}
}
