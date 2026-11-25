package ui

import (
	"claude-squad/session"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/mattn/go-runewidth"
	"github.com/muesli/reflow/wordwrap"
)

var (
	ipBoxStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.Color("#0078d4")).Padding(0, 1)
	ipTitleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#0078d4"))
	ipDimStyle    = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#888888", Dark: "#888888"})
	ipCursorStyle = lipgloss.NewStyle().Background(lipgloss.Color("#04395e")).Foreground(lipgloss.Color("#ffffff"))
)

// IssuePickDefault is how many of the first issues are ticked when the
// picker opens.
const IssuePickDefault = 5

// IssuePicker lists a project's open issues (oldest or newest first) with checkboxes,
// to start one Claude session per ticked issue.
type IssuePicker struct {
	Project string
	Name    string // the project's tab name
	Loading bool
	Err     error
	issues  []session.Issue
	inPR    int  // open issues hidden because an open PR already closes them
	stale   bool // showing the last list while a fresh one loads
	// refreshed says when the last reload landed, for the footer: an r that
	// brought the same list changed nothing on screen and looked ignored.
	refreshed string
	loadStart time.Time    // when the reload in flight started, for "refreshing… 5s"
	busy      map[int]bool // issues that already have a session
	skipped   map[int]bool // issues the user unticked: not ticked by default again
	// NewestFirst lists (and ticks) the newest issues first instead of the
	// oldest; s switches it, saved per project by the caller.
	NewestFirst bool
	// TickLimit is how many issues are ticked when the list arrives (0:
	// IssuePickDefault). The auto loop sets it to the free session slots.
	TickLimit int
	// Auto says the auto loop opened the picker and why ("1 free slot …");
	// AutoOn is whether the loop runs for this project (a switches it).
	Auto   string
	AutoOn bool
	// Limit is this project's session limit, LimitAll the all-projects one,
	// LimitOwn whether the project has its own. EditAll: +/- change LimitAll
	// (g switches). The caller saves them; the picker only shows them.
	Limit, LimitAll   int
	LimitOwn, EditAll bool
	selected          map[int]bool
	// Me and Pinned decide whose issues come first (session.Issue.Priority);
	// the caller saves Pinned.
	Me     string
	Pinned []string
	all    []session.Issue // every listed issue; issues is all, or only the people picked
	taken  []session.Issue // the ones hidden because a PR has them
	people peopleList
	groups map[int]int // each issue's group when the list was last arranged
	cursor int
	scroll int // preview scroll offset
	width  int
	height int
}

// NewIssuePicker opens the picker for a project while its issues load.
func NewIssuePicker(project, name string) *IssuePicker {
	return &IssuePicker{Project: project, Name: name, Loading: true, selected: map[int]bool{}, skipped: map[int]bool{}}
}

// SetSkipped sets the issues the user skipped before (kept per project).
func (p *IssuePicker) SetSkipped(skipped map[int]bool) {
	if skipped != nil {
		p.skipped = skipped
	}
}

// Skipped returns the skipped issues still open, for saving. Closed ones
// drop out of the list.
func (p *IssuePicker) Skipped() []int {
	var out []int
	for _, is := range p.all {
		if p.skipped[is.Number] {
			out = append(out, is.Number)
		}
	}
	return out
}

// SetIssues shows a project's issues, leaving out the ones an open PR
// already closes. The first list ticks the first IssuePickDefault (in list
// order) that don't have a session and weren't skipped; a refresh of the list keeps the ticks and the cursor.
func (p *IssuePicker) SetIssues(issues []session.Issue, busy map[int]bool, err error) {
	refresh := p.all != nil
	var cur int
	if refresh && p.cursor < len(p.issues) {
		cur = p.issues[p.cursor].Number
	}
	var shown, taken []session.Issue
	for _, is := range issues { // issues come oldest first
		if is.PR > 0 {
			taken = append(taken, is)
			continue
		}
		shown = append(shown, is)
	}
	if err != nil && refresh {
		// Keep showing the last list; just say the refresh failed.
		p.Err, p.Loading, p.stale = nil, false, false
		return
	}
	if refresh && p.stale && err == nil {
		was := map[int]bool{}
		for _, is := range p.all {
			was[is.Number] = true
		}
		added := 0
		for _, is := range shown {
			if !was[is.Number] {
				added++
			}
		}
		p.refreshed = fmt.Sprintf("✓ refreshed %s: %d listed", time.Now().Format("15:04:05"), len(shown))
		if added > 0 {
			p.refreshed += fmt.Sprintf(", %d new", added)
		}
	}
	next, _ := p.moved()
	p.all, p.taken, p.inPR = shown, taken, len(taken)
	p.busy, p.Err, p.Loading, p.stale = busy, err, false, false
	p.issues = p.only(shown)
	p.arrange()
	if p.people.open {
		p.reloadPeople() // a load that ended while the people list is open
	}
	if refresh {
		if next != 0 {
			cur = next // the cursor's own row changed group: don't follow it there
		}
		keep := map[int]bool{}
		p.cursor = 0
		for i, is := range p.issues {
			if p.selected[is.Number] && !busy[is.Number] {
				keep[is.Number] = true
			}
			if is.Number == cur {
				p.cursor = i
			}
		}
		p.selected = keep
		return
	}
	p.selected, p.cursor = map[int]bool{}, 0
	p.tickDefault()
}

// tickDefault ticks the first IssuePickDefault issues in list order that
// have no session and weren't skipped.
func (p *IssuePicker) tickDefault() {
	limit := p.TickLimit
	if limit <= 0 {
		limit = IssuePickDefault
	}
	p.tickFirst(limit)
}

// tickFirst ticks issues in list order, skipping ones with a session or
// skipped, until limit are ticked.
func (p *IssuePicker) tickFirst(limit int) {
	for _, is := range p.issues {
		if len(p.selected) == limit {
			break
		}
		if !p.busy[is.Number] && !p.skipped[is.Number] && !is.Waits() {
			p.selected[is.Number] = true
		}
	}
}

// Groups of the list, in the order they show. The first three are ready:
// pinned people's, other people's, then your own and nobody's (as in
// session.Issue.Priority). Only those can be started by auto or the digits;
// the rest used to fill the top of the list, because old issues are the ones
// that get skipped.
const (
	groupPinned = session.PriorityPinned
	groupPeople = session.PriorityPeople
	groupRest   = session.PriorityRest
	groupReady  = groupRest // up to here
	groupBusy   = iota - 1
	groupWaits
	groupSkipped
)

func (p *IssuePicker) group(is session.Issue) int {
	switch {
	case p.busy[is.Number]:
		return groupBusy
	case p.skipped[is.Number]:
		return groupSkipped
	case is.Waits():
		return groupWaits
	}
	return is.Priority(p.Me, p.Pinned)
}

// arrange orders the list: ready issues first, then the ones on hold, each
// group by age.
func (p *IssuePicker) arrange() {
	slices.SortStableFunc(p.issues, func(a, b session.Issue) int {
		if c := p.group(a) - p.group(b); c != 0 {
			return c
		}
		if c := a.Priority(p.Me, p.Pinned) - b.Priority(p.Me, p.Pinned); c != 0 {
			return c // on hold
		}
		c := a.CreatedAt.Compare(b.CreatedAt)
		if c == 0 {
			c = a.Number - b.Number
		}
		if p.NewestFirst {
			c = -c
		}
		return c
	})
	p.groups = map[int]int{}
	for _, is := range p.issues {
		p.groups[is.Number] = p.group(is)
	}
}

// readyCount is how many rows are above the on-hold line.
func (p *IssuePicker) readyCount() int {
	n := 0
	for _, is := range p.issues {
		if p.groups[is.Number] <= groupReady {
			n++
		}
	}
	return n
}

// moved reports whether x or space changed the group of the cursor's issue
// since the list was arranged, and the issue the cursor goes to when that row
// moves away (0: none).
func (p *IssuePicker) moved() (next int, ok bool) {
	if p.cursor >= len(p.issues) {
		return 0, false
	}
	is := p.issues[p.cursor]
	if p.groups[is.Number] == p.group(is) {
		return 0, false
	}
	switch {
	case p.cursor+1 < len(p.issues):
		next = p.issues[p.cursor+1].Number
	case p.cursor > 0:
		next = p.issues[p.cursor-1].Number
	}
	return next, true
}

// moveTo puts the cursor on row i. A row skipped or un-skipped with x stays
// under the cursor, so x again takes it back, and goes to its group here,
// once the cursor leaves it.
func (p *IssuePicker) moveTo(i int) {
	if i < 0 || i >= len(p.issues) || i == p.cursor {
		return
	}
	target := p.issues[i].Number
	if _, ok := p.moved(); ok {
		p.arrange()
	}
	p.cursor = max(0, slices.IndexFunc(p.issues, func(is session.Issue) bool { return is.Number == target }))
	p.scroll = 0
}

// holdLine heads the issues on hold: how many, why, and the key to get there.
func (p *IssuePicker) holdLine(width int) string {
	var n [groupSkipped + 1]int
	for _, is := range p.issues {
		n[p.group(is)]++
	}
	var parts []string
	for _, g := range []struct {
		group int
		what  string
	}{{groupBusy, "with a session"}, {groupWaits, "waiting"}, {groupSkipped, "skipped"}} {
		if n[g.group] > 0 {
			parts = append(parts, fmt.Sprintf("%d %s", n[g.group], g.what))
		}
	}
	return ruleLine("On hold: "+strings.Join(parts, " · "), width)
}

// ruleLine is a heading row inside the list.
func ruleLine(text string, width int) string {
	line := "── " + text + " "
	line = runewidth.Truncate(line, width, "…")
	return line + strings.Repeat("─", max(0, width-runewidth.StringWidth(line)))
}

// ToggleOrder switches between oldest and newest first, moving to the top
// and ticking the first issues of the new order.
func (p *IssuePicker) ToggleOrder() {
	p.NewestFirst = !p.NewestFirst
	p.arrange()
	p.cursor = 0
	p.selected = map[int]bool{}
	p.tickDefault()
	p.scroll = 0
}

// ShowCached shows the last list of the project at once, until SetIssues
// brings the fresh one.
func (p *IssuePicker) ShowCached(issues []session.Issue, busy map[int]bool) {
	p.SetIssues(issues, busy, nil)
	p.stale = true
}

// StartRefresh marks a reload of the list in flight: the list on screen
// stays, with "refreshing…" in the footer, until SetIssues brings the new one.
func (p *IssuePicker) StartRefresh() { p.stale, p.refreshed, p.loadStart = true, "", time.Now() }

// RefreshingFor is how long the reload in flight has run (0: none).
func (p *IssuePicker) RefreshingFor() time.Duration {
	if !p.stale || p.loadStart.IsZero() {
		return 0
	}
	return time.Since(p.loadStart)
}

// Refreshing reports a reload in flight (the list on screen is the old one).
func (p *IssuePicker) Refreshing() bool { return p.stale }

func (p *IssuePicker) SetSize(w, h int) { p.width, p.height = w, h }

// Current returns the issue under the cursor.
func (p *IssuePicker) Current() (session.Issue, bool) {
	if p.Loading || p.cursor >= len(p.issues) {
		return session.Issue{}, false
	}
	return p.issues[p.cursor], true
}

// HandleKey processes a key; it returns done (close the picker) and start
// (start sessions for Selected()).
func (p *IssuePicker) HandleKey(key string) (done, start bool) {
	if p.people.open {
		p.peopleKey(key)
		return false, false
	}
	switch key {
	case "up", "k":
		p.moveTo(p.cursor - 1)
	case "down", "j":
		p.moveTo(p.cursor + 1)
	case "tab":
		// To the first issue on hold, and back to the top.
		if p.cursor < p.readyCount() {
			p.moveTo(p.readyCount())
		} else {
			p.moveTo(0)
		}
	case "pgdown", "shift+down", "J":
		p.scroll += 5
	case "pgup", "shift+up", "K":
		p.scroll = max(0, p.scroll-5)
	case " ":
		// Tick or untick only. Unticking used to skip the issue too, so
		// trimming a list to the 3 you wanted skipped the rest for good.
		if p.cursor < len(p.issues) {
			n := p.issues[p.cursor].Number
			if !p.busy[n] {
				p.selected[n] = !p.selected[n]
				if p.selected[n] {
					delete(p.skipped, n) // starting it on purpose takes the skip back
				}
			}
		}
	case "x":
		// Skip: never ticked by default or offered by auto, but kept in the
		// list (greyed) so it can be started by hand any time. x again
		// takes it back.
		if p.cursor < len(p.issues) {
			n := p.issues[p.cursor].Number
			if !p.busy[n] {
				if p.skipped[n] {
					delete(p.skipped, n)
				} else {
					p.skipped[n] = true
					delete(p.selected, n)
				}
			}
		}
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		// Start exactly N: tick the first N in list order, nothing else.
		p.selected = map[int]bool{}
		p.tickFirst(int(key[0] - '0'))
	case "f":
		if !p.Loading && p.Err == nil {
			p.openPeople()
		}
	case "enter":
		return true, len(p.Selected()) > 0
	case "esc", "q", "ctrl+c":
		return true, false
	}
	return false, false
}

// Selected returns the ticked issues in list order (the order they start).
func (p *IssuePicker) Selected() []session.Issue {
	var out []session.Issue
	for _, is := range p.issues {
		if p.selected[is.Number] {
			out = append(out, is)
		}
	}
	return out
}

func (p *IssuePicker) Render() string {
	w := max(40, min(p.width-4, 120))
	var b strings.Builder
	order, other := "oldest first", "newest first"
	if p.NewestFirst {
		order, other = other, order
	}
	// tab's key sits here, not in the key row, which is cut at the right.
	if hold := len(p.issues) - p.readyCount(); hold > 0 {
		where := "on hold"
		if p.cursor >= p.readyCount() {
			where = "back to top"
		}
		order += fmt.Sprintf(" · %d ready · %d on hold · tab: %s", len(p.issues)-hold, hold, where)
		order = runewidth.Truncate(order, max(0, w-4-len("Start Claude on issues   project: ‹  ›   ")-runewidth.StringWidth(p.Name)), "…")
	}
	b.WriteString(ipTitleStyle.Render("Start Claude on issues") + "   project: " + ipTitleStyle.Render("‹ "+p.Name+" ›") + "   " + ipDimStyle.Render(order) + "\n")
	if p.people.open {
		b.WriteString(KeyRow("", []Key{{"enter", "show only"}, {"space", "tick several"}, {"p", "pin (always first)"}, {"esc", "back"}}, w-4) + "\n")
		b.WriteString(p.limitLine() + "\n")
	} else {
		b.WriteString(KeyRow("", []Key{{"enter", "start"}, {"esc", "cancel"}, {"space", "tick"}, {"x", "skip"}, {"r", "refresh"}, {"1-9", "tick first N"}, {"↑↓", "move"},
			{"s", other}, {"←→", "project"}, {"o", "open"}, {"a", autoLabel(p.AutoOn)}, {"J/K", "scroll"}}, w-4) + "\n")
		// Its keys sit on this line: at the end of the row above they'd be the
		// first to fall behind its "…".
		limit := p.limitLine()
		// In a very narrow window there is no room beside it, not even for "…".
		if rest := w - 4 - lipgloss.Width(limit) - 3; rest > 0 {
			limit += "   " + KeyRow("", []Key{{"f", p.peopleLabel()}, {"+/-", "change"}, {"g", limitScopeLabel(p.EditAll)}}, rest)
		}
		b.WriteString(limit + "\n")
	}
	if p.Auto != "" {
		b.WriteString(ipTitleStyle.Render("⟳ Auto: ") + p.Auto + "\n")
	}
	b.WriteString("\n")
	// Loading, error and empty states take the full list's height too, so
	// the picker doesn't jump when the list arrives.
	avail := max(12, p.height-10)
	full := strings.Count(b.String(), "\n") + avail + 1
	fixed := func(s string) string {
		if n := strings.Count(s, "\n") + 1; n < full {
			s += strings.Repeat("\n", full-n)
		}
		return ipBoxStyle.Width(w).Render(s)
	}
	if p.Loading {
		b.WriteString("Loading issues…")
		return fixed(b.String())
	}
	if p.Err != nil {
		b.WriteString(p.Err.Error())
		return fixed(b.String())
	}
	if p.people.open {
		return fixed(b.String() + p.renderPeople(w-4, avail))
	}
	if len(p.issues) == 0 {
		if p.filtered() {
			b.WriteString(p.noneFrom())
		} else if p.inPR > 0 {
			b.WriteString(fmt.Sprintf("No open issues left: the other %d are already in a PR (open or merged).", p.inPR))
		} else {
			b.WriteString("No open issues.")
		}
		return fixed(b.String())
	}
	// Rows left for the list and the issue text: the screen minus the border,
	// the 3 header and 2 footer rows and a little margin. The list takes about
	// 40% and the highlighted issue's text the rest.
	room := max(5, avail*2/5) // list rows; fixed, so the picker keeps its height
	// Heading lines are rows of their own: before your own issues (when other
	// people's are above them) and before the ones on hold.
	ready := p.readyCount()
	asked := 0
	for asked < ready && p.groups[p.issues[asked].Number] < groupRest {
		asked++
	}
	const restRow, holdRow = -1, -2
	var list []int // an issue's index, or a heading
	cursorRow, holdAt := 0, 0
	for i := range p.issues {
		if i == asked && asked > 0 && asked < ready {
			list = append(list, restRow)
		}
		if i == ready {
			holdAt = len(list)
			list = append(list, holdRow)
		}
		if i == p.cursor {
			cursorRow = len(list)
		}
		list = append(list, i)
	}
	rows := len(list)
	start := max(0, cursorRow-room+1)
	if p.cursor >= ready {
		// On hold: show as many of them as fit, not just the first.
		start = max(start, min(holdAt, rows-room))
	}
	nameW := p.nameWidth(w)
	for row := start; row < rows && row < start+room; row++ {
		i := list[row]
		switch i {
		case restRow:
			b.WriteString(ipDimStyle.Render(ruleLine(fmt.Sprintf("Your own, or no name: %d", ready-asked), w-4)) + "\n")
			continue
		case holdRow:
			b.WriteString(ipDimStyle.Render(p.holdLine(w-4)) + "\n")
			continue
		}
		is := p.issues[i]
		box := "[ ]"
		switch {
		case p.busy[is.Number]:
			box = "[~]"
		case p.selected[is.Number]:
			box = "[x]"
		}
		age := ageString(time.Since(is.CreatedAt))
		title := is.Title
		switch {
		case p.busy[is.Number]:
			title = "(has a session) " + title
		case p.skipped[is.Number]:
			title = "(skipped) " + title
		case is.Waits():
			title = "(" + is.WaitReason() + ") " + title
		case is.QAFailed:
			title = "(failed testing, again) " + title
		}
		line := fmt.Sprintf("%s #%-5d %s%s", box, is.Number, p.nameCell(is, nameW), title)
		line = runewidth.Truncate(line, w-8, "…")
		line += strings.Repeat(" ", max(1, w-4-runewidth.StringWidth(line)-len(age))) + age
		if p.busy[is.Number] || p.skipped[is.Number] || is.Waits() {
			line = ipDimStyle.Render(line)
		} else {
			line = strings.TrimSuffix(line, age) + ipDimStyle.Render(age)
		}
		if i == p.cursor {
			line = ipCursorStyle.Render(line)
		}
		b.WriteString(line + "\n")
	}
	for i := rows - start; i < room; i++ {
		b.WriteString("\n") // fewer issues than rows: keep the list's height
	}
	b.WriteString(p.renderPreview(w-4, avail-room-1))
	foot := fmt.Sprintf("%d ticked · each gets its own session running gai issue, one after another", len(p.Selected()))
	if p.inPR > 0 {
		foot += fmt.Sprintf(" · %d hidden: already in a PR (open or merged)", p.inPR)
	}
	if p.stale {
		took := ""
		if d := p.RefreshingFor(); d >= time.Second {
			took = fmt.Sprintf(" %ds", int(d.Seconds()))
		}
		foot = "⟳ refreshing from GitHub…" + took + " · " + foot // first: the footer is cut at the right
	} else if p.refreshed != "" {
		foot = p.refreshed + " · " + foot
	}
	b.WriteString("\n" + ipDimStyle.Render(runewidth.Truncate(foot, w-4, "…")))
	return ipBoxStyle.Width(w).Render(b.String())
}

// renderPreview shows the highlighted issue's full title and body as plain
// text in exactly rows lines (styled markdown carries escape codes the
// overlay can't place), scrolled by p.scroll.
func (p *IssuePicker) renderPreview(width, rows int) string {
	is := p.issues[p.cursor]
	head := []string{ipDimStyle.Render(strings.Repeat("─", width))}
	for _, l := range wrapLines(fmt.Sprintf("#%d %s", is.Number, is.Title), width) {
		head = append(head, ipTitleStyle.Render(l))
	}
	by := is.URL
	if len(is.People) > 0 {
		by = "by " + strings.Join(is.People, ", ") + " · " + is.URL
	}
	head = append(head, ipDimStyle.Render(runewidth.Truncate(by, width, "…")))
	body := plainMarkdown(is.Body)
	if body == "" {
		body = "(no description)"
	}
	lines := wrapLines(body, width)
	bodyRows := max(1, rows-len(head)-1) // one row kept for the "more" hint
	p.scroll = min(p.scroll, max(0, len(lines)-bodyRows))
	shown := lines[p.scroll:min(len(lines), p.scroll+bodyRows)]
	out := append(head, shown...)
	if rest := len(lines) - p.scroll - len(shown); rest > 0 {
		out = append(out, ipDimStyle.Render(fmt.Sprintf("… %d more lines (J/K scroll, o open in browser)", rest)))
	}
	// Always the same height, whatever the issue's length: a short issue
	// used to shrink the whole picker and a long one grow it, so it jumped
	// on every ↑↓. Longer text scrolls (J/K).
	for len(out) < rows {
		out = append(out, "")
	}
	return "\n" + strings.Join(out, "\n")
}

var (
	mdImageRe = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)|<img[^>]*>`)
	mdLinkRe  = regexp.MustCompile(`\[([^\]]+)\]\(([^)]+)\)`)
	mdHeadRe  = regexp.MustCompile(`(?m)^#{1,6}\s*`)
	mdTagRe   = regexp.MustCompile(`</?[a-zA-Z][^>]*>`)
	blankRe   = regexp.MustCompile(`\n{3,}`)
)

// plainMarkdown turns an issue body into readable plain text.
func plainMarkdown(md string) string {
	s := strings.ReplaceAll(md, "\r", "")
	s = strings.ReplaceAll(s, "\t", "  ")
	s = mdImageRe.ReplaceAllString(s, "[image]")
	s = mdLinkRe.ReplaceAllString(s, "$1 ($2)")
	s = mdHeadRe.ReplaceAllString(s, "")
	s = mdTagRe.ReplaceAllString(s, "")
	s = strings.NewReplacer("**", "", "__", "", "`", "").Replace(s)
	return strings.TrimSpace(blankRe.ReplaceAllString(s, "\n\n"))
}

// wrapLines word-wraps text to width and cuts any word still too long, so
// every line fits on one screen row.
func wrapLines(text string, width int) []string {
	var out []string
	for _, l := range strings.Split(wordwrap.String(text, width-1), "\n") {
		for runewidth.StringWidth(l) > width {
			head := runewidth.Truncate(l, width, "")
			out = append(out, head)
			l = l[len(head):]
		}
		out = append(out, l)
	}
	return out
}

func ageString(d time.Duration) string {
	switch {
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// limitScopeLabel is the g key's hint: what pressing it switches to.
func limitScopeLabel(editAll bool) string {
	if editAll {
		return "this project"
	}
	return "all projects"
}

// limitLine says which number applies here and which one +/- changes: that
// one comes first, and the other is in brackets.
func (p *IssuePicker) limitLine() string {
	var line string
	switch {
	case p.EditAll && p.LimitOwn:
		line = fmt.Sprintf("%d all projects  (this project: %d)", p.LimitAll, p.Limit)
	case p.EditAll:
		line = fmt.Sprintf("%d all projects", p.LimitAll)
	case p.LimitOwn:
		line = fmt.Sprintf("%d this project  (all projects: %d)", p.Limit, p.LimitAll)
	default:
		line = fmt.Sprintf("%d this project, same as all projects", p.Limit)
	}
	return ipTitleStyle.Render("Limit: ") + line
}

// autoLabel is the a key's hint: what pressing it does.
func autoLabel(on bool) string {
	if on {
		return "auto off"
	}
	return "auto on"
}

// Lists reports whether issue n is in the picker's list.
func (p *IssuePicker) Lists(n int) bool {
	for _, is := range p.all {
		if is.Number == n {
			return true
		}
	}
	return false
}
