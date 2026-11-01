package config

import (
	"os"
	"path/filepath"
	"time"
)

func isGitDir(path string) bool {
	_, err := os.Stat(filepath.Join(path, ".git"))
	return err == nil
}

// OpenProject adds path as a tab (if it isn't one already), selects it and
// saves the config.
func (c *Config) OpenProject(path string) error {
	open := false
	for _, p := range c.OpenProjects {
		if p == path {
			open = true
			break
		}
	}
	if !open {
		c.OpenProjects = append(c.OpenProjects, path)
	}
	c.ActiveProject = path
	if c.ProjectAdded == nil {
		c.ProjectAdded = map[string]time.Time{}
	}
	c.ProjectAdded[path] = time.Now()
	return SaveConfig(c)
}

// CloseProject removes path from the tabs and saves the config. If it was the
// active tab, the first remaining tab becomes active.
func (c *Config) CloseProject(path string) error {
	var open []string
	for _, p := range c.OpenProjects {
		if p != path {
			open = append(open, p)
		}
	}
	c.OpenProjects = open
	delete(c.ProjectAdded, path)
	if c.ActiveProject == path {
		c.ActiveProject = ""
		if len(open) > 0 {
			c.ActiveProject = open[0]
		}
	}
	return SaveConfig(c)
}

// StartProject returns the project to open when cs starts outside a git
// repository: the last active tab, else the first open tab that still exists.
// It returns "" when there is none, so the caller should show the picker.
func (c *Config) StartProject() string {
	if c.ActiveProject != "" && isGitDir(c.ActiveProject) {
		return c.ActiveProject
	}
	for _, p := range c.OpenProjects {
		if isGitDir(p) {
			return p
		}
	}
	return ""
}
