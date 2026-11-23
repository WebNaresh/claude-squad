package git

import "testing"

func TestOriginRepoRe(t *testing.T) {
	cases := map[string]string{
		"git@github.com:WebNaresh/claude-squad.git":     "WebNaresh/claude-squad",
		"https://github.com/WebNaresh/claude-squad.git": "WebNaresh/claude-squad",
		"https://github.com/WebNaresh/claude-squad":     "WebNaresh/claude-squad",
		"https://github.com/WebNaresh/claude-squad/":    "WebNaresh/claude-squad",
		"https://tok@github.com/o/practice-stack.git":   "o/practice-stack",
		"ssh://git@github.com/o/my.repo.git":            "o/my.repo",
		"https://gitlab.com/o/r.git":                    "",
	}
	for in, want := range cases {
		got := ""
		if m := originRepoRe.FindStringSubmatch(in); m != nil {
			got = m[1] + "/" + m[2]
		}
		if got != want {
			t.Errorf("%q: got %q, want %q", in, got, want)
		}
	}
}
