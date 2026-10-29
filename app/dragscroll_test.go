package app

import (
	"fmt"
	"strings"
	"testing"
)

func TestOverlayAt(t *testing.T) {
	var doc []string
	for i := 10; i < 20; i++ {
		doc = append(doc, fmt.Sprint("row ", i))
	}
	view := func(top int) []string {
		var r []string
		for i := top; i < top+5; i++ {
			r = append(r, fmt.Sprint("row ", i))
		}
		return r
	}
	for _, c := range []struct{ top, want int }{{10, 0}, {9, -1}, {8, -2}, {13, 3}, {15, 5}} {
		if o, ok := overlayAt(doc, view(c.top), 0); !ok || o != c.want {
			t.Errorf("view from row %d: o=%d ok=%v, want %d", c.top, o, ok, c.want)
		}
	}
	if _, ok := overlayAt(doc, []string{"a", "b", "c", "d", "e"}, 0); ok {
		t.Error("unrelated picture matched")
	}
}

// A drag held past the top: three pictures, each one line further up; the
// copy runs from the start row (bottom) up over rows no longer on screen.
func TestDragScrollCopiesRowsOutOfView(t *testing.T) {
	all := []string{"== 1. invoices", "id | status", "a1 | DRAFT", "a2 | READY", "(2 rows)", "== 2. lines", "x | 20000", "webnaresh %"}
	frame := func(top int) string {
		var out []string
		for _, r := range all[top : top+4] {
			out = append(out, "│ "+r+strings.Repeat(" ", 20-len(r))+" │")
		}
		return strings.Join(out, "\n")
	}
	m := &home{lastView: frame(4)}
	m.sel = tileSelection{active: true, x0: 1, x1: 22, y0: 0, y1: 3, sx: 1, sy: 3, ex: 1, ey: 0, dragged: true}
	m.sel.scroll.target = "x"
	m.syncDragDoc()
	for top := 3; top >= 0; top-- {
		m.lastView = frame(top)
		m.syncDragDoc()
	}
	m.sel.scroll.on = true
	m.sel.sx = 22 // from the start row's end, up to the top row's start
	got := m.selectedText()
	want := strings.Join(all[:8], "\n")
	if got != want {
		t.Errorf("got\n%s\nwant\n%s", got, want)
	}
}

// A psql table in a 20-column shell: tmux cut its lines mid-word.
func TestLinesForShellWrap(t *testing.T) {
	lines := []string{
		"== 1. invoices",
		" cmvn | PROPOSAL_SCHEDULE | 85000",
		"(2 rows)",
	}
	rows := []selRow{
		{raw: "== 1. invoices", c0: 0, c1: 19},
		{raw: " cmvn | PROPOSAL_SCH", c0: 0, c1: 19},
		{raw: "EDULE | 85000", c0: 0, c1: 19},
		{raw: "(2 rows)", c0: 0, c1: 7},
	}
	got, ok := linesFor(lines, 20, rows)
	want := "== 1. invoices\n cmvn | PROPOSAL_SCHEDULE | 85000\n(2 rows)"
	if !ok || got != want {
		t.Errorf("got %q ok=%v\nwant %q", got, ok, want)
	}
	// Nothing wrapped (Claude's own rows): left to the screen rules.
	if _, ok := linesFor([]string{"a", "b"}, 20, []selRow{{raw: "a", c1: 19}, {raw: "b", c1: 19}}); ok {
		t.Error("unwrapped rows taken from tmux")
	}
}
