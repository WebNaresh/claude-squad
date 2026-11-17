package app

import (
	"claude-squad/config"
	"claude-squad/session"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Tiles lost to a reboot: every tile lives in the tmux server, so a reboot
// (or a dead tmux server) ends them all, and cs started on an empty screen
// with no way back (2026-11-17: 11 tiles across 4 projects). cs keeps a list
// of the Claude tiles open right now, with the tmux server they ran in, in
// live-tiles.json. When the server is a different one than the list says,
// every tile on the list that isn't open any more died with the old server,
// not by the user's hand, and is resumed (`claude --resume`) in its project.

type liveTile struct {
	Project   string `json:"project"`
	Name      string `json:"name"`
	Title     string `json:"title"`
	SessionID string `json:"session_id"`
	Issue     int    `json:"issue"`
}

type liveTiles struct {
	Server string     `json:"server"` // tmux server "pid start_time"
	At     time.Time  `json:"at"`
	Tiles  []liveTile `json:"tiles"`
}

var issueNameRe = regexp.MustCompile(`_i(\d+)$`)

func liveTilesFile() string {
	dir, err := config.GetConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "live-tiles.json")
}

func loadLiveTiles() (liveTiles, bool) {
	var l liveTiles
	data, err := os.ReadFile(liveTilesFile())
	if err != nil || json.Unmarshal(data, &l) != nil {
		return l, false
	}
	return l, true
}

func saveLiveTiles(l liveTiles) {
	if inTest {
		return
	}
	if data, err := json.Marshal(l); err == nil {
		_ = os.WriteFile(liveTilesFile(), data, 0o600)
	}
}

// tmuxServerID names the running tmux server; "" when none answers.
func tmuxServerID() string {
	out, err := exec.Command("tmux", "display-message", "-p", "#{pid} #{start_time}").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// currentTiles lists the Claude tiles in sessions (Path already moved to its
// repo root by setExternalSessions). A tile whose conversation id isn't known
// this round (`claude agents` slow or failing) keeps the one saved before, so
// one bad listing doesn't drop it.
func currentTiles(sessions []*session.ExternalSession, before []liveTile) []liveTile {
	prev := map[string]liveTile{}
	for _, t := range before {
		prev[t.Name] = t
	}
	var out []liveTile
	for _, e := range sessions {
		// Only tiles in this tmux server: those are what a dead server takes
		// with it. Claude sessions listed from elsewhere (`claude agents` isn't
		// per-server; a test cs sees the user's real ones) are not ours.
		if e.Kind != session.KindTmux || strings.HasPrefix(e.Name, session.TermPrefix) {
			continue
		}
		t := liveTile{Project: e.Path, Name: e.Name, Title: e.Label, SessionID: e.SessionID}
		if m := issueNameRe.FindStringSubmatch(e.Name); m != nil {
			t.Issue, _ = strconv.Atoi(m[1])
		}
		if t.SessionID == "" {
			p, ok := prev[e.Name]
			if !ok || p.SessionID == "" {
				continue
			}
			t.SessionID = p.SessionID
			if t.Title == "" {
				t.Title = p.Title
			}
		}
		out = append(out, t)
	}
	return out
}

// lostTiles is the saved tiles not open now and not closed by the user after
// the list was saved.
func lostTiles(saved liveTiles, open []*session.ExternalSession, closed []closedEntry) []liveTile {
	isOpen := map[string]bool{}
	for _, e := range open {
		if e.SessionID != "" {
			isOpen[e.SessionID] = true
		}
	}
	userClosed := map[string]bool{}
	for _, c := range closed {
		if !c.At.Before(saved.At) {
			userClosed[c.SessionID] = true
		}
	}
	var out []liveTile
	seen := map[string]bool{}
	for _, t := range saved.Tiles {
		if t.SessionID == "" || isOpen[t.SessionID] || userClosed[t.SessionID] || seen[t.SessionID] {
			continue
		}
		seen[t.SessionID] = true
		out = append(out, t)
	}
	return out
}

// noteLiveTiles saves the open Claude tiles and, when the tmux server is a new
// one, resumes the tiles that died with the old one.
func (m *home) noteLiveTiles(sessions []*session.ExternalSession) tea.Cmd {
	if inTest {
		return nil
	}
	saved, ok := loadLiveTiles()
	now := currentTiles(sessions, saved.Tiles)
	if ok && saved.Server == m.tmuxServer && sameTiles(now, saved.Tiles) {
		return nil
	}
	server := tmuxServerID()
	if server == "" {
		return nil
	}
	m.tmuxServer = server
	var lost []liveTile
	if ok && saved.Server != "" && saved.Server != server {
		lost = lostTiles(saved, sessions, m.recent)
	}
	saveLiveTiles(liveTiles{Server: server, At: time.Now(), Tiles: now})
	if len(lost) == 0 {
		return nil
	}
	program := m.program
	return func() tea.Msg {
		reopened := 0
		var failed []string
		for _, t := range lost {
			if _, err := os.Stat(t.Project); err != nil {
				failed = append(failed, t.Name)
				logEvent("lost tiles: %s not reopened, %s is gone", t.Name, t.Project)
				continue
			}
			name, err := session.ResumeSession(t.Project, program, t.SessionID, t.Issue)
			if err != nil {
				failed = append(failed, t.Name)
				logEvent("lost tiles: %s not reopened: %v", t.Name, err)
				continue
			}
			reopened++
			logEvent("lost tiles: reopened %s as %s (%s)", t.Name, name, t.SessionID)
		}
		msg := fmt.Sprintf("Reopened %d tile(s) closed by a restart", reopened)
		if len(failed) > 0 {
			msg += fmt.Sprintf("; could not reopen %s", strings.Join(failed, ", "))
		}
		return fmt.Errorf("%s", msg)
	}
}

func sameTiles(a, b []liveTile) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
