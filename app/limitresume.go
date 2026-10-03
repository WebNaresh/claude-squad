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

const limitMargin = 30 * time.Second

var limitRe = regexp.MustCompile(`hit your (?:session|usage|weekly) limit[^·]*·\s*resets\s+(\d{1,2})(?::(\d{2}))?\s*([ap]m)`)

// limitReset returns when a pane's last screen says the limit resets, from
// the latest limit line, or ok=false. A time already passed by more than
// 12 hours means tomorrow.
func limitReset(screen string, now time.Time) (time.Time, string, bool) {
	lines := strings.Split(screen, "\n")
	for i := len(lines) - 1; i >= 0 && i >= len(lines)-40; i-- {
		if !isLimitNotice(lines, i) {
			continue
		}
		m := limitRe.FindStringSubmatch(strings.ToLower(lines[i]))
		h, _ := strconv.Atoi(m[1])
		min := 0
		if m[2] != "" {
			min, _ = strconv.Atoi(m[2])
		}
		if m[3] == "pm" && h != 12 {
			h += 12
		} else if m[3] == "am" && h == 12 {
			h = 0
		}
		at := time.Date(now.Year(), now.Month(), now.Day(), h, min, 0, 0, now.Location())
		if now.Sub(at) > 12*time.Hour {
			at = at.AddDate(0, 0, 1)
		}
		return at, strings.TrimSpace(lines[i]), true
	}
	return time.Time{}, "", false
}

// isLimitNotice reports whether line i is Claude's own limit notice: a
// "⎿" result line followed by "/upgrade…". The same words quoted in a
// conversation (a test, a reply about this feature) once made the runner
// type "continue" into a session that wasn't stopped at all.
func isLimitNotice(lines []string, i int) bool {
	l := strings.TrimSpace(lines[i])
	if !strings.HasPrefix(l, "⎿") || !limitRe.MatchString(strings.ToLower(l)) {
		return false
	}
	return i+1 < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[i+1]), "/upgrade")
}

// limitIsLast reports whether nothing happened after the latest limit line:
// no new step ("●") below it, so Claude is still stopped there.
func limitIsLast(screen string) bool {
	lines := strings.Split(screen, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if isLimitNotice(lines, i) {
			return true
		}
		if strings.HasPrefix(l, "●") {
			return false
		}
	}
	return false
}


// resumeAfterLimit types "continue" into idle tiles whose usage limit has
// reset. done remembers what was already resumed (session + limit line).
func resumeAfterLimit(sessions []*session.ExternalSession, done map[string]bool, now time.Time) {
	for _, e := range sessions {
		// Not by status: a background shell left running ("1 shell still
		// running") keeps Claude "busy" while it waits on the limit (#2231).
		// The screen decides: the limit line is the last thing Claude did.
		if e.Kind != session.KindTmux || e.Status == "waiting" || e.Live == "" {
			continue
		}
		screen := session.PaneScreen(e.Live)
		at, _, ok := limitReset(screen, now)
		// promptEmpty (livepane.go) reads the colours: the greyed suggestion
		// Claude shows in an empty prompt ("ok wait") isn't text someone typed;
		// reading plain text skipped #2251 for it.
		if !ok || now.Before(at.Add(limitMargin)) || !limitIsLast(screen) || !promptEmpty(e.Live) {
			continue
		}
		key := e.Name + "|" + at.Format(time.RFC3339) // once per limit, tomorrow's too
		if done[key] {
			continue
		}
		done[key] = true
		_ = session.PressKeys(e.Live, "continue")
		logEvent("daemon: %s: usage limit reset (%s), typed continue", e.Name, at.Format("15:04"))
	}
}
