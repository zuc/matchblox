package push

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/blox-eng/matchblox/internal/queue"
	"github.com/blox-eng/matchblox/internal/state"
)

// The rates, from design 0010 §3.6.
const (
	buzzEvery     = 30 * time.Second
	progressEvery = 3 * time.Minute
	usageEvery    = 5 * time.Minute
	usageStep     = 2 // whole percent
	// restartAfter is when an activity is started again: 30 minutes before
	// the 8 hours iOS keeps one.
	restartAfter = 8*time.Hour - 30*time.Minute
	// startRetry is how long a start that failed for another reason than a
	// dead token waits before it is tried again.
	startRetry = time.Minute
)

// RenewStatus is the status when the phone's push-to-start token is dead.
const RenewStatus = "island: open matchblox on the phone to renew"

// Phones are the registrations the host keeps.
type Phones interface {
	List() []Registration
}

// Sender watches the service's docs and sends the buzz and the island's
// updates through the relay, at the rates of design 0010. Each event is sent
// once with a timeout and dropped when it fails; nothing is queued.
type Sender struct {
	// HostLabel is the host's short name the buzz line starts with.
	HostLabel string
	// Timeout is how long one send may take; zero is DefaultTimeout.
	Timeout time.Duration

	relay  Relay
	phones Phones
	n      *Numbers
	hostFP string
	now    func() time.Time

	in     chan state.Doc
	ctx    context.Context
	cancel context.CancelFunc
	once   sync.Once
	done   chan struct{}

	mu     sync.Mutex
	status string // push: <why>, or ""
	renew  bool

	// Only the worker touches what follows.
	buzzSet map[string]bool // the waits the last buzz saw
	buzzAt  time.Time
	last    *ContentState // the last state sent to the island
	pctAt   time.Time
	usageAt time.Time
	started map[string]time.Time // push-to-start token -> last good start
	tried   map[string]time.Time // push-to-start token -> last failed start
	dead    map[string]bool      // push-to-start tokens the relay answered 410
}

// NewSender starts the sender's one worker; Close stops it.
func NewSender(r Relay, p Phones, n *Numbers, hostFP string, now func() time.Time) *Sender {
	ctx, cancel := context.WithCancel(context.Background())
	s := &Sender{relay: r, phones: p, n: n, hostFP: hostFP, now: now,
		in: make(chan state.Doc, 1), ctx: ctx, cancel: cancel, done: make(chan struct{}),
		started: map[string]time.Time{}, tried: map[string]time.Time{}, dead: map[string]bool{}}
	go s.run()
	return s
}

// OnDoc hands a doc to the worker and never blocks: the one slot keeps the
// newest doc, so a slow relay drops docs and never stalls the caller.
func (s *Sender) OnDoc(doc state.Doc) {
	for {
		select {
		case s.in <- doc:
			return
		default:
		}
		select {
		case <-s.in: // drop the older doc
		default:
		}
	}
}

// Close stops the worker and abandons a send in flight.
func (s *Sender) Close() {
	s.once.Do(s.cancel)
	<-s.done
}

// Status is "" or the one line the Machine tab shows: the last failure as
// `push: <why>`, cleared by a success, or else the renewal copy while the
// phone's push-to-start token is dead.
func (s *Sender) Status() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.status != "" {
		return s.status
	}
	if s.renew {
		return RenewStatus
	}
	return ""
}

func (s *Sender) run() {
	defer close(s.done)
	for {
		select {
		case <-s.ctx.Done():
			return
		case d := <-s.in:
			s.handle(d)
		}
	}
}

// handle applies the rules to one doc; the worker calls it.
func (s *Sender) handle(doc state.Doc) {
	now := s.now()
	regs := s.phones.List()
	s.buzz(doc, regs, now)
	s.island(doc, regs, now)
	renew := false
	for _, r := range regs {
		if r.Island != nil && s.dead[r.Island.StartToken] {
			renew = true
		}
	}
	s.mu.Lock()
	s.renew = renew
	s.mu.Unlock()
}

// buzzBody is the APNs alert of a buzz. The alert names nothing; the Notification
// Service Extension (mutable-content) opens buzz.sealed with the notify key and
// shows the line. buzz.host and buzz.n are in clear so a tap opens the session.
//
//	{"aps":{"alert":{"body":"matchblox"},"sound":"default","mutable-content":1},
//	 "buzz":{"host":"f10f7812","n":3,"sealed":"<base64 enc‖ciphertext>"}}
type buzzBody struct {
	APS struct {
		Alert struct {
			Body string `json:"body"`
		} `json:"alert"`
		Sound   string `json:"sound"`
		Mutable int    `json:"mutable-content"`
	} `json:"aps"`
	Buzz Buzz `json:"buzz"`
}

// buzz sends one buzz when a session starts to wait, at most each 30 s; a wait
// held back by the 30 s goes with the next doc after them.
func (s *Sender) buzz(doc state.Doc, regs []Registration, now time.Time) {
	set := map[string]bool{}
	var fresh *queue.Item
	for i, it := range doc.Queue {
		if it.SessionID == "" || !waits(it) {
			continue
		}
		set[it.SessionID] = true
		if !s.buzzSet[it.SessionID] && (fresh == nil || it.Since.After(fresh.Since)) {
			fresh = &doc.Queue[i]
		}
	}
	if fresh == nil {
		s.buzzSet = set // only ends: nothing to buzz about
		return
	}
	if !s.buzzAt.IsZero() && now.Sub(s.buzzAt) < buzzEvery {
		return
	}
	s.buzzAt, s.buzzSet = now, set
	line := BuzzLine(s.HostLabel, *fresh)
	num := s.n.Session(fresh.SessionID)
	for _, r := range regs {
		if !r.Buzz || r.DeviceToken == "" {
			continue
		}
		sealed, err := Seal(r.NotifyKey, []byte(line))
		if err != nil {
			s.fail("notify key")
			continue
		}
		var b buzzBody
		b.APS.Alert.Body, b.APS.Sound, b.APS.Mutable = "matchblox", "default", 1
		b.Buzz = Buzz{Host: Host8(s.hostFP), N: num, Sealed: sealed}
		pl, _ := json.Marshal(b)
		s.send(r, Event{Target: r.DeviceToken, Kind: "device", PushType: "alert", Priority: 10, Payload: pl})
	}
}

// liveBody is the APNs liveactivity body. An update:
//
//	{"aps":{"timestamp":1792594800,"event":"update","content-state":{...}}}
//
// A push-to-start adds the attributes and the channel the new activity
// subscribes to, and an alert that names nothing (iOS wants one on a start):
//
//	{"aps":{"timestamp":...,"event":"start","content-state":{...},
//	 "attributes-type":"IslandAttributes","attributes":{"host":"f10f7812"},
//	 "input-push-channel":"<channel id>","alert":{"title":"matchblox","body":"matchblox"}}}
type liveBody struct {
	APS liveAPS `json:"aps"`
}

type liveAPS struct {
	Timestamp      int64             `json:"timestamp"`
	Event          string            `json:"event"`
	State          ContentState      `json:"content-state"`
	AttributesType string            `json:"attributes-type,omitempty"`
	Attributes     *islandAttributes `json:"attributes,omitempty"`
	Channel        string            `json:"input-push-channel,omitempty"`
	Alert          *startAlert       `json:"alert,omitempty"`
}

type islandAttributes struct {
	Host string `json:"host"`
}

type startAlert struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// island starts the activity of each following phone that has none, or whose
// activity nears its 8 hours, and sends the updates that are due to the
// channel.
func (s *Sender) island(doc state.Doc, regs []Registration, now time.Time) {
	var follow []Registration
	for _, r := range regs {
		if r.Island != nil {
			follow = append(follow, r)
		}
	}
	if len(follow) == 0 {
		return
	}
	cs := Build(doc, s.hostFP, s.n, now)
	prio, due := s.due(cs, now)
	sent := false
	channels := map[string]bool{}
	for _, r := range follow {
		tok, ch := r.Island.StartToken, r.Island.Channel
		if s.needStart(tok, now) {
			sent = true
			b := liveBody{APS: liveAPS{Timestamp: now.Unix(), Event: "start", State: cs,
				AttributesType: "IslandAttributes", Attributes: &islandAttributes{Host: cs.Host},
				Channel: ch, Alert: &startAlert{Title: "matchblox", Body: "matchblox"}}}
			pl, _ := json.Marshal(b)
			err := s.send(r, Event{Target: tok, Kind: "start", PushType: "liveactivity", Priority: 10, Payload: pl})
			var re *RelayError
			switch {
			case err == nil:
				s.started[tok] = now
				delete(s.tried, tok)
				channels[ch] = true // the start carried this state
				continue
			case errors.As(err, &re) && re.Code == http.StatusGone:
				s.dead[tok] = true
				s.clear() // the renewal says it, not push: relay 410
			default:
				s.tried[tok] = now
			}
		}
		if !due || ch == "" || channels[ch] {
			continue
		}
		channels[ch] = true
		sent = true
		pl, _ := json.Marshal(liveBody{APS: liveAPS{Timestamp: now.Unix(), Event: "update", State: cs}})
		s.send(r, Event{Target: ch, Kind: "channel", PushType: "liveactivity", Priority: prio, Payload: pl})
	}
	if !sent {
		return
	}
	switch {
	case s.last == nil:
		s.pctAt, s.usageAt = now, now
	default:
		if pctOf(s.last) != pctOf(&cs) {
			s.pctAt = now
		}
		if usageMoved(s.last.Accounts, cs.Accounts) {
			s.usageAt = now
		}
	}
	s.last = &cs
}

// needStart says whether to push-to-start with tok: never started, or
// started 7 h 30 ago; not a dead token, and not within a minute of a failure.
func (s *Sender) needStart(tok string, now time.Time) bool {
	if tok == "" || s.dead[tok] {
		return false
	}
	if at, ok := s.tried[tok]; ok && now.Sub(at) < startRetry {
		return false
	}
	at, ok := s.started[tok]
	return !ok || now.Sub(at) >= restartAfter
}

// due says whether cs goes to the channel now, and at which priority: a
// change of the top session or its state, a wait more, or the stoker at once;
// progress each 3 minutes; usage each 5 minutes, low.
func (s *Sender) due(cs ContentState, now time.Time) (int, bool) {
	l := s.last
	if l == nil {
		return 10, true
	}
	if (l.Top == nil) != (cs.Top == nil) || cs.Top != nil && (l.Top.N != cs.Top.N || l.Top.State != cs.Top.State) ||
		cs.Wait > l.Wait || cs.Stoker != l.Stoker {
		return 10, true
	}
	if pctOf(l) != pctOf(&cs) && now.Sub(s.pctAt) >= progressEvery {
		return 10, true
	}
	if usageMoved(l.Accounts, cs.Accounts) && now.Sub(s.usageAt) >= usageEvery {
		return 5, true
	}
	return 0, false
}

func pctOf(cs *ContentState) int {
	if cs.Top == nil {
		return -1
	}
	return cs.Top.Pct
}

// usageMoved: an account or a window came or went, a window's whole percent
// moved by 2 or more, a window reset, or an account's forecast flipped.
func usageMoved(old, cur []AccountUse) bool {
	if len(old) != len(cur) {
		return true
	}
	for i := range cur {
		o, c := old[i], cur[i]
		if o.N != c.N || o.Lasts != c.Lasts || len(o.Windows) != len(c.Windows) {
			return true
		}
		for j := range c.Windows {
			ow, cw := o.Windows[j], c.Windows[j]
			if ow.Name != cw.Name || abs(ow.Pct-cw.Pct) >= usageStep {
				return true
			}
			if !ow.Resets.IsZero() && (cw.Resets.IsZero() || cw.Resets.Sub(ow.Resets) > time.Minute) {
				return true // the window reset
			}
		}
	}
	return false
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

// send gives e its grant and sends it with the timeout. A relay that ignores
// the context is abandoned at the timeout, so the worker never stalls.
func (s *Sender) send(r Registration, e Event) error {
	e.Grant = r.Grants[e.Kind+":"+e.Target]
	if e.Grant == "" {
		err := errors.New("no grant")
		s.fail("no grant")
		return err
	}
	t := s.Timeout
	if t <= 0 {
		t = DefaultTimeout
	}
	ctx, cancel := context.WithTimeout(s.ctx, t)
	defer cancel()
	errc := make(chan error, 1)
	go func() { errc <- s.relay.Send(ctx, e) }()
	var err error
	select {
	case err = <-errc:
	case <-ctx.Done():
		err = ctx.Err()
	}
	if err == nil {
		s.clear()
	} else {
		s.fail(why(err))
	}
	return err
}

// why is the short reason of a failed send.
func why(err error) string {
	var re *RelayError
	var ne net.Error
	switch {
	case errors.As(err, &re):
		return re.Error()
	case errors.Is(err, context.DeadlineExceeded), errors.As(err, &ne) && ne.Timeout():
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "stopped"
	default:
		return "relay unreachable"
	}
}

func (s *Sender) fail(why string) {
	s.mu.Lock()
	s.status = "push: " + why
	s.mu.Unlock()
}

func (s *Sender) clear() {
	s.mu.Lock()
	s.status = ""
	s.mu.Unlock()
}
