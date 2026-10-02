package ui

import (
	"strings"
	"testing"
	"time"

	"claude-squad/session"

	"github.com/charmbracelet/lipgloss"
)

// The picker must keep one height while moving between issues (short and
// long texts) and while the list loads, or it jumps on every ↑↓.
func TestIssuePickerHeightIsFixed(t *testing.T) {
	p := NewIssuePicker("/p", "p")
	p.SetSize(160, 50)
	loading := lipgloss.Height(p.Render())
	issues := []session.Issue{
		{Number: 1, Title: "short", Body: "one line", CreatedAt: time.Now()},
		{Number: 2, Title: "long", Body: strings.Repeat("a long line of text\n", 80), CreatedAt: time.Now()},
		{Number: 3, Title: "empty", CreatedAt: time.Now()},
	}
	p.SetIssues(issues, nil, nil)
	var heights []int
	for i := 0; i < len(issues); i++ {
		heights = append(heights, lipgloss.Height(p.Render()))
		p.HandleKey("down")
	}
	for i, h := range heights {
		if h != heights[0] {
			t.Errorf("issue %d: height %d, issue 1: %d", i+1, h, heights[0])
		}
	}
	if loading != heights[0] {
		t.Errorf("loading height %d, loaded %d", loading, heights[0])
	}
}
