package app

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTmuxKey(t *testing.T) {
	for name, want := range map[string]string{
		"alt+backspace": "M-BSpace", // Option+Backspace: delete word
		"ctrl+w":        "C-w",
		"ctrl+u":        "C-u",
		"backspace":     "BSpace",
		"alt+d":         "M-d",
		"alt+enter":     "M-Enter",
		"shift+tab":     "BTab",
		"alt+up":        "M-Up",
	} {
		got, ok := tmuxKey(name)
		require.True(t, ok, name)
		require.Equal(t, want, got, name)
	}
	_, ok := tmuxKey("alt+f13")
	require.False(t, ok)
}
