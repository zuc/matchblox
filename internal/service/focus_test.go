package service

import (
	"context"
	"testing"
	"time"

	"github.com/blox-eng/matchblox/internal/proto"
	"github.com/blox-eng/matchblox/internal/push"
	"github.com/blox-eng/matchblox/internal/sample"
	"github.com/blox-eng/matchblox/internal/state"
)

func focusService(t *testing.T) (*Service, *push.Numbers, *time.Time) {
	t.Helper()
	s := newTest(t, time.Hour)
	now := time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)
	s.Now = func() time.Time { return now }
	n, err := push.OpenNumbers(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.PushNumbers = n
	s.mu.Lock()
	s.cur.Sessions = []sample.Session{{Pane: "%1", Name: "api", SessionID: "sess-a"}}
	s.mu.Unlock()
	return s, n, &now
}

func focusOf(s *Service) *state.Focus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.cur.Focus
}

// The focus act names the session of a number and its pane in the doc, so
// every console gets it with the next broadcast.
func TestTheFocusActNamesTheSession(t *testing.T) {
	s, n, now := focusService(t)
	num := n.Session("sess-a")
	if res := s.act(context.Background(), proto.Act{RecID: FocusAct(num), Text: "/dev/ttys004"}); res.Err != "" {
		t.Fatal(res.Err)
	}
	if f := focusOf(s); f == nil || *f != (state.Focus{SessionID: "sess-a", Pane: "%1", Client: "/dev/ttys004", At: *now}) {
		t.Fatalf("focus = %+v", f)
	}
	// A client that is not a tty name moves no client.
	*now = now.Add(time.Second)
	s.act(context.Background(), proto.Act{RecID: FocusAct(num), Text: "/dev/tty;kill-server"})
	if f := focusOf(s); f == nil || f.Client != "" || f.Pane != "%1" {
		t.Fatalf("focus = %+v", f)
	}
}

// A number of a session that ended, or one never given, is gone.
func TestTheFocusOfAnEndedSessionIsGone(t *testing.T) {
	s, n, now := focusService(t)
	ended := n.Session("sess-old")
	for _, num := range []int{ended, 99} {
		*now = now.Add(time.Second)
		if res := s.act(context.Background(), proto.Act{RecID: FocusAct(num)}); res.Err != "" {
			t.Fatal(res.Err)
		}
		f := focusOf(s)
		if f == nil || !f.Gone || f.Pane != "" || !f.At.Equal(*now) {
			t.Fatalf("number %d: focus = %+v", num, f)
		}
	}
}

// Without the numbers the act resolves nothing and sets nothing: no
// console flashes a session as ended that may well run.
func TestTheFocusWithoutNumbersDoesNothing(t *testing.T) {
	s, _, _ := focusService(t)
	s.PushNumbers = nil
	if res := s.act(context.Background(), proto.Act{RecID: FocusAct(1)}); res.Err != "" {
		t.Fatal(res.Err)
	}
	if f := focusOf(s); f != nil {
		t.Fatalf("focus = %+v", f)
	}
}

func TestTheFocusActRefusesABadNumber(t *testing.T) {
	s, _, _ := focusService(t)
	for _, id := range []string{"focus:", "focus:x", "focus:0", "focus:-1", "focus:012", "focus:1 2"} {
		if res := s.act(context.Background(), proto.Act{RecID: id}); res.Err == "" {
			t.Errorf("%q: no error", id)
		}
		if f := focusOf(s); f != nil {
			t.Fatalf("%q: focus = %+v", id, f)
		}
	}
}

// A focus does not stick: a console that opens later than focusFor after
// it does not move.
func TestTheFocusEndsAfterAWhile(t *testing.T) {
	s, n, now := focusService(t)
	s.act(context.Background(), proto.Act{RecID: FocusAct(n.Session("sess-a"))})
	*now = now.Add(focusFor - time.Second)
	s.publish(sample.Snapshot{At: *now}, func(state.Doc) {})
	if focusOf(s) == nil {
		t.Fatal("the focus ended too soon")
	}
	*now = now.Add(2 * time.Second)
	s.publish(sample.Snapshot{At: *now}, func(state.Doc) {})
	if f := focusOf(s); f != nil {
		t.Fatalf("focus = %+v after %v", f, focusFor+time.Second)
	}
}
