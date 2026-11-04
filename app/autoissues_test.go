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
	"github.com/charmbracelet/x/ansi"
)

// autoHome is a home with one auto project running `running` Claude
// sessions, its open PR closing inPR issues, and `open` issues not in a PR.
func autoHome(t *testing.T, running, inPR, open int) (*home, string) {
	t.Helper()
	daemonRunning = func() bool { return false } // not the user's real background runner
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
		issues = append(issues, session.Issue{Number: 100 + i, PR: 951, GatherPR: 951})
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

// The room is the chosen PR's: another PR's issues, however many, neither
// win nor hold the batch back (glitchgrab #766 vs #779, gai-tools #79).
func TestPRRoomCountsOnlyTheChosenPR(t *testing.T) {
	issues := []session.Issue{{Number: 1, PR: 766, GatherPR: 779}, {Number: 2, PR: 766, GatherPR: 779},
		{Number: 3, PR: 779, GatherPR: 779}, {Number: 4, GatherPR: 779}}
	room, pr := prRoom(issues)
	if pr != 779 || room != autoMaxPerPR-1 {
		t.Errorf("prRoom = room %d, PR #%d; want PR #779 with room %d", room, pr, autoMaxPerPR-1)
	}
}

// Nothing chosen: no target, so gai's menu gets no keys.
func TestPRRoomNoChoiceNoTarget(t *testing.T) {
	issues := []session.Issue{{Number: 1, PR: 766, PRUnclear: true}, {Number: 2, PRUnclear: true}}
	if room, pr := prRoom(issues); pr != 0 || room != autoMaxPerPR {
		t.Errorf("prRoom = room %d, PR #%d; want no PR", room, pr)
	}
	if !prUnclear(issues) {
		t.Error("prUnclear = false")
	}
	if keys, ok := prKeys("Multiple open PRs — select one:\n  ▶ #766: @x · feat · 2026-11-01 · 1 issue · t\n    #779: @me · dev · 2026-11-02 · no issues · t", 0); ok {
		t.Errorf("prKeys sent %v with no target", keys)
	}
}

// gai's question for a teammate's lone PR counts as asking, so it gets no keys
// and reaches the user like the several-PRs one.
func TestGaiAsksPR(t *testing.T) {
	for screen, want := range map[string]bool{
		"Multiple open PRs — select one:":                  true,
		"The only open PR is someone else's — confirm it:": true,
		"→ Attaching #1 to PR #779":                        false,
	} {
		if got := gaiAsksPR(screen); got != want {
			t.Errorf("gaiAsksPR(%q) = %v", screen, got)
		}
	}
}

// While only the user can say which PR, one tile asks: more would each sit on
// the same question. The strip says so instead of "no open PR yet".
func TestUnclearPROffersOneAndSaysSo(t *testing.T) {
	m, project := autoHome(t, 0, 0, 3)
	for i := range m.issueCache[project] {
		m.issueCache[project][i].PRUnclear = true
	}
	m.progress = loadProgress()
	m.stepAuto()
	if m.issuePicker == nil || m.issuePicker.TickLimit != 1 {
		t.Fatalf("want an offer of one issue, got picker=%v", m.issuePicker != nil)
	}
	if plain := ansi.Strip(m.renderProgress(160)); !strings.Contains(plain, "which PR?") {
		t.Errorf("strip %q doesn't say gai is asking", plain)
	}
}

// Enter on gai's [y/N] for someone else's PR is the safe no; left unanswered
// it stalled a removal until the tile was killed.
func TestGaiPromptIncludesNoDefault(t *testing.T) {
	if !isGaiPrompt("PR #766 is @x's. Rewrite its title and body too? [y/N] ") {
		t.Error("[y/N] prompt not answered")
	}
}

// gai's new rows, with the author and branch before the title, still parse.
func TestPRKeysReadsRowsWithAuthorAndBranch(t *testing.T) {
	screen := "Multiple open PRs — select one:\n  ▶ #779: @me · dev · 2026-11-02 · no issues · batch\n    #766: @x · feat/763 · 2026-11-01 · 1 issue · booking"
	keys, ok := prKeys(screen, 766)
	if !ok || len(keys) != 2 || keys[0] != "Down" || keys[1] != "Enter" {
		t.Errorf("prKeys = %v, %v", keys, ok)
	}
}

// An issue a merged PR lists is taken (never started again), and a merged
// PR never counts as the project's PR with room.
func TestMergedPRIssuesAreTaken(t *testing.T) {
	m, project := autoHome(t, 0, 0, 0)
	m.issueCache[project] = []session.Issue{
		{Number: 944, PR: 951, PRMerged: true},
		{Number: 960},
	}
	if room, pr := prRoom(m.issueCache[project]); pr != 0 || room != autoMaxPerPR {
		t.Errorf("merged PR counted: room %d PR #%d", room, pr)
	}
	m.stepAuto()
	if m.issuePicker == nil || m.issuePicker.TickLimit != 1 {
		t.Fatalf("want an offer of just #960, got picker=%v", m.issuePicker != nil)
	}
}

// An issue whose session started but isn't on the PR yet takes a place: with
// 14 of 15 on the PR and one starting, nothing more starts.
func TestJoiningTakesPRRoom(t *testing.T) {
	var issues []session.Issue
	for n := 1; n <= 14; n++ {
		issues = append(issues, session.Issue{Number: n, PR: 2435, GatherPR: 2435})
	}
	issues = append(issues, session.Issue{Number: 2414, GatherPR: 2435}, session.Issue{Number: 2415, GatherPR: 2435})
	room, pr := prRoom(issues)
	if pr != 2435 || room != 1 {
		t.Fatalf("prRoom = %d, #%d; want 1, #2435", room, pr)
	}
	if room -= joining(issues, map[int]bool{2414: true}); room != 0 {
		t.Fatalf("room with #2414 starting = %d, want 0", room)
	}
}

// A project at its session limit still loads its list: after a cs restart the
// strip above the tiles has nothing else to show the PR and open counts from.
func TestAutoFullProjectStillLoadsItsList(t *testing.T) {
	m, project := autoHome(t, autoMaxSessions, 8, 3)
	// As after a restart: no list in memory, none fetched yet.
	delete(m.issueCache, project)
	delete(m.autoFetched, project)
	m.autoCheck()
	if m.autoFetched[project].IsZero() {
		t.Error("project at its limit never loads its issues, so the strip stays without PR and open counts")
	}
}

// gai's last row, "Create a new PR", is not a "#N:" row: prKeys neither counts
// it nor ever moves the cursor onto it, so no batch answers it for the user.
func TestPRKeysNeverPicksCreateNewPR(t *testing.T) {
	screen := "Multiple open PRs — select one:\n  ▶ #766: @x · feat · 2026-11-01 · 1 issue · t\n    #779: @me · dev · 2026-11-02 · no issues · t\n    Create a new PR"
	if prRowRe.MatchString("    Create a new PR") {
		t.Fatal("prRowRe matches the Create a new PR row")
	}
	keys, ok := prKeys(screen, 779)
	if !ok || len(keys) != 2 || keys[0] != "Down" || keys[1] != "Enter" {
		t.Errorf("prKeys(779) = %v, %v; want [Down Enter]", keys, ok)
	}
	// Lone teammate PR + the new row: no target (0) means no keys at all.
	lone := "The only open PR is someone else's — confirm it:\n  ▶ #766: @x · feat · 2026-11-01 · 1 issue · t\n    Create a new PR"
	if keys, ok := prKeys(lone, 0); ok {
		t.Errorf("prKeys sent %v with no target", keys)
	}
	if !gaiAsksPR(lone) {
		t.Error("lone teammate PR menu not seen as a question")
	}
}
