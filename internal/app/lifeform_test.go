package app

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/blox-eng/matchblox/internal/proto"
)

func lifeState() proto.State {
	st := queueState()
	st.Agents = []string{"claude", "codex"}
	st.Sessions[0].Cwd = "/src/api"
	return st
}

func TestNOpensNewSessionWhereTheSessionWorks(t *testing.T) {
	m, f := loadedWith(t, 100, lifeState())
	next, _ := key(m, "N")
	fm := next.(Model).form
	if fm == nil || fm.rename || fm.where != "/src/api" || fm.agent != "claude" || fm.at != fieldWhere {
		t.Fatalf("form %+v", fm)
	}
	if s := screen(next); !strings.Contains(s, "New session") || !strings.Contains(s, "blank: no new worktree") || !strings.Contains(s, "● claude") {
		t.Fatalf("the form:\n%s", s)
	}
	// branch, then the agent (→ picks codex), then the first message.
	next, _ = key(next, "tab")
	next = typeText(next, "feat/88")
	if fm := next.(Model).form; fm.autoName() != "api-feat-88" || !strings.Contains(screen(next), "api-feat-88") {
		t.Fatalf("name %q:\n%s", fm.autoName(), screen(next))
	}
	next, _ = key(next, "tab")
	next, _ = key(next, "tab")
	next, _ = key(next, "right")
	next, _ = key(next, "tab")
	next = typeText(next, "fix it")
	next, cmd := key(next, "enter")
	cmd()
	acts := f.acts()
	if len(acts) != 1 || acts[0].RecID != "new:session" || acts[0].Confirm != "y" {
		t.Fatalf("sent %+v", acts)
	}
	var n proto.NewSession
	if err := json.Unmarshal([]byte(acts[0].Text), &n); err != nil || n != (proto.NewSession{Where: "/src/api", Branch: "feat/88", Agent: "codex", First: "fix it"}) {
		t.Fatalf("%+v %v", n, err)
	}
	// A refusal stays in the form; a start closes it and says so.
	id := next.(Model).form.act
	next, _ = next.Update(resultMsg(proto.Result{ActID: id, Err: "the branch feat/88 is there already: start the session in its worktree"}))
	if fm := next.(Model).form; fm == nil || !strings.Contains(screen(next), "the branch feat/88 is there already") {
		t.Fatalf("refusal: %+v", fm)
	}
	next, cmd = key(next, "enter")
	cmd()
	next, _ = next.Update(resultMsg(proto.Result{ActID: next.(Model).form.act, Ran: [][]string{{"tmux"}}}))
	if nm := next.(Model); nm.form != nil || !strings.Contains(nm.flash, "started api-feat-88") || nm.lastAgent != "codex" {
		t.Fatalf("form %+v flash %q", nm.form, nm.flash)
	}
	// The next N starts from the agent this console picked last.
	if next, _ = key(next, "N"); next.(Model).form.agent != "codex" {
		t.Fatal("the last agent is not remembered")
	}
}

func TestNewSessionEscSendsNothing(t *testing.T) {
	m, f := loadedWith(t, 44, lifeState())
	next, _ := key(m, "N")
	next = typeText(next, "x")
	next, _ = key(next, "esc")
	if next.(Model).form != nil || len(f.acts()) != 0 {
		t.Fatal("esc sent or kept the form")
	}
}

func TestNewSessionWithNoAgentSaysWhy(t *testing.T) {
	m, _ := loadedWith(t, 100, queueState())
	next, _ := key(m, "N")
	if nm := next.(Model); nm.form != nil || nm.flash != "no agent to start here: install claude, codex, opencode or cursor-agent" {
		t.Fatalf("form %+v flash %q", nm.form, nm.flash)
	}
}

func TestRRenamesTheSession(t *testing.T) {
	m, f := loadedWith(t, 100, lifeState())
	next, _ := key(m, "r")
	fm := next.(Model).form
	if fm == nil || !fm.rename || fm.pane != "%1" {
		t.Fatalf("form %+v", fm)
	}
	for range len(fm.name) {
		next, _ = key(next, "backspace")
	}
	next = typeText(next, "a.b")
	next, _ = key(next, "enter")
	if fm := next.(Model).form; fm == nil || !strings.Contains(fm.err, "a name is letters") || len(f.acts()) != 0 {
		t.Fatalf("a bad name was sent: %+v", fm)
	}
	for range 3 {
		next, _ = key(next, "backspace")
	}
	next = typeText(next, "ships")
	next, cmd := key(next, "enter")
	cmd()
	if acts := f.acts(); len(acts) != 1 || acts[0] != (proto.Act{RecID: "rename:%1", Confirm: "y", Text: "ships"}) {
		t.Fatalf("sent %+v", acts)
	}
	next, _ = next.Update(resultMsg(proto.Result{ActID: next.(Model).form.act, Skipped: []string{"alpha renames when it rests"}}))
	if nm := next.(Model); nm.form != nil || nm.flash != "alpha renames when it rests" {
		t.Fatalf("form %+v flash %q", nm.form, nm.flash)
	}
}

func TestATapPicksAnAgentAndAField(t *testing.T) {
	m, _ := loadedWith(t, 44, lifeState())
	next, _ := key(m, "N")
	sc := next.(Model).draw(44)
	if len(sc.formAgents) != 2 {
		t.Fatalf("agent spans %+v", sc.formAgents)
	}
	c := sc.formAgents[1]
	tapped, _ := next.Update(tea.MouseClickMsg{X: sc.peekX + c.x0, Y: sc.formAgentsAt, Button: tea.MouseLeft})
	if fm := tapped.(Model).form; fm.agent != "codex" || fm.at != fieldAgent {
		t.Fatalf("form %+v", fm)
	}
	// A tap on the first message's line puts the cursor there.
	for i, p := range sc.peekRows {
		if p == "field:4" {
			tapped, _ = tapped.Update(tea.MouseClickMsg{X: sc.peekX + 4, Y: sc.peekAt + i, Button: tea.MouseLeft})
		}
	}
	if fm := tapped.(Model).form; fm.at != fieldFirst {
		t.Fatalf("the tap left the cursor on %d", fm.at)
	}
}

// switchState is lifeState with nothing waiting, on app-feature (claude in
// %1): one y switches it.
func switchState(t *testing.T, st proto.State) (tea.Model, *fakeConn) {
	t.Helper()
	st.Queue = nil
	m, f := loadedWith(t, 100, st)
	return selectPane(t, m, "%1"), f
}

func TestAOpensSwitchAgentAndJumpsAfter(t *testing.T) {
	next, f := switchState(t, lifeState())
	var ran [][]string
	m := next.(Model)
	m.opt.Run = func(argv []string) error { ran = append(ran, argv); return nil }
	next, _ = key(m, "A")
	fm := next.(Model).form
	if fm == nil || !fm.swap || fm.agent != "codex" || fm.first != proto.DefaultHandoff {
		t.Fatalf("form %+v", fm)
	}
	if s := screen(next); !strings.Contains(s, "Switch agent") || !strings.Contains(s, "handoff") {
		t.Fatalf("the form:\n%s", s)
	}
	if next, _ = key(next, "left"); next.(Model).form.agent != "codex" {
		t.Fatalf("← left codex for %q", next.(Model).form.agent)
	}
	next, _ = key(next, "enter")
	nm := next.(Model)
	if nm.form != nil || nm.pending == nil || nm.pending.rec != "switch:%1" || !strings.Contains(nm.pending.say, "claude → codex") || len(f.acts()) != 0 {
		t.Fatalf("form %+v pending %+v acts %+v", nm.form, nm.pending, f.acts())
	}
	next, cmd := key(next, "y")
	cmd()
	acts := f.acts()
	if len(acts) != 1 || acts[0].RecID != "switch:%1" || acts[0].Confirm != "y" {
		t.Fatalf("sent %+v", acts)
	}
	var sw proto.SwitchAgent
	if err := json.Unmarshal([]byte(acts[0].Text), &sw); err != nil || sw != (proto.SwitchAgent{Agent: "codex", Handoff: proto.DefaultHandoff}) {
		t.Fatalf("%+v %v", sw, err)
	}
	// A refusal jumps nowhere and says why.
	act := next.(Model).jumpAfter.act
	failed, _ := next.Update(resultMsg(proto.Result{ActID: act, Err: "codex is not installed here"}))
	if fm := failed.(Model); fm.jumpAfter.act != "" || !strings.Contains(fm.flash, "codex is not installed here") {
		t.Fatalf("jump %+v flash %q", fm.jumpAfter, fm.flash)
	}
	// A switch that ran jumps to the pane.
	done, cmd := next.Update(resultMsg(proto.Result{ActID: act, Ran: [][]string{{"tmux"}}}))
	c := cmd().(tea.BatchMsg)
	c[len(c)-1]()
	if len(ran) != 1 || strings.Join(ran[0], " ") != "tmux switch-client -t %1" || done.(Model).flash != "switched app-feature to codex" {
		t.Fatalf("ran %v flash %q", ran, done.(Model).flash)
	}
}

func TestSwitchOfABusySessionTakesASecondY(t *testing.T) {
	st := lifeState()
	st.Sessions[0].Busy = true
	next, f := switchState(t, st)
	next, _ = key(next, "A")
	next, _ = key(next, "enter")
	next, cmd := key(next, "y")
	if cmd != nil {
		cmd()
	}
	if line := actionLine(next); len(f.acts()) != 0 || !strings.Contains(line, "app-feature works: y again switches it mid-turn") {
		t.Fatalf("one y switched a session that works: %q %+v", line, f.acts())
	}
	_, cmd = key(next, "y")
	cmd()
	if acts := f.acts(); len(acts) != 1 || !strings.Contains(acts[0].Text, `"anyway":true`) || !strings.Contains(acts[0].Text, `"agent":"codex"`) {
		t.Fatalf("sent %+v", acts)
	}
}

func TestSwitchRefusesOutsideTmuxAndOneAgent(t *testing.T) {
	st := lifeState()
	for i := range st.Sessions {
		st.Sessions[i].Pane = ""
	}
	m, _ := loadedWith(t, 100, st)
	s, _ := m.selectedSession()
	next, _ := key(m, "A")
	if nm := next.(Model); nm.form != nil || nm.flash != s.Name+" is outside tmux: nothing to switch" {
		t.Fatalf("form %+v flash %q", nm.form, nm.flash)
	}
	st = lifeState()
	st.Agents = []string{"claude"}
	next, _ = switchState(t, st)
	next, _ = key(next, "A")
	if nm := next.(Model); nm.form != nil || nm.flash != "no other agent to switch to here" {
		t.Fatalf("form %+v flash %q", nm.form, nm.flash)
	}
}

func TestSwitchRefusesABadHandoffInTheForm(t *testing.T) {
	next, f := switchState(t, lifeState())
	next, _ = key(next, "A")
	next, _ = key(next, "tab")
	for range len(proto.DefaultHandoff) {
		next, _ = key(next, "backspace")
	}
	next = typeText(next, "--yolo")
	next, _ = key(next, "enter")
	if nm := next.(Model); nm.form == nil || nm.form.err != "the handoff does not start with -" || nm.pending != nil || len(f.acts()) != 0 {
		t.Fatalf("form %+v pending %+v", nm.form, nm.pending)
	}
}

func TestSwitchEscSendsNothing(t *testing.T) {
	next, f := switchState(t, lifeState())
	next, _ = key(next, "A")
	next, _ = key(next, "esc")
	if nm := next.(Model); nm.form != nil || nm.pending != nil || len(f.acts()) != 0 {
		t.Fatal("esc sent or kept the form")
	}
}

// A phone has no A key to hand: the opened row, and the detail beside the
// list, carry A switch, and a tap on it opens the form.
func TestATapOnSwitchOpensTheForm(t *testing.T) {
	for _, w := range []int{50, 120} {
		st := lifeState()
		st.Queue = nil
		m, _ := loadedWith(t, w, st)
		next := selectPane(t, m, "%1")
		out := strings.Split(ansi.Strip(next.(Model).render()), "\n")
		y := slices.IndexFunc(out[:len(out)-1], func(l string) bool { return strings.Contains(l, "A switch") })
		if y < 0 {
			t.Fatalf("%d columns: no A switch above the key line:\n%s", w, strings.Join(out, "\n"))
		}
		x := len([]rune(out[y][:strings.Index(out[y], "A switch")]))
		tapped, _ := tap(next, x, y)
		if fm := tapped.(Model).form; fm == nil || !fm.swap {
			t.Fatalf("%d columns: a tap on A switch opened %+v", w, fm)
		}
	}
}
