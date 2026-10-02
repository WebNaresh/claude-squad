package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

const lockFileName = "cs.lock"

// AcquireLock makes sure only one cs window runs at a time. Two windows each
// hold their own copy of the agent list and the last one to save wins, which
// brings deleted agents back or drops new ones. It returns a release function,
// or an error naming the process that already holds the lock.
func AcquireLock() (release func(), err error) {
	return acquire(lockFileName, "cs is already open in another window (process %d); use that one, or quit it first")
}

// IssuesDaemonLock is held by the background issue runner (cs --issues-daemon).
const IssuesDaemonLock = "issues-daemon.lock"

// AcquireNamedLock takes lock file name for this process; busy is the error
// format (with the holder's pid) when another live process holds it.
func AcquireNamedLock(name, busy string) (release func(), err error) {
	return acquire(name, busy)
}

// LockHolder returns the live process holding lock file name, or 0.
func LockHolder(name string) int {
	dir, err := GetConfigDir()
	if err != nil {
		return 0
	}
	data, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		return 0
	}
	if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && processAlive(pid) {
		return pid
	}
	return 0
}

func acquire(name, busy string) (release func(), err error) {
	dir, err := GetConfigDir()
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, err
	}
	path := filepath.Join(dir, name)

	if data, err := os.ReadFile(path); err == nil {
		if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil && pid != os.Getpid() && processAlive(pid) {
			return nil, fmt.Errorf(busy, pid)
		}
	}
	if err := os.WriteFile(path, []byte(strconv.Itoa(os.Getpid())), 0644); err != nil {
		return nil, err
	}
	return func() {
		if data, err := os.ReadFile(path); err == nil && strings.TrimSpace(string(data)) == strconv.Itoa(os.Getpid()) {
			_ = os.Remove(path)
		}
	}, nil
}

func processAlive(pid int) bool {
	p, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return p.Signal(syscall.Signal(0)) == nil
}
