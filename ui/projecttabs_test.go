package ui

import (
	"testing"

	"github.com/charmbracelet/lipgloss"
	"github.com/stretchr/testify/require"
)

func TestProjectTabs_HitTest(t *testing.T) {
	tabs := NewProjectTabs()
	tabs.SetWidth(100)
	tabs.SetProjects([]string{"/c/practice-stack", "/c/basic", "/c/glitchgrab"}, "/c/basic")
	row := tabs.String()
	require.Equal(t, 100, lipgloss.Width(row))
	require.Equal(t, "/c/basic", tabs.Active())

	// " practice-stack " is 16 columns, then a 1-column separator.
	require.Equal(t, 0, tabs.HitTest(0))
	require.Equal(t, 0, tabs.HitTest(15))
	require.Equal(t, -1, tabs.HitTest(16))
	require.Equal(t, 1, tabs.HitTest(17))
	require.Equal(t, -1, tabs.HitTest(60))
	require.Equal(t, HitAddProject, tabs.HitTest(99))
	addW := lipgloss.Width(" + add project (⌃Space A) ")
	require.Equal(t, HitAddProject, tabs.HitTest(100-addW))
	require.Equal(t, -1, tabs.HitTest(100-addW-1))

	require.Equal(t, "/c/glitchgrab", tabs.Select(2))
	require.Equal(t, "/c/practice-stack", tabs.Next())
	require.Equal(t, "/c/glitchgrab", tabs.Prev())
}

func TestUniqueNames(t *testing.T) {
	names := uniqueNames([]string{
		"/Users/w/coding-line/glitchgrab",
		"/Users/w/github-runners/runner-2/_work/abhyasika_turbo_repo/abhyasika_turbo_repo",
		"/Users/w/github-runners/runner-3/_work/abhyasika_turbo_repo/abhyasika_turbo_repo",
		"/Users/w/github-runners/runner-3/_work/glitchgrab/glitchgrab",
		"/Users/w/coding-line/claude-squad",
	})
	require.Equal(t, []string{
		"glitchgrab · coding-line",
		"abhyasika_turbo_repo · runner-2",
		"abhyasika_turbo_repo · runner-3",
		"glitchgrab · runner-3",
		"claude-squad",
	}, names)
}
