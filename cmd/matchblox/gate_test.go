package main

import (
	"errors"
	"os"
	"os/exec"
	"slices"
	"testing"
	"time"

	"github.com/blox-eng/matchblox/internal/doors"
	"github.com/blox-eng/matchblox/internal/remote"
	"github.com/blox-eng/matchblox/internal/setup"
)

// The matchblox key starts the service stream or the console and nothing else: jumps and
// door commands use the builder's own login, so a tmux client (which can
// open a shell) or an agent is never one of its commands.
func TestGateRunsOnlyItsCommands(t *testing.T) {
	const id = "0123456789ab"
	for _, c := range []struct {
		mode, id, orig string
		want           []string
	}{
		{"", "", remote.ServeCommand, []string{"serve", "--stdio"}},
		{"phone", "", remote.ServeCommand, []string{"serve", "--stdio"}},
		{"phone", id, remote.ServeCommand, []string{"serve", "--stdio"}},
		{"phone", "", remote.ConsoleCommand, []string{}},
		{"phone", id, remote.ConsoleCommand, []string{}},
		{"phone", id, "matchblox push register", []string{"push", "register", id}},
	} {
		got, err := gateDecision(c.mode, c.id, c.orig)
		if err != nil || !slices.Equal(got, c.want) || got == nil {
			t.Fatalf("gateDecision(%q, %q, %q) = %q, %v; want %q", c.mode, c.id, c.orig, got, err, c.want)
		}
	}
	// A laptop key never gets the console, and an unknown mode gets nothing.
	if argv, err := gateDecision("", "", remote.ConsoleCommand); err == nil || argv != nil {
		t.Errorf("a laptop key opened the console: %q", argv)
	}
	for _, orig := range []string{remote.ServeCommand, remote.ConsoleCommand, remote.PushRegisterCommand, ""} {
		if argv, err := gateDecision("pair", id, orig); err == nil || argv != nil {
			t.Errorf("unknown mode ran %q", orig)
		}
	}
	// Only a phone registers, and only one whose line has its id.
	for _, c := range []struct{ mode, id, want string }{
		{"phone", "", "this phone's key line is old; open matchblox serve once, then try again"},
		{"", "", "this key starts only the matchblox stream; use your own ssh login for a shell"},
		{"", id, "this key starts only the matchblox stream; use your own ssh login for a shell"},
	} {
		if argv, err := gateDecision(c.mode, c.id, remote.PushRegisterCommand); err == nil || argv != nil || err.Error() != c.want {
			t.Errorf("gateDecision(%q, %q, register) = %q, %v; want %q", c.mode, c.id, argv, err, c.want)
		}
	}
	last := func(a []string) string { return a[len(a)-1] }
	for _, orig := range []string{
		"", "sh", "bash -c id", "id",
		"tmux attach-session",
		"tmux select-window -t %12 ';' select-pane -t %12 ';' attach-session -t %12",
		"tmux new-window -c / ';' attach-session",
		last(remote.NavArgv("ws-1", [][]string{{"tmux", "switch-client", "-t", "%12"}})),
		last(remote.TermArgv("ws-1", doors.GuideArgv(false))),
		remote.Quote(doors.GuideArgv(false)),
		"matchblox serve --stdio; id",
		"matchblox serve --stdio ",
		"matchblox serve",
		"matchblox console; id",
		"matchblox console ",
		"matchblox  console",
		"matchblox gate pair 0123456789ab",
		"matchblox push register x",
		"matchblox push register " + id,
		"matchblox push register ",
		"matchblox push  register",
		"matchblox push register; id",
		"matchblox push",
		"matchblox push unregister",
	} {
		for _, mode := range []string{"", "phone"} {
			for _, i := range []string{"", id} {
				if argv, err := gateDecision(mode, i, orig); err == nil || argv != nil {
					t.Errorf("gateDecision(%q, %q, %q) let it run", mode, i, orig)
				}
			}
		}
	}
}

func TestGateRefusalNamesTheFixOfItsMode(t *testing.T) {
	for _, c := range []struct{ mode, orig, want string }{
		{"", "", "this key starts only the matchblox stream; use your own ssh login for a shell"},
		{"", "sh", "this key starts only the matchblox stream; use your own ssh login for a shell"},
		{"phone", "", "this key starts only the matchblox stream and console; use your own ssh login for a shell"},
		{"phone", "sh", "this key starts only the matchblox stream and console; use your own ssh login for a shell"},
		{"", remote.ConsoleCommand, "this key starts only the matchblox stream; run matchblox pair to add a phone"},
	} {
		if _, err := gateDecision(c.mode, "", c.orig); err == nil || err.Error() != c.want {
			t.Errorf("gateDecision(%q, %q) error = %v, want %q", c.mode, c.orig, err, c.want)
		}
	}
}

// gateWith runs gateMain with a fake run, in a temporary home with the
// console command as the requested command. It returns the arguments that run
// got, or nil if run never ran.
func gateWith(t *testing.T, tty string, args ...string) (home string, ran []string) {
	t.Helper()
	home = t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SSH_TTY", tty)
	t.Setenv("SSH_ORIGINAL_COMMAND", remote.ConsoleCommand)
	oldArgs, oldRun := os.Args, gateRun
	t.Cleanup(func() { os.Args, gateRun = oldArgs, oldRun })
	gateRun = func(argv []string) error { ran = argv; return nil }
	if err := gateMain(args); err != nil {
		t.Fatal(err)
	}
	return home, ran
}

func TestGatePhoneWithIDMarksTheDevice(t *testing.T) {
	before := time.Now()
	home, ran := gateWith(t, "/dev/ttys004", "phone", "0123456789ab")
	if ran == nil {
		t.Fatal("the console did not run")
	}
	at, tty, ok := setup.DeviceIn(home, "0123456789ab")
	if !ok || tty != "/dev/ttys004" || at.Before(before) {
		t.Fatalf("note = %v, %q, %v", at, tty, ok)
	}
}

func TestGatePhoneWithoutIDStillOpens(t *testing.T) {
	home, ran := gateWith(t, "/dev/ttys004", "phone")
	if ran == nil {
		t.Fatal("the console did not run")
	}
	if m := setup.DeviceTTYs(home); len(m) != 0 {
		t.Fatalf("a note was written: %v", m)
	}
}

// A note that cannot be written never blocks the console.
func TestGatePhoneOpensWhenTheNoteFails(t *testing.T) {
	home := t.TempDir()
	if err := os.WriteFile(home+"/.ssh", nil, 0o600); err != nil { // a file where the directory goes
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("SSH_ORIGINAL_COMMAND", remote.ConsoleCommand)
	oldArgs, oldRun := os.Args, gateRun
	t.Cleanup(func() { os.Args, gateRun = oldArgs, oldRun })
	var ran bool
	gateRun = func([]string) error { ran = true; return nil }
	if err := gateMain([]string{"phone", "0123456789ab"}); err != nil || !ran {
		t.Fatalf("gateMain = %v, ran %v", err, ran)
	}
}

// gateMain exits the process on a refusal, so the test runs it in a child.
func TestGatePhoneRefusesABadID(t *testing.T) {
	if os.Getenv("MATCHBLOX_GATE_CHILD") == "1" {
		gateRun = func([]string) error { return errors.New("ran") }
		_ = gateMain([]string{"phone", "xyz"})
		os.Exit(0)
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestGatePhoneRefusesABadID$")
	cmd.Env = append(os.Environ(), "MATCHBLOX_GATE_CHILD=1", "SSH_ORIGINAL_COMMAND="+remote.ConsoleCommand)
	out, err := cmd.CombinedOutput()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 126 {
		t.Fatalf("exit = %v, want 126\n%s", err, out)
	}
}

// The service stream of a phone is not its console: it leaves the note alone.
func TestGatePhoneServeStreamWritesNoNote(t *testing.T) {
	home := t.TempDir()
	const id = "0123456789ab"
	at := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	if err := setup.MarkDeviceIn(home, id, "/dev/ttys004", at); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("SSH_TTY", "")
	t.Setenv("SSH_ORIGINAL_COMMAND", remote.ServeCommand)
	oldArgs, oldRun := os.Args, gateRun
	t.Cleanup(func() { os.Args, gateRun = oldArgs, oldRun })
	var ran []string
	gateRun = func(argv []string) error { ran = argv; return nil }
	if err := gateMain([]string{"phone", id}); err != nil || !slices.Equal(ran, []string{"serve", "--stdio"}) {
		t.Fatalf("gateMain = %v, ran %q", err, ran)
	}
	if gotAt, tty, ok := setup.DeviceIn(home, id); !ok || tty != "/dev/ttys004" || !gotAt.Equal(at) {
		t.Fatalf("the note changed: %v, %q, %v", gotAt, tty, ok)
	}
	// A device with no note gets none.
	t.Setenv("SSH_ORIGINAL_COMMAND", remote.ServeCommand) // gateMain unset it
	if err := gateMain([]string{"phone", "ba9876543210"}); err != nil {
		t.Fatal(err)
	}
	if _, _, ok := setup.DeviceIn(home, "ba9876543210"); ok {
		t.Fatal("the serve stream wrote a note")
	}
}

// A phone with an id registers through the gate: run gets the id from the
// key line, never from the request, and the console's note stays as it was.
func TestGatePhoneRegistersByTheIDOfItsLine(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("SSH_TTY", "/dev/ttys004")
	t.Setenv("SSH_ORIGINAL_COMMAND", remote.PushRegisterCommand)
	oldArgs, oldRun := os.Args, gateRun
	t.Cleanup(func() { os.Args, gateRun = oldArgs, oldRun })
	var ran []string
	gateRun = func(argv []string) error { ran = argv; return nil }
	if err := gateMain([]string{"phone", "0123456789ab"}); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(ran, []string{"push", "register", "0123456789ab"}) {
		t.Fatalf("ran %q", ran)
	}
	if _, _, ok := setup.DeviceIn(home, "0123456789ab"); ok {
		t.Fatal("registering marked the device in")
	}
}
