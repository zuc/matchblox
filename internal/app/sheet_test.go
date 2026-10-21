package app

import (
	"slices"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/blox-eng/matchblox/internal/proto"
)

// sheetState is the console with a stoker agent on its own tmux server
// (server "": none). An agent that runs (PaneUp) rests, ready for an
// order, unless v says how it starts.
func sheetState(v proto.StokerView, server string) proto.State {
	if server != "" && v.Agent == "" {
		v.Agent = "claude"
		v.Ready = v.Ready || v.PaneUp && !v.Starting
	}
	st := stokerState(v)
	st.StokerServer = server
	return st
}

func typed(m tea.Model, s string) tea.Model {
	for _, r := range s {
		m, _ = m.Update(tea.KeyPressMsg{Code: r, Text: string(r)})
	}
	return m
}

func press(m tea.Model, code rune) (tea.Model, tea.Cmd) {
	return m.Update(tea.KeyPressMsg{Code: code})
}

func TestNOpensSheetAndSendsOrder(t *testing.T) {
	m, f := loadedWith(t, 100, sheetState(proto.StokerView{PaneUp: true}, "matchblox"))
	next := sendKey(m, "n")
	if got := f.acts(); len(got) != 0 {
		t.Fatalf("n sent %+v before the order", got)
	}
	s := screen(next)
	for _, want := range []string{"How should I run tonight?", "☾ Night", "gos ○ merges  ○ releases  ○ deploys", "⏎ send"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q:\n%s", want, s)
		}
	}
	next = typed(next, "merge them")
	next, cmd := press(next, tea.KeyEnter)
	if cmd == nil {
		t.Fatal("enter sent nothing")
	}
	cmd()
	a := f.acts()
	if len(a) != 1 || a[0].RecID != "stoker:order" || a[0].Text != `{"words":"merge them","night":true}` {
		t.Fatalf("acts %+v", a)
	}
	next, _ = next.Update(resultMsg(proto.Result{ActID: "a1"}))
	if next.(Model).sheet != nil {
		t.Fatal("the sheet stays after the order went: the run starts at once")
	}
}

// TestTheGoChipsAreTheRunsGrants: Ruling BA. Tab takes the cursor to the
// go chips, ←→ move between them and space turns one on or off; Tab or
// Esc goes back to the order line. The chips, and only they, become the
// run's goes; off asks the builder.
func TestTheGoChipsAreTheRunsGrants(t *testing.T) {
	m, f := loadedWith(t, 100, sheetState(proto.StokerView{PaneUp: true}, "matchblox"))
	next := sendKey(m, "n")
	next = typed(next, "merge them on green")
	for _, k := range []string{"tab", "space", "right", "right", "space", "left", "space", "left", "space", "esc"} {
		next, _ = next.Update(keyPress(k))
	}
	if next.(Model).sheet == nil {
		t.Fatal("esc on the chips closed the sheet")
	}
	next = typed(next, "!")
	for _, k := range []string{"tab", "tab"} {
		next, _ = next.Update(keyPress(k))
	}
	next = typed(next, "?")
	if s := screen(next); !strings.Contains(s, "gos ○ merges  ✓ releases  ✓ deploys") || !strings.Contains(s, "merge them on green!?") {
		t.Fatalf("the chips are not on:\n%s", s)
	}
	_, cmd := press(next, tea.KeyEnter)
	cmd()
	if a := f.acts(); len(a) != 1 || a[0].Text != `{"words":"merge them on green!?","night":true,"grants":["release","deploy"]}` {
		t.Fatalf("acts %+v", a)
	}
}

// TestAnAltKeyIsNoChip: no Alt key toggles a chip; macOS types µ®∂ for
// them, so the chips are reached with Tab.
func TestAnAltKeyIsNoChip(t *testing.T) {
	m, _ := loadedWith(t, 100, sheetState(proto.StokerView{PaneUp: true}, "matchblox"))
	next := sendKey(m, "n")
	next, _ = next.Update(tea.KeyPressMsg{Code: 'm', Mod: tea.ModAlt})
	if goes := next.(Model).sheet.goes; len(goes) != 0 {
		t.Fatalf("alt+m toggled %q", goes)
	}
}

func TestATapTogglesAGoChip(t *testing.T) {
	m, _ := loadedWith(t, 100, sheetState(proto.StokerView{PaneUp: true}, "matchblox"))
	next := sendKey(m, "n").(Model)
	sc := next.draw(max(next.width, minWidth))
	if len(sc.sheetGoes) != 4 {
		t.Fatalf("chip spans %+v", sc.sheetGoes)
	}
	at := sc.sheetGoes[2] // deploys
	tapped, _ := next.Update(tea.MouseClickMsg{X: sc.peekX + at.x0, Y: sc.sheetGoesAt, Button: tea.MouseLeft})
	if goes := tapped.(Model).sheet.goes; !slices.Equal(goes, []string{"deploy"}) {
		t.Fatalf("a tap on deploys: %q", goes)
	}
}

func TestSheetRefusalStaysInTheSheet(t *testing.T) {
	m, _ := loadedWith(t, 100, sheetState(proto.StokerView{PaneUp: true}, "matchblox"))
	var next = sendKey(m, "n")
	next = typed(next, "x")
	next, cmd := press(next, tea.KeyEnter)
	cmd()
	next, _ = next.Update(resultMsg(proto.Result{ActID: "a1", Err: "the night is already on"}))
	if s := screen(next); !strings.Contains(s, "the night is already on") || next.(Model).sheet == nil || next.(Model).sheet.text != "x" {
		t.Fatalf("refusal not in the sheet:\n%s", s)
	}
}

func TestSheetEscSendsNothing(t *testing.T) {
	m, f := loadedWith(t, 100, sheetState(proto.StokerView{PaneUp: true}, "matchblox"))
	var next = sendKey(m, "n")
	next = typed(next, "x")
	next, cmd := press(next, tea.KeyEscape)
	if cmd != nil || next.(Model).sheet != nil || len(f.acts()) != 0 {
		t.Fatalf("esc sent %+v", f.acts())
	}
}

func TestOIsSealedAtNight(t *testing.T) {
	until := time.Date(2026, 10, 10, 7, 0, 0, 0, time.Local)
	m, f := loadedWith(t, 100, sheetState(proto.StokerView{Mode: proto.StokerMode{On: true, Until: until}}, "matchblox"))
	next := sendKey(m, "o").(Model)
	if next.sheet != nil || next.flash != sealedText || len(f.acts()) != 0 {
		t.Fatalf("flash %q sheet %v", next.flash, next.sheet)
	}
	run := proto.StokerOrder{ID: "r1", Words: "merge them", State: "running", Grants: []string{"merge"}}
	v := proto.StokerView{Mode: proto.StokerMode{On: true, Order: &run}, PaneUp: true}
	m, _ = loadedWith(t, 100, sheetState(v, "matchblox"))
	day := sendKey(m, "o").(Model)
	if day.sheet == nil || !day.sheet.restart || day.sheet.night || day.sheet.text != "" || !slices.Equal(day.sheet.goes, []string{"merge"}) {
		t.Fatalf("o by day: %+v", day.sheet)
	}
	if s := screen(day); !strings.Contains(s, "How should I run?") || !strings.Contains(s, "✓ merges") {
		t.Fatalf("no day sheet:\n%s", s)
	}
	m, _ = loadedWith(t, 100, sheetState(proto.StokerView{PaneUp: true}, "matchblox"))
	if off := sendKey(m, "o").(Model); off.sheet != nil || off.flash == "" {
		t.Fatalf("o with the stoker off: %+v %q", off.sheet, off.flash)
	}
}

// TestOOnABusyStokerSaysItIsBusy: o to a day run whose stoker agent is
// up and mid-turn says the order waits for its rest, never that it starts.
func TestOOnABusyStokerSaysItIsBusy(t *testing.T) {
	run := proto.StokerOrder{ID: "r1", Words: "merge them", State: "running"}
	start := "tmux -L matchblox new-session -d -s stoker -c /home/me claude"
	v := proto.StokerView{Mode: proto.StokerMode{On: true, Order: &run}, Agent: "claude", Start: start, PaneUp: true}
	m, f := loadedWith(t, 100, sheetState(v, "matchblox"))
	var next = sendKey(m, "o")
	s := screen(next)
	if !strings.Contains(s, busyText) || strings.Contains(s, startingText) || strings.Contains(s, "new-session") {
		t.Fatalf("o on a busy stoker:\n%s", s)
	}
	if a := f.acts(); len(a) != 0 {
		t.Fatalf("o on a running stoker started it: %+v", a)
	}
	next = typed(next, "and the docs")
	next, cmd := press(next, tea.KeyEnter)
	if cmd != nil {
		cmd()
	}
	if s := screen(next); !strings.Contains(s, busyText) || strings.Contains(s, startingText) || len(f.acts()) != 0 {
		t.Fatalf("⏎ on a busy stoker sent %+v:\n%s", f.acts(), s)
	}
}

// TestTheCardNamesTheGoes: the STOKER tab says what the chips give, what
// still asks, and what no guard stops.
func TestTheCardNamesTheGoes(t *testing.T) {
	o := proto.StokerOrder{ID: "r1", Words: "merge them", State: "running", Grants: []string{"release"},
		Plan:      []proto.StokerPlan{{N: 1, Session: "cx", Text: "rebase"}},
		Unguarded: []proto.StokerUnguard{{Session: "cx", Goes: []string{"merge", "deploy"}}}}
	v := proto.StokerView{Mode: proto.StokerMode{On: true, Order: &o}}
	m, _ := loadedWith(t, 100, sheetState(v, "matchblox"))
	s := screen(sendKey(m, "c"))
	for _, want := range []string{"✓ releases", "! merges, deploys and kills still ask you", "! no merge guard watches cx: its merges and deploys go unchecked"} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q:\n%s", want, s)
		}
	}
	if strings.Contains(s, "waits for your go") {
		t.Fatalf("a plan that waits for a go:\n%s", s)
	}
}

// TestSheetWithoutAStokerAgent: a service that starts no stoker agent
// (fixtures) turns today's stoker on at ⏎, and the sheet shows no door.
func TestSheetWithoutAStokerAgent(t *testing.T) {
	m, f := loadedWith(t, 100, sheetState(proto.StokerView{}, ""))
	var next = sendKey(m, "f")
	if s := screen(next); strings.Contains(s, "tmux new") || strings.Contains(s, startingText) || !strings.Contains(s, "How should I run?") {
		t.Fatalf("sheet:\n%s", s)
	}
	next, cmd := press(next, tea.KeyEnter)
	cmd()
	if a := f.acts(); len(a) != 1 || a[0].RecID != "stoker:order" || a[0].Text != `{"words":"","night":false}` {
		t.Fatalf("acts %+v", a)
	}
	if next.(Model).sheet != nil {
		t.Fatal("the sheet stays after today's run starts")
	}
}

func TestSheetShowsARefusal(t *testing.T) {
	m, _ := loadedWith(t, 100, sheetState(proto.StokerView{PaneUp: true}, "matchblox"))
	var next = sendKey(m, "n")
	next = typed(next, "go")
	next, cmd := press(next, tea.KeyEnter)
	cmd()
	next, _ = next.Update(resultMsg(proto.Result{ActID: "a1", Err: "the stoker is not running: f or n starts it"}))
	s := screen(next)
	if !strings.Contains(s, "! the stoker is not running") || !strings.Contains(s, "⏎ send") {
		t.Fatalf("refusal not shown with the order to edit:\n%s", s)
	}
}

// TestSpaceOnAWaysChipPutsItIn: past the go chips, → reaches the recipes
// of the ways file, and space puts one into the order.
func TestSpaceOnAWaysChipPutsItIn(t *testing.T) {
	v := proto.StokerView{Recipes: []string{"merge-order", "priority"}}
	m, _ := loadedWith(t, 100, sheetState(v, "matchblox"))
	var next = sendKey(m, "n")
	next = typed(next, "run")
	for _, k := range []string{"tab", "right", "right", "right", "right", "space", "right", "space"} {
		next, _ = next.Update(keyPress(k))
	}
	if got := next.(Model).sheet; got.text != "run merge-order priority" || len(got.goes) != 0 {
		t.Fatalf("sheet %+v", got)
	}
	if s := screen(next); !strings.Contains(s, "ways merge-order  priority") {
		t.Fatalf("no chips:\n%s", s)
	}
}

// TestARecipeChipThatWentAwayIsNoCrash: the ways file shrinks while the
// cursor is on its last recipe chip; space then picks nothing.
func TestARecipeChipThatWentAwayIsNoCrash(t *testing.T) {
	s := sheet{at: len(goChips) + 1}
	s.pick([]string{"merge-order"})
	if s.text != "" || s.at != len(goChips) {
		t.Fatalf("sheet %+v", s)
	}
}

func TestSheetBlankOrderIsSent(t *testing.T) {
	m, f := loadedWith(t, 100, sheetState(proto.StokerView{PaneUp: true}, "matchblox"))
	next := sendKey(m, "n")
	_, cmd := press(next, tea.KeyEnter)
	if cmd == nil {
		t.Fatal("a blank order sent nothing")
	}
	cmd()
	if a := f.acts(); len(a) != 1 || a[0].RecID != "stoker:order" || a[0].Text != `{"words":"","night":true}` {
		t.Fatalf("acts %+v", a)
	}
}

// TestTheDoorShowsWhenTheAgentIsNotOnPath: the agent's command is not on
// PATH: the sheet names it and how to set another before ⏎, starts
// nothing, and ⏎ turns today's stoker on.
func TestTheDoorShowsWhenTheAgentIsNotOnPath(t *testing.T) {
	missing := `codex is not on PATH: install it, or name the stoker's agent in config.toml, [stoker] agent = "codex" (or opencode, cursor-agent)`
	m, f := loadedWith(t, 100, sheetState(proto.StokerView{Agent: "codex", Missing: missing}, "matchblox"))
	next := sendKey(m, "n")
	if s := screen(next); !strings.Contains(s, "! codex is not on PATH") || strings.Contains(s, startingText) || !strings.Contains(s, "⏎ start") {
		t.Fatalf("no door before ⏎:\n%s", s)
	}
	if a := f.acts(); len(a) != 0 {
		t.Fatalf("an agent not on PATH was started: %+v", a)
	}
}

// TestFStartsTheStokerAndTheOrderWaitsForItsRest: f with no stoker agent
// running starts it; the sheet says so with the exact command, and ⏎
// waits: the order is typed once the agent rests, never into a
// half-started one.
func TestFStartsTheStokerAndTheOrderWaitsForItsRest(t *testing.T) {
	start := "tmux -L matchblox new-session -d -s stoker -c /home/me claude"
	m, f := loadedWith(t, 100, sheetState(proto.StokerView{Agent: "claude", Start: start}, "matchblox"))
	var next = sendKey(m, "f")
	if a := f.acts(); len(a) != 1 || a[0].RecID != "stoker:start" {
		t.Fatalf("f sent %+v", a)
	}
	starting := sheetState(proto.StokerView{Agent: "claude", Start: start, Starting: true}, "matchblox")
	next, _ = next.Update(stateMsg(starting))
	if s := screen(next); !strings.Contains(s, startingText) || !strings.Contains(s, "tmux -L matchblox new-session -d -s stoker") {
		t.Fatalf("the sheet does not say the stoker starts:\n%s", s)
	}
	next = typed(next, "merge them")
	next, cmd := press(next, tea.KeyEnter)
	if cmd != nil {
		cmd()
	}
	if a := f.acts(); len(a) != 1 {
		t.Fatalf("⏎ sent %+v before the stoker rests", a[1:])
	}
	if s := screen(next); !strings.Contains(s, "the order goes once it rests") {
		t.Fatalf("⏎ does not say it waits:\n%s", s)
	}
	// Up, busy at its first prompt: still waiting.
	next, _ = next.Update(stateMsg(sheetState(proto.StokerView{Agent: "claude", Start: start, PaneUp: true, Starting: true}, "matchblox")))
	if a := f.acts(); len(a) != 1 {
		t.Fatalf("an order went into a half-started stoker: %+v", a[1:])
	}
	// At rest: the order goes by itself.
	next, cmd = next.Update(stateMsg(sheetState(proto.StokerView{Agent: "claude", Start: start, PaneUp: true, Ready: true}, "matchblox")))
	if next.(Model).sheet.act == "" {
		t.Fatal("the order did not go when the stoker came to rest")
	}
	batch := cmd().(tea.BatchMsg) // recv, then the order: the last
	batch[len(batch)-1]()
	if a := f.acts(); len(a) != 2 || a[1].RecID != "stoker:order" || a[1].Text != `{"words":"merge them","night":false}` {
		t.Fatalf("acts %+v", a)
	}
	// A running stoker is not started again.
	m, f = loadedWith(t, 100, sheetState(proto.StokerView{PaneUp: true}, "matchblox"))
	sendKey(m, "n")
	if a := f.acts(); len(a) != 0 {
		t.Fatalf("n started a running stoker: %+v", a)
	}
}

// TestTheCardQuotesTheLatestOrder: by day the card quotes the newest
// order, with the earlier ones folded under it, a line each.
func TestTheCardQuotesTheLatestOrder(t *testing.T) {
	at := time.Date(2026, 10, 10, 12, 0, 0, 0, time.Local)
	o := proto.StokerOrder{ID: "r1", Words: "now the docs", State: "running", At: at.Add(time.Hour), Earlier: []proto.StokerSaid{
		{At: at, Words: "merge them on green"}, {At: at.Add(30 * time.Minute), Words: "and hold the billing work until the release is out, whatever it says"}}}
	v := proto.StokerView{Mode: proto.StokerMode{On: true, Since: at, Order: &o}}
	m, _ := loadedWith(t, 60, sheetState(v, "matchblox"))
	s := screen(sendKey(m, "c"))
	quote := strings.Index(s, "“now the docs”")
	later := strings.Index(s, "earlier 12:30 “and hold the billing work")
	first := strings.Index(s, "earlier 12:00 “merge them on green”")
	if quote < 0 || later < quote || first < later {
		t.Fatalf("the latest order is not quoted first, the earlier folded under it:\n%s", s)
	}
	fitsWidth(t, sendKey(m, "c").(Model), 60)
}
