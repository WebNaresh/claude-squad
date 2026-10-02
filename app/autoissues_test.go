package app

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"claude-squad/config"
	"claude-squad/session"
	"claude-squad/ui"

	"github.com/charmbracelet/bubbles/spinner"
)

// autoHome is a home with one auto project running `running` Claude
// sessions, its open PR closing inPR issues, and `open` issues not in a PR.
func autoHome(t *testing.T, running, inPR, open int) (*home, string) {
	t.Helper()
	project := t.TempDir()
	sp := spinner.New()
	m := &home{ctx: context.Background(), state: stateDefault, appConfig: config.DefaultConfig(),
		list: ui.NewList(&sp, false), projectTabs: ui.NewProjectTabs(), sessionsLoaded: true, errBox: ui.NewErrBox(),
		dockNames: map[string]string{}, issueCache: map[string][]session.Issue{}}
	m.projectTabs.SetProjects([]string{project}, project)
	var ext []*session.ExternalSession
	for i := 0; i < running; i++ {
		ext = append(ext, &session.ExternalSession{Kind: session.KindTmux, Name: fmt.Sprintf("%sp_i%d", session.ExternalPrefix, 900+i), Path: project, Status: "busy"})
	}
	m.list.SetExternal(ext)
	m.list.SetProject(project)
	var issues []session.Issue
	for i := 0; i < inPR; i++ {
		issues = append(issues, session.Issue{Number: 100 + i, PR: 951})
	}
	for i := 0; i < open; i++ {
		issues = append(issues, session.Issue{Number: 200 + i})
	}
	m.issueCache[project] = issues
	m.autoDeclined, m.autoFetched, m.autoSaid = map[string]int{}, map[string]time.Time{project: time.Now()}, map[string]string{}
	return m, project
}

func TestAutoOffersFreeSlotsCappedByPR(t *testing.T) {
	m, _ := autoHome(t, 4, 14, 5) // 2 free slots, but the PR takes only 1 more
	m.stepAuto()
	if m.issuePicker == nil || m.state != stateIssuePicker {
		t.Fatal("no offer with 2 free slots and room in the PR")
	}
	if m.issuePicker.TickLimit != 1 {
		t.Errorf("ticks %d issues, want 1 (PR has 14 of %d)", m.issuePicker.TickLimit, autoMaxPerPR)
	}
}

func TestAutoQuietWhenFullOrPRFull(t *testing.T) {
	if m, _ := autoHome(t, autoMaxSessions, 0, 5); m.stepAuto() != nil && m.issuePicker != nil {
		t.Error("offered with all session slots taken")
	}
	if m, _ := autoHome(t, 2, autoMaxPerPR, 5); m.stepAuto() != nil && m.issuePicker != nil {
		t.Error("offered with the PR full")
	}
}

func TestAutoEscWaitsForASessionToClose(t *testing.T) {
	m, project := autoHome(t, 5, 0, 5)
	m.stepAuto()
	if m.issuePicker == nil {
		t.Fatal("no first offer")
	}
	m.issuePicker, m.state = nil, stateDefault
	m.autoPickerClosed(project, false) // Esc
	m.stepAuto()
	if m.issuePicker != nil {
		t.Fatal("asked again right after Esc with nothing closed")
	}
	// One session closes: 4 running.
	m.list.SetExternal(m.list.ExternalSessions()[:4])
	m.stepAuto()
	if m.issuePicker == nil || m.issuePicker.TickLimit != 2 {
		t.Fatalf("after a session closed: picker=%v, want an offer of 2", m.issuePicker != nil)
	}
}

func TestAutoOtherTabGetsBadgeNotPicker(t *testing.T) {
	m, project := autoHome(t, 4, 0, 5)
	other := t.TempDir()
	m.projectTabs.SetProjects([]string{other, project}, other) // looking at another tab
	m.stepAuto()
	if m.issuePicker != nil {
		t.Fatal("picker opened for a tab you're not on")
	}
	if !strings.Contains(m.projectTabs.String(), "·2 free") {
		t.Errorf("tab row %q has no ·2 free badge", m.projectTabs.String())
	}
}
