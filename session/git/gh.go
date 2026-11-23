package git

import (
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
)

var originRepoRe = regexp.MustCompile(`github\.com[:/]([^/\s]+)/([^/\s]+?)(?:\.git)?/?\s*$`)

// originRepos caches dir → "owner/repo" of its origin remote.
var originRepos sync.Map

// originRepo returns "owner/repo" of dir's origin remote on GitHub, or "".
func originRepo(dir string) string {
	if v, ok := originRepos.Load(dir); ok {
		return v.(string)
	}
	repo := ""
	if out, err := exec.Command("git", "-C", dir, "remote", "get-url", "origin").Output(); err == nil {
		if m := originRepoRe.FindStringSubmatch(strings.TrimSpace(string(out))); m != nil {
			repo = m[1] + "/" + m[2]
		}
	}
	originRepos.Store(dir, repo)
	return repo
}

// GhCmd is gh run in dir against the origin repo. Without it gh picks the
// "upstream" remote first, so in a fork cs listed and started the upstream
// project's issues (claude-squad, 2026-11-21: smtg-ai #56 and #156).
func GhCmd(dir string, args ...string) *exec.Cmd {
	cmd := exec.Command("gh", args...)
	cmd.Dir = dir
	if repo := originRepo(dir); repo != "" {
		cmd.Env = append(os.Environ(), "GH_REPO="+repo)
	}
	return cmd
}
