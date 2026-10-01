package ui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// The key bar is the line at the bottom: what's happening and the keys that
// matter now, drawn as keycaps so the app can be used without reading docs.

var (
	keycapStyle = lipgloss.NewStyle().Bold(true).
			Foreground(lipgloss.Color("#1a1a1a")).
			Background(lipgloss.Color("#dde4f0")).
			Padding(0, 1)
	keyLabelStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#333333", Dark: "#cccccc"})
	keyDimStyle   = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#888888", Dark: "#888888"})
	keyLeadStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("230")).Background(lipgloss.Color("62")).Padding(0, 1)
	keyStateStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("62"))
)

// Key is one shortcut: {key(s), what it does}.
type Key [2]string

// KeyRow renders keys as keycaps with labels, dropping the ones that don't
// fit in width (the leading ones are the most important).
func KeyRow(lead string, keys []Key, width int) string {
	const sep = "   "
	more := keyDimStyle.Render("…")
	parts := []string{}
	if lead != "" {
		parts = append(parts, lead)
	}
	used := lipgloss.Width(lead)
	for n, k := range keys {
		item := keycapStyle.Render(k[0]) + " " + keyLabelStyle.Render(k[1])
		w := lipgloss.Width(item)
		if len(parts) > 0 {
			w += len(sep)
		}
		// Keep room for the "…" marker unless this is the last key.
		need := w
		if n < len(keys)-1 {
			need += len(sep) + lipgloss.Width(more)
		}
		if used+need > width {
			parts = append(parts, more)
			break
		}
		parts = append(parts, item)
		used += w
	}
	return strings.Join(parts, sep)
}

// KeyBar renders the one-line bar: what's happening on the left, then the
// keys that matter now, after lead (e.g. "⌃Space then") when they need a
// prefix; plain keys (now) come before lead. A last "?" key (all keys)
// always stays, however narrow the bar.
func KeyBar(state string, now []Key, lead string, keys []Key, width int) string {
	head := keyStateStyle.Render(state) + keyDimStyle.Render("  │")
	if len(now) > 0 {
		head += "  " + KeyRow("", now, width/2)
	}
	if lead != "" {
		if len(now) > 0 {
			head += " "
		}
		head += "  " + keyLeadStyle.Render(lead)
	}
	if n := len(keys); n > 0 && keys[n-1][0] == "?" {
		pin := keycapStyle.Render(keys[n-1][0]) + " " + keyLabelStyle.Render(keys[n-1][1])
		return KeyRow(head, keys[:n-1], width-lipgloss.Width(pin)-3) + "   " + pin
	}
	return KeyRow(head, keys, width)
}
