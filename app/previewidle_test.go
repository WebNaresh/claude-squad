package app

import (
	"testing"
	"time"
)

// An idle cs redrew every 400ms all day (~11% CPU); after previewIdleAfter it
// ticks once a second, and a key wakes the sleeping tick at once.
func TestPreviewIntervalSlowsWhenIdle(t *testing.T) {
	m := &home{}
	cases := []struct {
		ago  time.Duration
		want time.Duration
	}{
		{100 * time.Millisecond, 50 * time.Millisecond},
		{2 * time.Second, 100 * time.Millisecond},
		{5 * time.Second, 400 * time.Millisecond},
		{previewIdleAfter + time.Second, time.Second},
		{time.Hour, time.Second},
	}
	for _, c := range cases {
		m.lastKey = time.Now().Add(-c.ago)
		if got := m.previewInterval(); got != c.want {
			t.Errorf("%s after the last key: interval %s, want %s", c.ago, got, c.want)
		}
	}
}

func TestMarkActiveWakesIdleTick(t *testing.T) {
	m := &home{previewWake: make(chan struct{}, 1)}
	m.lastKey = time.Now().Add(-time.Hour)

	done := make(chan time.Duration)
	go func() {
		start := time.Now()
		previewWait(m.previewInterval(), m.previewWake)
		done <- time.Since(start)
	}()
	time.Sleep(20 * time.Millisecond)
	m.markActive()
	m.markActive() // a second wake must not block

	select {
	case waited := <-done:
		if waited > 500*time.Millisecond {
			t.Fatalf("tick waited %s after a key; want it woken at once", waited)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("tick never woke after markActive")
	}
	if time.Since(m.lastKey) > time.Second {
		t.Fatal("markActive did not record the key time")
	}
	if got := m.previewInterval(); got != 50*time.Millisecond {
		t.Fatalf("interval right after a key = %s, want 50ms", got)
	}
}

// A trackpad burst (hundreds of wheel events) must wake the tick once, not
// queue a capture per event; while active nothing wakes it at all.
func TestMarkActiveBurstWakesOnce(t *testing.T) {
	m := &home{previewWake: make(chan struct{}, 1)}
	m.lastKey = time.Now().Add(-time.Hour)
	for i := 0; i < 300; i++ {
		m.markActive()
	}
	if n := len(m.previewWake); n != 1 {
		t.Fatalf("after a burst from idle: %d wakes pending, want 1", n)
	}
	<-m.previewWake
	m.markActive() // still active: the tick runs at 50ms anyway
	if n := len(m.previewWake); n != 0 {
		t.Fatalf("event while active queued %d wakes, want 0", n)
	}
}

// Tests build home without the channel; markActive must not block then.
func TestMarkActiveWithoutChannel(t *testing.T) {
	m := &home{}
	m.markActive()
}
