package hooks

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const cursorChat = "19bece93-9e50-4441-b026-46d1cbf6e177"

func parseCursor(t *testing.T, file string) Event {
	t.Helper()
	f, err := os.Open(filepath.Join("..", "..", "testdata", "hooks", file))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	ev, err := ParseCursor(f, env(map[string]string{"TMUX_PANE": "%4"}))
	if err != nil {
		t.Fatal(err)
	}
	return ev
}

// TestCursorEventsAreTheSixCursorAccepts: Cursor ignores its whole
// hooks.json, silently, when one event name in it is one it does not take
// (notification did that on a live machine). The door writes this list.
func TestCursorEventsAreTheSixCursorAccepts(t *testing.T) {
	want := []string{"sessionStart", "sessionEnd", "beforeSubmitPrompt", "stop", "afterAgentResponse", "postToolUse"}
	if !slices.Equal(CursorEvents, want) {
		t.Errorf("CursorEvents = %q, want %q", CursorEvents, want)
	}
}

func TestParseCursorMapsEachEvent(t *testing.T) {
	for _, c := range []struct {
		file, name, message string
		asks                bool
	}{
		{"cursor-sessionStart.json", "SessionStart", "", false},
		{"cursor-beforeSubmitPrompt.json", "UserPromptSubmit", "", false},
		{"cursor-postToolUse.json", "PostToolUse", "", false},
		// Cursor writes its transcript after the stop hook: the reply comes
		// with afterAgentResponse, right after.
		{"cursor-stop-completed.json", "Stop", "", false},
		{"cursor-afterAgentResponse.json", Reply, "Shall I also push it?", true},
		// Esc mid-turn: the person stopped it, nothing waits.
		{"cursor-stop-aborted.json", "UserPromptSubmit", "", false},
		// Ctrl-C, mid-turn or at rest.
		{"cursor-stop-error.json", "UserPromptSubmit", "", false},
		{"cursor-sessionEnd.json", "SessionEnd", "", false},
	} {
		ev := parseCursor(t, c.file)
		if ev.Name != c.name || ev.Message != c.message || ev.Asks != c.asks {
			t.Errorf("%s: %q %q asks=%v, want %q %q asks=%v", c.file, ev.Name, ev.Message, ev.Asks, c.name, c.message, c.asks)
		}
		if ev.SessionID != cursorChat || ev.Agent != CursorAgent || ev.Pane != "%4" || ev.Cwd != "/work/app" {
			t.Errorf("%s: %+v, want chat %s of %s in %%4, /work/app", c.file, ev, cursorChat, CursorAgent)
		}
	}
	for _, f := range []string{"cursor-stop-completed.json", "cursor-afterAgentResponse.json"} {
		if ev := parseCursor(t, f); ev.Turn != "3600b2ee-f3ad-4db6-95b9-0e4b521b3f74" {
			t.Errorf("%s: turn %q, want its generation_id", f, ev.Turn)
		}
	}
	if ev := parseCursor(t, "cursor-postToolUse.json"); ev.ToolUseID != "dd61bbbb-d9e0-40e9-9460-e42bf6eafaa0" {
		t.Errorf("postToolUse: tool_use_id %q", ev.ToolUseID)
	}
}

func TestParseCursorDropsUnknownEvents(t *testing.T) {
	if _, err := ParseCursor(strings.NewReader(`{"hook_event_name":"afterAgentThought","session_id":"x"}`), env(nil)); err == nil {
		t.Error("afterAgentThought parsed, want an error")
	}
}
