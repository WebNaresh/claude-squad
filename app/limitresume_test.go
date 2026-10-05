package app

import (
	"testing"
	"time"
)

func TestLimitReset(t *testing.T) {
	loc := time.FixedZone("IST", 5*3600+1800)
	now := time.Date(2026, 10, 3, 11, 31, 0, 0, loc)
	screen := "● Bash(...)\n  ⎿  You've hit your session limit · resets 12:20pm (Asia/Calcutta)\n     /upgrade to increase your usage limit.\n\n❯ \n"
	at, _, ok := limitReset(screen, now)
	if !ok || at.Hour() != 12 || at.Minute() != 20 || at.Day() != 3 {
		t.Fatalf("reset = %v ok=%v, want today 12:20", at, ok)
	}
	// "resets 1am" seen at 11pm is tomorrow... and at 2am it's today, passed.
	late := time.Date(2026, 10, 3, 23, 0, 0, 0, loc)
	if at, _, _ := limitReset("  ⎿  You've hit your session limit · resets 1am\n     /upgrade to increase your usage limit.\n", late); at.Day() != 4 {
		t.Errorf("1am seen at 11pm = %v, want tomorrow", at)
	}
	if !limitIsLast(screen) {
		t.Error("stopped on the limit, but not seen as last")
	}
	if limitIsLast(screen + "● Bash(go test)\n❯ \n") {
		t.Error("resumed already (a new step), but limit seen as last")
	}
	// The same words quoted in a reply or a test are not a stop.
	quoted := "● Done. It reads \"You've hit your session limit · resets 1am\" and resumes.\n\n❯ \n"
	if _, _, ok := limitReset(quoted, now); ok || limitIsLast(quoted) {
		t.Error("quoted limit text taken for a real stop")
	}
	if _, _, ok := limitReset("all good\n❯ \n", now); ok {
		t.Error("no limit line, but a reset found")
	}
}

func TestAccountSwitch(t *testing.T) {
	limit := "● Bash(...)\n  ⎿  You've hit your session limit · resets 6:10pm (Asia/Calcutta)\n     /upgrade to increase your usage limit.\n\n✻ Worked for 1s · done 3:48 PM\n"
	notice := "⏺ Remote Control disconnected — signed-in claude.ai account or organization changed on this machine — run\n  /remote-control to start a session for the current account, or /login to switch back, then\n  /remote-control\n\n❯ \n"
	screen := limit + notice
	if !switchedAccount(screen) {
		t.Error("notice is the last step, but no switch seen")
	}
	if !limitIsLast(screen) {
		t.Error("the switch notice counted as a new step after the limit")
	}
	// After /remote-control and continue: Claude works again, no switch.
	if switchedAccount(screen + "⏺ Bash(go test)\n❯ \n") {
		t.Error("a later step, but still seen as just switched")
	}
	if switchedAccount("⏺ Done.\n❯ \n") {
		t.Error("no notice, but a switch seen")
	}
	// ⏺ is a step too: a resumed session isn't stopped on its limit.
	if limitIsLast(limit + "⏺ Bash(go test)\n❯ \n") {
		t.Error("⏺ step after the limit not seen")
	}
}
