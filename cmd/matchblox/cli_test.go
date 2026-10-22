package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/blox-eng/matchblox/internal/holds"
	"github.com/blox-eng/matchblox/internal/idle"
	"github.com/blox-eng/matchblox/internal/proto"
	"github.com/blox-eng/matchblox/internal/said"
	"github.com/blox-eng/matchblox/internal/sample"
	"github.com/blox-eng/matchblox/internal/ships"
	"github.com/blox-eng/matchblox/internal/state"
	"github.com/blox-eng/matchblox/internal/stoker"
	"github.com/blox-eng/matchblox/internal/transport"
)

func cliDoc() state.Doc {
	var d state.Doc
	d.At = time.Now()
	d.Sessions = []sample.Session{
		{Pane: "%3", Target: "app:1.1", Name: "api", SessionID: "s3", Status: "idle", ContextPct: 41, Account: "a@b.c · Max", Progress: &sample.Progress{Pct: 60}},
		{Pane: "%4", Target: "app:1.2", Name: "web", SessionID: "s4", Status: "busy", Busy: true},
		{Pane: "%5", Target: "app:2.1", Name: "web", SessionID: "s5", Status: "idle", Asks: true},
	}
	d.Reasons = map[string]idle.Reason{"%3": {Word: "#4556 waits your merge", Tone: "warn"}}
	d.Holds = []holds.Hold{{Pane: "%3", SessionID: "s3", By: "stoker"}}
	d.Ships = &ships.Report{Ships: []ships.Ship{{Repo: "o/r", Number: 4556, Stage: ships.StageGreen, Kind: ships.WaitsForYou, Note: "green", Session: ships.SessionRef{Pane: "%3"}}}}
	return d
}

func TestSessionsJSONCarriesReasonAndHold(t *testing.T) {
	var buf bytes.Buffer
	if err := writeSessions(&buf, cliDoc(), true); err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rows); err != nil {
		t.Fatalf("%v: %s", err, buf.String())
	}
	if len(rows) != 3 {
		t.Fatalf("%d rows", len(rows))
	}
	r := rows[0]
	if r["reason"] != "#4556 waits your merge" || r["held"] != true || r["word"] != "idle" || r["pane"] != "%3" || r["context_pct"] != 41.0 || r["plan_pct"] != 60.0 {
		t.Fatalf("row %v", r)
	}
	ship, _ := r["ship"].(map[string]any)
	if ship["number"] != 4556.0 || ship["stage"] != "green" || ship["repo"] != "o/r" {
		t.Fatalf("ship %v", r["ship"])
	}
	if rows[1]["ship"] != nil || rows[1]["held"] != false || rows[1]["word"] != "busy" || rows[2]["word"] != "asks" {
		t.Fatalf("rows %v", rows[1:])
	}
	if r["stoker"] != false || rows[0]["stoker"] != false {
		t.Fatalf("stoker flag %v", r["stoker"])
	}
}

func TestSessionsJSONMarksTheStokerPane(t *testing.T) {
	d := cliDoc()
	d.StokerServer = "mbtest"
	// The stoker's %9 is on its own server; the builder's %9 is not it.
	d.Sessions = append(d.Sessions, sample.Session{Pane: "%9@mbtest", Target: "stoker:1.1", Name: "boss"},
		sample.Session{Pane: "%9", Target: "stoker:1.1", Name: "mine"})
	var buf bytes.Buffer
	if err := writeSessions(&buf, d, true); err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	for _, r := range rows {
		if want := r["pane"] == "%9@mbtest"; r["stoker"] != want {
			t.Fatalf("%v: stoker %v", r["pane"], r["stoker"])
		}
	}
}

func TestResolveByPaneTargetName(t *testing.T) {
	d := cliDoc()
	for who, pane := range map[string]string{"%3": "%3", "app:1.2": "%4", "api": "%3"} {
		s, err := resolve(d, who)
		if err != nil || s.Pane != pane {
			t.Errorf("%s: %v %v", who, s.Pane, err)
		}
	}
}

func TestResolveRefusesAmbiguousName(t *testing.T) {
	_, err := resolve(cliDoc(), "web")
	if err == nil || err.Error() != "web is 2 sessions: %4, %5; name the pane" {
		t.Fatalf("%v", err)
	}
	if _, err := resolve(cliDoc(), "nope"); err == nil || err.Error() != "no session named nope" {
		t.Fatalf("%v", err)
	}
}

func TestCLINeedsAFreshService(t *testing.T) {
	path := isolate(t)
	if _, err := readDoc(path, staleAfter); err == nil || err.Error() != "matchblox is not running: matchblox serve" {
		t.Fatalf("no service: %v", err)
	}
	if err := state.Write(state.Path(), cliDoc()); err != nil {
		t.Fatal(err)
	}
	if d, err := readDoc(path, staleAfter); err != nil || len(d.Sessions) != 3 {
		t.Fatalf("state file: %v", err)
	}
	old := cliDoc()
	old.At = time.Now().Add(-time.Minute)
	if err := state.Write(state.Path(), old); err != nil {
		t.Fatal(err)
	}
	if _, err := readDoc(path, staleAfter); err == nil || !strings.Contains(err.Error(), "not running") {
		t.Fatalf("stale: %v", err)
	}
	fresh := filepath.Join(filepath.Dir(path), "fresh.sock")
	fakeService(t, fresh, time.Now())
	if _, err := readDoc(fresh, staleAfter); err != nil {
		t.Fatal(err)
	}
}

func TestShipsAndUsageText(t *testing.T) {
	var buf bytes.Buffer
	if err := writeShips(&buf, cliDoc(), false); err != nil || !strings.Contains(buf.String(), "o/r#4556") || !strings.Contains(buf.String(), "green") {
		t.Fatalf("%v %q", err, buf.String())
	}
	buf.Reset()
	if err := writeSessions(&buf, cliDoc(), false); err != nil || strings.Count(buf.String(), "\n") != 3 {
		t.Fatalf("%v %q", err, buf.String())
	}
	buf.Reset()
	if err := writeUsage(&buf, cliDoc(), true); err != nil || strings.TrimSpace(buf.String()) != "null" && !strings.HasPrefix(buf.String(), "[") {
		t.Fatalf("%v %q", err, buf.String())
	}
}

func TestAHoldFollowsTheSessionNotThePane(t *testing.T) {
	d := cliDoc()
	d.Sessions[0].SessionID = "fresh-agent"
	if heldBy(d, d.Sessions[0]) {
		t.Fatal("a new agent in a parked pane was shown as held")
	}
	var buf bytes.Buffer
	if err := writeSessions(&buf, d, true); err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rows); err != nil || rows[0]["held"] != false {
		t.Fatalf("%v %v", err, rows)
	}
}

func TestSendNamesThePaneAndTheSender(t *testing.T) {
	a, err := handAct(cliDoc(), "send", []string{"api", "fix", "the", "build"}, 0, "", "%9")
	if err != nil || a.RecID != "message:%3" || a.Text != "fix the build" || a.Confirm != "y" || a.From != "%9" {
		t.Fatalf("%+v %v", a, err)
	}
	for cmd, want := range map[string]string{"compact": "compact:%3", "release": "release:%3"} {
		if a, err := handAct(cliDoc(), cmd, []string{"%3"}, 0, "", ""); err != nil || a.RecID != want || a.Text != "" {
			t.Fatalf("%s: %+v %v", cmd, a, err)
		}
	}
	a, err = handAct(cliDoc(), "hold", []string{"api"}, 0, "not priority", "")
	if err != nil || a.RecID != "hold:%3" || a.Text != "not priority" {
		t.Fatalf("%+v %v", a, err)
	}
}

func TestHandRefusals(t *testing.T) {
	for name, c := range map[string]struct {
		cmd   string
		words []string
		pick  int
		why   string
		want  string
	}{
		"no session":  {"send", nil, 0, "", "send needs a session"},
		"no words":    {"send", []string{"api"}, 0, "", "send needs words"},
		"both":        {"send", []string{"api", "x"}, 1, "", "words or --pick, not both"},
		"hold no why": {"hold", []string{"api"}, 0, "", "hold needs its reason"},
		"two named":   {"send", []string{"web", "x"}, 0, "", "web is 2 sessions: %4, %5; name the pane"},
		"unknown":     {"compact", []string{"nope"}, 0, "", "no session named nope"},
		"no question": {"send", []string{"api"}, 1, "", "api asks no question"},
	} {
		if _, err := handAct(cliDoc(), c.cmd, c.words, c.pick, c.why, ""); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", name, err, c.want)
		}
	}
}

func TestSendPickRoutesToPick(t *testing.T) {
	d := cliDoc()
	d.Sessions[0].Ask = &said.Ask{ID: "toolu_1", Questions: []said.Question{{Question: "Which?", Options: []said.Option{{Label: "a"}, {Label: "b"}}}}}
	a, err := handAct(d, "send", []string{"api"}, 2, "", "%9")
	if err != nil || a.RecID != "pick:%3" || a.Confirm != "y" || a.From != "%9" {
		t.Fatalf("%+v %v", a, err)
	}
	var ans said.Answer
	if err := json.Unmarshal([]byte(a.Text), &ans); err != nil || ans.ID != "toolu_1" || len(ans.Choices) != 1 || len(ans.Choices[0].Picks) != 1 || ans.Choices[0].Picks[0] != 2 {
		t.Fatalf("answer %+v %v", ans, err)
	}
	if _, err := handAct(d, "send", []string{"api"}, 3, "", ""); err == nil || !strings.Contains(err.Error(), "offers 2 options, not 3") {
		t.Fatalf("option 3: %v", err)
	}
	d.Sessions[0].Ask.Questions = append(d.Sessions[0].Ask.Questions, said.Question{Question: "And?"})
	if _, err := handAct(d, "send", []string{"api"}, 1, "", ""); err == nil || !strings.Contains(err.Error(), "asks 2 questions") {
		t.Fatalf("two questions: %v", err)
	}
}

func TestPositionalsAcceptFlagsAnywhere(t *testing.T) {
	for _, args := range [][]string{{"api", "--pick", "2"}, {"--pick", "2", "api"}, {"api", "--pick=2"}} {
		fl, o := newFlags()
		words, err := positionals(fl, args)
		if err != nil || len(words) != 1 || words[0] != "api" || *o.pick != 2 {
			t.Fatalf("%q: %q pick %d %v", args, words, *o.pick, err)
		}
	}
	fl, o := newFlags()
	words, err := positionals(fl, []string{"api", "--why", "not priority"})
	if err != nil || len(words) != 1 || *o.why != "not priority" {
		t.Fatalf("%q why %q %v", words, *o.why, err)
	}
}

func TestPositionalsKeepWordsThatLookLikeFlags(t *testing.T) {
	for _, tc := range []struct {
		args, want []string
		pick       int
		state      string
	}{
		{[]string{"api", "-1 is wrong"}, []string{"api", "-1 is wrong"}, 0, ""},
		{[]string{"api", "-", "--x"}, []string{"api", "-", "--x"}, 0, ""},
		{[]string{"api", "--", "--pick", "2"}, []string{"api", "--pick", "2"}, 0, ""},
		{[]string{"--", "api", "--why"}, []string{"api", "--why"}, 0, ""},
		{[]string{"3", "--state", "done", "merged", "--", "--state", "x"}, []string{"3", "merged", "--state", "x"}, 0, "done"},
		{[]string{"api", "--pick", "2", "--", "-2"}, []string{"api", "-2"}, 2, ""},
	} {
		fl, o := newFlags()
		words, err := positionals(fl, tc.args)
		if err != nil || !slices.Equal(words, tc.want) || *o.pick != tc.pick || *o.state != tc.state {
			t.Errorf("%q: %q pick %d state %q %v", tc.args, words, *o.pick, *o.state, err)
		}
	}
	fl, _ := newFlags()
	if _, err := positionals(fl, []string{"api", "--why"}); err == nil {
		t.Fatal("a flag with no value went through")
	}
}

func TestReportResultSaysWhyOnStderr(t *testing.T) {
	var buf bytes.Buffer
	if err := reportResult(&buf, proto.Result{Err: "the stoker rests: a person works"}); err == nil || err.Error() != "the stoker rests: a person works" {
		t.Fatalf("err %v", err)
	}
	if err := reportResult(&buf, proto.Result{Skipped: []string{"tmux send-keys: the pane has text you typed"}}); err == nil {
		t.Fatal("a skipped step is not a success")
	}
	if err := reportResult(&buf, proto.Result{Ran: [][]string{{"tmux", "send-keys"}}}); err != nil || buf.String() != "tmux send-keys\n" {
		t.Fatalf("%q %v", buf.String(), err)
	}
}

func TestDoActWaitsForItsResult(t *testing.T) {
	sock := isolate(t)
	l, err := transport.Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	got := make(chan proto.Act, 1)
	go func() {
		nc, err := l.Accept()
		if err != nil {
			return
		}
		c := transport.NewConn(nc)
		defer c.Close()
		c.Send(proto.KindHello, "", proto.Hello{Version: proto.Version})
		if env, err := c.Recv(); err != nil || env.Kind != proto.KindHello {
			return
		}
		c.Send(proto.KindSnapshot, "", proto.State{})
		env, err := c.Recv()
		if err != nil || env.Kind != proto.KindAct {
			return
		}
		var a proto.Act
		json.Unmarshal(env.Body, &a)
		got <- a
		c.Send(proto.KindSnapshot, "", proto.State{})
		c.Send(proto.KindResult, "other", proto.Result{Err: "not mine"})
		c.Send(proto.KindResult, env.ID, proto.Result{ActID: env.ID, Ran: [][]string{{"tmux"}}})
	}()
	res, err := doAct(sock, proto.Act{RecID: "message:%3", Text: "hi", From: "%9"})
	if err != nil || len(res.Ran) != 1 || res.Err != "" {
		t.Fatalf("%+v %v", res, err)
	}
	if a := <-got; a.RecID != "message:%3" || a.From != "%9" || a.Text != "hi" {
		t.Fatalf("act %+v", a)
	}
}

func TestDoActWithoutAServiceSaysSo(t *testing.T) {
	if _, err := doAct(isolate(t), proto.Act{}); !errors.Is(err, errNotRunning) {
		t.Fatalf("err %v", err)
	}
}

func TestPlanStdinParse(t *testing.T) {
	// asks: and grant:, which older briefs asked for, read and are left
	// out: the goes are the builder's chips.
	in := "1. work: merge #1 on green\n\n2. app:1.2: rebase on main: then push\n  asks: merge\ngrant: merge:#4556\n"
	p, err := readPlan(strings.NewReader(in))
	if err != nil {
		t.Fatal(err)
	}
	want := stoker.PlanText{
		Steps: []stoker.PlanStep{{N: 1, Session: "work", Text: "merge #1 on green"}, {N: 2, Session: "app:1.2", Text: "rebase on main: then push"}},
	}
	if !reflect.DeepEqual(p, want) {
		t.Fatalf("plan %+v, want %+v", p, want)
	}
	for _, bad := range []string{"merge them\n", "1 work: x\n", "x. work: y\n", "1. work\n", "1. : x\n", "1. work: \n", "grant: merge\n", ""} {
		if _, err := readPlan(strings.NewReader(bad)); err == nil {
			t.Errorf("%q reads", bad)
		}
	}
	a, err := stokerCmdAct([]string{"plan"}, "", "", "", strings.NewReader(in), "%9")
	if err != nil {
		t.Fatal(err)
	}
	var got stoker.PlanText
	if a.RecID != "stoker:plan" || a.From != "%9" || json.Unmarshal([]byte(a.Text), &got) != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("act %+v", a)
	}
}

func TestStokerStepAct(t *testing.T) {
	if cmd, host, _, err := parseCommand([]string{"stoker", "step", "1"}); cmd != "stoker" || host != "" || err != nil {
		t.Fatalf("matchblox stoker: cmd %q host %q err %v", cmd, host, err)
	}
	a, err := stokerCmdAct([]string{"step", "2", "green,", "waits"}, "waits", "merge:#4556", "", nil, "%9")
	if err != nil {
		t.Fatal(err)
	}
	var e stoker.Entry
	if a.RecID != "stoker:step" || a.From != "%9" || json.Unmarshal([]byte(a.Text), &e) != nil ||
		e.Step != 2 || e.State != "waits" || e.Needs != "merge:#4556" || e.Text != "green, waits" {
		t.Fatalf("act %+v entry %+v", a, e)
	}
	a, err = stokerCmdAct([]string{"step", "0", "compacted", "web"}, "done", "", "compact on PR", nil, "%9")
	if err != nil || json.Unmarshal([]byte(a.Text), &e) != nil || e.Rule != "compact on PR" {
		t.Fatalf("rule step: %v %+v", err, e)
	}
	for _, words := range [][]string{nil, {"go"}, {"step"}, {"step", "x", "y"}, {"step", "1"}} {
		if _, err := stokerCmdAct(words, "done", "", "", nil, "%9"); err == nil {
			t.Errorf("%q makes an act", words)
		}
	}
	if _, err := stokerCmdAct([]string{"step", "1", "x"}, "", "", "", nil, "%9"); err == nil {
		t.Error("a step with no --state makes an act")
	}
}

// TestStokerQuitAct: matchblox stoker quit ends the stoker agent; the
// person typed it, so it carries their y.
func TestStokerQuitAct(t *testing.T) {
	a, err := stokerCmdAct([]string{"quit"}, "", "", "", nil, "")
	if err != nil || a.RecID != "stoker:quit" || a.Confirm != "y" || a.Text != "" {
		t.Fatalf("act %+v, %v", a, err)
	}
	if _, err := stokerCmdAct([]string{"quit", "now"}, "", "", "", nil, ""); err == nil {
		t.Fatal("quit takes no words")
	}
	if got := stokerDone([]string{"quit"}, "", ""); got != "the stoker agent and its tmux session are ended" {
		t.Fatalf("said %q", got)
	}
}

// TestBriefNamesEveryCommand holds the brief to the binary: every command
// the stoker's hands use is a command, and the brief names each one.
func TestBriefNamesEveryCommand(t *testing.T) {
	b := stoker.Brief(stoker.Order{ID: "o-1", Words: "x"}, "/w/ways.md", stoker.Hands, stoker.Board{})
	hands := slices.Concat(readCommands, handCommands, stokerCommands)
	for _, c := range hands {
		if first := strings.Fields(c)[0]; !slices.Contains(commands, first) {
			t.Errorf("%q is not in commands", first)
		}
		if !strings.Contains(b, "`matchblox "+c) {
			t.Errorf("the brief does not name `matchblox %s`", c)
		}
	}
	for _, c := range commands {
		if !slices.ContainsFunc(hands, func(h string) bool { return strings.Fields(h)[0] == c }) && !slices.Contains([]string{"connect", "serve", "status", "setup", "pair", "devices", "version", "switch"}, c) {
			t.Errorf("%s is a new command the stoker's hands do not list", c)
		}
	}
}

// TestPeekPrintsWhatTheSessionSaidAndAsks: F3, `matchblox peek` reads
// what the console's peek shows, read-only.
func TestPeekPrintsWhatTheSessionSaidAndAsks(t *testing.T) {
	d := cliDoc()
	d.Sessions[0].Said = "one\ntwo\nthree\nIs #4556 ready to merge?"
	d.Sessions[0].Ask = &said.Ask{ID: "t", Questions: []said.Question{{Question: "Merge #4556?", Options: []said.Option{{Label: "yes"}, {Label: "no"}}}}}
	var buf bytes.Buffer
	if err := writePeek(&buf, d, "api", 2, false); err != nil {
		t.Fatal(err)
	}
	want := "api %3 asks: Merge #4556?\n  1. yes\n  2. no\nsaid:\n  three\n  Is #4556 ready to merge?\n"
	if buf.String() != want {
		t.Fatalf("peek printed\n%s\nwant\n%s", buf.String(), want)
	}
	buf.Reset()
	if err := writePeek(&buf, d, "%3", 0, true); err != nil {
		t.Fatal(err)
	}
	var got stoker.Peek
	if err := json.Unmarshal(buf.Bytes(), &got); err != nil || got.Asks != "Merge #4556?" || got.Said != d.Sessions[0].Said || len(got.Questions) != 1 {
		t.Fatalf("peek --json: %+v, %v\n%s", got, err, buf.String())
	}
	buf.Reset()
	if err := writePeek(&buf, d, "web", 20, false); err == nil || !strings.Contains(err.Error(), "name the pane") {
		t.Fatalf("an ambiguous name: %v", err)
	}
	if err := writePeek(&buf, d, "%4", 20, false); err != nil || buf.String() != "web %4 · busy\nsaid: nothing yet\n" {
		t.Fatalf("a session that said nothing: %q, %v", buf.String(), err)
	}
}

// TestDoActTellsWhetherTheActWent: F4, a service that drops the line after
// the act went leaves it unknown; one that drops it before, not sent.
func TestDoActTellsWhetherTheActWent(t *testing.T) {
	for _, readAct := range []bool{false, true} {
		sock := isolate(t)
		l, err := transport.Listen(sock)
		if err != nil {
			t.Fatal(err)
		}
		go func() {
			nc, err := l.Accept()
			if err != nil {
				return
			}
			c := transport.NewConn(nc)
			defer c.Close()
			if !readAct {
				_, _ = c.Recv() // the client's hello, then the line drops
				return
			}
			c.Send(proto.KindHello, "", proto.Hello{Version: proto.Version})
			for {
				env, err := c.Recv()
				if err != nil || env.Kind == proto.KindAct {
					return
				}
			}
		}()
		_, err = doAct(sock, proto.Act{RecID: "message:%3", Text: "hi"})
		l.Close()
		if want := map[bool]error{false: errNotSent, true: errNoResult}[readAct]; !errors.Is(err, want) {
			t.Fatalf("read the act %v: %v, want %v", readAct, err, want)
		}
	}
}

// TestSendSaysWhatHappened: F4, the output names the command and says
// typed, refused with why, not typed, or not known.
func TestSendSaysWhatHappened(t *testing.T) {
	ran := proto.Result{Ran: [][]string{{"tmux", "send-keys", "-t", "%3", "-l", "run the tests"}, {"tmux", "send-keys", "-t", "%3", "Enter"}}}
	for _, c := range []struct {
		res       proto.Result
		err       error
		out, fail string
	}{
		{ran, nil, "send api: typed\ntmux send-keys -t %3 -l run the tests\ntmux send-keys -t %3 Enter\n", ""},
		{proto.Result{Err: "api asks a question: answer it with --pick"}, nil, "", "send api: refused: api asks a question: answer it with --pick"},
		{proto.Result{Skipped: []string{"the pane has text you typed"}}, nil, "", "send api: refused: the pane has text you typed"},
		{proto.Result{}, errNotSent, "", "send api: not typed: the service did not answer"},
		// Ruling AW (M2): the text went and the Enter did not.
		{proto.Result{Ran: ran.Ran[:1], Skipped: []string{"api waits at a permission prompt"}}, nil, "", "send api: typed, not sent (Enter not pressed): api waits at a permission prompt"},
		{proto.Result{Ran: ran.Ran[:1], Err: "exit status 1"}, nil, "", "send api: typed, not sent (Enter not pressed): exit status 1"},
		{proto.Result{}, errNotRunning, "", "send api: not typed: matchblox is not running: matchblox serve"},
		{proto.Result{}, errNoResult, "", "send api: not known whether it was typed: peek api before you send again"},
	} {
		var buf bytes.Buffer
		err := handOutcome(&buf, "send", "api", c.res, c.err)
		if buf.String() != c.out || (err == nil) != (c.fail == "") || err != nil && err.Error() != c.fail {
			t.Errorf("%+v %v: printed %q, err %v", c.res, c.err, buf.String(), err)
		}
	}
	var buf bytes.Buffer
	if err := handOutcome(&buf, "hold", "api", proto.Result{}, errNoResult); err == nil || err.Error() != "hold api: not known whether it ran: peek api before you try again" {
		t.Fatalf("hold: %v", err)
	}
}

// TestTheWordOfAShellIsNotNoAgent: F8, Claude Code's registry says
// "shell" for a session at rest whose background shell still runs. The
// stoker read it as no agent in the pane; the word says what it is.
func TestTheWordOfAShellIsNotNoAgent(t *testing.T) {
	for s, want := range map[*sample.Session]string{
		{Status: "shell"}:             "background",
		{Status: "busy", Busy: true}:  "busy",
		{Status: "idle"}:              "idle",
		{Status: ""}:                  "idle",
		{Status: "agents"}:            "agents",
		{Status: "shell", Asks: true}: "asks",
	} {
		if got := stoker.Word(*s); got != want {
			t.Errorf("status %q: word %q, want %q", s.Status, got, want)
		}
	}
}

// TestStokerCommandsSayWhatTheyDid: F9, a logged step says so in one line,
// as the plan does.
func TestStokerCommandsSayWhatTheyDid(t *testing.T) {
	for _, c := range []struct {
		words       []string
		state, rule string
		want        string
	}{
		{[]string{"plan"}, "", "", "the plan is on the card"},
		{[]string{"step", "2", "green"}, "waits", "", "step 2 logged: waits"},
		{[]string{"step", "0", "compacted api"}, "done", "compact on PR", `step 0 logged: done (rule "compact on PR")`},
	} {
		if got := stokerDone(c.words, c.state, c.rule); got != c.want {
			t.Errorf("%q: %q, want %q", c.words, got, c.want)
		}
	}
}

func TestWhenParse(t *testing.T) {
	for _, c := range []struct {
		words []string
		o     whenOpts
		want  string
	}{
		{[]string{"when", "send", "Billed", "rebase", "on", "main"}, whenOpts{ship: 4573, stage: "merged"},
			`{"ship":4573,"stage":"merged","do":"send","who":"Billed","text":"rebase on main"}`},
		{[]string{"when", "hold", "work"}, whenOpts{rest: "work", why: "not priority"},
			`{"rest":"work","do":"hold","who":"work","text":"not priority"}`},
		{[]string{"when", "compact", "work"}, whenOpts{rest: "work"},
			`{"rest":"work","do":"compact","who":"work"}`},
		{[]string{"when"}, whenOpts{drop: "t2"}, `{"drop":"t2"}`},
	} {
		a, err := whenCmdAct(c.words, c.o, "%9")
		if err != nil {
			t.Fatalf("%q: %v", c.words, err)
		}
		if a.RecID != "stoker:when" || a.From != "%9" || a.Text != c.want {
			t.Errorf("%q: %+v, want %s", c.words, a, c.want)
		}
	}
	for _, c := range []struct {
		words []string
		o     whenOpts
	}{
		{[]string{"when"}, whenOpts{rest: "w"}},                               // no hand
		{[]string{"when", "merge", "w"}, whenOpts{rest: "w"}},                 // no such hand
		{[]string{"when", "send", "w"}, whenOpts{rest: "w"}},                  // no text
		{[]string{"when", "compact", "w"}, whenOpts{}},                        // no event
		{[]string{"when", "compact", "w"}, whenOpts{ship: 3}},                 // no stage
		{[]string{"when", "compact", "w", "x"}, whenOpts{rest: "w"}},          // words after the session
		{[]string{"when", "send", "w", "x"}, whenOpts{rest: "w", drop: "t1"}}, // a drop is alone
	} {
		if _, err := whenCmdAct(c.words, c.o, "%9"); err == nil {
			t.Errorf("%q %+v makes an act", c.words, c.o)
		}
	}
}

func TestWhenListPrintsTheRunsTriggers(t *testing.T) {
	d := cliDoc()
	var b strings.Builder
	if err := writeTriggers(&b, d); err != nil || b.String() != "no trigger waits\n" {
		t.Fatalf("%v %q", err, b.String())
	}
	d.Stoker = &stoker.View{Mode: stoker.Mode{On: true, Triggers: []stoker.Trigger{
		{ID: "t1", Ship: 4573, Stage: "merged", Do: "send", Who: "Billed", Text: "rebase on main"},
		{ID: "t3", Rest: "work", Do: "compact", Who: "work"},
	}}}
	b.Reset()
	if err := writeTriggers(&b, d); err != nil {
		t.Fatal(err)
	}
	if want := "t1  when #4573 merged → send Billed: rebase on main\nt3  when work rests → compact work\n"; b.String() != want {
		t.Fatalf("%q, want %q", b.String(), want)
	}
}

func TestSessionsLineMarksOutsideTmux(t *testing.T) {
	d := cliDoc()
	d.Sessions = []sample.Session{{Name: "loose", Status: "idle"}}
	var text, js bytes.Buffer
	if err := writeSessions(&text, d, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(text.String(), "outside tmux") {
		t.Fatalf("line %q", text.String())
	}
	if err := writeSessions(&js, d, true); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(js.String(), `"target": ""`) {
		t.Fatalf("json %s", js.String())
	}
}
