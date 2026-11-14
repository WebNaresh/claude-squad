package app

import (
	"strings"
	"sync"
)

// View-only tiles (a Claude running in its own terminal window) have no tmux
// pane: cs draws the conversation itself, so the wheel, which scrolls a tmux
// pane's scroll-back, had nothing to scroll and did nothing (#3008's tile,
// 2026-11-14). Each view-only tile keeps how far it is scrolled back
// (home.viewScroll); the wheel moves it and the capture cuts there.

// viewPos is how far a view-only tile is scrolled back from its bottom, and
// how far it could scroll then. New lines arriving while scrolled back raise
// the most; the back grows with it, so the text being read stays put.
type viewPos struct{ back, most int }

// viewMaxBack: how far each view-only tile can scroll back now, by session
// ID. Set by the capture (off the UI thread), read by the wheel.
var viewMaxBack sync.Map

func viewMost(id string) int {
	if v, ok := viewMaxBack.Load(id); ok {
		return v.(int)
	}
	return 0
}

// at is the back to draw when the tile can scroll `most` rows: the stored
// back plus whatever arrived since, kept between the bottom and the top.
func (p viewPos) at(most int) int {
	if p.back <= 0 {
		return 0
	}
	return max(0, min(p.back+most-p.most, most))
}

// scrollViewOnly moves a view-only tile step rows back (negative: forward).
func scrollViewOnly(p viewPos, step int, id string) viewPos {
	most := viewMost(id)
	return viewPos{back: max(0, min(p.at(most)+step, most)), most: most}
}

// viewOnlyWindow cuts a rendered conversation so its last row is the
// scrolled-back row, and returns how many rows back that is. The tile shows
// the last h rows of what comes back.
func viewOnlyWindow(content string, h int, p viewPos, id string) (string, int) {
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	most := max(0, len(lines)-h)
	viewMaxBack.Store(id, most)
	back := p.at(most)
	if back == 0 {
		return content, 0
	}
	return strings.Join(lines[:len(lines)-back], "\n") + "\n", back
}
