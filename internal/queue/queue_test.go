package queue

import (
	"testing"
	"time"

	"github.com/blox-eng/matchblox/internal/hooks"
	"github.com/blox-eng/matchblox/internal/said"
	"github.com/blox-eng/matchblox/internal/sample"
)

var t0 = time.Date(2026, 10, 1, 9, 0, 0, 0, time.UTC)

func at(s int) time.Time { return t0.Add(time.Duration(s) * time.Second) }

func session(id, pane, name, status string, idle time.Duration) sample.Session {
	return sample.Session{SessionID: id, Pane: pane, Target: "lab:1." + pane[1:], Name: name, Status: status, Busy: status == "busy", Idle: idle}
}

// TestQueueStates covers each row of design 0001 §7.1.
func TestQueueStates(t *testing.T) {
	for _, tc := range []struct {
		name   string
		events []hooks.Event
		want   string // "" = not in the queue
	}{
		{"permission", []hooks.Event{{Name: "Notification", Kind: hooks.KindPermission}}, StatePermission},
		{"idle prompt", []hooks.Event{{Name: "Notification", Kind: hooks.KindIdlePrompt}}, StateQuestion},
		{"stop", []hooks.Event{{Name: "Stop"}}, StateFinished},
		{"prompt submit leaves", []hooks.Event{{Name: "Stop"}, {Name: "UserPromptSubmit"}}, ""},
		{"session start appears, not waiting", []hooks.Event{{Name: "SessionStart"}}, ""},
		{"session end disappears", []hooks.Event{{Name: "Stop"}, {Name: "SessionEnd"}}, ""},
		{"other notification ignored", []hooks.Event{{Name: "Notification", Kind: "auth_success"}}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := New()
			for i, ev := range tc.events {
				ev.SessionID, ev.Pane, ev.At = "s1", "%1", at(i)
				q.Apply(ev)
			}
			q.Merge([]sample.Session{session("s1", "%1", "api", "idle", time.Minute)}, at(10))
			items := q.Items()
			if tc.want == "" {
				if len(items) != 0 {
					t.Fatalf("items %+v", items)
				}
				return
			}
			if len(items) != 1 || items[0].State != tc.want || items[0].Estimated {
				t.Fatalf("items %+v, want one %s", items, tc.want)
			}
		})
	}
}

func TestQueueFillsTheSession(t *testing.T) {
	q := New()
	q.Apply(hooks.Event{Name: "Stop", SessionID: "s1", At: at(0), Message: "Done."})
	q.Merge([]sample.Session{session("s1", "%3", "api-auth", "idle", 0)}, at(5))
	it := q.Items()[0]
	if it.Pane != "%3" || it.Target != "lab:1.3" || it.Name != "api-auth" || it.LastLine != "Done." || !it.Since.Equal(at(0)) {
		t.Fatalf("%+v", it)
	}
}

func TestQueueKeepsTheWaitStart(t *testing.T) {
	q := New()
	q.Apply(hooks.Event{Name: "Stop", SessionID: "s1", At: at(0), Message: "Shall I merge?"})
	q.Apply(hooks.Event{Name: "Notification", Kind: hooks.KindIdlePrompt, SessionID: "s1", At: at(60), Message: "Claude is waiting for your input"})
	q.Merge([]sample.Session{session("s1", "%1", "api", "idle", 0)}, at(61))
	it := q.Items()[0]
	if it.State != StateQuestion || !it.Since.Equal(at(0)) || it.LastLine != "Shall I merge?" {
		t.Fatalf("%+v", it)
	}
}

func TestQueueOldestFirst(t *testing.T) {
	q := New()
	q.Apply(hooks.Event{Name: "Stop", SessionID: "new", At: at(30)})
	q.Apply(hooks.Event{Name: "Notification", Kind: hooks.KindPermission, SessionID: "old", At: at(10)})
	q.Merge([]sample.Session{
		session("new", "%1", "web", "idle", 0),
		session("old", "%2", "api", "idle", 0),
		session("guess", "%3", "docs", "idle", 5*time.Minute), // since at(40)-5m: oldest
	}, at(40))
	var got []string
	for _, it := range q.Items() {
		got = append(got, it.SessionID)
	}
	if len(got) != 3 || got[0] != "guess" || got[1] != "old" || got[2] != "new" {
		t.Fatalf("order %v", got)
	}
}

func TestUserPromptSubmitLeavesQueue(t *testing.T) {
	q := New()
	q.Apply(hooks.Event{Name: "Notification", Kind: hooks.KindPermission, SessionID: "s1", At: at(0)})
	q.Apply(hooks.Event{Name: "UserPromptSubmit", SessionID: "s1", At: at(1)})
	// The sampler still reads idle for a moment: a hooked session is never guessed.
	q.Merge([]sample.Session{session("s1", "%1", "api", "idle", time.Second)}, at(2))
	if items := q.Items(); len(items) != 0 {
		t.Fatalf("items %+v", items)
	}
}

func TestFallbackIsEstimated(t *testing.T) {
	q := New()
	q.Merge([]sample.Session{
		session("a", "%1", "api", "idle", 4*time.Minute),
		session("b", "%2", "web", "busy", 0),
	}, at(600))
	items := q.Items()
	if len(items) != 1 {
		t.Fatalf("items %+v", items)
	}
	it := items[0]
	if !it.Estimated || it.State != StateFinished || it.SessionID != "a" || !it.Since.Equal(at(600).Add(-4*time.Minute)) {
		t.Fatalf("%+v", it)
	}
}

func TestQueueDropsAGoneSession(t *testing.T) {
	q := New()
	q.Apply(hooks.Event{Name: "Stop", SessionID: "s1", At: at(0)})
	q.Merge(nil, at(1))
	if items := q.Items(); len(items) != 0 {
		t.Fatalf("items %+v", items)
	}
}

func TestQueueMatchesByPaneWithoutSessionID(t *testing.T) {
	q := New()
	q.Apply(hooks.Event{Name: "Stop", SessionID: "s1", Pane: "%7", At: at(0)})
	q.Merge([]sample.Session{session("", "%7", "api", "idle", 0)}, at(1))
	items := q.Items()
	if len(items) != 1 || items[0].Name != "api" || items[0].Estimated {
		t.Fatalf("items %+v", items)
	}
}

// Review 5: a spooled event older than what the queue already knows is stale.
func TestQueueIgnoresOlderEvent(t *testing.T) {
	q := New()
	q.Apply(hooks.Event{Name: "UserPromptSubmit", SessionID: "s1", Pane: "%1", At: at(20)})
	q.Apply(hooks.Event{Name: "Stop", SessionID: "s1", Pane: "%1", At: at(10)})
	q.Merge([]sample.Session{session("s1", "%1", "api", "idle", time.Second)}, at(30))
	if items := q.Items(); len(items) != 0 {
		t.Fatalf("a stale Stop came back: %+v", items)
	}
}

// Review 6: the person approved in the pane; a tool ran.
func TestPostToolUseClearsPermission(t *testing.T) {
	q := New()
	q.Apply(hooks.Event{Name: "Notification", Kind: hooks.KindPermission, SessionID: "s1", Pane: "%1", At: at(0)})
	q.Apply(hooks.Event{Name: "PostToolUse", SessionID: "s1", Pane: "%1", At: at(5)})
	q.Merge([]sample.Session{session("s1", "%1", "api", "busy", 0)}, at(6))
	if items := q.Items(); len(items) != 0 {
		t.Fatalf("items %+v", items)
	}
}

// PostToolUse does not clear a session that finished its turn.
func TestPostToolUseKeepsFinished(t *testing.T) {
	q := New()
	q.Apply(hooks.Event{Name: "Stop", SessionID: "s1", Pane: "%1", At: at(0)})
	q.Apply(hooks.Event{Name: "PostToolUse", SessionID: "s1", Pane: "%1", At: at(1)})
	q.Merge([]sample.Session{session("s1", "%1", "api", "idle", 0)}, at(2))
	if items := q.Items(); len(items) != 1 {
		t.Fatalf("items %+v", items)
	}
}

// Review 8: the sampler can lag behind a hook (a new session id after
// /clear); the event waits a grace period instead of being dropped.
func TestQueueKeepsUnseenForAGrace(t *testing.T) {
	q := New()
	q.Apply(hooks.Event{Name: "Stop", SessionID: "new", Pane: "%9", At: at(0)})
	q.Merge(nil, at(1))
	q.Merge([]sample.Session{session("new", "%9", "api", "idle", 0)}, at(10))
	if items := q.Items(); len(items) != 1 || items[0].Estimated {
		t.Fatalf("the event was dropped before the sampler saw the session: %+v", items)
	}
	q.Apply(hooks.Event{Name: "Stop", SessionID: "gone", Pane: "%8", At: at(10)})
	q.Merge(nil, at(10+int(UnseenGrace/time.Second)+1))
	if _, ok := q.hooked["gone"]; ok {
		t.Fatal("an unseen session is kept forever")
	}
}

// TestAPaneThatAsksWaitsForYou: an agent without hooks waits for the person
// when its pane is idle and asks; the item says it was read from the pane.
func TestAPaneThatAsksWaitsForYou(t *testing.T) {
	asks := session("", "%4", "codex", "idle", time.Minute)
	asks.FromPane, asks.Asks, asks.LastLine = true, true, "Should I also update the docs?"
	done := session("", "%5", "opencode", "idle", 2*time.Minute)
	done.FromPane, done.LastLine = true, "Done. All 42 tests pass."
	q := New()
	q.Merge([]sample.Session{asks, done}, at(600))
	items := q.Items()
	if len(items) != 2 {
		t.Fatalf("items %+v", items)
	}
	byName := map[string]Item{items[0].Name: items[0], items[1].Name: items[1]}
	if it := byName["codex"]; it.State != StateQuestion || !it.FromPane || !it.Estimated || it.LastLine != asks.LastLine {
		t.Fatalf("codex %+v", it)
	}
	// An approval menu is a permission: a typed line and Enter would approve.
	menu := session("", "%6", "codex", "idle", time.Minute)
	menu.FromPane, menu.Permits, menu.LastLine = true, true, "Would you like to run the following command?"
	q.Merge([]sample.Session{menu}, at(600))
	if it := q.Items()[0]; it.State != StatePermission {
		t.Fatalf("an approval menu is %q, want permission", it.State)
	}
	if it := byName["opencode"]; it.State != StateFinished || !it.FromPane || it.LastLine != done.LastLine {
		t.Fatalf("opencode %+v", it)
	}
}

// TestAStopThatAsksWaits: a turn that ended on a question waits for the
// person; it is not done.
func TestAStopThatAsksWaits(t *testing.T) {
	q := New()
	q.Apply(hooks.Event{Name: "Stop", SessionID: "s1", At: at(0), Message: "Shall I open the PR?", Asks: true})
	q.Merge([]sample.Session{session("s1", "%3", "api", "idle", 0)}, at(5))
	if it := q.Items()[0]; it.State != StateQuestion || it.LastLine != "Shall I open the PR?" {
		t.Fatalf("%+v", it)
	}
}

// A session working through its subagents does not wait for you: their
// results start its next turn.
func TestASessionWorkingThroughSubagentsDoesNotWait(t *testing.T) {
	q := New()
	s := session("a", "%1", "api", sample.StatusAgents, 4*time.Minute)
	s.Busy, s.Agents = true, 1
	q.Merge([]sample.Session{s}, at(600))
	if items := q.Items(); len(items) != 0 {
		t.Fatalf("items %+v", items)
	}
}

// The same with hooks: the turn that dispatched the subagents ends with a
// Stop, and their results start the next turn without a prompt. Neither
// waits for you; a question the turn asked still does.
func TestAHookedSessionWorkingThroughSubagentsDoesNotWait(t *testing.T) {
	agents := session("s1", "%1", "api", sample.StatusAgents, time.Minute)
	agents.Busy, agents.Agents = true, 2
	busy := session("s1", "%1", "api", "busy", 0)
	for _, tc := range []struct {
		name string
		stop hooks.Event
		s    sample.Session
		want string // "" = not in the queue
	}{
		{"subagents work", hooks.Event{Message: "Dispatched the implementer."}, agents, ""},
		{"their results run a turn", hooks.Event{Message: "Dispatched the implementer."}, busy, ""},
		{"a question waits anyway", hooks.Event{Message: "Shall I open the PR?", Asks: true}, agents, StateQuestion},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := New()
			ev := tc.stop
			ev.Name, ev.SessionID, ev.Pane, ev.At = "Stop", "s1", "%1", at(0)
			q.Apply(ev)
			q.Merge([]sample.Session{tc.s}, at(60))
			items := q.Items()
			if tc.want == "" && len(items) != 0 || tc.want != "" && (len(items) != 1 || items[0].State != tc.want) {
				t.Fatalf("items %+v, want %q", items, tc.want)
			}
		})
	}
	// The subagents are done and the turn they ran ended: it waits again.
	q := New()
	q.Apply(hooks.Event{Name: "Stop", SessionID: "s1", Pane: "%1", At: at(0)})
	q.Merge([]sample.Session{agents}, at(60))
	q.Merge([]sample.Session{session("s1", "%1", "api", "idle", 0)}, at(120))
	if items := q.Items(); len(items) != 1 || items[0].State != StateFinished || !items[0].Since.Equal(at(120)) {
		t.Fatalf("items %+v, want one finished since the work ended", items)
	}
	// The tools that turn runs send hooks of their own: none of them brings
	// the wait back while the session works.
	q = New()
	q.Apply(hooks.Event{Name: "Stop", SessionID: "s1", Pane: "%1", At: at(0)})
	q.Merge([]sample.Session{agents}, at(60))
	q.Apply(hooks.Event{Name: "PreToolUse", SessionID: "s1", Pane: "%1", At: at(119)})
	q.Merge([]sample.Session{busy}, at(120))
	if items := q.Items(); len(items) != 0 {
		t.Fatalf("items %+v, want none while the results run", items)
	}
	// The turn their results ran ends with a Stop: the wait starts there,
	// not at the turn that dispatched them.
	q = New()
	q.Apply(hooks.Event{Name: "Stop", SessionID: "s1", Pane: "%1", At: at(0)})
	q.Merge([]sample.Session{agents}, at(60))
	q.Apply(hooks.Event{Name: "Stop", SessionID: "s1", Pane: "%1", At: at(600)})
	q.Merge([]sample.Session{session("s1", "%1", "api", "idle", 0)}, at(610))
	if items := q.Items(); len(items) != 1 || !items[0].Since.Equal(at(600)) {
		t.Fatalf("items %+v, want one since the last Stop", items)
	}
}

// A Stop is merged at once with the last sample, read while its turn still
// ran: that busy is the past, and the wait shows from the Stop. So does a
// pane read whose text changes (a status line's clock), which is no work.
func TestAStopWaitsDespiteAStaleOrPaneBusy(t *testing.T) {
	busy := session("s1", "%1", "api", "busy", 0)
	pane := busy
	pane.FromPane = true
	for _, tc := range []struct {
		name string
		s    sample.Session
		now  time.Time
	}{
		{"a sample from before the Stop", busy, at(9)},
		{"a sample just after the Stop", busy, at(10).Add(BusyAfter - time.Second)},
		{"a pane that redraws", pane, at(60)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := New()
			q.Apply(hooks.Event{Name: "Stop", SessionID: "s1", Pane: "%1", At: at(10)})
			q.Merge([]sample.Session{tc.s}, tc.now)
			if items := q.Items(); len(items) != 1 || !items[0].Since.Equal(at(10)) {
				t.Fatalf("items %+v, want one since the Stop", items)
			}
		})
	}
}

func hookAskOf(id string) *said.Ask {
	return &said.Ask{ID: id, Questions: []said.Question{{Question: "Which fruit?", Options: []said.Option{{Label: "Apple"}, {Label: "Banana"}}}}}
}

// askOf is the call the session in pane waits on, after the events.
func askOf(q *Queue, ss []sample.Session, now time.Time, pane string) *said.Ask {
	q.Merge(ss, now)
	for _, s := range q.WithAsks(ss) {
		if s.Pane == pane {
			return s.Ask
		}
	}
	return nil
}

// TestHookAskLifecycle: a PreToolUse of the question tool holds its call
// until a hook says the menu is gone (design 0006 §11.8, auto mode).
func TestHookAskLifecycle(t *testing.T) {
	pre := hooks.Event{Name: "PreToolUse", ToolUseID: "toolu_1", Ask: hookAskOf("toolu_1")}
	for _, tc := range []struct {
		name  string
		after []hooks.Event
		held  bool
	}{
		{"set", nil, true},
		{"a notification keeps it", []hooks.Event{{Name: "Notification", Kind: hooks.KindPermission}}, true},
		{"another tool's PostToolUse keeps it", []hooks.Event{{Name: "PostToolUse", ToolUseID: "toolu_2"}}, true},
		{"another tool's PreToolUse keeps it", []hooks.Event{{Name: "PreToolUse", ToolUseID: "toolu_2"}}, true},
		{"its PostToolUse clears it", []hooks.Event{{Name: "PostToolUse", ToolUseID: "toolu_1"}}, false},
		{"a prompt clears it", []hooks.Event{{Name: "UserPromptSubmit"}}, false},
		{"Stop clears it", []hooks.Event{{Name: "Stop"}}, false},
		{"SessionEnd clears it", []hooks.Event{{Name: "SessionEnd"}}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q := New()
			for i, ev := range append([]hooks.Event{pre}, tc.after...) {
				ev.SessionID, ev.Pane, ev.At = "s1", "%1", at(i)
				q.Apply(ev)
			}
			got := askOf(q, []sample.Session{session("s1", "%1", "api", "idle", time.Minute)}, at(10), "%1")
			if tc.held != (got != nil && got.ID == "toolu_1") {
				t.Fatalf("ask %+v, want held=%v", got, tc.held)
			}
		})
	}
}

func TestHookAskExpires(t *testing.T) {
	q := New()
	q.Apply(hooks.Event{Name: "PreToolUse", SessionID: "s1", Pane: "%1", At: t0, ToolUseID: "toolu_1", Ask: hookAskOf("toolu_1")})
	ss := []sample.Session{session("s1", "%1", "api", "idle", time.Minute)}
	if askOf(q, ss, t0.Add(AskLife-time.Minute), "%1") == nil {
		t.Fatal("gone before its life")
	}
	if got := askOf(q, ss, t0.Add(AskLife+time.Minute), "%1"); got != nil {
		t.Fatalf("still held after its life: %+v", got)
	}
}

// TestHookAskOrder: the transcript's call wins when it is the same call;
// the hook's fills an empty transcript and wins a different (older) call.
func TestHookAskOrder(t *testing.T) {
	q := New()
	q.Apply(hooks.Event{Name: "PreToolUse", SessionID: "s1", Pane: "%1", At: t0, ToolUseID: "toolu_1", Ask: hookAskOf("toolu_1")})
	transcript := &said.Ask{ID: "toolu_1", Questions: []said.Question{{Question: "From the transcript", Options: []said.Option{{Label: "A"}}}}}
	older := &said.Ask{ID: "toolu_0", Questions: transcript.Questions}

	s := session("s1", "%1", "api", "idle", time.Minute)
	if got := askOf(q, []sample.Session{s}, at(1), "%1"); got == nil || got.Questions[0].Question != "Which fruit?" {
		t.Fatalf("an empty transcript: %+v", got)
	}
	s.Ask = transcript
	if got := askOf(q, []sample.Session{s}, at(1), "%1"); got != transcript {
		t.Fatalf("the same call: %+v, want the transcript's", got)
	}
	s.Ask = older
	if got := askOf(q, []sample.Session{s}, at(1), "%1"); got == nil || got.ID != "toolu_1" {
		t.Fatalf("another call: %+v, want the hook's", got)
	}
	// With no hook call the transcript's stays: it is the fallback.
	q.Apply(hooks.Event{Name: "PostToolUse", SessionID: "s1", Pane: "%1", At: at(2), ToolUseID: "toolu_1"})
	s.Ask = transcript
	if got := askOf(q, []sample.Session{s}, at(3), "%1"); got != transcript {
		t.Fatalf("no hook call: %+v", got)
	}
}

// TestWithAsksLeavesTheSampleAlone: the sampler's list is not changed, so
// a call the hooks clear is not left behind in it.
func TestWithAsksLeavesTheSampleAlone(t *testing.T) {
	q := New()
	q.Apply(hooks.Event{Name: "PreToolUse", SessionID: "s1", Pane: "%1", At: t0, ToolUseID: "toolu_1", Ask: hookAskOf("toolu_1")})
	ss := []sample.Session{session("s1", "%1", "api", "idle", time.Minute), session("s2", "%2", "web", "idle", time.Minute)}
	q.Merge(ss, at(1))
	out := q.WithAsks(ss)
	if ss[0].Ask != nil || out[0].Ask == nil || out[1].Ask != nil {
		t.Fatalf("sample %+v, out %+v", ss, out)
	}
}

// TestHookAskByPane: a session the sampler sees without its id (read from
// the pane) gets the call of the hooked session in that pane.
func TestHookAskByPane(t *testing.T) {
	q := New()
	q.Apply(hooks.Event{Name: "PreToolUse", SessionID: "s1", Pane: "%1", At: t0, ToolUseID: "toolu_1", Ask: hookAskOf("toolu_1")})
	if got := askOf(q, []sample.Session{session("", "%1", "api", "idle", time.Minute)}, at(1), "%1"); got == nil {
		t.Fatal("no call by pane")
	}
	if got := askOf(q, []sample.Session{session("", "%2", "web", "idle", time.Minute)}, at(1), "%2"); got != nil {
		t.Fatalf("another pane got the call: %+v", got)
	}
}

// TestHookAskEndsWithItsTurn: Esc on the menu sends no hook, but the
// transcript closes the turn; a turn closed before the call keeps it.
func TestHookAskEndsWithItsTurn(t *testing.T) {
	q := New()
	q.Apply(hooks.Event{Name: "PreToolUse", SessionID: "s1", Pane: "%1", At: at(5), ToolUseID: "toolu_1", Ask: hookAskOf("toolu_1")})
	s := session("s1", "%1", "api", "idle", time.Minute)
	s.TurnEnded = at(1) // the turn before the prompt that asks
	if askOf(q, []sample.Session{s}, at(6), "%1") == nil {
		t.Fatal("an older turn's end cleared the call")
	}
	s.TurnEnded = at(7)
	if got := askOf(q, []sample.Session{s}, at(8), "%1"); got != nil {
		t.Fatalf("the call outlived its turn: %+v", got)
	}
	s.TurnEnded = time.Time{}
	if got := askOf(q, []sample.Session{s}, at(9), "%1"); got != nil {
		t.Fatalf("the call came back: %+v", got)
	}
}

// TestACursorEventJoinsOnlyACursorSession: Cursor's hooks are global, so an
// `agent -p` in another agent's pane sends its events from that pane too.
// They are Cursor's, and never decide another agent's session.
func TestACursorEventJoinsOnlyACursorSession(t *testing.T) {
	q := New()
	q.Apply(hooks.Event{Name: "Stop", Agent: hooks.CursorAgent, SessionID: "chat1", Pane: "%6", At: at(0)})
	codex := session("", "%6", "api", "idle", time.Minute)
	codex.Agent = "codex"
	q.Merge([]sample.Session{codex}, at(10))
	if items := q.Items(); len(items) != 1 || !items[0].Estimated {
		t.Errorf("codex items %+v, want the one estimated from its pane", items)
	}
	cursor := session("", "%6", "api", "idle", time.Minute)
	cursor.Agent = sample.CursorAgent
	q.Merge([]sample.Session{cursor}, at(11))
	if items := q.Items(); len(items) != 1 || items[0].State != StateFinished || items[0].Estimated {
		t.Errorf("cursor items %+v, want one finished from the hook", items)
	}
}

// TestAHookedCursorSessionThatPermitsWaitsForPermission: no Cursor hook
// fires when its approval menu draws, so its pane tells it.
func TestAHookedCursorSessionThatPermitsWaitsForPermission(t *testing.T) {
	q := New()
	q.Apply(hooks.Event{Name: "UserPromptSubmit", Agent: hooks.CursorAgent, SessionID: "chat1", Pane: "%7", At: at(0)})
	q.Apply(hooks.Event{Name: "UserPromptSubmit", SessionID: "c1", Pane: "%8", At: at(0)})
	cursor := session("", "%7", "web", "idle", time.Minute)
	cursor.Agent, cursor.Permits, cursor.FromPane, cursor.LastLine = sample.CursorAgent, true, true, "Run this command?"
	claude := session("c1", "%8", "api", "idle", time.Minute)
	claude.Agent, claude.Permits = "claude", true
	q.Merge([]sample.Session{cursor, claude}, at(10))
	items := q.Items()
	if len(items) != 1 || items[0].Pane != "%7" || items[0].State != StatePermission || items[0].LastLine != "Run this command?" {
		t.Errorf("items %+v, want one permission wait of %%7", items)
	}
}

// TestACursorReplyFillsItsFinishedTurn: Cursor's reply comes after its
// stop, in afterAgentResponse. It fills the finished turn's line, and a
// reply that asks makes it a question; with no finished turn it is nothing.
func TestACursorReplyFillsItsFinishedTurn(t *testing.T) {
	cursor := session("", "%6", "app", "idle", time.Minute)
	cursor.Agent = sample.CursorAgent
	stop := hooks.Event{Name: "Stop", Agent: hooks.CursorAgent, SessionID: "chat1", Pane: "%6", Turn: "g1"}
	reply := hooks.Event{Name: hooks.Reply, Agent: hooks.CursorAgent, SessionID: "chat1", Pane: "%6", Turn: "g1", Message: "Shall I also push it?", Asks: true}
	// Cursor runs both hooks at once: they come in either order, each
	// stamped when its own process read it.
	for name, evs := range map[string][]hooks.Event{
		"stop, then reply":              {at0(stop, 0), at0(reply, 1)},
		"reply, then stop":              {at0(reply, 1), at0(stop, 0)},
		"reply stamped before the stop": {at0(stop, 1), at0(reply, 0)},
	} {
		q := New()
		for _, ev := range evs {
			q.Apply(ev)
		}
		q.Merge([]sample.Session{cursor}, at(10))
		if items := q.Items(); len(items) != 1 || items[0].State != StateQuestion || items[0].LastLine != "Shall I also push it?" {
			t.Errorf("%s: items %+v, want one question", name, items)
		}
	}
	other := reply
	other.Turn = "g0"
	q := New()
	q.Apply(at0(other, 0))
	q.Apply(at0(stop, 1))
	q.Merge([]sample.Session{cursor}, at(10))
	if items := q.Items(); len(items) != 1 || items[0].State != StateFinished || items[0].LastLine != "" {
		t.Errorf("a reply of another turn: %+v, want finished with no line", items)
	}
	q = New()
	q.Apply(hooks.Event{Name: "UserPromptSubmit", Agent: hooks.CursorAgent, SessionID: "chat1", Pane: "%6", At: at(0)})
	q.Apply(hooks.Event{Name: hooks.Reply, Agent: hooks.CursorAgent, SessionID: "chat1", Pane: "%6", At: at(1), Message: "partial"})
	q.Merge([]sample.Session{cursor}, at(10))
	if items := q.Items(); len(items) != 0 {
		t.Errorf("a reply with no finished turn: %+v, want none", items)
	}
}

func at0(ev hooks.Event, s int) hooks.Event {
	ev.At = at(s)
	return ev
}

// TestAnUnpairedReplyIsDropped: a Reply whose session never stops (an
// `agent -p` the sampler never sees) is not kept for ever.
func TestAnUnpairedReplyIsDropped(t *testing.T) {
	q := New()
	q.Apply(hooks.Event{Name: hooks.Reply, Agent: hooks.CursorAgent, SessionID: "stray", Turn: "g1", At: at(0)})
	q.Merge(nil, at(0).Add(UnseenGrace+time.Second))
	if len(q.replies) != 0 {
		t.Errorf("replies %+v, want none", q.replies)
	}
}

// TestTwoChatsInOnePaneJoinTheNewer: an interactive Cursor that runs
// `agent -p` with its shell tool sends two chats' events from one pane.
// The pane's session is the chat that spoke last, the same on every merge.
func TestTwoChatsInOnePaneJoinTheNewer(t *testing.T) {
	cursor := session("", "%6", "app", "idle", time.Minute)
	cursor.Agent = sample.CursorAgent
	q := New()
	q.Apply(hooks.Event{Name: "UserPromptSubmit", Agent: hooks.CursorAgent, SessionID: "print", Pane: "%6", At: at(0)})
	q.Apply(hooks.Event{Name: "Stop", Agent: hooks.CursorAgent, SessionID: "chat", Pane: "%6", At: at(5)})
	for i := range 50 {
		q.Merge([]sample.Session{cursor}, at(10+i))
		if items := q.Items(); len(items) != 1 || items[0].SessionID != "chat" || items[0].State != StateFinished {
			t.Fatalf("merge %d: items %+v, want the chat's finished turn", i, items)
		}
	}
}

// TestAForgottenPaneShowsNoneOfItsOldAgentsWaits: after a switch the old
// agent's late hooks do not speak for the new one in the pane.
func TestAForgottenPaneShowsNoneOfItsOldAgentsWaits(t *testing.T) {
	q := New()
	q.Apply(hooks.Event{Name: "Stop", SessionID: "old", Pane: "%1", At: at(0)})
	q.Merge([]sample.Session{{SessionID: "old", Pane: "%1", Status: "idle"}}, at(1))
	if items := q.Items(); len(items) != 1 || items[0].SessionID != "old" {
		t.Fatalf("the old agent does not wait: %+v", items)
	}
	q.Forget(q.HookedIn("%1"), at(1))
	next := sample.Session{Pane: "%1", Agent: "codex", Status: "busy", Busy: true}
	q.Merge([]sample.Session{next}, at(2))
	if items := q.Items(); len(items) != 0 {
		t.Fatalf("the forgotten agent still waits: %+v", items)
	}
	q.Apply(hooks.Event{Name: "Stop", SessionID: "old", Pane: "%1", At: at(3)})
	q.Merge([]sample.Session{next}, at(4))
	if items := q.Items(); len(items) != 0 {
		t.Fatalf("a late hook of the old agent makes the new one wait: %+v", items)
	}
	q.Apply(hooks.Event{Name: "Stop", SessionID: "new", Pane: "%1", At: at(5)})
	q.Merge([]sample.Session{{SessionID: "new", Pane: "%1", Agent: "codex", Status: "idle"}}, at(6))
	if items := q.Items(); len(items) != 1 || items[0].SessionID != "new" {
		t.Fatalf("the pane is deaf to its new agent: %+v", items)
	}
}

// TestForgetLeavesAnIDThatHookedInAfter: the ids are taken before the
// switch; a new agent that hooks into the pane before Forget is heard.
func TestForgetLeavesAnIDThatHookedInAfter(t *testing.T) {
	q := New()
	q.Apply(hooks.Event{Name: "Stop", SessionID: "old", Pane: "%1", At: at(0)})
	old := q.HookedIn("%1")
	q.Apply(hooks.Event{Name: "Stop", SessionID: "new", Pane: "%1", At: at(1)})
	q.Forget(old, at(2))
	if q.Pane("old") != "" || q.Pane("new") != "%1" {
		t.Fatalf("old %q new %q, want only old forgotten", q.Pane("old"), q.Pane("new"))
	}
	q.Merge([]sample.Session{{SessionID: "new", Pane: "%1", Agent: "codex", Status: "idle"}}, at(3))
	if items := q.Items(); len(items) != 1 || items[0].SessionID != "new" || items[0].Estimated {
		t.Fatalf("the new agent's hook was forgotten: %+v", items)
	}
}

// TestAForgottenSessionIsHeardAgainAfterTheGrace: the forgotten id's late
// hooks are dropped for UnseenGrace, then the queue lets go of it.
func TestAForgottenSessionIsHeardAgainAfterTheGrace(t *testing.T) {
	q := New()
	q.Apply(hooks.Event{Name: "Stop", SessionID: "old", Pane: "%1", At: at(0)})
	q.Forget(q.HookedIn("%1"), at(1))
	late := int(UnseenGrace / time.Second)
	q.Apply(hooks.Event{Name: "Stop", SessionID: "old", Pane: "%1", At: at(2)})
	q.Merge([]sample.Session{{SessionID: "old", Pane: "%1", Status: "idle"}}, at(1+late))
	if _, ok := q.hooked["old"]; ok {
		t.Fatal("a late hook within the grace was kept")
	}
	q.Merge(nil, at(2+late))
	q.Apply(hooks.Event{Name: "Stop", SessionID: "old", Pane: "%1", At: at(3 + late)})
	q.Merge([]sample.Session{{SessionID: "old", Pane: "%1", Status: "idle"}}, at(4+late))
	if items := q.Items(); len(items) != 1 || items[0].SessionID != "old" {
		t.Fatalf("the forgotten id is deaf after the grace: %+v", items)
	}
}
