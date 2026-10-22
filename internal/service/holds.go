package service

import (
	"context"

	"github.com/blox-eng/matchblox/internal/holds"
	"github.com/blox-eng/matchblox/internal/proto"
	"github.com/blox-eng/matchblox/internal/sample"
	"github.com/blox-eng/matchblox/internal/stoker"
)

// Text the stoker types to a session it holds and to one it lets go.
const (
	holdMessage    = "finish this step, then stop and wait"
	releaseMessage = "carry on"
)

func (s *Service) heldOf(sessionID string) (holds.Hold, bool) {
	if s.Holds == nil || sessionID == "" {
		return holds.Hold{}, false
	}
	return s.Holds.Of(sessionID)
}

// heldIn is the hold on x, the session in pane, or the hold of a switch
// that waits for the new agent's session id there.
func (s *Service) heldIn(pane string, x sample.Session) (holds.Hold, bool) {
	if h, ok := s.heldOf(x.SessionID); ok {
		return h, true
	}
	if s.Holds == nil {
		return holds.Hold{}, false
	}
	return s.Holds.Pending(pane)
}

// heldSessions is the session ids the stoker leaves alone, a switched
// seat's new session among them before its hold moves to it. Nil: none.
func (s *Service) heldSessions() map[string]bool {
	if s.Holds == nil {
		return nil
	}
	held := s.Holds.Held()
	for _, h := range s.Holds.List() {
		if !h.Rebind {
			continue
		}
		if x, ok := s.sessionAt(h.Pane); ok && x.SessionID != "" {
			held[x.SessionID] = true
		}
	}
	return held
}

// holdAt is the hold on the session in pane, or on the one that was there
// when it was held and is gone from the snapshot.
func (s *Service) holdAt(pane string) (holds.Hold, bool) {
	if x, ok := s.sessionAt(pane); ok {
		return s.heldIn(pane, x)
	}
	if s.Holds != nil {
		for _, h := range s.Holds.List() {
			if h.Pane == pane {
				return h, true
			}
		}
	}
	return holds.Hold{}, false
}

// park toggles the builder's hold on the session in pane: nothing types
// into a parked session, and the stoker leaves it alone.
func (s *Service) park(pane string) proto.Result {
	if s.Holds == nil {
		return proto.Result{Err: "parking is off"}
	}
	if h, ok := s.holdAt(pane); ok {
		if h.By == holds.ByStoker {
			return proto.Result{Err: h.Name + " is held by the stoker: z lets it go"}
		}
		if _, _, err := s.Holds.Release(h.SessionID); err != nil {
			return proto.Result{Err: err.Error()}
		}
		s.republish()
		return proto.Result{}
	}
	x, ok := s.sessionAt(pane)
	switch {
	case !ok:
		return proto.Result{Err: "no agent session in " + pane + " any more"}
	case x.SessionID == "":
		return proto.Result{Err: x.Name + " has no session id yet: park it after its first turn"}
	}
	if err := s.Holds.Set(holds.Hold{SessionID: x.SessionID, Pane: pane, Name: x.Name, By: holds.ByYou, At: s.now()}); err != nil {
		return proto.Result{Err: err.Error()}
	}
	s.republish()
	return proto.Result{}
}

// hold is the stoker's: it queues one message to the session, then holds
// it, with a.Text as why. The message goes first, so the hold's own
// refusal does not stop it. It never sends Esc or Ctrl+C.
func (s *Service) hold(ctx context.Context, pane string, a proto.Act) proto.Result {
	why, err := stoker.Why(a.Text)
	if err != nil {
		return proto.Result{Err: err.Error()}
	}
	if s.Holds == nil {
		return proto.Result{Err: "holds are off"}
	}
	x, ok := s.sessionAt(pane)
	switch {
	case !ok:
		return proto.Result{Err: "no agent session in " + pane + " any more"}
	case x.SessionID == "":
		return proto.Result{Err: x.Name + " has no session id yet"}
	}
	msg := a
	msg.Text = holdMessage
	res := s.message(ctx, pane, msg)
	if res.Err != "" || len(res.Ran) == 0 {
		return res
	}
	if err := s.Holds.Set(holds.Hold{SessionID: x.SessionID, Pane: pane, Name: x.Name, By: holds.ByStoker, Why: why, At: s.now()}); err != nil {
		res.Err = err.Error()
	}
	s.republish()
	return res
}

// release lets a session go. The hold comes off first, or the held
// refusal would stop the stoker's "carry on".
func (s *Service) release(ctx context.Context, pane string, a proto.Act) proto.Result {
	h, ok := s.holdAt(pane)
	if !ok {
		return proto.Result{Err: "nothing holds " + pane}
	}
	if h.By != holds.ByStoker && !a.Console {
		return proto.Result{Err: "parked by you: only z lets it go"}
	}
	if _, _, err := s.Holds.Release(h.SessionID); err != nil {
		return proto.Result{Err: err.Error()}
	}
	s.republish()
	if h.By != holds.ByStoker {
		return proto.Result{}
	}
	msg := a
	msg.Text = releaseMessage
	return s.message(ctx, pane, msg)
}

// republish shows the holds as they are now, without waiting for a sample.
func (s *Service) republish() {
	s.mu.Lock()
	s.cur.Holds = s.Holds.List()
	s.mu.Unlock()
	s.broadcast()
}
