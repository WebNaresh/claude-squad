package app

import (
	"claude-squad/session"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"

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
	at         time.Time // when the button went down
	scroll     dragScroll
}

// heldTooLong: a press held this long without moving is not a click. On a
// Mac with Force Click on, a firm press opens Look Up (seen: its "No
// Results Found" popup over the path) and the terminal stops sending the drag; letting go then opened the file instead
// of copying the command the user was selecting (2026-10-28).
const heldTooLong = 700 * time.Millisecond

// startSelection records a press inside a tile's text.
func (m *home) startSelection(x, y int, hit gridHit) {
	m.sel = tileSelection{active: true, x0: hit.x0 + 1, x1: hit.x0 + hit.w0 - 2,
		y0: hit.textTop, y1: hit.textTop + len(hit.lines) - 1, sx: x, sy: y, ex: x, ey: y, pressHit: hit, at: time.Now()}
}

// moveSelection extends the drag, kept inside the tile it started in. Past
// the tile's top or bottom edge the pane scrolls (dragscroll.go).
func (m *home) moveSelection(x, y int) tea.Cmd {
	s := &m.sel
	s.ex, s.ey = min(max(x, s.x0), s.x1), min(max(y, s.y0), s.y1)
	// Above the tile the selection runs to the row's start, below it to the
	// row's end, as in a plain terminal.
	if y < s.y0 {
		s.ex = s.x0
	} else if y > s.y1 {
		s.ex = s.x1
	}
	if s.ex != s.sx || s.ey != s.sy {
		s.dragged, s.shown = true, true
	}
	return m.dragPastEdge(y)
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

// selectedText returns the selected text from the screen as shown, with
// Claude's line wrapping undone (joinWrapped), so a long path or command
// comes out as one line, not several with the indent in between.
func (m *home) selectedText() string {
	if m.sel.scroll.on {
		// Dragged past the edge: the selection runs over rows scrolled out
		// of view, kept in scroll.doc (dragscroll.go).
		m.syncDragDoc()
		screen, sel := m.sel.docScreen()
		return selectedTextFrom(screen, sel)
	}
	return selectedTextFrom(strings.Split(m.lastView, "\n"), m.sel)
}

func selectedTextFrom(screen []string, sel tileSelection) string {
	// The pane sits inside the tile's text columns, one padding column on
	// each side.
	paneL, paneR := sel.x0+1, sel.x1-1
	var rows []selRow
	for y := range screen {
		from, to, ok := sel.span(y)
		if !ok {
			continue
		}
		line, bar := unquote(strings.TrimRight(ansi.Strip(ansi.Cut(screen[y], paneL, paneR+1)), " "))
		text, _ := unquote(strings.TrimRight(ansi.Strip(ansi.Cut(screen[y], from, to+1)), " "))
		raw := strings.TrimRight(ansi.Strip(ansi.Cut(screen[y], paneL, paneR+1)), " ")
		rows = append(rows, selRow{text: text, line: line, end: to >= paneR, skip: paneL - from, bar: bar,
			raw: raw, c0: max(from-paneL, 0), c1: to - paneL})
	}
	if text, ok := tmuxLines(sel.scroll.target, rows); ok {
		return text
	}
	return joinWrapped(rows, paneR-paneL+1)
}

// selRow is one selected screen row: the selected text, the whole pane row,
// and whether the selection runs to the row's end.
type selRow struct {
	text, line string
	end        bool
	skip       int    // columns of text before the pane's first column (padding); negative when the drag started inside the row
	bar        int    // columns of a quote bar unquote took out of line
	raw        string // the pane row as shown, nothing taken out
	c0, c1     int    // selected pane columns, inclusive
}

// unquote takes the bar Claude draws in front of each row of a quote ("> "
// in its markdown) out of a row: a reply drafted as a quote pasted into
// WhatsApp with "▎" on every line and mid-sentence where a row wrapped.
// It returns the row and how many columns went.
func unquote(row string) (string, int) {
	rest := strings.TrimLeft(row, " ")
	after, ok := strings.CutPrefix(rest, "▎")
	if !ok {
		return row, 0
	}
	n := 1
	if a, ok := strings.CutPrefix(after, " "); ok {
		after, n = a, 2
	}
	return row[:len(row)-len(rest)] + after, n
}

// listStart matches a row that starts a new item rather than continuing the
// one above: Claude's step and result marks, bullets, numbered items, the
// prompt.
var listStart = regexp.MustCompile(`^(?:[⏺●⎿❯✻│─╭╰>#]|[-*•] |\d+[.)] )`)

// boxRow matches a row drawn with box characters: a rule, a table row.
var boxRow = regexp.MustCompile(`^[─│├┼┤╭╰┌└━┃]`)

// wraps reports whether Claude wrapped row a onto row b in a pane w columns
// wide: a fills the pane (a word cut mid-way), or b's first word would not
// have fit after a, so Claude moved it down (a command ending in ">" with
// the long path on the next row).
func wraps(a, b selRow, w int) bool {
	if !a.end || strings.TrimSpace(a.line) == "" || strings.TrimSpace(b.line) == "" {
		return false
	}
	// Box and table rows fill the pane but are drawn, not wrapped.
	next := strings.TrimLeft(b.line, " ")
	if boxRow.MatchString(strings.TrimLeft(a.line, " ")) || listStart.MatchString(next) {
		return false
	}
	aw := ansi.StringWidth(a.line) + a.bar
	if aw >= w {
		return true
	}
	first, _, _ := strings.Cut(next, " ")
	// A word longer than a whole row (b is one cut piece of it) starts on
	// the row before when there's room, so on a row of its own it began
	// a new line ("The local file is:" then the path).
	if first == next && ansi.StringWidth(b.line)+b.bar >= w {
		return false
	}
	return aw+1+ansi.StringWidth(first) > w
}

// inCommand: b goes on a "! …" command line (one shell line) that a wraps
// to. Claude moves a long path down to a row of its own, which elsewhere
// starts a new line; in a command that split it and zsh ran the path alone.
func inCommand(line string, a, b selRow) bool {
	next := strings.TrimSpace(b.line)
	return a.end && next != "" && !listStart.MatchString(next) && isCommandStart(strings.TrimSpace(line))
}

// joinWrapped joins wrapped rows and drops the indent all lines share.
// A word or path too long for the row was cut mid-word, so it is joined
// without a space; a row wrapped at a space gets the space back.
func joinWrapped(rows []selRow, w int) string {
	var lines []string
	var indents, skips []int // each line's indent on screen (whole first row) and its first row's skip
	for i, r := range rows {
		if i > 0 && len(lines) > 0 && (wraps(rows[i-1], r, w) || inCommand(lines[len(lines)-1], rows[i-1], r)) {
			prev := strings.TrimSpace(rows[i-1].text)
			sep := " "
			if ansi.StringWidth(rows[i-1].line)+rows[i-1].bar >= w {
				if last := prev[strings.LastIndex(prev, " ")+1:]; last == prev || strings.Contains(last, "/") {
					sep = ""
				}
			}
			lines[len(lines)-1] += sep + strings.TrimLeft(r.text, " ")
			continue
		}
		lines = append(lines, r.text)
		indents = append(indents, len(r.line)-len(strings.TrimLeft(r.line, " ")))
		skips = append(skips, r.skip)
	}
	// Drop the indent the lines share. It is measured on the whole row, so a
	// drag that starts at the first letter ("Hi Chirag,", no spaces copied)
	// doesn't stop the rows below losing theirs.
	indent := -1
	for i, l := range lines {
		if strings.TrimSpace(l) != "" && (indent < 0 || indents[i] < indent) {
			indent = indents[i]
		}
	}
	for i, l := range lines {
		// Spaces copied left of the shared indent's column go.
		n := 0
		for n < indent+skips[i] && n < len(l) && l[n] == ' ' {
			n++
		}
		lines[i] = l[n:]
	}
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
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
