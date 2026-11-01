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
// right opens the folder chooser. Quiet projects stay open but sit behind a
// "⋯ N more" chip.
type ProjectTabs struct {
	projects []string
	active   int
	counts   map[string]int
	needs    map[string]int
	free     map[string]int // auto issue loop: free session slots to fill
	checks   map[string]TabChecks
	// quiet projects keep their tab out of the row (see Shown).
	quiet map[string]bool
	width int
	// spans holds each tab's [start, end) column from the last render, and
	// addStart the column where the "+ add project" button begins.
	spans    [][2]int
	addStart int
	// moreSpan is the "⋯ N more" chip's [start, end) column; empty when no
	// tab is hidden.
	moreSpan [2]int
}

func NewProjectTabs() *ProjectTabs {
	return &ProjectTabs{counts: map[string]int{}, needs: map[string]int{}, free: map[string]int{}, checks: map[string]TabChecks{}, quiet: map[string]bool{}}
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

// TabChecks is a project's PR check status, drawn at the end of its tab:
// Text is "✓ 5/5", "● 2 running" or "✗ 1 failed"; Color its foreground.
type TabChecks struct {
	Text  string
	Color string
}

// SetChecks sets the checks chip of a project's tab ("" hides it).
func (t *ProjectTabs) SetChecks(project string, c TabChecks) {
	if c.Text == "" {
		delete(t.checks, project)
		return
	}
	t.checks[project] = c
}

// SetFree sets how many issues the auto loop could start in a project now
// (shown as "·2 free"; 0 hides it).
func (t *ProjectTabs) SetFree(project string, n int) { t.free[project] = n }

// SetCount sets the number of agents shown next to a project's name.
func (t *ProjectTabs) SetCount(project string, n int) { t.counts[project] = n }

// SetQuiet sets the projects with nothing going on, which leave the row.
func (t *ProjectTabs) SetQuiet(quiet map[string]bool) { t.quiet = quiet }

// Shown reports whether tab i is in the row. The selected tab and any project
// with a session are never hidden, so the row follows sessions at once.
func (t *ProjectTabs) Shown(i int) bool {
	p := t.projects[i]
	return i == t.active || !t.quiet[p] || t.counts[p] > 0 || t.needs[p] > 0 || t.free[p] > 0
}

// ShownProjects returns the projects in the row, in tab order.
func (t *ProjectTabs) ShownProjects() []string {
	var out []string
	for i, p := range t.projects {
		if t.Shown(i) {
			out = append(out, p)
		}
	}
	return out
}

// HiddenProjects returns the projects behind the "⋯ N more" chip.
func (t *ProjectTabs) HiddenProjects() []string {
	var out []string
	for i, p := range t.projects {
		if !t.Shown(i) {
			out = append(out, p)
		}
	}
	return out
}

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

// step moves the selection to the next shown tab in dir (wrapping).
func (t *ProjectTabs) step(dir int) string {
	n := len(t.projects)
	for i := 1; i < n; i++ {
		if j := ((t.active+dir*i)%n + n) % n; t.Shown(j) {
			t.active = j
			break
		}
	}
	return t.Active()
}

// Next selects the shown tab to the right (wrapping) and returns its project.
func (t *ProjectTabs) Next() string { return t.step(1) }

// Prev selects the shown tab to the left (wrapping) and returns its project.
func (t *ProjectTabs) Prev() string { return t.step(-1) }

// HitAddProject is returned by HitTest for a click on the "+ add project" button.
const HitAddProject = -2

// HitMore is returned by HitTest for a click on the "⋯ N more" chip.
const HitMore = -3

// Select makes tab i active and returns its project.
func (t *ProjectTabs) Select(i int) string {
	if i >= 0 && i < len(t.projects) {
		t.active = i
	}
	return t.Active()
}

// HitTest maps a click at column x on the tab row to a tab index,
// HitAddProject, HitMore, or -1 for empty space. It uses the spans from the last render.
func (t *ProjectTabs) HitTest(x int) int {
	if t.spans == nil {
		return -1 // not rendered yet
	}
	if x >= t.addStart {
		return HitAddProject
	}
	if x >= t.moreSpan[0] && x < t.moreSpan[1] {
		return HitMore
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
	var labels []string
	var shown []int // labels[k] is tab shown[k]
	activeAt := 0
	for i, p := range t.projects {
		if !t.Shown(i) {
			continue
		}
		if i == t.active {
			activeAt = len(shown)
		}
		shown = append(shown, i)
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
		base := projectTabStyle
		if i == t.active {
			base = projectTabActiveStyle
		}
		c, hasChecks := t.checks[p]
		if !hasChecks {
			labels = append(labels, base.Render(label))
			continue
		}
		// The chip keeps the tab's background, so the active tab stays one block.
		plain := base.UnsetPadding()
		labels = append(labels, plain.Render(" "+label)+
			plain.Foreground(lipgloss.Color(c.Color)).Bold(true).Render(" "+c.Text)+plain.Render(" "))
	}

	more := ""
	if n := len(t.projects) - len(shown); n > 0 {
		more = projectTabAddStyle.Render(fmt.Sprintf("⋯ %d more", n))
	}

	// Keep the active tab on screen when the row is too wide by dropping tabs
	// from the left.
	sep := projectTabSepStyle.Render("│")
	reserved := lipgloss.Width(addButton) + runewidth.StringWidth(hint) + lipgloss.Width(more)
	start := 0
	for start < activeAt && rowWidth(labels[start:], sep)+2 > t.width-reserved {
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
		t.spans[shown[i]] = [2]int{col, col + w}
		row.WriteString(labels[i])
		col += w
	}
	t.moreSpan = [2]int{}
	if more != "" {
		t.moreSpan = [2]int{col, col + lipgloss.Width(more)}
		row.WriteString(more)
		col += lipgloss.Width(more)
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
