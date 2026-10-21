package ui

import (
	"strings"
	"testing"

	"claude-squad/session"
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
