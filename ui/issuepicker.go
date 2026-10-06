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
	inPR    int          // open issues hidden because an open PR already closes them
	stale   bool         // showing the last list while a fresh one loads
	busy    map[int]bool // issues that already have a session
	skipped map[int]bool // issues the user unticked: not ticked by default again
	// NewestFirst lists (and ticks) the newest issues first instead of the
	// oldest; s switches it, saved per project by the caller.
	NewestFirst bool
	// TickLimit is how many issues are ticked when the list arrives (0:
	// IssuePickDefault). The auto loop sets it to the free session slots.
	TickLimit int
	// Auto says the auto loop opened the picker and why ("1 free slot …");
	// AutoOn is whether the loop runs for this project (a switches it).
	Auto     string
	AutoOn   bool
	selected map[int]bool
	cursor   int
	scroll   int // preview scroll offset
	width    int
	height   int
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
	for _, is := range p.issues {
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
	refresh := p.issues != nil
	var cur int
	if refresh && p.cursor < len(p.issues) {
		cur = p.issues[p.cursor].Number
	}
	var shown []session.Issue
	p.inPR = 0
	for _, is := range issues { // issues come oldest first
		if is.PR > 0 {
			p.inPR++
			continue
		}
		shown = append(shown, is)
	}
	if p.NewestFirst {
		slices.Reverse(shown)
	}
	if err != nil && refresh {
		// Keep showing the last list; just say the refresh failed.
		p.Err, p.Loading, p.stale = nil, false, false
		return
	}
	p.issues, p.busy, p.Err, p.Loading, p.stale = shown, busy, err, false, false
	if refresh {
		keep := map[int]bool{}
		p.cursor = 0
		for i, is := range shown {
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
	for _, is := range p.issues {
		if len(p.selected) == limit {
			break
		}
		if !p.busy[is.Number] && !p.skipped[is.Number] {
			p.selected[is.Number] = true
		}
	}
}

// ToggleOrder switches between oldest and newest first, moving to the top
// and ticking the first issues of the new order.
func (p *IssuePicker) ToggleOrder() {
	p.NewestFirst = !p.NewestFirst
	slices.Reverse(p.issues)
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
	switch key {
	case "up", "k":
		if p.cursor > 0 {
			p.cursor--
			p.scroll = 0
		}
	case "down", "j":
		if p.cursor < len(p.issues)-1 {
			p.cursor++
			p.scroll = 0
		}
	case "pgdown", "shift+down", "J":
		p.scroll += 5
	case "pgup", "shift+up", "K":
		p.scroll = max(0, p.scroll-5)
	case " ", "x":
		if p.cursor < len(p.issues) {
			n := p.issues[p.cursor].Number
			if !p.busy[n] {
				// Unticking skips the issue: it isn't ticked by default next
				// time. Ticking it again takes it back.
				p.selected[n] = !p.selected[n]
				p.skipped[n] = !p.selected[n]
			}
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
	b.WriteString(ipTitleStyle.Render("Start Claude on issues") + "   project: " + ipTitleStyle.Render("‹ "+p.Name+" ›") + "   " + ipDimStyle.Render(order) + "\n")
	b.WriteString(KeyRow("", []Key{{"enter", "start"}, {"esc", "cancel"}, {"space", "tick / skip"}, {"↑↓", "move"},
		{"s", other}, {"←→", "project"}, {"o", "open"}, {"a", autoLabel(p.AutoOn)}, {"J/K", "scroll"}}, w-4) + "\n")
	if p.Auto != "" {
		b.WriteString(ipTitleStyle.Render("⟳ Auto: ") + p.Auto + "\n")
	}
	b.WriteString("\n")
	// Loading, error and empty states take the full list's height too, so
	// the picker doesn't jump when the list arrives.
	avail := max(12, p.height-9)
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
	if len(p.issues) == 0 {
		if p.inPR > 0 {
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
	start := 0
	if p.cursor >= room {
		start = p.cursor - room + 1
	}
	for i := start; i < len(p.issues) && i < start+room; i++ {
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
		case is.QAFailed:
			title = "(failed testing, again) " + title
		}
		line := fmt.Sprintf("%s #%-5d %s", box, is.Number, title)
		line = runewidth.Truncate(line, w-8, "…")
		line += strings.Repeat(" ", max(1, w-4-runewidth.StringWidth(line)-len(age))) + age
		if p.busy[is.Number] || p.skipped[is.Number] {
			line = ipDimStyle.Render(line)
		} else {
			line = strings.TrimSuffix(line, age) + ipDimStyle.Render(age)
		}
		if i == p.cursor {
			line = ipCursorStyle.Render(line)
		}
		b.WriteString(line + "\n")
	}
	for i := len(p.issues) - start; i < room; i++ {
		b.WriteString("\n") // fewer issues than rows: keep the list's height
	}
	b.WriteString(p.renderPreview(w-4, avail-room-1))
	foot := fmt.Sprintf("%d ticked · each gets its own session running gai issue, one after another", len(p.Selected()))
	if p.inPR > 0 {
		foot += fmt.Sprintf(" · %d hidden: already in a PR (open or merged)", p.inPR)
	}
	if p.stale {
		foot += " · refreshing…"
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
	head = append(head, ipDimStyle.Render(runewidth.Truncate(is.URL, width, "…")))
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

// autoLabel is the a key's hint: what pressing it does.
func autoLabel(on bool) string {
	if on {
		return "auto off"
	}
	return "auto on"
}

// Lists reports whether issue n is in the picker's list.
func (p *IssuePicker) Lists(n int) bool {
	for _, is := range p.issues {
		if is.Number == n {
			return true
		}
	}
	return false
}
