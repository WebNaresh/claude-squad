package app

import (
	"claude-squad/session"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSwitchedOnLimitOncePerLimit(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CLAUDE_SQUAD_HOME", filepath.Join(home, "cs"))
	t.Setenv("HOME", home) // ~/.claude.json lives here; the real one is never read
	os.WriteFile(filepath.Join(home, ".claude.json"), []byte(`{"oauthAccount":{"accountUuid":"A"}}`), 0o600)
	var used []string
	chromeReset = func(string) string { return "" }
	useAccount = func(n string) error { used = append(used, n); return nil }
	defer func() { useAccount = session.UseAccount }()

	done := map[string]bool{}
	at := time.Now().Add(time.Hour)
	// No saved account: no switch, and not retried every scan.
	if switchedOnLimit(at, time.Now(), done) || len(used) != 0 {
		t.Fatal("switched with no saved account")
	}
	if switchedOnLimit(at, time.Now(), done) {
		t.Fatal("retried within the same limit")
	}
}
