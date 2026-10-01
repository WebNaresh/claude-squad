package app

import (
	"claude-squad/log"
	"claude-squad/session"
	"claude-squad/session/tmux"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Typing goes to the selected session by default: every key goes to its tmux
// pane, as if typing in its terminal. cs keeps only modified keys for itself,
// so arrows, Esc, ctrl+c and Tab still reach Claude:
//   option+←/→  switch project tab     option+↑/↓  switch session
//   option+s    Source Control         option+i    back to the session
//   ctrl+q      stop typing (ctrl+q twice quits)
// Option keys need Terminal's "Use Option as Meta key"; Option+←/→ also
// arrive as alt+b/alt+f, Terminal.app's default word-jump codes.

var viewOnlyHintStyle = lipgloss.NewStyle().Background(lipgloss.Color("240")).Foreground(lipgloss.Color("230"))

var focusHintStyle = lipgloss.NewStyle().Background(lipgloss.Color("62")).Foreground(lipgloss.Color("230"))

// tmuxKeyNames maps bubbletea key names to tmux send-keys names.
var tmuxKeyNames = map[string]string{
	"enter": "Enter", "backspace": "BSpace", "tab": "Tab", "shift+tab": "BTab",
	"esc": "Escape", "up": "Up", "down": "Down", "left": "Left", "right": "Right",
	"home": "Home", "end": "End", "pgup": "PPage", "pgdown": "NPage",
	"delete": "DC", "insert": "IC", " ": "Space",
	"ctrl+up": "C-Up", "ctrl+down": "C-Down", "ctrl+left": "C-Left", "ctrl+right": "C-Right",
	"alt+enter": "M-Enter", "shift+enter": "S-Enter",
	"shift+left": "S-Left", "shift+right": "S-Right", "shift+up": "S-Up", "shift+down": "S-Down",
	"shift+home": "S-Home", "shift+end": "S-End",
}

// keyQueue sends typed keys to tmux in order on a background goroutine, so
// a held-down key (auto-repeat) never waits on, or is dropped by, the UI.
var keyQueue = make(chan queuedKey, 512)

type queuedKey struct {
	target string
	msg    tea.KeyMsg
	at     time.Time
	wheel  int // mouse wheel (capture on): scroll this many lines, msg is up/down
}

// Mouse-wheel scrolling: with mouse capture off, Terminal.app turns the wheel
// into bursts of ↑/↓ keys 0–5ms apart. Sent to Claude they recall old
// messages, so a burst (arrows closer than wheelGap) scrolls the session's
// screen back in tmux instead. A single arrow (a real key press, or Claude's
// menus) is sent normally, wheelGap later. Any other key first returns the
// screen to live.
const wheelGap = 10 * time.Millisecond

func init() {
	go func() {
		scrolled := map[string]bool{} // targets currently scrolled back
		var lastWheel time.Time       // when the last wheel burst was seen
		var pending *queuedKey
		next := func() (queuedKey, bool) {
			if pending != nil {
				k := *pending
				pending = nil
				return k, true
			}
			k, ok := <-keyQueue
			return k, ok
		}
		for {
			k, ok := next()
			if !ok {
				return
			}
			dir := k.msg.String()
			if k.wheel > 0 {
				// Real wheel events: add up the ones already waiting for the same
				// tile and direction, then scroll once.
				lines := k.wheel
			wheel:
				for {
					select {
					case n := <-keyQueue:
						if n.wheel > 0 && n.target == k.target && n.msg.String() == dir {
							lines += n.wheel
							continue
						}
						pending = &n
						break wheel
					default:
						break wheel
					}
				}
				if scrollPane(k.target, dir, lines) {
					scrolled[k.target] = true
					tmux.SetScrolled(k.target, true)
				}
				continue
			}
			if dir == "up" || dir == "down" {
				count := 1
				timer := time.NewTimer(wheelGap)
			burst:
				for {
					select {
					case n := <-keyQueue:
						if n.msg.String() == dir && n.target == k.target {
							count++
							timer.Reset(wheelGap)
							continue
						}
						pending = &n
						break burst
					case <-timer.C:
						break burst
					}
				}
				timer.Stop()
				// A burst, or a single notch right after one (slow wheel turns
				// send one arrow at a time), is scrolling, not a key press.
				if count > 1 || time.Since(lastWheel) < wheelSticky {
					lastWheel = time.Now()
					if scrollPane(k.target, dir, count) {
						scrolled[k.target] = true
						tmux.SetScrolled(k.target, true)
					}
					continue
				}
			}
			// Plain letters already waiting go to tmux in one call: starting a
			// tmux process per key is what makes fast typing lag.
			if plainText(k.msg) {
				k.msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: textOf(k.msg)}
			batch:
				for {
					select {
					case n := <-keyQueue:
						if n.target == k.target && plainText(n.msg) {
							k.msg.Runes = append(append([]rune{}, k.msg.Runes...), textOf(n.msg)...)
							continue
						}
						pending = &n
						break batch
					default:
						break batch
					}
				}
			}
			if scrolled[k.target] {
				// Leave scroll-back before typing, so the key reaches Claude.
				_ = exec.Command("tmux", "send-keys", "-t", k.target, "-X", "cancel").Run()
				delete(scrolled, k.target)
				tmux.SetScrolled(k.target, false)
			}
			logKey(fmt.Sprintf("sent+%dms", time.Since(k.at).Milliseconds()), k.target, k.msg)
			if err := sendKey(k.target, k.msg); err != nil {
				logEvent("key send failed to %s: %v", k.target, err)
				log.WarningLog.Printf("session pane: sending %q to %s: %v", k.msg.String(), k.target, err)
			}
		}
	}()
}

// plainText reports keys that are just typed text (letters or space), which
// can be sent to tmux together.
func plainText(msg tea.KeyMsg) bool {
	return (msg.Type == tea.KeyRunes && !msg.Paste && !msg.Alt) || msg.Type == tea.KeySpace
}

func textOf(msg tea.KeyMsg) []rune {
	if msg.Type == tea.KeySpace {
		return []rune{' '}
	}
	return msg.Runes
}

// wheelSticky: a single arrow this soon after a wheel burst is still the wheel.
const wheelSticky = 300 * time.Millisecond

// scrollPane scrolls a session. Full-screen programs (Claude in `claude
// attach`, which draws on the alternate screen and has no tmux scroll-back)
// get real mouse-wheel events, which Claude scrolls its own view with;
// others scroll back in tmux's copy mode (-e leaves it at the bottom). It
// reports whether tmux copy mode was used.
func scrollPane(target, dir string, lines int) bool {
	alt, _ := exec.Command("tmux", "display-message", "-p", "-t", target, "#{alternate_on}").Output()
	if strings.TrimSpace(string(alt)) == "1" {
		logEvent("wheel scroll %s %d on %s (wheel events to the full-screen app)", dir, lines, target)
		ev := "\x1b[<64;1;1M" // SGR mouse wheel up
		if dir == "down" {
			ev = "\x1b[<65;1;1M"
		}
		_ = exec.Command("tmux", "send-keys", "-t", target, "-l", strings.Repeat(ev, lines)).Run()
		return false
	}
	logEvent("wheel scroll %s %d lines on %s (tmux scroll-back, not sent to Claude)", dir, lines, target)
	if dir == "up" {
		_ = exec.Command("tmux", "copy-mode", "-e", "-t", target).Run()
		_ = exec.Command("tmux", "send-keys", "-t", target, "-X", "-N", strconv.Itoa(lines), "scroll-up").Run()
		return true
	}
	_ = exec.Command("tmux", "send-keys", "-t", target, "-X", "-N", strconv.Itoa(lines), "scroll-down").Run()
	return true
}

// sendKey forwards one key press to a tmux session.
func sendKey(target string, msg tea.KeyMsg) error {
	if msg.Paste {
		// Bracketed paste keeps a multi-line paste from submitting early.
		load := exec.Command("tmux", "load-buffer", "-b", "cs-paste", "-")
		load.Stdin = strings.NewReader(string(msg.Runes))
		if err := load.Run(); err != nil {
			return err
		}
		return exec.Command("tmux", "paste-buffer", "-p", "-d", "-b", "cs-paste", "-t", target).Run()
	}
	if msg.Type == tea.KeyRunes {
		args := []string{"send-keys", "-t", target}
		if msg.Alt {
			return exec.Command("tmux", append(args, "M-"+string(msg.Runes))...).Run()
		}
		return exec.Command("tmux", append(args, "-l", "--", string(msg.Runes))...).Run()
	}
	key, ok := tmuxKey(msg.String())
	if !ok {
		// Never send an unknown name: tmux would type it as literal text.
		log.InfoLog.Printf("session pane: key %q not forwarded", msg.String())
		return nil
	}
	return exec.Command("tmux", "send-keys", "-t", target, key).Run()
}

// tmuxKey translates a bubbletea key name (e.g. "alt+backspace",
// "ctrl+w") into a tmux send-keys name ("M-BSpace", "C-w").
func tmuxKey(name string) (string, bool) {
	if key, ok := tmuxKeyNames[name]; ok {
		return key, true
	}
	if rest, ok := strings.CutPrefix(name, "alt+"); ok {
		if key, ok := tmuxKey(rest); ok {
			return "M-" + key, true
		}
		if len([]rune(rest)) == 1 {
			return "M-" + rest, true
		}
		return "", false
	}
	if rest, ok := strings.CutPrefix(name, "ctrl+"); ok && len(rest) == 1 {
		return "C-" + rest, true
	}
	return "", false
}

// liveTarget returns the tmux session to type into for the selected row,
// starting a background session's attach wrapper if needed.
func (m *home) liveTarget() (string, error) {
	if len(m.gridRows()) == 0 && m.projectTabs.Active() != "" {
		// Nothing running in this project: give it a terminal tile.
		return m.ensureTerminal()
	}
	if e := m.list.GetSelectedExternal(); e != nil {
		w, h := m.tabbedWindow.GetPreviewSize()
		return e.EnsureLive(max(w, 40), max(h, 10))
	}
	inst := m.list.GetSelectedInstance()
	if inst == nil {
		return "", fmt.Errorf("select a session first")
	}
	if !inst.Started() || inst.Paused() || !inst.TmuxAlive() {
		return "", fmt.Errorf("this agent isn't running; press r to resume it")
	}
	return inst.TmuxName(), nil
}

// focusSession gives the session pane the keyboard.
func (m *home) focusSession() tea.Cmd {
	target, err := m.liveTarget()
	if err != nil {
		return m.handleError(err)
	}
	m.sessionFocus = target
	m.sessionFocusRow = m.selectedRowKey()
	m.tabbedWindow.ShowPreview()
	return m.instanceChanged()
}

// autoFocus gives the keyboard to the selected session when it can be typed
// into (not view-only), unless Source Control has focus.
func (m *home) autoFocus() tea.Cmd {
	defer func() {
		if m.sessionFocus != "" {
			logEvent("typing -> %s (%s)", m.sessionFocus, m.selectedRowKey())
		} else {
			logEvent("typing off (nothing typeable selected: %s)", m.selectedRowKey())
		}
	}()
	m.sessionFocus = ""
	if m.sourceControl.Focused() || m.state != stateDefault {
		return m.instanceChanged()
	}
	if e := m.list.GetSelectedExternal(); e != nil && e.Kind == session.KindTerminal {
		return m.instanceChanged()
	}
	if target, err := m.liveTarget(); err == nil {
		m.sessionFocus = target
		m.sessionFocusRow = m.selectedRowKey()
		m.tabbedWindow.ShowPreview()
	}
	return m.instanceChanged()
}

// moveSession switches to the previous/next session and types into it.
func (m *home) moveSession(up bool) tea.Cmd {
	logEvent("move session up=%v", up)
	if up {
		m.list.Up()
	} else {
		m.list.Down()
	}
	return m.autoFocus()
}

// moveProject switches project tab and types into its selected session.
func (m *home) moveProject(left bool) tea.Cmd {
	var project string
	if left {
		project = m.projectTabs.Prev()
	} else {
		project = m.projectTabs.Next()
	}
	return tea.Batch(m.switchProject(project), m.autoFocus())
}

// navKey handles the Option shortcuts that work everywhere: switching
// project, session, and between Source Control and the session.
func (m *home) navKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	switch msg.String() {
	case "alt+left", "alt+b":
		return m.moveProject(true), true
	case "alt+right", "alt+f":
		return m.moveProject(false), true
	case "alt+up":
		return m.moveSession(true), true
	case "alt+down":
		return m.moveSession(false), true
	case "alt+s", "alt+S", "ß":
		return m.openSourceControl(), true
	// ⌥T, or Ctrl+Shift+5 as in VS Code. Terminals send no standard code for
	// Ctrl+Shift+5; xterm-style ones send ctrl+] (0x1D), others may be mapped
	// to it in Terminal's keyboard settings.
	case "alt+t", "alt+T", "†":
		return m.openTerminal(), true
	case "alt+n", "alt+N", "˜":
		return m.openIssuePicker(), true
	// Terminal.app sends Option+Shift+arrows as plain Shift+arrows, so both
	// move between tiles in the grid.
	case "alt+shift+left", "shift+left":
		return m.moveTile(-1, 0), true
	case "alt+shift+right", "shift+right":
		return m.moveTile(1, 0), true
	case "alt+shift+up", "shift+up":
		return m.moveTile(0, -1), true
	case "alt+shift+down", "shift+down":
		return m.moveTile(0, 1), true
	case "alt+i", "alt+I":
		m.sourceControl.SetFocused(false)
		m.tabbedWindow.ClearFileDiff()
		return m.autoFocus(), true
	}
	return nil, false
}

// openSourceControl moves the keyboard to the Source Control column.
func (m *home) openSourceControl() tea.Cmd {
	logEvent("source control focused")
	m.sessionFocus = ""
	m.sourceControl.SetFocused(true)
	return m.showSelectedFileDiff()
}

// handleSessionKey routes a key while typing into a session.
func (m *home) handleSessionKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if msg.String() == "ctrl+q" {
		logEvent("typing off: ctrl+q")
		m.sessionFocus = ""
		return m, m.instanceChanged()
	}
	if cmd, ok := m.navKey(msg); ok {
		return m, cmd
	}
	if msg.String() == "left" && promptEmpty(m.sessionFocus) {
		logEvent("dropped ← on empty prompt (would open Claude's agent view)")
		// On an empty prompt Claude's ← opens its agent view; nothing to move.
		return m, nil
	}
	logKey("queue", m.sessionFocus, msg)
	// Forwarding a key changes nothing on screen until the session echoes it
	// (picked up by the next preview tick), so skip rebuilding the view. A
	// wheel scroll is hundreds of these per second.
	m.skipRender = true
	select {
	case keyQueue <- queuedKey{target: m.sessionFocus, msg: msg, at: time.Now()}:
	default:
		log.WarningLog.Printf("session pane: key queue full, dropped %q", msg.String())
		logEvent("key DROPPED (queue full): %s", describeKey(msg))
	}
	return m, nil
}

// timeNow is time.Now, named for use in other files without an extra import.
func timeNow() time.Time { return time.Now() }

// selectedLiveName is the tmux session the selected row shows, if known
// (without starting anything).
func (m *home) selectedLiveName() string {
	if e := m.list.GetSelectedExternal(); e != nil {
		return e.Live
	}
	if inst := m.list.GetSelectedInstance(); inst != nil {
		return inst.TmuxName()
	}
	return ""
}

// selectedRowKey identifies the selected row, so typing focus can be dropped
// when the selection moves to a different session.
func (m *home) selectedRowKey() string {
	if e := m.list.GetSelectedExternal(); e != nil {
		return "session:" + e.Name
	}
	if inst := m.list.GetSelectedInstance(); inst != nil {
		return "agent:" + inst.TmuxName()
	}
	return ""
}

// promptEmpty reports whether the Claude input line on a session's screen
// holds no typed text (the line starting with ❯). Claude shows a dimmed
// suggestion on an empty prompt, so dim text counts as empty.
func promptEmpty(target string) bool {
	out, err := exec.Command("tmux", "capture-pane", "-p", "-e", "-t", target).Output()
	if err != nil {
		return false
	}
	lines := strings.Split(string(out), "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if _, rest, ok := strings.Cut(lines[i], "❯"); ok && strings.TrimSpace(stripANSI(lines[i][:strings.Index(lines[i], "❯")])) == "" {
			return strings.TrimSpace(undimmedText(rest)) == ""
		}
	}
	return false
}

var ansiRe = regexp.MustCompile("\x1b\\[[0-9;]*m")

func stripANSI(s string) string { return ansiRe.ReplaceAllString(s, "") }

// undimmedText returns the characters of s that are not drawn dim (SGR 2).
func undimmedText(s string) string {
	var b strings.Builder
	dim := false
	for len(s) > 0 {
		if loc := ansiRe.FindStringIndex(s); loc != nil && loc[0] == 0 {
			for _, p := range strings.Split(strings.Trim(s[2:loc[1]-1], ";"), ";") {
				switch p {
				case "2":
					dim = true
				case "0", "", "22":
					dim = false
				}
			}
			s = s[loc[1]:]
			continue
		}
		r, size := utf8.DecodeRuneInString(s)
		if !dim {
			b.WriteRune(r)
		}
		s = s[size:]
	}
	return b.String()
}

// closeLiveWrappers ends cs's hidden attach wrappers on a real quit.
func closeLiveWrappers() {
	session.CloseAttachWrappers()
}
