package ui

import (
	"fmt"
	"slices"
	"strings"

	"claude-squad/session"

	"github.com/mattn/go-runewidth"
)

// The picker's people list (f): whose issues to show, and whose come first.

// peopleList is the open list: row 0 is everyone, then names, then nobody.
type peopleList struct {
	open   bool
	rows   []personRow
	cursor int
	ticked map[string]bool // lower-case names, as edited in the open list
	edited bool            // space was pressed since it opened
	only   map[string]bool // the ones in force (empty: everyone)
}

type personRow struct {
	name               string // "": issues with no name
	everyone           bool
	ready, hold, taken int
}

func personKey(name string) string { return strings.ToLower(name) }

func (p *IssuePicker) filtered() bool { return len(p.people.only) > 0 }

// only returns the issues of the people picked.
func (p *IssuePicker) only(issues []session.Issue) []session.Issue {
	if !p.filtered() {
		return slices.Clone(issues)
	}
	var out []session.Issue
	for _, is := range issues {
		if len(is.People) == 0 && p.people.only[""] {
			out = append(out, is)
		}
		if slices.ContainsFunc(is.People, func(n string) bool { return p.people.only[personKey(n)] }) {
			out = append(out, is)
		}
	}
	return out
}

// ChoosingPeople reports the people list is open: it takes every key.
func (p *IssuePicker) ChoosingPeople() bool { return p.people.open }

func (p *IssuePicker) isPinned(name string) bool {
	return name != "" && slices.ContainsFunc(p.Pinned, func(q string) bool { return session.SamePerson(q, name) })
}

// personRows lists everyone with an open issue, plus pinned people with none.
func (p *IssuePicker) personRows() []personRow {
	byKey := map[string]*personRow{}
	row := func(name string) *personRow {
		k := personKey(name)
		if byKey[k] == nil {
			byKey[k] = &personRow{name: name}
		}
		return byKey[k]
	}
	all := personRow{everyone: true}
	count := func(issues []session.Issue, add func(*personRow, session.Issue)) {
		for _, is := range issues {
			add(&all, is)
			if len(is.People) == 0 {
				add(row(""), is)
			}
			for _, n := range is.People {
				add(row(n), is)
			}
		}
	}
	count(p.all, func(r *personRow, is session.Issue) {
		if p.group(is) <= groupReady {
			r.ready++
		} else {
			r.hold++
		}
	})
	count(p.taken, func(r *personRow, _ session.Issue) { r.taken++ })
	for _, n := range p.Pinned {
		row(n)
	}
	var out []personRow
	for _, r := range byKey {
		out = append(out, *r)
	}
	// The list's own order: pinned, other people, you, no name; the most
	// ready issues first in each.
	rank := func(r personRow) int {
		switch {
		case r.name == "":
			return 3
		case p.isPinned(r.name):
			return 0
		case session.SamePerson(r.name, p.Me):
			return 2
		}
		return 1
	}
	slices.SortFunc(out, func(a, b personRow) int {
		if c := rank(a) - rank(b); c != 0 {
			return c
		}
		if c := b.ready - a.ready; c != 0 {
			return c
		}
		return strings.Compare(personKey(a.name), personKey(b.name))
	})
	return append([]personRow{all}, out...)
}

func (p *IssuePicker) openPeople() {
	p.people.open, p.people.cursor, p.people.edited = true, 0, false
	p.people.rows = p.personRows()
	p.people.ticked = map[string]bool{}
	for k := range p.people.only {
		p.people.ticked[k] = true
	}
}

// reloadPeople rebuilds the open list, keeping the cursor on its person.
func (p *IssuePicker) reloadPeople() {
	pl := &p.people
	cur := pl.rows[min(pl.cursor, len(pl.rows)-1)]
	pl.rows = p.personRows()
	pl.cursor = max(0, slices.IndexFunc(pl.rows, func(r personRow) bool {
		return r.everyone == cur.everyone && session.SamePerson(r.name, cur.name)
	}))
}

func (p *IssuePicker) peopleKey(key string) {
	pl := &p.people
	pl.cursor = min(pl.cursor, len(pl.rows)-1)
	cur := pl.rows[pl.cursor]
	switch key {
	case "up", "k":
		pl.cursor = max(0, pl.cursor-1)
	case "down", "j":
		pl.cursor = min(len(pl.rows)-1, pl.cursor+1)
	case " ":
		pl.edited = true
		if cur.everyone {
			pl.ticked = map[string]bool{}
		} else if k := personKey(cur.name); pl.ticked[k] {
			delete(pl.ticked, k)
		} else {
			pl.ticked[k] = true
		}
	case "p":
		if cur.everyone || cur.name == "" {
			return
		}
		if p.isPinned(cur.name) {
			p.Pinned = slices.DeleteFunc(slices.Clone(p.Pinned), func(q string) bool { return session.SamePerson(q, cur.name) })
		} else {
			p.Pinned = append(slices.Clone(p.Pinned), cur.name)
		}
		// The rows re-sort; the cursor stays on the person.
		p.reloadPeople()
		p.relist() // whose issues come first changed
	case "enter":
		// The people just ticked; else the one under the cursor (ticks left
		// from the last filter must not win over the row you press enter on).
		only := pl.ticked
		if cur.everyone {
			only = map[string]bool{}
		} else if len(only) == 0 || !pl.edited {
			only = map[string]bool{personKey(cur.name): true}
		}
		pl.only, pl.open = only, false
		p.relist()
	case "esc", "f", "q", "ctrl+c":
		pl.open = false
	}
}

// relist shows the list afresh after the filter or the pins changed: from the
// top, with its first issues ticked (a tick on a hidden issue must not start).
func (p *IssuePicker) relist() {
	p.issues = p.only(p.all)
	p.arrange()
	p.cursor, p.scroll, p.selected = 0, 0, map[int]bool{}
	p.tickDefault()
}

// peopleLabel is the f key's hint: whose issues are listed now.
func (p *IssuePicker) peopleLabel() string {
	if !p.filtered() {
		return "from: everyone"
	}
	return "from: " + p.onlyNames()
}

// onlyNames names the people picked: the first, and how many more.
func (p *IssuePicker) onlyNames() string {
	var names []string
	for _, r := range p.personRows() {
		if !r.everyone && p.people.only[personKey(r.name)] {
			names = append(names, r.label())
		}
	}
	if len(names) == 0 { // their last open issue is gone
		for k := range p.people.only {
			names = append(names, k)
		}
		slices.Sort(names)
	}
	if len(names) > 1 {
		return fmt.Sprintf("%s +%d", names[0], len(names)-1)
	}
	return names[0]
}

func (r personRow) label() string {
	switch {
	case r.everyone:
		return "Everyone"
	case r.name == "":
		return "No name (scans, old reports)"
	}
	return r.name
}

func (p *IssuePicker) noneFrom() string {
	s := "No open issues from " + p.onlyNames() + "."
	if n := len(p.only(p.taken)); n > 0 {
		s = fmt.Sprintf("No open issues left from %s: %d already in a PR (open or merged).", p.onlyNames(), n)
	}
	return s + " f: show someone else."
}

// nameWidth is the name column's width (0: nobody is named).
func (p *IssuePicker) nameWidth(w int) int {
	widest := 0
	for _, is := range p.issues {
		widest = max(widest, runewidth.StringWidth(p.nameText(is, 99)))
	}
	return min(widest, 18, w/6)
}

// nameText is who asked for it, within width; "+2" (more people) is never cut.
func (p *IssuePicker) nameText(is session.Issue, width int) string {
	if len(is.People) == 0 {
		return "–"
	}
	// With a filter on, the name shown is the one it was picked for.
	name := is.People[0]
	for _, n := range is.People {
		if p.people.only[personKey(n)] {
			name = n
			break
		}
	}
	more := ""
	if len(is.People) > 1 {
		more = fmt.Sprintf(" +%d", len(is.People)-1)
	}
	if is.From(p.Pinned) {
		name = "★" + name
	}
	return runewidth.Truncate(name, max(1, width-len(more)), "…") + more
}

func (p *IssuePicker) nameCell(is session.Issue, width int) string {
	if width < 4 {
		return ""
	}
	return runewidth.FillRight(p.nameText(is, width), width) + "  "
}

// renderPeople draws the people list in exactly rows lines.
func (p *IssuePicker) renderPeople(width, rows int) string {
	pl := &p.people
	lines := []string{
		ipDimStyle.Render(runewidth.Truncate("Issues people asked for come first, then your own. Pinned ★ people come before everyone.", width, "…")),
		"",
	}
	room := max(1, rows-len(lines))
	start := max(0, pl.cursor-room+1)
	for i := start; i < len(pl.rows) && i < start+room; i++ {
		r := pl.rows[i]
		box := "[ ]"
		if r.everyone && len(pl.ticked) == 0 || !r.everyone && pl.ticked[personKey(r.name)] {
			box = "[x]"
		}
		star := " "
		if p.isPinned(r.name) {
			star = "★"
		}
		label := r.label()
		if r.name != "" && session.SamePerson(r.name, p.Me) {
			label += " (you)"
		}
		counts := []string{fmt.Sprintf("%d ready", r.ready)}
		if r.hold > 0 {
			counts = append(counts, fmt.Sprintf("%d on hold", r.hold))
		}
		if r.taken > 0 {
			counts = append(counts, fmt.Sprintf("%d in a PR", r.taken))
		}
		nameW := min(40, max(10, width/2))
		line := fmt.Sprintf("%s %s %s  %s", box, star, runewidth.FillRight(runewidth.Truncate(label, nameW, "…"), nameW), strings.Join(counts, " · "))
		line = runewidth.FillRight(runewidth.Truncate(line, width, "…"), width)
		switch {
		case i == pl.cursor:
			line = ipCursorStyle.Render(line)
		case r.ready == 0:
			line = ipDimStyle.Render(line)
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}
