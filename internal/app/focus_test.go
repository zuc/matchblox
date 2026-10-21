package app

import (
	"strings"
	"testing"
	"time"

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

// A session at work has no answer to give: the focus opens its pane.
func TestFocusOpensThePaneOfABurn(t *testing.T) {
	m, _ := loadedWith(t, 100, fixtureState())
	var ran []string
	m.opt.Run = func(argv []string) error { ran = append(ran, strings.Join(argv, " ")); return nil }
	next, cmd := m.Update(stateMsg(focused("%1", time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC))))
	nm := next.(Model)
	if nm.peek != nil {
		t.Fatalf("a session at work got the peek: %+v", nm.peek)
	}
	if s, ok := nm.selectedSession(); !ok || s.Pane != "%1" {
		t.Fatalf("selected %q", s.Pane)
	}
	drain(cmd)
	if len(ran) != 1 || ran[0] != "tmux switch-client -t %1" {
		t.Fatalf("ran %q", ran)
	}
}

// A number whose session ended says so, and nothing else moves.
func TestFocusOfAGoneSessionSaysSo(t *testing.T) {
	m, _ := loadedWith(t, 100, fixtureState())
	var ran []string
	m.opt.Run = func(argv []string) error { ran = append(ran, strings.Join(argv, " ")); return nil }
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
		if nm.flash != "That session ended." || nm.peek != nil || len(ran) != 0 {
			t.Fatalf("%+v: flash %q peek %+v ran %q", f, nm.flash, nm.peek, ran)
		}
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
