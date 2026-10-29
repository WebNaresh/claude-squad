package app

import (
	"claude-squad/session"
	"claude-squad/ui"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// Mouse in the grid (mouse capture on, the default): a click focuses the tile
// under it, and a click on an image Claude sent ("[image] /path/x.png") opens
// all the session's images on one page in Chrome, at that image (gallery.go),
// since Terminal.app can't show images or follow the wrapped path itself. A
// click on any other file path opens that file (openpath.go). The wheel
// scrolls the tile under the pointer.

// gridHit is the tile under a screen cell.
type gridHit struct {
	idx        int      // index into gridRows/gridTiles
	ghost      bool     // the placeholder of a just-closed session
	row        int      // content row inside the tile (-1: border or title)
	lines      []string // the tile's content rows as plain text
	x0, y0, w0 int
	textTop    int // screen row of lines[0]
}

// hitGrid finds the tile at screen cell (x, y), reading the tile layout the
// same way RenderGrid draws it.
func (m *home) hitGrid(x, y int) (gridHit, bool) {
	shown, focus := m.shownTiles() // as drawn: with a closed tile's placeholder
	n := len(shown)
	if n == 0 || m.sourceControl.Focused() || m.lastView == "" {
		return gridHit{}, false
	}
	screen := strings.Split(m.lastView, "\n")
	// The grid sits right of the Source Control box, below the tab row and
	// the blank line under it (see view).
	top, gx := 2, m.screenWidth-m.paneWidth
	cols, rows, bodyH := ui.GridLayout(n, m.paneWidth, m.contentHeight)
	tileW, tileH := m.paneWidth/cols, bodyH/rows
	if x < gx || y < top || tileW == 0 || tileH == 0 {
		return gridHit{}, false
	}
	c, r := (x-gx)/tileW, (y-top)/tileH
	if c >= cols || r >= rows {
		return gridHit{}, false
	}
	perPage := cols * rows
	idx := max(0, focus)/perPage*perPage + r*cols + c
	if idx >= n {
		return gridHit{}, false
	}
	ghost := false
	if gi := m.ghostIdx(); gi >= 0 {
		if idx == gi {
			ghost = true
		} else if idx > gi {
			idx-- // back to the index among the grid's sessions
		}
	}
	h := gridHit{idx: idx, x0: gx + c*tileW, y0: top + r*tileH, w0: tileW, ghost: ghost}
	// Content rows sit below the top border (which holds the title), inside
	// the side border and one column of padding.
	first, last := h.y0+1, h.y0+tileH-2
	for sy := first; sy <= last && sy < len(screen); sy++ {
		h.lines = append(h.lines, ansi.Strip(ansi.Cut(screen[sy], h.x0+2, h.x0+tileW-2)))
	}
	h.textTop = first
	h.row = y - first
	if h.row < 0 || h.row >= len(h.lines) {
		h.row = -1
	}
	return h, true
}

// handleGridMouse handles clicks and the wheel over the grid.
func (m *home) handleGridMouse(msg tea.MouseMsg) (tea.Cmd, bool) {
	if m.state != stateDefault {
		return nil, false
	}
	if m.sel.active {
		switch msg.Action {
		case tea.MouseActionMotion:
			return m.moveSelection(msg.X, msg.Y), true
		case tea.MouseActionRelease:
			if m.sel.dragged {
				return m.finishSelection(), true
			}
			// No drag: a click. Open the image or path under it, but only
			// in a tile that already had focus: a click meant to focus a
			// tile (to type) once landed on an image line, opened Chrome and
			// took the keyboard away.
			m.sel.active = false
			if !m.sel.wasFocused {
				return nil, true
			}
			hit := m.sel.pressHit
			if c := commandAt(hit.lines, hit.row, hit.w0-4); c != "" {
				return copyCommand(c), true
			}
			if held := time.Since(m.sel.at); held >= heldTooLong {
				logEvent("press held %s without a drag: not opened as a click", held.Round(100*time.Millisecond))
				return m.handleError(fmt.Errorf("held without moving: nothing selected · a firm press opens macOS Look Up and stops the drag: press lightly, or turn off Trackpad › Look up & data detectors")), true
			}
			return m.clickAt(m.sel.pressHit, m.sel.ext, m.sel.sx), true
		}
	}
	if msg.Action != tea.MouseActionPress {
		return nil, false
	}
	hit, ok := m.hitGrid(msg.X, msg.Y)
	if !ok {
		if msg.Button == tea.MouseButtonLeft {
			logEvent("click %d,%d: no tile there (source control focused=%v, tiles=%d)", msg.X, msg.Y, m.sourceControl.Focused(), len(m.gridTiles))
		}
		return nil, false
	}
	if hit.ghost {
		// No reopen on a click here: right after a close, the next click in
		// that spot is usually meant for the tile moving in, and it reopened
		// the session just closed (#2239). "↺ reopen" in Recently closed does.
		return nil, true
	}
	rows := m.gridRows()
	if hit.idx >= len(rows) {
		return nil, false
	}
	entry := rows[hit.idx].row
	switch msg.Button {
	case tea.MouseButtonWheelUp, tea.MouseButtonWheelDown:
		target := ""
		if entry.Instance != nil {
			target = entry.Instance.TmuxName()
		} else if entry.External != nil {
			target = entry.External.Live
		}
		if target == "" {
			return nil, true
		}
		key := tea.KeyMsg{Type: tea.KeyUp}
		if msg.Button == tea.MouseButtonWheelDown {
			key = tea.KeyMsg{Type: tea.KeyDown}
		}
		select {
		case keyQueue <- queuedKey{target: target, msg: key, at: timeNow(), wheel: 1}:
		default:
		}
		m.lastKey = time.Now() // redraw tiles fast while scrolling (previewInterval)
		// A trackpad sends scroll events in bursts of hundreds; drawing a
		// frame for each kept the UI busy and keys waited (2,000+ in two
		// minutes once). The next preview tick shows the scrolled tile.
		m.skipRender = true
		// Recapture this tile on the next frame even if it isn't focused
		// (marked stale, so the old picture shows until then).
		if key := rowKey(entry.Instance, entry.External); m.tileCache != nil {
			if c, ok := m.tileCache[key]; ok {
				c.at = time.Time{}
				m.tileCache[key] = c
			}
		}
		return nil, true
	case tea.MouseButtonLeft:
	default:
		return nil, false
	}

	wasFocused := rowKey(entry.Instance, entry.External) == m.selectedRowKey()
	logEvent("click %d,%d: tile %d of %d, %s (already selected=%v)", msg.X, msg.Y, hit.idx+1, len(rows), rowKey(entry.Instance, entry.External), wasFocused)
	// A click is input too: without this, a question auto-jumped away 89ms
	// after a click on another tile, so ⌃Space W nearly closed the wrong one.
	m.lastKey, m.lastPick = time.Now(), time.Now()
	// Focus the clicked tile.
	if entry.Instance != nil {
		m.list.SelectInstance(entry.Instance)
	} else if entry.External != nil {
		m.list.SelectExternal(entry.External.Name)
	}
	// Start a selection; letting go without dragging makes it a click
	// (clickAt). Any earlier highlight goes.
	m.sel = tileSelection{}
	if hit.row >= 0 {
		m.startSelection(msg.X, msg.Y, hit)
		m.sel.ext, m.sel.wasFocused = entry.External, wasFocused
		if entry.Instance != nil {
			m.sel.scroll.target = entry.Instance.TmuxName()
		} else if entry.External != nil {
			m.sel.scroll.target = entry.External.Live
		}
	}
	return m.autoFocus(), true
}

// clickAt opens the image or file path under a click in a tile's text.
func (m *home) clickAt(hit gridHit, e *session.ExternalSession, x int) tea.Cmd {
	if e == nil || hit.row < 0 {
		return nil
	}
	// An issue number (Claude's status line shows "#2210", a link in a plain
	// terminal; cs keeps the mouse, so it opens it itself).
	if n := issueAt(hit.lines[hit.row], x-(hit.x0+2)); n > 0 {
		if url := issueURL(e, n); url != "" {
			logEvent("opened issue #%d from %s", n, e.Name)
			return func() tea.Msg { return session.OpenInBrowser(url) }
		}
	}
	// The path under the click wins: an image Claude sent opens on the
	// session's image page, any other file on its own.
	if p := pathAt(hit.lines, hit.row, e.Path); p != "" {
		if isImage(p) && slices.Contains(sentFiles(e), p) {
			return openGallery(e, p)
		}
		// A sent image's line cut off by the tile's edge ("…/d9c6d7c1-…-8c2b2aa13b0")
		// resolves only to a parent folder; Finder opened instead of the image.
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			if f := cutImageAt(hit.lines, hit.row, sentFiles(e)); f != "" {
				logEvent("clicked a cut-off image path in %s: %s", e.Name, filepath.Base(f))
				return openGallery(e, f)
			}
		}
		return openPath(p)
	}
	if file := imageAt(hit.lines, hit.row, e); file != "" {
		return openGallery(e, file)
	}
	return nil
}

func sentFiles(e *session.ExternalSession) []string {
	files, _ := e.SentFiles()
	return files
}

// imageAt returns the sent image shown at content row row, or "". Claude
// draws each sent file as a block: "› /long/path (size)" with "[image]" on
// the next row, the path wrapped over several rows. The block's text is
// matched against the files the session sent, by file name.
func imageAt(lines []string, row int, e *session.ExternalSession) string {
	isStart := func(l string) bool { return strings.HasPrefix(strings.TrimSpace(l), "›") }
	isEnd := func(l string) bool {
		t := strings.TrimSpace(l)
		return t == "" || strings.HasPrefix(t, "●") || strings.HasPrefix(t, "⏺")
	}
	if isEnd(lines[row]) {
		return ""
	}
	start := row
	for start > 0 && row-start < 6 && !isStart(lines[start]) && !isEnd(lines[start-1]) {
		start--
	}
	end := row
	for end+1 < len(lines) && end-row < 6 && !isStart(lines[end+1]) && !isEnd(lines[end+1]) {
		end++
	}
	var b strings.Builder
	for _, l := range lines[start : end+1] {
		b.WriteString(strings.Join(strings.Fields(wrapRow(l)), ""))
	}
	block := b.String()
	if !strings.Contains(block, "[image]") && !strings.Contains(block, "/") {
		return ""
	}
	files, err := e.SentFiles()
	if err != nil {
		return ""
	}
	best := ""
	for _, f := range files { // oldest first: a later file with the same name wins
		if base := filepath.Base(f); strings.Contains(block, base) && len(base) >= len(filepath.Base(best)) {
			best = f
		}
	}
	return best
}

// cutImageAt finds the sent file whose path starts with the text of the
// "›" block at row, when the tile cut that text before the file name. Sent
// files sharing that start (a before and an after in one folder) are told
// apart by the block's place among the consecutive "›" blocks around it:
// Claude lists them in the order they were sent.
func cutImageAt(lines []string, row int, files []string) string {
	isStart := func(l string) bool { return strings.HasPrefix(strings.TrimSpace(l), "›") }
	start := row
	for start > 0 && row-start < 6 && !isStart(lines[start]) {
		start--
	}
	if !isStart(lines[start]) {
		return ""
	}
	text := func(r int) string {
		var b strings.Builder
		for i := r; i < len(lines) && (i == r || !isStart(lines[i])) && strings.TrimSpace(lines[i]) != ""; i++ {
			b.WriteString(wrapRow(lines[i]))
		}
		t := b.String()
		if i := strings.Index(t, "/"); i >= 0 {
			t = t[i:]
		}
		return strings.TrimSpace(t)
	}
	prefix := text(start)
	if len(prefix) < 20 {
		return ""
	}
	var matches []string
	for _, f := range files {
		if strings.HasPrefix(f, prefix) {
			matches = append(matches, f)
		}
	}
	if len(matches) == 0 {
		return ""
	}
	// The consecutive "›" blocks with the same cut text, and which one this is.
	first := start
	for r := start - 1; r >= 0 && r >= start-12; r-- {
		if isStart(lines[r]) {
			if text(r) != prefix {
				break
			}
			first = r
		} else if strings.TrimSpace(lines[r]) == "" {
			break
		}
	}
	idx, n := 0, 0
	for r := first; r < len(lines) && r <= first+24; r++ {
		if isStart(lines[r]) {
			if text(r) != prefix {
				break
			}
			if r == start {
				idx = n
			}
			n++
		} else if strings.TrimSpace(lines[r]) == "" {
			break
		}
	}
	if k := len(matches) - n + idx; k >= 0 && k < len(matches) {
		return matches[k]
	}
	return matches[len(matches)-1]
}
