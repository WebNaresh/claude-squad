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
		t := wrapRow(lines[r])
		b.WriteString(t)
		for range len(t) {
			rowOf = append(rowOf, r)
		}
	}
	text := b.String()
	for start := 0; start < len(text); {
		loc := pathRe.FindStringIndex(text[start:])
		if loc == nil {
			break
		}
		a, z := start+loc[0], start+loc[1]
		if rowOf[a] > row {
			break // starts below the click
		}
		// Joining rows can glue the next word, or the next listed file, onto
		// the path's end; take the longest prefix that exists. It can also
		// glue the word before onto its start ("Steps are in" + "/private/…"
		// → "in/private/…"): then try again from the path's own "/".
		found, end := "", 0
		starts := []int{a}
		if i := strings.Index(text[a:z], "/"); i > 0 {
			starts = append(starts, a+i)
		}
		for _, from := range starts {
			for e := z; e > from+1; e-- {
				if p := resolvePath(strings.TrimRight(text[from:e], ".,:;)'\"`"), dir); p != "" {
					found, end = p, e
					break
				}
			}
			if found != "" {
				break
			}
		}
		if found != "" && rowOf[end-1] >= row {
			return found
		}
		// That path ended above the click: look at what follows it.
		if found != "" {
			start = end
		} else {
			start = z
		}
	}
	return ""
}

// Claude prints a sent file's size at the right edge of its wrapped path
// ("…-practice-stack (283.9K" / "[image] /3f69…/before-booking-det B)" /
// "ails.png"), and labels the block "[image]" or "›". Those pieces would be
// glued into the path when the rows are joined, so they are cut first.
var (
	sizeTailRe = regexp.MustCompile(`\s+(\(\d+(\.\d+)?\s*[KMGT]?B?\)?|[KMGT]?B\))$`)
	blockHead  = regexp.MustCompile(`^(\[image\]|›|⎿)\s*`)
)

// wrapRow is one row of a wrapped path with the size and the label cut off.
func wrapRow(l string) string {
	t := strings.TrimSpace(l)
	t = blockHead.ReplaceAllString(t, "")
	return strings.TrimSpace(sizeTailRe.ReplaceAllString(t, ""))
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
		if st, err := os.Stat(p); err == nil && (st.IsDir() || isImage(p)) {
			cmd = exec.Command("open", p) // Finder, or Preview for an image
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

func isImage(p string) bool {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".png", ".jpg", ".jpeg", ".gif", ".webp", ".heic", ".svg", ".pdf":
		return true
	}
	return false
}
