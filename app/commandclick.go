package app

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"github.com/charmbracelet/x/ansi"

	tea "github.com/charmbracelet/bubbletea"
)

// commandAt returns the "! …" command (one Claude asks the user to run)
// whose rows hold the click, joined across the rows Claude wrapped it onto,
// or "". w is the pane's width. A drag over it was the only way to copy it,
// and on a Mac with Force Click on a firm press opens Look Up and the drag
// never arrives (2026-10-28); a click always does.
func commandAt(lines []string, row, w int) string {
	if row < 0 || row >= len(lines) {
		return ""
	}
	start := -1
	for r := row; r >= 0 && row-r < 12; r-- {
		l := strings.TrimSpace(lines[r])
		if isCommandStart(l) {
			start = r
			break
		}
		if l == "" || listStart.MatchString(l) {
			return "" // the paragraph above the click starts elsewhere
		}
	}
	if start < 0 {
		return ""
	}
	// Every row below, down to a blank row, a list item or a row indented
	// less than the "!", is the same command: it is one shell line. Claude
	// moves a long path down to a row of its own ("-f" then the path), and
	// the drag-copy rule that starts a new line there (joinWrapped) split
	// the command: zsh ran the path on its own ("permission denied").
	col := strings.Index(lines[start], "!")
	end := start
	for r := start + 1; r < len(lines) && r-start < 20; r++ {
		l := strings.TrimRight(lines[r], " ")
		t := strings.TrimLeft(l, " ")
		if t == "" || len(l)-len(t) < col || listStart.MatchString(t) {
			break
		}
		end = r
	}
	if end < row {
		return "" // the click is below the command's last row
	}
	text := strings.TrimSpace(lines[start])
	for r := start + 1; r <= end; r++ {
		prev := strings.TrimRight(lines[r-1], " ")
		sep := " "
		// A full row was cut mid-word (a path): no space at the join.
		if ansi.StringWidth(prev) >= w {
			p := strings.TrimSpace(prev)
			if last := p[strings.LastIndex(p, " ")+1:]; last == p || strings.Contains(last, "/") {
				sep = ""
			}
		}
		text += sep + strings.TrimSpace(lines[r])
	}
	if i := strings.Index(text, "! "); i > 0 {
		text = text[i:] // drop a list marker before it ("1. ! …")
	}
	return strings.TrimSpace(text)
}

var listMarker = regexp.MustCompile(`^\s*(?:[-*•]|\d+[.)])\s+`)

// isCommandStart: a row that begins a "! …" command, on its own or after a
// list marker.
func isCommandStart(l string) bool {
	l = strings.TrimSpace(listMarker.ReplaceAllString(l, ""))
	return strings.HasPrefix(l, "! ") && len(strings.TrimSpace(l[2:])) > 0
}

// copyCommand puts a clicked command on the clipboard.
func copyCommand(cmdText string) tea.Cmd {
	return func() tea.Msg {
		c := exec.Command("pbcopy")
		c.Stdin = strings.NewReader(cmdText)
		if err := c.Run(); err != nil {
			return fmt.Errorf("could not copy: %w", err)
		}
		logEvent("copied a clicked command (%d chars)", len(cmdText))
		return fmt.Errorf("copied the command · ⌘V to paste")
	}
}
