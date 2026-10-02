package app

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

// A click on a file path in a tile opens the file: Claude often ends with
// "the query is in /private/tmp/…/scratchpad/usage.sql", wrapped over
// several rows, which Terminal.app can't open and is hard to copy.

// pathRe matches path-like text: absolute, ~/, or relative with a slash.
var pathRe = regexp.MustCompile(`(?:~|\.{0,2})?/?[\w.\-~@+]+(?:/[\w.\-~@+]*)+`)

// pathAt returns the existing file or folder whose path is shown at content
// row row (wrapped paths are joined back), or "". Relative paths resolve
// against dir, the session's folder.
func pathAt(lines []string, row int, dir string) string {
	if row < 0 || row >= len(lines) {
		return ""
	}
	// Claude wraps long paths onto the next rows, indented: join a few rows
	// around the click without spaces, remembering which row each byte is from.
	// The paragraph around the click: up to the blank rows above and below.
	lo, hi := row, row
	for lo > 0 && row-lo < 12 && strings.TrimSpace(lines[lo-1]) != "" {
		lo--
	}
	for hi < len(lines)-1 && hi-row < 12 && strings.TrimSpace(lines[hi+1]) != "" {
		hi++
	}
	var b strings.Builder
	var rowOf []int
	for r := lo; r <= hi; r++ {
		t := strings.TrimSpace(lines[r])
		b.WriteString(t)
		for range len(t) {
			rowOf = append(rowOf, r)
		}
	}
	text := b.String()
	for _, loc := range pathRe.FindAllStringIndex(text, -1) {
		if rowOf[loc[0]] > row || rowOf[loc[1]-1] < row {
			continue // not under the click
		}
		// Joining rows can glue the next word onto the path's end
		// ("usage.sql" + "Crunched"); take the longest prefix that exists.
		for end := loc[1]; end > loc[0]+1; end-- {
			if p := resolvePath(strings.TrimRight(text[loc[0]:end], ".,:;)'\"`"), dir); p != "" {
				return p
			}
		}
	}
	return ""
}

func resolvePath(p, dir string) string {
	if p == "" || p == "/" {
		return ""
	}
	if strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil {
			return ""
		}
		p = filepath.Join(home, p[2:])
	} else if !filepath.IsAbs(p) {
		if dir == "" {
			return ""
		}
		p = filepath.Join(dir, p)
	}
	if _, err := os.Stat(p); err != nil {
		return ""
	}
	return p
}

// openPath opens a file in VS Code (or the default text editor) and a
// folder in Finder.
func openPath(p string) tea.Cmd {
	return func() tea.Msg {
		var cmd *exec.Cmd
		if st, err := os.Stat(p); err == nil && st.IsDir() {
			cmd = exec.Command("open", p)
		} else if code, err := exec.LookPath("code"); err == nil {
			cmd = exec.Command(code, "-g", p)
		} else {
			cmd = exec.Command("open", "-t", p)
		}
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("could not open %s: %s", filepath.Base(p), strings.TrimSpace(string(out)))
		}
		logEvent("opened %s", p)
		return fmt.Errorf("opened %s", filepath.Base(p))
	}
}
