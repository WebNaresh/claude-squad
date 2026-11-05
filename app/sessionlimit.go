package app

import (
	"claude-squad/config"
	"claude-squad/ui"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// How many Claude sessions a project may run. A project's own number wins,
// then the number for all projects, then defaultMaxSessions. The issue picker
// sets them (+/- and g). They live in their own file, not config.json: the
// background runner is another process, and a running cs keeps config.json in
// memory and rewrites it. Lowering a limit closes nothing; it only stops new
// sessions until the count drops below it.

const (
	defaultMaxSessions = 6
	minSessionLimit    = 1
	maxSessionLimit    = 12
)

type sessionLimits struct {
	All      int            `json:"all,omitempty"`
	Projects map[string]int `json:"projects,omitempty"`
}

func sessionLimitsFile() string {
	dir, err := config.GetConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, "session-limits.json")
}

// readSessionLimits never fails: a missing or broken file means the defaults.
func readSessionLimits() sessionLimits {
	var l sessionLimits
	if f := sessionLimitsFile(); f != "" {
		if data, err := os.ReadFile(f); err == nil {
			_ = json.Unmarshal(data, &l)
		}
	}
	return l
}

func clampLimit(n int) int {
	return min(max(n, minSessionLimit), maxSessionLimit)
}

func (l sessionLimits) all() int {
	if l.All > 0 {
		return clampLimit(l.All)
	}
	return defaultMaxSessions
}

func (l sessionLimits) of(project string) int {
	if n := l.Projects[project]; n > 0 {
		return clampLimit(n)
	}
	return l.all()
}

// sessionLimit is the number project runs under now.
func sessionLimit(project string) int {
	return readSessionLimits().of(project)
}

// stepped returns the limits with one number moved by delta. A project that
// reaches the all-projects number drops its own, so it follows that number
// again: there is no separate "use the global number" step.
func (l sessionLimits) stepped(project string, all bool, delta int) sessionLimits {
	projects := make(map[string]int, len(l.Projects))
	for p, n := range l.Projects {
		projects[p] = n
	}
	out := sessionLimits{All: l.All, Projects: projects}
	if all {
		out.All = clampLimit(l.all() + delta)
		return out
	}
	n := clampLimit(l.of(project) + delta)
	if n == out.all() {
		delete(out.Projects, project)
	} else {
		out.Projects[project] = n
	}
	return out
}

// write saves atomically: the background runner reads this file every scan.
func (l sessionLimits) write() error {
	f := sessionLimitsFile()
	if f == "" {
		return fmt.Errorf("no config folder")
	}
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	tmp := f + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, f)
}

// showLimits puts the saved limits on the picker.
func showLimits(p *ui.IssuePicker) {
	l := readSessionLimits()
	_, own := l.Projects[p.Project]
	p.Limit, p.LimitAll, p.LimitOwn = l.of(p.Project), l.all(), own
}

// stepLimit handles +/- in the picker. A failed save changes nothing on
// screen, and says why.
func (m *home) stepLimit(p *ui.IssuePicker, delta int) error {
	l := readSessionLimits().stepped(p.Project, p.EditAll, delta)
	if err := l.write(); err != nil {
		return fmt.Errorf("could not save the session limit: %w", err)
	}
	showLimits(p)
	logEvent("session limit: %s now %d (all projects %d)", filepath.Base(p.Project), p.Limit, p.LimitAll)
	return nil
}
