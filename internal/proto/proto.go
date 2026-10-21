// Package proto is the wire between the service and a console: one JSON
// envelope on each line, hello first from each side.
package proto

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"io"

	"github.com/blox-eng/matchblox/internal/gitscan"
	"github.com/blox-eng/matchblox/internal/history"
	"github.com/blox-eng/matchblox/internal/holds"
	"github.com/blox-eng/matchblox/internal/idle"
	"github.com/blox-eng/matchblox/internal/ships"
	"github.com/blox-eng/matchblox/internal/state"
	"github.com/blox-eng/matchblox/internal/stoker"
)

// Version changes only when an older peer cannot read the messages.
const Version = 1

// MaxLine bounds one message, so a broken peer cannot grow our memory.
const MaxLine = 16 << 20

var ErrLineTooLong = errors.New("proto: message longer than 16 MiB")

type Kind string

const (
	KindHello    Kind = "hello"
	KindSnapshot Kind = "snapshot"
	KindEvent    Kind = "event"
	KindAct      Kind = "act"
	KindResult   Kind = "result"
	KindStoker   Kind = "stoker"
	KindError    Kind = "error"
	// KindHook is the first and only message of `matchblox hook`: one
	// hooks.Event, sent after the service hello, with no hello back.
	KindHook Kind = "hook"
)

type Envelope struct {
	Kind Kind            `json:"kind"`
	ID   string          `json:"id,omitempty"`
	Body json.RawMessage `json:"body,omitempty"`
}

type Hello struct {
	Version int    `json:"version"`
	Binary  string `json:"binary"`
	Host    string `json:"host"`
	OS      string `json:"os"`
	Arch    string `json:"arch"`
	// Client is what says hello to the service: ClientConsole, or
	// ClientCLI for a matchblox command. Empty: a command.
	Client string `json:"client,omitempty"`
}

// The kinds of client in a hello. Only a console is a person at the keys.
const (
	ClientConsole = "console"
	ClientCLI     = "cli"
)

// State is what the console draws. History is set only in the first
// snapshot to each console; later snapshots leave it nil and the console
// extends it from each sample.
type State struct {
	state.Doc
	History *history.Series `json:"history,omitempty"`
}

type Event struct {
	Type string `json:"type"`
	Text string `json:"text"`
	Pane string `json:"pane,omitempty"`
}

// Act asks the service to run an action it built itself. The console names
// the action; it never sends argv.
type Act struct {
	RecID   string `json:"rec_id"`
	Which   string `json:"which"`             // "primary" | "secondary"
	Confirm string `json:"confirm,omitempty"` // "y" for a destructive action
	// Text is what the person typed for an "answer:<pane>" act.
	Text string `json:"text,omitempty"`
	// From is the tmux pane the act was typed in ($TMUX_PANE of the CLI).
	// Where the service knows the caller's pid, it finds the pane itself
	// and this is ignored.
	From string `json:"from,omitempty"`
	// Console: the act came from a console, a person at the keys. The
	// service sets it from the hello; it never comes over the wire.
	Console bool `json:"-"`
}

type Result struct {
	ActID   string     `json:"act_id"`
	Ran     [][]string `json:"ran,omitempty"`
	Skipped []string   `json:"skipped,omitempty"`
	Err     string     `json:"err,omitempty"`
	// Names answers the names act: what each number of the island is
	// called now.
	Names *Names `json:"names,omitempty"`
}

// Names are the session and account names by their island numbers.
type Names struct {
	Sessions map[int]string `json:"sessions"`
	Accounts map[int]string `json:"accounts"`
}

type Error struct {
	Text string `json:"text"`
}

// Encode writes one message and its newline in a single Write, so two
// goroutines that share a lock never interleave half lines.
func Encode(w io.Writer, kind Kind, id string, body any) error {
	raw, err := json.Marshal(body)
	if err != nil {
		return err
	}
	line, err := json.Marshal(Envelope{Kind: kind, ID: id, Body: raw})
	if err != nil {
		return err
	}
	_, err = w.Write(append(line, '\n'))
	return err
}

// Decode reads one message. A line over MaxLine is an error, not a stall.
func Decode(r *bufio.Reader) (Envelope, error) {
	var line []byte
	for {
		chunk, err := r.ReadSlice('\n')
		if len(line)+len(chunk) > MaxLine {
			return Envelope{}, ErrLineTooLong
		}
		line = append(line, chunk...)
		if err == nil {
			break
		}
		if !errors.Is(err, bufio.ErrBufferFull) {
			if errors.Is(err, io.EOF) && len(bytes.TrimSpace(line)) > 0 {
				return Envelope{}, io.ErrUnexpectedEOF
			}
			return Envelope{}, err
		}
	}
	var env Envelope
	if err := json.Unmarshal(line, &env); err != nil {
		return Envelope{}, err
	}
	return env, nil
}

// StokerReadBack is how long the service reads back the result of a compact.
const StokerReadBack = stoker.ResumeWait

// The console draws git data without importing the scanner, the ships
// without importing their tracker, and the stoker's steps without
// importing the stoker.
type (
	GitReport   = gitscan.Report
	Worktree    = gitscan.Worktree
	PR          = gitscan.PR
	Ships       = ships.Report
	Ship        = ships.Ship
	ShipStage   = ships.Stage
	ShipSession = ships.SessionRef
	StokerView  = stoker.View
	StokerMode  = stoker.Mode
	StokerEntry = stoker.Entry
	StokerCard  = stoker.Card
	StokerMiss  = stoker.Miss
	StokerLapse = stoker.Lapse
	StokerOrder = stoker.Order
	StokerPlan  = stoker.PlanStep
	StokerSaid  = stoker.Said
	// StokerTrigger is a `matchblox stoker when` that has not fired.
	StokerTrigger = stoker.Trigger
	// StokerUnguard is a plan's session no merge guard watches.
	StokerUnguard = stoker.Unguard
	Hold          = holds.Hold
	IdleReason    = idle.Reason
	// Focus is the session a tap on the island or a buzz opens.
	Focus = state.Focus
)

// NewSession is the Text of a "new:session" act: start an agent in a tmux
// session of its own on the builder's server. Branch, when set, first
// adds a worktree for it next to the repository in Where. Name is the
// tmux session's, its window's and the agent's; empty, it is the
// directory's. First is the agent's first message.
type NewSession struct {
	Where  string `json:"where"`
	Branch string `json:"branch,omitempty"`
	Name   string `json:"name,omitempty"`
	Agent  string `json:"agent"`
	First  string `json:"first,omitempty"`
}
