package app

import (
	"testing"
	"time"
)

func TestRunAge(t *testing.T) {
	now := time.Now()
	for _, c := range []struct {
		ago  time.Duration
		want string
	}{
		{10 * time.Second, "<1m"},
		{12 * time.Minute, "12m"},
		{65 * time.Minute, "1h 5m"},
		{51 * time.Hour, "2d 3h"},
	} {
		if got := runAge(now.Add(-c.ago)); got != c.want {
			t.Errorf("runAge(-%v) = %q, want %q", c.ago, got, c.want)
		}
	}
	if got := runAge(time.Time{}); got != "" {
		t.Errorf("runAge(zero) = %q, want empty", got)
	}
}
