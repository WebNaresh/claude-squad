package app

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// cs paints its own background, VS Code's Dark Modern #1f1f1f, instead of
// showing the terminal's (often navy) one, so it looks the same in every
// terminal with no settings (Terminal.app shows true colour since 2.15).
const blackBG = "48;2;31;31;31"

// softer replaces the saturated 256-colour diff backgrounds sessions draw
// (dark green 22, red 52, and their word highlights) with the ones Claude
// Code uses in VS Code's terminal.
var softer = map[string]string{
	"48;5;22": "48;2;34;92;43",
	"48;5;28": "48;2;56;166;96",
	"48;5;52": "48;2;122;41;54",
	"48;5;88": "48;2;179;89;107",
}

var sgrRe = regexp.MustCompile(`\x1b\[([0-9;:]*)m`)

// onBlack returns frame drawn on black: every reset and "default
// background" now means black, every line is padded to width and the
// frame to height rows. Running it twice changes nothing.
func onBlack(frame string, width, height int) string {
	frame = sgrRe.ReplaceAllStringFunc(frame, func(seq string) string {
		params := seq[2 : len(seq)-1]
		if params == "" || params == "0" {
			return "\x1b[0;" + blackBG + "m"
		}
		parts := strings.Split(params, ";")
		changed := false
		for i := 0; i < len(parts); i++ {
			switch parts[i] {
			case "38", "48", "58": // colour with arguments: skip them
				if i+2 < len(parts) && parts[i+1] == "5" {
					if to, ok := softer[strings.Join(parts[i:i+3], ";")]; ok {
						parts[i], parts[i+1], parts[i+2] = to, "", ""
						changed = true
					}
					i += 2
				} else if i+1 < len(parts) && parts[i+1] == "2" {
					i += 4
				}
			case "49":
				parts[i] = blackBG
				changed = true
			case "0", "":
				if i == 0 && !strings.Contains(params, blackBG) {
					parts[i] = "0;" + blackBG
					changed = true
				}
			}
		}
		if !changed {
			return seq
		}
		var kept []string
		for _, p := range parts {
			if p != "" {
				kept = append(kept, p)
			}
		}
		return "\x1b[" + strings.Join(kept, ";") + "m"
	})
	lines := strings.Split(frame, "\n")
	for len(lines) < height {
		lines = append(lines, "")
	}
	start := "\x1b[" + blackBG + "m"
	for i, l := range lines {
		pad := ""
		if w := ansi.StringWidth(l); w < width {
			pad = strings.Repeat(" ", width-w)
		}
		if !strings.HasPrefix(l, start) {
			l = start + l
		}
		if !strings.HasSuffix(l, "\x1b[0m") {
			lines[i] = l + pad
		} else {
			lines[i] = l + start + pad + "\x1b[0m"
		}
	}
	return strings.Join(lines, "\n")
}
