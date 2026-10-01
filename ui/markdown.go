package ui

import (
	"strings"

	"github.com/charmbracelet/glamour"
)

// MarkdownCache renders markdown for the preview pane, the way Claude's own
// terminal shows it (bold, lists, tables, code). Rendering is slow next to the
// 100ms preview tick, so the last result is reused until the source or the
// width changes.
type MarkdownCache struct {
	width    int
	renderer *glamour.TermRenderer
	key      string
	out      string
}

func (c *MarkdownCache) Render(md, version string, width int) string {
	if width < 20 {
		width = 20
	}
	key := version
	if key == "" {
		key = md
	}
	if c.renderer != nil && c.width == width && c.key == key {
		return c.out
	}
	if c.renderer == nil || c.width != width {
		// A fixed style: auto-detection queries the terminal, which can hang
		// inside the running UI.
		r, err := glamour.NewTermRenderer(glamour.WithStandardStyle("dark"), glamour.WithWordWrap(width-6))
		if err != nil {
			return md
		}
		c.renderer, c.width = r, width
	}
	out, err := c.renderer.Render(md)
	if err != nil {
		out = md
	}
	c.key, c.out = key, strings.Trim(out, "\n")
	return c.out
}
