package hooks

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/blox-eng/matchblox/internal/said"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func parseFile(t *testing.T, file, name string) Event {
	t.Helper()
	f, err := os.Open(filepath.Join("..", "..", "testdata", "hooks", file))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	ev, err := Parse(f, name, env(map[string]string{"TMUX_PANE": "%4"}))
	if err != nil {
		t.Fatal(err)
	}
	return ev
}

func TestParseNotificationPermission(t *testing.T) {
	ev := parseFile(t, "notification-permission.json", "Notification")
	want := Event{
		Name: "Notification", Kind: KindPermission,
		SessionID: "7f1c2a90-3b4d-4e5f-8a6b-1c2d3e4f5a6b", Cwd: "/home/dev/src/api",
		Message: "Claude needs your permission to use Bash", Pane: "%4",
	}
	ev.At = time.Time{}
	if !reflect.DeepEqual(ev, want) {
		t.Fatalf("got %+v\nwant %+v", ev, want)
	}
}

func TestParseNotificationKinds(t *testing.T) {
	for file, kind := range map[string]string{
		"notification-idle.json":   KindIdlePrompt,
		"notification-legacy.json": KindPermission, // no notification_type: read the message
	} {
		if got := parseFile(t, file, "Notification").Kind; got != kind {
			t.Errorf("%s: kind %q, want %q", file, got, kind)
		}
	}
}

func TestParseStopKeepsTheLastLine(t *testing.T) {
	ev := parseFile(t, "stop.json", "Stop")
	if ev.Message != "Shall I open the pull request?" {
		t.Fatalf("message %q", ev.Message)
	}
	if ev.At.IsZero() {
		t.Fatal("no time")
	}
}

func TestParseNameFromInput(t *testing.T) {
	ev, err := Parse(strings.NewReader(`{"session_id":"s","hook_event_name":"Stop"}`), "", env(nil))
	if err != nil || ev.Name != "Stop" {
		t.Fatalf("%+v %v", ev, err)
	}
}

func TestParseRejectsNoSession(t *testing.T) {
	if _, err := Parse(strings.NewReader(`{"hook_event_name":"Stop"}`), "Stop", env(nil)); err == nil {
		t.Fatal("no error")
	}
}

func TestReplayThenTruncate(t *testing.T) {
	spool := filepath.Join(t.TempDir(), "state", "spool.jsonl")
	a := Event{Name: "Stop", SessionID: "a", At: time.Unix(10, 0).UTC()}
	b := Event{Name: "Notification", Kind: KindPermission, SessionID: "b", At: time.Unix(20, 0).UTC()}
	for _, ev := range []Event{a, b} {
		if err := Append(spool, ev); err != nil {
			t.Fatal(err)
		}
	}
	var got []Event
	if err := Replay(spool, func(ev Event) { got = append(got, ev) }); err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !reflect.DeepEqual(got[0], a) || !reflect.DeepEqual(got[1], b) {
		t.Fatalf("got %+v", got)
	}
	got = nil
	if err := Replay(spool, func(ev Event) { got = append(got, ev) }); err != nil || len(got) != 0 {
		t.Fatalf("second replay: %+v %v", got, err)
	}
}

func TestReplayNoSpool(t *testing.T) {
	if err := Replay(filepath.Join(t.TempDir(), "none.jsonl"), func(Event) { t.Fatal("event") }); err != nil {
		t.Fatal(err)
	}
}

func TestReplaySkipsABrokenLine(t *testing.T) {
	spool := filepath.Join(t.TempDir(), "spool.jsonl")
	if err := os.WriteFile(spool, []byte("{broken\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Append(spool, Event{Name: "Stop", SessionID: "a"}); err != nil {
		t.Fatal(err)
	}
	n := 0
	if err := Replay(spool, func(Event) { n++ }); err != nil || n != 1 {
		t.Fatalf("n=%d err=%v", n, err)
	}
}

// TestStopThatAsksIsAQuestion: a reply ends with the progress line, so the
// question above it is what the Stop event carries.
func TestStopThatAsksIsAQuestion(t *testing.T) {
	in := `{"session_id":"s1","hook_event_name":"Stop","last_assistant_message":"I fixed the parser.\n\nShall I open the PR?\n\nProgress [██████░░] review"}`
	ev, err := Parse(strings.NewReader(in), "Stop", func(string) string { return "" })
	if err != nil || !ev.Asks || ev.Message != "Shall I open the PR?" {
		t.Fatalf("event %+v, %v", ev, err)
	}
	in = `{"session_id":"s1","hook_event_name":"Stop","last_assistant_message":"All tests pass.\n\nProgress [████████] done"}`
	if ev, _ := Parse(strings.NewReader(in), "Stop", func(string) string { return "" }); ev.Asks || ev.Message != "All tests pass." {
		t.Fatalf("event %+v", ev)
	}
}

// TestNotificationTextIsClean: a notification message is shown in the
// console; a control key in it would drive the person's terminal.
func TestNotificationTextIsClean(t *testing.T) {
	in := `{"session_id":"s1","hook_event_name":"Notification","notification_type":"permission_prompt","message":"Claude needs\u001b]52;c;eA==\u0007 your permission"}`
	ev, err := Parse(strings.NewReader(in), "Notification", func(string) string { return "" })
	if err != nil || ev.Message != "Claude needs your permission" {
		t.Fatalf("message %q, %v", ev.Message, err)
	}
}

// TestParsePreToolUseAsk: the question tool's call, as Claude Code sends it
// before the menu draws.
func TestParsePreToolUseAsk(t *testing.T) {
	ev := parseFile(t, "pre-tool-use-ask.json", "PreToolUse")
	if ev.Name != "PreToolUse" || ev.ToolUseID != "toolu_01VnB3ZqK8fX2c9WmYt4Lh7R" || ev.Pane != "%4" || ev.Cwd != "/home/dev/src/api" {
		t.Fatalf("event %+v", ev)
	}
	want := said.Ask{ID: "toolu_01VnB3ZqK8fX2c9WmYt4Lh7R", Questions: []said.Question{
		{Question: "Which days can you work?", Header: "Days", Multi: true, Options: []said.Option{
			{Label: "Monday", Description: "Available on Mondays."},
			{Label: "Tuesday", Description: "Available on Tuesdays."},
			{Label: "Wednesday", Description: "Available on Wednesdays."}}},
		{Question: "Which shift do you prefer?", Header: "Shift", Options: []said.Option{
			{Label: "Morning", Description: "Early start, finish in the afternoon."},
			{Label: "Evening", Description: "Later start, finish in the evening."}}},
	}}
	if ev.Ask == nil || !reflect.DeepEqual(*ev.Ask, want) {
		t.Fatalf("ask %+v", ev.Ask)
	}
}

func TestParseOtherToolsCarryNoAsk(t *testing.T) {
	if ev := parseFile(t, "pre-tool-use-bash.json", "PreToolUse"); ev.Ask != nil || ev.ToolUseID != "toolu_01Hq7DxW2nP5sKcR8vJm3TfA" || ev.Command != "go test ./..." {
		t.Fatalf("event %+v", ev)
	}
	// The answered call: PostToolUse names it and asks nothing.
	if ev := parseFile(t, "post-tool-use-ask.json", "PostToolUse"); ev.Ask != nil || ev.ToolUseID != "toolu_01VnB3ZqK8fX2c9WmYt4Lh7R" {
		t.Fatalf("event %+v", ev)
	}
	// A call with no questions or no id is not a menu to answer.
	for _, in := range []string{
		`{"session_id":"s","tool_name":"AskUserQuestion","tool_use_id":"t","tool_input":{"questions":[]}}`,
		`{"session_id":"s","tool_name":"AskUserQuestion","tool_input":{"questions":[{"question":"Q?","options":[{"label":"A"}]}]}}`,
		`{"session_id":"s","tool_name":"AskUserQuestion","tool_use_id":"t","tool_input":"broken"}`,
	} {
		if ev, err := Parse(strings.NewReader(in), "PreToolUse", env(nil)); err != nil || ev.Ask != nil {
			t.Errorf("%s: %+v %v", in, ev.Ask, err)
		}
	}
}

// TestAskTextIsClean: the console draws the question and its options.
func TestAskTextIsClean(t *testing.T) {
	in := `{"session_id":"s","tool_name":"AskUserQuestion","tool_use_id":"t","tool_input":{"questions":[{"question":"Which\u001b[2J one?","options":[{"label":"A\u0007","description":"d\u001b]52;c;eA==\u0007"}]}]}}`
	ev, err := Parse(strings.NewReader(in), "PreToolUse", env(nil))
	if err != nil || ev.Ask == nil {
		t.Fatalf("%+v %v", ev, err)
	}
	q := ev.Ask.Questions[0]
	if q.Question != "Which one?" || q.Options[0].Label != "A" || q.Options[0].Description != "d" {
		t.Fatalf("question %+v", q)
	}
}

// TestAskSurvivesTheSpool: a hook that missed the service keeps its call.
func TestAskSurvivesTheSpool(t *testing.T) {
	spool := filepath.Join(t.TempDir(), "spool.jsonl")
	ev := parseFile(t, "pre-tool-use-ask.json", "PreToolUse")
	if err := Append(spool, ev); err != nil {
		t.Fatal(err)
	}
	var got Event
	if err := Replay(spool, func(e Event) { got = e }); err != nil {
		t.Fatal(err)
	}
	if got.Ask == nil || !reflect.DeepEqual(*got.Ask, *ev.Ask) || got.ToolUseID != ev.ToolUseID {
		t.Fatalf("replayed %+v", got)
	}
}

func TestScrubHidesTheValuesALineSets(t *testing.T) {
	for in, want := range map[string]string{
		"GH_TOKEN=abc123 gh pr merge 5":                         "GH_TOKEN=… gh pr merge 5",
		"export API_KEY='a b c' && gh pr merge 5":               "export API_KEY=… && gh pr merge 5",
		`FOO="x y" BAR=1 gh pr merge 5`:                         "FOO=… BAR=… gh pr merge 5",
		"cd api;TOKEN=xyz make release":                         "cd api;TOKEN=… make release",
		`X="$(gh pr merge 5)"`:                                  `X="$(gh pr merge 5)"`,
		"X=$(gh pr merge 5)":                                    "X=$(gh pr merge 5)",
		"gh pr merge 5 --body=done":                             "gh pr merge 5 --body=done",
		"go test ./...":                                         "go test ./...",
		"gh api -f merge_method=squash repos/o/r/pulls/5/merge": "gh api -f merge_method=… repos/o/r/pulls/5/merge",
	} {
		if got := Scrub(in); got != want {
			t.Errorf("Scrub(%q) = %q, want %q", in, got, want)
		}
	}
	b := []byte(`{"session_id":"s1","hook_event_name":"PreToolUse","tool_name":"Bash","tool_input":{"command":"GH_TOKEN=abc123 gh pr merge 5"}}`)
	ev, err := Parse(bytes.NewReader(b), "PreToolUse", func(string) string { return "" })
	if err != nil || ev.Command != "GH_TOKEN=… gh pr merge 5" {
		t.Fatalf("parsed %q %v", ev.Command, err)
	}
	// The guard reads the line as the shell runs it; the raw line never
	// leaves the process.
	if ev.Raw != "GH_TOKEN=abc123 gh pr merge 5" {
		t.Fatalf("raw %q", ev.Raw)
	}
	out, err := json.Marshal(ev)
	if err != nil || bytes.Contains(out, []byte("abc123")) {
		t.Fatalf("the raw line went out: %s %v", out, err)
	}
}

// TestParseDropsACursorPayload: Cursor can run Claude Code's hooks too, with
// its own payloads. Its own hook (ParseCursor) is the one source of its
// events.
func TestParseDropsACursorPayload(t *testing.T) {
	f, err := os.Open(filepath.Join("..", "..", "testdata", "hooks", "cursor-stop-completed.json"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := Parse(f, "Stop", env(nil)); !errors.Is(err, ErrCursor) {
		t.Errorf("err = %v, want ErrCursor", err)
	}
}
