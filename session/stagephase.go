package session

import (
	"os"
	"path/filepath"
	"strings"
)

// StagePhase reads the /stage progress that ~/.claude/hooks/session-state.sh
// keeps for a Claude session (the "#N ✓ staged" badge on its status line),
// as a short label, or "" when there is none. The badge's file is cleared
// when the user sends the session a new message; .stagelast keeps the last
// step until /stage runs again, so the tile keeps showing it.
func StagePhase(sessionID string) string {
	if sessionID == "" {
		return ""
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	dir := filepath.Join(home, ".claude", ".session-named")
	b, err := os.ReadFile(filepath.Join(dir, sessionID+".stagephase"))
	if err != nil {
		if b, err = os.ReadFile(filepath.Join(dir, sessionID+".stagelast")); err != nil {
			return ""
		}
	}
	return stageLabels[strings.TrimSpace(string(b))]
}

var stageLabels = map[string]string{
	"staging": "⋯ staging",
	"staged":  "✓ staged",
	"posting": "⇡ posting proof",
	"posted":  "✓ proof posted",
	"asking":  "? guide update",
	"guide":   "📖 publishing guide",
	"done":    "✓ done · close session",
}
