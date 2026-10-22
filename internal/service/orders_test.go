package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/blox-eng/matchblox/internal/panes"
	"github.com/blox-eng/matchblox/internal/proto"
	"github.com/blox-eng/matchblox/internal/sample"
	"github.com/blox-eng/matchblox/internal/state"
	"github.com/blox-eng/matchblox/internal/stoker"
)

// orderTest is handsTest with the stoker off, as the builder finds it
// before a launch, and a Codex session "cx" in %2.
func orderTest(t *testing.T) (*Service, *[][]string) {
	t.Helper()
	s, ran := handsTest(t)
	// night0 is on the real clock: a night that ends at 07:00 ends mid-test
	// when CI runs at 08:00. Its morning is half a day off instead.
	s.NightEnds = night0.Add(12 * time.Hour).Format("15:04")
	if err := s.Stoker.Set(stoker.Mode{}); err != nil {
		t.Fatal(err)
	}
	s.publish(sample.Snapshot{At: night0, Sessions: []sample.Session{
		{Agent: "claude", Pane: "%1", Target: "app:1.1", SessionID: "s1", Name: "work", Status: "idle", Idle: 5 * time.Minute},
		{Agent: "codex", Pane: "%2", Target: "app:1.2", SessionID: "s2", Name: "cx", Status: "idle", Idle: 5 * time.Minute},
		{Agent: "claude", Pane: "%9@mbtest", Target: "stoker:1.1", SessionID: "s9", Name: "stoker", Status: "busy", Busy: true},
	}}, func(state.Doc) {})
	return s, ran
}

// typed are the lines typed into pane, in order.
// typed are the lines typed into pane, on the tmux server it is on: the
// builder's %9 is not the stoker's %9@mbtest.
func typed(ran [][]string, pane string) []string {
	tmux, id := sample.Tmux(pane)
	var out []string
	for _, argv := range ran {
		if len(argv) > len(tmux) && slices.Equal(argv[:len(tmux)], tmux) && argv[len(tmux)] == "send-keys" &&
			slices.Contains(argv, id) && slices.Contains(argv, "-l") {
			out = append(out, argv[len(argv)-1])
		}
	}
	return out
}

func orderAct(t *testing.T, words string, night bool, grants ...string) proto.Act {
	t.Helper()
	b, err := json.Marshal(map[string]any{"words": words, "night": night, "grants": grants})
	if err != nil {
		t.Fatal(err)
	}
	return proto.Act{Console: true, RecID: "stoker:order", Confirm: "y", Text: string(b)}
}

const twoSteps = `{"steps":[{"n":1,"session":"work","text":"merge #1 on green"},{"n":2,"session":"cx","text":"rebase on main"}],"grants":["merge"]}`

func mustAct(t *testing.T, s *Service, a proto.Act) {
	t.Helper()
	if res := s.act(t.Context(), a); res.Err != "" {
		t.Fatalf("%s: %s", a.RecID, res.Err)
	}
}

// planned is an order whose chips give releases, and the plan the stoker
// wrote for it.
func planned(t *testing.T, s *Service, words string, night bool) {
	t.Helper()
	mustAct(t, s, orderAct(t, words, night, "release"))
	mustAct(t, s, fromStokerAct("stoker:plan", twoSteps))
}

func TestOrderWithoutTheStokerAgentRunning(t *testing.T) {
	s, ran := orderTest(t)
	// matchblox starts no agent here: none runs on the stoker's server.
	s.publish(sample.Snapshot{At: night0, Sessions: []sample.Session{
		{Agent: "claude", Pane: "%1", Target: "app:1.1", SessionID: "s1", Name: "work", Status: "idle"},
	}}, func(state.Doc) {})
	if v := s.snapshotView(); v.PaneUp {
		t.Fatal("no agent on the stoker's server, yet PaneUp")
	}
	res := s.act(t.Context(), orderAct(t, "merge them", true))
	if res.Err != "the stoker is not running: f or n starts it" || len(*ran) != 0 {
		t.Fatalf("err %q ran %q", res.Err, *ran)
	}
	if m := s.Stoker.Mode(); m.On || m.Order != nil {
		t.Fatalf("mode %+v", m)
	}

	// Its command is not on PATH: the order turns today's stoker on, for
	// the night, and keeps the words.
	s.LookPath = func(string) (string, error) { return "", errors.New("not found") }
	mustAct(t, s, orderAct(t, "merge them", true))
	if m := s.Stoker.Mode(); !m.On || m.Order == nil || m.Order.Words != "merge them" || len(*ran) != 0 {
		t.Fatalf("mode %+v ran %q", m, *ran)
	}
	mustAct(t, s, proto.Act{Console: true, RecID: "stoker:off"})

	// No stoker pane configured: the order turns today's stoker on, for
	// the night, and keeps the words.
	s.StokerServer = ""
	mustAct(t, s, orderAct(t, "merge them", true))
	m := s.Stoker.Mode()
	if !m.On || !m.Nightly || m.Order == nil || m.Order.Words != "merge them" || m.Order.State != stoker.OrderRunning || len(*ran) != 0 {
		t.Fatalf("mode %+v order %+v ran %q", m, m.Order, *ran)
	}
}

func TestOrderStartsTheRunAtOnce(t *testing.T) {
	s, ran := orderTest(t)
	mustAct(t, s, orderAct(t, "merge them on green", true, "merge", "release", "merge"))
	path := filepath.Join(s.Stoker.Dir(), "stoker-brief.md")
	got := typed(*ran, "%9@mbtest")
	if len(got) != 1 || got[0] != "Read "+path+" and follow it." || len(got[0]) > panes.MaxAnswer {
		t.Fatalf("typed %q", got)
	}
	m := s.Stoker.Mode()
	if !m.On || !m.Nightly || m.Until.IsZero() || len(m.Start) == 0 || m.Order == nil || m.Order.State != stoker.OrderRunning ||
		!m.Order.Night || m.Order.ID == "" || !slices.Equal(m.Order.Grants, []string{"merge", "release"}) {
		t.Fatalf("mode %+v order %+v", m, m.Order)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := stoker.Brief(*m.Order, s.Ways, stoker.Hands, s.board(m.Order.At)); string(b) != want {
		t.Fatalf("the file holds\n%s\nwant\n%s", b, want)
	}
	if !strings.Contains(string(b), "> merge them on green") || !strings.Contains(string(b), "Run "+m.Order.ID+" starts now") ||
		!strings.Contains(string(b), "This run gives merges and releases: deploys and kills ask the builder.") ||
		!strings.Contains(string(b), "- work (%1): idle · out of work · context 0%") || strings.Contains(string(b), "- stoker (%9@mbtest)") {
		t.Fatalf("brief:\n%s", b)
	}
}

func TestOrderTakesOnlyTheChips(t *testing.T) {
	s, ran := orderTest(t)
	for _, g := range []string{"push", "merge:#4", "merge:@work"} {
		if res := s.act(t.Context(), orderAct(t, "x", false, g)); res.Err != fmt.Sprintf("%q is no go: merge, release, deploy or kill", g) {
			t.Errorf("chip %s: %q", g, res.Err)
		}
	}
	if m := s.Stoker.Mode(); m.On || m.Order != nil || len(*ran) != 0 {
		t.Fatalf("a refused order ran: %+v %q", m, *ran)
	}
}

func TestPlanFromStokerOnly(t *testing.T) {
	s, _ := orderTest(t)
	if res := s.act(t.Context(), fromStokerAct("stoker:plan", twoSteps)); res.Err != "no order to plan: launch one first" {
		t.Fatalf("a plan with no order: %q", res.Err)
	}
	mustAct(t, s, orderAct(t, "merge them", false))
	for _, from := range []string{"", "%1"} {
		a := proto.Act{RecID: "stoker:plan", Confirm: "y", Text: twoSteps, From: from}
		if res := s.act(t.Context(), a); res.Err != "only the stoker writes its plan" {
			t.Fatalf("from %q: %q", from, res.Err)
		}
	}
	if o := s.Stoker.Mode().Order; o.State != stoker.OrderRunning || len(o.Plan) != 0 {
		t.Fatalf("a refused plan landed: %+v", o)
	}
	for text, want := range map[string]string{
		`{"steps":[{"n":1,"session":"nobody","text":"x"}]}`: "step 1: no session named nobody",
		`{"steps":[]}`: "the plan has no step",
	} {
		if res := s.act(t.Context(), fromStokerAct("stoker:plan", text)); res.Err != want {
			t.Errorf("%s: %q, want %q", text, res.Err, want)
		}
	}
	mustAct(t, s, fromStokerAct("stoker:plan", twoSteps))
	o := s.Stoker.Mode().Order
	// The plan's own goes are read and ignored: the goes are the chips'.
	if o.State != stoker.OrderRunning || len(o.Plan) != 2 || o.Plan[1].Session != "cx" || len(o.Grants) != 0 {
		t.Fatalf("order %+v", o)
	}
	// cx is Codex, which has no merge guard, and the order gave no go.
	if len(o.Unguarded) != 1 || o.Unguarded[0].Session != "cx" || !slices.Equal(o.Unguarded[0].Goes, []string{"merge", "release", "deploy"}) {
		t.Fatalf("unguarded %+v", o.Unguarded)
	}

	// A new plan replaces the one before it, with no go and no restart.
	mustAct(t, s, fromStokerAct("stoker:plan", `{"steps":[{"n":2,"session":"work","text":"merge #2"}]}`))
	if n := s.Stoker.Mode().Order; len(n.Plan) != 1 || n.Plan[0].Text != "merge #2" || n.ID != o.ID || !n.Went.Equal(o.Went) || len(n.Unguarded) != 0 {
		t.Fatalf("the plan was not replaced: %+v", n)
	}

	mustAct(t, s, proto.Act{Console: true, RecID: "stoker:off", Confirm: "y"})
	if res := s.act(t.Context(), fromStokerAct("stoker:plan", twoSteps)); res.Err != "the run ended: a new order starts a new one" {
		t.Fatalf("a plan after the run: %q", res.Err)
	}
}

func TestNightIsSealed(t *testing.T) {
	s, ran := orderTest(t)
	planned(t, s, "merge them", true)
	n := len(*ran)
	res := s.act(t.Context(), orderAct(t, "and the docs", false))
	if res.Err != "the night is sealed: n stops it, then launch a new one" || len(*ran) != n {
		t.Fatalf("err %q ran %q", res.Err, (*ran)[n:])
	}
	if o := s.Stoker.Mode().Order; o.Words != "merge them" || o.State != stoker.OrderRunning {
		t.Fatalf("order %+v", o)
	}
}

func TestStepLogsToRun(t *testing.T) {
	s, _ := orderTest(t)
	planned(t, s, "merge them", false)
	mustAct(t, s, fromStokerAct("stoker:step", `{"step":1,"state":"doing","text":"reading #1"}`))
	mustAct(t, s, fromStokerAct("stoker:step", `{"step":1,"state":"waits","needs":"merge:#4556","text":"green, waits for the merge"}`))
	mustAct(t, s, fromStokerAct("stoker:step", `{"step":0,"rule":"compact on PR","state":"done","text":"compacted work"}`))
	for text, want := range map[string]string{
		`{"step":1,"state":"sleeping","text":"x"}`:             `"sleeping" is no state: doing, done, failed or waits`,
		`{"step":7,"state":"done","text":"x"}`:                 "the plan has no step 7",
		`{"step":0,"state":"done","text":"x"}`:                 "step 0 is a standing rule: name it with --rule",
		`{"step":1,"state":"done","text":""}`:                  "a step needs its text",
		`{"step":1,"state":"waits","needs":"push","text":"x"}`: `"push" is no grant: merge, release, deploy, merge:#<n> or merge:@<session>`,
	} {
		if res := s.act(t.Context(), fromStokerAct("stoker:step", text)); res.Err != want {
			t.Errorf("%s: %q, want %q", text, res.Err, want)
		}
	}
	a := proto.Act{RecID: "stoker:step", Confirm: "y", Text: `{"step":1,"state":"done","text":"x"}`, From: "%1"}
	if res := s.act(t.Context(), a); res.Err != "only the stoker logs its steps" {
		t.Fatalf("a step from %%1: %q", res.Err)
	}

	var steps []stoker.Entry
	for _, e := range s.Stoker.Entries() {
		if e.Logged() {
			steps = append(steps, e)
		}
	}
	if len(steps) != 3 || steps[0].Text != "reading #1" {
		t.Fatalf("steps %+v", steps)
	}
	if run := s.Stoker.View().Run; len(run) != 3 || run[1].Text != "green, waits for the merge" {
		t.Fatalf("run %+v", run)
	}
	w := steps[1]
	if w.Step != 1 || w.State != stoker.StepWaits || w.Needs != "merge:#4556" || w.Text != "green, waits for the merge" || w.Session != "s1" || w.Name != "work" || w.Pane != "%1" || w.Ordered() {
		t.Fatalf("the waiting step %+v", w)
	}
	if r := steps[2]; r.Step != 0 || r.Rule != "compact on PR" || r.Text != "compacted work" {
		t.Fatalf("the rule step %+v", r)
	}
	if v := s.snapshotView(); v.Run == nil || !v.PaneUp {
		t.Fatalf("the console's view %+v", v)
	}
}

func TestViewNamesTheRecipes(t *testing.T) {
	s, _ := orderTest(t)
	s.Ways = filepath.Join(t.TempDir(), "ways.md")
	if err := os.WriteFile(s.Ways, []byte("# Recipe: merge order\n\n# Recipe: tidy\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s.refreshStoker()
	if v := s.snapshotView(); !slices.Equal(v.Recipes, []string{"merge order", "tidy"}) {
		t.Fatalf("recipes %q", v.Recipes)
	}
}

// snapshotView is the stoker view the consoles get now.
func (s *Service) snapshotView() stoker.View {
	s.mu.Lock()
	defer s.mu.Unlock()
	return *s.cur.Stoker
}

func TestStokerVerbsAreTheBuilders(t *testing.T) {
	s, ran := orderTest(t)
	for _, verb := range []string{"on", "off", "rest", "night", "ack"} {
		if res := s.act(t.Context(), fromStokerAct("stoker:"+verb, "")); res.Err != "the stoker does not start, stop or rest itself" {
			t.Fatalf("stoker:%s from the stoker: %q", verb, res.Err)
		}
	}
	if res := s.act(t.Context(), fromStokerAct("stoker:order", `{"words":"x"}`)); res.Err != "the stoker does not give itself orders" {
		t.Fatalf("stoker:order from the stoker: %q", res.Err)
	}
	if m := s.Stoker.Mode(); m.On || m.Order != nil || len(*ran) != 0 {
		t.Fatalf("mode %+v ran %q", m, *ran)
	}
	mustAct(t, s, proto.Act{Console: true, RecID: "stoker:on", Confirm: "y"}) // the builder's own
	if res := s.act(t.Context(), fromStokerAct("stoker:off", "")); res.Err == "" || !s.Stoker.Mode().On {
		t.Fatalf("the stoker turned itself off: %q", res.Err)
	}
}

func TestThePlanIsWrittenWhileTheStokerRests(t *testing.T) {
	s, _ := orderTest(t)
	mustAct(t, s, orderAct(t, "A", false))
	mustAct(t, s, proto.Act{Console: true, RecID: "stoker:rest", Confirm: "y"})
	mustAct(t, s, fromStokerAct("stoker:plan", twoSteps))
	mustAct(t, s, fromStokerAct("stoker:step", `{"step":1,"state":"doing","text":"reading"}`))
	if o := s.Stoker.Mode().Order; o.Words != "A" || len(o.Plan) != 2 {
		t.Fatalf("order %+v", o)
	}
	// Its messages still wait for the builder to leave.
	if res := s.act(t.Context(), fromStokerAct("message:%1", "go on")); res.Err != "the stoker rests: a person works" {
		t.Fatalf("a message while resting: %q", res.Err)
	}
}

func TestBlankOrderIsAccepted(t *testing.T) {
	s, _ := orderTest(t)
	mustAct(t, s, orderAct(t, "  ", false))
	b, err := os.ReadFile(filepath.Join(s.Stoker.Dir(), "stoker-brief.md"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "No order this time: follow the standing rules in your ways file.") {
		t.Fatalf("brief:\n%s", b)
	}
}

// A run that ended, a restart of the service, then a new order: the new
// order shows only its own plan, and nothing of the run before it.
func TestNewOrderAfterARestartShowsOnlyItsOwn(t *testing.T) {
	s, _ := orderTest(t)
	planned(t, s, "merge them on green", false)
	mustAct(t, s, fromStokerAct("stoker:step", `{"step":1,"state":"done","text":"merged #1"}`))
	mustAct(t, s, proto.Act{Console: true, RecID: "stoker:off", Confirm: "y"})
	if m := s.Stoker.Mode(); m.Card == nil || m.Order == nil || len(m.Order.Plan) == 0 {
		t.Fatalf("the ended run keeps its card and plan: %+v", m)
	}
	st, err := stoker.Open(s.Stoker.Dir()) // the service restarts
	if err != nil {
		t.Fatal(err)
	}
	s.Stoker = st

	mustAct(t, s, orderAct(t, "review the new PRs", false))
	m := s.Stoker.Mode()
	if m.Order == nil || m.Order.Words != "review the new PRs" || len(m.Order.Plan) != 0 {
		t.Fatalf("the new order shows another plan: %+v", m.Order)
	}
	if m.Card != nil {
		t.Fatalf("the new order shows the old run's card: %+v", m.Card)
	}
	for _, e := range s.Stoker.View().Run {
		if e.Logged() {
			t.Fatalf("the new order's card shows the old run's step %+v", e)
		}
	}
	b, err := os.ReadFile(filepath.Join(s.Stoker.Dir(), "stoker-brief.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "merge #1 on green") || strings.Contains(string(b), "merged #1") {
		t.Fatalf("the brief carries the old run:\n%s", b)
	}
	runs, err := s.Stoker.Runs()
	if err != nil || len(runs) != 1 {
		t.Fatalf("the old run's card is kept in the runs file: %v %v", runs, err)
	}
}

// TestADayOrderIsOneLine: by day, o's order is typed into the stoker's
// pane as one line. The run goes on: no restart, no new brief, done steps
// stay done, and the run's goes become the sheet's chips.
func TestADayOrderIsOneLine(t *testing.T) {
	s, ran := orderTest(t)
	mustAct(t, s, orderAct(t, "merge them", false))
	mustAct(t, s, fromStokerAct("stoker:plan", twoSteps))
	mustAct(t, s, fromStokerAct("stoker:step", `{"step":1,"state":"done","text":"merged #1"}`))
	mustAct(t, s, fromStokerAct("message:%1", "rebase on main"))
	was := s.Stoker.Mode()
	path := filepath.Join(s.Stoker.Dir(), "stoker-brief.md")
	brief, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	mustAct(t, s, orderAct(t, "now the docs", false, "merge"))
	if got := typed(*ran, "%9@mbtest"); len(got) != 2 || got[1] != "New order: now the docs" {
		t.Fatalf("typed %q", got)
	}
	m := s.Stoker.Mode()
	o := m.Order
	if !m.On || !m.Since.Equal(was.Since) || o.ID != was.Order.ID || !o.Went.Equal(was.Order.Went) || o.State != stoker.OrderRunning ||
		o.Words != "now the docs" || len(o.Earlier) != 1 || o.Earlier[0].Words != "merge them" || !slices.Equal(o.Grants, []string{"merge"}) || len(o.Plan) != 2 {
		t.Fatalf("mode %+v order %+v", m, o)
	}
	if again, _ := os.ReadFile(path); string(again) != string(brief) {
		t.Fatalf("a new brief was written:\n%s", again)
	}
	if run := s.Stoker.View().Run; !slices.ContainsFunc(run, func(e stoker.Entry) bool { return e.Step == 1 && e.State == stoker.StepDone }) {
		t.Fatalf("the done step is gone: %+v", run)
	}
	// The pane the stoker drove has the new goes at once.
	if denies(t, s, "%1", "gh pr merge 4556") {
		t.Fatal("the new order's merge go did not reach the driven pane")
	}

	// A long order goes to a file the stoker is told to read.
	long := strings.Repeat("and then the next one ", 30)
	mustAct(t, s, orderAct(t, long, false))
	got := typed(*ran, "%9@mbtest")
	file := filepath.Join(s.Stoker.Dir(), "stoker-order.md")
	if len(got) != 3 || got[2] != "Read "+file+" and follow it." {
		t.Fatalf("typed %q", got)
	}
	if b, err := os.ReadFile(file); err != nil || !strings.Contains(string(b), "New order: "+strings.TrimSpace(long)) {
		t.Fatalf("the order file: %v\n%s", err, b)
	}
	if o := s.Stoker.Mode().Order; len(o.Earlier) != 2 || o.Earlier[1].Words != "now the docs" || len(o.Grants) != 0 {
		t.Fatalf("order %+v", o)
	}
	if !denies(t, s, "%1", "gh pr merge 4556") {
		t.Fatal("the chips turned merges off and the driven pane may still merge")
	}
}

// TestADayOrderIsTypedOnlyIntoTheRunThatIsOn: the run is checked under the
// mode's lock right before the line is typed, so a run that ends meanwhile
// never leaves the stoker reading a new order its chips did not take.
func TestADayOrderIsTypedOnlyIntoTheRunThatIsOn(t *testing.T) {
	s, ran := orderTest(t)
	mustAct(t, s, orderAct(t, "merge them", false))
	run := s.Actions.Run
	done := make(chan struct{})
	s.Actions.Run = func(argv []string) error {
		if strings.HasPrefix(argv[len(argv)-1], "New order:") {
			go func() {
				defer close(done)
				_, _ = s.Stoker.Update(func(m stoker.Mode) (stoker.Mode, bool) { return m.Off(night0), true })
			}()
			select { // the off lands now, unless the lock holds it back
			case <-done:
			case <-time.After(200 * time.Millisecond):
			}
		}
		return run(argv)
	}
	res := s.act(t.Context(), orderAct(t, "now the docs", false, "merge"))
	<-done
	if typedIt := slices.Contains(typed(*ran, "%9@mbtest"), "New order: now the docs"); res.Err != "" && typedIt {
		t.Fatalf("the stoker read a new order the run refused: %q", res.Err)
	}
	if s.Stoker.Mode().On {
		t.Fatal("the run was not turned off")
	}
}
