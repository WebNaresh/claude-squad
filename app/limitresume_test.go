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
	// continue sent and Claude still thinking (no step yet): not stopped.
	limitLines := "● Bash(...)\n  ⎿  You've hit your session limit · resets 12:20pm (Asia/Calcutta)\n     /upgrade to increase your usage limit.\n\n"
	thinking := limitLines + "❯ continue\n\n✶ Razzmatazzing… (95s)\n─────\n❯ \n─────\n"
	if limitIsLast(thinking) {
		t.Error("continue taken and Claude thinking, but limit seen as last")
	}
	// continue refused: a new limit line below the sent message.
	refused := limitLines + "❯ continue\n  ⎿  You've hit your session limit · resets 12:20pm (Asia/Calcutta)\n     /upgrade to increase your usage limit.\n\n─────\n❯ \n─────\n"
	if !limitIsLast(refused) {
		t.Error("continue refused, but not seen as stopped")
	}
	// A greyed suggestion in the input box isn't a sent message.
	if !limitIsLast(limitLines + "─────\n❯ commit\n─────\n") {
		t.Error("suggestion in the input box taken for a sent message")
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
	if !remoteControlOff(screen) {
		t.Error("notice is the last step, but no switch seen")
	}
	if !limitIsLast(screen) {
		t.Error("the switch notice counted as a new step after the limit")
	}
	// A 47-column tile: Claude wraps the notice, "signed-in" on the next line.
	wrapped := limit + "⏺ Remote Control disconnected —                \n  signed-in claude.ai account or \n  organization changed on this machine\n  /remote-control\n\n❯ /login\n  ⎿  Login successful\n\n❯ \n"
	if !remoteControlOff(wrapped) {
		t.Error("notice wrapped in a narrow tile, but no switch seen")
	}
	if !limitIsLast(wrapped) {
		t.Error("the wrapped switch notice counted as a new step after the limit")
	}
	// Narrower still: "disconnected —" alone on its own line.
	if !remoteControlOff("⏺ Remote Control\n  disconnected —\n  signed-in claude.ai\n\n❯ \n") {
		t.Error("notice wrapped twice, but no switch seen")
	}
	// The tile /login ran in, notice pushed off its short screen.
	if !remoteControlOff("❯ /login\n  ⎿  Login successful\n\n❯ \n") {
		t.Error("/login finished here, but no switch seen")
	}
	// Quoted in tool output, indented: not this tile's notice.
	if remoteControlOff("⏺ Bash(tmux capture-pane)\n  ⎿  ok\n     ⏺ Remote Control disconnected — signed-in claude.ai\n\n❯ \n") {
		t.Error("a quoted notice seen as this tile's switch")
	}
	// Already handled: /remote-control prints no step, and a cs restart
	// forgets what it typed.
	if remoteControlOff(notice + "❯ /remote-control\n\n  /remote-control is active\n\n❯ /reload-plugins\n  ⎿  Reloaded\n\n❯ \n") {
		t.Error("/remote-control already typed, but still seen as just switched")
	}
	// Work after the notice (the tile was on a question, or mid-turn) leaves
	// Remote Control off all the same: #3043 was never turned back on.
	if !remoteControlOff(screen + "⏺ Bash(go test)\n❯ \n") {
		t.Error("work after the notice, but Remote Control seen as on")
	}
	// A second notice after one that was handled: off again.
	again := notice + "❯ /remote-control\n  /remote-control is active\n⏺ Done.\n" + notice
	if !remoteControlOff(again) {
		t.Error("second notice after a handled one, but Remote Control seen as on")
	}
	if remoteControlOff("⏺ Done.\n❯ \n") {
		t.Error("no notice, but a switch seen")
	}
	// ⏺ is a step too: a resumed session isn't stopped on its limit.
	if limitIsLast(limit + "⏺ Bash(go test)\n❯ \n") {
		t.Error("⏺ step after the limit not seen")
	}
}

func TestContinueRest(t *testing.T) {
	for typed, want := range map[string]string{"": "continue", "co": "ntinue", "Cont": "inue", "continue": ""} {
		if got, ok := continueRest(typed); !ok || got != want {
			t.Errorf("continueRest(%q) = %q, %v; want %q", typed, got, ok, want)
		}
	}
	for _, typed := range []string{"fix the bug", "cx", "continue please"} {
		if _, ok := continueRest(typed); ok {
			t.Errorf("continueRest(%q) took a draft for continue", typed)
		}
	}
}

// "5-hour limit", wrapped by a 65-column tile (2026-10-28: no tile continued).
func TestLimitResetWrapped(t *testing.T) {
	loc := time.FixedZone("IST", 5*3600+1800)
	now := time.Date(2026, 10, 28, 17, 33, 0, 0, loc)
	screen := "  ⎿  Allowed by auto mode classifier\n  ⎿  You've hit your 5-hour limit · resets \n     5:30pm (Asia/Calcutta)  \n     /upgrade or /usage-credits to finish what \n     you're working on.\n\n✻ Cogitated for 4m 52s\n─────\n❯ \n─────\n"
	at, _, ok := limitReset(screen, now)
	if !ok || at.Hour() != 17 || at.Minute() != 30 {
		t.Fatalf("reset = %v ok=%v, want 17:30", at, ok)
	}
	if !limitIsLast(screen) {
		t.Error("stopped on a wrapped limit, but not seen as last")
	}
}

func TestWrapUpLimit(t *testing.T) {
	now := time.Date(2026, 11, 4, 14, 0, 0, 0, time.Local)
	want := time.Date(2026, 11, 4, 15, 20, 0, 0, time.Local)
	for _, screen := range []string{
		"❯ \n───\n  ⚠ Usage limit reached · limit resets 3:20pm\n  · clau.de/wrap-up · /upgrade to keep using\n  Claude Code\n  🟡 [CAVEMAN] #2983\n",
		"❯ \n───\n  ⚠ Usage limit reached · wrapping up · limit\n  resets 3:20pm · clau.de/wrap-up · /upgrade\n",
	} {
		if at, finished, ok := wrapUpLimit(screen, now); !ok || finished || !at.Equal(want) {
			t.Errorf("got %v %v %v for %q", at, finished, ok, screen)
		}
	}
	if _, finished, ok := wrapUpLimit("❯ \n  ⚠ Usage limit reached · limit resets 3:20pm\n  🟡 #2980 ✓ done · close session\n", now); !ok || !finished {
		t.Error("finished tile not seen")
	}
	// The same words in the conversation, above the input box, aren't the footer.
	if _, _, ok := wrapUpLimit("⏺ Usage limit reached · limit resets 3:20pm\n❯ \n  auto mode on\n", now); ok {
		t.Error("quoted footer seen as the limit")
	}
}

func TestContinuedAfterSwitch(t *testing.T) {
	notice := "⏺ Remote Control disconnected —\n  signed-in claude.ai account or\n\n❯ /remote-control\n"
	if s, c := continuedAfterSwitch("⏺ Done.\n❯ \n"); s || c {
		t.Error("no notice, but a switch seen")
	}
	if s, c := continuedAfterSwitch("❯ continue\n" + notice + "❯ \n"); !s || c {
		t.Error("a continue from before the switch counted")
	}
	if s, c := continuedAfterSwitch(notice + "❯ continue\n⏺ Working\n❯ \n"); !s || !c {
		t.Error("continue after the switch not seen")
	}
}

func TestWrapUpDue(t *testing.T) {
	at := time.Date(2026, 11, 4, 15, 20, 0, 0, time.Local)
	before, after := at.Add(-time.Hour), at.Add(time.Minute)
	for _, c := range []struct {
		switched, continued bool
		now                 time.Time
		want                bool
	}{
		{false, false, before, false}, // same account: wait for the reset
		{false, false, after, true},
		{true, false, before, true}, // new account: carry on now
		{true, true, before, false}, // already continued: not twice
		{true, true, after, true},   // a later limit on the new account
	} {
		if got := wrapUpDue(c.switched, c.continued, at, c.now); got != c.want {
			t.Errorf("switched=%v continued=%v now=%v: got %v", c.switched, c.continued, c.now, got)
		}
	}
}

// /remote-control that never lands is typed again later, a few times only.
func TestRCRetry(t *testing.T) {
	rcTries = map[string]rcTry{}
	now := time.Now()
	if !rcDue("t", now) {
		t.Fatal("first try not due")
	}
	rcTries["t"] = rcTry{at: now, n: 1}
	if rcDue("t", now.Add(30*time.Second)) {
		t.Error("retried too soon")
	}
	if !rcDue("t", now.Add(rcRetryAfter)) {
		t.Error("no retry after the wait")
	}
	rcTries["t"] = rcTry{at: now, n: rcMaxTries}
	if rcDue("t", now.Add(time.Hour)) {
		t.Error("kept retrying past the limit")
	}
}

// /remote-control alone isn't enough: until /reload-plugins is in, the tile
// still counts as switched, so no continue goes in before the reload.
func TestReloadBeforeContinue(t *testing.T) {
	notice := "⏺ Remote Control disconnected — signed-in claude.ai account or organization changed\n\n"
	if !remoteControlOff(notice + "❯ /remote-control\n  /remote-control is active\n\n❯ \n") {
		t.Error("no /reload-plugins yet, but seen as handled")
	}
	if remoteControlOff(notice + "❯ /remote-control\n❯ /reload-plugins\n  ⎿  Reloaded\n❯ continue\n⏺ Done.\n❯ \n") {
		t.Error("reloaded, but still seen as switched")
	}
}
