package ui

import "testing"

// A wide screen once put every session in one row of tall narrow strips.
func TestGridLayoutBalanced(t *testing.T) {
	cases := []struct{ n, w, h, cols, rows int }{
		{7, 322, 110, 4, 2},
		{4, 322, 110, 2, 2},
		{6, 322, 110, 3, 2},
		{12, 322, 110, 4, 3},
		{2, 160, 50, 2, 1},
		{1, 160, 50, 1, 1},
	}
	for _, c := range cases {
		cols, rows, _ := GridLayout(c.n, c.w, c.h)
		if cols != c.cols || rows != c.rows {
			t.Errorf("%d tiles in %dx%d: got %dx%d, want %dx%d", c.n, c.w, c.h, cols, rows, c.cols, c.rows)
		}
	}
	// Too many to fit at the minimum size: pages instead (one row kept).
	if _, _, bodyH := GridLayout(20, 160, 40); bodyH != 39 {
		t.Errorf("20 tiles in 160x40 should page, bodyH=%d", bodyH)
	}
}
