//go:build windows

package main

import "errors"

// restartSelf is not supported on Windows; the user restarts cs by hand.
func restartSelf() error {
	return errors.New("a new build is ready; restart cs to use it")
}
