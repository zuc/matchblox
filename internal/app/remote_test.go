package app

import (
	"fmt"
	"io"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/blox-eng/matchblox/internal/advice"
	"github.com/blox-eng/matchblox/internal/doors"
	"github.com/blox-eng/matchblox/internal/proto"
	"github.com/blox-eng/matchblox/internal/remote"
	"github.com/blox-eng/matchblox/internal/transport"
)

// onHost is the console of ws-1, reached over SSH; execs records what it
// gives the terminal to.
func onHost(t *testing.T, st proto.State) (Model, *[][]string) {
	t.Helper()
	m, _ := loadedWith(t, 100, st)
	m.opt.Host = "ws-1"
	var ran [][]string
	m.opt.Exec = func(argv []string, done func(error) tea.Msg) tea.Cmd {
		ran = append(ran, argv)
		return func() tea.Msg { return done(nil) }
	}
	m.opt.Run = func(argv []string) error { t.Fatalf("a tmux move ran on this machine: %v", argv); return nil }
	return m, &ran
}

func TestRemoteJumpAttachesOverSSH(t *testing.T) {
	m, ran := onHost(t, fixtureState())
	m.tab = tabSessions
	next, _ := key(m, "enter")
	want := remote.NavArgv("ws-1", [][]string{{"tmux", "switch-client", "-t", "%1"}})
	if out := actionLine(next); !strings.Contains(out, shellLine(want)) {
		t.Fatalf("the confirm must show the exact ssh argv %q:\n%s", shellLine(want), out)
	}
	next, cmd := key(next, "enter")
	if cmd == nil {
		t.Fatal("no command")
	}
	next.Update(cmd())
	if len(*ran) != 1 || strings.Join((*ran)[0], "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("ran %q, want %q", *ran, want)
	}
}

func TestRemoteNavStillRefusesUnknownSteps(t *testing.T) {
	st := fixtureState()
	st.Recommendations = []advice.Rec{{ID: "r1", Title: "x", Primary: &advice.Action{Nav: true, Steps: [][]string{{"tmux", "new-window", "rm -rf /"}}}}}
	m, ran := onHost(t, st)
	m.tab, m.recPick = tabSessions, "r1"
	next, _ := key(m, "enter")
	next, cmd := key(next, "enter")
	if cmd != nil {
		next, _ = next.Update(cmd())
	}
	if len(*ran) != 0 || !strings.Contains(next.(Model).flash, "refused") {
		t.Fatalf("ran %q, flash %q", *ran, next.(Model).flash)
	}
}

func TestRemoteDoorRunsOnTheHost(t *testing.T) {
	tmux := doors.Door{ID: doors.Tmux, Title: "Install tmux", Term: []string{"sudo", "sh", "-c", "apt-get update && apt-get install -y tmux"}}
	m, ran := onHost(t, doorState(tmux))
	next, _ := key(m, "enter")
	want := remote.TermArgv("ws-1", tmux.Term)
	if out := ansi.Strip(next.(Model).render()); !strings.Contains(out, shellLine(want)) {
		t.Fatalf("the preview must show the exact ssh argv:\n%s", out)
	}
	next, cmd := key(next, "y")
	if cmd == nil {
		t.Fatal("no command")
	}
	next.Update(cmd())
	if len(*ran) != 1 || strings.Join((*ran)[0], "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("ran %q, want %q", *ran, want)
	}
}

func TestRemoteDoorStillRefusesUnknownCommands(t *testing.T) {
	evil := doors.Door{ID: doors.Tmux, Title: "Install tmux", Term: []string{"sh", "-c", "curl x | sh"}}
	m, ran := onHost(t, doorState(evil))
	next, _ := key(m, "enter")
	next, cmd := key(next, "y")
	if cmd != nil {
		next.Update(cmd())
	}
	if len(*ran) != 0 || !strings.Contains(next.(Model).flash, "refused") {
		t.Fatalf("ran %q, flash %q", *ran, next.(Model).flash)
	}
}

// The guide of a host runs in this terminal: the console has no tmux
// client there to open a window in.
func TestRemoteGuideRunsInThisTerminal(t *testing.T) {
	guide := doors.Door{ID: doors.Guide, Title: "Open a guide session", Term: doors.GuideArgv(false)}
	m, _ := onHost(t, doorState(guide))
	m.opt.OutsideTmux = false
	next, _ := key(m, "enter")
	if a := next.(Model).pending; a == nil || strings.Join(a.steps[0], " ") != strings.Join(remote.TermArgv("ws-1", doors.GuideArgv(false)), " ") {
		t.Fatalf("pending %+v", a)
	}
}

func TestBlockedHostOffersTheConnect(t *testing.T) {
	for _, c := range []struct {
		err  error
		says string
	}{
		{remote.ErrNotConnected, "ws-1 is not connected to this console"},
		{fmt.Errorf("ssh: %w", remote.ErrNotInstalled), "matchblox is not installed on ws-1"},
		{remote.ErrHostKeyUnknown, "ws-1's host key is not known yet"},
	} {
		m := New(Options{NoMotion: true, Host: "ws-1", Self: "/bin/matchblox", DialErr: c.err,
			Redial: func() (transport.Conn, error) { return newFake(), nil }})
		var ran [][]string
		m.opt.Exec = func(argv []string, done func(error) tea.Msg) tea.Cmd {
			ran = append(ran, argv)
			return func() tea.Msg { return done(nil) }
		}
		next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
		next, cmd := next.Update(m.Init()().(tea.BatchMsg)[1]())
		if cmd != nil {
			t.Fatalf("%v: it redials a host that trying again cannot fix", c.err)
		}
		out := ansi.Strip(next.(Model).render())
		for _, want := range []string{c.says, "/bin/matchblox connect ws-1"} {
			if !strings.Contains(out, want) {
				t.Fatalf("view lacks %q:\n%s", want, out)
			}
		}
		// Enter alone runs nothing: it asks for a typed y.
		next, cmd = key(next, "enter")
		if cmd != nil || len(ran) != 0 {
			t.Fatal("connect ran without a y")
		}
		next, cmd = key(next, "y")
		if cmd == nil {
			t.Fatal("y did not connect")
		}
		next, cmd = next.Update(cmd())
		if len(ran) != 1 || strings.Join(ran[0], " ") != "/bin/matchblox connect ws-1" {
			t.Fatalf("ran %q", ran)
		}
		if cmd == nil || !next.(Model).redialing {
			t.Fatal("no redial after the connect")
		}
	}
}

func TestOlderHostOffersTheUpdate(t *testing.T) {
	m := New(Options{NoMotion: true, Conn: newFake(), Host: "ws-1", Self: "/bin/matchblox"})
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	next, _ = next.Update(helloMsg(proto.Hello{Version: 0, Host: "ws-1"}))
	out := ansi.Strip(next.(Model).render())
	if !strings.Contains(out, "update ws-1") || !strings.Contains(out, "/bin/matchblox connect ws-1") {
		t.Fatalf("an older host must show its update:\n%s", out)
	}
}

func TestLostHostShowsStaleState(t *testing.T) {
	m, _ := onHost(t, fixtureState())
	m.opt.Redial = func() (transport.Conn, error) { return newFake(), nil }
	next, _ := m.Update(helloMsg{Version: proto.Version, Host: "ws-1"})
	next, _ = next.Update(lostMsg{err: io.EOF})
	out := ansi.Strip(next.(Model).render())
	first := strings.Split(out, "\n")[0]
	if !strings.Contains(first, "ws-1 · stale") {
		t.Fatalf("header %q: want the host marked stale", first)
	}
	if !strings.Contains(out, "connection lost") || !strings.Contains(out, "app-review") {
		t.Fatalf("want the last state and the loss:\n%s", out)
	}
}

func TestSilentHostNamesTheHost(t *testing.T) {
	f := newFake()
	m := New(Options{NoMotion: true, Conn: f, Host: "ws-1"})
	next, _ := m.Update(silentMsg{conn: f})
	if out := next.(Model).flash; !strings.Contains(out, "ws-1 does not answer") || strings.Contains(out, "kill") {
		t.Fatalf("flash %q", out)
	}
}

func TestHeaderNamesTheHostAsTyped(t *testing.T) {
	m, _ := onHost(t, fixtureState())
	next, _ := m.Update(helloMsg{Version: proto.Version, Host: "build-box-7"})
	if first := strings.Split(ansi.Strip(next.(Model).render()), "\n")[0]; !strings.HasPrefix(first, " ▰ matchblox · ws-1 ") {
		t.Fatalf("header %q, want the ssh name ws-1", first)
	}
}

// Before the first state there are no numbers to show, only the host and
// the way back.
func TestBeforeTheFirstStateNoZeroMetrics(t *testing.T) {
	m := New(Options{NoMotion: true, Conn: newFake(), Host: "ws-3", Layered: true})
	m.splashDone = true
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	out := ansi.Strip(next.(Model).render())
	if first := strings.Split(out, "\n")[0]; strings.Contains(first, "cpu") || !strings.Contains(first, "ws-3") {
		t.Fatalf("header %q", first)
	}
	if !strings.Contains(out, "esc hosts") {
		t.Fatalf("no way back:\n%s", out)
	}
	m.opt.DialErr, m.blocked = remote.ErrNotConnected, remote.ErrNotConnected
	if out := ansi.Strip(m.render()); !strings.Contains(out, "esc hosts") {
		t.Fatalf("no way back from a blocked host:\n%s", out)
	}
}

// A changed host key can be an attack: the console offers no connect, only
// what to check, and Enter tries again once the builder removed the old key.
func TestChangedHostKeyOffersNoConnect(t *testing.T) {
	dials := 0
	m := New(Options{NoMotion: true, Host: "ws-1", Self: "/bin/matchblox", DialErr: remote.ErrHostKeyChanged,
		Redial: func() (transport.Conn, error) { dials++; return newFake(), nil }})
	m.opt.Exec = func(argv []string, done func(error) tea.Msg) tea.Cmd { t.Fatalf("ran %q", argv); return nil }
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	next, _ = next.Update(lostMsg{err: remote.ErrHostKeyChanged})
	out := ansi.Strip(next.(Model).render())
	if strings.Contains(out, "matchblox connect") || !strings.Contains(out, "ssh-keygen -R ws-1") || !strings.Contains(out, "⏎ try again") {
		t.Fatalf("view:\n%s", out)
	}
	next, cmd := key(next, "enter")
	if cmd == nil || next.(Model).blocked != nil {
		t.Fatal("enter did not try again")
	}
	if _, dial := next.Update(cmd()); dial != nil {
		dial()
	}
	if dials != 1 {
		t.Fatalf("dials %d", dials)
	}
}
