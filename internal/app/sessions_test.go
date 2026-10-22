package app

import (
	"reflect"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/blox-eng/matchblox/internal/proto"
	"github.com/blox-eng/matchblox/internal/queue"
	"github.com/blox-eng/matchblox/internal/sample"
)

// TestSessionsGroupByNeed: who waits, the oldest wait first; who works (a
// turn, subagents, a shell: tests when it runs a test runner); then the
// quiet ones, the newest first. One that waits since yesterday is quiet.
// Another sort is one flat list.
func TestSessionsGroupByNeed(t *testing.T) {
	st := fixtureState()
	mk := func(pid int, name string, f func(*sample.Session)) sample.Session {
		s := st.Sessions[0]
		s.PID, s.Name, s.Pane, s.Target = pid, name, "%"+name, "w:"+name
		s.Busy, s.Status, s.Idle, s.Do, s.Shells, s.Agents, s.Progress = false, "idle", time.Minute, "", nil, 0, nil
		f(&s)
		return s
	}
	st.Sessions = []sample.Session{
		mk(1, "quiet-new", func(s *sample.Session) { s.Idle = 5 * time.Minute }),
		mk(2, "quiet-old", func(s *sample.Session) { s.Idle = 3 * time.Hour }),
		mk(3, "asks-late", func(*sample.Session) {}),
		mk(4, "asks-early", func(*sample.Session) {}),
		mk(5, "busy", func(s *sample.Session) { s.Busy, s.Status = true, "busy" }),
		mk(6, "agents", func(s *sample.Session) { s.Busy, s.Status, s.Agents = true, sample.StatusAgents, 2 }),
		mk(7, "tests", func(s *sample.Session) { s.Shells = []sample.Shell{{PID: 70, Command: "go test ./..."}} }),
		mk(8, "shell", func(s *sample.Session) { s.Shells = []sample.Shell{{PID: 80, Command: "sleep 100"}} }),
		mk(9, "cold-asks", func(s *sample.Session) { s.Idle = 30 * time.Hour }),
	}
	st.Queue = []queue.Item{
		{Pane: "%asks-late", Name: "asks-late", State: queue.StateQuestion, Since: st.At.Add(-2 * time.Minute)},
		{Pane: "%asks-early", Name: "asks-early", State: queue.StateFinished, Since: st.At.Add(-20 * time.Minute)},
		{Pane: "%cold-asks", Name: "cold-asks", State: queue.StateQuestion, Since: st.At.Add(-30 * time.Hour)},
	}
	m, _ := loadedWith(t, 132, st)
	if got := order(m); got != "asks-early asks-late agents busy shell tests quiet-new quiet-old cold-asks" {
		t.Fatalf("order %q", got)
	}
	labels := []int{lineOf(t, m, "WAITS FOR YOU · 2"), lineOf(t, m, "WORKING · 4"), lineOf(t, m, "QUIET · 3")}
	if labels[0] > labels[1] || labels[1] > labels[2] {
		t.Fatalf("the groups are out of order %v:\n%s", labels, screen(m))
	}
	out := m.render()
	for name, word := range map[string]string{"asks-early": "done", "asks-late": "waits", "agents": "agents", "busy": "busy",
		"tests": "tests", "shell": "shell", "quiet-old": "idle", "cold-asks": "cold"} {
		if l := listOf(rowOf(out, name), 132); !strings.Contains(l, " "+word+" ") {
			t.Errorf("%s: row %q lacks %q", name, l, word)
		}
	}
	if s, _ := m.selectedSession(); s.Name != "asks-early" {
		t.Fatalf("the console opens on %s, not the first that waits", s.Name)
	}
	if h := m.head(132); !strings.Contains(h, "● 2 wait for you") {
		t.Fatalf("header %q", h)
	}
	next, _ := key(m, "s")
	if out := screen(next); !strings.Contains(out, "WORKING · 4") || !strings.Contains(out, "WAITS FOR YOU · 2  ·  by newest ▾") {
		t.Fatalf("another sort drops the groups:\n%s", out)
	}
	for range 3 {
		next, _ = key(next, "g")
	}
	if out := screen(next); strings.Contains(out, "WORKING") || !strings.Contains(out, "9 SESSIONS · by newest ▾") {
		t.Fatalf("no groups, one flat list:\n%s", out)
	}
}

// TestTabsAndTheirDigits: Sessions, Ships, Machine, Git, History, Panes,
// the digits in that order; p opens Ships, Tab and Shift+Tab cycle. Short
// of room the tabs far from the open one shorten to their digit.
func TestTabsAndTheirDigits(t *testing.T) {
	if want := []string{"sessions", "ships", "machine", "git", "history", "panes", "stoker"}; !reflect.DeepEqual(tabNames, want) {
		t.Fatalf("tabs %q, want %q", tabNames, want)
	}
	m, _ := loadedWith(t, 100, queueState())
	for i := range tabNames {
		if next, _ := key(m, string(rune('1'+i))); next.(Model).tab != i {
			t.Fatalf("%d opens tab %d", i+1, next.(Model).tab)
		}
	}
	next, _ := key(m, "p")
	if next.(Model).tab != tabShips || !strings.Contains(screen(next), "No ships yet.") {
		t.Fatalf("p does not open Ships:\n%s", screen(next))
	}
	if next, _ := m.key("tab"); next.(Model).tab != tabShips {
		t.Fatal("Tab does not open the next tab")
	}
	if next, _ := m.key("shift+tab"); next.(Model).tab != tabStoker {
		t.Fatal("Shift+Tab does not open the previous tab")
	}
	phone, _ := loadedWith(t, 44, queueState())
	for tab, want := range map[int]string{
		tabSessions: " SESSIONS 3  ships  machine  4  5  6",
		tabHistory:  " 1  2  3  git  HISTORY  panes  7",
	} {
		phone.tab = tab
		if line, _ := phone.tabsLine(44); !strings.HasPrefix(stripped(line), want) {
			t.Errorf("tab %d on a phone: %q, want %q", tab, stripped(line), want)
		}
	}
	// A detached busy loop marks the Machine tab.
	st := queueState()
	st.Orphans = []sample.Orphan{{PID: 4242, Comm: "bash"}}
	loops, _ := loadedWith(t, 100, st)
	if line, _ := loops.tabsLine(100); !strings.Contains(stripped(line), "machine !") {
		t.Fatalf("no alert on the Machine tab: %q", stripped(line))
	}
}

// reasonFixture is three sessions at rest, one of them in pane %3.
func reasonFixture() proto.State {
	st := sortFixture()
	for i := range st.Sessions {
		st.Sessions[i].SessionID = "sid-" + st.Sessions[i].Name
	}
	st.Sessions[2].Pane = "%3"
	st.Reasons = map[string]proto.IdleReason{"%alpha": {Word: "#4556 waits your merge", Tone: "accent"}}
	return st
}

func TestIdleReasonInRow(t *testing.T) {
	for _, w := range []int{44, 86, 132} {
		m, _ := loadedWith(t, w, reasonFixture())
		out := screen(m)
		if !strings.Contains(out, "#4556 waits your merge") {
			t.Fatalf("width %d: the reason is not in the row in full:\n%s", w, out)
		}
		if row := rowOf(m.render(), "alpha"); strings.Contains(row, " idle ") {
			t.Fatalf("width %d: the row keeps the idle word: %q", w, row)
		}
	}
}

func TestZParksAndHeldIsLast(t *testing.T) {
	st := reasonFixture()
	m, f := loadedWith(t, 100, st)
	m.selPID, m.chose = st.Sessions[2].PID, true
	_, cmd := key(m, "z")
	drain(cmd)
	acts := f.acts()
	if len(acts) != 1 || acts[0].RecID != "park:%3" {
		t.Fatalf("z sent %+v", acts)
	}
	st.Holds = []proto.Hold{{SessionID: "sid-charlie", Pane: "%3", Name: "charlie", By: "you"}}
	next, _ := m.Update(stateMsg(st))
	out := screen(next)
	if !strings.Contains(out, "HELD · 1") || !strings.Contains(out, "⏸ parked by you") {
		t.Fatalf("the parked session is not in HELD:\n%s", out)
	}
	if lineOf(t, next, "HELD · 1") < lineOf(t, next, "QUIET · 1") {
		t.Fatalf("HELD is not the last group:\n%s", out)
	}
	if got := order(next); got != "bravo alpha charlie" {
		t.Fatalf("order %q", got)
	}
}

func TestGCyclesGrouping(t *testing.T) {
	m, _ := loadedWith(t, 120, sortFixture())
	var next tea.Model = m
	next, _ = key(next, "1")
	for _, want := range []struct{ mode, label string }{
		{"status", "BUSY · 1"}, {"repo", "· 3"}, {"none", "3 SESSIONS"}, {"need", "WORKING · 1"},
	} {
		next, _ = key(next, "g")
		if got := next.(Model).flash; got != "group: "+want.mode {
			t.Fatalf("flash %q, want group: %s", got, want.mode)
		}
		if out := screen(next); !strings.Contains(out, want.label) {
			t.Fatalf("group %s lacks %q:\n%s", want.mode, want.label, out)
		}
	}
	// The sort works inside each group: by name, reversed.
	next, _ = key(next, "g") // status
	next, _ = key(next, "g") // repo: one repo holds all three
	for range 4 {
		next, _ = key(next, "s")
	}
	if got := order(next); got != "alpha bravo charlie" {
		t.Fatalf("by name inside the group: %q", got)
	}
	next, _ = key(next, "S")
	if got := order(next); got != "charlie bravo alpha" {
		t.Fatalf("by name, reversed: %q", got)
	}
}

func TestSortSaysNewest(t *testing.T) {
	m, _ := loadedWith(t, 100, sortFixture())
	var next tea.Model = m
	next, _ = key(next, "1")
	next, _ = key(next, "s")
	if got := next.(Model).flash; got != "sort: newest" {
		t.Fatalf("flash %q", got)
	}
	next, _ = key(next, "S")
	if got := next.(Model).flash; got != "sort: newest, reversed" {
		t.Fatalf("flash %q", got)
	}
}

// TestTheStokersSessionStaysOutOfSessions: #81, the stoker's own agent is
// not a lane. It leaves the list and its counts, and the header says when
// it waits for you, so a wait is never hidden.
func TestTheStokersSessionStaysOutOfSessions(t *testing.T) {
	for _, w := range []int{44, 132} {
		st := reasonFixture()
		st.StokerServer = "mbtest"
		st.Sessions[0].Pane, st.Sessions[0].Target = st.Sessions[0].Pane+"@mbtest", "stoker:1.0"
		m, _ := loadedWith(t, w, st)
		if rowOf(m.render(), "alpha") != "" {
			t.Fatalf("width %d: the stoker's session is a row of Sessions", w)
		}
		for _, s := range m.snap.Sessions {
			if s.Server() == "mbtest" {
				t.Fatalf("width %d: the stoker's session is in the list", w)
			}
		}
		st.Sessions[0].Asks = true
		st.Stoker = &proto.StokerView{Mode: proto.StokerMode{On: true}}
		m, _ = loadedWith(t, w, st)
		if h := ansi.Strip(m.head(w)); !strings.Contains(h, "stoker waits") {
			t.Fatalf("width %d: the stoker waits for you and the header does not say so: %q", w, h)
		}
	}
}

func TestASessionOutsideTmuxSaysSo(t *testing.T) {
	st := lifeState()
	st.Sessions = []sample.Session{{Name: "loose", Cwd: "/work/app", Status: "idle"}}
	m, f := loadedWith(t, 100, st)
	if s := screen(m); !strings.Contains(s, "outside tmux") {
		t.Fatalf("the row names no place:\n%s", s)
	}
	for _, k := range []string{"A", "r", "z", "x"} {
		next, cmd := key(m, k)
		if cmd != nil {
			cmd()
		}
		if nm := next.(Model); nm.form != nil || nm.pending != nil {
			t.Fatalf("%s opened %+v %+v", k, nm.form, nm.pending)
		}
	}
	if acts := f.acts(); len(acts) != 0 {
		t.Fatalf("sent %+v", acts)
	}
}

func TestAShipWithoutADriverIsNotOutsideTmux(t *testing.T) {
	m, _ := loadedWith(t, 100, fixtureState())
	sh := proto.Ship{Number: 7}
	for _, got := range []string{m.driverName(sh), m.driverGone(sh)} {
		if strings.Contains(got, "outside tmux") {
			t.Fatalf("%q", got)
		}
	}
	if got := m.driverGone(sh); got != "its driver no longer runs" {
		t.Fatalf("%q", got)
	}
}
