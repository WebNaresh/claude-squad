package session

import (
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// Who asked for an issue. GitHub's author is no help: Glitchgrab files nearly
// every issue as its own app and writes the person into the body.
var (
	// "> **Reported by:** CS Rajan Kumar (mail) 📞 +91… • ID: `…` • **Created:** …"
	reportedByRe = regexp.MustCompile(`(?m)^>?\s*\*\*Reported by:\*\*\s*(.+)$`)
	// Where the name in that line ends.
	afterNameRe = regexp.MustCompile(`\s*(?:\([^()\s]+@[^()\s]+\)|📞|•|\s\x60).*$`)
	// "- [ ] 2. <title> (reported by Shekhar Maske)": an issue that gathers
	// several reports. Done ones ([x]) no longer wait for anyone.
	itemByRe = regexp.MustCompile(`(?m)^- \[ \] \d+\. .*\(reported by ([^()]+)\)\s*$`)
	callByRe = regexp.MustCompile(`(?m)^Raised by \*\*(.+?)\*\* on the call\b`)
	ideaByRe = regexp.MustCompile(`(?m)^Filed from a GlitchRecord idea chat by (.+?), \d{4}-`)
	callNRe  = regexp.MustCompile(`^(.*\S)\s+\(\d+\)$`)
)

// peopleIn returns the people an issue's body names as its reporters, the
// main one first.
func peopleIn(body string) []string {
	body = strings.ReplaceAll(body, "\r", "")
	var out []string
	add := func(name string) {
		name = strings.Join(strings.Fields(strings.Map(func(r rune) rune {
			if unicode.IsControl(r) {
				return ' ' // a name is drawn in the terminal: no escape codes
			}
			return r
		}, name)), " ")
		switch strings.ToLower(name) {
		case "", "unknown", "anonymous", "anonymous tester":
			return
		}
		if !slices.ContainsFunc(out, func(o string) bool { return SamePerson(o, name) }) {
			out = append(out, name)
		}
	}
	// The line is at the end; a report can quote an older one before it.
	if m := reportedByRe.FindAllStringSubmatch(body, -1); len(m) > 0 {
		add(afterNameRe.ReplaceAllString(m[len(m)-1][1], ""))
	}
	for _, m := range itemByRe.FindAllStringSubmatch(body, -1) {
		add(m[1])
	}
	if m := callByRe.FindStringSubmatch(body); m != nil {
		name := m[1]
		if c := callNRe.FindStringSubmatch(name); c != nil {
			name = c[1] + " on a call" // "Client (0)": the recorder's label
		}
		add(name)
	}
	if m := ideaByRe.FindStringSubmatch(body); m != nil {
		add(m[1])
	}
	return out
}

// setPeople fills People: the reporters in the body, else the GitHub user who
// opened the issue (login is yours, shown as me), else nobody (a bot).
func (is *Issue) setPeople(login, me string) {
	is.People = peopleIn(is.Body)
	if len(is.People) > 0 || is.Author.Login == "" || is.Author.IsBot || strings.HasPrefix(is.Author.Login, "app/") {
		return
	}
	name := is.Author.Login
	if login != "" && strings.EqualFold(name, login) && me != "" {
		name = me
	}
	is.People = []string{name}
}

// SamePerson compares names the way people type them: "SAGAR MEHRA" and
// "Sagar  Mehra" are one person, whatever email each report came from.
func SamePerson(a, b string) bool {
	return strings.EqualFold(strings.Join(strings.Fields(a), " "), strings.Join(strings.Fields(b), " "))
}

// Me is your name in a project (git's user.name), to tell your own issues
// from other people's.
func Me(dir string) string {
	out, err := exec.Command("git", "-C", dir, "config", "user.name").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// From reports whether one of the people asked for the issue.
func (is Issue) From(people []string) bool {
	return slices.ContainsFunc(is.People, func(p string) bool {
		return slices.ContainsFunc(people, func(q string) bool { return SamePerson(p, q) })
	})
}

// Priorities, the order issues are listed and started in.
const (
	PriorityPinned = iota // someone you pinned asked for it
	PriorityPeople        // someone other than you did
	PriorityRest          // yours, or nobody's (scans, follow-ups)
)

func (is Issue) Priority(me string, pinned []string) int {
	if is.From(pinned) {
		return PriorityPinned
	}
	for _, p := range is.People {
		if !SamePerson(p, me) {
			return PriorityPeople
		}
	}
	return PriorityRest
}

// ByPriority returns issues in the order to start them: by priority, the
// order they came in (oldest first) within each.
func ByPriority(issues []Issue, me string, pinned []string) []Issue {
	out := slices.Clone(issues)
	slices.SortStableFunc(out, func(a, b Issue) int { return a.Priority(me, pinned) - b.Priority(me, pinned) })
	return out
}
