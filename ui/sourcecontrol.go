package ui

import (
	"claude-squad/session/git"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

var (
	scTitleStyle    = lipgloss.NewStyle().Background(lipgloss.Color("#0078d4")).Foreground(lipgloss.Color("#ffffff"))
	scSectionStyle  = lipgloss.NewStyle().Bold(true)
	scDimStyle      = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#888888", Dark: "#777777"})
	scWarnStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#e5a50a"))
	scSelectedStyle = lipgloss.NewStyle().Background(lipgloss.Color("#04395e")).Foreground(lipgloss.Color("#ffffff"))
	scLetterColors  = map[byte]lipgloss.Color{
		'M': "#e2c08d", 'A': "#73c991", 'D': "#c74e39", 'R': "#73c991", 'C': "#73c991", 'U': "#73c991", '?': "#73c991",
	}
)

// scRow is one selectable line: a file in the Staged or the Changes section.
type scRow struct {
	file   git.FileStatus
	staged bool
}

// SourceControl is the left column: the project's git changes, like VS Code's
// Source Control view. It covers the project folder itself; agents started
// with n work in their own worktrees, so their changes show in their Diff tab.
type SourceControl struct {
	root       string
	branch     string
	files      []git.FileStatus
	rows       []scRow
	cursor     int
	focused    bool
	autoCommit bool
	err        error
	loading    bool
	width      int
	height     int
}

func NewSourceControl() *SourceControl { return &SourceControl{} }

func (s *SourceControl) SetSize(width, height int) { s.width, s.height = width, height }
func (s *SourceControl) SetFocused(f bool)         { s.focused = f }
func (s *SourceControl) Focused() bool             { return s != nil && s.focused }
func (s *SourceControl) Root() string              { return s.root }

// SetStatus replaces the file list, keeping the cursor on the same file.
func (s *SourceControl) SetStatus(root, branch string, files []git.FileStatus, autoCommit bool, err error) {
	var keep *scRow
	if r := s.Selected(); r != nil && root == s.root {
		keep = r
	}
	s.root, s.branch, s.files, s.autoCommit, s.err = root, branch, files, autoCommit, err
	s.loading = false
	s.rows = s.rows[:0]
	for _, f := range files {
		if f.Staged() {
			s.rows = append(s.rows, scRow{file: f, staged: true})
		}
	}
	for _, f := range files {
		if f.Changed() {
			s.rows = append(s.rows, scRow{file: f})
		}
	}
	s.cursor = min(s.cursor, max(0, len(s.rows)-1))
	if keep != nil {
		for i, r := range s.rows {
			if r.file.Path == keep.file.Path && r.staged == keep.staged {
				s.cursor = i
			}
		}
	}
}

// SetLoading shows a project whose status hasn't been read yet.
func (s *SourceControl) SetLoading(root string) {
	s.SetStatus(root, "", nil, false, nil)
	s.loading = true
}

// Selected returns the file under the cursor, or nil.
func (s *SourceControl) Selected() *scRow {
	if s.cursor < 0 || s.cursor >= len(s.rows) {
		return nil
	}
	r := s.rows[s.cursor]
	return &r
}

// SelectedFile returns the selected file and whether it is the staged entry.
func (s *SourceControl) SelectedFile() (git.FileStatus, bool, bool) {
	r := s.Selected()
	if r == nil {
		return git.FileStatus{}, false, false
	}
	return r.file, r.staged, true
}

func (s *SourceControl) Up() {
	if s.cursor > 0 {
		s.cursor--
	}
}

func (s *SourceControl) Down() {
	if s.cursor < len(s.rows)-1 {
		s.cursor++
	}
}

// StagedCount returns how many files are staged.
func (s *SourceControl) StagedCount() int {
	n := 0
	for _, r := range s.rows {
		if r.staged {
			n++
		}
	}
	return n
}

func (s *SourceControl) String() string {
	// A box like the session tiles: title and branch in the top border, the
	// staged and changed files inside. Its keys are in the key bar.
	w := max(s.width-4, 10) // border and one column of padding on each side
	room := max(s.height-2, 1)
	var head []string
	if s.autoCommit {
		head = append(head, scDimStyle.Render(runewidth.Truncate("⟳ gai-watch auto-commit", w, "…")), "")
	}

	var lines []string
	rowLine := map[int]int{} // row index -> line index, to keep the cursor visible
	if s.err != nil {
		lines = append(lines, scWarnStyle.Render(runewidth.Truncate(s.err.Error(), w, "…")))
	} else {
		// Like VS Code, the Staged section shows only when something is staged.
		staged := s.StagedCount()
		if staged > 0 {
			lines = append(lines, scSectionStyle.Render(fmt.Sprintf("Staged (%d)", staged)))
		}
		for i, r := range s.rows {
			if i == staged {
				if staged > 0 {
					lines = append(lines, "")
				}
				lines = append(lines, scSectionStyle.Render(fmt.Sprintf("Changes (%d)", len(s.rows)-staged)))
			}
			rowLine[i] = len(lines)
			lines = append(lines, s.renderRow(r, i == s.cursor && s.focused, w))
		}
		if len(s.rows) == 0 {
			msg := "No changes"
			if s.loading {
				msg = "Loading…"
			}
			lines = append(lines, "", scDimStyle.Render(msg))
		}
	}

	// Scroll so the cursor stays on screen.
	fit := room - len(head)
	start := 0
	if l, ok := rowLine[s.cursor]; ok && fit > 0 && l >= fit {
		start = l - fit + 1
	}
	if fit > 0 && len(lines)-start > fit {
		lines = lines[start : start+fit]
	}
	body := strings.Join(append(head, lines...), "\n")

	border, color := lipgloss.RoundedBorder(), lipgloss.Color("#3c3c3c")
	if s.focused {
		border, color = lipgloss.ThickBorder(), lipgloss.Color("#0078d4")
	}
	box := lipgloss.NewStyle().Border(border, false, true, true, true).BorderForeground(color).
		Padding(0, 1).Width(s.width - 2).Height(room).MaxHeight(room + 1).Render(body)
	return s.topBorder(border, color) + "\n" + box
}

// topBorder draws the box's top edge with the title and branch in it.
func (s *SourceControl) topBorder(b lipgloss.Border, color lipgloss.Color) string {
	edge := lipgloss.NewStyle().Foreground(color)
	title := " " + scSectionStyle.Render("Source Control")
	if s.branch != "" {
		title += scDimStyle.Render(" · " + runewidth.Truncate(s.branch, max(0, s.width-24), "…"))
	}
	title += " "
	fill := max(0, s.width-3-lipgloss.Width(title))
	return edge.Render(b.TopLeft+b.Top) + title + edge.Render(strings.Repeat(b.Top, fill)+b.TopRight)
}

func (s *SourceControl) renderRow(r scRow, selected bool, width int) string {
	letter := r.file.Y
	if r.staged {
		letter = r.file.X
	}
	if r.file.Untracked() {
		letter = 'U'
	}
	name := filepath.Base(r.file.Path)
	dir := filepath.Dir(r.file.Path)
	if dir == "." {
		dir = ""
	}
	// Truncate plain text first; styling adds escape codes that would throw
	// the width count off.
	avail := width - 3
	plainName := runewidth.Truncate(" "+name, avail, "…")
	plainDir := ""
	if dir != "" {
		plainDir = runewidth.Truncate(" "+dir, max(0, avail-runewidth.StringWidth(plainName)), "…")
	}
	pad := strings.Repeat(" ", max(0, avail-runewidth.StringWidth(plainName)-runewidth.StringWidth(plainDir)))
	if selected {
		return scSelectedStyle.Render(plainName + plainDir + pad + " " + string(letter))
	}
	return plainName + scDimStyle.Render(plainDir) + pad + " " + lipgloss.NewStyle().Foreground(scLetterColors[letter]).Render(string(letter))
}
