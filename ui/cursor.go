package ui

import (
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// DrawCursor draws a block cursor into captured screen content: column x on
// the row fromBottom rows above the end (1 = last row). tmux's capture leaves
// the cursor out, so without this you can't see where you are typing.
func DrawCursor(content string, x, fromBottom int) string {
	// One line per pane row; drop only the final newline so blank rows at the
	// bottom still count.
	lines := strings.Split(strings.TrimSuffix(content, "\n"), "\n")
	row := len(lines) - fromBottom
	if row < 0 || row >= len(lines) || x < 0 {
		return content
	}
	line := lines[row]
	if w := ansi.StringWidth(line); w <= x {
		line += strings.Repeat(" ", x-w+1)
	}
	under := ansi.Strip(ansi.Cut(line, x, x+1))
	if under == "" {
		under = " "
	}
	lines[row] = ansi.Truncate(line, x, "") + "\x1b[0m\x1b[7m" + under + "\x1b[0m" + ansi.TruncateLeft(line, x+1, "")
	return strings.Join(lines, "\n")
}
