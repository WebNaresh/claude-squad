package session

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStagePhaseKeepsLast(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	dir := filepath.Join(home, ".claude", ".session-named")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := StagePhase("s1"); got != "" {
		t.Errorf("no files: got %q", got)
	}
	// The badge was cleared by a new message; the last step stays.
	_ = os.WriteFile(filepath.Join(dir, "s1.stagelast"), []byte("done\n"), 0o644)
	if got := StagePhase("s1"); got != "✓ done · close session" {
		t.Errorf("after reset: got %q", got)
	}
	// A new /stage is under way: the live step wins.
	_ = os.WriteFile(filepath.Join(dir, "s1.stagephase"), []byte("staging\n"), 0o644)
	if got := StagePhase("s1"); got != "⋯ staging" {
		t.Errorf("during /stage: got %q", got)
	}
}
