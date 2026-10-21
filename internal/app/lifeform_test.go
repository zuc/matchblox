package app

import (
	"encoding/json"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

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
