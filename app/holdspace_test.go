package app

import (
	"strings"
	"sync"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// hold plays a Space press: the key is physically down for downFor; spaces
// arrive at the given times (ms after the press), then another key if any.
func hold(t *testing.T, downFor time.Duration, arrive []int, other string, otherAt int) (sent []string, at []time.Duration, rest *queuedKey) {
	t.Helper()
	keys := make(chan queuedKey, 100)
	start := time.Now()
	for _, ms := range arrive {
		time.AfterFunc(time.Duration(ms)*time.Millisecond, func() {
			keys <- queuedKey{target: "cc_x", msg: tea.KeyMsg{Type: tea.KeySpace}, at: time.Now()}
		})
	}
	if other != "" {
		time.AfterFunc(time.Duration(otherAt)*time.Millisecond, func() {
			keys <- queuedKey{target: "cc_x", msg: tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(other)}, at: time.Now()}
		})
	}
	var mu sync.Mutex
	rest, done := holdSpace(queuedKey{target: "cc_x", msg: tea.KeyMsg{Type: tea.KeySpace}, at: start}, keys, holdIO{
		enabled: func(string) bool { return true },
		down:    func() bool { return time.Since(start) < downFor },
		send: func(_, s string) {
			mu.Lock()
			defer mu.Unlock()
			sent, at = append(sent, s), append(at, time.Since(start))
		},
		log: func(string, ...any) {},
	})
	if !done {
		t.Fatal("not handled")
	}
	return sent, at, rest
}

// Held 2s while the Terminal delivers what it did on 2026-11-25 (bunches, then
// up to 970ms of nothing), or nothing at all: Claude gets 5 at once, then never
// a gap over its 120ms until the key is up, and nothing after.
func TestHoldSpaceWhateverArrives(t *testing.T) {
	for _, arrive := range [][]int{
		{500, 583, 666, 749, 832, 915},          // a calm Mac
		{500, 514, 525, 1230, 1235, 2150, 2300}, // bunches; two late after release
		{},
	} {
		sent, at, rest := hold(t, 2*time.Second, arrive, "", 0)
		if rest != nil || sent[0] != " " || len(sent[1]) != claudeHold {
			t.Fatalf("%v: rest %v, sent %q…", arrive, rest, sent[:2])
		}
		for i := 2; i < len(sent); i++ {
			if gap := at[i] - at[i-1]; sent[i] != " " || gap > 120*time.Millisecond {
				t.Errorf("%v: %q after %s", arrive, sent[i], gap)
			}
		}
		if last := at[len(at)-1]; last < 1850*time.Millisecond || last > 2100*time.Millisecond {
			t.Errorf("%v: last beat at %s, key up at 2s", arrive, last)
		}
	}
}

// A tap is one space, at once, with no waiting around.
func TestHoldSpaceTap(t *testing.T) {
	for _, d := range []time.Duration{0, 90 * time.Millisecond, 165 * time.Millisecond} {
		start := time.Now()
		sent, at, rest := hold(t, d, nil, "", 0)
		if strings.Join(sent, "") != " " || rest != nil || at[0] > 20*time.Millisecond {
			t.Fatalf("%s: sent %q", d, sent)
		}
		if took := time.Since(start); took > d+80*time.Millisecond {
			t.Fatalf("%s: took %s", d, took)
		}
	}
}

// The next letter (Space still down when fast typing) or second tap is handed
// back at once.
func TestHoldSpaceTypingGoesOn(t *testing.T) {
	sent, _, rest := hold(t, 150*time.Millisecond, nil, "a", 60)
	if strings.Join(sent, "") != " " || rest == nil || string(rest.msg.Runes) != "a" {
		t.Fatalf("sent %q, rest %v", sent, rest)
	}
	sent, _, rest = hold(t, 150*time.Millisecond, []int{60}, "", 0)
	if strings.Join(sent, "") != " " || rest == nil || rest.msg.Type != tea.KeySpace {
		t.Fatalf("second space: sent %q, rest %v", sent, rest)
	}
}

// Another key ends the hold and is handed back.
func TestHoldSpaceOtherKeyEndsIt(t *testing.T) {
	sent, _, rest := hold(t, 3*time.Second, nil, "x", 800)
	if rest == nil || string(rest.msg.Runes) != "x" || len(sent) < 5 || len(sent[1]) != claudeHold {
		t.Fatalf("sent %q, rest %v", sent, rest)
	}
}

// A press that waited in a busy cs: the key is up by now, so it was a tap; or
// still down, so it is a hold straight away.
func TestHoldSpaceOldPress(t *testing.T) {
	for _, down := range []bool{false, true} {
		var sent []string
		until := time.Now().Add(200 * time.Millisecond)
		holdSpace(queuedKey{target: "cc_x", msg: tea.KeyMsg{Type: tea.KeySpace}, at: time.Now().Add(-time.Second)}, make(chan queuedKey), holdIO{
			enabled: func(string) bool { return true },
			down:    func() bool { return down && time.Now().Before(until) },
			send:    func(_, s string) { sent = append(sent, s) },
			log:     func(string, ...any) {},
		})
		if isHold := len(sent) > 1 && len(sent[1]) == claudeHold; isHold != down {
			t.Fatalf("down=%v: sent %q", down, sent)
		}
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

// A key-up that macOS never reports doesn't type spaces for ever.
func TestHoldSpaceStuckKey(t *testing.T) {
	var sent []string
	start := time.Now()
	holdSpace(queuedKey{target: "cc_x", msg: tea.KeyMsg{Type: tea.KeySpace}, at: start}, make(chan queuedKey), holdIO{
		enabled: func(string) bool { return true },
		down:    func() bool { return true },
		max:     300 * time.Millisecond,
		send:    func(_, s string) { sent = append(sent, s) },
		log:     func(string, ...any) {},
	})
	if took := time.Since(start); took > 2*time.Second || len(sent) > 12 {
		t.Fatalf("took %s, sent %d", took, len(sent))
	}
}
