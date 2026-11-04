package session

import "testing"

func TestChoosePR(t *testing.T) {
	teammate := PRInfo{Number: 766, Author: "teammate", Head: "feat/763", Closes: []int{763}}
	mine := PRInfo{Number: 779, Author: "me", Head: "dev"}
	mine2 := PRInfo{Number: 780, Author: "Me", Head: "feat/other"}
	bot := PRInfo{Number: 800, Author: "app/claude", Head: "dev", Bot: true, Closes: []int{1, 2, 3}}
	cases := []struct {
		name          string
		open          []PRInfo
		login, branch string
		pr            int
		unclear       bool
	}{
		{"teammate's PR holds an issue, mine from dev holds none", []PRInfo{teammate, mine}, "me", "dev", 779, false},
		{"two of mine, neither from this branch: ask", []PRInfo{mine, mine2}, "me", "main", 0, true},
		{"a teammate's PR on my branch: ask", []PRInfo{teammate, mine}, "me", "feat/763", 0, true},
		{"two of mine on one branch: ask", []PRInfo{mine, {Number: 781, Author: "me", Head: "dev"}}, "me", "dev", 0, true},
		{"login match ignores case", []PRInfo{teammate, mine2}, "me", "feat/other", 780, false},
		{"lone PR of mine, any branch", []PRInfo{mine}, "me", "main", 779, false},
		{"lone teammate PR: ask", []PRInfo{teammate}, "me", "feat/763", 0, true},
		{"login unknown: ask", []PRInfo{teammate, mine}, "", "dev", 0, true},
		{"detached HEAD: ask", []PRInfo{teammate, mine}, "me", "", 0, true},
		{"bot PR is ignored, not counted", []PRInfo{bot, mine}, "me", "main", 779, false},
		{"only a bot's PR: a new batch", []PRInfo{bot}, "me", "dev", 0, false},
		{"nothing open: a new batch", nil, "me", "dev", 0, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pr, unclear := ChoosePR(c.open, c.login, c.branch)
			if pr != c.pr || unclear != c.unclear {
				t.Errorf("ChoosePR = %d, %v; want %d, %v", pr, unclear, c.pr, c.unclear)
			}
		})
	}
}
