package app

import (
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/blox-eng/matchblox/internal/proto"
	"github.com/blox-eng/matchblox/internal/state"
)

// focused is the fixture's state with a focus on the session in pane.
func focused(pane string, at time.Time) proto.State {
	st := fixtureState()
	for _, s := range st.Sessions {
		if s.Pane == pane {
			st.Focus = &state.Focus{SessionID: s.SessionID, Pane: s.Pane, At: at}
		}
	}
	return st
}

// A tap on the island or a buzz for a session that waits opens its answer
// peek, from any tab, once for each focus.
func TestFocusOpensThePeekOfAWait(t *testing.T) {
	m, _ := loadedWith(t, 100, fixtureState())
	m.setTab(tabGit)
	at := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	next, _ := m.Update(stateMsg(focused("%2", at)))
	nm := next.(Model)
	if nm.tab != tabSessions || nm.peek == nil || nm.peek.pane != "%2" {
		t.Fatalf("tab %d peek %+v: want the peek of app-review", nm.tab, nm.peek)
	}
	if s, ok := nm.selectedSession(); !ok || s.Pane != "%2" {
		t.Fatalf("selected %q", s.Pane)
	}
	// The next state carries the same focus: the closed peek stays closed.
	nm.peek = nil
	next, _ = nm.Update(stateMsg(focused("%2", at)))
	if next.(Model).peek != nil {
		t.Fatal("the same focus opened the peek again")
	}
	// A new focus opens it again.
	next, _ = next.Update(stateMsg(focused("%2", at.Add(time.Second))))
	if next.(Model).peek == nil {
		t.Fatal("a new focus did not open the peek")
	}
}

// A session at work has no answer to give: the focus opens its pane on the
// phone's own tmux client, once that client is attached, and on no other.
func TestFocusOpensThePaneOfABurn(t *testing.T) {
	m, _ := loadedWith(t, 100, fixtureState())
	var ran []string
	m.opt.Run = func(argv []string) error { ran = append(ran, strings.Join(argv, " ")); return nil }
	asks := 0
	m.opt.Clients = func() ([]string, error) {
		asks++
		if asks < 3 { // the phone's client attaches a moment after the act
			return []string{"/dev/ttys001"}, nil
		}
		return []string{"/dev/ttys001", "/dev/ttys004"}, nil
	}
	st := focused("%1", time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC))
	st.Focus.Client = "/dev/ttys004"
	next, cmd := m.Update(stateMsg(st))
	nm := next.(Model)
	if nm.peek != nil {
		t.Fatalf("a session at work got the peek: %+v", nm.peek)
	}
	if s, ok := nm.selectedSession(); !ok || s.Pane != "%1" || nm.tab != tabSessions {
		t.Fatalf("selected %q on tab %d", s.Pane, nm.tab)
	}
	runFocus(t, cmd)
	if len(ran) != 1 || ran[0] != "tmux switch-client -c /dev/ttys004 -t %1" || asks < 3 {
		t.Fatalf("ran %q after %d asks", ran, asks)
	}
}

// runFocus runs the commands of a state update, giving each up to one
// seconds: the focus waits for the phone's client.
func runFocus(t *testing.T, cmd tea.Cmd) {
	t.Helper()
	if cmd == nil {
		return
	}
	out := make(chan tea.Msg, 1)
	go func() { out <- cmd() }()
	select {
	case msg := <-out:
		if b, ok := msg.(tea.BatchMsg); ok {
			for _, c := range b {
				runFocus(t, c)
			}
		}
	case <-time.After(time.Second):
	}
}

// A console whose tmux has no such client, a focus without one, a remote
// console and one outside tmux never move a terminal: the peek is shared,
// a jump is not.
func TestFocusMovesOnlyThePhonesClient(t *testing.T) {
	old := focusWait
	focusWait = 300 * time.Millisecond
	t.Cleanup(func() { focusWait = old })
	at := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	for _, c := range []struct {
		name   string
		client string
		opt    func(*Options)
	}{
		{"another server", "/dev/ttys004", func(*Options) {}},
		{"no client", "", func(*Options) {}},
		{"remote", "/dev/ttys004", func(o *Options) { o.Host = "ws-1" }},
		{"outside tmux", "/dev/ttys004", func(o *Options) { o.OutsideTmux = true }},
	} {
		m, _ := loadedWith(t, 100, fixtureState())
		var ran []string
		m.opt.Run = func(argv []string) error { ran = append(ran, strings.Join(argv, " ")); return nil }
		m.opt.Exec = func(argv []string, done func(error) tea.Msg) tea.Cmd {
			ran = append(ran, strings.Join(argv, " "))
			return nil
		}
		m.opt.Clients = func() ([]string, error) { return []string{"/dev/ttys001"}, nil }
		if c.name != "another server" {
			m.opt.Clients = func() ([]string, error) { return []string{"/dev/ttys001", "/dev/ttys004"}, nil }
		}
		c.opt(&m.opt)
		st := focused("%1", at)
		st.Focus.Client = c.client
		next, cmd := m.Update(stateMsg(st))
		runFocus(t, cmd)
		if s, ok := next.(Model).selectedSession(); !ok || s.Pane != "%1" || len(ran) != 0 {
			t.Errorf("%s: selected %q, ran %q", c.name, s.Pane, ran)
		}
	}
}

// A number whose session ended says so, and nothing else moves.
func TestFocusOfAGoneSessionSaysSo(t *testing.T) {
	m, _ := loadedWith(t, 100, fixtureState())
	var ran []string
	m.opt.Run = func(argv []string) error { ran = append(ran, strings.Join(argv, " ")); return nil }
	m.setTab(tabGit)
	at := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	for _, f := range []*state.Focus{
		{SessionID: "gone-session", Gone: true, At: at},
		// The service saw it, but this console's state no longer has it.
		{SessionID: "22222222-bbbb-4000-8000-000000000009", Pane: "%9", At: at.Add(time.Second)},
	} {
		st := fixtureState()
		st.Focus = f
		next, cmd := m.Update(stateMsg(st))
		nm := next.(Model)
		drain(cmd)
		if nm.flash != "That session ended." || nm.peek != nil || len(ran) != 0 || nm.tab != tabSessions {
			t.Fatalf("%+v: flash %q peek %+v ran %q tab %d", f, nm.flash, nm.peek, ran, nm.tab)
		}
		nm.setTab(tabGit)
		m = nm
	}
}

// Without a focus nothing changes: the console stays where it is.
func TestNoFocusLeavesTheConsoleAlone(t *testing.T) {
	m, _ := loadedWith(t, 100, fixtureState())
	m.setTab(tabGit)
	next, _ := m.Update(stateMsg(fixtureState()))
	if nm := next.(Model); nm.tab != tabGit || nm.peek != nil || nm.flash != "" {
		t.Fatalf("tab %d peek %+v flash %q", nm.tab, nm.peek, nm.flash)
	}
}
