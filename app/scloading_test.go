package app

import (
	"os"
	"testing"
	"time"

	"claude-squad/ui"
)

// Closing the last session of a temporary tab (a CI runner's checkout) moved
// to the next tab with its git read dropped; that tab stayed "loading" for
// good and its ↑161 never changed, through three pushes.
func TestTemporaryTabEndDoesNotFreezeGitStatus(t *testing.T) {
	t.Setenv("CLAUDE_SQUAD_HOME", t.TempDir())
	wd, _ := os.Getwd()
	t.Cleanup(func() { _ = os.Chdir(wd) }) // switching tabs chdirs
	m, project := autoHome(t, 0, 0, 0)
	m.sourceControl, m.scLoading, m.scCache = ui.NewSourceControl(), map[string]time.Time{}, map[string]scStatusMsg{}
	m.fitted, m.menu, m.tabbedWindow = map[string]string{}, ui.NewMenu(), ui.NewTabbedWindow(ui.NewPreviewPane(), ui.NewDiffPane(), ui.NewTerminalPane())
	m.appConfig.OpenProjects = []string{project}
	runner := t.TempDir()
	m.sessionRoots = []string{runner}
	m.projectTabs.SetProjects([]string{project, runner}, runner)

	m.sessionRoots = nil // its last session ended
	m.refreshTabs(runner)

	if m.projectTabs.Active() != project {
		t.Fatalf("active tab %q, want %q", m.projectTabs.Active(), project)
	}
	if m.refreshSourceControl() == nil {
		t.Error("the tab's git status is stuck loading: the 2s refresh never runs again")
	}
}

// A git read that never answers must not block the tab's refresh forever.
func TestLostGitReadIsRetried(t *testing.T) {
	m, project := autoHome(t, 0, 0, 0)
	m.scLoading = map[string]time.Time{project: time.Now()}
	if m.refreshSourceControl() != nil {
		t.Error("started a second read while one is running")
	}
	m.scLoading[project] = time.Now().Add(-scLoadingStale)
	if m.refreshSourceControl() == nil {
		t.Error("a read lost 30s ago still blocks the refresh")
	}
}
