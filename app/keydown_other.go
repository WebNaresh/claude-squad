//go:build !darwin || !cgo

package app

// spaceDown: unknown here, so a held Space is only its repeats, as without cs.
func spaceDown() bool { return false }
