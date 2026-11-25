package session

import (
	"slices"
	"testing"
)

func TestPeopleIn(t *testing.T) {
	for _, c := range []struct {
		body string
		want []string
	}{
		{"x\n---\n> **Reported by:** CS Asha Rao (a@b.in) 📞 +911 • ID: `c1` • **Created:** 2026", []string{"CS Asha Rao"}},
		{"> **Reported by:** CA R K - Partner (x@y.in) • **Created:** 2026\r\n", []string{"CA R K - Partner"}},
		{"> **Reported by:** Suresh J 📞 +918 • ID: `q`", []string{"Suresh J"}},
		{"> **Reported by:** Tester • **Created:** 2026", []string{"Tester"}},
		{"> **Reported by:** Anonymous tester", nil},
		{"- [ ] 1. A thing (reported by Unknown)\n- [x] 2. Done (reported by Gone)\n- [ ] 3. [B](http://x) (reported by Asha Rao)\n- [ ] 4. C (reported by ASHA  RAO)\n\n> **Reported by:** Bina (b@c.in)", []string{"Bina", "Asha Rao"}},
		{"> **Reported by:** Old\n\n> **Reported by:** New (n@m.in)", []string{"New"}},
		{"Raised by **Client (0)** on the call at 39:38.", []string{"Client on a call"}},
		{"Filed from a GlitchRecord idea chat by Bina K, 2026-11-21. Open", []string{"Bina K"}},
		{"> **Reported by:** Evil\x1b[31m Name (e@f.in)", []string{"Evil [31m Name"}},
		{"He said it was reported by Asha", nil},
	} {
		if got := peopleIn(c.body); !slices.Equal(got, c.want) {
			t.Errorf("%q: %q, want %q", c.body, got, c.want)
		}
	}
}

func TestSetPeopleFallsBackToAuthor(t *testing.T) {
	var is Issue
	is.Author.Login = "mylogin"
	is.setPeople("mylogin", "My Name")
	if !slices.Equal(is.People, []string{"My Name"}) {
		t.Fatalf("own issue: %q", is.People)
	}
	is.Author.Login = "app/glitchgrab-app"
	is.setPeople("mylogin", "My Name")
	if len(is.People) != 0 {
		t.Fatalf("bot issue: %q", is.People)
	}
}

func TestByPriority(t *testing.T) {
	list := []Issue{{Number: 1}, {Number: 2, People: []string{"Me"}}, {Number: 3, People: []string{"Asha"}},
		{Number: 4, People: []string{"Bina"}}, {Number: 5, People: []string{"me", "Asha"}}}
	var got []int
	for _, is := range ByPriority(list, "ME", []string{"bina"}) {
		got = append(got, is.Number)
	}
	if !slices.Equal(got, []int{4, 3, 5, 1, 2}) {
		t.Fatalf("order %v", got)
	}
}
