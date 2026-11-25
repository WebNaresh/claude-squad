package app

import (
	"testing"

	"claude-squad/session"
)

// The saved list must keep what puts an issue on hold: without the video and
// research marks the picker ticked those issues until the fresh list came.
func TestIssueCacheKeepsOnHold(t *testing.T) {
	t.Setenv("CLAUDE_SQUAD_HOME", t.TempDir())
	writeIssueCache("/p", []session.Issue{{Number: 1, Video: true}, {Number: 2, Research: true}, {Number: 3}})
	got, ok := readIssueCache("/p")
	if !ok || len(got) != 3 || !got[0].Waits() || !got[1].Waits() || got[2].Waits() {
		t.Fatalf("cache lost what puts an issue on hold: %+v", got)
	}
}
