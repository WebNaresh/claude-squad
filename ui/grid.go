package ui

import (
	"math"
	"strconv"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

var (
	gridTileStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#3c3c3c"))
	gridFocusStyle   = lipgloss.NewStyle().Border(lipgloss.ThickBorder()).BorderForeground(lipgloss.Color("#0078d4"))
	gridTitleStyle   = lipgloss.NewStyle().Bold(true)
	gridStatusStyle  = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#888888", Dark: "#888888"})
	gridNeedsStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#e5a50a")).Bold(true)
	gridEmptyMessage = "No Claude sessions in this project yet. ⌃Space C starts one."
)

// GridTile is one session shown in the grid.
type GridTile struct {
	Title    string
	Status   string
	NeedsYou bool
	// Stage is the /stage progress ("✓ staged", "✓ done · close session"),
	// shown in place of the status while set.
	Stage   string
	Content string
	// Accent is the session's own colour (title and border), the same
	// wherever its tile moves, so after a close the tiles that shift are
	// still told apart. Empty: the plain grey.
	Accent lipgloss.Color
	// Ghost: a just-closed session's place, kept a few seconds before the
	// tiles close the gap (app/closeghost.go).
	Ghost bool
}

// Smallest useful tile (columns × rows, border included); the grid fits as
// many as the area allows and pages the rest.
const (
	gridMinTileW = 40
	gridMinTileH = 12
)

// GridLayout returns the columns, rows and the height left for tiles when
// showing n tiles in a width×height area (one line is kept for the page
// indicator when they don't all fit).
func GridLayout(n, width, height int) (cols, rows, bodyH int) {
	if c, r, ok := balancedLayout(n, width, height); ok {
		return c, r, height
	}
	fit := func(h int) (int, int) {
		c := max(1, min(n, width/gridMinTileW))
		r := max(1, min((n+c-1)/c, h/gridMinTileH))
		return c, r
	}
	cols, rows = fit(height)
	bodyH = height
	if n > cols*rows {
		bodyH = height - 1
		cols, rows = fit(bodyH)
	}
	return cols, rows, bodyH
}

// tileAspect is the tile shape aimed for, in columns per row. Terminal cells
// are about twice as tall as wide, so 2.0 looks about square; a wide screen
// used to put every session in one row of tall narrow strips.
const tileAspect = 2.0

// balancedLayout picks the columns × rows that show all n tiles with each
// tile closest to tileAspect (ties: fewer empty slots). ok is false when no
// layout fits them all at the minimum tile size; then the grid pages.
func balancedLayout(n, width, height int) (cols, rows int, ok bool) {
	best := math.Inf(1)
	for c := 1; c <= n; c++ {
		r := (n + c - 1) / c
		w, h := width/c, height/r
		if w < gridMinTileW || h < gridMinTileH {
			continue
		}
		score := math.Abs(math.Log(float64(w) / float64(h) / tileAspect))
		score += float64(c*r-n) * 0.2 // empty slots look like a gap
		if score < best {
			best, cols, rows, ok = score, c, r, true
		}
	}
	return cols, rows, ok
}

// GridTileSize returns the content size (inside the border and its one
// column of padding; the title sits in the top border) of each tile for n tiles in a width×height area.
func GridTileSize(n, width, height int) (w, h int) {
	cols, rows, bodyH := GridLayout(n, width, height)
	return max(width/cols-4, 10), max(bodyH/rows-2, 3)
}

// RenderGrid draws the page of tiles containing the focused one, the
// focused tile with a bright thick border.
func RenderGrid(tiles []GridTile, focused, width, height int) string {
	if len(tiles) == 0 {
		return lipgloss.Place(width, height, lipgloss.Center, lipgloss.Center, gridEmptyMessage)
	}
	cols, rows, bodyH := GridLayout(len(tiles), width, height)
	perPage := cols * rows
	page := max(0, focused) / perPage
	start := page * perPage
	end := min(start+perPage, len(tiles))
	shown := tiles[start:end]

	tileW, tileH := width/cols, bodyH/rows
	innerW, innerH := GridTileSize(len(tiles), width, height)

	var rowStrs []string
	for r := 0; r < rows; r++ {
		var cells []string
		for c := 0; c < cols; c++ {
			i := r*cols + c
			if i >= len(shown) {
				cells = append(cells, lipgloss.NewStyle().Width(tileW).Height(tileH).Render(""))
				continue
			}
			cells = append(cells, RenderTile(shown[i], start+i == focused, innerW, innerH))
		}
		rowStrs = append(rowStrs, lipgloss.JoinHorizontal(lipgloss.Top, cells...))
	}
	out := lipgloss.JoinVertical(lipgloss.Left, rowStrs...)
	if len(tiles) > perPage {
		pages := (len(tiles) + perPage - 1) / perPage
		out = lipgloss.JoinVertical(lipgloss.Left, out,
			gridStatusStyle.Render("  page "+strconv.Itoa(page+1)+" of "+strconv.Itoa(pages)+" · ⇧arrows move between tiles · Cmd − fits more"))
	}
	return out
}

// RenderTile draws one tile: title and status in the top border, the
// bottom of the content inside, w×h inside the border and padding.
func RenderTile(t GridTile, focused bool, w, h int) string {
	style, border, color := gridTileStyle, lipgloss.RoundedBorder(), lipgloss.Color("#3c3c3c")
	// Every session in its own colour, bold enough to tell tiles apart at a
	// glance (a thick frame and a coloured name tag); the one being typed in
	// gets a white double frame on top of its colour tag.
	switch {
	case t.Ghost:
		border, color = lipgloss.NormalBorder(), lipgloss.Color("#e06c75")
		style = style.Border(border).BorderForeground(color)
	case focused:
		style, border, color = gridFocusStyle, lipgloss.DoubleBorder(), lipgloss.Color("#ffffff")
		style = style.Border(border).BorderForeground(color)
	case t.Accent != "":
		border, color = lipgloss.ThickBorder(), t.Accent
		style = style.Border(border).BorderForeground(color)
	}
	edge := lipgloss.NewStyle().Foreground(color)

	// Title on the left of the top border, status on the right.
	status := gridStatusStyle.Render(" " + t.Status + " ")
	if t.Stage != "" {
		status = stageTag(t.Stage)
	}
	titleText := t.Title
	if t.NeedsYou {
		status = gridNeedsStyle.Render(" needs you ")
		titleText = "❓ " + titleText
	}
	if focused && !t.Ghost {
		// Said in words on the right too, where the name tag can't be cut.
		status = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#000000")).Background(lipgloss.Color("#ffffff")).Render(" ◀ SELECTED ") + " " + status
	}
	titleStyle := gridTitleStyle
	if t.Ghost {
		titleStyle = titleStyle.Foreground(lipgloss.Color("#ffffff")).Background(lipgloss.Color("#be5046")).Padding(0, 1)
	} else if focused {
		// The selected tile reads as selected from across the room: a white
		// tag with an arrow, on top of its white double frame.
		titleStyle = titleStyle.Bold(true).Foreground(lipgloss.Color("#000000")).Background(lipgloss.Color("#ffffff")).Padding(0, 1)
		titleText = "▶ " + titleText
	} else if t.Accent != "" {
		// The name as a solid tag in the session's colour.
		titleStyle = titleStyle.Foreground(lipgloss.Color("#1a1a1a")).Background(t.Accent).Padding(0, 1)
	}
	title := " " + titleStyle.Render(cutKeepingIssue(titleText, max(0, w-lipgloss.Width(status)-4))) + " "
	fill := max(0, w-lipgloss.Width(title)-lipgloss.Width(status))
	top := edge.Render(border.TopLeft+border.Top) + title + edge.Render(strings.Repeat(border.Top, fill)) +
		status + edge.Render(border.Top+border.TopRight)

	// Show the bottom of the screen (where Claude's prompt is), cut to fit.
	content := t.Content
	if strings.TrimSpace(ansi.Strip(content)) == "" {
		content = gridStatusStyle.Render("Nothing on screen yet")
	}
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	if len(lines) > h {
		lines = lines[len(lines)-h:]
	}
	for i, l := range lines {
		lines[i] = ansi.Truncate(l, w, "")
	}
	for len(lines) < h {
		lines = append(lines, "")
	}
	body := lipgloss.NewStyle().Width(w).Render(strings.Join(lines, "\n"))
	return top + "\n" + style.BorderTop(false).Padding(0, 1).Width(w+2).Render(body)
}

// cutKeepingIssue shortens a tile name to w cells, cutting the middle so
// the issue number at the end ("…-#2268") stays: it is what tells two tiles
// apart, and what you check before closing one.
func cutKeepingIssue(title string, w int) string {
	if ansi.StringWidth(title) <= w {
		return title
	}
	i := strings.LastIndex(title, "#")
	if i < 0 || ansi.StringWidth(title[i:])+4 > w {
		return ansi.Truncate(title, w, "…")
	}
	tail := title[i:]
	return ansi.Truncate(title[:i], w-ansi.StringWidth(tail), "…") + tail
}

// stageTag draws the /stage progress as a solid tag, so a finished session
// can't be mistaken for one still working: green "✓ DONE · close it", amber
// when a question waits on the user, blue while staging is under way.
func stageTag(stage string) string {
	bg, fg, text := lipgloss.Color("#1f6feb"), lipgloss.Color("#ffffff"), stage
	switch {
	case strings.HasPrefix(stage, "✓ done"):
		bg, fg, text = lipgloss.Color("#2ea043"), lipgloss.Color("#0d1117"), "✓ DONE · close it"
	case strings.HasPrefix(stage, "?"):
		bg, fg = lipgloss.Color("#e5a50a"), lipgloss.Color("#0d1117")
	}
	return lipgloss.NewStyle().Bold(true).Foreground(fg).Background(bg).Padding(0, 1).Render(text)
}
