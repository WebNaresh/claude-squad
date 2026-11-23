package app

import (
	"claude-squad/session/git"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ChooseProjectFolder opens the macOS folder chooser and returns the git
// repository root of the folder picked. It returns "" if the user cancels.
func ChooseProjectFolder() (string, error) {
	out, err := exec.Command("osascript", "-e",
		`POSIX path of (choose folder with prompt "Add a project to claude-squad")`).Output()
	if err != nil {
		// osascript exits with an error when the user presses Cancel.
		if ee, ok := err.(*exec.ExitError); ok && strings.Contains(string(ee.Stderr), "-128") {
			return "", nil
		}
		return "", fmt.Errorf("folder chooser failed: %w", err)
	}
	folder := strings.TrimRight(strings.TrimSpace(string(out)), "/")
	if root, err := git.RepoRoot(folder); err == nil {
		return root, nil
	}
	return initProjectFolder(folder)
}

// initProjectFolder makes a picked folder that isn't a git repository yet
// into one, so a new project can be added straight away: a fresh empty
// folder was refused with "is not a git repository" (global_career_guidance,
// 2026-11-15). Refused instead for the home folder and its Desktop,
// Documents, Downloads…, and for a folder that holds other repositories:
// those are folders of things, not a project.
func initProjectFolder(folder string) (string, error) {
	if home, err := os.UserHomeDir(); err == nil {
		switch rel, _ := filepath.Rel(home, folder); rel {
		case ".":
			return "", fmt.Errorf("%s is your home folder; pick a project folder inside it", folder)
		case "Desktop", "Documents", "Downloads", "Library", "Movies", "Music", "Pictures", "Public":
			return "", fmt.Errorf("%s isn't a project folder; pick or make one inside it", folder)
		}
	}
	entries, err := os.ReadDir(folder)
	if err != nil {
		return "", fmt.Errorf("can't read %s: %w", folder, err)
	}
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		if _, err := os.Stat(filepath.Join(folder, e.Name(), ".git")); err == nil {
			return "", fmt.Errorf("%s holds other projects (%s); pick one of them", filepath.Base(folder), e.Name())
		}
	}
	if out, err := exec.Command("git", "-C", folder, "init", "-b", "main").CombinedOutput(); err != nil {
		return "", fmt.Errorf("git init in %s failed: %s", folder, strings.TrimSpace(string(out)))
	}
	logEvent("add project: %s wasn't a git repository; ran git init", folder)
	return folder, nil
}
