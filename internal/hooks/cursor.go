package hooks

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/blox-eng/matchblox/internal/said"
)

// CursorAgent is the agent a Cursor event comes from, the name package
// sample gives Cursor's sessions: the queue joins its events to those only.
const CursorAgent = "cursor-agent"

// CursorEvents are the Cursor hooks the queue reads, the ones the setup door
// writes to ~/.cursor/hooks.json. Cursor ignores the whole file, without a
// word, when one event in it is one it does not take: notification is not.
var CursorEvents = []string{"sessionStart", "sessionEnd", "beforeSubmitPrompt", "stop", "afterAgentResponse", "postToolUse"}

// Reply is a turn's reply, apart from its Stop: Cursor writes the reply
// nowhere before its stop hook runs, and sends it in afterAgentResponse, a
// hook it runs at the same time. It fills the line of its turn's wait.
const Reply = "Reply"

var ErrCursor = errors.New("hooks: a Cursor payload on Claude Code's hook")

// cursorInput is Cursor's hook JSON, only the fields we read.
type cursorInput struct {
	SessionID      string   `json:"session_id"`
	ConversationID string   `json:"conversation_id"`
	GenerationID   string   `json:"generation_id"`
	HookEventName  string   `json:"hook_event_name"`
	Status         string   `json:"status"`
	Text           string   `json:"text"`
	WorkspaceRoots []string `json:"workspace_roots"`
	ToolUseID      string   `json:"tool_use_id"`
}

// cursorNames are Claude Code's names for the Cursor events the queue reads.
var cursorNames = map[string]string{
	"sessionStart":       "SessionStart",
	"sessionEnd":         "SessionEnd",
	"beforeSubmitPrompt": "UserPromptSubmit",
	"stop":               "Stop",
	"afterAgentResponse": Reply,
	"postToolUse":        "PostToolUse",
}

// ParseCursor reads one Cursor hook event, in the queue's names. Only a turn
// that completed is a Stop: one the person stopped (Esc, "aborted") or quit
// (Ctrl-C, "error") waits for nothing, so it clears the wait as a prompt
// does. A Stop has no line: the Reply after it brings it.
func ParseCursor(stdin io.Reader, env func(string) string) (Event, error) {
	var in cursorInput
	if err := json.NewDecoder(io.LimitReader(stdin, 1<<20)).Decode(&in); err != nil {
		return Event{}, err
	}
	if in.SessionID == "" {
		in.SessionID = in.ConversationID
	}
	if in.SessionID == "" {
		return Event{}, ErrNoSession
	}
	name, ok := cursorNames[in.HookEventName]
	if !ok {
		return Event{}, fmt.Errorf("hooks: Cursor's %q is not an event the queue reads", in.HookEventName)
	}
	sock, _, _ := strings.Cut(env("TMUX"), ",")
	ev := Event{Name: name, Agent: CursorAgent, SessionID: in.SessionID, Turn: in.GenerationID, At: time.Now().UTC(), Pane: env("TMUX_PANE"), Socket: sock}
	if len(in.WorkspaceRoots) > 0 {
		ev.Cwd = in.WorkspaceRoots[0]
	}
	switch {
	case name == "PostToolUse":
		ev.ToolUseID = in.ToolUseID
	case name == "Stop" && in.Status != "completed":
		ev.Name = "UserPromptSubmit"
	case name == Reply:
		line, asks := said.Reply(in.Text)
		ev.Message, ev.Asks = cut(line), asks
	}
	return ev, nil
}
