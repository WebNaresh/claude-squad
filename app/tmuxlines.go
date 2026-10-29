package app

import (
	"os/exec"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// tmuxLines returns the selected text as the program printed it, read from
// tmux, when tmux wrapped any of the selected rows. A shell (the dock, a
// terminal tile) wraps long lines at the exact column, mid-word; guessing
// the joins from the screen (joinWrapped, made for Claude's word-wrapped
// text) put spaces inside words ("PROPOSAL_SCH EDULE"), kept some wrapped
// rows apart and glued separate lines together (a psql table, 2026-10-29).
// tmux knows which rows are one line (capture-pane -J): the selected rows
// are found among the pane's rows rebuilt from those lines, and the lines
// they belong to are copied. ok=false when the rows aren't found or tmux
// wrapped none of them (Claude writes its own line breaks).
func tmuxLines(target string, rows []selRow) (string, bool) {
	if target == "" || len(rows) == 0 {
		return "", false
	}
	out, err := exec.Command("tmux", "display-message", "-p", "-t", "="+target+":", "#{pane_width}").Output()
	if err != nil {
		return "", false
	}
	w, err := strconv.Atoi(strings.TrimSpace(string(out)))
	if err != nil || w <= 0 {
		return "", false
	}
	out, err = exec.Command("tmux", "capture-pane", "-p", "-J", "-S", "-", "-t", "="+target+":").Output()
	if err != nil {
		return "", false
	}
	return linesFor(strings.Split(strings.TrimSuffix(string(out), "\n"), "\n"), w, rows)
}

type paneRow struct {
	line, col int // the printed line it is part of, and its first column
	text      string
}

// linesFor finds rows among the pane rows that lines wrap into at width w
// (the latest match) and returns the selected part of those lines.
func linesFor(lines []string, w int, rows []selRow) (string, bool) {
	var pane []paneRow
	for i, l := range lines {
		n := ansi.StringWidth(l)
		for c := 0; c == 0 || c < n; c += w {
			pane = append(pane, paneRow{line: i, col: c, text: strings.TrimRight(ansi.Cut(l, c, c+w), " ")})
		}
	}
	n := len(rows)
	at := -1
	for i := len(pane) - n; i >= 0 && at < 0; i-- {
		at = i
		for j, r := range rows {
			if pane[i+j].text != r.raw {
				at = -1
				break
			}
		}
	}
	if at < 0 {
		return "", false
	}
	first, last := pane[at], pane[at+n-1]
	// Wrapped: the first row continues a line, a row goes on into the next,
	// or the last row goes on below the selection.
	wrapped := first.col > 0 || at+n < len(pane) && pane[at+n].line == last.line
	for j := at; j < at+n-1 && !wrapped; j++ {
		wrapped = pane[j].line == pane[j+1].line
	}
	if !wrapped {
		return "", false // tmux wrapped none of them
	}
	start, end := first.col+rows[0].c0, last.col+rows[n-1].c1
	var b []string
	for i := first.line; i <= last.line; i++ {
		l := lines[i]
		a, z := 0, ansi.StringWidth(l)
		if i == first.line {
			a = start
		}
		if i == last.line {
			z = min(z, end+1)
		}
		b = append(b, strings.TrimRight(ansi.Cut(l, a, z), " "))
	}
	return strings.Join(b, "\n"), true
}
