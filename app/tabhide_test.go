package app

import (
	"claude-squad/session"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/lipgloss"
)

func TestReadQuietTabs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CLAUDE_SQUAD_HOME", filepath.Join(home, "cs"))
	now := time.Now()
	used := func(project string, at time.Time) {
		dir := filepath.Join(home, ".claude", "projects", regexp.MustCompile(`[^a-zA-Z0-9]`).ReplaceAllString(project, "-"))
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		f := filepath.Join(dir, "s.jsonl")
		if err := os.WriteFile(f, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(f, at, at); err != nil {
			t.Fatal(err)
		}
	}
	used("/c/recent_one", now.Add(-6*24*time.Hour))
	used("/c/old", now.Add(-8*24*time.Hour))

	got := readQuietTabs([]string{"/c/issue", "/c/recent_one", "/c/old", "/c/never", "/c/just-added"},
		map[string][]session.Issue{"/c/issue": {{Number: 1}}},
		map[string]time.Time{"/c/just-added": now.Add(-time.Hour)}, now).quiet
	want := map[string]bool{"/c/issue": false, "/c/recent_one": false, "/c/old": true, "/c/never": true, "/c/just-added": false}
	for p, w := range want {
		if got[p] != w {
			t.Errorf("%s quiet = %v, want %v", p, got[p], w)
		}
	}
}

func TestHasOpenIssueIgnoresSkipped(t *testing.T) {
	issues := []session.Issue{{Number: 7}}
	if hasOpenIssue(issues, map[int]bool{7: true}) {
		t.Fatal("skipped issue kept the tab")
	}
	if !hasOpenIssue(issues, nil) {
		t.Fatal("open issue didn't keep the tab")
	}
}

func TestTabPickerFirstRowMatchesRender(t *testing.T) {
	p := &tabPicker{projects: []string{"/a", "/b"}, names: []string{"a-proj", "b-proj"}, used: []string{"3d ago", "never used"}}
	lines := strings.Split(p.render(), "\n")
	if lipgloss.Height(p.render()) < tabPickerFirstRow+2 || !strings.Contains(lines[tabPickerFirstRow], "a-proj") ||
		!strings.Contains(lines[tabPickerFirstRow+1], "b-proj") {
		t.Fatalf("rows not at tabPickerFirstRow:\n%s", p.render())
	}
}
