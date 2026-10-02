package app

import (
	"claude-squad/config"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Progress strip: one line above the tiles for the current project, so the
// work shows: the open PR filling up to autoMaxPerPR issues, open issues left
// (and how many fewer than this morning), sessions running out of
// autoMaxSessions, sessions finished today and the day streak, plus totals
// across all projects. "Finished" = a Claude session closed with ⌃Space W
// (closeSession); counts are kept in progress.json so restarts don't lose them.

type progressData struct {
	Done        map[string]map[string]int `json:"done"`          // day → project → sessions finished
	OpenAtStart map[string]map[string]int `json:"open_at_start"` // day → project → open issues first seen
}

var (
	progBarOn   = lipgloss.NewStyle().Foreground(lipgloss.Color("#3fb950"))
	progBarOff  = lipgloss.NewStyle().Foreground(lipgloss.Color("#3a3a3a"))
	progLabel   = lipgloss.NewStyle().Foreground(lipgloss.Color("#9d9d9d"))
	progValue   = lipgloss.NewStyle().Foreground(lipgloss.Color("#e6e6e6")).Bold(true)
	progGood    = lipgloss.NewStyle().Foreground(lipgloss.Color("#3fb950")).Bold(true)
	progFire    = lipgloss.NewStyle().Foreground(lipgloss.Color("#f0883e")).Bold(true)
	progDimNote = lipgloss.NewStyle().Foreground(lipgloss.Color("#6e6e6e"))
)

func progressFile() string {
	dir, err := config.GetConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "progress.json")
}

func today() string { return time.Now().Format("2006-01-02") }

// loadProgress reads progress.json (empty when missing).
func loadProgress() *progressData {
	p := &progressData{}
	if data, err := os.ReadFile(progressFile()); err == nil {
		_ = json.Unmarshal(data, p)
	}
	if p.Done == nil {
		p.Done = map[string]map[string]int{}
	}
	if p.OpenAtStart == nil {
		p.OpenAtStart = map[string]map[string]int{}
	}
	return p
}

func (p *progressData) save() {
	if inTest {
		return
	}
	if f := progressFile(); f != "" {
		if data, err := json.Marshal(p); err == nil {
			_ = os.WriteFile(f, data, 0o600)
		}
	}
}

// addDone counts a finished session of project today.
func (p *progressData) addDone(project string) {
	d := today()
	if p.Done[d] == nil {
		p.Done[d] = map[string]int{}
	}
	p.Done[d][project]++
	p.save()
}

// noteOpen records the first open-issue count seen today, the baseline for
// "(−4 today)".
func (p *progressData) noteOpen(project string, open int) {
	d := today()
	if p.OpenAtStart[d] == nil {
		p.OpenAtStart[d] = map[string]int{}
	}
	if _, ok := p.OpenAtStart[d][project]; !ok {
		p.OpenAtStart[d][project] = open
		p.save()
	}
}

// streak is how many days in a row (up to today, or yesterday if nothing is
// done yet today) had a finished session in any project.
func (p *progressData) streak() int {
	day := time.Now()
	if p.doneOn(day.Format("2006-01-02")) == 0 {
		day = day.AddDate(0, 0, -1)
	}
	n := 0
	for p.doneOn(day.Format("2006-01-02")) > 0 {
		n++
		day = day.AddDate(0, 0, -1)
	}
	return n
}

func (p *progressData) doneOn(day string) int {
	n := 0
	for _, v := range p.Done[day] {
		n += v
	}
	return n
}

// projectStats are one project's numbers for the strip.
type projectStats struct {
	pr, inPR, open, running int
	known                   bool // the issue list has been loaded
}

func (m *home) projectStats(project string) projectStats {
	s := projectStats{running: m.autoRunning(project)}
	issues, ok := m.issueCache[project]
	if !ok {
		return s
	}
	s.known = true
	room, pr := prRoom(issues)
	s.pr, s.inPR = pr, autoMaxPerPR-room
	for _, is := range issues {
		if is.PR == 0 {
			s.open++
		}
	}
	m.progress.noteOpen(project, s.open+s.inPR)
	return s
}

// bar draws n of max as a filled bar of width cells.
func bar(n, max, width int) string {
	fill := 0
	if max > 0 {
		fill = min(width, n*width/max)
	}
	return progBarOn.Render(strings.Repeat("█", fill)) + progBarOff.Render(strings.Repeat("░", width-fill))
}

// renderProgress draws the strip, cut to width.
func (m *home) renderProgress(width int) string {
	project := m.projectTabs.Active()
	if project == "" || m.progress == nil {
		return ""
	}
	s := m.projectStats(project)
	sep := progLabel.Render(" · ")
	var parts []string
	if s.known {
		if s.pr > 0 {
			parts = append(parts, progLabel.Render(fmt.Sprintf("PR #%d ", s.pr))+bar(s.inPR, autoMaxPerPR, 15)+" "+
				progValue.Render(fmt.Sprintf("%d/%d", s.inPR, autoMaxPerPR))+progLabel.Render(" issues"))
		} else {
			parts = append(parts, progLabel.Render("no open PR yet"))
		}
		open := progValue.Render(fmt.Sprint(s.open)) + progLabel.Render(" open")
		if start, ok := m.progress.OpenAtStart[today()][project]; ok {
			if d := s.open + s.inPR - start; d < 0 {
				open += progGood.Render(fmt.Sprintf(" (−%d today)", -d))
			} else if d > 0 {
				open += progDimNote.Render(fmt.Sprintf(" (+%d today)", d))
			}
		}
		parts = append(parts, open)
	}
	parts = append(parts, progLabel.Render("▶ ")+progValue.Render(fmt.Sprintf("%d/%d", s.running, autoMaxSessions))+progLabel.Render(" running"))
	done := m.progress.Done[today()][project]
	parts = append(parts, progGood.Render(fmt.Sprintf("✓ %d", done))+progLabel.Render(" done today"))
	if st := m.progress.streak(); st > 0 {
		parts = append(parts, progFire.Render(fmt.Sprintf("🔥 %d-day streak", st)))
	}
	if !m.autoOn(project) {
		parts = append(parts, progDimNote.Render("auto off"))
	}
	line := " " + strings.Join(parts, sep)

	// All projects: finished today and how full their PRs are.
	if len(m.projectTabs.Projects()) > 1 {
		inPR, prs := 0, 0
		for _, p := range m.projectTabs.Projects() {
			if issues, ok := m.issueCache[p]; ok {
				if room, pr := prRoom(issues); pr > 0 {
					inPR += autoMaxPerPR - room
					prs++
				}
			}
		}
		all := progLabel.Render("   Σ ") + progGood.Render(fmt.Sprint(m.progress.doneOn(today()))) + progLabel.Render(" done today")
		if prs > 0 {
			all += progLabel.Render(fmt.Sprintf(" · %d/%d in %d PRs", inPR, prs*autoMaxPerPR, prs))
		}
		if ansi.StringWidth(line)+ansi.StringWidth(all) <= width {
			line += strings.Repeat(" ", width-ansi.StringWidth(line)-ansi.StringWidth(all)) + all
		}
	}
	return ansi.Truncate(line, width, "…")
}
