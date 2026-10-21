// Package hooks reads the events a coding agent sends through its hooks
// (`matchblox hook <event>`), and keeps them in a spool while no service
// listens.
package hooks

import (
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"
	"time"

	"github.com/blox-eng/matchblox/internal/said"
)

// Notification kinds the queue reads. Other kinds pass through as they are.
const (
	KindPermission = "permission"
	KindIdlePrompt = "idle_prompt"
)

// BashTool is Claude Code's shell tool, the one the merge guard reads.
const BashTool = "Bash"

type Event struct {
	Name string `json:"name"` // Notification | Stop | UserPromptSubmit | SessionStart | SessionEnd | PreToolUse | PostToolUse
	// Agent is the agent of an event of another agent's hooks (CursorAgent);
	// "" is Claude Code's.
	Agent string `json:"agent,omitempty"`
	// Turn is the turn of a Cursor event (its generation_id): its Stop and
	// its Reply come in either order.
	Turn      string    `json:"turn,omitempty"`
	Kind      string    `json:"kind,omitempty"`
	SessionID string    `json:"session_id"`
	Cwd       string    `json:"cwd,omitempty"`
	Message   string    `json:"message,omitempty"`
	Asks      bool      `json:"asks,omitempty"` // Stop: the reply ends on a question
	At        time.Time `json:"at"`
	Pane      string    `json:"pane,omitempty"` // $TMUX_PANE
	// Socket is the tmux server the pane is on ($TMUX's socket): pane ids
	// repeat across servers, and the stoker's is matchblox's own.
	Socket string `json:"socket,omitempty"`
	// ToolUseID is the tool call of a PreToolUse or PostToolUse.
	ToolUseID string `json:"tool_use_id,omitempty"`
	// Ask is the question tool's call a PreToolUse announces: its menu
	// waits for the person once the hook returns.
	Ask *said.Ask `json:"ask,omitempty"`
	// Command is the shell line of a PreToolUse of Bash, its values
	// scrubbed; Raw is the line as the shell runs it, which the merge
	// guard reads and which never leaves the hook's process. Guards are
	// what the guard read in Raw; Denied: it refused the call.
	Command string  `json:"command,omitempty"`
	Raw     string  `json:"-"`
	Guards  []Guard `json:"guards,omitempty"`
	Denied  bool    `json:"denied,omitempty"`
}

// Guard is one command of a shell line the stoker's order must give the
// go for: What is merge, release or deploy, N the pull request a merge
// names (0: none or one the guard cannot read), and Bare a `gh pr merge`
// that names no pull request: its branch's own. Package stoker reads them
// (stoker.Guards).
type Guard struct {
	What string `json:"what"`
	N    int    `json:"n,omitempty"`
	Bare bool   `json:"bare,omitempty"`
}

// input is the Claude Code hook JSON, only the fields we read.
type input struct {
	SessionID        string          `json:"session_id"`
	Cwd              string          `json:"cwd"`
	HookEventName    string          `json:"hook_event_name"`
	Message          string          `json:"message"`
	NotificationType string          `json:"notification_type"`
	LastAssistant    string          `json:"last_assistant_message"`
	ToolName         string          `json:"tool_name"`
	ToolUseID        string          `json:"tool_use_id"`
	ToolInput        json.RawMessage `json:"tool_input"`
	// CursorVersion is in every Cursor payload: Cursor can run Claude Code's
	// hooks too.
	CursorVersion string `json:"cursor_version"`
}

var ErrNoSession = errors.New("hooks: the event has no session_id")

// Parse reads one hook event. name is the event the hook was set for; when
// it is empty, the event name comes from the input.
func Parse(stdin io.Reader, name string, env func(string) string) (Event, error) {
	var in input
	if err := json.NewDecoder(io.LimitReader(stdin, 1<<20)).Decode(&in); err != nil {
		return Event{}, err
	}
	if in.CursorVersion != "" {
		return Event{}, ErrCursor
	}
	if in.SessionID == "" {
		return Event{}, ErrNoSession
	}
	if name == "" {
		name = in.HookEventName
	}
	sock, _, _ := strings.Cut(env("TMUX"), ",")
	ev := Event{Name: name, SessionID: in.SessionID, Cwd: in.Cwd, At: time.Now().UTC(), Pane: env("TMUX_PANE"), Socket: sock}
	switch name {
	case "Notification":
		ev.Message = cut(strings.ReplaceAll(said.Clean(in.Message), "\n", " "))
		ev.Kind = notificationKind(in.NotificationType, in.Message)
	case "Stop":
		line, asks := said.Reply(in.LastAssistant)
		ev.Message, ev.Asks = cut(line), asks
	case "PreToolUse", "PostToolUse":
		ev.ToolUseID = in.ToolUseID
		if name == "PreToolUse" && in.ToolName == BashTool {
			var bash struct {
				Command string `json:"command"`
			}
			if json.Unmarshal(in.ToolInput, &bash) == nil {
				ev.Command, ev.Raw = Scrub(bash.Command), bash.Command
			}
		}
		if name == "PreToolUse" && in.ToolName == said.AskTool && in.ToolUseID != "" {
			var ask said.Ask
			if json.Unmarshal(in.ToolInput, &ask) == nil && len(ask.Questions) > 0 {
				ask = ask.Clean()
				ask.ID = in.ToolUseID
				ev.Ask = &ask
			}
		}
	}
	return ev, nil
}

// assignment is a shell word that sets a variable: NAME=value, with the
// value in single quotes, in double quotes, or bare. A bare value stops at
// what could start a command ($, a paren, a backquote), so what follows
// still reads as a command.
var assignment = regexp.MustCompile(`(^|[\s;&|(])([A-Za-z_][A-Za-z0-9_]*)=('[^']*'|"[^"$` + "`" + `]*"|[^\s;&|()$'"` + "`" + `]+)`)

// Scrub hides the values a shell line sets (GH_TOKEN=…): the line goes to
// the spool and over the socket, and such a value is often a secret. A
// double-quoted value that runs a command ($(…), `…`) is kept whole, so
// the merge guard never loses a command it must read.
func Scrub(cmd string) string {
	return assignment.ReplaceAllString(cmd, "${1}${2}=…")
}

// notificationKind reads notification_type, or the message on older agent
// versions that do not send it.
func notificationKind(typ, msg string) string {
	switch typ {
	case "permission_prompt":
		return KindPermission
	case "":
	default:
		return typ
	}
	switch m := strings.ToLower(msg); {
	case strings.Contains(m, "permission"):
		return KindPermission
	case strings.Contains(m, "waiting for your input"):
		return KindIdlePrompt
	}
	return ""
}

func cut(line string) string {
	if r := []rune(line); len(r) > 200 {
		line = string(r[:199]) + "…"
	}
	return line
}

// ProgressPrompt is what the SessionStart hook adds to a new session: the
// console reads the bar from the agent's last reply.
const ProgressPrompt = "matchblox shows your progress. At the end of each reply, write one line: " +
	"Progress [████░░░░] <the step you do now>. Fill the bar for the part of the task that is done."
