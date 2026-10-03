package app

import (
	"bufio"
	"claude-squad/session"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

var issueRefRe = regexp.MustCompile(`#\d+`)

// issueAt returns the issue number of the "#1234" under screen column col
// of a tile's text line, or 0. Columns, not characters: an emoji before it
// (Claude's 🟢) takes two.
func issueAt(line string, col int) int {
	if col < 0 {
		return 0
	}
	for _, loc := range issueRefRe.FindAllStringIndex(line, -1) {
		from := ansi.StringWidth(line[:loc[0]])
		to := from + (loc[1] - loc[0])
		if col >= from && col < to {
			n, _ := strconv.Atoi(line[loc[0]+1 : loc[1]])
			return n
		}
	}
	return 0
}

var githubRemoteRe = regexp.MustCompile(`github\.com[:/]([^/\s]+)/([^/\s]+?)(?:\.git)?$`)

// issueURL is issue n's page: the link the session-naming hook recorded for
// this session (it may be another repo's issue), else the issue in the
// session folder's GitHub repo.
func issueURL(e *session.ExternalSession, n int) string {
	if home, err := os.UserHomeDir(); err == nil && e.SessionID != "" {
		if f, err := os.Open(filepath.Join(home, ".claude", ".session-named", e.SessionID+".issueurls")); err == nil {
			defer f.Close()
			sc := bufio.NewScanner(f)
			for sc.Scan() {
				if num, url, ok := strings.Cut(sc.Text(), "\t"); ok && num == strconv.Itoa(n) {
					return url
				}
			}
		}
	}
	out, err := exec.Command("git", "-C", e.Path, "remote", "get-url", "origin").Output()
	if err != nil {
		return ""
	}
	m := githubRemoteRe.FindStringSubmatch(strings.TrimSpace(string(out)))
	if m == nil {
		return ""
	}
	return fmt.Sprintf("https://github.com/%s/%s/issues/%d", m[1], m[2], n)
}
