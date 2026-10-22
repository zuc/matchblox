package app

import (
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/blox-eng/matchblox/internal/proto"
	"github.com/blox-eng/matchblox/internal/queue"
	"github.com/blox-eng/matchblox/internal/sample"
)

// TestExitedAgentsStayQuiet: exited agents are one faint line under the
// sessions; e lists them with the parent that keeps them and the fix.
func TestExitedAgentsStayQuiet(t *testing.T) {
	for _, w := range []int{50, 120} {
		st := fixtureState()
		for pid := 7000; pid < 7137; pid++ {
			st.Exited = append(st.Exited, sample.Exited{PID: pid, Parent: 3214249, ParentComm: "systemd"})
		}
		m, _ := loadedWith(t, w, st)
		var next tea.Model = m
		next, _ = key(next, "1")
		out := screen(next)
		if !strings.Contains(out, "137 exited agents · e shows them") || strings.Contains(out, "7000") {
			t.Fatalf("width %d: the exited agents are not one quiet line:\n%s", w, out)
		}
		next, _ = key(next, "e")
		out = screen(next)
		for _, want := range []string{"137 EXITED AGENTS", "systemd (pid 3214249) has not reaped them", "systemctl --user daemon-reexec", "7000"} {
			if !strings.Contains(out, want) {
				t.Fatalf("width %d: expanded lacks %q:\n%s", w, want, out)
			}
		}
		fitsWidth(t, next.(Model), w)
		next, _ = key(next, "e")
		if strings.Contains(screen(next), "7000") {
			t.Fatal("e did not fold them again")
		}
	}
}

func agingState() proto.State {
	st := sortFixture()
	st.Sessions = append(st.Sessions, st.Sessions[0], st.Sessions[0])
	st.Sessions[3].PID, st.Sessions[3].Name, st.Sessions[3].Pane, st.Sessions[3].Idle = 4, "delta", "%delta", 30*time.Hour
	st.Sessions[4].PID, st.Sessions[4].Name, st.Sessions[4].Pane, st.Sessions[4].Idle = 5, "echo", "%echo", 8*24*time.Hour
	// charlie (idle 1h) waits for the person: it goes above alpha (10 min).
	st.Queue = []queue.Item{{Pane: "%charlie", Name: "charlie", State: queue.StateQuestion, Since: st.At.Add(-time.Hour)}}
	return st
}

// TestIdleSessionsAge: who waits for the person, then who works, then idle
// by the newest; a day makes a session cold, a week makes it stale.
func TestIdleSessionsAge(t *testing.T) {
	for _, w := range []int{50, 120} {
		m, _ := loadedWith(t, w, agingState())
		next, _ := key(m, "1")
		if got := order(next); got != "charlie bravo alpha delta echo" {
			t.Fatalf("width %d: order %q", w, got)
		}
		lines := strings.Split(screen(next), "\n")
		for name, word := range map[string]string{"alpha": "idle", "delta": "cold", "echo": "stale"} {
			if l := lines[lineOf(t, next, name)]; !strings.Contains(l, word) {
				t.Fatalf("width %d: %s is not %s: %q", w, name, word, l)
			}
		}
	}
}

// TestXKillsASession: x on any session kills it after a typed y, with
// the exact commands on the RUN line; one that waits for you asks for a
// second y and says why.
func TestXKillsASession(t *testing.T) {
	st := agingState()
	st.Sessions[4].Status = "idle" // echo rests: one y kills it
	m, f := loadedWith(t, 160, st)
	var next tea.Model = m
	next, _ = key(next, "1")
	next, _ = key(next, "x") // charlie: it waits
	if line := actionLine(next); !strings.Contains(line, "Kill charlie: tmux kill-pane -t %charlie") {
		t.Fatalf("x on charlie: %q", line)
	}
	next, cmd := key(next, "y")
	if cmd != nil {
		cmd()
	}
	if line := actionLine(next); len(f.acts()) != 0 || !strings.Contains(line, "charlie waits for you: y again kills it all the same") {
		t.Fatalf("one y killed a session that waits: %q %+v", line, f.acts())
	}
	_, cmd = key(next, "y")
	if cmd == nil {
		t.Fatal("y sent nothing")
	}
	cmd()
	if acts := f.acts(); len(acts) != 1 || acts[0] != (proto.Act{RecID: "kill:%charlie", Which: "secondary", Confirm: "y", Text: "anyway"}) {
		t.Fatalf("sent %+v", acts)
	}
	next = selectPane(t, next, "%echo")
	if !strings.Contains(actionLine(next), "x kill") {
		t.Fatalf("the keys of a session lack x: %q", actionLine(next))
	}
	next, _ = key(next, "x")
	if line := actionLine(next); !strings.Contains(line, "Kill echo: tmux kill-pane -t %echo then kill -TERM 5") {
		t.Fatalf("x on echo: %q", line)
	}
	_, cmd = key(next, "y")
	cmd()
	if acts := f.acts(); len(acts) != 2 || acts[1] != (proto.Act{RecID: "kill:%echo", Which: "secondary", Confirm: "y"}) {
		t.Fatalf("sent %+v", acts)
	}
}

// TestOldSessionsSinkEvenWhenQueued: a queue item a day old does not lift a
// cold session over a fresh one: age outranks waiting (found live: without
// hooks the queue holds a "done" item for nearly every idle session).
func TestOldSessionsSinkEvenWhenQueued(t *testing.T) {
	st := agingState()
	st.Queue = append(st.Queue, queue.Item{Pane: "%delta", Name: "delta", State: queue.StateFinished, Since: st.At.Add(-30 * time.Hour)})
	m, _ := loadedWith(t, 120, st)
	next, _ := key(m, "1")
	if got := order(next); got != "charlie bravo alpha delta echo" {
		t.Fatalf("order %q", got)
	}
}

// TestTheFixFitsTheParent: zombies of PID 1 need the system manager, not
// the user one; one agent is "agent".
func TestTheFixFitsTheParent(t *testing.T) {
	st := fixtureState()
	st.Exited = []sample.Exited{{PID: 9001, Parent: 1, ParentComm: "systemd"}}
	m, _ := loadedWith(t, 120, st)
	next, _ := key(m, "1")
	if out := screen(next); !strings.Contains(out, "1 exited agent · e shows it") {
		t.Fatalf("singular:\n%s", out)
	}
	next, _ = key(next, "e")
	out := screen(next)
	if !strings.Contains(out, "sudo systemctl daemon-reexec reaps them") || strings.Contains(out, "--user") {
		t.Fatalf("pid 1 fix:\n%s", out)
	}
}

// A session the plan's limit stops says until when, and is quiet: nothing
// to do there before its reset.
func TestAnOutSessionSaysUntilWhen(t *testing.T) {
	st := fixtureState()
	back := st.At.Add(2 * time.Hour)
	for i := range st.Sessions {
		if !st.Sessions[i].Busy {
			st.Sessions[i].Out, st.Sessions[i].Idle = back, 10*time.Minute
			break
		}
	}
	m, _ := loadedWith(t, 120, st)
	m.tab = tabSessions
	at := back.Local().Format("15:04")
	if out := ansi.Strip(m.render()); !regexp.MustCompile(`out +` + at).MatchString(out) {
		t.Fatalf("no out %s in\n%s", at, out)
	}
	if g := m.groupOf(sample.Session{Out: back}); g != groupQuiet {
		t.Fatalf("an out session is in group %d", g)
	}
}

func TestStatusCountsTheOutSessions(t *testing.T) {
	st := fixtureState()
	n := 0
	for i := range st.Sessions {
		if !st.Sessions[i].Busy && n == 0 {
			st.Sessions[i].Out, st.Sessions[i].Idle, n = st.At.Add(time.Hour), 10*time.Minute, 1
		}
	}
	if out := Summary(st.Doc); !strings.Contains(out, ", out 1)") {
		t.Fatalf("%s", out)
	}
}
