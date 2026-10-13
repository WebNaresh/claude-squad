package app

import (
	"claude-squad/session"
	"encoding/json"
	"os"
	"path/filepath"
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

// isStep reports whether a line starts one of Claude's steps: "●", which
// tmux shows as "⏺" in some terminals.
func isStep(l string) bool {
	return strings.HasPrefix(l, "●") || strings.HasPrefix(l, "⏺")
}

// isAccountSwitch reports whether a step line is Claude's notice that the
// signed-in claude.ai account changed (the user ran /login for another
// subscription): "Remote Control disconnected — signed-in claude.ai account
// or organization changed on this machine …".
func isAccountSwitch(l string) bool {
	return isStep(l) && strings.Contains(l, "Remote Control disconnected — signed-in")
}

// limitIsLast reports whether nothing happened after the latest limit line:
// no new step below it, so Claude is still stopped there. The account-switch
// notice doesn't count as a step: Claude prints it on its own.
//
// A message sent below it ("❯ continue", other than the input box, which is
// the last ❯ line) also means Claude took it: refused, it would print a new
// limit line below. Without this, a continue Claude was still thinking on
// 90s later looked refused and got a second one.
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
		if isStep(l) && !isAccountSwitch(l) {
			return false
		}
		if i != inputBox && strings.HasPrefix(l, "❯") && strings.TrimSpace(strings.TrimPrefix(l, "❯")) != "" {
			return false
		}
	}
	return false
}

// switchedAccount reports whether the last step on the screen is the
// account-switch notice: the user signed in to another subscription since
// this session last did anything.
func switchedAccount(screen string) bool {
	lines := strings.Split(screen, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if l := strings.TrimSpace(lines[i]); isStep(l) {
			return isAccountSwitch(l)
		}
	}
	return false
}

// claudeAccount names the signed-in claude.ai account and organization
// (from ~/.claude.json), so each switch is handled once per tile.
func claudeAccount() string {
	home, _ := os.UserHomeDir()
	data, err := os.ReadFile(filepath.Join(home, ".claude.json"))
	if err != nil {
		return ""
	}
	var c struct {
		OAuthAccount struct {
			AccountUUID      string `json:"accountUuid"`
			OrganizationUUID string `json:"organizationUuid"`
		} `json:"oauthAccount"`
	}
	if json.Unmarshal(data, &c) != nil {
		return ""
	}
	return c.OAuthAccount.AccountUUID + "/" + c.OAuthAccount.OrganizationUUID
}

// resumeAfterLimit types "continue" into idle tiles whose usage limit has
// reset. done remembers what was already resumed (session + limit line). It
// returns when the next pending resume is due (zero: none), so the daemon
// looks again right then instead of on its next 15s scan.
func resumeAfterLimit(sessions []*session.ExternalSession, done map[string]bool, now time.Time) time.Time {
	account := ""
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
		if switchedAccount(screen) && promptEmpty(e.Live) {
			if account == "" {
				account = claudeAccount()
			}
			key := e.Name + "|account|" + account
			if done[key] {
				continue
			}
			done[key] = true
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
		if !promptEmpty(e.Live) {
			continue
		}
		key := e.Name + "|" + at.Format(time.RFC3339) // once per limit, tomorrow's too
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
		_ = session.PressKeys(e.Live, "continue")
		logEvent("daemon: %s: usage limit reset (%s), typed continue", e.Name, at.Format("15:04"))
	}
	return next
}
