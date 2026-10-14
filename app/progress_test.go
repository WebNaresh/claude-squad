package app

import (
	"claude-squad/session/git"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"
)

func TestProgressStrip(t *testing.T) {
	if os.Getenv("STRIP_OUT") != "" {
		lipgloss.SetColorProfile(termenv.TrueColor) // colours for the picture
	}
	m, project := autoHome(t, 5, 9, 23) // PR with 9 issues, 23 more open, 5 sessions
	m.progress = loadProgress()
	m.progress.Done, m.progress.OpenAtStart = map[string]map[string]int{}, map[string]map[string]int{}
	day := func(back int) string { return time.Now().AddDate(0, 0, -back).Format("2006-01-02") }
	m.progress.Done[day(0)] = map[string]int{project: 7}
	m.progress.Done[day(1)] = map[string]int{project: 3}
	m.progress.Done[day(2)] = map[string]int{project: 1}
	m.progress.OpenAtStart[day(0)] = map[string]int{project: 36} // 4 fewer now (9+23=32)
	strip := m.renderProgress(160)
	plain := ansi.Strip(strip)
	for _, want := range []string{"PR #951", "9/15 issues", "23 open", "(−4 today)", "5/6 running", "✓ 7 done today", "🔥 3-day streak"} {
		if !strings.Contains(plain, want) {
			t.Errorf("strip %q lacks %q", plain, want)
		}
	}
	if w := ansi.StringWidth(strip); w > 160 {
		t.Errorf("strip is %d wide, over 160", w)
	}
	if f := os.Getenv("STRIP_OUT"); f != "" {
		_ = os.WriteFile(f, []byte(strip+"\n"), 0o644)
	}
}

// The PR's checks sit beside its issues on the strip, so a running or failed
// check shows at the top, not only in the bottom bar.
func TestProgressStripShowsChecks(t *testing.T) {
	m, project := autoHome(t, 1, 9, 3) // PR #951
	m.progress = loadProgress()
	run := git.CheckItem{Status: "IN_PROGRESS"}
	ok := git.CheckItem{Status: "COMPLETED", Conclusion: "SUCCESS"}
	bad := git.CheckItem{Status: "COMPLETED", Conclusion: "FAILURE"}
	cases := []struct {
		name   string
		pr     git.PullRequest
		want   string
		absent bool
	}{
		{"running", git.PullRequest{Number: 951, State: "OPEN", Checks: []git.CheckItem{run, run}}, "● 2 of 2 running", false},
		{"failed", git.PullRequest{Number: 951, State: "OPEN", Checks: []git.CheckItem{ok, bad}}, "✗ 1 failed", false},
		{"passed", git.PullRequest{Number: 951, State: "OPEN", Checks: []git.CheckItem{ok, ok, ok}}, "✓ 3/3 checks", false},
		{"no checks", git.PullRequest{Number: 951, State: "OPEN"}, "checks", true},
		{"other PR", git.PullRequest{Number: 7, State: "OPEN", Checks: []git.CheckItem{run}}, "1 of 1 running", true},
		{"merged", git.PullRequest{Number: 951, State: "MERGED", Checks: []git.CheckItem{run}}, "1 of 1 running", true},
	}
	for _, c := range cases {
		m.bar.stripPRs = map[string]prEntry{project: {pr: c.pr, ok: true}}
		plain := ansi.Strip(m.renderProgress(160))
		if has := strings.Contains(plain, c.want); has == c.absent {
			t.Errorf("%s: strip %q, want %q present=%v", c.name, plain, c.want, !c.absent)
		}
	}
}

// Every project tab carries its PR's checks: only the check status, short.
func TestTabChecksChip(t *testing.T) {
	m, project := autoHome(t, 0, 0, 0)
	ok := git.CheckItem{Status: "COMPLETED", Conclusion: "SUCCESS"}
	run := git.CheckItem{Status: "IN_PROGRESS"}
	bad := git.CheckItem{Status: "COMPLETED", Conclusion: "FAILURE"}
	cases := []struct {
		name string
		pr   git.PullRequest
		want string // "" = no chip
	}{
		{"passed", git.PullRequest{Number: 65, State: "OPEN", Checks: []git.CheckItem{ok, ok}}, "✓ 2/2"},
		{"running", git.PullRequest{Number: 65, State: "OPEN", Checks: []git.CheckItem{run, run, ok}}, "● 2 running"},
		{"failed", git.PullRequest{Number: 65, State: "OPEN", Checks: []git.CheckItem{ok, bad}}, "✗ 1 failed"},
		{"no checks", git.PullRequest{Number: 65, State: "OPEN"}, ""},
		{"merged", git.PullRequest{Number: 65, State: "MERGED", Checks: []git.CheckItem{ok}}, ""},
	}
	for _, c := range cases {
		m.applyTabChecks(tabChecksMsg{root: project, pr: c.pr, ok: true})
		row := ansi.Strip(m.projectTabs.String())
		if has := c.want != "" && strings.Contains(row, c.want); has != (c.want != "") {
			t.Errorf("%s: tab row %q, want chip %q", c.name, row, c.want)
		}
		if c.want == "" && (strings.Contains(row, "✓ 2/2") || strings.Contains(row, "running") || strings.Contains(row, "failed")) {
			t.Errorf("%s: tab row %q shows a chip", c.name, row)
		}
	}
	m.applyTabChecks(tabChecksMsg{root: project, ok: false}) // gh failed: chip gone
	if row := ansi.Strip(m.projectTabs.String()); strings.Contains(row, "✓") {
		t.Errorf("chip kept after a failed read: %q", row)
	}
}
