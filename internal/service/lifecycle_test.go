package service

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/blox-eng/matchblox/internal/holds"
	"github.com/blox-eng/matchblox/internal/proto"
	"github.com/blox-eng/matchblox/internal/sample"
	"github.com/blox-eng/matchblox/internal/state"
	"github.com/blox-eng/matchblox/internal/stoker"
)

// lifeSessions are the builder's: work at rest in %1 (tmux session app,
// its only window), busy in %2 (session dev, one of two windows), a Codex
// one in %3.
func lifeSnap(busy bool) sample.Snapshot {
	return sample.Snapshot{At: night0, Sessions: []sample.Session{
		{Agent: "claude", PID: 101, Start: 7, Pane: "%1", Target: "app:1.1", SessionID: "s1", Name: "work", Status: "idle", Idle: 5 * time.Minute, Busy: busy},
		{Agent: "claude", PID: 102, Start: 7, Pane: "%2", Target: "dev:1.1", SessionID: "s2", Name: "busy", Status: "busy", Busy: true},
		{Agent: "codex", PID: 103, Start: 7, Pane: "%3", Target: "cx:1.1", SessionID: "s3", Name: "cx", Status: "idle", Idle: time.Hour},
	}, Panes: []sample.Pane{
		{ID: "%1", Target: "app:1.1"},
		{ID: "%2", Target: "dev:1.1"}, {ID: "%4", Target: "dev:2.1"},
		{ID: "%3", Target: "cx:1.1"},
		{ID: "%9@mbtest", Target: "stoker:1.1"},
	}}
}

// lifeTest is a service with the builder's sessions above, the steps it
// runs recorded, and the pane of a session it starts named by tmux.
func lifeTest(t *testing.T) (*Service, *[][]string) {
	t.Helper()
	s := newTest(t, time.Hour)
	s.StokerServer = "mbtest"
	s.LookPath = func(cmd string) (string, error) {
		if cmd == "opencode" {
			return "", exec.ErrNotFound
		}
		return "/usr/bin/" + cmd, nil
	}
	var mu sync.Mutex
	var ran [][]string
	s.Actions.Run = func(argv []string) error {
		mu.Lock()
		defer mu.Unlock()
		ran = append(ran, argv)
		return nil
	}
	s.Capture = func([]string) map[string]string { return map[string]string{"%1": idleScreen, "%3": idleScreen} }
	s.TmuxOut = func(argv []string) (string, error) { return "%42\n", nil }
	s.publish(lifeSnap(false), func(state.Doc) {})
	return s, &ran
}

func consoleAct(id, text string) proto.Act {
	return proto.Act{RecID: id, Confirm: "y", Text: text, Console: true}
}

func TestKillEndsTheAgentAndItsPane(t *testing.T) {
	s, ran := lifeTest(t)
	res := s.act(t.Context(), consoleAct("kill:%1", ""))
	want := [][]string{{"tmux", "kill-pane", "-t", "%1"}, {"kill", "-TERM", "101"}}
	if res.Err != "" || !slices.EqualFunc(*ran, want, slices.Equal) {
		t.Fatalf("err %q ran %q", res.Err, *ran)
	}
}

func TestKillNeedsTheTypedY(t *testing.T) {
	s, ran := lifeTest(t)
	a := consoleAct("kill:%1", "")
	a.Confirm = ""
	if res := s.act(t.Context(), a); res.Err != "needs confirm" || len(*ran) != 0 {
		t.Fatalf("err %q ran %q", res.Err, *ran)
	}
}

func TestKillAsksAgainWhileItWorks(t *testing.T) {
	s, ran := lifeTest(t)
	res := s.act(t.Context(), consoleAct("kill:%2", ""))
	if res.Err != "busy works: y again kills it mid-turn" || len(*ran) != 0 {
		t.Fatalf("err %q ran %q", res.Err, *ran)
	}
	if res := s.act(t.Context(), consoleAct("kill:%2", KillAnyway)); res.Err != "" || len(*ran) != 2 {
		t.Fatalf("err %q ran %q", res.Err, *ran)
	}
}

func TestKillNeverTheStokersSession(t *testing.T) {
	s, ran := lifeTest(t)
	snap := lifeSnap(false)
	snap.Sessions = append(snap.Sessions, sample.Session{Agent: "claude", PID: 109, Pane: "%9@mbtest", Name: "stoker", Status: "idle"})
	s.publish(snap, func(state.Doc) {})
	res := s.act(t.Context(), consoleAct("kill:%9@mbtest", KillAnyway))
	if res.Err != "the stoker is not killed here: Q on the STOKER tab quits it" || len(*ran) != 0 {
		t.Fatalf("err %q ran %q", res.Err, *ran)
	}
}

func TestKillAGoneSession(t *testing.T) {
	s, _ := lifeTest(t)
	if res := s.act(t.Context(), consoleAct("kill:%77", "")); res.Err != "no agent session in %77 any more" {
		t.Fatal(res.Err)
	}
}

func TestRenameNamesTmuxAndTheAgentAlike(t *testing.T) {
	s, ran := lifeTest(t)
	res := s.act(t.Context(), consoleAct("rename:%1", "ships-flake"))
	// The agent's /rename goes first: a guard that stops it leaves tmux
	// as it was, so the two names never part.
	want := [][]string{
		{"tmux", "send-keys", "-t", "%1", "-l", "--", "/rename ships-flake"},
		{"tmux", "send-keys", "-t", "%1", "Enter"},
		{"tmux", "rename-window", "-t", "%1", "ships-flake"},
		{"tmux", "rename-session", "-t", "%1", "ships-flake"},
	}
	if res.Err != "" || !slices.EqualFunc(*ran, want, slices.Equal) {
		t.Fatalf("err %q ran %q", res.Err, *ran)
	}
}

func TestRenameKeepsASessionOfManyWindows(t *testing.T) {
	s, ran := lifeTest(t)
	snap := lifeSnap(false)
	snap.Sessions[1].Busy, snap.Sessions[1].Status = false, "idle"
	s.publish(snap, func(state.Doc) {})
	s.Capture = func([]string) map[string]string { return map[string]string{"%2": idleScreen} }
	if res := s.act(t.Context(), consoleAct("rename:%2", "two")); res.Err != "" {
		t.Fatal(res.Err)
	}
	for _, step := range *ran {
		if slices.Contains(step, "rename-session") {
			t.Fatalf("renamed dev, which holds two windows: %q", *ran)
		}
	}
}

func TestRenameRefusesABadOrTakenName(t *testing.T) {
	s, ran := lifeTest(t)
	for name, why := range map[string]string{
		"a.b": "a name is letters, digits, - and _, and starts with a letter or a digit",
		"dev": "a tmux session is named dev already: choose another name",
	} {
		if res := s.act(t.Context(), consoleAct("rename:%1", name)); res.Err != why {
			t.Errorf("%s: %q", name, res.Err)
		}
	}
	if len(*ran) != 0 {
		t.Fatalf("ran %q", *ran)
	}
}

func TestRenameOfABusySessionWaitsForItsRest(t *testing.T) {
	s, ran := lifeTest(t)
	s.publish(lifeSnap(true), func(state.Doc) {})
	res := s.act(t.Context(), consoleAct("rename:%1", "later"))
	if res.Err != "" || len(*ran) != 0 || !slices.Equal(res.Skipped, []string{"work renames when it rests"}) {
		t.Fatalf("err %q skipped %q ran %q", res.Err, res.Skipped, *ran)
	}
	s.renameAtRest(t.Context()) // still busy
	if len(*ran) != 0 {
		t.Fatalf("ran while busy: %q", *ran)
	}
	s.publish(lifeSnap(false), func(state.Doc) {})
	s.renameAtRest(t.Context())
	if len(*ran) != 4 || (*ran)[2][len((*ran)[2])-1] != "later" {
		t.Fatalf("ran %q", *ran)
	}
	s.renameAtRest(t.Context())
	if len(*ran) != 4 {
		t.Fatalf("renamed twice: %q", *ran)
	}
}

func TestRenameOfCodexIsTmuxOnly(t *testing.T) {
	s, ran := lifeTest(t)
	if res := s.act(t.Context(), consoleAct("rename:%3", "cx2")); res.Err != "" {
		t.Fatal(res.Err)
	}
	for _, step := range *ran {
		if slices.Contains(step, "send-keys") {
			t.Fatalf("typed into Codex: %q", *ran)
		}
	}
	if len(*ran) != 2 {
		t.Fatalf("ran %q", *ran)
	}
}

func newAct(t *testing.T, n proto.NewSession) proto.Act {
	t.Helper()
	b, err := json.Marshal(n)
	if err != nil {
		t.Fatal(err)
	}
	return consoleAct("new:session", string(b))
}

func TestNewStartsTheAgentInASessionOfItsName(t *testing.T) {
	s, ran := lifeTest(t)
	dir := t.TempDir()
	res := s.act(t.Context(), newAct(t, proto.NewSession{Where: dir, Agent: "claude", Name: "fresh", First: "fix the flaky test"}))
	if res.Err != "" || len(*ran) != 1 {
		t.Fatalf("err %q ran %q", res.Err, *ran)
	}
	got := strings.Join((*ran)[0], " ")
	want := "tmux new-session -d -s fresh -n fresh -c " + dir + " claude --name fresh fix the flaky test"
	if !strings.HasSuffix(got, want) || !strings.HasPrefix(got, "env -u GIT_TERMINAL_PROMPT") || slices.Contains((*ran)[0], "-L") {
		t.Fatalf("ran %q", got)
	}
}

func TestNewNamesItAfterItsDirectory(t *testing.T) {
	s, ran := lifeTest(t)
	dir := filepath.Join(t.TempDir(), "blox-api")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if res := s.act(t.Context(), newAct(t, proto.NewSession{Where: dir, Agent: "codex"})); res.Err != "" {
		t.Fatal(res.Err)
	}
	if got := strings.Join((*ran)[0], " "); !strings.HasSuffix(got, "-s blox-api -n blox-api -c "+dir+" codex") {
		t.Fatalf("ran %q", got)
	}
}

func TestNewAddsAWorktreeForABranch(t *testing.T) {
	s, ran := lifeTest(t)
	// git names the repository by its real path: macOS's temp dir is a link.
	tmp, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(tmp, "blox-api")
	for _, c := range [][]string{{"init", "-q", repo}, {"-C", repo, "-c", "user.name=t", "-c", "user.email=t@t", "commit", "-q", "--allow-empty", "-m", "x"}} {
		if out, err := exec.Command("git", c...).CombinedOutput(); err != nil {
			t.Fatalf("%s: %s", err, out)
		}
	}
	res := s.act(t.Context(), newAct(t, proto.NewSession{Where: repo, Branch: "feat/88-x", Agent: "claude"}))
	if res.Err != "" || len(*ran) != 2 {
		t.Fatalf("err %q ran %q", res.Err, *ran)
	}
	wt := filepath.Join(filepath.Dir(repo), "blox-api-feat-88-x")
	if got, want := strings.Join((*ran)[0], " "), "git -C "+repo+" worktree add --no-track -b feat/88-x "+wt+" HEAD"; got != want {
		t.Fatalf("ran %q, want %q", got, want)
	}
	if got := strings.Join((*ran)[1], " "); !strings.HasSuffix(got, "-s blox-api-feat-88-x -n blox-api-feat-88-x -c "+wt+" claude --name blox-api-feat-88-x") {
		t.Fatalf("ran %q", got)
	}
}

func TestNewRefuses(t *testing.T) {
	s, ran := lifeTest(t)
	dir := t.TempDir()
	for why, n := range map[string]proto.NewSession{
		"a tmux session is named dev already: choose another name":                        {Where: dir, Agent: "claude", Name: "dev"},
		"opencode is not installed here":                                                  {Where: dir, Agent: "opencode", Name: "x"},
		"aider is not an agent matchblox starts: claude, codex, opencode or cursor-agent": {Where: dir, Agent: "aider", Name: "x"},
		"no directory " + dir + "/nope":                                                   {Where: dir + "/nope", Agent: "claude", Name: "x"},
		"a new session starts in a full path":                                             {Where: "rel/dir", Agent: "claude", Name: "x"},
		"a new worktree needs a git repository: " + dir + " is none":                      {Where: dir, Branch: "b", Agent: "claude"},
		"a first message does not start with -":                                           {Where: dir, Agent: "claude", Name: "x", First: "--dangerously-skip-permissions"},
		"the first message must be one line, without control keys":                        {Where: dir, Agent: "claude", Name: "x", First: "a\nb"},
	} {
		if res := s.act(t.Context(), newAct(t, n)); res.Err != why {
			t.Errorf("want %q, got %q", why, res.Err)
		}
	}
	if len(*ran) != 0 {
		t.Fatalf("ran %q", *ran)
	}
}

// The stoker's hands: kill waits for the builder's go unless the order
// gave kills, and is refused on a session that works, waits or is parked.

func handsLife(t *testing.T) (*Service, *[][]string) {
	t.Helper()
	s, ran := handsTest(t)
	s.TmuxOut = func([]string) (string, error) { return "%42\n", nil }
	hs, err := holds.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.Holds = hs
	return s, ran
}

func TestStokerKillWaitsForTheBuildersGo(t *testing.T) {
	s, ran := handsLife(t)
	res := s.act(t.Context(), fromStokerAct("kill:%1", ""))
	if res.Err != "the kill waits for the builder's go: it is on the card, y gives it" || len(*ran) != 0 {
		t.Fatalf("err %q ran %q", res.Err, *ran)
	}
	run := s.Stoker.View().Run
	e := run[len(run)-1]
	if e.Rule != stoker.RuleGo || e.State != stoker.StepWaits || e.Needs != "kill:%1" || e.Text != "stoker wants to kill work" {
		t.Fatalf("card %+v", e)
	}
	// Asked twice, the card asks once.
	s.act(t.Context(), fromStokerAct("kill:%1", ""))
	if n := len(s.Stoker.View().Run); n != len(run) {
		t.Fatalf("the card asks twice: %d steps, was %d", n, len(run))
	}
	if res := s.act(t.Context(), consoleAct("stoker:grant", e.ID)); res.Err != "" {
		t.Fatal(res.Err)
	}
	if res := s.act(t.Context(), fromStokerAct("kill:%1", "")); res.Err != "" || len(*ran) != 1 {
		t.Fatalf("err %q ran %q", res.Err, *ran)
	}
	// The go was good for one kill.
	s.publish(sample.Snapshot{At: night0, Sessions: []sample.Session{
		{Agent: "claude", Pane: "%1", SessionID: "s1", Name: "work", Status: "idle", Idle: 5 * time.Minute},
		{Agent: "claude", Pane: "%9@mbtest", SessionID: "s9", Name: "stoker", Status: "busy", Busy: true},
	}}, func(state.Doc) {})
	if res := s.act(t.Context(), fromStokerAct("kill:%1", "")); !strings.HasPrefix(res.Err, "the kill waits") {
		t.Fatalf("a second kill on one go: %q", res.Err)
	}
}

func TestStokerKillsWithTheKillsGo(t *testing.T) {
	s, ran := handsLife(t)
	if _, err := s.Stoker.Update(func(m stoker.Mode) (stoker.Mode, bool) {
		m.Order = &stoker.Order{ID: "run-1", State: stoker.OrderRunning, Grants: []string{"kill"}, At: night0, Went: night0}
		return m, true
	}); err != nil {
		t.Fatal(err)
	}
	if res := s.act(t.Context(), fromStokerAct("kill:%1", "")); res.Err != "" || len(*ran) != 1 {
		t.Fatalf("err %q ran %q", res.Err, *ran)
	}
}

func TestStokerNeverKillsWorkWaitsOrParked(t *testing.T) {
	s, ran := handsLife(t)
	if _, err := s.Stoker.Update(func(m stoker.Mode) (stoker.Mode, bool) {
		m.Order = &stoker.Order{ID: "run-1", State: stoker.OrderRunning, Grants: []string{"kill"}, At: night0, Went: night0}
		return m, true
	}); err != nil {
		t.Fatal(err)
	}
	snap := sample.Snapshot{At: night0, Sessions: []sample.Session{
		{Agent: "claude", Pane: "%1", SessionID: "s1", Name: "work", Status: "busy", Busy: true},
		{Agent: "claude", Pane: "%5", SessionID: "s5", Name: "parked", Status: "idle", Idle: time.Hour},
		{Agent: "claude", Pane: "%9@mbtest", SessionID: "s9", Name: "stoker", Status: "busy", Busy: true},
	}}
	s.publish(snap, func(state.Doc) {})
	if err := s.Holds.Set(holds.Hold{SessionID: "s5", Pane: "%5", Name: "parked", By: holds.ByYou, At: night0}); err != nil {
		t.Fatal(err)
	}
	for pane, why := range map[string]string{
		"%1": "work works: the stoker kills only a session at rest",
		"%5": "parked by you: the stoker leaves it alone",
	} {
		if res := s.act(t.Context(), fromStokerAct("kill:"+pane, KillAnyway)); res.Err != why {
			t.Errorf("%s: %q", pane, res.Err)
		}
	}
	if len(*ran) != 0 {
		t.Fatalf("ran %q", *ran)
	}
}

func TestStokerStartsSessionsOnTheBuildersServerAndDrivesThem(t *testing.T) {
	s, ran := handsLife(t)
	b, _ := json.Marshal(proto.NewSession{Where: t.TempDir(), Agent: "claude", Name: "n88", First: "work on #88"})
	res := s.act(t.Context(), fromStokerAct("new:session", string(b)))
	if res.Err != "" || len(*ran) != 1 || slices.Contains((*ran)[0], "-L") {
		t.Fatalf("err %q ran %q", res.Err, *ran)
	}
	es := s.Stoker.Entries()
	if e := es[len(es)-1]; !e.Ordered() || e.Pane != "%42" || e.Name != "n88" {
		t.Fatalf("not counted: %+v", e)
	}
	if !s.agentsPane(proto.Act{From: "%42"}) {
		t.Fatal("the session the stoker started is not one it drives")
	}
}

func TestStokerStartsNothingWithNoRun(t *testing.T) {
	s, ran := handsLife(t)
	if err := s.Stoker.Set(stoker.Mode{}); err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(proto.NewSession{Where: t.TempDir(), Agent: "claude", Name: "n88"})
	for _, a := range []proto.Act{fromStokerAct("new:session", string(b)), fromStokerAct("kill:%1", ""), fromStokerAct("rename:%1", "x")} {
		if res := s.act(t.Context(), a); res.Err != "the stoker is off: no run" {
			t.Errorf("%s: %q", a.RecID, res.Err)
		}
	}
	if len(*ran) != 0 {
		t.Fatalf("ran %q", *ran)
	}
}

func TestKillOfACodexAtRestTakesOneY(t *testing.T) {
	s, ran := lifeTest(t)
	if res := s.act(t.Context(), consoleAct("kill:%3", "")); res.Err != "" || len(*ran) != 2 {
		t.Fatalf("err %q ran %q", res.Err, *ran)
	}
}

// TestANewSessionIsSampledAtOnce: the service knows it made a pane, so it
// samples now instead of at the next tick.
func TestANewSessionIsSampledAtOnce(t *testing.T) {
	s, _ := lifeTest(t) // a tick of an hour
	var samples atomic.Int32
	s.sample = func() sample.Snapshot {
		samples.Add(1)
		snap := lifeSnap(false)
		snap.At = time.Now()
		return snap
	}
	ctx := run(t, s)
	c, _ := connect(t, ctx, s)
	snapshot(t, c)
	before := samples.Load()
	if r := act(t, c, "n", newAct(t, proto.NewSession{Where: t.TempDir(), Agent: "claude", Name: "fresh"})); r.Err != "" {
		t.Fatalf("%+v", r)
	}
	deadline := time.Now().Add(2 * time.Second)
	for samples.Load() == before {
		if time.Now().After(deadline) {
			t.Fatal("no sample after the new session: it waits for the tick")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestNewStartsCursor(t *testing.T) {
	for _, agent := range []string{"cursor-agent", "cursor"} {
		s, ran := lifeTest(t)
		dir := t.TempDir()
		res := s.act(t.Context(), newAct(t, proto.NewSession{Where: dir, Agent: agent, Name: "fresh", First: "fix the build"}))
		if res.Err != "" || len(*ran) != 1 {
			t.Fatalf("%s: err %q ran %q", agent, res.Err, *ran)
		}
		if got := strings.Join((*ran)[0], " "); !strings.HasSuffix(got, "-c "+dir+" cursor-agent --trust fix the build") {
			t.Errorf("%s: ran %q", agent, got)
		}
	}
}

// TestNewStartsCursorByAgentWhenOnlyThatIsInstalled: Cursor's installer
// adds both commands, a hand install may add only `agent`.
func TestNewStartsCursorByAgentWhenOnlyThatIsInstalled(t *testing.T) {
	s, ran := lifeTest(t)
	agent := cursorInstall(t)
	s.LookPath = func(cmd string) (string, error) {
		switch cmd {
		case "cursor-agent":
			return "", exec.ErrNotFound
		case "agent":
			return agent, nil
		}
		return "/usr/bin/" + cmd, nil
	}
	dir := t.TempDir()
	if res := s.act(t.Context(), newAct(t, proto.NewSession{Where: dir, Agent: "cursor-agent", Name: "fresh", First: "fix the build"})); res.Err != "" {
		t.Fatal(res.Err)
	}
	if got := strings.Join((*ran)[0], " "); !strings.HasSuffix(got, "-c "+dir+" agent --trust fix the build") {
		t.Errorf("ran %q", got)
	}
	if !slices.Contains(s.startable(), "cursor-agent") {
		t.Errorf("startable %q, want cursor-agent", s.startable())
	}
}

// cursorInstall is Cursor's installer's `agent`: a link into its versions.
func cursorInstall(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	bin := filepath.Join(home, ".local", "share", "cursor-agent", "versions", "2026.10.19-6bbf2c4", "cursor-agent")
	link := filepath.Join(home, ".local", "bin", "agent")
	for _, d := range []string{filepath.Dir(bin), filepath.Dir(link)} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(bin, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(bin, link); err != nil {
		t.Fatal(err)
	}
	return link
}

// TestAnotherAgentIsNotCursor: "agent" is a common name. Only Cursor's
// own counts for Cursor.
func TestAnotherAgentIsNotCursor(t *testing.T) {
	s, ran := lifeTest(t)
	other := filepath.Join(t.TempDir(), "agent")
	if err := os.WriteFile(other, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	s.LookPath = func(cmd string) (string, error) {
		switch cmd {
		case "cursor-agent":
			return "", exec.ErrNotFound
		case "agent":
			return other, nil
		}
		return "/usr/bin/" + cmd, nil
	}
	if slices.Contains(s.startable(), "cursor-agent") {
		t.Errorf("startable %q offers cursor-agent for another agent", s.startable())
	}
	res := s.act(t.Context(), newAct(t, proto.NewSession{Where: t.TempDir(), Agent: "cursor-agent", Name: "fresh"}))
	if res.Err != "cursor-agent is not installed here" || len(*ran) != 0 {
		t.Errorf("err %q ran %q, want it not installed", res.Err, *ran)
	}
}
