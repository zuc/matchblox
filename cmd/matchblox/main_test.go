package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blox-eng/matchblox/internal/config"
	"github.com/blox-eng/matchblox/internal/doors"
	"github.com/blox-eng/matchblox/internal/proto"
	"github.com/blox-eng/matchblox/internal/remote"
	"github.com/blox-eng/matchblox/internal/setup"
	"github.com/blox-eng/matchblox/internal/transport"
)

// TestMain lets a test start this binary as `matchblox <args>`.
func TestMain(m *testing.M) {
	if args := os.Getenv("MATCHBLOX_TEST_ARGS"); args != "" {
		if err := run(strings.Split(args, "\x1f")); err != nil {
			if errors.Is(err, errRestart) {
				os.Exit(exitRestart)
			}
			if !errors.Is(err, errShown) {
				fmt.Fprintln(os.Stderr, "matchblox:", err) // as main says it
			}
			os.Exit(1)
		}
		os.Exit(0)
	}
	// The replay draws the demo clock's times, never this machine's zone.
	time.Local = time.UTC
	os.Exit(m.Run())
}

func fixtures(t *testing.T) string {
	t.Helper()
	p, err := filepath.Abs("../../testdata/machine")
	if err != nil {
		t.Fatal(err)
	}
	return p
}

// isolate points the socket and state at fresh short directories.
func isolate(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "mb")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("XDG_STATE_HOME", filepath.Join(dir, "state"))
	return transport.SocketPath()
}

func hello(t *testing.T, c transport.Conn) proto.Hello {
	t.Helper()
	if err := c.Send(proto.KindHello, "", proto.Hello{Version: proto.Version}); err != nil {
		t.Fatal(err)
	}
	env, err := c.Recv()
	if err != nil || env.Kind != proto.KindHello {
		t.Fatalf("hello: %+v, %v", env, err)
	}
	var h proto.Hello
	_ = json.Unmarshal(env.Body, &h)
	return h
}

func TestConsoleStartsService(t *testing.T) {
	path := isolate(t)
	if _, err := transport.Dial(path, 100*time.Millisecond); err == nil {
		t.Fatal("a service already runs on the fresh socket")
	}
	var child *exec.Cmd
	spawnService = func() error {
		child = exec.Command(os.Args[0])
		child.Env = append(os.Environ(), "MATCHBLOX_TEST_ARGS=serve\x1f--fixtures\x1f"+fixtures(t))
		detach(child)
		return child.Start()
	}
	t.Cleanup(func() {
		if child != nil && child.Process != nil {
			child.Process.Kill()
			child.Wait()
		}
	})
	c, err := ensureService(path)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if h := hello(t, c); h.Version != proto.Version {
		t.Fatalf("hello %+v", h)
	}
	// A second console finds the running service and starts nothing.
	spawnService = func() error { t.Fatal("started a second service"); return nil }
	c2, err := ensureService(path)
	if err != nil {
		t.Fatal(err)
	}
	c2.Close()
}

func TestEnsureServiceGivesUpWithTheFix(t *testing.T) {
	path := isolate(t)
	spawnService = func() error { return nil } // starts nothing
	start := time.Now()
	_, err := ensureService(path)
	if err == nil || !strings.Contains(err.Error(), "matchblox serve") {
		t.Fatalf("err %v, want the command to run", err)
	}
	if d := time.Since(start); d > 3*time.Second {
		t.Fatalf("gave up after %v, want about 2 s", d)
	}
}

// The user unit starts serve again only after a nonzero exit: a replaced
// binary or a removed socket says so, a second serve exits 0.
func TestServeAsksForARestart(t *testing.T) {
	for _, tc := range []struct {
		name  string
		after func(exe, sock string) error
	}{
		{"replaced", func(exe, _ string) error { return os.WriteFile(exe, []byte("v2, longer"), 0o600) }},
		{"socket gone", func(_, sock string) error { return os.Remove(sock) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := isolate(t)
			exe := filepath.Join(t.TempDir(), "matchblox")
			if err := os.WriteFile(exe, []byte("v1"), 0o600); err != nil {
				t.Fatal(err)
			}
			s := newService(config.Default(), fixtures(t))
			s.Exe, s.ExeEvery = exe, 10*time.Millisecond
			done := make(chan error, 1)
			go func() { done <- serve(path, s) }()
			deadline := time.Now().Add(2 * time.Second)
			for {
				c, err := transport.Dial(path, 100*time.Millisecond)
				if err == nil {
					c.Close()
					break
				}
				if time.Now().After(deadline) {
					t.Fatal(err)
				}
				time.Sleep(10 * time.Millisecond)
			}
			if err := tc.after(exe, path); err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-done:
				if !errors.Is(err, errRestart) {
					t.Fatalf("serve returned %v, want errRestart", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("serve kept running")
			}
		})
	}
	t.Run("already runs", func(t *testing.T) {
		path := isolate(t)
		serveInProcess(t, path)
		if err := serve(path, newService(config.Default(), fixtures(t))); err != nil {
			t.Fatalf("serve returned %v, want nil", err)
		}
	})
}

func serveInProcess(t *testing.T, path string) {
	t.Helper()
	l, err := transport.Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s := newService(config.Default(), fixtures(t))
	go s.Serve(ctx, l)
}

func TestStatusReadsService(t *testing.T) {
	path := isolate(t)
	serveInProcess(t, path)
	var out bytes.Buffer
	if err := status(&out, path, config.Default(), "", true); err != nil {
		t.Fatal(err)
	}
	// Only the fixture machine has this session; the live machine does not.
	if !strings.Contains(out.String(), "app-feature") {
		t.Fatalf("status did not read the service:\n%s", out.String())
	}
}

// TestStatusWarnsOfTheStokersPaneKey: matchblox starts the stoker agent
// itself, and a config that still names [stoker] pane is told so.
func TestStatusWarnsOfTheStokersPaneKey(t *testing.T) {
	path := isolate(t)
	serveInProcess(t, path)
	cfg := config.Default()
	cfg.Stoker.PaneSet = true
	var out bytes.Buffer
	if err := status(&out, path, cfg, "", true); err != nil {
		t.Fatal(err)
	}
	if first, _, _ := strings.Cut(out.String(), "\n"); first != stokerPaneGone {
		t.Fatalf("status said:\n%s", out.String())
	}
	out.Reset()
	if err := status(&out, path, config.Default(), "", true); err != nil || strings.Contains(out.String(), stokerPaneGone) {
		t.Fatalf("a config without the key was warned: %v\n%s", err, out.String())
	}
}

func TestServeStdioRelaysToService(t *testing.T) {
	path := isolate(t)
	serveInProcess(t, path)
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	go serveStdio(path, inR, outW) //nolint:errcheck // ends with the test
	c := transport.Stdio(outR, inW)
	defer c.Close()
	if h := hello(t, c); h.Version != proto.Version {
		t.Fatalf("hello over stdio %+v", h)
	}
}

func TestServiceStartsOutsideAnyWorktree(t *testing.T) {
	isolate(t)
	link := filepath.Join(t.TempDir(), "matchblox")
	if err := os.Symlink(os.Args[0], link); err != nil {
		t.Fatal(err)
	}
	defer func(a string) { os.Args[0] = a }(os.Args[0])
	os.Args[0] = link
	c, closeLog, err := serviceCmd()
	if err != nil {
		t.Fatal(err)
	}
	defer closeLog()
	if c.Dir != "/" {
		t.Fatalf("the service would keep the console's cwd %q, which pins that worktree as in use", c.Dir)
	}
	if c.SysProcAttr == nil {
		t.Fatal("the service is not detached")
	}
	if c.Args[1] != "serve" {
		t.Fatalf("argv %v", c.Args)
	}
	if c.Path != link {
		t.Fatalf("the service starts as %q, not the invoked link %q: it would watch a file an upgrade never changes", c.Path, link)
	}
}

func TestInvokedPathKeepsTheLink(t *testing.T) {
	dir := t.TempDir()
	target, link := filepath.Join(dir, "matchblox-0.1.0"), filepath.Join(dir, "matchblox")
	if err := os.WriteFile(target, []byte("#!/bin/sh\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if got := invokedPath(link); got != link {
		t.Fatalf("by path: got %q, want the link %q", got, link)
	}
	t.Setenv("PATH", dir)
	if got := invokedPath("matchblox"); got != link {
		t.Fatalf("by name: got %q, want the link %q", got, link)
	}
}

// fakeService answers every console with hello and one snapshot taken at.
func fakeService(t *testing.T, path string, at time.Time) {
	t.Helper()
	l, err := transport.Listen(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			nc, err := l.Accept()
			if err != nil {
				return
			}
			c := transport.NewConn(nc)
			_ = c.Send(proto.KindHello, "", proto.Hello{Version: proto.Version})
			st := proto.State{}
			st.At = at
			_ = c.Send(proto.KindSnapshot, "", st)
		}
	}()
}

func TestStatusRefusesAStuckService(t *testing.T) {
	path := isolate(t)
	fakeService(t, path, time.Now().Add(-time.Hour))
	if _, ok := fromService(path, staleAfter); ok {
		t.Fatal("an hour-old snapshot from a stuck sample loop was taken as current")
	}
	path2 := filepath.Join(filepath.Dir(path), "fresh.sock")
	fakeService(t, path2, time.Now())
	if _, ok := fromService(path2, staleAfter); !ok {
		t.Fatal("a fresh snapshot was refused")
	}
}

// A fixture tree is not this machine: its recs name panes and pids that
// exist here too, so a fixture service must run nothing.
func TestFixtureServiceRunsNothing(t *testing.T) {
	s := newService(config.Default(), fixtures(t))
	marker := filepath.Join(t.TempDir(), "ran")
	if err := s.Actions.Run([]string{"touch", marker}); err == nil {
		t.Fatal("a fixture step reported success")
	}
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a fixture step ran on the live machine")
	}
}

// fixtureHello serves one console from a fixture service and returns its hello.
func fixtureHello(t *testing.T, root string) proto.Hello {
	t.Helper()
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	s := newService(config.Default(), root)
	a, b := net.Pipe()
	go s.Handle(ctx, transport.NewConn(b)) //nolint:errcheck // ends with the test
	c := transport.NewConn(a)
	t.Cleanup(func() { c.Close() })
	sent := make(chan error, 1)
	go func() { sent <- c.Send(proto.KindHello, "", proto.Hello{Version: proto.Version}) }()
	env, err := c.Recv()
	if err != nil || env.Kind != proto.KindHello {
		t.Fatalf("first message %+v, %v", env, err)
	}
	if err := <-sent; err != nil {
		t.Fatal(err)
	}
	var h proto.Hello
	_ = json.Unmarshal(env.Body, &h)
	return h
}

func TestFixtureHostName(t *testing.T) {
	if h := fixtureHello(t, fixtures(t)); h.Host != "ws-1" {
		t.Fatalf("host %q, want ws-1 from the fixture", h.Host)
	}
	if h := fixtureHello(t, t.TempDir()); h.Host != "fixtures" {
		t.Fatalf("host %q, want fixtures when the fixture has no hostname file", h.Host)
	}
}

func setupEnv(t *testing.T) setup.Env {
	t.Helper()
	home := t.TempDir()
	return setup.Env{GOOS: "linux", Home: home, Exe: "/bin/matchblox", StateDir: filepath.Join(home, "state"),
		Getenv: func(string) string { return "" },
		LookPath: func(name string) (string, error) {
			if name == "claude" || name == "tmux" {
				return "/usr/bin/" + name, nil
			}
			return "", errors.New("not found")
		},
		Source: func(string) error { return nil },
		Run:    func([]string) error { return nil }} // no real launchctl or systemctl
}

func TestSetupCommandOpensWhatTheBuilderTypes(t *testing.T) {
	e := setupEnv(t)
	var out strings.Builder
	var ran [][]string
	err := setupDoors(e, strings.NewReader("y\n\n\nn\n\n"), &out, func(argv []string) error { ran = append(ran, argv); return nil })
	if err != nil {
		t.Fatal(err)
	}
	got := out.String()
	for _, want := range []string{"✓ Install tmux", "Add the queue hooks", "+    \"Stop\": [", "wrote " + filepath.Join(e.Home, ".claude", "settings.json"), "Add the way back", "Open a guide session", "uses your tokens"} {
		if !strings.Contains(got, want) {
			t.Errorf("no %q in:\n%s", want, got)
		}
	}
	if b, _ := os.ReadFile(filepath.Join(e.Home, ".claude", "settings.json")); !strings.Contains(string(b), "/bin/matchblox hook Stop") {
		t.Errorf("hooks not written:\n%s", b)
	}
	if _, err := os.Stat(filepath.Join(e.Home, ".tmux.conf")); err == nil {
		t.Error("Enter wrote the way back")
	}
	if ran != nil {
		t.Errorf("ran %q on n", ran)
	}
}

func TestSetupCommandShowsClosedDoors(t *testing.T) {
	e := setupEnv(t)
	if err := setup.Close(e, doors.Guide); err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	var ran [][]string
	if err := setupDoors(e, strings.NewReader("\n\n\n\n\ny\n"), &out, func(argv []string) error { ran = append(ran, argv); return nil }); err != nil {
		t.Fatal(err)
	}
	if len(ran) != 1 || ran[0][0] != "claude" {
		t.Fatalf("ran %q:\n%s", ran, out.String())
	}
}

func TestFirstRunWritesTheConfig(t *testing.T) {
	p := filepath.Join(t.TempDir(), "matchblox", "config.toml")
	firstRun(p, 16, 64<<30)
	cfg, err := config.Load(p)
	if err != nil || cfg.Alerts.Load1Over != 16 {
		t.Fatalf("config %+v, %v", cfg.Alerts, err)
	}
}

func TestFirstArgThatIsNoCommandIsAHost(t *testing.T) {
	for _, c := range []struct {
		args      []string
		cmd, host string
		rest      []string
		wantErr   bool
	}{
		{nil, "console", "", nil, false},
		{[]string{"--no-motion"}, "console", "", []string{"--no-motion"}, false},
		{[]string{"serve", "--stdio"}, "serve", "", []string{"--stdio"}, false},
		{[]string{"ws-1"}, "console", "ws-1", nil, false},
		{[]string{"ws-1", "--no-motion"}, "console", "ws-1", []string{"--no-motion"}, false},
		{[]string{"me@build.example.com"}, "console", "me@build.example.com", nil, false},
		{[]string{"ws;1"}, "", "", nil, true},
	} {
		cmd, host, rest, err := parseCommand(c.args)
		if (err != nil) != c.wantErr || cmd != c.cmd || host != c.host || strings.Join(rest, " ") != strings.Join(c.rest, " ") {
			t.Errorf("parseCommand(%q) = %q %q %q %v", c.args, cmd, host, rest, err)
		}
	}
}

// fakeRemote puts an ssh on PATH whose far side is this test binary, as
// `matchblox serve --stdio` on an isolated host.
func fakeRemote(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	far := filepath.Join(dir, "far")
	if err := os.Mkdir(far, 0o700); err != nil {
		t.Fatal(err)
	}
	ssh := "#!/bin/sh\nwhile [ $# -gt 0 ]; do case \"$1\" in -o|-i) shift 2;; --) shift; break;; -*) shift;; *) break;; esac; done\nshift\nexec sh -c \"$*\"\n"
	mb := "#!/bin/sh\nMATCHBLOX_TEST_ARGS=\"$(printf '%s\\037%s' \"$1\" \"$2\")\" exec " + os.Args[0] + "\n"
	for name, body := range map[string]string{filepath.Join(dir, "ssh"): ssh, filepath.Join(far, "matchblox"): mb} {
		if err := os.WriteFile(name, []byte(body), 0o700); err != nil { //nolint:gosec // a test script
			t.Fatal(err)
		}
	}
	t.Setenv("PATH", dir+":"+far+":"+os.Getenv("PATH"))
}

func TestRemoteReachesTheHostService(t *testing.T) {
	path := isolate(t)
	serveInProcess(t, path)
	fakeRemote(t)
	key := filepath.Join(t.TempDir(), remote.KeyName)
	if err := os.WriteFile(key, []byte("key"), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := remote.Connect(context.Background(), remote.Target{Host: "ws-1", Key: key})
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if h := hello(t, c); h.Version != proto.Version {
		t.Fatalf("hello over ssh %+v", h)
	}
	for {
		env, err := c.Recv()
		if err != nil {
			t.Fatal(err)
		}
		if env.Kind == proto.KindSnapshot {
			return
		}
	}
}

// TestAgentLookPathFindsTheAgentsOwnDirs: the service can be the first to
// run, with a small PATH; the agents' own install dirs are searched too.
func TestAgentLookPathFindsTheAgentsOwnDirs(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, ".opencode", "bin")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "opencode"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", t.TempDir())
	look := agentLookPath(home)
	if p, err := look("opencode"); err != nil || p != filepath.Join(dir, "opencode") {
		t.Fatalf("opencode: %q, %v", p, err)
	}
	if _, err := look("no-such-agent-x7"); err == nil {
		t.Fatal("found an agent that is not there")
	}
}

// The night door is a key in the console, not a file: setup points to it
// and asks nothing.
func TestSetupCommandPointsTheNightToTheConsole(t *testing.T) {
	e := setupEnv(t)
	var out strings.Builder
	if err := setupDoors(e, strings.NewReader("\n\n\n\n\ny\n"), &out, func([]string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	night := got[strings.Index(got, "Try a night"):strings.Index(got, "Write your ways")]
	if strings.Contains(got, "no door") || strings.Contains(night, "y opens it") || !strings.Contains(night, "press n in the console") {
		t.Fatalf("setup:\n%s", got)
	}
}

func TestSetupCommandAsksForTheMouseAfterTheWayBack(t *testing.T) {
	for _, c := range []struct {
		answers string
		on      bool
	}{{"\n\n\ny\ny\n", true}, {"\n\n\ny\n\n", false}} {
		e := setupEnv(t)
		var out strings.Builder
		if err := setupDoors(e, strings.NewReader(c.answers), &out, func([]string) error { return nil }); err != nil {
			t.Fatal(err)
		}
		b, err := os.ReadFile(filepath.Join(e.Home, ".tmux.conf"))
		if err != nil || !strings.Contains(string(b), "=matchblox") {
			t.Fatalf("%q: way back not written (%v):\n%s", c.answers, err, out.String())
		}
		if on := strings.Contains(string(b), "set -g mouse on"); on != c.on {
			t.Fatalf("%q: mouse on = %v:\n%s", c.answers, on, out.String())
		}
		if !strings.Contains(out.String(), "+set -g mouse on") {
			t.Fatalf("%q: the mouse line is not shown before the question:\n%s", c.answers, out.String())
		}
	}
}

// TestHelpListsTheCommands: F10, --help names every command a builder or
// an agent types, one line each, before the flags.
func TestHelpListsTheCommands(t *testing.T) {
	fl, _ := newFlags()
	var buf strings.Builder
	fl.SetOutput(&buf)
	if err := fl.Parse([]string{"--help"}); !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("--help: %v", err)
	}
	out := buf.String()
	cmds, flags := strings.Index(out, "Commands:"), strings.Index(out, "Flags:")
	if cmds < 0 || flags < cmds {
		t.Fatalf("help:\n%s", out)
	}
	for _, c := range commands {
		if !strings.Contains(out[cmds:flags], "\n  matchblox "+c+" ") {
			t.Errorf("help does not list %s:\n%s", c, out)
		}
	}
	if !strings.Contains(out[flags:], "-json") {
		t.Errorf("help lost its flags:\n%s", out)
	}
}

// The pair door is a page in the console, not a file: setup points to it
// and asks nothing.
func TestSetupSaysHowToPair(t *testing.T) {
	e := setupEnv(t)
	var out strings.Builder
	if err := setupDoors(e, strings.NewReader("\n\n\n\n\ny\n"), &out, func([]string) error { return nil }); err != nil {
		t.Fatal(err)
	}
	got := out.String()
	pair := got[strings.Index(got, "Pair a device"):strings.Index(got, "Keep matchblox running")]
	if strings.Contains(pair, "y opens it") || !strings.Contains(pair, "press Enter on it in the console, or run matchblox pair") {
		t.Fatalf("setup:\n%s", got)
	}
}
