package push

import (
	"context"
	"crypto/ecdh"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/blox-eng/matchblox/internal/limits"
	"github.com/blox-eng/matchblox/internal/queue"
	"github.com/blox-eng/matchblox/internal/sample"
	"github.com/blox-eng/matchblox/internal/state"
	"github.com/blox-eng/matchblox/internal/stoker"
)

// fakeRelay records each event; fail, when set, says how a send ends.
type fakeRelay struct {
	mu     sync.Mutex
	events []Event
	fail   func(Event) error
}

func (r *fakeRelay) Send(_ context.Context, e Event) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, e)
	if r.fail != nil {
		return r.fail(e)
	}
	return nil
}

// take returns the events sent since the last take.
func (r *fakeRelay) take() []Event {
	r.mu.Lock()
	defer r.mu.Unlock()
	ev := r.events
	r.events = nil
	return ev
}

type phones []Registration

func (p phones) List() []Registration { return p }

type clock struct{ t time.Time }

func (c *clock) now() time.Time      { return c.t }
func (c *clock) add(d time.Duration) { c.t = c.t.Add(d) }

// phone is a registration named name: its tokens and channel carry the name,
// and it holds a grant for each.
func phone(t *testing.T, name string, buzz, island bool) (Registration, *ecdh.PrivateKey) {
	t.Helper()
	k, err := ecdh.X25519().GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	r := Registration{Device: name, At: t0, RegisterRequest: RegisterRequest{
		DeviceToken: "dev-" + name, NotifyKey: k.PublicKey().Bytes(), Buzz: buzz,
		Grants: map[string]string{"device:dev-" + name: "g-dev-" + name},
	}}
	if island {
		r.Island = &IslandReg{StartToken: "start-" + name, Channel: "chan-" + name}
		r.Grants["start:start-"+name] = "g-start-" + name
		r.Grants["channel:chan-"+name] = "g-chan-" + name
	}
	return r, k
}

func newSender(t *testing.T, r Relay, c *clock, regs ...Registration) *Sender {
	t.Helper()
	s := NewSender(r, phones(regs), openNumbers(t, t.TempDir()), t.TempDir(), fp, c.now)
	s.HostLabel = "work-1"
	t.Cleanup(s.Close)
	return s
}

func waiting(ids ...string) state.Doc {
	var ss []sample.Session
	var q []queue.Item
	for i, id := range ids {
		ss = append(ss, sess(id, false, t0))
		q = append(q, queue.Item{SessionID: id, Name: "api-" + id, State: queue.StateQuestion,
			Since: t0.Add(time.Duration(i) * time.Second), LastLine: "Run the migration?"})
	}
	return doc(ss, q)
}

func burning(pct int) state.Doc {
	s := sess("b", true, t0)
	s.Progress = &sample.Progress{Pct: pct}
	return doc([]sample.Session{s}, nil)
}

func ofKind(ev []Event, kind string) []Event {
	var out []Event
	for _, e := range ev {
		if e.Kind == kind {
			out = append(out, e)
		}
	}
	return out
}

// aps is the part of a liveactivity payload the tests read.
type aps struct {
	APS struct {
		Timestamp      int64           `json:"timestamp"`
		Event          string          `json:"event"`
		State          ContentState    `json:"content-state"`
		AttributesType string          `json:"attributes-type"`
		Attributes     json.RawMessage `json:"attributes"`
		Channel        string          `json:"input-push-channel"`
		StaleDate      int64           `json:"stale-date"`
	} `json:"aps"`
}

func liveOf(t *testing.T, e Event) aps {
	t.Helper()
	var a aps
	if err := json.Unmarshal(e.Payload, &a); err != nil {
		t.Fatalf("payload %s: %v", e.Payload, err)
	}
	return a
}

func TestABuzzGoesAtMostEach30s(t *testing.T) {
	r := &fakeRelay{}
	c := &clock{t: t0}
	p, key := phone(t, "a", true, false)
	s := newSender(t, r, c, p)

	s.handle(waiting("x"))
	ev := ofKind(r.take(), "device")
	if len(ev) != 1 {
		t.Fatalf("buzz events %+v", ev)
	}
	e := ev[0]
	if e.Target != "dev-a" || e.Grant != "g-dev-a" || e.PushType != "alert" || e.Priority != 10 {
		t.Fatalf("event %+v", e)
	}
	if strings.Contains(string(e.Payload), "api-x") || strings.Contains(string(e.Payload), "migration") {
		t.Fatalf("the buzz carries text in clear: %s", e.Payload)
	}
	var body struct {
		APS struct {
			Alert   json.RawMessage `json:"alert"`
			Mutable int             `json:"mutable-content"`
		} `json:"aps"`
		Buzz Buzz `json:"buzz"`
	}
	if err := json.Unmarshal(e.Payload, &body); err != nil {
		t.Fatal(err)
	}
	if body.APS.Mutable != 1 || len(body.APS.Alert) == 0 || body.Buzz.Host != Host8(fp) || body.Buzz.N != s.n.Session("x") {
		t.Fatalf("body %s", e.Payload)
	}
	plain, err := openWith(t, key.Bytes(), body.Buzz.Sealed)
	if err != nil || string(plain) != `work-1 · api-x waits: "Run the migration?"` {
		t.Fatalf("sealed opens to %q, %v", plain, err)
	}

	c.add(10 * time.Second)
	s.handle(waiting("x", "y"))
	if ev := ofKind(r.take(), "device"); len(ev) != 0 {
		t.Fatalf("a second buzz within 30 s: %+v", ev)
	}
	c.add(20 * time.Second)
	s.handle(waiting("x", "y"))
	ev = ofKind(r.take(), "device")
	if len(ev) != 1 {
		t.Fatalf("the held buzz does not go at 30 s: %+v", ev)
	}
	json.Unmarshal(ev[0].Payload, &body)
	if body.Buzz.N != s.n.Session("y") {
		t.Fatalf("the buzz is not for the new wait: %+v", body.Buzz)
	}
	c.add(time.Minute)
	s.handle(waiting("x", "y"))
	if ev := ofKind(r.take(), "device"); len(ev) != 0 {
		t.Fatalf("a buzz with no change: %+v", ev)
	}
}

func TestANewWaitUpdatesTheIslandAtOnce(t *testing.T) {
	r := &fakeRelay{}
	c := &clock{t: t0}
	p, _ := phone(t, "a", false, true)
	s := newSender(t, r, c, p)

	s.handle(burning(10))
	if ev := ofKind(r.take(), "start"); len(ev) != 1 {
		t.Fatalf("no start: %+v", ev)
	}
	c.add(time.Second)
	d := burning(10)
	d.Sessions = append(d.Sessions, sess("w", false, t0))
	d.Queue = []queue.Item{{SessionID: "w", State: queue.StatePermission, Since: t0}}
	s.handle(d)
	ev := r.take()
	if len(ev) != 1 || ev[0].Kind != "channel" || ev[0].Target != "chan-a" || ev[0].Grant != "g-chan-a" ||
		ev[0].PushType != "liveactivity" || ev[0].Priority != 10 {
		t.Fatalf("events %+v", ev)
	}
	a := liveOf(t, ev[0])
	if a.APS.Event != "update" || a.APS.Timestamp != c.t.Unix() || a.APS.State.Wait != 1 || a.APS.State.Top.State != "waits" {
		t.Fatalf("payload %s", ev[0].Payload)
	}
	// The wait breaks through the Usage face for 60 s: the activity goes stale then.
	if want := t0.Add(time.Minute).Unix(); a.APS.StaleDate != want {
		t.Fatalf("stale-date %d, want %d", a.APS.StaleDate, want)
	}
	c.add(time.Second)
	s.handle(d)
	if ev := r.take(); len(ev) != 0 {
		t.Fatalf("an update with no change: %+v", ev)
	}
}

func TestProgressWaits3Minutes(t *testing.T) {
	r := &fakeRelay{}
	c := &clock{t: t0}
	p, _ := phone(t, "a", false, true)
	s := newSender(t, r, c, p)

	s.handle(burning(10))
	r.take()
	c.add(time.Minute)
	s.handle(burning(20))
	if ev := r.take(); len(ev) != 0 {
		t.Fatalf("progress within 3 minutes: %+v", ev)
	}
	c.add(2 * time.Minute)
	s.handle(burning(20))
	ev := r.take()
	if len(ev) != 1 || ev[0].Priority != 10 || liveOf(t, ev[0]).APS.State.Top.Pct != 20 {
		t.Fatalf("progress at 3 minutes: %+v", ev)
	}
	c.add(time.Minute)
	s.handle(burning(30))
	if ev := r.take(); len(ev) != 0 {
		t.Fatalf("progress 1 minute after the last: %+v", ev)
	}
}

func TestUsageWaitsFor2PercentOr5Minutes(t *testing.T) {
	r := &fakeRelay{}
	c := &clock{t: t0}
	p, _ := phone(t, "a", false, true)
	s := newSender(t, r, c, p)
	use := func(five float64, resets time.Time) state.Doc {
		a := acct("a", five, 10, t0, &limits.Outlook{Lasts: true})
		a.Windows[1].ResetsAt = resets
		return doc(nil, nil, a)
	}
	r5 := t0.Add(2 * time.Hour)

	s.handle(use(40, r5))
	r.take()
	c.add(6 * time.Minute)
	s.handle(use(41, r5))
	if ev := r.take(); len(ev) != 0 {
		t.Fatalf("a 1 %% change goes: %+v", ev)
	}
	s.handle(use(42, r5))
	ev := r.take()
	if len(ev) != 1 || ev[0].Priority != 5 || ev[0].Kind != "channel" || liveOf(t, ev[0]).APS.State.Accounts[0].Windows[0].Pct != 42 {
		t.Fatalf("a 2 %% change: %+v", ev)
	}
	c.add(time.Minute)
	s.handle(use(50, r5))
	if ev := r.take(); len(ev) != 0 {
		t.Fatalf("usage within 5 minutes: %+v", ev)
	}
	c.add(4 * time.Minute)
	s.handle(use(50, r5))
	if ev := r.take(); len(ev) != 1 || ev[0].Priority != 5 {
		t.Fatalf("usage at 5 minutes: %+v", ev)
	}
	// A reset with almost the same percent goes too.
	c.add(5 * time.Minute)
	s.handle(use(49, r5.Add(5*time.Hour)))
	if ev := r.take(); len(ev) != 1 || ev[0].Priority != 5 {
		t.Fatalf("a reset: %+v", ev)
	}
}

func TestTheStokerFlipGoesAtOnce(t *testing.T) {
	r := &fakeRelay{}
	c := &clock{t: t0}
	p, _ := phone(t, "a", false, true)
	s := newSender(t, r, c, p)
	d := doc(nil, nil)
	d.Stoker = &stoker.View{}

	s.handle(d)
	r.take()
	for _, on := range []bool{true, false} {
		c.add(time.Second)
		d.Stoker = &stoker.View{Mode: stoker.Mode{On: on}}
		s.handle(d)
		ev := r.take()
		if len(ev) != 1 || ev[0].Priority != 10 || liveOf(t, ev[0]).APS.State.Stoker != on {
			t.Fatalf("stoker %v: %+v", on, ev)
		}
	}
}

func TestTheIslandStartsAgainBefore8Hours(t *testing.T) {
	r := &fakeRelay{}
	c := &clock{t: t0}
	p, _ := phone(t, "a", false, true)
	s := newSender(t, r, c, p)

	s.handle(burning(10))
	ev := r.take()
	if len(ev) != 1 || ev[0].Kind != "start" || ev[0].Target != "start-a" || ev[0].Grant != "g-start-a" ||
		ev[0].PushType != "liveactivity" || ev[0].Priority != 10 {
		t.Fatalf("start %+v", ev)
	}
	a := liveOf(t, ev[0])
	if a.APS.Event != "start" || a.APS.AttributesType != "IslandAttributes" || a.APS.Channel != "chan-a" ||
		string(a.APS.Attributes) != `{"host":"`+Host8(fp)+`"}` || a.APS.State.Top == nil {
		t.Fatalf("start payload %s", ev[0].Payload)
	}

	c.add(7*time.Hour + 29*time.Minute)
	s.handle(burning(10))
	if ev := r.take(); len(ev) != 0 {
		t.Fatalf("before 7h30: %+v", ev)
	}
	c.add(time.Minute)
	s.handle(burning(10))
	if ev := r.take(); len(ev) != 1 || ev[0].Kind != "start" {
		t.Fatalf("at 7h30 no new start: %+v", ev)
	}
	c.add(time.Minute)
	s.handle(burning(10))
	if ev := r.take(); len(ev) != 0 {
		t.Fatalf("a start after the new start: %+v", ev)
	}
}

func TestOnlyTheFollowingPhoneGetsTheIsland(t *testing.T) {
	r := &fakeRelay{}
	c := &clock{t: t0}
	a, _ := phone(t, "a", true, true)
	b, _ := phone(t, "b", true, false)
	s := newSender(t, r, c, a, b)

	s.handle(waiting("x"))
	ev := r.take()
	if d := ofKind(ev, "device"); len(d) != 2 {
		t.Fatalf("buzz to both: %+v", d)
	}
	for _, e := range ev {
		if e.Kind != "device" && strings.HasSuffix(e.Target, "-b") {
			t.Fatalf("the phone that does not follow gets %+v", e)
		}
	}
	if st := ofKind(ev, "start"); len(st) != 1 || st[0].Target != "start-a" {
		t.Fatalf("start %+v", st)
	}
}

func TestBuzzOffGetsNoBuzz(t *testing.T) {
	r := &fakeRelay{}
	c := &clock{t: t0}
	p, _ := phone(t, "a", false, true)
	s := newSender(t, r, c, p)

	s.handle(waiting("x"))
	if ev := ofKind(r.take(), "device"); len(ev) != 0 {
		t.Fatalf("buzz off buzzes: %+v", ev)
	}
}

func TestAFailedStartAsksToRenew(t *testing.T) {
	r := &fakeRelay{fail: func(e Event) error {
		if e.Kind == "start" {
			return &RelayError{Code: http.StatusGone}
		}
		return nil
	}}
	c := &clock{t: t0}
	p, _ := phone(t, "a", false, true)
	s := newSender(t, r, c, p)

	s.handle(burning(10))
	if got := s.Status(); got != "island: open matchblox on the phone to renew" {
		t.Fatalf("status %q", got)
	}
	r.take()
	c.add(time.Minute)
	s.handle(waiting("x"))
	ev := r.take()
	if len(ev) != 1 || ev[0].Kind != "channel" {
		t.Fatalf("the channel updates do not go on: %+v", ev)
	}
	if got := s.Status(); got != "island: open matchblox on the phone to renew" {
		t.Fatalf("status after a channel update %q", got)
	}
	c.add(time.Hour)
	s.handle(waiting("x"))
	if ev := ofKind(r.take(), "start"); len(ev) != 0 {
		t.Fatalf("a dead token is tried again: %+v", ev)
	}
}

// hangRelay blocks every send until release closes, whatever its context.
type hangRelay struct{ release chan struct{} }

func (h hangRelay) Send(context.Context, Event) error { <-h.release; return nil }

func TestARelayThatHangsDoesNotStallPublish(t *testing.T) {
	h := hangRelay{release: make(chan struct{})}
	defer close(h.release)
	c := &clock{t: t0}
	p, _ := phone(t, "a", true, true)
	s := NewSender(h, phones{p}, openNumbers(t, t.TempDir()), t.TempDir(), fp, c.now)
	s.Timeout = 30 * time.Millisecond
	defer s.Close()

	for i := range 5 {
		start := time.Now()
		s.OnDoc(waiting("x"))
		if d := time.Since(start); d >= 10*time.Millisecond {
			t.Fatalf("OnDoc %d took %v", i, d)
		}
	}
	deadline := time.Now().Add(2 * time.Second)
	for s.Status() == "" && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if got := s.Status(); got != "push: timeout" {
		t.Fatalf("status %q", got)
	}
}

func TestTheRelayPostsTheEventToPushSend(t *testing.T) {
	var got map[string]any
	code := http.StatusOK
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost || req.URL.Path != "/push/send" || req.Header.Get("Content-Type") != "application/json" {
			t.Errorf("%s %s %q", req.Method, req.URL.Path, req.Header.Get("Content-Type"))
		}
		b, _ := io.ReadAll(req.Body)
		got = nil
		if err := json.Unmarshal(b, &got); err != nil {
			t.Errorf("body %s: %v", b, err)
		}
		w.WriteHeader(code)
	}))
	defer srv.Close()
	rl := NewHTTPRelay(srv.URL, time.Second)
	e := Event{Target: "chan-a", Kind: "channel", Grant: "g", PushType: "liveactivity", Priority: 5, Payload: []byte(`{"aps":{"event":"update"}}`)}
	if err := rl.Send(context.Background(), e); err != nil {
		t.Fatal(err)
	}
	if got["target"] != "chan-a" || got["kind"] != "channel" || got["grant"] != "g" || got["push_type"] != "liveactivity" ||
		got["priority"] != float64(5) {
		t.Fatalf("body %+v", got)
	}
	if pl, ok := got["payload"].(map[string]any); !ok || pl["aps"] == nil {
		t.Fatalf("payload is not the APNs body: %+v", got["payload"])
	}
	code = http.StatusGone
	var re *RelayError
	if err := rl.Send(context.Background(), e); err == nil || !errors.As(err, &re) || re.Code != 410 || err.Error() != "relay 410" {
		t.Fatalf("410: %v", err)
	}
}

func TestAFailedStartDoesNotSwallowAHeldChange(t *testing.T) {
	r := &fakeRelay{fail: func(e Event) error {
		if e.Kind == "start" {
			return &RelayError{Code: http.StatusServiceUnavailable}
		}
		return nil
	}}
	c := &clock{t: t0}
	p, _ := phone(t, "a", false, true)
	s := newSender(t, r, c, p)

	s.handle(burning(10)) // the start fails; the first update goes to the channel
	r.take()
	c.add(time.Minute)
	s.handle(burning(20)) // held by the 3 minutes; the start is tried again and fails
	if ev := ofKind(r.take(), "channel"); len(ev) != 0 {
		t.Fatalf("progress within 3 minutes: %+v", ev)
	}
	c.add(2 * time.Minute)
	s.handle(burning(20))
	ev := ofKind(r.take(), "channel")
	if len(ev) != 1 || liveOf(t, ev[0]).APS.State.Top.Pct != 20 {
		t.Fatalf("the held progress is lost: %+v", ev)
	}
}

// livePhones is a list of registrations that can change between docs.
type livePhones struct {
	mu   sync.Mutex
	regs []Registration
}

func (p *livePhones) List() []Registration {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]Registration(nil), p.regs...)
}

func (p *livePhones) add(r Registration) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.regs = append(p.regs, r)
}

func TestAStartOfAnotherPhoneDoesNotSwallowAHeldChange(t *testing.T) {
	r := &fakeRelay{}
	c := &clock{t: t0}
	a, _ := phone(t, "a", false, true)
	b, _ := phone(t, "b", false, true)
	ps := &livePhones{regs: []Registration{a}}
	s := NewSender(r, ps, openNumbers(t, t.TempDir()), t.TempDir(), fp, c.now)
	t.Cleanup(s.Close)

	s.handle(burning(10))
	r.take()
	c.add(time.Minute)
	ps.add(b)
	s.handle(burning(20)) // held for a; b starts with it
	ev := r.take()
	if len(ev) != 1 || ev[0].Kind != "start" || ev[0].Target != "start-b" {
		t.Fatalf("b's start: %+v", ev)
	}
	c.add(2 * time.Minute)
	s.handle(burning(20))
	var toA []Event
	for _, e := range ofKind(r.take(), "channel") {
		if e.Target == "chan-a" {
			toA = append(toA, e)
		}
	}
	if len(toA) != 1 || liveOf(t, toA[0]).APS.State.Top.Pct != 20 {
		t.Fatalf("a never gets the held progress: %+v", toA)
	}
}

// A wait that begins anew leaves the count and the top as they were, and still
// goes at once, so the island can break through for 60 s.
func TestAWaitThatBeginsAnewUpdatesAtOnce(t *testing.T) {
	for name, tc := range map[string]struct{ first, second state.Doc }{
		"another of two waits": {waiting("a", "b"), func() state.Doc {
			d := waiting("a", "b", "c")
			d.Queue = []queue.Item{d.Queue[0], d.Queue[2]}
			d.Queue[1].Since = t0.Add(10 * time.Second)
			return d
		}()},
		"the same session asks again": {waiting("a"), func() state.Doc {
			d := waiting("a")
			d.Queue[0].Since = t0.Add(5 * time.Second)
			return d
		}()},
	} {
		t.Run(name, func(t *testing.T) {
			r := &fakeRelay{}
			c := &clock{t: t0}
			p, _ := phone(t, "a", false, true)
			s := newSender(t, r, c, p)
			s.handle(tc.first)
			r.take()
			c.add(time.Second)
			s.handle(tc.second)
			ev := r.take()
			if len(ev) != 1 || ev[0].Kind != "channel" || ev[0].Priority != 10 {
				t.Fatalf("events %+v", ev)
			}
			a := liveOf(t, ev[0])
			if !a.APS.State.WaitAt.Equal(tc.second.Queue[len(tc.second.Queue)-1].Since) {
				t.Fatalf("payload %s", ev[0].Payload)
			}
		})
	}
}

func TestAWaitThatEndsAloneDoesNotPush(t *testing.T) {
	r := &fakeRelay{}
	c := &clock{t: t0}
	p, _ := phone(t, "a", false, true)
	s := newSender(t, r, c, p)
	s.handle(waiting("a", "b"))
	r.take()
	c.add(time.Second)
	d := waiting("a", "b")
	d.Queue = d.Queue[:1] // b is answered; a remains the top
	s.handle(d)
	if ev := ofKind(r.take(), "channel"); len(ev) != 0 {
		t.Fatalf("pushed: %+v", ev)
	}
}

// A number that cannot be written down is not sent: the buzz and the island
// wait, and the Machine tab says why.
func TestANumberThatCannotBeSavedIsNotSent(t *testing.T) {
	r := &fakeRelay{}
	c := &clock{t: t0}
	p, _ := phone(t, "a", true, true)
	s := newSender(t, r, c, p)
	blocked := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(blocked, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	s.n.dir = filepath.Join(blocked, "sub")

	s.handle(waiting("x"))
	if ev := r.take(); len(ev) != 0 {
		t.Fatalf("sent with an unsaved number: %+v", ev)
	}
	if st := s.Status(); !strings.HasPrefix(st, "push: numbers: ") {
		t.Fatalf("status %q", st)
	}
	s.n.dir = t.TempDir()
	c.add(time.Minute)
	s.handle(waiting("x"))
	if ev := r.take(); len(ofKind(ev, "device")) != 1 || len(ofKind(ev, "start")) != 1 {
		t.Fatalf("after the disk mends: %+v", ev)
	}
	if st := s.Status(); st != "" {
		t.Fatalf("status after the disk mends %q", st)
	}
}

// A restart of the service does not start a second island: the starts are
// kept on disk, and the new sender starts again only near the 8 hours.
func TestARestartDoesNotStartASecondIsland(t *testing.T) {
	dir := t.TempDir()
	r := &fakeRelay{}
	c := &clock{t: t0}
	p, _ := phone(t, "a", false, true)
	s := NewSender(r, phones{p}, openNumbers(t, dir), dir, fp, c.now)
	s.handle(burning(10))
	if ev := ofKind(r.take(), "start"); len(ev) != 1 {
		t.Fatalf("first start %+v", ev)
	}
	s.Close()

	c.add(time.Hour)
	s = NewSender(r, phones{p}, openNumbers(t, dir), dir, fp, c.now)
	t.Cleanup(s.Close)
	s.handle(burning(10))
	if ev := ofKind(r.take(), "start"); len(ev) != 0 {
		t.Fatalf("a second island after a restart: %+v", ev)
	}
	c.add(6*time.Hour + 30*time.Minute)
	s.handle(burning(10))
	if ev := ofKind(r.take(), "start"); len(ev) != 1 {
		t.Fatalf("no new start at 7h30: %+v", ev)
	}
	// A starts file that does not parse is no start: the island starts.
	if err := os.WriteFile(filepath.Join(dir, "starts.json"), []byte("{nope"), 0o600); err != nil {
		t.Fatal(err)
	}
	s2 := NewSender(r, phones{p}, openNumbers(t, dir), dir, fp, c.now)
	t.Cleanup(s2.Close)
	s2.handle(burning(10))
	if ev := ofKind(r.take(), "start"); len(ev) != 1 {
		t.Fatalf("a bad starts file: %+v", ev)
	}
}
