package app

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"claude-squad/config"

	tea "github.com/charmbracelet/bubbletea"
)

// Lag watchdog. cs handles one message at a time on the UI goroutine
// (Update, then View); while one takes long, keys and clicks wait and the
// window looks frozen. A background goroutine notices:
//
//   - a stall: one Update or View running over stallAfter. stalls.log gets
//     what it was handling and every goroutine's stack (the exact blocking
//     call), and activity.log a "UI STALL" line, plus how long it took in
//     the end.
//   - a flood: more than floodPerSec messages in a second (e.g. a trackpad
//     scroll burst), each drawing a frame. activity.log gets the count by
//     message type.
//
// Read with: grep -E "UI STALL|UI FLOOD|slow frame" ~/.claude-squad/activity.log

const (
	stallAfter   = 500 * time.Millisecond
	slowFrame    = 150 * time.Millisecond
	floodPerSec  = 60
	stallsLogMax = 2 << 20
)

var watch struct {
	busySince atomic.Int64 // unix nanos the current Update/View began, 0 when idle
	what      atomic.Value // string: what it is handling
	reported  atomic.Bool  // this stall was already logged

	mu     sync.Mutex
	counts map[string]int // messages this second, by type
}

// watchBegin marks the UI goroutine busy with what; the returned func ends it.
func watchBegin(what string) func() {
	start := time.Now()
	watch.what.Store(what)
	watch.reported.Store(false)
	watch.busySince.Store(start.UnixNano())
	return func() {
		watch.busySince.Store(0)
		if watch.reported.Load() {
			logEvent("UI STALL over: %s took %s", what, time.Since(start).Round(time.Millisecond))
		}
	}
}

// watchUpdate counts a message and marks the UI busy handling it.
func watchUpdate(msg tea.Msg) func() {
	what := msgName(msg)
	watch.mu.Lock()
	if watch.counts == nil {
		watch.counts = map[string]int{}
	}
	watch.counts[what]++
	watch.mu.Unlock()
	return watchBegin("Update " + what)
}

// msgName names a message for the logs: the key or mouse action, or its type.
func msgName(msg tea.Msg) string {
	switch m := msg.(type) {
	case tea.KeyMsg:
		return "key"
	case tea.MouseMsg:
		return "mouse " + tea.MouseEvent(m).String()
	}
	return strings.TrimPrefix(fmt.Sprintf("%T", msg), "app.")
}

// startWatchdog runs the checks for the life of the process.
func startWatchdog() {
	if inTest {
		return
	}
	go func() {
		tick := time.NewTicker(100 * time.Millisecond)
		second := time.Now()
		for now := range tick.C {
			if since := watch.busySince.Load(); since != 0 && !watch.reported.Load() {
				if d := now.Sub(time.Unix(0, since)); d > stallAfter {
					watch.reported.Store(true)
					what, _ := watch.what.Load().(string)
					logEvent("UI STALL: %s running for %s (stacks in stalls.log)", what, d.Round(time.Millisecond))
					writeStall(what, d)
				}
			}
			if now.Sub(second) >= time.Second {
				second = now
				watch.mu.Lock()
				counts := watch.counts
				watch.counts = nil
				watch.mu.Unlock()
				total := 0
				for _, n := range counts {
					total += n
				}
				if total > floodPerSec {
					logEvent("UI FLOOD: %d messages in 1s (%s)", total, topCounts(counts, 4))
				}
			}
		}
	}()
}

// topCounts lists the most frequent message types: "mouse wheel up 120, …".
func topCounts(counts map[string]int, n int) string {
	type kv struct {
		k string
		v int
	}
	var all []kv
	for k, v := range counts {
		all = append(all, kv{k, v})
	}
	sort.Slice(all, func(i, j int) bool { return all[i].v > all[j].v })
	var parts []string
	for i := 0; i < len(all) && i < n; i++ {
		parts = append(parts, fmt.Sprintf("%s %d", all[i].k, all[i].v))
	}
	return strings.Join(parts, ", ")
}

// writeStall appends every goroutine's stack to stalls.log; the UI
// goroutine's shows the call it is stuck in.
func writeStall(what string, d time.Duration) {
	dir, err := config.GetConfigDir()
	if err != nil {
		return
	}
	path := filepath.Join(dir, "stalls.log")
	if st, err := os.Stat(path); err == nil && st.Size() > stallsLogMax {
		_ = os.Rename(path, path+".old")
	}
	buf := make([]byte, 1<<20)
	buf = buf[:runtime.Stack(buf, true)]
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "==== %s UI STALL: %s running for %s\n%s\n", time.Now().Format("2006-01-02 15:04:05.000"), what, d.Round(time.Millisecond), buf)
}
