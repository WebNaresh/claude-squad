package app

import (
	"claude-squad/session"
	"claude-squad/session/tmux"
	"claude-squad/ui"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// The grid: the right side shows every session of the current project
// at once, live. The selected session is the focused tile and gets the typing;
// ⌥⇧+arrows move between tiles.

// gridEntry is one tile: a session and the project tab it belongs to.
type gridEntry struct {
	project string
	row     ui.Row
}

// gridRows lists the sessions of the current project tab.
func (m *home) gridRows() []gridEntry {
	var out []gridEntry
	for _, p := range []string{m.projectTabs.Active()} {
		for _, inst := range m.list.GetInstances() {
			if ui.InProject(inst, p) {
				out = append(out, gridEntry{p, ui.Row{Instance: inst}})
			}
		}
		// Sessions first, the project terminal last (right/bottom).
		var terms []gridEntry
		for _, e := range m.list.ExternalSessions() {
			if m.projectOf(e.Path) != p || m.isDock(e) {
				continue // the docked terminal sits under Source Control
			}
			if strings.HasPrefix(e.Name, session.TermPrefix) {
				terms = append(terms, gridEntry{p, ui.Row{External: e}})
			} else {
				out = append(out, gridEntry{p, ui.Row{External: e}})
			}
		}
		out = append(out, terms...)
	}
	return out
}

// gridFocused returns the index of the selected session among gridRows, or
// -1 when the docked terminal is selected (no tile is focused then).
func (m *home) gridFocused(rows []gridEntry) int {
	if m.isDock(m.list.GetSelectedExternal()) {
		return -1
	}
	key := m.selectedRowKey()
	for i, r := range rows {
		if r.project == m.projectTabs.Active() && rowKey(r.row.Instance, r.row.External) == key {
			return i
		}
	}
	return 0
}

// refreshGrid re-captures the tiles. It decides on the UI thread (cheap:
// which tiles are stale, which screens need resizing) and returns a command
// that runs the tmux captures on a background goroutine, so key presses and
// switches are never stuck behind them. One capture runs at a time.
func (m *home) refreshGrid() tea.Cmd {
	if !m.sessionsLoaded {
		return nil // which shell is the dock isn't known yet; it must not be sized as a tile
	}
	if m.dockNames[m.projectTabs.Active()] == "" {
		_, _ = m.ensureDock()
	}
	rows := m.gridRows()
	m.gridFocus = m.gridFocused(rows)
	if m.tileCache == nil {
		m.tileCache = map[string]cachedTile{}
	}
	// Another tab (or tiles came or went): draw it from the cache now, even
	// while a capture runs, instead of leaving the old or a blank grid up.
	if keys := m.rowKeys(rows); keys != m.gridKeys {
		m.gridKeys = keys
		m.gridTiles = m.cachedTiles(rows)
	}
	if m.gridCapturing {
		return nil
	}
	tw, th := ui.GridTileSize(len(rows), m.paneWidth, m.contentHeight)
	live := m.selectedLiveName()
	// The same Claude conversation open in two tiles (resumed in a second
	// one): both write to one transcript, so both tiles say so.
	convs := map[string]int{}
	for _, r := range rows {
		if e := r.row.External; e != nil && e.SessionID != "" {
			convs[e.SessionID]++
		}
	}
	tiles := make([]ui.GridTile, len(rows))
	var jobs []gridJob
	for i, r := range rows {
		key := rowKey(r.row.Instance, r.row.External)
		focusedRow := r.row.External != nil && r.row.External.Live != "" && r.row.External.Live == live ||
			r.row.Instance != nil && r.row.Instance.TmuxName() == live
		c, cached := m.tileCache[key]
		if cached && !focusedRow && time.Since(c.at) < time.Second && c.w == tw && c.h == th {
			// Not being typed into and fresh enough: reuse.
			t := c.tile
			t.Title = c.title
			tiles[i] = t
			continue
		}
		if cached {
			// Show the last capture until the new one arrives.
			t := c.tile
			t.Title = c.title
			tiles[i] = t
		}
		j := gridJob{idx: i, key: key, row: r.row, cursorFor: m.sessionFocus}
		j.dup = r.row.External != nil && convs[r.row.External.SessionID] > 1
		if cached && c.w == tw && c.h == th {
			j.prev = c.tile.Content
		}
		name := ""
		if r.row.Instance != nil {
			name = r.row.Instance.TmuxName()
		} else if r.row.External.Live != "" {
			name = r.row.External.Live
		}
		if name != "" && m.needsFit(name, tw, th) {
			j.fit = true
			logEvent("resize %s to %dx%d (tile)", name, tw, th)
		}
		if e := r.row.External; e != nil && e.Kind == session.KindTerminal {
			if m.gridMarkdown[e.SessionID] == nil {
				m.gridMarkdown[e.SessionID] = &ui.MarkdownCache{}
			}
			j.md = m.gridMarkdown[e.SessionID]
		}
		jobs = append(jobs, j)
	}
	if len(jobs) == 0 {
		m.gridTiles = tiles
		return nil
	}
	keys := m.gridKeys
	m.gridCapturing = true
	return func() tea.Msg {
		captured := make([]ui.GridTile, len(jobs))
		for n, j := range jobs {
			captured[n] = captureTile(j, tw, th)
		}
		return gridCapturedMsg{tiles: tiles, jobs: jobs, captured: captured, w: tw, h: th, keys: keys}
	}
}

// contentRows is the number of rows down to the last non-blank one.
func contentRows(s string) int {
	lines := strings.Split(s, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.TrimSpace(ansi.Strip(lines[i])) != "" {
			return i + 1
		}
	}
	return 0
}

// gridJob is one tile to capture on the background goroutine.
type gridJob struct {
	prev      string // the tile's last capture at this size, to spot half-drawn screens
	idx       int
	key       string
	row       ui.Row
	fit       bool
	cursorFor string
	dup       bool // its conversation is open in another tile too
	md        *ui.MarkdownCache
}

type gridCapturedMsg struct {
	tiles    []ui.GridTile
	jobs     []gridJob
	captured []ui.GridTile
	w, h     int
	keys     string // gridKeys when the capture started
}

// applyGridCapture stores the background capture's tiles (UI thread).
func (m *home) applyGridCapture(msg gridCapturedMsg) {
	m.gridCapturing = false
	for n, j := range msg.jobs {
		t := msg.captured[n]
		m.tileCache[j.key] = cachedTile{tile: t, title: t.Title, at: time.Now(), w: msg.w, h: msg.h}
		msg.tiles[j.idx] = t
	}
	if msg.keys != m.gridKeys {
		// The tab changed or tiles came or went while this capture ran: it
		// only warms the cache; the grid on screen is already the new one.
		return
	}
	m.gridTiles = msg.tiles
}

// captureTile runs a tile's tmux calls. It runs off the UI thread and must
// not touch the model.
func captureTile(j gridJob, w, h int) ui.GridTile {
	if inst := j.row.Instance; inst != nil {
		t := ui.GridTile{Title: inst.Title, NeedsYou: inst.NeedsYou, Status: "agent"}
		if !inst.Started() || inst.Paused() {
			t.Status, t.Content = "paused", "Paused. Select it and press r to resume."
			return t
		}
		if j.fit {
			_ = inst.SetPreviewSize(w, h)
		}
		t.Content, _ = inst.Preview()
		t.Content = cursorInto(inst.TmuxName(), j.cursorFor, t.Content, 0)
		return t
	}
	e := j.row.External
	t := ui.GridTile{Title: e.Title(), NeedsYou: e.NeedsYou(), Status: e.Status, Stage: session.StagePhase(e.SessionID)}
	if j.dup {
		t.Stage = "⚠ same conversation open in 2 tiles · close one"
	}
	if e.Kind == session.KindTerminal {
		md, version := e.Conversation()
		t.Status = "view only"
		t.Content = j.md.Render(md, version, w)
		return t
	}
	name, err := e.EnsureLive(w, h)
	if err != nil {
		t.Content = err.Error()
		return t
	}
	if j.fit {
		(&session.ExternalSession{Kind: e.Kind, Name: e.Name, Live: name, Clients: e.Clients}).FitTo(w, h)
		// Give the program time to redraw for the new size; captured at once
		// it is still half drawn (seen as half-empty tiles after resizes).
		time.Sleep(150 * time.Millisecond)
	}
	t.Content, _ = tmux.NewExternalTmuxSession(name).CapturePaneContent()
	if j.prev != "" && contentRows(t.Content) < contentRows(j.prev)*2/3 {
		// Much emptier than last time: likely caught Claude between erasing
		// its lower lines and redrawing them. Look again a moment later; a
		// real clear (/clear) is still empty then.
		time.Sleep(40 * time.Millisecond)
		t.Content, _ = tmux.NewExternalTmuxSession(name).CapturePaneContent()
	}
	dropped := 0
	if !tmux.IsScrolled(name) {
		t.Content, dropped = fillFromHistory(name, t.Content)
	}
	if strings.Contains(ansi.Strip(t.Content), "Save and close editor to continue") {
		// Claude opened the prompt in an editor (Ctrl+G) and reads nothing
		// until it closes; looked like a frozen session.
		t.Stage, t.NeedsYou = "? waiting for your editor · close its tab", false
	}
	if r := contentRows(t.Content); r > 0 && r < h/2 {
		// Evidence for half-empty tiles: what was captured, and why.
		logEvent("half-empty tile %s: text in %d of %d rows (fit=%v dropped=%d)", name, r, h, j.fit, dropped)
	}
	t.Content = cursorInto(name, j.cursorFor, t.Content, dropped)
	return t
}

// fillFromHistory bottom-aligns an inline screen. Claude (not full-screen)
// draws from the top, so after its window grows the lower rows stay empty
// and the tile looked half filled. The empty rows are dropped and the same
// number of earlier lines from the pane's history fill the top. It returns
// the content and how many rows were dropped from the bottom.
func fillFromHistory(name, content string) (string, int) {
	lines := strings.Split(strings.TrimSuffix(content, "\n"), "\n")
	rows := contentRows(content)
	blank := len(lines) - rows
	if blank < 2 || rows == 0 {
		return content, 0
	}
	out, err := exec.Command("tmux", "display-message", "-p", "-t", name, "#{alternate_on} #{history_size}").Output()
	if err != nil {
		return content, 0
	}
	var alt, hist int
	if _, err := fmt.Sscanf(strings.TrimSpace(string(out)), "%d %d", &alt, &hist); err != nil || alt == 1 {
		return content, 0 // full-screen apps lay out their own screen
	}
	var above []string
	if n := min(blank, hist); n > 0 {
		h, err := exec.Command("tmux", "capture-pane", "-p", "-e", "-J", "-S", strconv.Itoa(-n), "-E", "-1", "-t", name).Output()
		if err == nil {
			above = strings.Split(strings.TrimSuffix(string(h), "\n"), "\n")
		}
	}
	return strings.Join(append(above, lines[:rows]...), "\n") + "\n", blank
}

// cursorInto draws the cursor into a tile when it's the session being typed into.
// dropped is how many rows were cut from the bottom of the screen
// (fillFromHistory), which moves the cursor that much closer to the bottom.
func cursorInto(target, focus, content string, dropped int) string {
	if target == "" || target != focus {
		return content
	}
	if x, fromBottom, visible, err := tmux.Cursor(target); err == nil && visible {
		return ui.DrawCursor(content, x, fromBottom-dropped)
	}
	return content
}

// projectNames maps each tab's path to its shown name.
func projectNames(projects []string) map[string]string {
	out := map[string]string{}
	for i, n := range ui.TabNames(projects) {
		out[projects[i]] = n
	}
	return out
}

// moveTile moves focus within the grid: ±1 sideways, ±columns vertically,
// switching project tab when the tile belongs to another project.
func (m *home) moveTile(dx, dy int) tea.Cmd {
	logEvent("move tile dx=%d dy=%d", dx, dy)
	if dy < 0 && dx == 0 && m.isDock(m.list.GetSelectedExternal()) {
		return m.focusServers() // ↑ from the terminal: the Servers list above it
	}
	rows := m.gridRows()
	n := len(rows)
	if n == 0 {
		return nil
	}
	cols, _, _ := ui.GridLayout(n, m.paneWidth, m.contentHeight)
	cur := m.gridFocused(rows)
	if cur < 0 {
		// From the dock (left of the grid): → goes to the first tile.
		if dx <= 0 {
			return nil
		}
		cur, dx = 0, 0
	} else if dx < 0 && cur == 0 {
		return m.focusDock() // ← from the first tile
	}
	// ← / → run through the tiles in reading order, wrapping between rows:
	// Terminal.app sends Shift+↑/↓ as plain ↑/↓ (they reach Claude), so
	// sideways is the reliable way to every tile.
	i := cur + dx + dy*cols
	if dy > 0 && i >= n && cur/cols < (n-1)/cols {
		i = n - 1 // nothing directly below: go to the last tile on the next row
	}
	if i < 0 || i >= n {
		return nil
	}
	target := rows[i]
	var cmd tea.Cmd
	if target.project != m.projectTabs.Active() {
		cmd = m.switchProject(m.projectTabs.Select(indexOf(m.projectTabs.Projects(), target.project)))
	}
	if target.row.Instance != nil {
		m.list.SelectInstance(target.row.Instance)
	} else {
		m.list.SelectExternal(target.row.External.Name)
	}
	return tea.Batch(cmd, m.autoFocus(), m.refreshGrid())
}

// Auto-focus on questions: when a session starts waiting on the user, select
// it and give it the keyboard, switching project tab if needed. To never
// steal keystrokes mid-sentence it only jumps after typingPause without a
// key press; otherwise it points to it in the message line.

const typingPause = 3 * time.Second

// rowKey identifies an agent or external session across refreshes.
func rowKey(inst *session.Instance, e *session.ExternalSession) string {
	if inst != nil {
		return "agent:" + inst.TmuxName()
	}
	return "session:" + e.Name
}

// focusNewQuestions is called after each status refresh. In the project on
// screen, when the session you're on doesn't need you and you've paused
// typing, it moves the focus to a session that asks a question, including
// ones that were already waiting (e.g. the next one after you answered one).
// Each question pulls focus once, so moving away on purpose sticks. A new
// question in another project only shows a note (! jumps there).
func (m *home) focusNewQuestions() tea.Cmd {
	type asking struct {
		key, project string
		inst         *session.Instance
		ext          *session.ExternalSession
	}
	var all []asking
	waiting := map[string]bool{}
	for _, inst := range m.list.GetInstances() {
		if inst.NeedsYou {
			all = append(all, asking{rowKey(inst, nil), m.projectOf(inst.Path), inst, nil})
		}
	}
	for _, e := range m.list.ExternalSessions() {
		if e.NeedsYou() {
			all = append(all, asking{rowKey(nil, e), m.projectOf(e.Path), nil, e})
		}
	}
	title := func(a asking) string {
		if a.inst != nil {
			return a.inst.Title
		}
		return a.ext.Title()
	}
	active := m.projectTabs.Active()
	first := m.needsSeen == nil
	newElsewhere := ""
	for _, a := range all {
		waiting[a.key] = true
		if !first && !m.needsSeen[a.key] && a.project != "" {
			logEvent("new question: %s in %s", title(a), filepath.Base(a.project))
			if a.project != active && newElsewhere == "" {
				newElsewhere = title(a) + " in " + filepath.Base(a.project)
			}
		}
	}
	m.needsSeen = waiting
	if m.questionJumped == nil {
		m.questionJumped = map[string]bool{}
	}
	for k := range m.questionJumped {
		if !waiting[k] {
			delete(m.questionJumped, k) // answered: a later question may jump again
		}
	}
	note := func() tea.Cmd {
		if newElsewhere == "" {
			return nil
		}
		return m.handleError(fmt.Errorf("❓ %s needs you · press ! to jump there", newElsewhere))
	}
	if time.Since(m.lastKey) < typingPause || m.state != stateDefault || m.sourceControl.Focused() {
		return note()
	}
	if waiting[m.selectedRowKey()] {
		return note() // you're on a question already
	}
	for _, a := range all {
		if a.project != active || m.questionJumped[a.key] {
			continue
		}
		m.questionJumped[a.key] = true
		logEvent("auto-jump to question: %s (idle %s)", title(a), time.Since(m.lastKey).Round(time.Second))
		if a.inst != nil {
			m.list.SelectInstance(a.inst)
		} else {
			m.list.SelectExternal(a.ext.Name)
		}
		return tea.Batch(note(), m.autoFocus())
	}
	return note()
}

// projectOf returns the tab a session belongs to, or "".
func (m *home) projectOf(path string) string {
	for _, p := range m.projectTabs.Projects() {
		if path == p || strings.HasPrefix(path, p+string(filepath.Separator)) {
			return p
		}
	}
	return ""
}

// needsFit reports whether a session's screen must be resized to w×h, and
// records that size as current. It tracks the size each session has now (not
// every size it ever had), so going back to an earlier layout resizes again.
func (m *home) needsFit(name string, w, h int) bool {
	size := fmt.Sprintf("%dx%d", w, h)
	if m.fitted[name] == size {
		return false
	}
	m.fitted[name] = size
	return true
}

// cachedTile is a tile's last capture, reused for tiles not being typed into.
type cachedTile struct {
	tile  ui.GridTile
	title string
	at    time.Time
	w, h  int
}

// renderGridCached re-renders the grid only when a tile, the focus or the
// size changed; drawing many tiles costs up to ~30ms, and most frames
// change nothing.
func (m *home) renderGridCached() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d|%d|%d|", m.gridFocus, m.paneWidth, m.contentHeight)
	for _, t := range m.gridTiles {
		fmt.Fprintf(&b, "%s|%s|%v|%d|", t.Title, t.Status, t.NeedsYou, len(t.Content))
		b.WriteString(t.Content)
	}
	// The progress strip sits in the row above the tiles (progress.go).
	strip := m.renderProgress(m.paneWidth)
	b.WriteString(strip)
	key := b.String()
	if key == m.gridRenderKey && m.gridRendered != "" {
		return m.gridRendered
	}
	m.gridRenderKey = key
	m.gridRendered = lipgloss.JoinVertical(lipgloss.Left, strip,
		ui.RenderGrid(m.gridTiles, m.gridFocus, m.paneWidth, m.contentHeight))
	return m.gridRendered
}

// rowKeys names the grid's sessions in order.
func (m *home) rowKeys(rows []gridEntry) string {
	var b strings.Builder
	for _, r := range rows {
		b.WriteString(rowKey(r.row.Instance, r.row.External))
		b.WriteByte('|')
	}
	return b.String()
}

// cachedTiles builds the grid from the last captures; a session never
// captured yet shows its title over an empty tile until its first capture.
func (m *home) cachedTiles(rows []gridEntry) []ui.GridTile {
	tiles := make([]ui.GridTile, len(rows))
	for i, r := range rows {
		if c, ok := m.tileCache[rowKey(r.row.Instance, r.row.External)]; ok {
			tiles[i] = c.tile
			tiles[i].Title = c.title
		} else if r.row.Instance != nil {
			tiles[i] = ui.GridTile{Title: r.row.Instance.Title, Content: "Loading…"}
		} else {
			tiles[i] = ui.GridTile{Title: r.row.External.Title(), Content: "Loading…"}
		}
	}
	return tiles
}
