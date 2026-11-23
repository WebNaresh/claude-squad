package app

import (
	"claude-squad/session"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func setCurrent(t *testing.T, home, uuid string) {
	t.Helper()
	os.WriteFile(filepath.Join(home, ".claude.json"), []byte(`{"oauthAccount":{"accountUuid":"`+uuid+`"}}`), 0o600)
}

// A notice first seen while A was signed in, still on screen after the switch
// to B, must mark A (not B) and must not switch again.
func TestStaleNoticeMarksItsOwnAccount(t *testing.T) {
	home := t.TempDir()
	t.Setenv("CLAUDE_SQUAD_HOME", filepath.Join(home, "cs"))
	t.Setenv("HOME", home)
	limitOwner = map[string]string{}
	var used []string
	chromeReset = func(string) string { return "" }
	useAccount = func(n string) error { used = append(used, n); return nil }
	defer func() { useAccount = session.UseAccount }()

	setCurrent(t, home, "A")
	at := time.Now().Add(time.Hour)
	noteLimitSeen(at)        // daemon scan while A is signed in
	setCurrent(t, home, "B") // user /login to B; notice still on the tile
	if switchedOnLimit(at, time.Now(), map[string]bool{}) || len(used) != 0 {
		t.Fatal("switched on a stale notice")
	}
	// B must not be limited by A's notice.
	setCurrent(t, home, "B")
	if got := limitOwner["acct|"+at.Format(time.RFC3339)]; got != "A" {
		t.Fatalf("owner = %q, want A", got)
	}
}
