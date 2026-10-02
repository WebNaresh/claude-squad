package app

import (
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
