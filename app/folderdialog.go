package app

import (
	"claude-squad/session/git"
	"fmt"
	"os/exec"
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
	root, err := git.RepoRoot(folder)
	if err != nil {
		return "", fmt.Errorf("%s is not a git repository", folder)
	}
	return root, nil
}
