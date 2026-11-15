package app

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestInitProjectFolder(t *testing.T) {
	// A new empty folder becomes a git repository and is added.
	dir := t.TempDir()
	got, err := initProjectFolder(dir)
	if err != nil || got != dir {
		t.Fatalf("empty folder: got %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(dir, ".git")); err != nil {
		t.Fatal("no .git after init")
	}
	// A folder of projects is refused, and not turned into a repository.
	parent := t.TempDir()
	if err := os.MkdirAll(filepath.Join(parent, "proj", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := initProjectFolder(parent); err == nil || !strings.Contains(err.Error(), "holds other projects") {
		t.Fatalf("folder of projects: %v", err)
	}
	if _, err := os.Stat(filepath.Join(parent, ".git")); err == nil {
		t.Fatal("folder of projects was git-inited")
	}
}
