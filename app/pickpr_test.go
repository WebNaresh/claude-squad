package app

import (
	"testing"
)

func TestPRRowParsing(t *testing.T) {
	rows := []string{"  ▶ #2305: fix(mcp): accept …", "    #2299: fix(services) [1 issue]", "    #2209: ### Report [13 issues]"}
	cursor, at := -1, -1
	for i, l := range rows {
		mm := prRowRe.FindStringSubmatch(l)
		if mm == nil {
			t.Fatalf("row %q not matched", l)
		}
		if mm[1] != "" {
			cursor = i
		}
		if mm[2] == "2209" {
			at = i
		}
	}
	if cursor != 0 || at != 2 {
		t.Errorf("cursor=%d at=%d, want 0 and 2", cursor, at)
	}
}
