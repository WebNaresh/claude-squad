package app

import (
	"hash/fnv"
	"strings"
	"time"

	"claude-squad/ui"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Closing a tile used to shift every tile after it one place at once, so a
// different session landed exactly where the closed one was and it looked
// as if the wrong one had closed. Two things keep them apart:
//
//   - every session has its own accent colour (title and border), picked
//     from its name, so it looks the same wherever its tile moves;
//   - a closed session leaves a "✕ Closed" placeholder in its place for
//     ghostFor; the tiles close the gap only then. Clicking it reopens it.

const ghostFor = 5 * time.Second

// Clearly different hues, readable on the dark background; none is the
// focus blue.
var accents = []lipgloss.Color{"#e5c07b", "#98c379", "#c678dd", "#56b6c2", "#e06c75", "#d19a66", "#ff79c6", "#8be9fd", "#b5bd68", "#a9a1e1"}

// accentFor is a session's colour: once given, it keeps it for as long as
// cs runs; a new one takes the first colour no other tile on screen has
// (a hash start, so a session tends to get the same one after a restart).
func (m *home) accentFor(key string, onScreen []string) lipgloss.Color {
	if m.accentOf == nil {
		m.accentOf = map[string]int{}
	}
	if i, ok := m.accentOf[key]; ok {
		return accents[i]
	}
	used := map[int]bool{}
	for _, k := range onScreen {
		if i, ok := m.accentOf[k]; ok && k != key {
			used[i] = true
		}
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(key))
	start := int(h.Sum32() % uint32(len(accents)))
	pick := start
	for n := 0; n < len(accents); n++ {
		if c := (start + n) % len(accents); !used[c] {
			pick = c
			break
		}
	}
	m.accentOf[key] = pick
	return accents[pick]
}

// closeGhost is a just-closed tile's place in the grid.
type closeGhost struct {
	project string
	idx     int // its index in the grid when it closed
	title   string
	at      time.Time
	entry   closedEntry // what "click to reopen" resumes
}

// noteGhost keeps the closed tile's place: its index among the grid's tiles
// before the close (gridKeys still lists it).
func (m *home) noteGhost(name, title string, e closedEntry) {
	key := "session:" + name
	for i, k := range strings.Split(strings.TrimSuffix(m.gridKeys, "|"), "|") {
		if k == key {
			m.ghost = &closeGhost{project: m.projectTabs.Active(), idx: i, title: title, at: time.Now(), entry: e}
			return
		}
	}
}

// ghostIdx is where the placeholder sits in the grid now, or -1.
func (m *home) ghostIdx() int {
	g := m.ghost
	if g == nil || time.Since(g.at) > ghostFor || g.project != m.projectTabs.Active() || g.idx > len(m.gridTiles) {
		return -1
	}
	return g.idx
}

// shownTiles are the tiles as drawn: each in its accent colour, with the
// placeholder in a just-closed session's place; and the focus index among
// them.
func (m *home) shownTiles() ([]ui.GridTile, int) {
	keys := strings.Split(strings.TrimSuffix(m.gridKeys, "|"), "|")
	tiles := make([]ui.GridTile, 0, len(m.gridTiles)+1)
	for i, t := range m.gridTiles {
		if i < len(keys) {
			t.Accent = m.accentFor(keys[i], keys)
		}
		tiles = append(tiles, t)
	}
	focus := m.gridFocus
	if gi := m.ghostIdx(); gi >= 0 {
		g := ui.GridTile{Ghost: true, Title: "✕ CLOSED", Status: "click to reopen",
			Content: "\n\n  ✕ Closed: " + m.ghost.title + "\n\n  The tiles move up in a moment.\n  Click here to reopen it."}
		tiles = append(tiles[:gi], append([]ui.GridTile{g}, tiles[gi:]...)...)
		if focus >= gi {
			focus++
		}
	}
	return tiles, focus
}

// clickGhost reopens the just-closed session from its placeholder.
func (m *home) clickGhost() tea.Cmd {
	g := m.ghost
	m.ghost = nil
	if g == nil || g.entry.SessionID == "" {
		return nil
	}
	return m.reopenClosed(g.entry)
}
