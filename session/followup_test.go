package session

import (
	"slices"
	"testing"
	"time"
)

// A "[Follow-up] … after #N deploys" issue names what it waits for; other
// titles wait for nothing (glitchgrab #627/#628 ran in #625's batch).
func TestFollowUpOf(t *testing.T) {
	for title, want := range map[string][]int{
		"[Follow-up] Update MCP + demo booking guides after #620 deploys":             {620},
		"[Follow-up] Confirm the bot knocks on /meet/<code> calls after #625 deploys": {625},
		"[followup] Re-check after #12 and #13 deploy":                                {12, 13},
		"Fix #620 crash after login":                                                  nil,
		"[Follow-up] Write the guide":                                                 nil,
	} {
		if got := followUpOf(title); !slices.Equal(got, want) {
			t.Errorf("followUpOf(%q) = %v, want %v", title, got, want)
		}
	}
}

// A dated follow-up waits for its "Not before:" line, or the date in its
// title (practice-stack #2664 "[Follow-up] 2026-11-04: …").
func TestDueDate(t *testing.T) {
	ist, _ := time.LoadLocation("Asia/Kolkata")
	for _, c := range []struct {
		title, body string
		want        time.Time
	}{
		{"[Follow-up] 2026-11-04: check Bing Images", "", time.Date(2026, 11, 4, 0, 0, 0, 0, time.Local)},
		{"[Follow-up] Send the last 6 Google requests", "Why\n\n**Not before:** 2026-10-23 13:47 IST\n", time.Date(2026, 10, 23, 13, 47, 0, 0, ist)},
		{"[Follow-up] x", "Not before: 2026-10-23T08:17:00Z", time.Date(2026, 10, 23, 8, 17, 0, 0, time.UTC)},
		{"Fix crash on 2026-11-04 invoices", "", time.Time{}},
		{"[Follow-up] Update guides after #620 deploys", "", time.Time{}},
	} {
		if got := dueDate(c.title, c.body); !got.Equal(c.want) {
			t.Errorf("dueDate(%q) = %v, want %v", c.title, got, c.want)
		}
	}
	if !(Issue{NotBefore: time.Now().Add(time.Hour)}).Waits() || (Issue{NotBefore: time.Now().Add(-time.Hour)}).Waits() {
		t.Error("Waits() wrong around NotBefore")
	}
}
