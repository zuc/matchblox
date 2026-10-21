package service

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/blox-eng/matchblox/internal/actions"
	"github.com/blox-eng/matchblox/internal/hooks"
	"github.com/blox-eng/matchblox/internal/proto"
	"github.com/blox-eng/matchblox/internal/sample"
	"github.com/blox-eng/matchblox/internal/state"
	"github.com/blox-eng/matchblox/internal/stoker"
	"github.com/blox-eng/matchblox/internal/tmuxtest"
)

// agentTest is orderTest with no stoker agent running yet, and a tmux
// that knows the stoker's session once new-session made it.
func agentTest(t *testing.T) (*Service, *[][]string) {
	t.Helper()
	s, ran := orderTest(t)
	s.publish(sample.Snapshot{At: night0, Sessions: []sample.Session{
		{Agent: "claude", Pane: "%1", Target: "app:1.1", SessionID: "s1", Name: "work", Status: "idle", Idle: 5 * time.Minute},
	}}, func(state.Doc) {})
	record := s.Actions.Run
	up := false
	s.Actions.Run = func(argv []string) error {
		_ = record(argv)
		switch {
		case slices.Contains(argv, "has-session") && !up:
			return errors.New("can't find session: stoker")
		case slices.Contains(argv, "new-session"):
			up = true
		case slices.Contains(argv, "kill-session"):
			up = false
		}
		return nil
	}
	return s, ran
}

func startAt(t *testing.T, s *Service) {
	t.Helper()
	mustAct(t, s, proto.Act{Console: true, RecID: "stoker:start", Which: "primary"})
}

// TestTheStokerStartsOnItsOwnServer: f with no stoker agent running
// starts one on matchblox's own tmux server, in the builder's home; a
// second f reuses it. The sheet shows it starting until it rests.
func TestTheStokerStartsOnItsOwnServer(t *testing.T) {
	s, ran := agentTest(t)
	startAt(t, s)
	home, _ := os.UserHomeDir()
	want := [][]string{
		{"tmux", "-L", "mbtest", "has-session", "-t", "=stoker"},
		{"env", "-u", "GIT_TERMINAL_PROMPT", "-u", "GIT_SSH_COMMAND", "-u", "MATCHBLOX_HOME",
			"tmux", "-L", "mbtest", "-f", os.DevNull, "new-session", "-d", "-s", "stoker", "-c", home, "claude", ";", "set-option", "-s", "exit-empty", "on"},
	}
	if !slices.EqualFunc(*ran, want, slices.Equal) {
		t.Fatalf("ran %q", *ran)
	}
	v := s.snapshotView()
	if !v.Starting || v.PaneUp || v.Ready || v.Agent != "claude" || v.Start != strings.Join(want[1], " ") {
		t.Fatalf("view %+v", v)
	}
	startAt(t, s) // its session is there: reused, not started twice
	if n := len(*ran); n != 3 || !slices.Contains((*ran)[2], "has-session") {
		t.Fatalf("ran %q", *ran)
	}

	// Up and busy: it is not ready, and an order is not typed into it.
	agent := sample.Session{Agent: "claude", Pane: "%0@mbtest", Target: "stoker:0.0", SessionID: "s0", Name: "stoker", Status: "busy", Busy: true}
	work := sample.Session{Agent: "claude", Pane: "%1", Target: "app:1.1", SessionID: "s1", Name: "work", Status: "idle", Idle: 5 * time.Minute}
	s.publish(sample.Snapshot{At: night0, Sessions: []sample.Session{work, agent}}, func(state.Doc) {})
	if v := s.snapshotView(); !v.PaneUp || v.Ready || !v.Starting {
		t.Fatalf("view %+v", v)
	}
	if res := s.act(t.Context(), orderAct(t, "merge them", true)); res.Err == "" || len(typed(*ran, "%0@mbtest")) != 0 {
		t.Fatalf("an order went into a busy agent: %q, ran %q", res.Err, *ran)
	}
	// At rest: ready, and the order is typed there, on its own server.
	agent.Status, agent.Busy = "idle", false
	s.publish(sample.Snapshot{At: night0, Sessions: []sample.Session{work, agent}}, func(state.Doc) {})
	if v := s.snapshotView(); !v.Ready || v.Starting {
		t.Fatalf("view %+v", v)
	}
	mustAct(t, s, orderAct(t, "merge them", true))
	if got := typed(*ran, "%0@mbtest"); len(got) != 1 || !strings.HasPrefix(got[0], "Read ") {
		t.Fatalf("typed %q", got)
	}
	if len(typed(*ran, "%0")) != 0 {
		t.Fatal("the builder's %0 was typed into")
	}
}

// TestASecondConsoleThatRacesFindsTheAgent: two consoles press f at
// once; tmux starts the session for one and refuses the other's
// new-session. The other finds the agent and reuses it, with no error.
func TestASecondConsoleThatRacesFindsTheAgent(t *testing.T) {
	s, ran := agentTest(t)
	up := false
	s.Actions.Run = func(argv []string) error {
		*ran = append(*ran, argv)
		switch {
		case slices.Contains(argv, "has-session") && !up:
			return errors.New("can't find session: stoker")
		case slices.Contains(argv, "new-session"):
			up = true // the other console's, a moment earlier
			return errors.New("duplicate session: stoker")
		}
		return nil
	}
	startAt(t, s)
	if v := s.snapshotView(); !v.Starting {
		t.Fatalf("view %+v", v)
	}
	if n := len(*ran); n != 3 || !slices.Contains((*ran)[2], "has-session") {
		t.Fatalf("ran %q", *ran)
	}
}

// TestTheStokersServerIsItsOwn: the stoker's server reads no tmux.conf of
// the builder's (no tpm, no resurrect), ends with its last session, and
// keeps none of the service's own variables in the agent's environment.
func TestTheStokersServerIsItsOwn(t *testing.T) {
	home := t.TempDir()
	conf := "set -g @builders yes\nset -s exit-empty off\n"
	if err := os.WriteFile(filepath.Join(home, ".tmux.conf"), []byte(conf), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("MATCHBLOX_HOME", "1")
	server := tmuxtest.Server(t)
	s := &Service{StokerServer: server, StokerAgent: "sleep 600"}
	if err := actions.Exec(t.Context(), s.startArgv()); err != nil {
		t.Fatal(err)
	}
	tmux := func(args ...string) string {
		out, _ := exec.Command("tmux", append([]string{"-L", server}, args...)...).Output() //nolint:gosec // a test's own socket
		return string(out)
	}
	if got := tmux("show-options", "-gqv", "@builders"); got != "" {
		t.Errorf("the builder's tmux.conf loaded: @builders = %q", got)
	}
	if got := strings.TrimSpace(tmux("show-options", "-sv", "exit-empty")); got != "on" {
		t.Errorf("exit-empty = %q", got)
	}
	env := tmux("show-environment", "-g")
	for _, v := range []string{"GIT_TERMINAL_PROMPT", "GIT_SSH_COMMAND", "MATCHBLOX_HOME"} {
		if strings.Contains(env, v+"=") {
			t.Errorf("the agent's environment has %s", v)
		}
	}
	if err := actions.Exec(t.Context(), s.stokerSessionArgv("kill-session")); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("tmux", "-L", server, "list-sessions").Run(); err == nil { //nolint:gosec // a test's own socket
		t.Error("quit left the stoker's server running")
	}
}

// TestOnlyTheBuilderStartsOrQuitsTheStoker: the stoker, a session it
// drives or another agent's pane never starts or ends it.
func TestOnlyTheBuilderStartsOrQuitsTheStoker(t *testing.T) {
	s, ran := agentTest(t)
	for _, a := range []proto.Act{
		{RecID: "stoker:start", From: "%1"},
		{RecID: "stoker:start"},
		{RecID: "stoker:quit", Confirm: "y", From: "%1"},
	} {
		if res := s.act(t.Context(), a); res.Err == "" {
			t.Fatalf("%+v went through", a)
		}
	}
	for _, verb := range []string{"start", "quit"} {
		if res := s.act(t.Context(), fromStokerAct("stoker:"+verb, "")); res.Err != "the stoker does not start, stop or rest itself" {
			t.Fatalf("stoker:%s from the stoker: %q", verb, res.Err)
		}
	}
	if len(*ran) != 0 {
		t.Fatalf("ran %q", *ran)
	}
}

// TestStokerQuitEndsItsSession: matchblox stoker quit (a shell of the
// builder's, outside every agent) and Q with a y end the agent and kill
// its session on its own server; f only turns the stoker off.
func TestStokerQuitEndsItsSession(t *testing.T) {
	s, ran := agentTest(t)
	startAt(t, s)
	mustAct(t, s, proto.Act{Console: true, RecID: "stoker:off"})
	if slices.ContainsFunc(*ran, func(argv []string) bool { return slices.Contains(argv, "kill-session") }) {
		t.Fatalf("f killed the agent: %q", *ran)
	}
	if res := s.act(t.Context(), proto.Act{Console: true, RecID: "stoker:quit"}); res.Err != "needs confirm" {
		t.Fatalf("quit without y: %q", res.Err)
	}
	*ran = nil
	mustAct(t, s, proto.Act{RecID: "stoker:quit", Confirm: "y"})
	want := []string{"tmux", "-L", "mbtest", "kill-session", "-t", "=stoker"}
	if len(*ran) != 2 || !slices.Equal((*ran)[1], want) {
		t.Fatalf("ran %q", *ran)
	}
	if v := s.snapshotView(); v.Starting {
		t.Fatalf("view %+v", v)
	}
	if res := s.act(t.Context(), proto.Act{Console: true, RecID: "stoker:quit", Confirm: "y"}); res.Err != "the stoker is not running" {
		t.Fatalf("a second quit: %q", res.Err)
	}
}

// TestAnAgentNotOnPathIsADoor: the agent's command is not on PATH: the
// sheet's door names it and how to set another, and nothing starts.
func TestAnAgentNotOnPathIsADoor(t *testing.T) {
	s, ran := agentTest(t)
	s.StokerAgent = "codex"
	s.LookPath = func(string) (string, error) { return "", errors.New("not found") }
	res := s.act(t.Context(), proto.Act{Console: true, RecID: "stoker:start"})
	want := `codex is not on PATH: install it, or name the stoker's agent in config.toml, [stoker] agent = "codex" (or opencode, cursor-agent)`
	if res.Err != want || len(*ran) != 0 {
		t.Fatalf("err %q ran %q", res.Err, *ran)
	}
	s.refreshStoker()
	if v := s.snapshotView(); v.Missing != want {
		t.Fatalf("view %+v", v)
	}
}

// TestPaneIDsOfTwoServersAreNotOne: the stoker's %9 and the builder's %9
// are two panes. The stoker's hook names its pane by $TMUX_PANE alone;
// its socket tells the service it is the stoker's, so a merge the guard
// denied there never asks the builder's go for their own %9.
func TestPaneIDsOfTwoServersAreNotOne(t *testing.T) {
	s, _, _ := guardedRun(t)
	s.drive("%9") // the builder's %9, driven by the stoker
	s.PeerPane = func(pid int) string { return map[int]string{4242: "%9@mbtest", 900: "%9"}[pid] }
	sock := s.stokerSocket()
	before := len(s.Stoker.Entries())
	hookOver(t, s, 4242, hooks.Event{Name: "PreToolUse", SessionID: "s9", Pane: "%9", Socket: sock, At: night0,
		Command: "gh pr merge 7", Guards: stoker.Guards("gh pr merge 7"), Denied: true})
	if es := s.Stoker.Entries(); len(es) != before {
		t.Fatalf("the stoker's denied merge asked a go for the builder's %%9: %+v", es[before:])
	}
	// The builder's own %9 still asks.
	hookOver(t, s, 900, hooks.Event{Name: "PreToolUse", SessionID: "s99", Pane: "%9", Socket: "/tmp/tmux-test/default", At: night0,
		Command: "gh pr merge 7", Guards: stoker.Guards("gh pr merge 7"), Denied: true})
	if es := s.Stoker.Entries(); len(es) != before+1 || es[before].Pane != "%9" {
		t.Fatalf("the builder's %%9 asked nothing: %+v", es[before:])
	}
	// The caller's identity: a CLI in the stoker's %9 is the stoker; one
	// in the builder's %9 is not.
	if !s.fromStoker(proto.Act{From: s.PeerPane(4242)}) || s.fromStoker(proto.Act{From: s.PeerPane(900)}) {
		t.Fatal("the peer's pane did not tell the two servers apart")
	}
}
