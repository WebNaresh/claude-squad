package app

import (
	"strings"
	"testing"
	"time"

	"claude-squad/session/git"
)

// The merge question comes up by itself after a refused push, so it must not
// open over typing, and conflicts are never offered.
func TestOfferMerge(t *testing.T) {
	in := git.Incoming{SyncState: git.SyncState{Upstream: "origin/main", Ahead: 15, Behind: 2}, Authors: []string{"Tess"}}
	start := time.Now()

	m, root := autoHome(t, 0, 0, 0)
	m.lastKey = start.Add(-time.Second)
	m.offerMerge(root, in, true, start)
	if m.state != stateConfirm {
		t.Fatal("not asked")
	}
	if v := strings.Join(strings.Fields(m.confirmationOverlay.Render()), " "); !strings.Contains(v, "Tess") {
		t.Fatalf("question doesn't say who: %s", v)
	}
	m.confirmationOverlay.OnConfirm()
	if msg, ok := m.confirmResult.(mergeStartMsg); !ok || !msg.push || msg.root != root {
		t.Fatalf("yes gave %#v", m.confirmResult)
	}

	for name, set := range map[string]func(*home, *git.Incoming){
		"typed since": func(m *home, _ *git.Incoming) { m.lastKey = start.Add(time.Second) },
		"dialog open": func(m *home, _ *git.Incoming) { m.state = stateHelp },
		"conflicts":   func(_ *home, in *git.Incoming) { in.Conflicts = []string{"a.go"} },
	} {
		m, root := autoHome(t, 0, 0, 0)
		in := in
		set(m, &in)
		was := m.state
		m.offerMerge(root, in, true, start)
		if m.state != was {
			t.Errorf("%s: asked anyway", name)
		}
	}
}
