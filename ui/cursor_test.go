package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func TestDrawCursor(t *testing.T) {
	screen := "top\n> ab\n\n"       // three rows: "top", "> ab", "" (last row blank)
	out := DrawCursor(screen, 3, 2) // row "> ab", column 3 = "b"
	lines := strings.Split(out, "\n")
	require.Equal(t, "top", lines[0])
	require.Equal(t, "> ab", ansi.Strip(lines[1]))
	require.Contains(t, lines[1], "\x1b[7mb\x1b[0m")

	// Cursor past the end of a line becomes a block on a space.
	out = DrawCursor("> \n", 2, 1)
	require.Contains(t, out, "\x1b[7m \x1b[0m")

	// Out of range leaves the content alone.
	require.Equal(t, screen, DrawCursor(screen, 1, 9))
}
