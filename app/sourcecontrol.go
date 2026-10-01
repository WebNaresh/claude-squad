package app

import (
	"claude-squad/session/git"
	"claude-squad/ui/overlay"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// The left column is the project's Source Control. g focuses it; inside it,
// keys act on the selected file and its diff shows in the right pane.

const scRefreshInterval = 2 * time.Second

type scStatusMsg struct {
	root       string
	branch     string
	files      []git.FileStatus
	autoCommit bool
	err        error
}

type scTickMsg struct{}

func scTick() tea.Cmd {
	return func() tea.Msg {
		time.Sleep(scRefreshInterval)
		return scTickMsg{}
	}
}

// refreshSourceControl reads git status for the active project off the UI
// thread; at most one read runs at a time.
func (m *home) refreshSourceControl() tea.Cmd {
	root := m.projectTabs.Active()
	if root == "" || m.scLoading[root] {
		return nil
	}
	m.scLoading[root] = true
	return func() tea.Msg {
		started := time.Now()
		files, err := git.Status(root)
		perf.gitNanos.Store(int64(time.Since(started)))
		return scStatusMsg{root: root, branch: git.Branch(root), files: files, autoCommit: git.AutoCommitWatched(root), err: err}
	}
}

// showSelectedFileDiff puts the selected file's diff in the right pane.
func (m *home) showSelectedFileDiff() tea.Cmd {
	f, staged, ok := m.sourceControl.SelectedFile()
	if !ok {
		m.tabbedWindow.SetFileDiff("", "No changes")
		return nil
	}
	root := m.sourceControl.Root()
	diff, err := git.FileDiff(root, f, staged)
	if err != nil {
		return m.handleError(err)
	}
	// The box's title names the file; git's header lines repeat it.
	if i := strings.Index(diff, "\n@@"); i >= 0 && strings.HasPrefix(diff, "diff --git") {
		diff = diff[i+1:]
	}
	if diff == "" {
		diff = "No textual changes (binary file or mode change)"
	}
	where := "changes"
	if staged {
		where = "staged"
	}
	m.tabbedWindow.SetFileDiff(f.Path+" · "+where, diff)
	return nil
}

// scAction runs a git action, then refreshes the panel.
func (m *home) scAction(action func(root string) error) tea.Cmd {
	root := m.sourceControl.Root()
	return func() tea.Msg {
		if err := action(root); err != nil {
			return err
		}
		return scActionDoneMsg{}
	}
}

type scActionDoneMsg struct{}

// scRefreshDiffMsg re-shows the selected file's diff after an action.
type scRefreshDiffMsg struct{}

// handleSourceControlKey handles a key while the Source Control column has
// focus. ok is false for keys it leaves to the normal handlers (switching
// project tabs, quitting, help).
func (m *home) handleSourceControlKey(msg tea.KeyMsg) (tea.Cmd, bool) {
	f, staged, hasFile := m.sourceControl.SelectedFile()
	switch msg.String() {
	case "esc", "i", "I":
		m.sourceControl.SetFocused(false)
		m.tabbedWindow.ClearFileDiff()
		return m.autoFocus(), true
	case "alt+up", "alt+down":
		if msg.String() == "alt+up" {
			m.list.Up()
		} else {
			m.list.Down()
		}
		return m.instanceChanged(), true
	case "up", "k":
		m.sourceControl.Up()
		return m.showSelectedFileDiff(), true
	case "down", "j":
		m.sourceControl.Down()
		return m.showSelectedFileDiff(), true
	case " ", "enter":
		if !hasFile {
			return nil, true
		}
		if staged {
			return m.scAction(func(root string) error { return git.Unstage(root, f.Path) }), true
		}
		return m.scAction(func(root string) error { return git.Stage(root, f.Path) }), true
	case "a":
		return m.scAction(func(root string) error { return git.Stage(root) }), true
	case "u":
		return m.scAction(func(root string) error { return git.Unstage(root) }), true
	case "d":
		if !hasFile || staged {
			return m.handleError(fmt.Errorf("select a file under Changes to discard (unstage it first with space)")), true
		}
		message := fmt.Sprintf("[!] Discard changes to %s? This can't be undone.", f.Path)
		if f.Untracked() {
			message = fmt.Sprintf("[!] Delete %s? It isn't tracked by git, so this can't be undone.", f.Path)
		}
		root := m.sourceControl.Root()
		return m.confirmAction(message, func() tea.Msg {
			if err := git.Discard(root, f); err != nil {
				return err
			}
			return scActionDoneMsg{}
		}), true
	case "c":
		if m.sourceControl.StagedCount() == 0 {
			return m.handleError(fmt.Errorf("nothing staged to commit; stage files with space or a")), true
		}
		m.commitOverlay = overlay.NewTextInputOverlay("Commit message (Tab, then Enter to commit)", "")
		m.state = stateCommit
		return tea.WindowSize(), true
	case "alt+left", "alt+right", "alt+b", "alt+f", "alt+i", "alt+I", "+", "=", "!", "q", "ctrl+c", "ctrl+q", "?":
		return nil, false
	}
	return nil, true
}

// handleCommitKey drives the commit message box.
func (m *home) handleCommitKey(msg tea.KeyMsg) tea.Cmd {
	shouldClose, _ := m.commitOverlay.HandleKeyPress(msg)
	if !shouldClose {
		return nil
	}
	o := m.commitOverlay
	m.commitOverlay = nil
	m.state = stateDefault
	if o.IsCanceled() || !o.IsSubmitted() {
		return tea.WindowSize()
	}
	message := o.GetValue()
	return tea.Batch(tea.WindowSize(), m.scAction(func(root string) error { return git.Commit(root, message) }))
}
