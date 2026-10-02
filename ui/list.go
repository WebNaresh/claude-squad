package ui

import (
	"claude-squad/log"
	"claude-squad/session"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
)

const readyIcon = "● "
const pausedIcon = "⏸ "

var readyStyle = lipgloss.NewStyle().
	Foreground(lipgloss.AdaptiveColor{Light: "#51bd73", Dark: "#51bd73"})

var addedLinesStyle = lipgloss.NewStyle().
	Foreground(lipgloss.AdaptiveColor{Light: "#51bd73", Dark: "#51bd73"})

var removedLinesStyle = lipgloss.NewStyle().
	Foreground(lipgloss.Color("#de613e"))

var pausedStyle = lipgloss.NewStyle().
	Foreground(lipgloss.AdaptiveColor{Light: "#888888", Dark: "#888888"})

var titleStyle = lipgloss.NewStyle().
	Padding(1, 1, 0, 1).
	Foreground(lipgloss.AdaptiveColor{Light: "#1a1a1a", Dark: "#dddddd"})

var listDescStyle = lipgloss.NewStyle().
	Padding(0, 1, 1, 1).
	Foreground(lipgloss.AdaptiveColor{Light: "#A49FA5", Dark: "#777777"})

var selectedTitleStyle = lipgloss.NewStyle().
	Padding(1, 1, 0, 1).
	Background(lipgloss.Color("#313131")).
	Foreground(lipgloss.AdaptiveColor{Light: "#1a1a1a", Dark: "#1a1a1a"})

var selectedDescStyle = lipgloss.NewStyle().
	Padding(0, 1, 1, 1).
	Background(lipgloss.Color("#313131")).
	Foreground(lipgloss.AdaptiveColor{Light: "#1a1a1a", Dark: "#1a1a1a"})

var externalHeaderStyle = lipgloss.NewStyle().
	Foreground(lipgloss.AdaptiveColor{Light: "#555555", Dark: "#aaaaaa"}).
	Underline(true)

var mainTitle = lipgloss.NewStyle().
	Background(lipgloss.Color("#0078d4")).
	Foreground(lipgloss.Color("#ffffff"))

var autoYesStyle = lipgloss.NewStyle().
	Background(lipgloss.Color("#313131")).
	Foreground(lipgloss.Color("#cccccc"))

type List struct {
	// all holds every instance across projects; items is the subset shown for
	// the active project. Indexes (selectedIdx etc.) always refer to items.
	all     []*session.Instance
	items   []*session.Instance
	project string
	// external holds Claude sessions started outside claude-squad; visible is
	// the subset in the active project, listed after items. selectedIdx values
	// at or past len(items) select an external session.
	external        []*session.ExternalSession
	visibleExternal []*session.ExternalSession
	selectedIdx     int
	height, width   int
	renderer        *InstanceRenderer
	autoyes         bool

	// map of repo name to number of instances using it. Used to display the repo name only if there are
	// multiple repos in play.
	repos map[string]int
}

func NewList(spinner *spinner.Model, autoYes bool) *List {
	return &List{
		items:    []*session.Instance{},
		renderer: &InstanceRenderer{spinner: spinner},
		repos:    make(map[string]int),
		autoyes:  autoYes,
	}
}

// SetSize sets the height and width of the list.
func (l *List) SetSize(width, height int) {
	l.width = width
	l.height = height
	l.renderer.setWidth(width)
}

// SetSessionPreviewSize sets the height and width for the tmux sessions. This makes the stdout line have the correct
// width and height.
func (l *List) SetSessionPreviewSize(width, height int) (err error) {
	for i, item := range l.all {
		if !item.Started() || item.Paused() {
			continue
		}

		if innerErr := item.SetPreviewSize(width, height); innerErr != nil {
			err = errors.Join(
				err, fmt.Errorf("could not set preview size for instance %d: %v", i, innerErr))
		}
	}
	return
}

func (l *List) NumInstances() int {
	return len(l.items)
}

// InstanceRenderer handles rendering of session.Instance objects
type InstanceRenderer struct {
	spinner *spinner.Model
	width   int
}

func (r *InstanceRenderer) setWidth(width int) {
	r.width = AdjustPreviewWidth(width)
}

// ɹ and ɻ are other options.
const branchIcon = "Ꮧ"

func (r *InstanceRenderer) Render(i *session.Instance, idx int, selected bool, hasMultipleRepos bool) string {
	prefix := fmt.Sprintf(" %d. ", idx)
	if idx >= 10 {
		prefix = prefix[:len(prefix)-1]
	}
	titleS := selectedTitleStyle
	descS := selectedDescStyle
	if !selected {
		titleS = titleStyle
		descS = listDescStyle
	}

	// add spinner next to title if it's running
	var join string
	switch i.Status {
	case session.Running, session.Loading:
		join = fmt.Sprintf("%s ", r.spinner.View())
	case session.Ready:
		join = readyStyle.Render(readyIcon)
	case session.Paused:
		join = pausedStyle.Render(pausedIcon)
	default:
	}

	// Cut the title if it's too long
	titleText := i.Title
	if i.NeedsYou {
		titleText = "❓ " + titleText
	}
	widthAvail := r.width - 3 - runewidth.StringWidth(prefix) - 1
	if widthAvail > 0 && runewidth.StringWidth(titleText) > widthAvail {
		titleText = runewidth.Truncate(titleText, widthAvail-3, "...")
	}
	title := titleS.Render(lipgloss.JoinHorizontal(
		lipgloss.Left,
		lipgloss.Place(r.width-3, 1, lipgloss.Left, lipgloss.Center, fmt.Sprintf("%s %s", prefix, titleText)),
		" ",
		join,
	))

	stat := i.GetDiffStats()

	var diff string
	var addedDiff, removedDiff string
	if stat == nil || stat.Error != nil || stat.IsEmpty() {
		// Don't show diff stats if there's an error or if they don't exist
		addedDiff = ""
		removedDiff = ""
		diff = ""
	} else {
		addedDiff = fmt.Sprintf("+%d", stat.Added)
		removedDiff = fmt.Sprintf("-%d ", stat.Removed)
		diff = lipgloss.JoinHorizontal(
			lipgloss.Center,
			addedLinesStyle.Background(descS.GetBackground()).Render(addedDiff),
			lipgloss.Style{}.Background(descS.GetBackground()).Foreground(descS.GetForeground()).Render(","),
			removedLinesStyle.Background(descS.GetBackground()).Render(removedDiff),
		)
	}

	remainingWidth := r.width
	remainingWidth -= runewidth.StringWidth(prefix)
	remainingWidth -= runewidth.StringWidth(branchIcon)
	remainingWidth -= 2 // for the literal " " and "-" in the branchLine format string

	diffWidth := runewidth.StringWidth(addedDiff) + runewidth.StringWidth(removedDiff)
	if diffWidth > 0 {
		diffWidth += 1
	}

	// Use fixed width for diff stats to avoid layout issues
	remainingWidth -= diffWidth

	branch := i.Branch
	if i.Started() && hasMultipleRepos {
		repoName, err := i.RepoName()
		if err != nil {
			log.ErrorLog.Printf("could not get repo name in instance renderer: %v", err)
		} else {
			branch += fmt.Sprintf(" (%s)", repoName)
		}
	}
	// Don't show branch if there's no space for it. Or show ellipsis if it's too long.
	branchWidth := runewidth.StringWidth(branch)
	if remainingWidth < 0 {
		branch = ""
	} else if remainingWidth < branchWidth {
		if remainingWidth < 3 {
			branch = ""
		} else {
			// We know the remainingWidth is at least 4 and branch is longer than that, so this is safe.
			branch = runewidth.Truncate(branch, remainingWidth-3, "...")
		}
	}
	remainingWidth -= runewidth.StringWidth(branch)

	// Add spaces to fill the remaining width.
	spaces := ""
	if remainingWidth > 0 {
		spaces = strings.Repeat(" ", remainingWidth)
	}

	branchLine := fmt.Sprintf("%s %s-%s%s%s", strings.Repeat(" ", len(prefix)), branchIcon, branch, spaces, diff)

	// join title and subtitle
	text := lipgloss.JoinVertical(
		lipgloss.Left,
		title,
		descS.Render(branchLine),
	)

	return text
}

// RenderExternal renders one external session row: its name and whether a
// terminal is also attached to it.
func (r *InstanceRenderer) RenderExternal(e *session.ExternalSession, selected bool) string {
	titleS, descS := titleStyle, listDescStyle
	if selected {
		titleS, descS = selectedTitleStyle, selectedDescStyle
	}
	title := e.Title()
	if e.NeedsYou() {
		title = "❓ " + title
	}
	title = runewidth.Truncate(title, max(0, r.width-6), "...")
	where := e.Describe()
	return lipgloss.JoinVertical(lipgloss.Left,
		titleS.Render(lipgloss.Place(r.width-2, 1, lipgloss.Left, lipgloss.Center, " ⧉  "+title)),
		descS.Render(lipgloss.Place(r.width-2, 1, lipgloss.Left, lipgloss.Center, "    "+where)),
	)
}

func (l *List) String() string {
	const titleText = " Instances "
	const autoYesText = " auto-yes "

	// Write the title.
	var b strings.Builder
	b.WriteString("\n")
	b.WriteString("\n")

	// Write title line
	// add padding of 2 because the border on list items adds some extra characters
	titleWidth := AdjustPreviewWidth(l.width) + 2
	if !l.autoyes {
		b.WriteString(lipgloss.Place(
			titleWidth, 1, lipgloss.Left, lipgloss.Bottom, mainTitle.Render(titleText)))
	} else {
		title := lipgloss.Place(
			titleWidth/2, 1, lipgloss.Left, lipgloss.Bottom, mainTitle.Render(titleText))
		autoYes := lipgloss.Place(
			titleWidth-(titleWidth/2), 1, lipgloss.Right, lipgloss.Bottom, autoYesStyle.Render(autoYesText))
		b.WriteString(lipgloss.JoinHorizontal(
			lipgloss.Top, title, autoYes))
	}

	b.WriteString("\n")
	b.WriteString("\n")

	// Render the list.
	for i, item := range l.items {
		b.WriteString(l.renderer.Render(item, i+1, i == l.selectedIdx, len(l.repos) > 1))
		if i != len(l.items)-1 {
			b.WriteString("\n\n")
		}
	}

	if len(l.visibleExternal) > 0 {
		b.WriteString("\n\n")
		b.WriteString(externalHeaderStyle.Render(" Claude sessions "))
		b.WriteString("\n")
		for i, e := range l.visibleExternal {
			b.WriteString("\n")
			b.WriteString(l.renderer.RenderExternal(e, len(l.items)+i == l.selectedIdx))
		}
	}
	return lipgloss.Place(l.width, l.height, lipgloss.Left, lipgloss.Top, b.String())
}

// Down selects the next item in the list.
func (l *List) Down() {
	if l.total() == 0 {
		return
	}
	if l.selectedIdx < l.total()-1 {
		l.selectedIdx++
	} else {
		l.selectedIdx = 0
	}
}

// Kill selects the next item in the list.
func (l *List) Kill() {
	if l.selectedIdx >= len(l.items) {
		return
	}
	targetInstance := l.items[l.selectedIdx]

	// Kill the tmux session
	if err := targetInstance.Kill(); err != nil {
		log.ErrorLog.Printf("could not kill instance: %v", err)
	}

	// If you delete the last one in the list, select the previous one.
	if l.selectedIdx == len(l.items)-1 {
		defer l.Up()
	}

	// Unregister the reponame.
	repoName, err := targetInstance.RepoName()
	if err != nil {
		log.ErrorLog.Printf("could not get repo name: %v", err)
	} else {
		l.rmRepo(repoName)
	}

	for i, inst := range l.all {
		if inst == targetInstance {
			l.all = append(l.all[:i], l.all[i+1:]...)
			break
		}
	}
	// Since there's items after this, the selectedIdx can stay the same.
	l.items = append(l.items[:l.selectedIdx], l.items[l.selectedIdx+1:]...)
}

func (l *List) Attach() (chan struct{}, error) {
	if e := l.GetSelectedExternal(); e != nil {
		return e.Attach()
	}
	targetInstance := l.items[l.selectedIdx]
	return targetInstance.Attach()
}

// Up selects the prev item in the list.
func (l *List) Up() {
	if l.total() == 0 {
		return
	}
	if l.selectedIdx > 0 {
		l.selectedIdx--
	} else {
		l.selectedIdx = l.total() - 1
	}
}

// total counts selectable rows: agents plus external sessions.
func (l *List) total() int {
	return len(l.items) + len(l.visibleExternal)
}

func (l *List) addRepo(repo string) {
	if _, ok := l.repos[repo]; !ok {
		l.repos[repo] = 0
	}
	l.repos[repo]++
}

func (l *List) rmRepo(repo string) {
	if _, ok := l.repos[repo]; !ok {
		log.ErrorLog.Printf("repo %s not found", repo)
		return
	}
	l.repos[repo]--
	if l.repos[repo] == 0 {
		delete(l.repos, repo)
	}
}

// AddInstance adds a new instance to the list. It returns a finalizer function that should be called when the instance
// is started. If the instance was restored from storage or is paused, you can call the finalizer immediately.
// When creating a new one and entering the name, you want to call the finalizer once the name is done.
func (l *List) AddInstance(instance *session.Instance) (finalize func()) {
	l.all = append(l.all, instance)
	if InProject(instance, l.project) {
		l.items = append(l.items, instance)
	}
	// The finalizer registers the repo name once the instance is started.
	return func() {
		repoName, err := instance.RepoName()
		if err != nil {
			log.ErrorLog.Printf("could not get repo name: %v", err)
			return
		}

		l.addRepo(repoName)
	}
}

// GetSelectedInstance returns the currently selected instance
func (l *List) GetSelectedInstance() *session.Instance {
	if l.selectedIdx >= len(l.items) {
		return nil
	}
	return l.items[l.selectedIdx]
}

// Row is one selectable list entry: an agent or an external session.
type Row struct {
	Instance *session.Instance
	External *session.ExternalSession
}

// VisibleRows returns the rows of the active project in list order.
func (l *List) VisibleRows() []Row {
	rows := make([]Row, 0, l.total())
	for _, inst := range l.items {
		rows = append(rows, Row{Instance: inst})
	}
	for _, e := range l.visibleExternal {
		rows = append(rows, Row{External: e})
	}
	return rows
}

// SelectedIndex returns the selected row's index in VisibleRows.
func (l *List) SelectedIndex() int { return l.selectedIdx }

// SelectIndex selects a row by its index in VisibleRows.
func (l *List) SelectIndex(i int) {
	if i >= 0 && i < l.total() {
		l.selectedIdx = i
	}
}

// GetSelectedExternal returns the selected external session, or nil when an
// agent (or nothing) is selected.
func (l *List) GetSelectedExternal() *session.ExternalSession {
	i := l.selectedIdx - len(l.items)
	if i < 0 || i >= len(l.visibleExternal) {
		return nil
	}
	return l.visibleExternal[i]
}

// SetExternal replaces the external sessions, keeping the same one selected
// when it still exists.
func (l *List) SetExternal(sessions []*session.ExternalSession) {
	var selected string
	if e := l.GetSelectedExternal(); e != nil {
		selected = e.Name
	}
	l.external = sessions
	l.filterExternal()
	for i, e := range l.visibleExternal {
		if e.Name == selected {
			l.selectedIdx = len(l.items) + i
			return
		}
	}
	if l.selectedIdx >= l.total() {
		l.selectedIdx = max(0, l.total()-1)
	}
}

func (l *List) filterExternal() {
	l.visibleExternal = nil
	for _, e := range l.external {
		if inProjectPath(e.Path, l.project) {
			l.visibleExternal = append(l.visibleExternal, e)
		}
	}
}

// SelectExternal selects the external session with the given name, if listed.
func (l *List) SelectExternal(name string) {
	for i, e := range l.visibleExternal {
		if e.Name == name {
			l.selectedIdx = len(l.items) + i
			return
		}
	}
}

// ExternalSessions returns every external session across projects.
func (l *List) ExternalSessions() []*session.ExternalSession {
	return l.external
}

// SetSelectedInstance sets the selected index. Noop if the index is out of bounds.
func (l *List) SetSelectedInstance(idx int) {
	if idx >= len(l.items) {
		return
	}
	l.selectedIdx = idx
}

// SelectInstance finds and selects the given instance in the list.
func (l *List) SelectInstance(target *session.Instance) {
	for i, inst := range l.items {
		if inst == target {
			l.SetSelectedInstance(i)
			return
		}
	}
}

// MoveUp swaps the selected instance with the one above it.
func (l *List) MoveUp() bool {
	if l.selectedIdx <= 0 || l.selectedIdx >= len(l.items) || len(l.items) < 2 {
		return false
	}
	l.swapInAll(l.items[l.selectedIdx], l.items[l.selectedIdx-1])
	l.items[l.selectedIdx], l.items[l.selectedIdx-1] = l.items[l.selectedIdx-1], l.items[l.selectedIdx]
	l.selectedIdx--
	return true
}

// MoveDown swaps the selected instance with the one below it.
func (l *List) MoveDown() bool {
	if l.selectedIdx >= len(l.items)-1 || len(l.items) < 2 {
		return false
	}
	l.swapInAll(l.items[l.selectedIdx], l.items[l.selectedIdx+1])
	l.items[l.selectedIdx], l.items[l.selectedIdx+1] = l.items[l.selectedIdx+1], l.items[l.selectedIdx]
	l.selectedIdx++
	return true
}

// GetInstances returns all instances in the list, across every project.
func (l *List) GetInstances() []*session.Instance {
	return l.all
}

// swapInAll swaps the positions of a and b in the full instance list so that
// reordering within a project is kept when saving.
func (l *List) swapInAll(a, b *session.Instance) {
	ia, ib := -1, -1
	for i, inst := range l.all {
		if inst == a {
			ia = i
		} else if inst == b {
			ib = i
		}
	}
	if ia >= 0 && ib >= 0 {
		l.all[ia], l.all[ib] = l.all[ib], l.all[ia]
	}
}

// InProject reports whether the instance was started inside the project folder.
// An empty project matches every instance.
func InProject(instance *session.Instance, project string) bool {
	return inProjectPath(instance.Path, project)
}

func inProjectPath(path, project string) bool {
	if project == "" {
		return true
	}
	return path == project || strings.HasPrefix(path, project+string(filepath.Separator))
}

// SetProject shows only the instances belonging to project.
func (l *List) SetProject(project string) {
	l.project = project
	l.items = l.items[:0:0]
	for _, inst := range l.all {
		if InProject(inst, project) {
			l.items = append(l.items, inst)
		}
	}
	l.filterExternal()
	l.selectedIdx = 0
}

// Remove drops an instance from the list without touching its session.
func (l *List) Remove(target *session.Instance) {
	for i, inst := range l.all {
		if inst == target {
			l.all = append(l.all[:i], l.all[i+1:]...)
			break
		}
	}
	for i, inst := range l.items {
		if inst == target {
			l.items = append(l.items[:i], l.items[i+1:]...)
			if l.selectedIdx >= len(l.items) && l.selectedIdx > 0 {
				l.selectedIdx--
			}
			break
		}
	}
}

// CountInProject returns how many agents belong to project.
func (l *List) CountInProject(project string) int {
	n := 0
	for _, inst := range l.all {
		if InProject(inst, project) {
			n++
		}
	}
	return n
}

// CountNeedsInProject returns how many sessions in project wait on the user.
func (l *List) CountNeedsInProject(project string) int {
	n := 0
	for _, inst := range l.all {
		if inst.NeedsYou && InProject(inst, project) {
			n++
		}
	}
	for _, e := range l.external {
		if e.NeedsYou() && inProjectPath(e.Path, project) {
			n++
		}
	}
	return n
}

// NextNeedsYou finds the next session waiting on the user, looking through
// projects in tab order starting at active (after the current selection
// there). It returns the project and either the agent or the external session.
func (l *List) NextNeedsYou(projects []string, active string) (string, *session.Instance, *session.ExternalSession) {
	if len(projects) == 0 {
		return "", nil, nil
	}
	start := 0
	for i, p := range projects {
		if p == active {
			start = i
		}
	}
	for k := 0; k <= len(projects); k++ {
		p := projects[(start+k)%len(projects)]
		var rows []any
		for _, inst := range l.all {
			if InProject(inst, p) {
				rows = append(rows, inst)
			}
		}
		for _, e := range l.external {
			if inProjectPath(e.Path, p) {
				rows = append(rows, e)
			}
		}
		from := 0
		if k == 0 {
			from = l.selectedIdx + 1 // in the active tab, look past the selection first
		} else if k == len(projects) {
			from = 0 // wrapped around to the active tab: check what we skipped
		}
		for i := from; i < len(rows); i++ {
			switch r := rows[i].(type) {
			case *session.Instance:
				if r.NeedsYou {
					return p, r, nil
				}
			case *session.ExternalSession:
				if r.NeedsYou() {
					return p, nil, r
				}
			}
		}
	}
	return "", nil, nil
}

// CountExternalInProject returns how many external sessions run in project.
func (l *List) CountExternalInProject(project string) int {
	n := 0
	for _, e := range l.external {
		if inProjectPath(e.Path, project) {
			n++
		}
	}
	return n
}
