package service

import (
	"strconv"
	"strings"
	"time"

	"github.com/blox-eng/matchblox/internal/proto"
	"github.com/blox-eng/matchblox/internal/remote"
	"github.com/blox-eng/matchblox/internal/state"
)

// focusFor is how long a focus stays in the doc. The phone's console
// connects only after the gate sent the act, so the focus outlives one
// broadcast; a console that opens later than this does not move.
var focusFor = 10 * time.Second

// focusPrefix starts the act that opens a session on the consoles: a tap on
// the island or a buzz runs `matchblox console --open <n>` through the gate.
const focusPrefix = "focus:"

// FocusAct is the act that focuses the session of island number n.
func FocusAct(n int) string { return focusPrefix + strconv.Itoa(n) }

// focus puts the session of the act's number in the doc, with its pane and
// the tmux client to move there (Act.Text, the phone's tty), or
// Gone when the number's session ended or the number was never given. It
// changes nothing else, and the stoker goes on resting.
func (s *Service) focus(a proto.Act) proto.Result {
	v := strings.TrimPrefix(a.RecID, focusPrefix)
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 || strconv.Itoa(n) != v {
		return proto.Result{Err: "the session number does not read: " + v}
	}
	if _, ok := s.sessionAt(a.From); ok {
		return proto.Result{Err: notTheBuilders}
	}
	if s.PushNumbers == nil {
		// No numbers (Task 9 sets them in newService): nothing to resolve.
		// Saying "That session ended." of a session that may well run would
		// be wrong, so the consoles just open where they are.
		return proto.Result{}
	}
	f := &state.Focus{At: s.now()}
	if remote.ClientTTY(a.Text) {
		f.Client = a.Text // the phone's tty, which the gate read from SSH_TTY
	}
	id, known := s.PushNumbers.SessionOf(n)
	s.mu.Lock()
	for _, x := range s.cur.Sessions {
		if known && x.SessionID == id {
			f.SessionID, f.Pane = id, x.Pane
		}
	}
	f.Gone = f.SessionID == ""
	s.cur.Focus = f
	s.mu.Unlock()
	s.broadcast()
	return proto.Result{}
}

// expireFocus takes a focus older than focusFor out of the doc. Hold s.mu.
func (s *Service) expireFocus(now time.Time) {
	if f := s.cur.Focus; f != nil && now.Sub(f.At) >= focusFor {
		s.cur.Focus = nil
	}
}
