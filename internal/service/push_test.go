package service

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/blox-eng/matchblox/internal/push"
	"github.com/blox-eng/matchblox/internal/sample"
	"github.com/blox-eng/matchblox/internal/state"
)

const pushFP = "SHA256:uNiVztksCsDhcc0u9e8BujQXVUpKZIDTMczCvj3tD2s"

// pushRelay records each event; fail, when set, says how a send ends.
type pushRelay struct {
	mu     sync.Mutex
	events []push.Event
	fail   func(push.Event) error
}

func (r *pushRelay) Send(_ context.Context, e push.Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
	if r.fail != nil {
		return r.fail(e)
	}
	return nil
}

func (r *pushRelay) all() []push.Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]push.Event(nil), r.events...)
}

// waitFor waits until the relay has seen n events.
func (r *pushRelay) waitFor(t *testing.T, n int) []push.Event {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if ev := r.all(); len(ev) >= n {
			return ev
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("the relay saw %d events, want %d: %+v", len(r.all()), n, r.all())
	return nil
}

type pushPhones []push.Registration

func (p pushPhones) List() []push.Registration { return p }

// pushClock is a clock the test moves while the sender's worker reads it.
type pushClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *pushClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *pushClock) add(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

// islandPhone follows this host's island; it has no buzz.
func islandPhone() push.Registration {
	return push.Registration{Device: "0123456789ab", RegisterRequest: push.RegisterRequest{
		DeviceToken: "dev-a",
		Island:      &push.IslandReg{StartToken: "start-a", Channel: "chan-a"},
		Grants:      map[string]string{"device:dev-a": "g-dev", "start:start-a": "g-start", "channel:chan-a": "g-chan"},
	}}
}

func pushService(t *testing.T, r push.Relay, regs ...push.Registration) (*Service, *pushClock) {
	t.Helper()
	s := newTest(t, time.Hour)
	c := &pushClock{t: time.Date(2026, 10, 21, 9, 0, 0, 0, time.UTC)}
	s.Now = c.now
	n, err := push.OpenNumbers(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	s.PushNumbers = n
	s.Push = push.NewSender(r, pushPhones(regs), n, pushFP, c.now)
	s.Push.HostLabel = "work-1"
	t.Cleanup(s.Push.Close)
	return s, c
}

func burns(pct int, at time.Time) sample.Snapshot {
	return sample.Snapshot{At: at, Sessions: []sample.Session{{PID: 7, Start: 1, Pane: "%1", Name: "api",
		SessionID: "sess-b", Busy: true, Status: "busy", Progress: &sample.Progress{Pct: pct}}}}
}

type pushLive struct {
	APS struct {
		Event string            `json:"event"`
		State push.ContentState `json:"content-state"`
	} `json:"aps"`
}

func liveState(t *testing.T, e push.Event) push.ContentState {
	t.Helper()
	var b pushLive
	if err := json.Unmarshal(e.Payload, &b); err != nil {
		t.Fatalf("payload %s: %v", e.Payload, err)
	}
	return b.APS.State
}

// Each published doc goes to the sender, which starts the island with the
// host's tag; a failed send shows in the next doc as push: <why>.
func TestPublishHandsTheDocToTheSender(t *testing.T) {
	r := &pushRelay{fail: func(push.Event) error { return &push.RelayError{Code: http.StatusServiceUnavailable} }}
	s, c := pushService(t, r, islandPhone())
	var docs []state.Doc
	rec := func(d state.Doc) { docs = append(docs, d) }

	s.publish(burns(40, c.now()), rec)
	ev := r.waitFor(t, 1)
	if ev[0].Kind != "start" || ev[0].Target != "start-a" || ev[0].Grant != "g-start" {
		t.Fatalf("first event %+v", ev[0])
	}
	if cs := liveState(t, ev[0]); cs.Host != push.Host8(pushFP) || cs.Top == nil || cs.Top.State != "burns" || cs.Top.Pct != 40 {
		t.Fatalf("content state %+v", cs)
	}
	deadline := time.Now().Add(5 * time.Second)
	for s.Push.Status() == "" && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	s.publish(burns(40, c.now()), rec)
	if len(docs) != 2 || docs[0].Push != "" || docs[1].Push != "push: relay 503" {
		t.Fatalf("doc push lines %q", []string{docs[0].Push, docs[len(docs)-1].Push})
	}
	if st := s.State(); st.Push != "push: relay 503" {
		t.Fatalf("state push %q", st.Push)
	}
}

// A service without a sender ([push] off) publishes as before: no push line.
func TestPushOffSendsNothing(t *testing.T) {
	s := newTest(t, time.Hour)
	var got state.Doc
	s.publish(burns(40, time.Now()), func(d state.Doc) { got = d })
	if s.Push != nil || got.Push != "" {
		t.Fatalf("push %v, line %q", s.Push, got.Push)
	}
}

// The sender holds a progress change for 3 minutes; the next tick, with
// nothing else changed, sends it: publish runs on every tick.
func TestAHeldProgressGoesOutOnTheNextTick(t *testing.T) {
	r := &pushRelay{}
	s, c := pushService(t, r, islandPhone())
	rec := func(state.Doc) {}

	s.publish(burns(10, c.now()), rec)
	r.waitFor(t, 1) // the start
	c.add(time.Minute)
	s.publish(burns(20, c.now()), rec)
	time.Sleep(50 * time.Millisecond)
	if ev := r.all(); len(ev) != 1 {
		t.Fatalf("progress within 3 minutes: %+v", ev[1:])
	}
	c.add(2 * time.Minute)
	s.publish(burns(20, c.now()), rec) // the tick: the same sample
	ev := r.waitFor(t, 2)
	if ev[1].Kind != "channel" || ev[1].Target != "chan-a" || liveState(t, ev[1]).Top.Pct != 20 {
		t.Fatalf("the held progress: %+v", ev[1])
	}
}

// Push that could not start (numbers.json unreadable) says why on the
// Machine tab, though there is no sender.
func TestPushThatCannotStartSaysWhy(t *testing.T) {
	s := newTest(t, time.Hour)
	s.PushOff = "push: numbers: numbers.json: bad"
	var got state.Doc
	s.publish(burns(40, time.Now()), func(d state.Doc) { got = d })
	if got.Push != "push: numbers: numbers.json: bad" {
		t.Fatalf("line %q", got.Push)
	}
}
