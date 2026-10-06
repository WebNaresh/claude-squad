package app

import (
	"claude-squad/config"
	"claude-squad/session"
	"encoding/json"
	"os"
	"path/filepath"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// Each issue session gets its own colour in Claude itself (the prompt
// box and name, via Claude's /color) and the same colour on its cs tile, so
// a session is recognised at a glance in both. The background runner types
// "/color <name>" into an issue session once Claude's prompt is up. Claude
// applies it at once even while it works; it is not sent as a message.
// A colour the user set by hand is kept.

// claudeColors are the colours /color accepts, with how cs draws each.
var claudeColors = []struct {
	name string
	hex  lipgloss.Color
}{
	{"red", "#e06c75"}, {"blue", "#61afef"}, {"green", "#98c379"}, {"yellow", "#e5c07b"},
	{"purple", "#c678dd"}, {"orange", "#d19a66"}, {"pink", "#ff79c6"}, {"cyan", "#56b6c2"},
}

// claudeColorHex is how cs draws a /color name, or "".
func claudeColorHex(name string) lipgloss.Color {
	for _, c := range claudeColors {
		if c.name == name {
			return c.hex
		}
	}
	return ""
}

// colorStartWindow is how soon after Claude starts a session may be
// coloured: only at its very start, never in the middle of the work.
const colorStartWindow = 3 * time.Minute

// colorIssueSessions types /color into issue sessions that just started,
// picking a colour no other session of the same project has. typed holds
// the colour cs gave each session (by session ID), counted as taken at
// once: Claude saves a /color a moment later, and picks in one pass once
// gave two sessions the same colour. A session is coloured once and never
// changed; one the user coloured is left alone.
func colorIssueSessions(sessions []*session.ExternalSession, typed map[string]string) {
	colorOf := func(o *session.ExternalSession) string {
		if c := typed[o.SessionID]; c != "" {
			return c
		}
		return session.AgentColor(o.SessionID)
	}
	for _, e := range sessions {
		n := session.IssueNumberOf(e.Name)
		if n == 0 || e.Kind != session.KindTmux || e.Live == "" || e.SessionID == "" {
			continue
		}
		if _, done := typed[e.SessionID]; done {
			continue
		}
		started := session.StartedAt(e.Pid)
		if started.IsZero() || time.Since(started) > colorStartWindow || session.AgentColor(e.SessionID) != "" {
			typed[e.SessionID] = "" // already working, or coloured by the user: leave it
			saveColors(typed)
			continue
		}
		if !promptEmpty(e.Live) { // Claude's prompt box is up and nothing is typed in it
			continue
		}
		used := map[string]bool{}
		for _, o := range sessions {
			if o != e && o.Path == e.Path && o.SessionID != "" {
				used[colorOf(o)] = true
			}
		}
		pick := claudeColors[n%len(claudeColors)].name
		for i := range claudeColors {
			if c := claudeColors[(n+i)%len(claudeColors)].name; !used[c] {
				pick = c
				break
			}
		}
		typed[e.SessionID] = pick
		saveColors(typed)
		_ = session.PressKeys(e.Live, "/color "+pick)
		logEvent("daemon: %s: typed /color %s", e.Name, pick)
		time.Sleep(300 * time.Millisecond)
	}
}

// colorsFile keeps the colours cs gave, so after a restart cs still knows
// which colours it chose (and may fix) and which the user chose (kept).
func colorsFile() string {
	dir, err := config.GetConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "session-colors.json")
}

func loadColors() map[string]string {
	m := map[string]string{}
	if data, err := os.ReadFile(colorsFile()); err == nil {
		_ = json.Unmarshal(data, &m)
	}
	return m
}

func saveColors(m map[string]string) {
	if f := colorsFile(); f != "" {
		if data, err := json.Marshal(m); err == nil {
			_ = os.WriteFile(f, data, 0o644)
		}
	}
}
