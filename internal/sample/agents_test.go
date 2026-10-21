package sample

import (
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// addProc writes one fixture process: its stat, argv and the pane it was
// started in.
func addProc(t *testing.T, root string, pid, ppid int, comm string, argv []string, pane string) {
	t.Helper()
	dir := filepath.Join(root, "proc", strconv.Itoa(pid))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	stat := strconv.Itoa(pid) + " (" + comm + ") S " + strconv.Itoa(ppid) + " 1 1 0 -1 0 0 0 0 0 1 0 0 0 20 0 1 0 1000 1 1"
	files := map[string]string{"stat": stat, "cmdline": strings.Join(argv, "\x00") + "\x00"}
	if pane != "" {
		files["environ"] = "TMUX_PANE=" + pane + "\x00"
	}
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func addPane(t *testing.T, root, line string) {
	t.Helper()
	f, err := os.OpenFile(filepath.Join(root, "tmux-panes.txt"), os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString(line + "\n"); err != nil {
		t.Fatal(err)
	}
}

// TestEveryAgentInAPaneIsASession: each agent is found the way it really
// looks in /proc (checked on a live machine): a native binary by its
// process name, a node agent started by a wrapper by argv[0] (its process
// name is node's "MainThread"), an npm script by the script after the
// interpreter, a Python entry point by its process name.
func TestEveryAgentInAPaneIsASession(t *testing.T) {
	root := copyFixture(t)
	for i, p := range []struct {
		pid  int
		comm string
		argv []string
	}{
		{800, "codex", []string{"/opt/codex/bin/codex"}},
		{810, "opencode", []string{"/home/u/.opencode/bin/opencode"}},
		{820, "MainThread", []string{"/home/u/.local/bin/cursor-agent", "--use-system-ca", "/home/u/.local/share/cursor-agent/index.js"}},
		{830, "MainThread", []string{"node", "/usr/local/bin/gemini"}},
		{840, "aider", []string{"/usr/bin/python3", "/home/u/.local/bin/aider"}},
		{850, "mybot", []string{"mybot", "--fast"}},
		{860, "vim", []string{"vim", "codex"}},             // editing a file named codex
		{870, "MainThread", []string{"node", "server.js"}}, // any other node program
	} {
		pane := "%" + strconv.Itoa(10+i)
		addProc(t, root, p.pid, 1, p.comm, p.argv, pane)
		addPane(t, root, pane+"\tagents:"+strconv.Itoa(i)+".1\tw\t"+strconv.Itoa(p.pid)+"\t"+p.comm+"\t/work")
	}
	// gemini starts itself again as a child with more memory: one session.
	addProc(t, root, 831, 830, "MainThread", []string{"node", "--max-old-space-size=8192", "/usr/local/bin/gemini"}, "%13")

	smp := newFixtureSampler(root)
	smp.Agents = []string{"claude", "codex", "opencode", "cursor-agent", "gemini", "aider", "mybot"}
	snap := smp.Sample()
	got := byPane(snap)
	for pane, name := range map[string]string{"%10": "codex", "%11": "opencode", "%12": "cursor-agent", "%13": "gemini", "%14": "aider", "%15": "mybot"} {
		if got[pane].Name != name {
			t.Errorf("pane %s: name %q, want %q", pane, got[pane].Name, name)
		}
	}
	for _, pane := range []string{"%16", "%17"} {
		if _, ok := got[pane]; ok {
			t.Errorf("pane %s is not an agent: %+v", pane, got[pane])
		}
	}
	if n := len(snap.Sessions); n != 3+6 {
		t.Errorf("%d sessions, want 9", n)
	}
}

// TestCursorsAgentLauncherIsCursorAndItsWorkerIsNot: Cursor's `agent`
// launcher runs node with argv[0] set to the launcher (seen on a live
// machine); its worker-server, also node and also Cursor's index.js,
// outlives `agent -p` and is no session.
func TestCursorsAgentLauncherIsCursorAndItsWorkerIsNot(t *testing.T) {
	const js = "/home/u/.local/share/cursor-agent/versions/2026.10.19-6bbf2c4/index.js"
	root := copyFixture(t)
	for i, p := range []struct {
		pid, ppid int
		argv      []string
	}{
		{900, 1, []string{"/home/u/.local/bin/agent", "--use-system-ca", js, "--trust"}},
		{910, 1, []string{"/home/u/.local/share/cursor-agent/versions/2026.10.19-6bbf2c4/node", js, "worker-server"}},
		{920, 1, []string{"/usr/bin/agent", "serve"}}, // another tool called agent
		// A print run, from a script in a shell's pane: no session.
		{930, 1, []string{"/home/u/.local/bin/agent", "--use-system-ca", js, "-p", "sum up"}},
		{940, 1, []string{"/home/u/.local/bin/cursor-agent", "--use-system-ca", js, "--print", "sum up"}},
	} {
		pane := "%" + strconv.Itoa(20+i)
		addProc(t, root, p.pid, p.ppid, "node", p.argv, pane)
		addPane(t, root, pane+"\tcur:"+strconv.Itoa(i)+".1\tw\t"+strconv.Itoa(p.pid)+"\tnode\t/work")
	}
	smp := newFixtureSampler(root)
	smp.Agents = []string{"claude", "cursor-agent"}
	got := byPane(smp.Sample())
	if s, ok := got["%20"]; !ok || s.Agent != CursorAgent {
		t.Errorf("%%20 = %+v, want a %s session", s, CursorAgent)
	}
	for _, pane := range []string{"%21", "%22", "%23", "%24"} {
		if s, ok := got[pane]; ok {
			t.Errorf("pane %s is not an agent: %+v", pane, s)
		}
	}
	unlisted := newFixtureSampler(root)
	unlisted.Agents = []string{"claude"}
	if s, ok := byPane(unlisted.Sample())["%20"]; ok {
		t.Errorf("cursor-agent is not in agents, yet %%20 is a session: %+v", s)
	}
}

// TestAnAgentOfAnotherTmuxServerTakesNoPaneOfOurs: pane ids repeat across
// tmux servers. An agent started under another server (its TMUX names
// another socket) never takes the pane with its TMUX_PANE in our list.
func TestAnAgentOfAnotherTmuxServerTakesNoPaneOfOurs(t *testing.T) {
	root := copyFixture(t)
	addProc(t, root, 880, 1, "codex", []string{"codex"}, "")
	env := "TMUX=/tmp/tmux-1000/other,4242,0\x00TMUX_PANE=%1\x00"
	if err := os.WriteFile(filepath.Join(root, "proc", "880", "environ"), []byte(env), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(root, "tmux-panes.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, l := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		lines = append(lines, l+"\t/tmp/tmux-1000/default\t4242")
	}
	if err := os.WriteFile(filepath.Join(root, "tmux-panes.txt"), []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	smp := newFixtureSampler(root)
	smp.Agents = []string{"claude", "codex"}
	for _, s := range smp.Sample().Sessions {
		if s.PID == 880 && s.Pane != "" {
			t.Fatalf("the agent of another server took pane %s", s.Pane)
		}
		if s.Pane == "%1" && s.PID != 200 {
			t.Fatalf("pane %%1 belongs to pid 200, got %d", s.PID)
		}
	}
	// An agent of an earlier server at the same socket path (it outlived
	// that server) takes no pane either.
	env = "TMUX=/tmp/tmux-1000/default,1717,0\x00TMUX_PANE=%1\x00"
	if err := os.WriteFile(filepath.Join(root, "proc", "880", "environ"), []byte(env), 0o644); err != nil {
		t.Fatal(err)
	}
	smp = newFixtureSampler(root)
	smp.Agents = []string{"claude", "codex"}
	for _, s := range smp.Sample().Sessions {
		if s.PID == 880 && s.Pane != "" {
			t.Fatalf("the agent of an earlier server took pane %s", s.Pane)
		}
	}
	// The same agent under our server keeps its pane.
	env = "TMUX=/tmp/tmux-1000/default,4242,0\x00TMUX_PANE=%3\x00"
	if err := os.WriteFile(filepath.Join(root, "proc", "880", "environ"), []byte(env), 0o644); err != nil {
		t.Fatal(err)
	}
	smp = newFixtureSampler(root)
	smp.Agents = []string{"claude", "codex"}
	for _, s := range smp.Sample().Sessions {
		if s.PID == 880 && s.Pane != "%3" {
			t.Fatalf("the agent of our server lost its pane: %q", s.Pane)
		}
	}
}

// TestTheStokersServerIsListedWithItsPanesNamed: matchblox keeps the
// stoker agent on a tmux server of its own, and pane ids repeat across
// servers. The private server's panes are listed too, each id carrying
// its server, so its %1 is never the builder's %1.
func TestTheStokersServerIsListedWithItsPanesNamed(t *testing.T) {
	root := copyFixture(t)
	addProc(t, root, 880, 1, "codex", []string{"codex"}, "")
	env := "TMUX=/tmp/tmux-1000/matchblox-test,5151,0\x00TMUX_PANE=%1\x00"
	if err := os.WriteFile(filepath.Join(root, "proc", "880", "environ"), []byte(env), 0o644); err != nil {
		t.Fatal(err)
	}
	smp := newFixtureSampler(root)
	smp.Agents = []string{"claude", "codex"}
	smp.Stoker = "matchblox-test"
	smp.StokerTmux = func() ([]byte, error) {
		return []byte("%1\tstoker:0.0\tcodex\t880\tcodex\t/home/user\t/tmp/tmux-1000/matchblox-test\t5151\n"), nil
	}
	snap := smp.Sample()
	got := byPane(snap)
	if s, ok := got["%1@matchblox-test"]; !ok || s.PID != 880 || s.Target != "stoker:0.0" || s.Server() != "matchblox-test" {
		t.Fatalf("the stoker's agent: got %+v (sessions %v)", s, paneIDs(snap))
	}
	if s := got["%1"]; s.PID != 200 || s.Server() != "" {
		t.Fatalf("the builder's %%1 belongs to pid 200, got %+v", s)
	}
	for _, p := range snap.IdlePanes {
		if strings.Contains(p.Pane, "@") {
			t.Fatalf("a pane of the stoker's server is no idle pane of the builder's: %+v", p)
		}
	}
	if !slices.ContainsFunc(snap.Panes, func(p Pane) bool { return p.ID == "%1@matchblox-test" && p.PID == 880 }) {
		t.Fatalf("the service needs the stoker's panes to tell a caller's pane: %+v", snap.Panes)
	}
}

func paneIDs(snap Snapshot) []string {
	var out []string
	for _, s := range snap.Sessions {
		out = append(out, s.Pane)
	}
	return out
}
