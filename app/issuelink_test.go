package app

import "testing"

func TestIssueAt(t *testing.T) {
	line := "  🟢 [CAVEMAN] #2210   PR #2309 · ← for agents"
	// Screen columns: "  🟢 [CAVEMAN] " is 15 wide (🟢 takes 2), so
	// "#2210" covers columns 15–19 and "#2309" 26–30.
	for col, want := range map[int]int{14: 0, 15: 2210, 19: 2210, 20: 0, 26: 2309, 30: 2309, 31: 0} {
		if got := issueAt(line, col); got != want {
			t.Errorf("column %d: got %d, want %d", col, got, want)
		}
	}
	if issueAt(line, 3) != 0 {
		t.Error("plain text counted as an issue")
	}
}
