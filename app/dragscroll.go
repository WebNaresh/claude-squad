package app

import (
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// Drag past a tile's (or the dock's) top or bottom edge and the pane scrolls
// while the selection keeps growing, like a plain terminal. Output taller
// than the tile (a psql table in the dock) could only be copied a screen at
// a time before (2026-10-29).
//
// The copy can't be read back from tmux by row: tiles are captured with -J
// (wrapped rows joined) and topped up from history, so a tile row isn't a
// pane row. Instead every picture seen while scrolling is laid over the
// rows collected so far (doc), matched by their text, and the new rows
// that came into view are added.

// dragScrollEvery is how often the pane scrolls one line while the pointer
// is held past the edge.
const dragScrollEvery = 60 * time.Millisecond

type dragScroll struct {
	target  string   // tmux session to scroll
	dir     int      // -1: pointer above the tile, +1: below, 0: inside
	ticking bool     // a dragScrollMsg is pending
	gen     int      // bumped per selection, so a stale tick stops
	on      bool     // scrolled at least once: the selection lives in doc
	doc     []string // the pane's rows seen so far, top to bottom, plain text
	viewTop int      // doc row shown at the tile's first text row
	anchor  int      // doc row the drag started on
}

type dragScrollMsg struct{ gen int }

var dragGen int

// dragPastEdge notes where the pointer is and starts scrolling when it left
// the tile's text rows.
func (m *home) dragPastEdge(y int) tea.Cmd {
	s := &m.sel.scroll
	switch {
	case y < m.sel.y0:
		s.dir = -1
	case y > m.sel.y1:
		s.dir = 1
	default:
		s.dir = 0
	}
	if s.dir == 0 || s.ticking || s.target == "" {
		return nil
	}
	if s.gen == 0 {
		dragGen++
		s.gen = dragGen
	}
	s.ticking = true
	return dragTick(s.gen)
}

func dragTick(gen int) tea.Cmd {
	return tea.Tick(dragScrollEvery, func(time.Time) tea.Msg { return dragScrollMsg{gen: gen} })
}

// onDragScroll scrolls the pane one line and grows the selection with it.
func (m *home) onDragScroll(msg dragScrollMsg) tea.Cmd {
	s := &m.sel.scroll
	if msg.gen != s.gen {
		return nil // an earlier drag's tick: this one has its own
	}
	if !m.sel.active || s.dir == 0 {
		s.ticking = false
		return nil
	}
	m.syncDragDoc()
	if !s.on {
		s.on = true
		logEvent("drag past the edge: scrolling %s", s.target)
	}
	key := tea.KeyMsg{Type: tea.KeyUp}
	if s.dir > 0 {
		key = tea.KeyMsg{Type: tea.KeyDown}
	}
	select {
	case keyQueue <- queuedKey{target: s.target, msg: key, at: timeNow(), wheel: 1}:
	default:
	}
	// The dragged tile has focus, so it is recaptured every frame anyway.
	m.lastKey = timeNow()
	return dragTick(msg.gen)
}

// viewRows returns the selection's pane rows as shown now, plain text.
func (m *home) viewRows() []string {
	screen := strings.Split(m.lastView, "\n")
	paneL, paneR := m.sel.x0+1, m.sel.x1-1
	var rows []string
	for y := m.sel.y0; y <= m.sel.y1; y++ {
		r := ""
		if y >= 0 && y < len(screen) {
			r = strings.TrimRight(ansi.Strip(ansi.Cut(screen[y], paneL, paneR+1)), " ")
		}
		rows = append(rows, r)
	}
	return rows
}

// syncDragDoc lays the tile's current picture over the rows collected so
// far and keeps the drag's start on its text as the view moves.
func (m *home) syncDragDoc() {
	s := &m.sel.scroll
	rows := m.viewRows()
	if s.doc == nil {
		s.doc, s.viewTop, s.anchor = rows, 0, m.sel.sy-m.sel.y0
		return
	}
	o, ok := overlayAt(s.doc, rows, s.viewTop)
	if !ok {
		return // mid-redraw or changed under us: try again next tick
	}
	if o < 0 {
		s.doc = append(append([]string{}, rows[:-o]...), s.doc...)
		s.anchor -= o
		o = 0
	}
	for i, r := range rows {
		if o+i < len(s.doc) {
			s.doc[o+i] = r
		} else {
			s.doc = append(s.doc, r)
		}
	}
	s.viewTop = o
	// The start moves with its text; off screen it is a row above or below.
	m.sel.sy = m.sel.y0 + s.anchor - s.viewTop
}

// overlayAt finds where rows sit in doc (o: doc row of rows[0], negative
// when rows start above doc), nearest to near, with every overlapping row
// equal and enough of them to be sure.
func overlayAt(doc, rows []string, near int) (int, bool) {
	h := len(rows)
	need := max(3, h/3)
	for d := 0; d <= h; d++ {
		for _, o := range []int{near - d, near + d} {
			if d == 0 && o != near {
				continue
			}
			n, same := 0, true
			for i := range rows {
				j := o + i
				if j < 0 || j >= len(doc) {
					continue
				}
				if doc[j] != rows[i] {
					same = false
					break
				}
				n++
			}
			if same && n >= need {
				return o, true
			}
		}
	}
	return 0, false
}

// docScreen turns the collected rows into a screen and the selection into
// rows of it, so the usual copy (selectedTextFrom) reads them.
func (s tileSelection) docScreen() ([]string, tileSelection) {
	pad := strings.Repeat(" ", s.x0+1)
	screen := make([]string, len(s.scroll.doc))
	for i, r := range s.scroll.doc {
		screen[i] = pad + r
	}
	t := s
	t.sy = s.scroll.anchor
	t.ey = s.scroll.viewTop + s.ey - s.y0
	t.y0, t.y1 = 0, len(screen)-1
	return screen, t
}
