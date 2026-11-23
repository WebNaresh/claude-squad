package app

import (
	"slices"
	"testing"
)

func TestBlockedPorts(t *testing.T) {
	text := `Error: listen EADDRINUSE: address already in use :::4444
    at <unknown> (Error: listen EADDRINUSE:
address already in use :::4444)
Error: listen EADDRINUSE: address already in use 127.0.0.1:3000
OSError: [Errno 48] Address already in use: port 8000
ready on :5173`
	if got, want := blockedPorts(text), []int{4444, 3000, 8000}; !slices.Equal(got, want) {
		t.Fatalf("blockedPorts = %v, want %v", got, want)
	}
	if got := blockedPorts("ready - started server on 0.0.0.0:3000"); got != nil {
		t.Fatalf("no clash: got %v", got)
	}
}
