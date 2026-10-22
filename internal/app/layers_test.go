package app

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/blox-eng/matchblox/internal/remote"
	"github.com/blox-eng/matchblox/internal/transport"
)

// shellFor is a shell whose consoles talk to fakes; opened records the hosts
// it opened, ran what it gave the terminal to.
type shellRig struct {
	sh     Shell
	opened []string
	conns  map[string]*fakeConn
	ran    [][]string
	added  []string
}

func newShell(t *testing.T, added []string, ssh []string, start string, firstRun bool) *shellRig {
	t.Helper()
	r := &shellRig{added: added, conns: map[string]*fakeConn{}}
	r.sh = NewShell(ShellOptions{
		Base:     Options{NoMotion: true, Self: "/bin/matchblox"},
		Hosts:    func() []string { return slices.Clone(r.added) },
		SSHHosts: func() []string { return ssh },
		Open: func(host string) Options {
			r.opened = append(r.opened, host)
			f := newFake()
			r.conns[host] = f
			return Options{Conn: f, Host: host}
		},
		Exec: func(argv []string, done func(error) tea.Msg) tea.Cmd {
			r.ran = append(r.ran, argv)
			if len(argv) == 3 && argv[1] == "connect" {
				r.added = append(r.added, argv[2]) // what matchblox connect writes
			}
			return func() tea.Msg { return done(nil) }
		},
		Start:    start,
		FirstRun: firstRun,
	})
	next, _ := r.sh.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	r.sh = next.(Shell)
	return r
}

func (r *shellRig) key(t *testing.T, k string) {
	t.Helper()
	next, cmd := key(r.sh, k)
	r.sh = next.(Shell)
	r.drain(cmd)
}

// drain runs what a key asked for and feeds back the shell's own messages.
// A console's reads block on the fake, so each command gets 100 ms.
func (r *shellRig) drain(cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	got := make(chan tea.Msg, 1)
	go func() { got <- cmd() }()
	var msg tea.Msg
	select {
	case msg = <-got:
	case <-time.After(100 * time.Millisecond):
		return
	}
	switch msg := msg.(type) {
	case tea.BatchMsg:
		for _, c := range msg {
			r.drain(c)
		}
	case layerMsg, connectedHostMsg:
		next, cmd := r.sh.Update(msg)
		r.sh = next.(Shell)
		r.drain(cmd)
	}
}

func (r *shellRig) view() string { return ansi.Strip(r.sh.render()) }

func TestNoHostsStartsInThisMachinesConsole(t *testing.T) {
	r := newShell(t, nil, nil, "", false)
	if r.sh.layer != layerConsole || !slices.Equal(r.opened, []string{""}) {
		t.Fatalf("layer %v, opened %q", r.sh.layer, r.opened)
	}
}

func TestHostsStartOnTheHostsLayer(t *testing.T) {
	r := newShell(t, []string{"ws-1", "ws-2"}, nil, "", false)
	out := r.view()
	for _, want := range []string{"matchblox · hosts", "this machine", "ws-1", "ws-2", "+ add a host"} {
		if !strings.Contains(out, want) {
			t.Fatalf("view lacks %q:\n%s", want, out)
		}
	}
	if len(r.opened) != 0 {
		t.Fatalf("opened %q before a pick", r.opened)
	}
}

func TestEnterOpensAHostAndEscComesBack(t *testing.T) {
	r := newShell(t, []string{"ws-1"}, nil, "", false)
	r.key(t, "down")
	r.key(t, "enter")
	if r.sh.layer != layerConsole || !slices.Equal(r.opened, []string{"ws-1"}) {
		t.Fatalf("layer %v, opened %q", r.sh.layer, r.opened)
	}
	next, _ := r.sh.Update(stateMsg(fixtureState()))
	r.sh = next.(Shell)
	if first := strings.Split(r.view(), "\n")[0]; !strings.Contains(first, "matchblox · ws-1") {
		t.Fatalf("header %q", first)
	}
	r.key(t, "esc")
	if r.sh.layer != layerHosts || !r.conns["ws-1"].closed {
		t.Fatalf("esc: layer %v, closed %v", r.sh.layer, r.conns["ws-1"].closed)
	}
	if r.sh.sel != 1 {
		t.Fatalf("the hosts layer must keep ws-1 selected, sel %d", r.sh.sel)
	}
}

func TestEscClearsTheSearchBeforeItLeaves(t *testing.T) {
	r := newShell(t, []string{"ws-1"}, nil, "ws-1", false)
	next, _ := r.sh.Update(stateMsg(fixtureState()))
	r.sh = next.(Shell)
	r.key(t, "/")
	r.key(t, "a")
	r.key(t, "enter")
	r.key(t, "esc")
	if r.sh.layer != layerConsole {
		t.Fatal("esc left the console while a search was on")
	}
	r.key(t, "esc")
	if r.sh.layer != layerHosts {
		t.Fatal("the second esc did not go back")
	}
}

func TestAHostArgumentOpensItDirectly(t *testing.T) {
	r := newShell(t, nil, nil, "ws-9", false)
	if r.sh.layer != layerConsole || !slices.Equal(r.opened, []string{"ws-9"}) {
		t.Fatalf("layer %v, opened %q", r.sh.layer, r.opened)
	}
}

func TestAddAHostConnectsThenOpensIt(t *testing.T) {
	r := newShell(t, []string{"ws-1"}, []string{"ws-1", "ws-2", "github.com"}, "", false)
	r.key(t, "down")
	r.key(t, "down")
	r.key(t, "enter") // + add a host
	out := r.view()
	if !strings.Contains(out, "ws-2") || !strings.Contains(out, "type a host") || strings.Count(out, "ws-1") != 0 {
		t.Fatalf("the picker must list the ssh hosts not added yet:\n%s", out)
	}
	r.key(t, "enter") // ws-2
	if !strings.Contains(r.view(), "/bin/matchblox connect ws-2") || len(r.ran) != 0 {
		t.Fatalf("enter must show the connect and wait:\n%s", r.view())
	}
	r.key(t, "y")
	if len(r.ran) != 1 || strings.Join(r.ran[0], " ") != "/bin/matchblox connect ws-2" {
		t.Fatalf("ran %q", r.ran)
	}
	if r.sh.layer != layerConsole || r.opened[len(r.opened)-1] != "ws-2" {
		t.Fatalf("after the connect: layer %v, opened %q", r.sh.layer, r.opened)
	}
}

func TestTypeAHost(t *testing.T) {
	r := newShell(t, nil, nil, "", true)
	r.key(t, "down")
	r.key(t, "enter") // another machine
	r.key(t, "enter") // type a host (the only row)
	for _, k := range strings.Split("me@ws-3", "") {
		r.key(t, k)
	}
	r.key(t, "enter")
	r.key(t, "y")
	if len(r.ran) != 1 || strings.Join(r.ran[0], " ") != "/bin/matchblox connect me@ws-3" {
		t.Fatalf("ran %q\n%s", r.ran, r.view())
	}
}

func TestATypedHostMustBeAHostName(t *testing.T) {
	r := newShell(t, nil, nil, "", true)
	r.key(t, "down")
	r.key(t, "enter")
	r.key(t, "enter")
	for _, k := range strings.Split("-oProxyCommand=x", "") {
		r.key(t, k)
	}
	r.key(t, "enter")
	if !strings.Contains(r.view(), "not a host name") {
		t.Fatalf("no refusal:\n%s", r.view())
	}
	r.key(t, "y")
	if len(r.ran) != 0 {
		t.Fatalf("ran %q", r.ran)
	}
}

func TestFirstRunAsksWhereTheAgentsRun(t *testing.T) {
	r := newShell(t, nil, nil, "", true)
	out := r.view()
	for _, want := range []string{"WHERE DO YOUR AGENTS RUN?", "this machine", "another machine"} {
		if !strings.Contains(out, want) {
			t.Fatalf("view lacks %q:\n%s", want, out)
		}
	}
	answered := false
	r.sh.opt.Answered = func() { answered = true }
	r.key(t, "enter")
	if r.sh.layer != layerConsole || !slices.Equal(r.opened, []string{""}) || !answered {
		t.Fatalf("this machine: layer %v, opened %q, answered %v", r.sh.layer, r.opened, answered)
	}
	r.key(t, "esc")
	if out := r.view(); strings.Contains(out, "WHERE DO YOUR AGENTS RUN?") || !strings.Contains(out, "+ add a host") {
		t.Fatalf("after the answer, Hosts is the plain list:\n%s", out)
	}
}

func TestRemoveAHostNeedsAY(t *testing.T) {
	removed := ""
	r := newShell(t, []string{"ws-1"}, nil, "", false)
	r.sh.opt.Remove = func(h string) error { removed = h; return nil }
	r.key(t, "down")
	r.key(t, "x")
	if removed != "" || !strings.Contains(r.view(), "╭─ Remove") || !strings.Contains(r.view(), "remove ws-1") {
		t.Fatalf("x must ask first, in a box:\n%s", r.view())
	}
	r.key(t, "y")
	if removed != "ws-1" {
		t.Fatalf("removed %q", removed)
	}
}

func TestOldConsoleMessagesDoNotReachTheNewOne(t *testing.T) {
	r := newShell(t, []string{"ws-1", "ws-2"}, nil, "ws-1", false)
	old := r.conns["ws-1"]
	r.key(t, "esc")
	r.key(t, "down") // from ws-1 to ws-2
	r.key(t, "enter")
	if r.curHostIs(t) != "ws-2" {
		t.Fatalf("opened %q", r.opened)
	}
	next, _ := r.sh.Update(fromConn{old, stateMsg(fixtureState())})
	r.sh = next.(Shell)
	if r.sh.cur.have {
		t.Fatal("a state of ws-1 reached the console of ws-2")
	}
	redials := 0
	r.sh.cur.opt.Redial = func() (transport.Conn, error) { redials++; return newFake(), nil }
	next, cmd := r.sh.Update(redialMsg{})
	r.sh = next.(Shell)
	if cmd != nil {
		cmd()
	}
	if redials != 0 {
		t.Fatal("a redial tick of the closed console dialed again")
	}
}

func (r *shellRig) curHostIs(t *testing.T) string {
	t.Helper()
	if r.sh.layer != layerConsole {
		t.Fatal("no console open")
	}
	return r.sh.curHost
}

// A console that was redialing when the builder left it must not hand its
// late connection, or its failure, to the console opened next.
func TestALateRedialOfAClosedConsoleIsDropped(t *testing.T) {
	r := newShell(t, []string{"ws-1"}, nil, "ws-1", false)
	gen := r.sh.cur.opt.Gen
	r.key(t, "esc")
	late := newFake()
	next, _ := r.sh.Update(connMsg{conn: late, gen: gen}) // on Hosts
	r.sh = next.(Shell)
	if !late.closed {
		t.Fatal("a late connection that reached Hosts was left open")
	}
	r.key(t, "enter") // ws-1, still selected: a new console
	if r.sh.layer != layerConsole || r.sh.cur.opt.Gen == gen {
		t.Fatalf("no new console: layer %v, gen %d", r.sh.layer, r.sh.cur.opt.Gen)
	}
	cur := r.sh.cur.conn
	late = newFake()
	next, _ = r.sh.Update(connMsg{conn: late, gen: gen})
	r.sh = next.(Shell)
	if r.sh.cur.conn != cur || !late.closed {
		t.Fatalf("the old console's connection replaced the new one's (closed %v)", late.closed)
	}
	next, _ = r.sh.Update(redialFailed{err: remote.ErrNotConnected, gen: gen})
	r.sh = next.(Shell)
	if r.sh.cur.blocked != nil || r.sh.cur.lost {
		t.Fatal("the old console's failure reached the new one")
	}
}

// A long ~/.ssh/config fills more rows than the screen has: the list
// follows the selection, so the last row is reachable.
func TestALongHostListScrolls(t *testing.T) {
	var many []string
	for i := range 40 {
		many = append(many, fmt.Sprintf("ws-%02d", i))
	}
	r := newShell(t, nil, many, "", true)
	next, _ := r.sh.Update(tea.WindowSizeMsg{Width: 100, Height: 12})
	r.sh = next.(Shell)
	r.key(t, "down")
	r.key(t, "enter") // another machine
	for range 40 {
		r.key(t, "down")
	}
	out := r.view()
	if !strings.Contains(out, "▌ type a host…") || len(strings.Split(out, "\n")) > 12 {
		t.Fatalf("the last row is off the screen:\n%s", out)
	}
	r.key(t, "enter")
	if r.sh.input == nil {
		t.Fatal("enter on the last row did not start typing")
	}
}
