package session

import "testing"

func TestIsClaudeCLI(t *testing.T) {
	for comm, want := range map[string]bool{
		"/Users/x/.local/bin/claude":  true,
		"claude":                      true,
		"node":                        false,
		"/usr/local/bin/claude-squad": false,
		"cs-fork":                     false,
	} {
		if got := isClaudeCLI(comm); got != want {
			t.Errorf("isClaudeCLI(%q) = %v, want %v", comm, got, want)
		}
	}
}
