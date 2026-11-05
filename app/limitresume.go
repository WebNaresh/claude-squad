package app

import (
	"claude-squad/session"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// When the Claude plan's usage limit is hit, every working session stops on
// "You've hit your session limit · resets 12:20pm (Asia/Calcutta)" and waits.
// The background runner checks every tile on each scan; once the reset time
// has passed (plus limitMargin) it types "continue" into each one still
// stopped there with an empty prompt, once per limit message, so the work
// carries on by itself.

// limitMargin covers clock skew past the shown reset minute. It was 30s and,
// with the 15s scan, tiles carried on up to 45s late; the daemon now checks
// at the reset itself, and a continue sent too early is retried
// (limitRetryAfter) instead of being lost.
const limitMargin = 5 * time.Second

// limitRetryAfter: still stopped on the same limit this long after its reset
// (the first continue came before the server's reset), type continue once more.
const limitRetryAfter = 90 * time.Second

// Any kind of limit: "session", "usage", "weekly", "5-hour" (Claude's
// wording since 2026-10; the old list missed it and no tile continued).
var limitRe = regexp.MustCompile(`hit your [a-z0-9-]+ limit[^·]*·\s*resets\s+(\d{1,2})(?::(\d{2}))?\s*([ap]m)`)

// limitReset returns when a pane's last screen says the limit resets, from
// the latest limit line, or ok=false. A time already passed by more than
// 12 hours means tomorrow.
func limitReset(screen string, now time.Time) (time.Time, string, bool) {
	lines := strings.Split(screen, "\n")
	for i := len(lines) - 1; i >= 0 && i >= len(lines)-40; i-- {
		notice, ok := limitNotice(lines, i)
		if !ok {
			continue
		}
		m := limitRe.FindStringSubmatch(strings.ToLower(notice))
		at := resetTime(m[1], m[2], m[3], now)
		return at, notice, true
	}
	return time.Time{}, "", false
}

// resetTime turns the hour, minutes and am/pm of a limit notice into a time.
// A time already passed by more than 12 hours means tomorrow.
func resetTime(hour, minute, ampm string, now time.Time) time.Time {
	h, _ := strconv.Atoi(hour)
	min := 0
	if minute != "" {
		min, _ = strconv.Atoi(minute)
	}
	if ampm == "pm" && h != 12 {
		h += 12
	} else if ampm == "am" && h == 12 {
		h = 0
	}
	at := time.Date(now.Year(), now.Month(), now.Day(), h, min, 0, 0, now.Location())
	if now.Sub(at) > 12*time.Hour {
		at = at.AddDate(0, 0, 1)
	}
	return at
}

// Since 2026-11 Claude can finish its turn at the limit ("wrap-up") instead of
// printing a "hit your limit" line, and tells the user to say continue. The
// only sign is the footer below the input box, which wraps in a narrow tile:
// "⚠ Usage limit reached · wrapping up · limit resets 3:20pm · clau.de/wrap-up".
var wrapUpRe = regexp.MustCompile(`usage limit reached[^⏵]*?limit resets (\d{1,2})(?::(\d{2}))?\s*([ap]m)`)

// footerBelowBox returns the lines below Claude's input box, joined.
func footerBelowBox(screen string) string {
	lines := strings.Split(screen, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "❯") {
			var footer []string
			for _, l := range lines[i+1:] {
				footer = append(footer, strings.TrimSpace(l))
			}
			return strings.ToLower(strings.Join(footer, " "))
		}
	}
	return ""
}

// wrapUpLimit reads the footer: when the limit shown there resets, and whether
// the status line says this tile is finished ("✓ done · close session").
func wrapUpLimit(screen string, now time.Time) (at time.Time, finished, ok bool) {
	footer := footerBelowBox(screen)
	m := wrapUpRe.FindStringSubmatch(footer)
	if m == nil {
		return time.Time{}, false, false
	}
	return resetTime(m[1], m[2], m[3], now), strings.Contains(footer, "✓ done"), true
}

// continuedAfterSwitch reads a tile's scroll-back: switched is whether the
// account-switch notice is there, continued whether "continue" was sent below
// it. Both come from the screen, so a cs restart can't repeat the continue.
func continuedAfterSwitch(history string) (switched, continued bool) {
	lines := strings.Split(history, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if isAccountSwitch(lines, i) {
			return true, continued
		}
		l := strings.TrimSpace(lines[i])
		if strings.HasPrefix(l, "❯") && strings.Contains(l, "continue") {
			continued = true
		}
	}
	return false, continued
}

// wrapUpDue says whether a tile on the footer limit should get continue now:
// straight after an account switch it hasn't continued from, else at the reset.
// A continue from an earlier switch only rules out the first case: this limit
// can be a later one, on the new account.
func wrapUpDue(switched, continued bool, at, now time.Time) bool {
	return (switched && !continued) || !now.Before(at.Add(limitMargin))
}

// resumeWrapUp types "continue" into a tile stopped by the footer limit:
// right away when the account changed since (the new account has its own
// limit), else once the limit resets. handled says the footer limit applied,
// so the older "hit your limit" rule has nothing to do here.
func resumeWrapUp(e *session.ExternalSession, screen string, done map[string]bool, now time.Time, due func(time.Time)) (handled bool) {
	at, finished, ok := wrapUpLimit(screen, now)
	if !ok {
		return false
	}
	// Mid-turn: wait for it to stop. A background shell keeps a stopped tile
	// "busy" (#2231), so that alone doesn't count as working.
	if finished || (e.Status == "busy" && !strings.Contains(footerBelowBox(screen), " shell")) {
		return true
	}
	key := e.Name + "|wrapup|" + at.Format(time.RFC3339)
	if done[key] {
		return true
	}
	switched, continued := continuedAfterSwitch(session.PaneHistory(e.Live))
	if !wrapUpDue(switched, continued, at, now) {
		due(at.Add(limitMargin))
		return true
	}
	// As in the older rule: finish a half-typed "co", leave any other draft.
	typed, hasPrompt := promptText(e.Live)
	rest, isContinue := continueRest(typed)
	if !hasPrompt || !isContinue {
		return true
	}
	done[key] = true
	_ = session.PressKeys(e.Live, rest)
	if switched && !continued {
		logEvent("daemon: %s: wrap-up limit on the old account, typed continue", e.Name)
	} else {
		logEvent("daemon: %s: wrap-up limit reset (%s), typed continue", e.Name, at.Format("15:04"))
	}
	return true
}

// isLimitNotice reports whether line i is Claude's own limit notice: a
// "⎿" result line followed by "/upgrade…". The same words quoted in a
// conversation (a test, a reply about this feature) once made the runner
// type "continue" into a session that wasn't stopped at all.
func isLimitNotice(lines []string, i int) bool {
	_, ok := limitNotice(lines, i)
	return ok
}

// limitNotice returns the limit notice starting at line i, joined across
// the lines Claude wrapped it onto: in a narrow tile "resets 5:30pm" lands
// on the next line, and the notice read alone never matched.
func limitNotice(lines []string, i int) (string, bool) {
	l := strings.TrimSpace(lines[i])
	if !strings.HasPrefix(l, "⎿") || !strings.Contains(strings.ToLower(l), "hit your") {
		return "", false
	}
	notice := l
	for j := i + 1; j < len(lines) && j <= i+4; j++ {
		next := strings.TrimSpace(lines[j])
		if strings.HasPrefix(next, "/upgrade") {
			return notice, limitRe.MatchString(strings.ToLower(notice))
		}
		if next == "" || strings.HasPrefix(next, "⎿") || strings.HasPrefix(next, "❯") || isStep(next) {
			return "", false
		}
		notice += " " + next
	}
	return "", false
}

// isStep reports whether a line starts one of Claude's steps: "●", which
// tmux shows as "⏺" in some terminals.
func isStep(l string) bool {
	return strings.HasPrefix(l, "●") || strings.HasPrefix(l, "⏺")
}

// isAccountSwitch reports whether line i starts Claude's notice that the
// signed-in claude.ai account changed (the user ran /login for another
// subscription): "Remote Control disconnected — signed-in claude.ai account
// or organization changed on this machine …". Claude wraps the notice itself,
// so tmux can't join it: in a 47-column tile "signed-in" lands on the next
// line, and the first line read alone never matched.
func isAccountSwitch(lines []string, i int) bool {
	// A step starts at column 0. Indented, it is the notice quoted in tool
	// output (a tile that read another tile's screen), which once made the
	// runner type /remote-control into that tile too.
	notice := strings.TrimRight(lines[i], " ")
	if !isStep(notice) {
		return false
	}
	for j := i + 1; j < len(lines) && j <= i+3; j++ {
		next := strings.TrimSpace(lines[j])
		if next == "" || isStep(next) {
			break
		}
		notice += " " + next
	}
	return strings.Contains(notice, "Remote Control disconnected — signed-in")
}

// limitIsLast reports whether nothing happened after the latest limit line:
// no new step below it, so Claude is still stopped there. The account-switch
// notice doesn't count as a step: Claude prints it on its own.
//
// A message sent below it ("❯ continue", other than the input box, which is
// the last ❯ line) also means Claude took it: refused, it would print a new
// limit line below. Without this, a continue Claude was still thinking on
// 90s later looked refused and got a second one. A slash command (/login,
// /remote-control) isn't a message: it goes to Claude Code, not the model.
func limitIsLast(screen string) bool {
	lines := strings.Split(screen, "\n")
	inputBox := -1
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "❯") {
			inputBox = i
			break
		}
	}
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if isLimitNotice(lines, i) {
			return true
		}
		if isStep(l) && !isAccountSwitch(lines, i) {
			return false
		}
		if sent := strings.TrimSpace(strings.TrimPrefix(l, "❯")); i != inputBox && strings.HasPrefix(l, "❯") && sent != "" && !strings.HasPrefix(sent, "/") {
			return false
		}
	}
	return false
}

// remoteControlOff reads a tile's scroll-back: the latest account-switch
// notice (or the "Login successful" of the tile /login ran in) has no
// /reload-plugins typed below it, so Remote Control or the claude.ai
// connectors are still on the old account there.
// The scroll-back, not the last step: a tile that was on a question or
// mid-turn at the switch went on working after the notice, and the runner,
// which only looked at the last step, never turned it back on (#3043, 5 Nov).
// /remote-control below the notice means it was handled; it prints no step,
// so this also survives a cs restart.
func remoteControlOff(history string) bool {
	lines := strings.Split(history, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		// Handled once /reload-plugins went in below it: the runner types it
		// after /remote-control, and a continue must not come before it.
		if strings.HasPrefix(l, "❯") && strings.HasPrefix(strings.TrimSpace(strings.TrimPrefix(l, "❯")), "/reload-plugins") {
			return false
		}
		if strings.HasPrefix(l, "⎿") && strings.HasSuffix(l, "Login successful") {
			return true
		}
		if isAccountSwitch(lines, i) {
			return true
		}
	}
	return false
}

// rcTries remembers when /remote-control was last typed into a tile, and how
// often: text that never lands (the tile's own screen showed nothing after
// "typed" was logged at 12:47 on 5 Nov) is typed again after rcRetryAfter,
// at most rcMaxTries times, instead of being written off for good.
var rcTries = map[string]rcTry{}

type rcTry struct {
	at time.Time
	n  int
}

const (
	rcRetryAfter = 90 * time.Second
	rcMaxTries   = 3
)

// rcDue says whether /remote-control should be typed into tile now.
func rcDue(tile string, now time.Time) bool {
	t := rcTries[tile]
	return t.n < rcMaxTries && (t.n == 0 || now.Sub(t.at) >= rcRetryAfter)
}

// resumeAfterLimit types "continue" into idle tiles whose usage limit has
// reset. done remembers what was already resumed (session + limit line). It
// returns when the next pending resume is due (zero: none), so the daemon
// looks again right then instead of on its next 15s scan.
func resumeAfterLimit(sessions []*session.ExternalSession, done map[string]bool, now time.Time) time.Time {
	var next time.Time
	due := func(t time.Time) {
		if t.After(now) && (next.IsZero() || t.Before(next)) {
			next = t
		}
	}
	for _, e := range sessions {
		// Not by status: a background shell left running ("1 shell still
		// running") keeps Claude "busy" while it waits on the limit (#2231).
		// The screen decides: the limit line is the last thing Claude did.
		if e.Kind != session.KindTmux || e.Status == "waiting" || e.Live == "" {
			continue
		}
		screen := session.PaneScreen(e.Live)
		at, _, ok := limitReset(screen, now)
		// Signed in to another subscription (/login): Remote Control and the
		// claude.ai connectors dropped, and the old account's limit no longer
		// applies. Turn Remote Control back on, reload the connectors and, if the tile was stopped on a limit, carry on now
		// instead of waiting for the old account's reset time.
		// Only once Claude stopped and the prompt is empty: typed into a busy
		// tile it would queue, into a question it would pick an option.
		// Remote Control off: nothing else is typed into this tile until
		// /remote-control and /reload-plugins went in, so a continue never
		// runs on the old account's connectors (glitchgrab #757 got a bare
		// continue from the wrap-up rule below, 5 Nov).
		rcOff := remoteControlOff(session.PaneHistory(e.Live))
		if !rcOff {
			delete(rcTries, e.Name) // handled: the next /login starts with fresh tries
		}
		if rcOff {
			if e.Status == "busy" || !promptEmpty(e.Live) {
				continue
			}
			if !rcDue(e.Name, now) {
				if rcTries[e.Name].n >= rcMaxTries && !done[e.Name+"|rc-gave-up"] {
					done[e.Name+"|rc-gave-up"] = true
					logEvent("daemon: %s: /remote-control typed %d times, still not on; type it in that tile", e.Name, rcMaxTries)
				}
				continue
			}
			rcTries[e.Name] = rcTry{at: now, n: rcTries[e.Name].n + 1}
			_ = session.PressKeys(e.Live, "/remote-control")
			// The claude.ai connectors (Glitchgrab: /stage's screenshot
			// upload) stay signed in to the old account until reloaded.
			time.Sleep(1500 * time.Millisecond) // let each local command finish first
			_ = session.PressKeys(e.Live, "/reload-plugins")
			logEvent("daemon: %s: claude.ai account changed, typed /remote-control and /reload-plugins", e.Name)
			if ok && limitIsLast(screen) {
				time.Sleep(1500 * time.Millisecond)
				_ = session.PressKeys(e.Live, "continue")
				done[e.Name+"|"+at.Format(time.RFC3339)] = true
				logEvent("daemon: %s: usage limit on the old account, typed continue", e.Name)
			}
			continue
		}
		// A "hit your limit" line keeps its own rule below: it finishes a
		// half-typed "co" and retries a continue sent too early.
		if !(ok && limitIsLast(screen)) && resumeWrapUp(e, screen, done, now, due) {
			continue
		}
		// promptEmpty (livepane.go) reads the colours: the greyed suggestion
		// Claude shows in an empty prompt ("ok wait") isn't text someone typed;
		// reading plain text skipped #2251 for it.
		if !ok || !limitIsLast(screen) {
			continue
		}
		if now.Before(at.Add(limitMargin)) {
			due(at.Add(limitMargin))
			continue
		}
		key := e.Name + "|" + at.Format(time.RFC3339) // once per limit, tomorrow's too
		// Text left in the prompt: the start of "continue" (typed while the
		// limit was on, #2568's "co") is finished; anything else is someone's
		// draft and is left alone.
		typed, hasPrompt := promptText(e.Live)
		rest, isContinue := continueRest(typed)
		if !hasPrompt || !isContinue {
			if hasPrompt && !done[key+"|draft"] {
				done[key+"|draft"] = true
				logEvent("daemon: %s: usage limit reset (%s), not continued: unsent text in its prompt", e.Name, at.Format("15:04"))
			}
			continue
		}
		if done[key] {
			// Typed, but the limit line is still the last thing: it went in
			// before the server's reset and Claude refused it. Once more.
			if retry := at.Add(limitRetryAfter); now.Before(retry) {
				due(retry)
			} else if !done[key+"|retry"] {
				done[key+"|retry"] = true
				_ = session.PressKeys(e.Live, "continue")
				logEvent("daemon: %s: still on the %s limit after continue, typed continue again", e.Name, at.Format("15:04"))
			}
			continue
		}
		done[key] = true
		_ = session.PressKeys(e.Live, rest)
		logEvent("daemon: %s: usage limit reset (%s), typed continue", e.Name, at.Format("15:04"))
	}
	return next
}

// continueRest returns what to type to send "continue" when typed is already
// in the prompt: all of it for an empty prompt, the remaining letters when
// typed is its start ("co" → "ntinue"); ok=false for any other text.
func continueRest(typed string) (string, bool) {
	if strings.HasPrefix("continue", strings.ToLower(typed)) {
		return "continue"[len(typed):], true
	}
	return "", false
}
