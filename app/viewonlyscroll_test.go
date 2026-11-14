package app

import (
	"fmt"
	"strings"
	"testing"
)

func numbered(n int) string {
	var rows []string
	for i := 1; i <= n; i++ {
		rows = append(rows, fmt.Sprint("line ", i))
	}
	return strings.Join(rows, "\n") + "\n"
}

func TestViewOnlyWindow(t *testing.T) {
	content := numbered(50)
	got, back := viewOnlyWindow(content, 10, viewPos{}, "t1")
	if back != 0 || got != content {
		t.Fatalf("bottom: back=%d", back)
	}
	// Five wheel notches up.
	var p viewPos
	for i := 0; i < 5; i++ {
		p = scrollViewOnly(p, 1, "t1")
	}
	if got, back = viewOnlyWindow(content, 10, p, "t1"); back != 5 || !strings.HasSuffix(got, "line 45\n") {
		t.Fatalf("5 back: back=%d", back)
	}
	// Past the top: stops with the first line on the tile's top row, and
	// one notch down moves at once.
	for i := 0; i < 100; i++ {
		p = scrollViewOnly(p, 1, "t1")
	}
	if got, back = viewOnlyWindow(content, 10, p, "t1"); back != 40 || !strings.HasSuffix(got, "line 10\n") {
		t.Fatalf("top: back=%d", back)
	}
	if p = scrollViewOnly(p, -1, "t1"); p.back != 39 {
		t.Fatalf("one down from top: %d", p.back)
	}
	// Back at the bottom it can't go below 0.
	for i := 0; i < 100; i++ {
		p = scrollViewOnly(p, -1, "t1")
	}
	if p.back != 0 {
		t.Fatalf("below bottom: %d", p.back)
	}
	// Shorter than the tile: nothing to scroll.
	if _, back = viewOnlyWindow("a\nb\n", 10, viewPos{back: 3, most: 0}, "t2"); back != 0 {
		t.Fatalf("short: back=%d", back)
	}
}

// Claude still writing while the tile is scrolled back: the text being read
// stays where it is instead of sliding with every new line.
func TestViewOnlyWindowStaysPut(t *testing.T) {
	viewOnlyWindow(numbered(50), 10, viewPos{}, "t3")
	p := scrollViewOnly(viewPos{}, 5, "t3") // ends at line 45
	got, back := viewOnlyWindow(numbered(60), 10, p, "t3")
	if back != 15 || !strings.HasSuffix(got, "line 45\n") {
		t.Fatalf("after 10 new lines: back=%d, want the view still ending at line 45", back)
	}
	// At the bottom it follows new lines as before.
	if got, _ = viewOnlyWindow(numbered(70), 10, viewPos{}, "t3"); !strings.HasSuffix(got, "line 70\n") {
		t.Fatal("bottom didn't follow")
	}
}
