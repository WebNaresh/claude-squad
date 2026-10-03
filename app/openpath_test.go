package app

import (
	"os"
	"path/filepath"
	"testing"
)

// The case from the screenshot: a path wrapped over three indented rows,
// followed by a full stop and the next paragraph.
func TestPathAtWrapped(t *testing.T) {
	dir := t.TempDir()
	long := filepath.Join(dir, "claude-501", "-Users-webnaresh-coding-line-practice-stack", "7f582338-8a55-4e40", "scratchpad")
	if err := os.MkdirAll(long, 0o755); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(long, "usage.sql")
	if err := os.WriteFile(file, []byte("select 1;"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Wrap the path at 30 columns, as Claude does in a narrow tile.
	text := "yourself: ! psql \"$PROD_URL\" -f " + file + "."
	var lines []string
	for len(text) > 30 {
		lines = append(lines, "   "+text[:30])
		text = text[30:]
	}
	lines = append(lines, "   "+text, "", "Crunched for 14s · done 1:05 AM")

	for row := 1; row < len(lines)-2; row++ {
		if got := pathAt(lines, row, ""); got != file {
			t.Errorf("row %d: got %q, want %q", row, got, file)
		}
	}
	if got := pathAt(lines, len(lines)-1, ""); got != "" {
		t.Errorf("click on the next paragraph opened %q", got)
	}
}

func TestPathAtRelative(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	f := filepath.Join(dir, "app", "grid.go")
	if err := os.WriteFile(f, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if got := pathAt([]string{"Edited app/grid.go and more"}, 0, dir); got != f {
		t.Errorf("got %q, want %q", got, f)
	}
	if got := pathAt([]string{"no path here and/or there"}, 0, dir); got != "" {
		t.Errorf("got %q for text without a real file", got)
	}
}

// Issue log "File paths in tiles can't be opened" came back: Claude prints
// the size at the right edge of a wrapped path and labels it "[image]", and
// lists files back to back. The click opened the parent folder.
func TestPathAtSentImages(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "claude-501", "-Users-webnaresh-coding-line-practice-stack")
	pad := filepath.Join(dir, "3f694cf3-168b-434b-89ac-c60dceb23b5b", "scratchpad")
	if err := os.MkdirAll(pad, 0o755); err != nil {
		t.Fatal(err)
	}
	before := filepath.Join(pad, "before-booking-details.png")
	after := filepath.Join(pad, "after-booking-details-highlighted.png")
	for _, f := range []string{before, after} {
		if err := os.WriteFile(f, []byte("png"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	lines := []string{
		"      " + dir + " (283.9K",
		"[image] /3f694cf3-168b-434b-89ac-c60dceb23b5b/scratchpad/before-booking-det B)",
		"       ails.png",
		"      " + dir + " (229.6K",
		"[image] /3f694cf3-168b-434b-89ac-c60dceb23b5b/scratchpad/after-booking-deta B)",
		"       ils-highlighted.png",
		"",
		"• Before: pick a time",
	}
	for row, want := range []string{before, before, before, after, after, after, "", ""} {
		if got := pathAt(lines, row, ""); got != want {
			t.Errorf("row %d: got %q, want %q", row, got, want)
		}
	}
}

// A path that starts on its own row after a sentence ("Steps are in" then
// "/private/…" wrapped mid-word): joining the rows glues "in" onto it, which
// once made the click open nothing.
func TestPathAtAfterSentence(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "0a44ad1c-cf08-4e9c", "scratchpad", "2236-migration.md")
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(file, []byte("steps"), 0o644); err != nil {
		t.Fatal(err)
	}
	cut := len(file) - len("tchpad/2236-migration.md")
	lines := []string{
		"  ⚠ Your action:",
		"  1. Production: the deploy stops at this migration until it's applied by hand. Steps are in",
		"     " + file[:cut],
		"     " + file[cut:],
		"  2. Decide on the capture-route data leak: fix it here, or hand it to the #2239 session.",
	}
	for row := 2; row <= 3; row++ {
		if got := pathAt(lines, row, ""); got != file {
			t.Errorf("row %d: got %q, want %q", row, got, file)
		}
	}
}
