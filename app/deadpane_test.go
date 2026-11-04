package app

import (
	"reflect"
	"testing"
)

func TestCompactDead(t *testing.T) {
	all := []string{"error: gh CLI not authenticated", "", "", "", "", "Pane is dead (status 1)", "", ""}
	want := []string{"error: gh CLI not authenticated", "", "Pane is dead (status 1)"}
	if got := compactDead(all, 29); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
	if got := compactDead(all, 1); !reflect.DeepEqual(got, []string{"Pane is dead (status 1)"}) {
		t.Fatalf("h=1: got %q", got)
	}
	if got := compactDead([]string{"", "Pane is dead"}, 5); !reflect.DeepEqual(got, []string{"Pane is dead"}) {
		t.Fatalf("only dead line: got %q", got)
	}
}
