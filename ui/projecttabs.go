package ui

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

var (
	projectTabActiveStyle = lipgloss.NewStyle().
				Background(lipgloss.Color("#0078d4")).
				Foreground(lipgloss.Color("#ffffff")).
				Bold(true).
				Padding(0, 1)
	projectTabStyle = lipgloss.NewStyle().
			Foreground(lipgloss.AdaptiveColor{Light: "#555555", Dark: "#aaaaaa"}).
			Padding(0, 1)
	projectTabHintStyle = lipgloss.NewStyle().
				Foreground(lipgloss.AdaptiveColor{Light: "#A49FA5", Dark: "#666666"})
	projectTabSepStyle = lipgloss.NewStyle().
				Foreground(lipgloss.AdaptiveColor{Light: "#cccccc", Dark: "#444444"})
	projectTabAddStyle = lipgloss.NewStyle().
				Foreground(lipgloss.AdaptiveColor{Light: "#555555", Dark: "#888888"}).
				Padding(0, 1)
)

// ProjectTabs renders the row of open projects across the top of the screen,
// like editor tabs. ←/→ or a click move between them; "+ add project" on the
// right opens the folder chooser.
type ProjectTabs struct {
	projects []string
	active   int
	counts   map[string]int
	needs    map[string]int
	free     map[string]int // auto issue loop: free session slots to fill
	width    int
	// spans holds each tab's [start, end) column from the last render, and
	// addStart the column where the "+ add project" button begins.
	spans    [][2]int
	addStart int
}

func NewProjectTabs() *ProjectTabs {
	return &ProjectTabs{counts: map[string]int{}, needs: map[string]int{}, free: map[string]int{}}
}

func (t *ProjectTabs) SetWidth(width int) { t.width = width }

// SetProjects replaces the open tabs and marks active as the selected one.
func (t *ProjectTabs) SetProjects(projects []string, active string) {
	t.projects = projects
	t.active = 0
	for i, p := range projects {
		if p == active {
			t.active = i
		}
	}
}

// SetNeeds sets how many sessions in a project wait on the user.
func (t *ProjectTabs) SetNeeds(project string, n int) { t.needs[project] = n }

// SetFree sets how many issues the auto loop could start in a project now
// (shown as "·2 free"; 0 hides it).
func (t *ProjectTabs) SetFree(project string, n int) { t.free[project] = n }

// SetCount sets the number of agents shown next to a project's name.
func (t *ProjectTabs) SetCount(project string, n int) { t.counts[project] = n }

func (t *ProjectTabs) Projects() []string {
	if t == nil {
		return nil
	}
	return t.projects
}

// Active returns the selected project path, or "" if there are no tabs.
func (t *ProjectTabs) Active() string {
	if t == nil || t.active < 0 || t.active >= len(t.projects) {
		return ""
	}
	return t.projects[t.active]
}

// Next selects the tab to the right (wrapping) and returns its project.
func (t *ProjectTabs) Next() string {
	if len(t.projects) > 0 {
		t.active = (t.active + 1) % len(t.projects)
	}
	return t.Active()
}

// Prev selects the tab to the left (wrapping) and returns its project.
func (t *ProjectTabs) Prev() string {
	if len(t.projects) > 0 {
		t.active = (t.active - 1 + len(t.projects)) % len(t.projects)
	}
	return t.Active()
}

// HitAddProject is returned by HitTest for a click on the "+ add project" button.
const HitAddProject = -2

// Select makes tab i active and returns its project.
func (t *ProjectTabs) Select(i int) string {
	if i >= 0 && i < len(t.projects) {
		t.active = i
	}
	return t.Active()
}

// HitTest maps a click at column x on the tab row to a tab index,
// HitAddProject, or -1 for empty space. It uses the spans from the last render.
func (t *ProjectTabs) HitTest(x int) int {
	if t.spans == nil {
		return -1 // not rendered yet
	}
	if x >= t.addStart {
		return HitAddProject
	}
	for i, sp := range t.spans {
		if x >= sp[0] && x < sp[1] {
			return i
		}
	}
	return -1
}

func (t *ProjectTabs) String() string {
	const hint = ""
	addButton := projectTabAddStyle.Render("+ add project (⌃Space A)")

	names := uniqueNames(t.projects)
	labels := make([]string, len(t.projects))
	for i, p := range t.projects {
		label := names[i]
		if n := t.counts[p]; n > 0 {
			label += fmt.Sprintf(" (%d)", n)
		}
		if n := t.needs[p]; n > 0 {
			label += fmt.Sprintf(" ❓%d", n)
		}
		if n := t.free[p]; n > 0 {
			label += fmt.Sprintf(" ·%d free", n)
		}
		if i == t.active {
			labels[i] = projectTabActiveStyle.Render(label)
		} else {
			labels[i] = projectTabStyle.Render(label)
		}
	}

	// Keep the active tab on screen when the row is too wide by dropping tabs
	// from the left.
	sep := projectTabSepStyle.Render("│")
	reserved := lipgloss.Width(addButton) + runewidth.StringWidth(hint)
	start := 0
	for start < t.active && rowWidth(labels[start:], sep)+2 > t.width-reserved {
		start++
	}

	// Build the row and remember where each tab sits for mouse clicks.
	t.spans = make([][2]int, len(t.projects))
	var row strings.Builder
	col := 0
	if start > 0 {
		row.WriteString(projectTabSepStyle.Render("‹ "))
		col += 2
	}
	for i := start; i < len(labels); i++ {
		if i > start {
			row.WriteString(sep)
			col += lipgloss.Width(sep)
		}
		w := lipgloss.Width(labels[i])
		t.spans[i] = [2]int{col, col + w}
		row.WriteString(labels[i])
		col += w
	}

	gap := t.width - col - reserved
	if gap < 1 {
		gap = 1
	}
	right := projectTabHintStyle.Render(hint) + addButton
	if t.width-col-lipgloss.Width(addButton) < runewidth.StringWidth(hint)+1 {
		right = addButton // not enough room for the hint
		gap = max(1, t.width-col-lipgloss.Width(addButton))
	}
	t.addStart = col + gap + lipgloss.Width(right) - lipgloss.Width(addButton)
	return row.String() + strings.Repeat(" ", gap) + right
}

// TabNames returns the names shown on the tabs for these project paths.
func TabNames(paths []string) []string { return uniqueNames(paths) }

// uniqueNames returns each path's folder name, adding the nearest parent
// folder that tells same-named projects apart (e.g. two CI runner checkouts:
// "repo · runner-2", "repo · runner-3").
func uniqueNames(paths []string) []string {
	names := make([]string, len(paths))
	count := map[string]int{}
	for i, p := range paths {
		names[i] = filepath.Base(p)
		count[names[i]]++
	}
	for i, p := range paths {
		if count[names[i]] < 2 {
			continue
		}
		parts := strings.Split(filepath.Dir(p), string(filepath.Separator))
		for j := len(parts) - 1; j >= 0; j-- {
			if parts[j] == "" || parts[j] == names[i] || strings.HasPrefix(parts[j], "_") {
				continue
			}
			unique := true
			for k, q := range paths {
				if k != i && filepath.Base(q) == names[i] && strings.Contains(q, string(filepath.Separator)+parts[j]+string(filepath.Separator)) {
					unique = false
					break
				}
			}
			if unique {
				names[i] += " · " + parts[j]
				break
			}
		}
	}
	return names
}

func rowWidth(labels []string, sep string) int {
	w := 0
	for i, l := range labels {
		if i > 0 {
			w += lipgloss.Width(sep)
		}
		w += lipgloss.Width(l)
	}
	return w
}
