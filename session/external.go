package session

import (
	"bufio"
	"bytes"
	"claude-squad/session/tmux"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ExternalPrefix is the tmux session prefix used by the claude launcher
// (~/.claude-squad/bin/claude) and by `s` for sessions in a project folder.
const ExternalPrefix = "cc_"

// ExternalKind says how claude-squad can reach a session it did not start.
type ExternalKind int

const (
	// KindTmux runs in a cc_ tmux session: live preview, attach with ctrl-q back.
	KindTmux ExternalKind = iota
	// KindBackground is hosted by Claude Code's own background service:
	// attach with `claude attach`, Ctrl+Z back.
	KindBackground
	// KindTerminal runs directly in some terminal (e.g. VS Code): view only.
	KindTerminal
)

// ExternalSession is a Claude session claude-squad did not start as an agent.
// It can be previewed and (for tmux and background kinds) attached to; it is
// never killed, saved or diffed.
type ExternalSession struct {
	Kind ExternalKind
	// Name is the tmux session name (KindTmux) or Claude's short id (KindBackground).
	Name string
	// Path is the folder the session runs in.
	Path string
	// Clients is how many terminals are attached to a tmux session.
	Clients int
	// Label is Claude's session name, Status is "busy" or "idle", SessionID the
	// transcript id; all from `claude agents --json` when known.
	Label     string
	Status    string
	SessionID string
	Pid       int
	// Prompted is true when the session's screen shows a Claude prompt
	// waiting for an answer (e.g. the folder-trust dialog, which Claude
	// doesn't report as "waiting").
	Prompted bool
	// Live is the tmux session showing this session's screen: Name for
	// KindTmux, the attach wrapper for KindBackground once it exists.
	Live string
}

// agentInfo is one entry of `claude agents --json`.
type agentInfo struct {
	ID        string `json:"id"`
	SessionID string `json:"sessionId"`
	Cwd       string `json:"cwd"`
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	Pid       int    `json:"pid"`
}

// RealClaude returns the claude binary itself, skipping the tmux launcher.
func RealClaude() string {
	if home, err := os.UserHomeDir(); err == nil {
		p := filepath.Join(home, ".local", "bin", "claude")
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return "claude"
}

var (
	agentsMu      sync.Mutex
	agentsCache   []agentInfo
	agentsFetched time.Time
)

// claudeAgents lists every running Claude session via `claude agents --json`,
// cached for ten seconds: starting the claude program costs a lot of CPU
// (~20% per run measured). Questions on live screens are still detected
// every refresh from the screen itself.
func claudeAgents() []agentInfo {
	agentsMu.Lock()
	defer agentsMu.Unlock()
	if time.Since(agentsFetched) < 10*time.Second {
		return agentsCache
	}
	agentsFetched = time.Now()
	cmd := exec.Command(RealClaude(), "agents", "--json")
	cmd.Env = append(os.Environ(), "CLAUDE_NO_TMUX=1")
	out, err := cmd.Output()
	if err != nil {
		return agentsCache
	}
	var agents []agentInfo
	if err := json.Unmarshal(out, &agents); err != nil {
		return agentsCache
	}
	agentsCache = agents
	return agents
}

// Background sessions the user closed (⌃Space W, or /exit inside the tile).
// `claude attach` resumes a stopped session, and cs attaches every background
// session it lists to show it live, so without this a closed session came back
// within seconds (the agents list is cached for 10s). A closed session stays
// hidden until Claude reports it busy again, i.e. the user resumed it.
var (
	closedMu sync.Mutex
	closedBG = map[string]bool{}
	attached = map[string]bool{} // background ids cs opened a wrapper for
)

// MarkClosed hides a background session and forgets it from the cached
// agents list.
func MarkClosed(id string) {
	closedMu.Lock()
	closedBG[id] = true
	delete(attached, id)
	closedMu.Unlock()
	agentsMu.Lock()
	kept := agentsCache[:0:0]
	for _, a := range agentsCache {
		if a.ID != id {
			kept = append(kept, a)
		}
	}
	agentsCache = kept
	agentsMu.Unlock()
}

// isClosed reports whether a listed background session was closed by the
// user; a busy one was resumed since, so it is shown again.
func isClosed(a agentInfo) bool {
	closedMu.Lock()
	defer closedMu.Unlock()
	if !closedBG[a.ID] {
		return false
	}
	if a.Status == "busy" {
		delete(closedBG, a.ID)
		return false
	}
	return true
}

// ListExternalSessions returns every Claude session claude-squad did not start
// as an agent: cc_ tmux sessions plus whatever Claude Code itself reports. It
// also returns Claude's status ("busy", "idle", "waiting"…) for cs's own
// agents, keyed by their tmux session name.
func ListExternalSessions() ([]*ExternalSession, map[string]string, error) {
	var sessions []*ExternalSession
	byPanePid := map[int]*ExternalSession{}
	wrappers := map[string]bool{}
	agentPanes := map[int]string{}
	agentStatus := map[string]string{}

	if out, err := exec.Command("tmux", "list-panes", "-a", "-F",
		"#{session_name}\t#{session_path}\t#{session_attached}\t#{pane_pid}").Output(); err == nil {
		seen := map[string]bool{}
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			parts := strings.Split(line, "\t")
			if len(parts) == 4 && strings.HasPrefix(parts[0], AttachPrefix) {
				wrappers[parts[0]] = true
			}
			if len(parts) == 4 && strings.HasPrefix(parts[0], tmux.TmuxPrefix) {
				if pid, err := strconv.Atoi(parts[3]); err == nil {
					agentPanes[pid] = parts[0]
				}
			}
			isTerm := len(parts) == 4 && strings.HasPrefix(parts[0], TermPrefix)
			if len(parts) != 4 || !(strings.HasPrefix(parts[0], ExternalPrefix) || isTerm) || seen[parts[0]] {
				continue
			}
			seen[parts[0]] = true
			e := &ExternalSession{Kind: KindTmux, Name: parts[0], Path: parts[1], Live: parts[0]}
			if isTerm {
				// A cs project terminal: a tile titled "terminal" until Claude runs in it.
				e.Label = "terminal"
			}
			if parts[2] != "0" {
				e.Clients = 1
			}
			if pid, err := strconv.Atoi(parts[3]); err == nil {
				byPanePid[pid] = e
			}
			sessions = append(sessions, e)
		}
	}

	parents := &parentTable{}
	for _, a := range claudeAgents() {
		if a.Kind == "background" && isClosed(a) {
			continue
		}
		if name, ok := agentPanes[a.Pid]; ok {
			agentStatus[name] = a.Status
			continue
		}
		if e, ok := byPanePid[a.Pid]; ok {
			// A tmux session we already list: just add Claude's details.
			e.Label, e.Status, e.SessionID = a.Name, a.Status, a.SessionID
			continue
		}
		if strings.HasPrefix(filepath.Base(a.Cwd), "claudesquad_") || strings.Contains(a.Cwd, "/.claude-squad/worktrees/") {
			continue // one of cs's own agents
		}
		// Claude started from a shell in one of our tmux panes (typed `claude`
		// in a cs project terminal or a cc_ session): its parent is the pane's
		// shell, so it is that tile's session.
		if ppid := parents.of(a.Pid); ppid > 0 {
			if e, ok := byPanePid[ppid]; ok {
				e.Label, e.Status, e.SessionID = a.Name, a.Status, a.SessionID
				continue
			}
		}
		e := &ExternalSession{Kind: KindTerminal, Name: fmt.Sprintf("pid-%d", a.Pid), Path: a.Cwd,
			Label: a.Name, Status: a.Status, SessionID: a.SessionID, Pid: a.Pid}
		if a.Kind == "background" && a.ID != "" {
			e.Kind, e.Name = KindBackground, a.ID
			if wrappers[AttachPrefix+a.ID] {
				e.Live = AttachPrefix + a.ID
			}
		}
		sessions = append(sessions, e)
	}
	for _, e := range sessions {
		if e.Live != "" {
			e.Prompted = screenWaiting(e.Live)
		}
	}
	return sessions, agentStatus, nil
}

// NeedsYou is true while Claude waits on the user: a question or a
// permission prompt ("waiting" in `claude agents`).
func (e *ExternalSession) NeedsYou() bool {
	return e.Status == "waiting" || e.Prompted
}

// promptMarkers are lines Claude Code shows only while waiting for an answer.
var promptMarkers = []string{
	"Enter to select", "Enter to confirm", "Do you want to proceed?",
	"No, and tell Claude what to do differently",
}

// screenWaiting reports whether a tmux session's screen shows a prompt.
func screenWaiting(name string) bool {
	out, err := exec.Command("tmux", "capture-pane", "-p", "-t", name).Output()
	if err != nil {
		return false
	}
	screen := string(out)
	for _, m := range promptMarkers {
		if strings.Contains(screen, m) {
			return true
		}
	}
	return false
}

// MoveIntoBackground moves a session that runs directly in some terminal
// window into Claude Code's background service, so cs can attach to it: it
// closes the session's process (that window drops back to its shell) and
// resumes the same conversation with `claude --bg --resume`. It returns the
// background id.
func (e *ExternalSession) MoveIntoBackground() (string, error) {
	if e.Kind != KindTerminal || e.SessionID == "" {
		return "", fmt.Errorf("this session can't be moved")
	}
	if transcriptPath(e.SessionID) == "" {
		return "", fmt.Errorf("this session has no conversation yet, so there is nothing to move; type in its own window, or start a chat here with t")
	}
	if e.Pid > 0 {
		if p, err := os.FindProcess(e.Pid); err == nil {
			_ = p.Signal(syscall.SIGTERM)
			for i := 0; i < 40 && p.Signal(syscall.Signal(0)) == nil; i++ {
				time.Sleep(200 * time.Millisecond)
			}
			if p.Signal(syscall.Signal(0)) == nil {
				return "", fmt.Errorf("it didn't close in its window; quit it there (/exit) and press Enter again")
			}
		}
	}
	cmd := exec.Command(RealClaude(), "--bg", "--resume", e.SessionID)
	cmd.Dir = e.Path
	cmd.Env = append(os.Environ(), "CLAUDE_NO_TMUX=1")
	out, err := cmd.CombinedOutput()
	text := strings.TrimSpace(stripSGR(string(out)))
	if err != nil || !strings.Contains(text, "backgrounded") {
		return "", fmt.Errorf("could not continue it here: %s (its conversation is safe; run `claude --resume %s` in that folder)", firstLine(text), e.SessionID)
	}
	invalidateAgents()
	id := e.SessionID[:min(8, len(e.SessionID))]
	// Make sure it really started in the background before reporting success.
	for i := 0; i < 15; i++ {
		time.Sleep(300 * time.Millisecond)
		invalidateAgents()
		for _, a := range claudeAgents() {
			if a.SessionID == e.SessionID && a.Kind == "background" {
				return id, nil
			}
		}
	}
	return "", fmt.Errorf("it closed in its window but didn't start here; continue it with `claude --resume %s` in %s", e.SessionID, e.Path)
}

var sgrRe = regexp.MustCompile("\x1b\\[[0-9;]*m")

func stripSGR(s string) string { return sgrRe.ReplaceAllString(s, "") }

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return line
}

// invalidateAgents makes the next listing ask Claude Code again.
func invalidateAgents() {
	agentsMu.Lock()
	agentsFetched = time.Time{}
	agentsMu.Unlock()
}

// TermPrefix names the per-project shell sessions cs shows when a project
// has nothing running (and on ⌥T). They are kept between cs runs.
const TermPrefix = "csterm_"

// EnsureProjectShell returns a shell session of the project, starting a
// login shell in dir if needed. A terminal someone ran Claude in (busy) is
// skipped: the next one is csterm_<folder>-2, -3, ...
func EnsureProjectShell(dir string, width, height int, busy func(name string) bool) (string, error) {
	base := TermPrefix + sessionNameRe.ReplaceAllString(filepath.Base(dir), "-")
	name := base
	for i := 2; ; i++ {
		if exec.Command("tmux", "has-session", "-t", "="+name).Run() != nil {
			break // free name: start a shell under it
		}
		if !busy(name) {
			return name, nil
		}
		name = base + "-" + strconv.Itoa(i)
	}
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/zsh"
	}
	if out, err := exec.Command("tmux", "new-session", "-d", "-s", name, "-c", dir,
		"-x", strconv.Itoa(width), "-y", strconv.Itoa(height), shell, "-l").CombinedOutput(); err != nil {
		return "", fmt.Errorf("could not open a terminal: %s", strings.TrimSpace(string(out)))
	}
	return name, nil
}

// parentTable answers parent-pid lookups from one `ps` call, made on first use.
type parentTable struct {
	loaded bool
	ppid   map[int]int
}

func (t *parentTable) of(pid int) int {
	if !t.loaded {
		t.loaded = true
		t.ppid = map[int]int{}
		if out, err := exec.Command("ps", "-axo", "pid=,ppid=").Output(); err == nil {
			for _, line := range strings.Split(string(out), "\n") {
				f := strings.Fields(line)
				if len(f) == 2 {
					p, _ := strconv.Atoi(f[0])
					pp, _ := strconv.Atoi(f[1])
					t.ppid[p] = pp
				}
			}
		}
	}
	return t.ppid[pid]
}

// AttachPrefix names the hidden tmux sessions that run `claude attach <id>`
// so a background session can be shown and typed into inside cs. It differs
// from ExternalPrefix so these wrappers are never listed as sessions.
const AttachPrefix = "csbg_"

// EnsureLive makes sure the session has a tmux session to show and type into,
// starting the attach wrapper for a background session if needed, and returns
// its name. View-only sessions return an error.
func (e *ExternalSession) EnsureLive(width, height int) (string, error) {
	switch e.Kind {
	case KindTmux:
		return e.Name, nil
	case KindTerminal:
		return "", fmt.Errorf("this session runs in its own terminal window, so it can only be watched here; type in that window")
	}
	name := AttachPrefix + e.Name
	if exec.Command("tmux", "has-session", "-t", "="+name).Run() != nil {
		closedMu.Lock()
		gone, closed := attached[e.Name], closedBG[e.Name]
		closedMu.Unlock()
		if closed {
			return "", fmt.Errorf("this session was closed")
		}
		if gone {
			// cs's own `claude attach` ended: the user typed /exit in the tile.
			// Re-attaching would resume it, so close it instead.
			MarkClosed(e.Name)
			go exec.Command(RealClaude(), "stop", e.Name).Run()
			return "", fmt.Errorf("this session was closed")
		}
		args := []string{"new-session", "-d", "-s", name, "-c", e.Path,
			"-x", strconv.Itoa(width), "-y", strconv.Itoa(height),
			"-e", "CLAUDE_NO_TMUX=1", RealClaude(), "attach", e.Name}
		if out, err := exec.Command("tmux", args...).CombinedOutput(); err != nil {
			return "", fmt.Errorf("could not open session: %s", strings.TrimSpace(string(out)))
		}
	}
	closedMu.Lock()
	attached[e.Name] = true
	closedMu.Unlock()
	e.Live = name
	return name, nil
}

// FitTo resizes the session's screen to the cs pane, but only when no other
// terminal shows it, so a session the user also has open elsewhere keeps the
// size of their window.
func (e *ExternalSession) FitTo(width, height int) {
	if e.Live == "" || (e.Kind == KindTmux && e.Clients > 0) {
		return
	}
	_ = exec.Command("tmux", "resize-window", "-t", e.Live, "-x", strconv.Itoa(width), "-y", strconv.Itoa(height)).Run()
}

// CloseAttachWrappers ends every hidden attach wrapper. The background
// sessions themselves keep running in Claude Code's service.
func CloseAttachWrappers() {
	// Forget them first: a wrapper cs ends itself is not the user's /exit.
	closedMu.Lock()
	attached = map[string]bool{}
	closedMu.Unlock()
	out, err := exec.Command("tmux", "list-sessions", "-F", "#{session_name}").Output()
	if err != nil {
		return
	}
	for _, name := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if strings.HasPrefix(name, AttachPrefix) {
			_ = exec.Command("tmux", "kill-session", "-t", "="+name).Run()
		}
	}
}

// StartSession starts program (claude) in a new tmux session in dir, named
// like the launcher's sessions so it is listed with them. It works in the
// folder itself: no worktree, no new branch.
func StartSession(dir, program string) (string, error) {
	base := ExternalPrefix + sessionNameRe.ReplaceAllString(filepath.Base(dir), "-")
	name := base
	for n := 2; exec.Command("tmux", "has-session", "-t", "="+name).Run() == nil; n++ {
		name = fmt.Sprintf("%s_%d", base, n)
	}
	if out, err := exec.Command("tmux", "new-session", "-d", "-s", name, "-c", dir, "-x", "200", "-y", "50", program).CombinedOutput(); err != nil {
		return "", fmt.Errorf("could not start session: %s", strings.TrimSpace(string(out)))
	}
	_ = exec.Command("tmux", "set-option", "-t", name, "window-size", "latest").Run()
	return name, nil
}

var sessionNameRe = regexp.MustCompile(`[^A-Za-z0-9_-]`)

// Title is the name shown in the list: Claude's session name when known,
// otherwise the tmux name without its prefix.
func (e *ExternalSession) Title() string {
	if e.Label != "" {
		return e.Label
	}
	return strings.TrimPrefix(e.Name, ExternalPrefix)
}

// Describe is the second line in the list: where it runs and what it's doing.
func (e *ExternalSession) Describe() string {
	var where string
	switch e.Kind {
	case KindTmux:
		where = "live here"
		if e.Clients > 0 {
			where = "live here · also in a terminal"
		}
	case KindBackground:
		where = "background"
	case KindTerminal:
		where = "own terminal · view only"
	}
	if e.NeedsYou() {
		return "needs you · " + where
	}
	if e.Status != "" {
		return where + " · " + e.Status
	}
	return where
}

// Preview returns the live screen of the session's tmux session.
func (e *ExternalSession) Preview() (string, error) {
	return tmux.NewExternalTmuxSession(e.Live).CapturePaneContent()
}

// Conversation returns the session's recent conversation as markdown, read
// from its transcript, plus a version string that changes whenever the
// transcript does (so callers can cache the rendered result).
func (e *ExternalSession) Conversation() (markdown, version string) {
	path := ""
	if e.SessionID != "" {
		path = transcriptPath(e.SessionID)
	}
	st, err := os.Stat(path)
	if path == "" || err != nil {
		return "_No conversation saved for this session yet._", ""
	}
	version = fmt.Sprintf("%s:%d:%d", path, st.Size(), st.ModTime().UnixNano())
	transcriptMu.Lock()
	cached, ok := transcriptCache[path]
	transcriptMu.Unlock()
	if ok && cached[0] == version {
		return cached[1], version // unchanged since last read: don't re-parse
	}
	md, err := transcriptMarkdown(path, 512<<10, 300)
	if err != nil {
		return "_Could not read this session's conversation._", version
	}
	transcriptMu.Lock()
	transcriptCache[path] = [2]string{version, md}
	transcriptMu.Unlock()
	return md, version
}

// SentBatch is one SendUserFile call: the files Claude handed over together
// and its caption.
type SentBatch struct {
	Files   []string
	Caption string
}

// LastSentFiles returns the files of the session's latest SendUserFile
// call (the screenshots Claude last handed over) that still exist.
func (e *ExternalSession) LastSentFiles() ([]string, error) {
	batches, err := e.SentBatches()
	if err != nil {
		return nil, err
	}
	if len(batches) == 0 {
		return nil, fmt.Errorf("this session hasn't sent any files that still exist")
	}
	return batches[len(batches)-1].Files, nil
}

// SentFiles returns every file the session sent that still exists, oldest
// first.
func (e *ExternalSession) SentFiles() ([]string, error) {
	batches, err := e.SentBatches()
	var out []string
	for _, b := range batches {
		out = append(out, b.Files...)
	}
	return out, err
}

// SentBatches returns the session's SendUserFile calls, oldest first, keeping
// only absolute paths to existing regular files (they go to `open` and into
// a web page) and dropping calls left with none.
func (e *ExternalSession) SentBatches() ([]SentBatch, error) {
	path := ""
	if e.SessionID != "" {
		path = transcriptPath(e.SessionID)
	}
	if path == "" {
		return nil, fmt.Errorf("no Claude conversation in this tile")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var batches []SentBatch
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 32<<20)
	for sc.Scan() {
		if !bytes.Contains(sc.Bytes(), []byte(`"SendUserFile"`)) {
			continue
		}
		var entry struct {
			Message struct {
				Content []struct {
					Type  string `json:"type"`
					Name  string `json:"name"`
					Input struct {
						Files   []string `json:"files"`
						Caption string   `json:"caption"`
					} `json:"input"`
				} `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(sc.Bytes(), &entry) != nil {
			continue
		}
		for _, c := range entry.Message.Content {
			if c.Type != "tool_use" || c.Name != "SendUserFile" {
				continue
			}
			var files []string
			for _, p := range c.Input.Files {
				if st, err := os.Stat(p); err == nil && filepath.IsAbs(p) && st.Mode().IsRegular() {
					files = append(files, p)
				}
			}
			if len(files) > 0 {
				batches = append(batches, SentBatch{Files: files, Caption: c.Input.Caption})
			}
		}
	}
	return batches, nil
}

// FullText returns everything the session has shown, for copying: a Claude
// session's whole conversation (its full-screen view keeps no scroll-back),
// otherwise the tmux pane with all its scroll-back.
func (e *ExternalSession) FullText() (string, error) {
	if e.SessionID != "" {
		if path := transcriptPath(e.SessionID); path != "" {
			return transcriptMarkdown(path, 0, 0)
		}
	}
	target := e.Live
	if target == "" {
		target = e.Name
	}
	out, err := exec.Command("tmux", "capture-pane", "-p", "-J", "-S", "-", "-t", target).Output()
	if err != nil {
		return "", fmt.Errorf("could not read %s: %w", e.Name, err)
	}
	return strings.TrimRight(string(out), "\n "), nil
}

// Hint is the one-line note shown above a conversation preview.
func (e *ExternalSession) Hint() string {
	if e.Kind == KindBackground {
		return "Background session · Enter opens it here, Ctrl+Z comes back"
	}
	return "Runs in its own terminal window · Enter moves it into cs so you can type here"
}

// Attach connects the terminal to a tmux session; ctrl-q detaches. The
// returned channel closes on detach, after which claude-squad's client is
// released. Other kinds are attached with AttachCommand instead.
func (e *ExternalSession) Attach() (chan struct{}, error) {
	if e.Kind != KindTmux {
		return nil, fmt.Errorf("not a tmux session")
	}
	t := tmux.NewExternalTmuxSession(e.Name)
	if err := t.Restore(); err != nil {
		return nil, err
	}
	ch, err := t.Attach()
	if err != nil {
		t.ReleaseClient()
		return nil, err
	}
	done := make(chan struct{})
	go func() {
		<-ch
		t.ReleaseClient()
		close(done)
	}()
	return done, nil
}

// AttachCommand returns the command that opens a background session in this
// terminal (`claude attach <id>`; Ctrl+Z returns).
func (e *ExternalSession) AttachCommand() *exec.Cmd {
	cmd := exec.Command(RealClaude(), "attach", e.Name)
	cmd.Env = append(os.Environ(), "CLAUDE_NO_TMUX=1")
	return cmd
}

var (
	transcriptMu    sync.Mutex
	transcriptPaths = map[string]string{}
	// transcriptCache holds each transcript's markdown with the version
	// (size+mtime) it was built from.
	transcriptMissAt = map[string]time.Time{} // when a missing transcript was last looked for
	transcriptCache  = map[string][2]string{}
)

// transcriptPath finds ~/.claude/projects/*/<sessionID>.jsonl.
func transcriptPath(sessionID string) string {
	transcriptMu.Lock()
	defer transcriptMu.Unlock()
	if p, ok := transcriptPaths[sessionID]; ok {
		return p
	}
	if time.Since(transcriptMissAt[sessionID]) < 5*time.Second {
		return "" // just looked: no file yet
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	matches, _ := filepath.Glob(filepath.Join(home, ".claude", "projects", "*", sessionID+".jsonl"))
	p := ""
	if len(matches) > 0 {
		p = matches[0]
	}
	// A new session's transcript appears only after its first message:
	// remember a miss for a few seconds, not for good (a new tile kept
	// the wrong colour because its /color was never read).
	if p != "" {
		transcriptPaths[sessionID] = p
	} else if time.Since(transcriptMissAt[sessionID]) > 5*time.Second {
		transcriptMissAt[sessionID] = time.Now()
	}
	return p
}

// transcriptMarkdown turns the last messages of a transcript (its last
// tailBytes, at most keep lines; 0 = all) into markdown. The transcript
// format is undocumented, so anything unexpected is skipped rather than
// treated as an error.
func transcriptMarkdown(path string, tailBytes int64, keep int) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()

	if st, err := f.Stat(); err == nil && tailBytes > 0 && st.Size() > tailBytes {
		_, _ = f.Seek(st.Size()-tailBytes, io.SeekStart)
	}

	var lines []string
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 1<<20), 8<<20)
	for sc.Scan() {
		var entry struct {
			Type    string `json:"type"`
			IsMeta  bool   `json:"isMeta"`
			Message struct {
				Role    string          `json:"role"`
				Content json.RawMessage `json:"content"`
			} `json:"message"`
		}
		if json.Unmarshal(sc.Bytes(), &entry) != nil || entry.IsMeta {
			continue
		}
		if entry.Type != "user" && entry.Type != "assistant" {
			continue
		}
		for _, l := range renderMessage(entry.Message.Role, entry.Message.Content) {
			if !strings.HasPrefix(l, toolMark) {
				lines = append(lines, l)
				continue
			}
			// Consecutive tool calls share one line; a tool call after text
			// starts its own paragraph.
			last := len(lines) - 1
			switch {
			case last >= 0 && strings.HasPrefix(lines[last], toolMark):
				lines[last] += " " + l
			case last >= 0 && lines[last] != "":
				lines = append(lines, "", l)
			default:
				lines = append(lines, l)
			}
		}
	}
	if keep > 0 && len(lines) > keep {
		lines = lines[len(lines)-keep:]
	}
	return strings.Join(lines, "\n"), nil
}

func renderMessage(role string, content json.RawMessage) []string {
	var text string
	if json.Unmarshal(content, &text) == nil {
		return formatTurn(role, text)
	}
	var blocks []struct {
		Type string `json:"type"`
		Text string `json:"text"`
		Name string `json:"name"`
	}
	if json.Unmarshal(content, &blocks) != nil {
		return nil
	}
	var out []string
	for _, b := range blocks {
		switch b.Type {
		case "text":
			out = append(out, formatTurn(role, b.Text)...)
		case "tool_use":
			out = append(out, toolMark+b.Name+"`")
		}
	}
	return out
}

// toolMark starts the markdown for a tool call, e.g. "`⎿ Bash`".
const toolMark = "`⎿ "

// formatTurn renders one message as markdown: the user's words as a quote,
// Claude's reply as-is (it is already markdown).
func formatTurn(role, text string) []string {
	text = strings.TrimSpace(text)
	if text == "" || strings.HasPrefix(text, "<") {
		return nil // empty, or harness-injected markup rather than a real message
	}
	lines := strings.Split(text, "\n")
	if role != "user" {
		return append([]string{""}, lines...)
	}
	out := []string{"", "> **You:** " + lines[0]}
	for _, l := range lines[1:] {
		out = append(out, "> "+l)
	}
	return out
}

// ResumeSession reopens a closed Claude conversation in a new tmux session in
// dir: `<program> --resume <sessionID>`. An issue session gets its issue name
// back (cc_<folder>_i<N>) when it is free, so it counts as that issue again.
func ResumeSession(dir, program, sessionID string, issue int) (string, error) {
	name := ""
	if issue > 0 {
		if n := IssueSessionName(dir, issue); exec.Command("tmux", "has-session", "-t", "="+n).Run() != nil {
			name = n
		}
	}
	if name == "" {
		base := ExternalPrefix + sessionNameRe.ReplaceAllString(filepath.Base(dir), "-")
		name = base
		for n := 2; exec.Command("tmux", "has-session", "-t", "="+name).Run() == nil; n++ {
			name = fmt.Sprintf("%s_%d", base, n)
		}
	}
	if out, err := exec.Command("tmux", "new-session", "-d", "-s", name, "-c", dir, "-x", "200", "-y", "50",
		program+" --resume "+sessionID).CombinedOutput(); err != nil {
		return "", fmt.Errorf("could not reopen it: %s", strings.TrimSpace(string(out)))
	}
	_ = exec.Command("tmux", "set-option", "-t", name, "window-size", "latest").Run()
	return name, nil
}

// KillSession ends a tmux session and makes sure the program in it stops
// too. Claude ignores the hangup tmux sends when its session is killed and
// kept running on its own, so a closed tile came back as a "view only" one
// (#2239). Its pane's process gets SIGTERM after a second, then SIGKILL; in
// the background, so closing never waits.
func KillSession(name string) error {
	pid := 0
	if out, err := exec.Command("tmux", "display-message", "-p", "-t", "="+name+":", "#{pane_pid}").Output(); err == nil {
		pid, _ = strconv.Atoi(strings.TrimSpace(string(out)))
	}
	err := exec.Command("tmux", "kill-session", "-t", "="+name).Run()
	if pid > 1 {
		go func() {
			alive := func() bool { return syscall.Kill(pid, 0) == nil }
			time.Sleep(time.Second)
			if alive() {
				_ = syscall.Kill(pid, syscall.SIGTERM)
				time.Sleep(3 * time.Second)
				if alive() {
					_ = syscall.Kill(pid, syscall.SIGKILL)
				}
			}
		}()
	}
	return err
}

// EndEditorWait ends Claude's wait for an external editor in a tmux session:
// Ctrl+G opens the prompt in $EDITOR (VS Code: `code -w`) and Claude reads
// nothing until that editor closes. It stops the waiting editor command
// under the session's pane; Claude then takes back the text as saved. It
// returns whether one was waiting.
func EndEditorWait(name string) bool {
	out, err := exec.Command("tmux", "display-message", "-p", "-t", "="+name+":", "#{pane_pid}").Output()
	if err != nil {
		return false
	}
	pane, _ := strconv.Atoi(strings.TrimSpace(string(out)))
	if pane <= 1 {
		return false
	}
	ps, err := exec.Command("ps", "-axo", "pid=,ppid=,args=").Output()
	if err != nil {
		return false
	}
	ended := false
	for _, l := range strings.Split(string(ps), "\n") {
		f := strings.Fields(l)
		if len(f) < 3 {
			continue
		}
		pid, _ := strconv.Atoi(f[0])
		ppid, _ := strconv.Atoi(f[1])
		args := strings.Join(f[2:], " ")
		if ppid == pane && strings.Contains(args, "claude-prompt-") {
			_ = syscall.Kill(pid, syscall.SIGTERM)
			ended = true
		}
	}
	return ended
}
