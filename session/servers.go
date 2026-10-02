package session

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
)

// Server is a process listening on a TCP port, and the tmux session (cs
// tile) it was started from, found by walking its parent processes up to a
// tmux pane.
type Server struct {
	Port    int
	PID     int
	Command string // short form of its command line ("next dev --port 3333")
	Cwd     string
	Tmux    string // tmux session it runs in, or "" when started outside tmux
	// ByClaude: Claude started it (its Bash tool), so Ctrl+C in the tile
	// would interrupt Claude instead; it is stopped with SIGTERM.
	ByClaude bool
	// Root is the top process of the server's chain below its tile, Claude
	// or the system (bun run dev → next dev → next-server); stopping it
	// stops the whole chain.
	Root int
}

// ListServers returns the listening TCP ports of the user's processes.
func ListServers() ([]Server, error) {
	out, err := exec.Command("lsof", "-nP", "-iTCP", "-sTCP:LISTEN", "-F", "pn").Output()
	if err != nil && len(out) == 0 {
		return nil, nil // lsof exits 1 when nothing listens
	}
	seen := map[[2]int]bool{}
	var servers []Server
	pid := 0
	for _, line := range strings.Split(string(out), "\n") {
		if len(line) < 2 {
			continue
		}
		switch line[0] {
		case 'p':
			pid, _ = strconv.Atoi(line[1:])
		case 'n':
			i := strings.LastIndex(line, ":")
			port, _ := strconv.Atoi(line[i+1:])
			if port == 0 || pid == 0 || seen[[2]int{pid, port}] {
				continue
			}
			seen[[2]int{pid, port}] = true
			servers = append(servers, Server{Port: port, PID: pid})
		}
	}
	if len(servers) == 0 {
		return nil, nil
	}

	// Which tmux pane each process descends from.
	panes := map[int]string{}
	if out, err := exec.Command("tmux", "list-panes", "-a", "-F", "#{pane_pid} #{session_name}").Output(); err == nil {
		for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if p, name, ok := strings.Cut(l, " "); ok {
				n, _ := strconv.Atoi(p)
				panes[n] = name
			}
		}
	}
	ppid, comm := processTable()
	pids := make([]string, 0, len(servers))
	for i := range servers {
		s := &servers[i]
		s.Root = s.PID
		top, orphan := s.PID, true
		for p, n := s.PID, 0; p > 1 && n < 40; p, n = ppid[p], n+1 {
			if p != s.PID && strings.Contains(strings.ToLower(filepath.Base(comm[p])), "claude") {
				s.ByClaude = true
			}
			if name, ok := panes[p]; ok {
				s.Tmux = name
				break
			}
			if interactive(comm[p]) {
				orphan = false // under a terminal the user types in
			}
			top = p
		}
		// Left running by a shell that is gone (its top process's parent is
		// launchd): the whole chain is the server's, so stop it from the top.
		if s.Tmux == "" && !s.ByClaude && orphan && ppid[top] == 1 {
			s.Root = top
		}
		pids = append(pids, strconv.Itoa(s.PID))
	}

	// Command lines and working folders, one call each for all of them.
	args := map[int]string{}
	if out, err := exec.Command("ps", "-o", "pid=,args=", "-p", strings.Join(pids, ",")).Output(); err == nil {
		for _, l := range strings.Split(string(out), "\n") {
			f := strings.Fields(l)
			if len(f) >= 2 {
				n, _ := strconv.Atoi(f[0])
				args[n] = shortCommand(f[1:])
			}
		}
	}
	cwds := map[int]string{}
	if out, err := exec.Command("lsof", "-a", "-d", "cwd", "-F", "pn", "-p", strings.Join(pids, ",")).Output(); err == nil || len(out) > 0 {
		p := 0
		for _, l := range strings.Split(string(out), "\n") {
			if len(l) < 2 {
				continue
			}
			switch l[0] {
			case 'p':
				p, _ = strconv.Atoi(l[1:])
			case 'n':
				cwds[p] = l[1:]
			}
		}
	}
	kept := servers[:0]
	for _, sv := range servers {
		sv.Command, sv.Cwd = args[sv.PID], cwds[sv.PID]
		// Claude's own tool servers (MCP) aren't the user's servers.
		if sv.ByClaude && strings.Contains(strings.ToLower(sv.Command), "mcp") {
			continue
		}
		kept = append(kept, sv)
	}
	servers = kept
	sort.Slice(servers, func(i, j int) bool { return servers[i].Port < servers[j].Port })
	return servers, nil
}

// processTable returns every process's parent and executable, from one ps call.
func processTable() (ppid map[int]int, comm map[int]string) {
	ppid, comm = map[int]int{}, map[int]string{}
	out, err := exec.Command("ps", "-axo", "pid=,ppid=,comm=").Output()
	if err != nil {
		return
	}
	for _, l := range strings.Split(string(out), "\n") {
		f := strings.Fields(l)
		if len(f) >= 3 {
			p, _ := strconv.Atoi(f[0])
			pp, _ := strconv.Atoi(f[1])
			ppid[p], comm[p] = pp, strings.Join(f[2:], " ")
		}
	}
	return
}

// interactive reports a process a user types in or a terminal app, which a
// server's stop must never reach.
func interactive(comm string) bool {
	base := strings.TrimPrefix(filepath.Base(comm), "-")
	switch base {
	case "zsh", "fish", "login", "tmux", "sshd", "screen":
		return true
	}
	return strings.Contains(comm, ".app/")
}

// shortCommand drops folders from a command line and keeps a few words:
// "/Users/x/.bun/bin/node /repo/node_modules/.bin/next dev" -> "node next dev".
func shortCommand(words []string) string {
	var out []string
	for _, w := range words {
		if strings.Contains(w, "/") && !strings.HasPrefix(w, "-") {
			w = filepath.Base(w)
		}
		out = append(out, w)
		if len(out) == 4 {
			break
		}
	}
	return strings.Join(out, " ")
}

// StopServer stops a server the way the user would: Ctrl+C in the tmux
// session it runs in. Started by Claude or outside any tile, its chain gets
// SIGTERM from the top (Root), never Claude or a shell the user types in.
func StopServer(s Server) error {
	if s.Tmux != "" && !s.ByClaude {
		return exec.Command("tmux", "send-keys", "-t", s.Tmux, "C-c").Run()
	}
	root := s.Root
	if s.Tmux != "" || s.ByClaude {
		root = s.PID // the chain above may be Claude's own shell or the tile's
	}
	_ = syscall.Kill(root, syscall.SIGTERM)
	if err := syscall.Kill(s.PID, syscall.SIGTERM); err != nil && root == s.PID {
		return fmt.Errorf("could not stop :%d (pid %d): %w", s.Port, s.PID, err)
	}
	return nil
}
