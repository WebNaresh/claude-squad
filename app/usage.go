package app

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

// Every usageInterval cs writes one line to ~/.claude-squad/usage.log: its
// own CPU/memory/goroutines and render rate, the Claude and tmux processes,
// the Mac's load and free memory, and how long the slow refreshes took. Cut
// back at 1MB like the other logs.

const usageInterval = 30 * time.Second

type usageTickMsg struct{ line string }

// perf counters, updated from the UI and refresh code.
var perf struct {
	renders       atomic.Int64 // View() calls since the last usage line
	renderNanos   atomic.Int64 // time spent in View()
	sessionsNanos atomic.Int64 // last ListExternalSessions duration
	gitNanos      atomic.Int64 // last git status duration
}

func usageTick() tea.Cmd {
	return func() tea.Msg {
		time.Sleep(usageInterval)
		return usageTickMsg{line: collectUsage()}
	}
}

func collectUsage() string {
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	renders := perf.renders.Swap(0)
	renderNanos := perf.renderNanos.Swap(0)
	avgRender := time.Duration(0)
	if renders > 0 {
		avgRender = time.Duration(renderNanos / renders)
	}

	csCPU, csRSS := psSelf()
	claudeN, claudeRSS, claudeCPU := psMatch(func(cmd string) bool {
		return strings.Contains(cmd, "/.local/bin/claude") || strings.HasPrefix(cmd, "claude ") || cmd == "claude" ||
			strings.Contains(cmd, "/claude/versions/")
	})
	tmuxN, tmuxRSS, _ := psMatch(func(cmd string) bool { return strings.HasPrefix(cmd, "tmux") })
	sessions := 0
	if out, err := exec.Command("tmux", "list-sessions").Output(); err == nil {
		sessions = strings.Count(string(out), "\n")
	}
	load, _ := exec.Command("sysctl", "-n", "vm.loadavg").Output()
	free := "?"
	if out, err := exec.Command("memory_pressure", "-Q").Output(); err == nil {
		if i := strings.LastIndex(string(out), ":"); i >= 0 {
			free = strings.TrimSpace(string(out)[i+1:])
		}
	}
	return fmt.Sprintf("cs cpu=%s%% rss=%dMB heap=%dMB goroutines=%d renders=%d/%s avg=%s | claude procs=%d rss=%dMB cpu=%.0f%% | tmux procs=%d sessions=%d rss=%dMB | mac load=%s free-mem=%s | last refresh sessions=%s git=%s",
		csCPU, csRSS/1024, ms.HeapAlloc>>20, runtime.NumGoroutine(), renders, usageInterval, avgRender.Round(time.Microsecond),
		claudeN, claudeRSS/1024, claudeCPU,
		tmuxN, sessions, tmuxRSS/1024,
		strings.Trim(strings.TrimSpace(string(load)), "{} "), free,
		time.Duration(perf.sessionsNanos.Load()).Round(time.Millisecond), time.Duration(perf.gitNanos.Load()).Round(time.Millisecond))
}

// psSelf returns cs's own CPU% and RSS (KB).
func psSelf() (string, int) {
	out, err := exec.Command("ps", "-o", "%cpu=,rss=", "-p", strconv.Itoa(os.Getpid())).Output()
	if err != nil {
		return "?", 0
	}
	f := strings.Fields(string(out))
	if len(f) < 2 {
		return "?", 0
	}
	rss, _ := strconv.Atoi(f[1])
	return f[0], rss
}

// psMatch sums processes whose command matches: count, RSS (KB), CPU%.
func psMatch(match func(cmd string) bool) (n, rss int, cpu float64) {
	out, err := exec.Command("ps", "-axo", "rss=,%cpu=,command=").Output()
	if err != nil {
		return 0, 0, 0
	}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 {
			continue
		}
		cmd := strings.Join(f[2:], " ")
		if !match(cmd) {
			continue
		}
		r, _ := strconv.Atoi(f[0])
		c, _ := strconv.ParseFloat(f[1], 64)
		n, rss, cpu = n+1, rss+r, cpu+c
	}
	return n, rss, cpu
}

// writeUsage appends a usage line.
func writeUsage(line string) {
	if inTest {
		return
	}
	writeLog("usage.log", time.Now().Format("2006-01-02 15:04:05")+" "+line+"\n")
}
