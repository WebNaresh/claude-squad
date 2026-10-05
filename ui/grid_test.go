package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestCutKeepingIssue(t *testing.T) {
	got := cutKeepingIssue("▶ 🟢 bug-title-i-need-help-#2268", 20)
	if !strings.HasSuffix(got, "…#2268") || ansi.StringWidth(got) > 20 {
		t.Fatalf("got %q (%d cells)", got, ansi.StringWidth(got))
	}
	if got := cutKeepingIssue("short-#1", 20); got != "short-#1" {
		t.Fatalf("short title changed: %q", got)
	}
}
