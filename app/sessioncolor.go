package app

import (
	"claude-squad/config"
	"claude-squad/session"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
)

// Each issue session gets its own colour in Claude itself (the prompt
// box and name, via Claude's /color) and the same colour on its cs tile, so
// a session is recognised at a glance in both. The background runner types
// "/color <name>" into an issue session once Claude's prompt is up. Claude
// applies it at once even while it works; it is not sent as a message.
// A colour the user set by hand is kept.

// claudeColors are the colours /color accepts, drawn exactly as Claude draws
// them: Claude uses these 256-colour palette entries (read off its screen, 9
// Oct; Terminal.app has no true colour), so a tile's frame is the same shade
// as the session's own prompt box. cs's own hexes were visibly different
// (a sand yellow next to Claude's gold).
var claudeColors = []struct {
	name string
	hex  lipgloss.Color
}{
	{"red", "167"}, {"blue", "110"}, {"green", "35"}, {"yellow", "178"},
	{"purple", "140"}, {"orange", "174"}, {"pink", "175"}, {"cyan", "37"},
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

// colorIssueSessions gives issue sessions their colour, once, at the start.
// The colour is picked the moment the tile appears (gai is still asking its
// questions, Claude isn't running yet) and saved under "tile:<name>", so the
// cs tile is drawn in it from its first second; once Claude's prompt is up,
// "/color <that colour>" makes Claude match. Before, the tile showed a cs
// colour for the first minute and then switched to Claude's (#2372, #2389:
// "why is it changing colour in between"). The pick is one no other session
// of the same project has. typed also holds the colour typed into each
// session (by session ID), "" for one left alone (already working, or
// coloured by the user). A session is coloured once and never changed.
func colorIssueSessions(sessions []*session.ExternalSession, typed map[string]string) {
	colorOf := func(o *session.ExternalSession) string {
		if o.SessionID != "" {
			if c := typed[o.SessionID]; c != "" {
				return c
			}
			if c := session.AgentColor(o.SessionID); c != "" {
				return c
			}
		}
		return typed[tileColorKey(o.Name)]
	}
	pickFor := func(e *session.ExternalSession, n int) string {
		used := map[string]bool{}
		for _, o := range sessions {
			if o != e && o.Path == e.Path {
				used[colorOf(o)] = true
			}
		}
		for i := range claudeColors {
			if c := claudeColors[(n+i)%len(claudeColors)].name; !used[c] {
				return c
			}
		}
		return claudeColors[n%len(claudeColors)].name
	}
	changed := false
	live := map[string]bool{}
	for _, e := range sessions {
		n := session.IssueNumberOf(e.Name)
		if n == 0 || e.Kind != session.KindTmux || e.Live == "" {
			continue
		}
		key := tileColorKey(e.Name)
		live[key] = true
		if e.SessionID == "" {
			// Claude not started yet: choose its colour now, for the tile.
			if typed[key] == "" {
				typed[key] = pickFor(e, n)
				changed = true
				logEvent("daemon: %s: colour %s picked at start", e.Name, typed[key])
			}
			continue
		}
		if _, done := typed[e.SessionID]; done {
			continue
		}
		started := session.SessionStartedAt(e.SessionID, e.Pid)
		if started.IsZero() {
			continue // its ~/.claude/sessions file isn't there yet: look again next pass
		}
		if time.Since(started) > colorStartWindow || session.AgentColor(e.SessionID) != "" {
			typed[e.SessionID] = "" // already working, or coloured by the user: leave it
			changed = true
			continue
		}
		if !promptEmpty(e.Live) { // Claude's prompt box is up and nothing is typed in it
			continue
		}
		pick := typed[key]
		if pick == "" {
			pick = pickFor(e, n)
		}
		typed[e.SessionID], typed[key] = pick, pick
		changed = true
		saveColors(typed)
		_ = session.PressKeys(e.Live, "/color "+pick)
		logEvent("daemon: %s: typed /color %s", e.Name, pick)
		time.Sleep(300 * time.Millisecond)
	}
	// A closed tile's pick goes, so a later session for the same issue
	// gets a colour free at that time.
	for k := range typed {
		if strings.HasPrefix(k, "tile:") && !live[k] {
			delete(typed, k)
			changed = true
		}
	}
	if changed {
		saveColors(typed)
	}
}

// tileColorKey is where the colour picked for a tile (by its tmux name) is
// kept in session-colors.json.
func tileColorKey(name string) string { return "tile:" + name }

// pickedColors is session-colors.json for the window, re-read only when the
// background runner has changed it.
var pickedColors struct {
	at time.Time
	m  map[string]string
}

// pickedTileColor is the colour the background runner chose for a tile
// whose Claude has no /color yet, or "".
func pickedTileColor(name string) string {
	if fi, err := os.Stat(colorsFile()); err == nil && !fi.ModTime().Equal(pickedColors.at) {
		pickedColors.at, pickedColors.m = fi.ModTime(), loadColors()
	}
	return pickedColors.m[tileColorKey(name)]
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
