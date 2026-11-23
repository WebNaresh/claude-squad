package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func TestSessionLimitOrder(t *testing.T) {
	l := sessionLimits{}
	if l.of("/a") != defaultMaxSessions {
		t.Fatalf("no file: got %d", l.of("/a"))
	}
	l = sessionLimits{All: 8}
	if l.of("/a") != 8 {
		t.Fatalf("all projects: got %d", l.of("/a"))
	}
	l.Projects = map[string]int{"/a": 4}
	if l.of("/a") != 4 || l.of("/b") != 8 {
		t.Fatalf("own number: got %d and %d", l.of("/a"), l.of("/b"))
	}
	// A hand-edited file can't ask for more tiles than cs allows.
	l = sessionLimits{All: 99, Projects: map[string]int{"/a": -3}}
	if l.of("/b") != maxSessionLimit || l.of("/a") != maxSessionLimit {
		t.Fatalf("out of range: got %d and %d", l.of("/b"), l.of("/a"))
	}
}

func TestSessionLimitStep(t *testing.T) {
	l := sessionLimits{}.stepped("/a", false, 1)
	if l.of("/a") != 7 || l.of("/b") != 6 {
		t.Fatalf("project step: %+v", l)
	}
	// Back to the all-projects number: it follows that number again.
	l = l.stepped("/a", false, -1)
	if _, own := l.Projects["/a"]; own {
		t.Fatalf("still has its own number: %+v", l)
	}
	l = l.stepped("/a", true, 2)
	if l.of("/a") != 8 || l.of("/b") != 8 {
		t.Fatalf("all step: %+v", l)
	}
	for i := 0; i < 20; i++ {
		l = l.stepped("/a", true, -1)
	}
	if l.all() != minSessionLimit {
		t.Fatalf("floor: %d", l.all())
	}
	for i := 0; i < 20; i++ {
		l = l.stepped("/a", true, 1)
	}
	if l.all() != maxSessionLimit {
		t.Fatalf("ceiling: %d", l.all())
	}
	// The original isn't touched.
	o := sessionLimits{Projects: map[string]int{"/a": 3}}
	o.stepped("/a", false, 1)
	if o.Projects["/a"] != 3 {
		t.Fatalf("changed the original: %+v", o)
	}
}

func TestSessionLimitsFile(t *testing.T) {
	t.Setenv("CLAUDE_SQUAD_HOME", t.TempDir())
	if err := (sessionLimits{All: 8, Projects: map[string]int{"/a": 4}}).write(); err != nil {
		t.Fatal(err)
	}
	if sessionLimit("/a") != 4 || sessionLimit("/b") != 8 {
		t.Fatalf("read back %d and %d", sessionLimit("/a"), sessionLimit("/b"))
	}
	if err := os.WriteFile(sessionLimitsFile(), []byte("{broken"), 0o644); err != nil {
		t.Fatal(err)
	}
	if sessionLimit("/a") != defaultMaxSessions {
		t.Fatalf("broken file: %d", sessionLimit("/a"))
	}
}

// The tab's limit decides whether the auto loop offers issues: full at the
// default, a free slot once this project's number goes up, and full again for
// a number that only another project raised.
func TestAutoLoopUsesTheLimit(t *testing.T) {
	m, project := autoHome(t, defaultMaxSessions, 0, 5)
	m.autoCheck()
	if m.issuePicker != nil {
		t.Fatal("offered issues at the limit")
	}
	if err := (sessionLimits{Projects: map[string]int{project + "-other": 8}}).write(); err != nil {
		t.Fatal(err)
	}
	m.autoCheck()
	if m.issuePicker != nil {
		t.Fatal("another project's limit opened this one")
	}
	if err := (sessionLimits{Projects: map[string]int{project: 8}}).write(); err != nil {
		t.Fatal(err)
	}
	m.autoCheck()
	if m.issuePicker == nil {
		t.Fatal("no offer after the limit went up")
	}
	if m.issuePicker.TickLimit != 2 {
		t.Fatalf("ticked %d, want the 2 free slots", m.issuePicker.TickLimit)
	}
}

func TestPickerLimitKeys(t *testing.T) {
	m, project := autoHome(t, 0, 0, 5)
	m.newIssuePicker(project)
	p := m.issuePicker
	if p.Limit != defaultMaxSessions || p.LimitOwn {
		t.Fatalf("start: %+v", p.Limit)
	}
	key := func(k string) {
		t.Helper()
		m.handleIssuePickerKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
	}
	key("+")
	key("=")
	if p.Limit != 8 || !p.LimitOwn || p.LimitAll != defaultMaxSessions {
		t.Fatalf("project +: %d own=%v all=%d", p.Limit, p.LimitOwn, p.LimitAll)
	}
	key("g")
	key("-")
	if p.Limit != 8 || p.LimitAll != defaultMaxSessions-1 {
		t.Fatalf("all -: %d all=%d", p.Limit, p.LimitAll)
	}
	if m.issuePicker == nil {
		t.Fatal("limit keys closed the picker")
	}
	// Saved, so the background runner sees it too.
	if sessionLimit(project) != 8 || sessionLimit("/elsewhere") != defaultMaxSessions-1 {
		t.Fatalf("saved: %d and %d", sessionLimit(project), sessionLimit("/elsewhere"))
	}
	if _, err := os.Stat(filepath.Dir(sessionLimitsFile())); err != nil {
		t.Fatal(err)
	}
}

// A free slot the runner can't fill (the PR is full) says why on the strip,
// and only then.
func TestStripSaysWhyASlotIsEmpty(t *testing.T) {
	const why = "PR full: merge it to start more"
	strip := func(running, inPR int) string {
		m, project := autoHome(t, running, inPR, 5)
		if err := (sessionLimits{Projects: map[string]int{project: 8}}).write(); err != nil {
			t.Fatal(err)
		}
		m.progress = loadProgress()
		return ansi.Strip(m.renderProgress(240))
	}
	got := strip(7, autoMaxPerPR)
	t.Log(got)
	if !strings.Contains(got, "7/8 running") || !strings.Contains(got, why) {
		t.Errorf("free slot, full PR: %q", got)
	}
	if got := strip(8, autoMaxPerPR); strings.Contains(got, why) {
		t.Errorf("no free slot: %q", got)
	}
	if got := strip(7, 3); strings.Contains(got, why) {
		t.Errorf("PR has room: %q", got)
	}
}
