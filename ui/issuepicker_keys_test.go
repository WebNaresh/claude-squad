package ui

import (
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
