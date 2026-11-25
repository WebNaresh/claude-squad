package ui

import (
	"slices"
	"strings"
	"testing"
	"time"

	"claude-squad/session"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

func peoplePicker() *IssuePicker {
	p := NewIssuePicker("/p", "p")
	p.Me = "Me"
	p.SetSize(140, 50)
	p.SetSkipped(map[int]bool{6: true})
	issues := []session.Issue{
		{Number: 1, Title: "scan"},
		{Number: 2, Title: "own", People: []string{"Me"}},
		{Number: 3, Title: "x", People: []string{"Asha Rao"}},
		{Number: 4, Title: "y", People: []string{"Bina", "Asha Rao"}},
		{Number: 5, Title: "z", People: []string{"Bina"}},
		{Number: 6, Title: "skipped", People: []string{"Asha Rao"}},
		{Number: 7, Title: "in pr", People: []string{"Chet"}, PR: 9},
	}
	now := time.Now()
	for i := range issues {
		issues[i].CreatedAt = now
	}
	p.SetIssues(issues, map[int]bool{}, nil)
	return p
}

func press(p *IssuePicker, ks ...string) {
	for _, k := range ks {
		p.HandleKey(k)
	}
}

// Other people's issues come before your own and the nameless ones.
func TestPickerPeopleFirst(t *testing.T) {
	p := peoplePicker()
	if got := listed(p); !slices.Equal(got, []int{3, 4, 5, 1, 2, 6}) {
		t.Fatalf("order %v", got)
	}
	p.HandleKey("3")
	if got := ticked(p); !slices.Equal(got, []int{3, 4, 5}) {
		t.Fatalf("3 ticked %v", got)
	}
	out := ansi.Strip(p.Render())
	for _, want := range []string{"Asha Rao", "Bina +1", "Your own, or no name: 2", "from: everyone"} {
		if !strings.Contains(out, want) {
			t.Fatalf("no %q:\n%s", want, out)
		}
	}
}

// f, a person, enter: only theirs, ticks included; Everyone brings all back.
func TestPickerOnlyOnePerson(t *testing.T) {
	p := peoplePicker()
	press(p, "f", "down", "enter") // Asha and Bina tie on 2 ready: Asha first
	if got := listed(p); !slices.Equal(got, []int{3, 4, 6}) {
		t.Fatalf("Asha's: %v", got)
	}
	if got := ticked(p); !slices.Equal(got, []int{3, 4}) {
		t.Fatalf("ticked %v: a hidden issue must not be", got)
	}
	if out := ansi.Strip(p.Render()); !strings.Contains(out, "from: Asha Rao") || !strings.Contains(out, "Asha Rao +1") {
		t.Fatalf("filter not shown:\n%s", out)
	}
	if got := p.Skipped(); !slices.Equal(got, []int{6}) {
		t.Fatalf("skips %v", got)
	}
	// A refresh keeps the filter; skips of hidden issues stay too.
	p.SetIssues([]session.Issue{{Number: 3, People: []string{"Asha Rao"}}, {Number: 8, People: []string{"Bina"}}}, map[int]bool{}, nil)
	if got := listed(p); !slices.Equal(got, []int{3}) {
		t.Fatalf("refresh dropped the filter: %v", got)
	}
	press(p, "f", "enter")
	if got := listed(p); !slices.Equal(got, []int{3, 8}) {
		t.Fatalf("everyone: %v", got)
	}
}

func TestPickerSeveralPeopleAndNone(t *testing.T) {
	p := peoplePicker()
	press(p, "f", "down", " ", "down", " ", "enter")
	if got := listed(p); !slices.Equal(got, []int{3, 4, 5, 6}) {
		t.Fatalf("Asha and Bina: %v", got)
	}
	// Chet's only issue is in a PR: say so, and don't crash on an empty list.
	press(p, "f", "down", " ", "down", " ", "down", "enter")
	out := ansi.Strip(p.Render())
	if !strings.Contains(out, "No open issues left from Chet: 1 already in a PR") {
		t.Fatalf("no reason for the empty list:\n%s", out)
	}
	press(p, "x", " ", "tab", "down", "up")
	p.SetIssues(nil, nil, nil)
	p.Render()
}

// p pins: first in the list, and the cursor stays on the person.
func TestPickerPin(t *testing.T) {
	p := peoplePicker()
	press(p, "f", "down", "down", "p") // Bina
	if !slices.Equal(p.Pinned, []string{"Bina"}) || p.people.rows[p.people.cursor].name != "Bina" {
		t.Fatalf("pinned %v, cursor on %q", p.Pinned, p.people.rows[p.people.cursor].name)
	}
	press(p, "esc")
	if got := listed(p); !slices.Equal(got, []int{4, 5, 3, 1, 2, 6}) {
		t.Fatalf("order %v", got)
	}
	if !strings.Contains(p.Render(), "★Bina") {
		t.Fatal("no star")
	}
	press(p, "f", "down", "p", "esc")
	if len(p.Pinned) != 0 || listed(p)[0] != 3 {
		t.Fatalf("unpin: %v %v", p.Pinned, listed(p))
	}
}

// The people list is as tall and wide as the issue list.
func TestPickerPeopleKeepsSize(t *testing.T) {
	for _, w := range []int{44, 80, 140} {
		p := peoplePicker()
		p.SetSize(w, 40)
		a := p.Render()
		p.HandleKey("f")
		b := p.Render()
		if lipgloss.Height(a) != lipgloss.Height(b) || lipgloss.Width(a) != lipgloss.Width(b) {
			t.Errorf("width %d: %dx%d, people %dx%d", w, lipgloss.Width(a), lipgloss.Height(a), lipgloss.Width(b), lipgloss.Height(b))
		}
	}
}

// With Asha's filter on, f and enter on Bina shows Bina's, not Asha's again.
func TestPickerSwitchPerson(t *testing.T) {
	p := peoplePicker()
	press(p, "f", "down", "enter")
	press(p, "f", "down", "down", "enter")
	if got := listed(p); !slices.Equal(got, []int{4, 5}) {
		t.Fatalf("Bina's: %v", got)
	}
}

// A load that ends while the people list is open updates it, cursor kept.
func TestPickerPeopleReload(t *testing.T) {
	p := peoplePicker()
	press(p, "f", "down", "down") // Bina
	p.SetIssues([]session.Issue{{Number: 5, People: []string{"Bina"}}, {Number: 9, People: []string{"Zed"}}}, map[int]bool{}, nil)
	if r := p.people.rows[p.people.cursor]; r.name != "Bina" || r.ready != 1 || len(p.people.rows) != 3 {
		t.Fatalf("rows %+v, cursor on %+v", p.people.rows, r)
	}
}
