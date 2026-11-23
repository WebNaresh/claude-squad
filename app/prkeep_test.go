package app

import (
	"claude-squad/session/git"
	"testing"
)

// GitHub not answering keeps the branch's PR in the bottom bar; "no PR" or
// a new branch still clears it (practice-stack PR #3337 vanished, 2026-11-18).
func TestPRKeptWhenGitHubFails(t *testing.T) {
	m := &home{}
	m.bar.prs = map[string]prEntry{}
	pr := git.PullRequest{Number: 3337, State: "OPEN"}
	m.applyPR(prMsg{root: "/p", branch: "dev", pr: pr, ok: true})
	m.applyPR(prMsg{root: "/p", branch: "dev", failed: true})
	if e := m.bar.prs["/p"]; !e.ok || e.pr.Number != 3337 || e.loading {
		t.Fatalf("PR dropped on a failed lookup: %+v", e)
	}
	m.applyPR(prMsg{root: "/p", branch: "other", failed: true})
	if e := m.bar.prs["/p"]; e.ok {
		t.Fatalf("other branch shows dev's PR: %+v", e)
	}
	m.applyPR(prMsg{root: "/p", branch: "dev", pr: pr, ok: true})
	m.applyPR(prMsg{root: "/p", branch: "dev"})
	if e := m.bar.prs["/p"]; e.ok {
		t.Fatalf("PR kept after GitHub said there is none: %+v", e)
	}
}
