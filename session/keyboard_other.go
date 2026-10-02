//go:build !darwin && !linux

package session

// RepairClaudeKeyboards is a no-op where terminal modes can't be read.
func RepairClaudeKeyboards() []string { return nil }
