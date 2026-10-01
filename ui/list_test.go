package ui

import (
	"claude-squad/session"
	"testing"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/stretchr/testify/require"
)

func newTestList(titles ...string) *List {
	s := spinner.New()
	l := NewList(&s, false)
	for _, t := range titles {
		inst, _ := session.NewInstance(session.InstanceOptions{
			Title:   t,
			Path:    ".",
			Program: "echo",
		})
		l.AddInstance(inst)
	}
	return l
}

func TestMoveUp(t *testing.T) {
	l := newTestList("a", "b", "c")
	l.SetSelectedInstance(1) // select "b"

	moved := l.MoveUp()
	require.True(t, moved)
	require.Equal(t, 0, l.selectedIdx)
	require.Equal(t, "b", l.items[0].Title)
	require.Equal(t, "a", l.items[1].Title)
	require.Equal(t, "c", l.items[2].Title)
}

func TestMoveUp_AtTop(t *testing.T) {
	l := newTestList("a", "b", "c")
	l.SetSelectedInstance(0)

	moved := l.MoveUp()
	require.False(t, moved)
	require.Equal(t, 0, l.selectedIdx)
	require.Equal(t, "a", l.items[0].Title)
}

func TestMoveDown(t *testing.T) {
	l := newTestList("a", "b", "c")
	l.SetSelectedInstance(1) // select "b"

	moved := l.MoveDown()
	require.True(t, moved)
	require.Equal(t, 2, l.selectedIdx)
	require.Equal(t, "a", l.items[0].Title)
	require.Equal(t, "c", l.items[1].Title)
	require.Equal(t, "b", l.items[2].Title)
}

func TestMoveDown_AtBottom(t *testing.T) {
	l := newTestList("a", "b", "c")
	l.SetSelectedInstance(2)

	moved := l.MoveDown()
	require.False(t, moved)
	require.Equal(t, 2, l.selectedIdx)
	require.Equal(t, "c", l.items[2].Title)
}

func TestMoveWithSingleItem(t *testing.T) {
	l := newTestList("only")
	l.SetSelectedInstance(0)

	require.False(t, l.MoveUp())
	require.False(t, l.MoveDown())
}

func TestSetProject_FiltersAndKeepsAll(t *testing.T) {
	s := spinner.New()
	l := NewList(&s, false)
	for _, tc := range []struct{ title, path string }{{"a", "/p/one"}, {"b", "/p/two/sub"}, {"c", "/p/one"}, {"d", "/p/onebis"}} {
		inst, _ := session.NewInstance(session.InstanceOptions{Title: tc.title, Path: tc.path, Program: "echo"})
		l.AddInstance(inst)
	}

	l.SetProject("/p/one")
	require.Equal(t, 2, l.NumInstances())
	require.Equal(t, "a", l.items[0].Title)
	require.Equal(t, "c", l.items[1].Title)
	require.Equal(t, 4, len(l.GetInstances()))

	l.SetProject("/p/two")
	require.Equal(t, 1, l.NumInstances())
	require.Equal(t, "b", l.GetSelectedInstance().Title)
	require.Equal(t, 1, l.CountInProject("/p/onebis"))
}

func TestMoveUp_WithProjectKeepsOrderInAll(t *testing.T) {
	s := spinner.New()
	l := NewList(&s, false)
	for _, tc := range []struct{ title, path string }{{"a", "/p/one"}, {"x", "/p/two"}, {"b", "/p/one"}} {
		inst, _ := session.NewInstance(session.InstanceOptions{Title: tc.title, Path: tc.path, Program: "echo"})
		l.AddInstance(inst)
	}
	l.SetProject("/p/one")
	l.SetSelectedInstance(1) // "b"
	require.True(t, l.MoveUp())

	all := l.GetInstances()
	require.Equal(t, "b", all[0].Title)
	require.Equal(t, "x", all[1].Title)
	require.Equal(t, "a", all[2].Title)
}

func TestNextNeedsYou_AcrossTabs(t *testing.T) {
	s := spinner.New()
	l := NewList(&s, false)
	mk := func(title, path string, needs bool) *session.Instance {
		inst, _ := session.NewInstance(session.InstanceOptions{Title: title, Path: path, Program: "echo"})
		inst.NeedsYou = needs
		l.AddInstance(inst)
		return inst
	}
	mk("a", "/p/one", false)
	mk("b", "/p/one", false)
	c := mk("c", "/p/two", true)
	l.SetExternal([]*session.ExternalSession{{Name: "cc_x", Path: "/p/one", Status: "waiting"}})
	l.SetProject("/p/one")

	// From the first row of /p/one, the waiting external session in the same tab comes first.
	p, inst, ext := l.NextNeedsYou([]string{"/p/one", "/p/two"}, "/p/one")
	require.Equal(t, "/p/one", p)
	require.Nil(t, inst)
	require.Equal(t, "cc_x", ext.Name)

	// With it selected, the next one is the agent in the other tab.
	l.SelectExternal("cc_x")
	p, inst, _ = l.NextNeedsYou([]string{"/p/one", "/p/two"}, "/p/one")
	require.Equal(t, "/p/two", p)
	require.Equal(t, c, inst)

	require.Equal(t, 2, l.CountNeedsInProject("/p/one")+l.CountNeedsInProject("/p/two"))
	p, _, _ = l.NextNeedsYou(nil, "")
	require.Equal(t, "", p)
}
