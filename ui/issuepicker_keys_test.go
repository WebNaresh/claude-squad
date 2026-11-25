package ui

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"claude-squad/session"

	"github.com/charmbracelet/x/ansi"
)

func pickerWith(n int) *IssuePicker {
	p := NewIssuePicker("/p", "p")
	var issues []session.Issue
	for i := 1; i <= n; i++ {
		issues = append(issues, session.Issue{Number: i})
	}
	p.SetIssues(issues, map[int]bool{}, nil)
	return p
}

func ticked(p *IssuePicker) []int {
	var out []int
	for _, is := range p.Selected() {
		out = append(out, is.Number)
	}
	return out
}

// Space only ticks; x skips (kept in the list); a digit ticks the first N.
func TestPickerTickSkipAndCount(t *testing.T) {
	p := pickerWith(8)

	p.HandleKey(" ") // untick #1
	if p.skipped[1] {
		t.Fatal("unticking with space skipped the issue")
	}

	p.HandleKey("down")
	p.HandleKey("x") // skip #2
	if !p.skipped[2] || p.selected[2] {
		t.Fatalf("x: skipped=%v selected=%v, want skipped, not ticked", p.skipped[2], p.selected[2])
	}
	if len(p.issues) != 8 {
		t.Fatal("skipped issue left the list")
	}

	p.HandleKey("3") // first 3, skipping #2
	if got := ticked(p); len(got) != 3 || got[0] != 1 || got[1] != 3 || got[2] != 4 {
		t.Fatalf("3 ticked %v, want [1 3 4]", got)
	}
	p.HandleKey("1")
	if got := ticked(p); len(got) != 1 || got[0] != 1 {
		t.Fatalf("1 ticked %v, want [1]", got)
	}

	p.HandleKey(" ") // cursor on #2 (skipped): ticking takes the skip back
	if !p.selected[2] || p.skipped[2] {
		t.Fatal("ticking a skipped issue didn't take the skip back")
	}
	p.HandleKey("x")
	p.HandleKey("x") // x twice: back to normal
	if p.skipped[2] {
		t.Fatal("x again didn't un-skip")
	}
}

func TestPickerRefreshKeepsListAndCursor(t *testing.T) {
	p := NewIssuePicker("/p", "p")
	p.SetSize(100, 40)
	issues := []session.Issue{{Number: 1, Title: "a"}, {Number: 2, Title: "b"}}
	p.SetIssues(issues, map[int]bool{}, nil)
	p.HandleKey("down")
	p.StartRefresh()
	if !strings.Contains(p.Render(), "refreshing from GitHub…") {
		t.Fatal("no refreshing note while the reload is in flight")
	}
	p.SetIssues(append(issues, session.Issue{Number: 3, Title: "c"}), map[int]bool{}, nil)
	if strings.Contains(p.Render(), "refreshing…") {
		t.Fatal("refreshing note stays after the list arrived")
	}
	if cur, _ := p.Current(); cur.Number != 2 {
		t.Fatalf("cursor moved to #%d", cur.Number)
	}
	if !strings.Contains(p.Render(), "refresh") {
		t.Fatal("key row has no refresh")
	}
}

// r on a list that came back the same must still show it reloaded: the
// footer says "refreshed" with the count, and how many issues are new.
func TestPickerRefreshShowsResult(t *testing.T) {
	p := pickerWith(3)
	p.SetSize(120, 30)
	p.StartRefresh()
	if !strings.Contains(p.Render(), "refreshing from GitHub") {
		t.Fatal("no refreshing… while the reload runs")
	}
	p.SetIssues([]session.Issue{{Number: 1}, {Number: 2}, {Number: 3}}, map[int]bool{}, nil)
	if v := p.Render(); !strings.Contains(v, "refreshed") || !strings.Contains(v, "3 listed") || strings.Contains(v, "new") {
		t.Fatalf("same list: want \"refreshed … 3 listed\" and no \"new\", got footer of:\n%s", v)
	}
	p.StartRefresh()
	p.SetIssues([]session.Issue{{Number: 1}, {Number: 2}, {Number: 3}, {Number: 4}}, map[int]bool{}, nil)
	if v := p.Render(); !strings.Contains(v, "4 listed, 1 new") {
		t.Fatalf("one added: want \"4 open, 1 new\", got:\n%s", v)
	}
}

// A follow-up waiting for another issue to merge is listed, dimmed and
// labelled, but never ticked by default (glitchgrab #627/#628).
func TestPickerFollowUpNotTicked(t *testing.T) {
	p := NewIssuePicker("/p", "p")
	p.SetIssues([]session.Issue{
		{Number: 1},
		{Number: 2, Title: "[Follow-up] Update guides after #1 deploys", WaitsFor: 1},
		{Number: 3},
	}, map[int]bool{}, nil)
	if p.selected[2] || !p.selected[1] || !p.selected[3] {
		t.Fatalf("ticked %v, want 1 and 3 (not the waiting follow-up)", ticked(p))
	}
	p.SetSize(120, 30)
	if out := p.Render(); !strings.Contains(out, "(waits for #1 to merge)") {
		t.Fatalf("picker doesn't say the follow-up waits:\n%s", out)
	}
}

// A follow-up due on a later date isn't ticked and says when it's due.
func TestPickerDatedFollowUpNotTicked(t *testing.T) {
	due := time.Date(2099, 11, 4, 0, 0, 0, 0, time.Local)
	p := NewIssuePicker("/p", "p")
	p.SetIssues([]session.Issue{{Number: 1}, {Number: 2, Title: "[Follow-up] 2099-11-04: check", NotBefore: due}}, map[int]bool{}, nil)
	if p.selected[2] || !p.selected[1] {
		t.Fatalf("ticked %v, want only 1", ticked(p))
	}
	p.SetSize(120, 30)
	if out := p.Render(); !strings.Contains(out, "(waits until 4 Nov 00:00)") {
		t.Fatalf("picker doesn't say when it's due:\n%s", out)
	}
}

// The limit line says which number applies and which one +/- changes.
func TestPickerLimitLine(t *testing.T) {
	p := pickerWith(1)
	p.Limit, p.LimitAll = 6, 6
	for _, c := range []struct {
		own, all bool
		limit    int
		want     string
	}{
		{false, false, 6, "6 this project, same as all projects"},
		{true, false, 8, "8 this project  (all projects: 6)"},
		{false, true, 6, "6 all projects"},
		{true, true, 8, "6 all projects  (this project: 8)"},
	} {
		p.LimitOwn, p.EditAll, p.Limit = c.own, c.all, c.limit
		if got := ansi.Strip(p.limitLine()); got != "Limit: "+c.want {
			t.Errorf("own=%v all=%v: %q", c.own, c.all, got)
		}
	}
}

func listed(p *IssuePicker) []int {
	var out []int
	for _, is := range p.issues {
		out = append(out, is.Number)
	}
	return out
}

// Skipped, waiting and running issues go below the ones that can be started,
// in both orders: practice-stack's 18 old skipped ones filled the top.
func TestPickerOnHoldBelow(t *testing.T) {
	issues := []session.Issue{{Number: 1}, {Number: 2, Video: true}, {Number: 3}, {Number: 4}, {Number: 5}, {Number: 6}}
	p := NewIssuePicker("/p", "p")
	p.SetSize(120, 50)
	p.SetSkipped(map[int]bool{1: true, 4: true})
	p.ShowCached(issues, map[int]bool{3: true})
	check := func(what string, want ...int) {
		t.Helper()
		if got := listed(p); !slices.Equal(got, want) {
			t.Fatalf("%s: %v, want %v", what, got, want)
		}
	}
	check("oldest first", 5, 6, 3, 2, 1, 4)
	p.SetIssues(issues, map[int]bool{3: true}, nil)
	check("refreshed", 5, 6, 3, 2, 1, 4)
	out := ansi.Strip(p.Render())
	if !strings.Contains(out, "On hold: 1 with a session · 1 waiting · 2 skipped") {
		t.Fatalf("no on-hold line:\n%s", out)
	}
	p.HandleKey("tab")
	if is, _ := p.Current(); is.Number != 3 {
		t.Fatalf("tab went to #%d, want #3", is.Number)
	}
	p.HandleKey("tab")
	if is, _ := p.Current(); is.Number != 5 {
		t.Fatalf("tab back went to #%d, want #5", is.Number)
	}
	p.ToggleOrder()
	check("newest first", 6, 5, 3, 2, 4, 1)
}

// x keeps the row under the cursor (x again takes it back); it moves below
// once the cursor leaves, and the cursor stays among the issues it was in.
func TestPickerSkipMovesBelowOnLeave(t *testing.T) {
	p := pickerWith(4)
	p.HandleKey("x")
	if is, _ := p.Current(); is.Number != 1 || listed(p)[0] != 1 {
		t.Fatal("row moved while the cursor was on it")
	}
	p.HandleKey("down")
	if is, _ := p.Current(); is.Number != 2 || !slices.Equal(listed(p), []int{2, 3, 4, 1}) {
		t.Fatalf("after down: cursor #%d, list %v", is.Number, listed(p))
	}
	p.HandleKey("x") // a refresh moves it too, without taking the cursor along
	p.SetIssues([]session.Issue{{Number: 1}, {Number: 2}, {Number: 3}, {Number: 4}}, map[int]bool{}, nil)
	if is, _ := p.Current(); is.Number != 3 || !slices.Equal(listed(p), []int{3, 4, 1, 2}) {
		t.Fatalf("after refresh: cursor #%d, list %v", is.Number, listed(p))
	}
	p.HandleKey("tab")
	p.HandleKey("x") // un-skip #1
	p.HandleKey("down")
	if is, _ := p.Current(); is.Number != 2 || !slices.Equal(listed(p), []int{1, 3, 4, 2}) {
		t.Fatalf("after un-skip: cursor #%d, list %v", is.Number, listed(p))
	}
}

// Scrolled to the last row, the on-hold line must not push the cursor's row
// off the list or change the picker's height.
func TestPickerOnHoldLastRowVisible(t *testing.T) {
	p := NewIssuePicker("/p", "p")
	p.SetSize(120, 50)
	var issues []session.Issue
	for i := 1; i <= 40; i++ {
		issues = append(issues, session.Issue{Number: i, Title: "t"})
	}
	p.SetSkipped(map[int]bool{7: true, 8: true})
	p.SetIssues(issues, map[int]bool{}, nil)
	h := strings.Count(p.Render(), "\n")
	for i := 0; i < 40; i++ {
		p.HandleKey("down")
		v := ansi.Strip(p.Render())
		is, _ := p.Current()
		if got := strings.Count(v, "\n"); got != h {
			t.Fatalf("row %d: height %d, want %d", i, got, h)
		}
		if !strings.Contains(v, fmt.Sprintf("] #%-5d", is.Number)) {
			t.Fatalf("#%d is under the cursor but not on screen", is.Number)
		}
	}
}

// tab must show the on-hold issues, not only the first of them.
func TestPickerTabShowsOnHoldRows(t *testing.T) {
	p := NewIssuePicker("/p", "p")
	p.SetSize(120, 50)
	var issues []session.Issue
	for i := 1; i <= 40; i++ {
		issues = append(issues, session.Issue{Number: i, Title: "t"})
	}
	p.SetSkipped(map[int]bool{1: true, 2: true, 3: true, 4: true})
	p.SetIssues(issues, map[int]bool{}, nil)
	p.HandleKey("tab")
	v := ansi.Strip(p.Render())
	for _, want := range []string{"On hold: 4 skipped", "] #1 ", "] #4 "} {
		if !strings.Contains(v, want) {
			t.Fatalf("after tab, %q is not on screen:\n%s", want, v)
		}
	}
}
