package overlay

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// The dialog is pasted over a screen whose resets carry a background colour
// (app/background.go: ESC[0;48;2;31;31;31m), whose rows hold emoji such as
// 🟢, and clickable links Claude prints (OSC 8: ESC]8;id=…;url ESC\). The
// old cutting counted a link's hidden URL as text: rows came out wider than
// the screen and a half-cut code printed as "8;48;2;31;31;31m". Every row
// must keep the screen's width, and no escape code may show as text.
func TestPlaceOverlayKeepsRowsIntact(t *testing.T) {
	reset := "\x1b[0;48;2;31;31;31m"
	row := reset + "\x1b[38;5;34m🟢\x1b[0;48;2;31;31;31m react-minified-error-310-#948 " + reset + "\x1b]8;id=1c45ugt;https://github.com/o/r/pull/951\x1b\\#951\x1b]8;;\x1b\\ " + strings.Repeat("x", 40) + "\x1b[38;2;200;100;50m" + strings.Repeat("y", 30) + reset
	width := ansi.StringWidth(row)
	bg := strings.Repeat(row+"\n", 9) + row
	fg := NewConfirmationOverlay("Close 🟢 react-minified-error-310-#948? Claude stops; the conversation can be resumed later.")
	fg.SetWidth(40)
	out := PlaceOverlay(20, 1, fg.Render(), bg, false, false)
	for i, l := range strings.Split(out, "\n") {
		if w := ansi.StringWidth(l); w != width {
			t.Errorf("row %d is %d columns wide, want %d: %q", i, w, width, ansi.Strip(l))
		}
		if s := ansi.Strip(l); strings.Contains(s, "48;2") || strings.Contains(s, "38;5") || strings.Contains(s, "]8;") || strings.Contains(s, "\x1b") {
			t.Errorf("row %d shows a cut escape code: %q", i, s)
		}
	}
}
