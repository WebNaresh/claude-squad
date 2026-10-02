package app

import (
	"claude-squad/session"
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

// Text selection with mouse capture on. Terminal.app's own drag-select needs
// mouse capture off (and then spans every tile on those rows), so cs does it:
// drag inside a tile to highlight text in that tile only; letting go copies
// it to the clipboard. A press without a drag is a click (focus, open image).

// tileSelection is a drag in progress or just finished, in screen cells.
type tileSelection struct {
	active   bool // button held
	dragged  bool // moved since the press: a selection, not a click
	x0, x1   int  // the tile's text columns (inclusive)
	y0, y1   int  // the tile's text rows (inclusive)
	sx, sy   int  // where the drag started
	ex, ey   int  // where it is now
	shown    bool // highlight drawn (until the next press or key)
	pressHit gridHit
	ext      *session.ExternalSession // the pressed tile's session, for clicks
	// wasFocused: the tile had focus before the press; only then does a
	// click open an image or path.
	wasFocused bool
}

// startSelection records a press inside a tile's text.
func (m *home) startSelection(x, y int, hit gridHit) {
	m.sel = tileSelection{active: true, x0: hit.x0 + 1, x1: hit.x0 + hit.w0 - 2,
		y0: hit.textTop, y1: hit.textTop + len(hit.lines) - 1, sx: x, sy: y, ex: x, ey: y, pressHit: hit}
}

// moveSelection extends the drag, kept inside the tile it started in.
func (m *home) moveSelection(x, y int) {
	s := &m.sel
	s.ex, s.ey = min(max(x, s.x0), s.x1), min(max(y, s.y0), s.y1)
	if s.ex != s.sx || s.ey != s.sy {
		s.dragged, s.shown = true, true
	}
}

// ordered returns the selection's start and end in reading order.
func (s tileSelection) ordered() (ax, ay, bx, by int) {
	ax, ay, bx, by = s.sx, s.sy, s.ex, s.ey
	if by < ay || by == ay && bx < ax {
		ax, ay, bx, by = bx, by, ax, ay
	}
	return
}

// span returns the selected columns on screen row y, or ok=false.
func (s tileSelection) span(y int) (from, to int, ok bool) {
	ax, ay, bx, by := s.ordered()
	if y < ay || y > by {
		return 0, 0, false
	}
	from, to = s.x0, s.x1
	if y == ay {
		from = ax
	}
	if y == by {
		to = bx
	}
	return from, to, from <= to
}

// highlightSelection draws the selection in reverse video (written directly,
// so it shows whatever colour support lipgloss detects).
func (m *home) highlightSelection(view string) string {
	if !m.sel.shown {
		return view
	}
	lines := strings.Split(view, "\n")
	for y := range lines {
		from, to, ok := m.sel.span(y)
		if !ok {
			continue
		}
		l := lines[y]
		lines[y] = ansi.Cut(l, 0, from) + "\x1b[7m" + ansi.Strip(ansi.Cut(l, from, to+1)) + "\x1b[27m" + ansi.Cut(l, to+1, ansi.StringWidth(l))
	}
	return strings.Join(lines, "\n")
}

// selectedText returns the selected text from the screen as shown.
func (m *home) selectedText() string {
	screen := strings.Split(m.lastView, "\n")
	var out []string
	for y := range screen {
		if from, to, ok := m.sel.span(y); ok {
			out = append(out, strings.TrimRight(ansi.Strip(ansi.Cut(screen[y], from, to+1)), " "))
		}
	}
	return strings.TrimRight(strings.Join(out, "\n"), "\n")
}

// finishSelection copies a dragged selection to the clipboard.
func (m *home) finishSelection() tea.Cmd {
	m.sel.active = false
	text := m.selectedText()
	if text == "" {
		m.sel.shown = false
		return nil
	}
	n := strings.Count(text, "\n") + 1
	return func() tea.Msg {
		cmd := exec.Command("pbcopy")
		cmd.Stdin = strings.NewReader(text)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("could not copy: %w", err)
		}
		logEvent("copied a %d-line selection", n)
		return fmt.Errorf("copied %d line(s) · ⌘V to paste", n)
	}
}

// mouseRest matches the tail of an SGR mouse report ("<65;345;81M") and
// anything glued after it.
var mouseRest = regexp.MustCompile(`^\[?<\d+;\d+;\d+[Mm]`)

// isMouseFragment reports keys that are really pieces of a mouse report.
// A fast wheel burst can arrive split mid-report; bubbletea then reads its
// start (ESC [) as alt+[ and the rest as typed text, which was typed into
// sessions as "^[[<65;345;81M". Typing Option+[ in Terminal.app doesn't send
// alt+[ (no "Option as Meta"), so dropping it loses nothing.
func isMouseFragment(msg tea.KeyMsg) bool {
	if msg.Alt && msg.Type == tea.KeyRunes && string(msg.Runes) == "[" {
		return true
	}
	return msg.Type == tea.KeyRunes && mouseRest.MatchString(string(msg.Runes))
}
