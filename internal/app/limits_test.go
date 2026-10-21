package app

import (
	"errors"
	"image/color"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/blox-eng/matchblox/internal/advice"
	"github.com/blox-eng/matchblox/internal/limits"
	"github.com/blox-eng/matchblox/internal/proto"
)

func limitsState() proto.State {
	st := queueState()
	at := st.At
	st.Limits = []limits.Account{
		{Provider: "anthropic", Account: "builder@example.com · Max", State: limits.Measured, At: at, Forecast: &limits.Outlook{Lasts: true},
			Windows: []limits.Window{{UsedPct: 62, Minutes: limits.BoxMinutes, ResetsAt: at.Add(2 * time.Hour)},
				{UsedPct: 41, Minutes: limits.WeekMinutes, ResetsAt: at.Add(3 * 24 * time.Hour)}}},
		{Provider: "openai", Account: "builder@example.org · Plus", State: limits.Measured, At: at, Forecast: &limits.Outlook{Out: at.Add(26 * time.Hour)},
			Windows: []limits.Window{{UsedPct: 12, Minutes: limits.BoxMinutes, ResetsAt: at.Add(4 * time.Hour)},
				{UsedPct: 92, Minutes: limits.WeekMinutes, ResetsAt: at.Add(5 * 24 * time.Hour)}}},
		{Provider: "anthropic", Account: "work@example.net · Team", State: limits.Unmeasured, Fix: "open the door “Show your limits”"},
	}
	return st
}

// selectRec presses ↑ until the recommendation id is selected.
func selectRec(t *testing.T, m tea.Model, id string) tea.Model {
	t.Helper()
	for range m.(Model).rowCount() {
		m, _ = key(m, "up")
	}
	for range m.(Model).rowCount() {
		if r, ok := m.(Model).selectedRec(); ok && r.ID == id {
			return m
		}
		m, _ = key(m, "down")
	}
	t.Fatalf("no recommendation %s to select", id)
	return m
}

// TestTheHeaderHasNoLimits: the header is one line and the tabs are under
// it; the limits are in the dock (design/0006 §11.5).
func TestTheHeaderHasNoLimits(t *testing.T) {
	for _, w := range []int{44, 86, 132} {
		m, _ := loadedWith(t, w, limitsState())
		lines := strings.Split(screen(m), "\n")
		if !strings.Contains(lines[1], "SESSIONS") || strings.Contains(strings.Join(lines[:3], "\n"), "62%") {
			t.Fatalf("width %d: the header is not one line:\n%s", w, strings.Join(lines[:4], "\n"))
		}
		if !strings.Contains(lines[0], "● 2 wait for you") {
			t.Fatalf("width %d: the header does not say who waits: %q", w, lines[0])
		}
	}
}

// TestTheDockSaysUsageAndTheMachine: under the body, each account's
// windows as /usage says them, with the forecast on its block, and the
// machine; side by side when there is room.
func TestTheDockSaysUsageAndTheMachine(t *testing.T) {
	m, _ := loadedWith(t, 132, limitsState())
	out := screen(m)
	at := lineOf(t, m, "USAGE")
	if !strings.Contains(strings.Split(out, "\n")[at], "MACHINE") {
		t.Fatalf("usage and the machine are not side by side:\n%s", out)
	}
	until := limitsState().At.Add(26 * time.Hour).Local().Format("Mon")
	for _, want := range []string{
		"✻ builder  5h   ▰▰▰▰▰▱▱▱ 62%", "wk   ▰▰▰▱▱▱▱▱ 41%", "lasts the week",
		"✻ work     not measured · open the door “Show your limits”",
		"❋ Plus     5h   ▰▱▱▱▱▱▱▱ 12%", "92%", "out " + until,
		"cpu", "mem", "runs 3 claude",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the dock lacks %q:\n%s", want, out)
		}
	}
	fg := func(pct float64) color.Color { return m.useStyle(pct).GetForeground() }
	if fg(59) != m.st.theme.Muted || fg(60) != m.st.theme.Warn || fg(90) != m.st.theme.Neg {
		t.Error("a window's bar is warn from 60% and neg from 90%")
	}
	// Nothing measured says so.
	none := queueState()
	none.Limits = nil
	plain, _ := loadedWith(t, 132, none)
	if !strings.Contains(screen(plain), "no session runs under a plan") {
		t.Fatalf("an empty usage panel:\n%s", screen(plain))
	}
}

// TestAPhoneDockShowsOnePanel: on a phone the dock is usage, the machine
// or the stoker; u, or a tap on its switch, cycles it. An account other than
// Anthropic's takes one line.
func TestAPhoneDockShowsOnePanel(t *testing.T) {
	m, _ := loadedWith(t, 44, limitsState())
	m.height = len(m.body(44).lines) + len(m.dock(44, -1).lines) + 1 + 5 // no row to grow into
	out := screen(m)
	if !strings.Contains(out, "USAGE") || !strings.Contains(out, "machine ›") || strings.Contains(out, "MACHINE") {
		t.Fatalf("the phone dock is not usage alone:\n%s", out)
	}
	if !strings.Contains(out, "❋ Plus     5h ▱▱▱ 12% · wk 92%") {
		t.Fatalf("Codex does not fold to one line:\n%s", out)
	}
	fitsWidth(t, m, 44)
	next, _ := key(m, "u")
	if out := screen(next); !strings.Contains(out, "MACHINE") || !strings.Contains(out, "stoker ›") || strings.Contains(out, "USAGE") {
		t.Fatalf("u does not show the machine:\n%s", out)
	}
	y := lineOf(t, next, "stoker ›")
	x := strings.Index(strings.Split(screen(next), "\n")[y], "stoker ›")
	st, _ := tap(next, len([]rune(strings.Split(screen(next), "\n")[y][:x])), y)
	if out := screen(st); !strings.Contains(out, "STOKER") || !strings.Contains(out, "usage ›") || !strings.Contains(out, "f starts it") {
		t.Fatalf("a tap on stoker › does not show the stoker:\n%s", out)
	}
	if back, _ := key(st, "u"); !strings.Contains(screen(back), "USAGE") {
		t.Fatalf("u does not come back to usage:\n%s", screen(back))
	}
}

// TestTheDockFoldsWhenItDoesNotFit: the dock shows when the body, the
// dock and one blank row fit; one row less and it folds into the Machine
// tab, which has usage first.
func TestTheDockFoldsWhenItDoesNotFit(t *testing.T) {
	m, _ := loadedWith(t, 44, limitsState())
	need := len(m.body(44).lines) + len(m.dock(44, -1).lines) + 1 + 5 // the header, the tabs, two hairlines, the key line
	for h, docked := range map[int]bool{need: true, need - 1: false} {
		m.height = h
		if got := strings.Contains(screen(m), "USAGE"); got != docked {
			t.Errorf("height %d (needs %d): docked %v, want %v:\n%s", h, need, got, docked, screen(m))
		}
	}
	m.height = need - 1
	next, _ := key(m, "3")
	lines := strings.Split(screen(next), "\n")
	if !strings.Contains(lines[3], "USAGE") || !strings.Contains(screen(next), "MACHINE") {
		t.Fatalf("the Machine tab does not start with usage:\n%s", screen(next))
	}
}

// TestARecommendationShowsItsResult: Enter on a recommendation shows the
// step, a second Enter runs it, and the row then says what happened: done,
// or what failed and that Enter tries again.
func TestARecommendationShowsItsResult(t *testing.T) {
	st := limitsState()
	st.Recommendations = []advice.Rec{{ID: "wk", Level: "warn", Title: "The week runs out Wed ~19:00: compact app-feature",
		Evidence: "builder@example.org · Plus: 92% of the week used",
		Primary:  &advice.Action{Label: "jump to the session", Nav: true, Steps: [][]string{{"tmux", "switch-client", "-t", "%1"}}},
		Second: &advice.Action{Label: "send /compact", Destructive: true, Steps: [][]string{{"tmux", "send-keys", "-t", "%1", "/compact", "Enter"}},
			Guards: []advice.Guard{{IdlePane: "%1"}}}}}
	m, f := loadedWith(t, 100, st)
	var next tea.Model = m
	next = selectRec(t, next, "wk")
	next, _ = key(next, "x")
	if p := next.(Model).pending; p == nil || p.String() != "tmux send-keys -t %1 /compact Enter" {
		t.Fatalf("x shows %+v", p)
	}
	next = runCmd(key(next, "y"))
	sent := f.lastAct(t)
	next, _ = next.Update(resultMsg(proto.Result{ActID: sent, Skipped: []string{"%1 is busy now"}}))
	if out := screen(next); !strings.Contains(out, "✗ skipped: %1 is busy now · x tries again") {
		t.Fatalf("a skipped step does not say so on the row:\n%s", out)
	}
	next, _ = key(next, "x")
	next = runCmd(key(next, "y"))
	next, _ = next.Update(resultMsg(proto.Result{ActID: f.lastAct(t), Ran: [][]string{{"tmux", "send-keys", "-t", "%1", "/compact", "Enter"}}}))
	if out := screen(next); !strings.Contains(out, "✓ ran: tmux send-keys -t %1 /compact Enter") {
		t.Fatalf("a step that ran does not say so on the row:\n%s", out)
	}
	next, _ = next.Update(resultMsg(proto.Result{ActID: "a-other", Err: "nope"}))
	if out := screen(next); strings.Contains(out, "✗ failed: nope") {
		t.Fatalf("another act's result landed on the row:\n%s", out)
	}
	next, _ = key(next, "x")
	next = runCmd(key(next, "y"))
	next, _ = next.Update(resultMsg(proto.Result{ActID: f.lastAct(t), Err: "the pane is gone"}))
	if out := screen(next); !strings.Contains(out, "✗ failed: the pane is gone · x tries again") {
		t.Fatalf("a failure does not say so on the row:\n%s", out)
	}
	// The rec goes, and comes back later: it shows its title, not an old result.
	gone := st
	gone.Recommendations = nil
	next, _ = next.Update(stateMsg(gone))
	next, _ = next.Update(stateMsg(st))
	if out := screen(next); strings.Contains(out, "✗ failed") || !strings.Contains(out, "▲ The week runs out Wed ~19:00") {
		t.Fatalf("an old result came back with the rec:\n%s", out)
	}
}

// runCmd runs the command a key returned, as the program would.
func runCmd(m tea.Model, cmd tea.Cmd) tea.Model {
	if cmd != nil {
		cmd()
	}
	return m
}

func (f *fakeConn) lastAct(t *testing.T) string {
	t.Helper()
	f.mu.Lock()
	defer f.mu.Unlock()
	for i := len(f.sent) - 1; i >= 0; i-- {
		if f.sent[i].Kind == proto.KindAct {
			return f.sent[i].ID
		}
	}
	t.Fatal("no act was sent")
	return ""
}

// TestAJumpLaterDoesNotRewriteTheRow: the jump of a recommendation shows
// on its row; a jump from a queue row afterwards does not replace it.
func TestAJumpLaterDoesNotRewriteTheRow(t *testing.T) {
	st := limitsState()
	st.Recommendations = []advice.Rec{{ID: "wk", Level: "warn", Title: "The week runs out Wed ~19:00: compact app-feature",
		Primary: &advice.Action{Label: "jump to the session", Nav: true, Steps: [][]string{{"tmux", "switch-client", "-t", "%1"}}}}}
	m, _ := loadedWith(t, 100, st)
	var next tea.Model = m
	next = selectRec(t, next, "wk")
	next, _ = key(next, "enter")
	next, _ = key(next, "enter")
	next, _ = next.Update(ranMsg{cmd: "tmux switch-client -t %1", jumped: "%1"})
	if !strings.Contains(screen(next), "✓ ran: tmux switch-client -t %1") {
		t.Fatalf("the jump is not on the row:\n%s", screen(next))
	}
	next, _ = next.Update(ranMsg{cmd: "tmux switch-client -t %2", err: errors.New("no pane %2")})
	if out := screen(next); strings.Contains(out, "✗ failed") || !strings.Contains(out, "✓ ran: tmux switch-client -t %1") {
		t.Fatalf("another jump rewrote the row:\n%s", out)
	}
}

// TestStatusTextHasTheLimits: `matchblox status --text` says each
// account's limits as the dock does.
func TestStatusTextHasTheLimits(t *testing.T) {
	st := limitsState()
	out := Summary(st.Doc)
	for _, want := range []string{
		"limits anthropic builder@example.com · Max: 5h 62% · wk 41% · lasts the week\n",
		"limits anthropic work@example.net · Team: not measured · open the door “Show your limits”\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q in\n%s", want, out)
		}
	}
}

// TestCursorsAccountIsInTheDock: a Cursor session's account has a line of
// its own in USAGE, with Cursor's mark, as every agent's account does.
func TestCursorsAccountIsInTheDock(t *testing.T) {
	st := limitsState()
	st.Limits = append(st.Limits, limits.Account{Provider: "cursor", Account: "builder@example.com · Acme", State: limits.Unmeasured, Fix: "Cursor's plan usage is not read yet"})
	m, _ := loadedWith(t, 132, st)
	if out := screen(m); !strings.Contains(out, "⬢ Acme") || !strings.Contains(out, "not measured · Cursor's plan usage is not read yet") {
		t.Errorf("the dock lacks Cursor's account:\n%s", out)
	}
}
