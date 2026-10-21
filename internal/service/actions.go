package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/blox-eng/matchblox/internal/advice"
	"github.com/blox-eng/matchblox/internal/doors"
	"github.com/blox-eng/matchblox/internal/holds"
	"github.com/blox-eng/matchblox/internal/panes"
	"github.com/blox-eng/matchblox/internal/proto"
	"github.com/blox-eng/matchblox/internal/queue"
	"github.com/blox-eng/matchblox/internal/said"
	"github.com/blox-eng/matchblox/internal/sample"
	"github.com/blox-eng/matchblox/internal/setup"
	"github.com/blox-eng/matchblox/internal/ships"
)

// doneFor is how long an act that ran answers "already done" to the same
// request: long enough for the next git scan to drop the rec.
var doneFor = 10 * time.Minute

// messageDoneFor is the same for a message to a session that does not
// wait: only a double send, as the same words may well go again later.
const messageDoneFor = 5 * time.Second

// acts lets one act on each action run at a time and remembers which ran,
// so two consoles that confirm the same step run it once. A second confirm
// waits for the first, then answers "already done".
type acts struct {
	mu       sync.Mutex
	inflight map[string]chan struct{}
	done     map[string]time.Time
}

// begin waits until no act on key runs. It reports false when key ran
// within the last within; otherwise the caller owns key until end.
func (a *acts) begin(key string, within time.Duration) bool {
	for {
		a.mu.Lock()
		if a.inflight == nil {
			a.inflight, a.done = map[string]chan struct{}{}, map[string]time.Time{}
		}
		if at, ok := a.done[key]; ok && time.Since(at) < within {
			a.mu.Unlock()
			return false
		}
		wait, busy := a.inflight[key]
		if !busy {
			a.inflight[key] = make(chan struct{})
			a.mu.Unlock()
			return true
		}
		a.mu.Unlock()
		<-wait
	}
}

func (a *acts) end(key string, ran bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	for k, at := range a.done {
		if now.Sub(at) >= doneFor {
			delete(a.done, k)
		}
	}
	if ran {
		a.done[key] = now
	}
	close(a.inflight[key])
	delete(a.inflight, key)
}

// notTheBuilders refuses a pane act typed in another agent's pane. The
// hand rules are user-wide, so any session may call the CLI unprompted.
const notTheBuilders = "only the stoker and the sessions it drives act from an agent's pane: this one is neither"

// NamesAct asks what each island number is called now. A phone that
// registers through the gate gets the answer; it changes nothing.
const NamesAct = "push:names"

func (s *Service) act(ctx context.Context, a proto.Act) proto.Result {
	if a.RecID == NamesAct {
		return s.names() // it reads; the stoker goes on resting
	}
	// A session the stoker drove speaks for the stoker: what it sends
	// counts and drives, and it gives no go.
	agent := s.agentsPane(a)
	if !agent && !a.Console && (paneOf(a.RecID) != "" || a.RecID == NewSessionID) {
		if _, ok := s.sessionAt(a.From); ok {
			return proto.Result{Err: notTheBuilders}
		}
	}
	if !agent && !strings.HasPrefix(a.RecID, "stoker:") {
		s.touchStoker(s.now()) // the builder acts: a stoker that rests goes on resting
	}
	if agent {
		if err := s.orderable(a); err != nil {
			return proto.Result{Err: err.Error()}
		}
	} else if yours(a.RecID) && !a.Console {
		return proto.Result{Err: "only you do this, at the console"}
	}
	res := s.dispatch(ctx, a)
	if agent {
		s.recordOrder(a, res)
	} else if pane := typedPane(a.RecID); a.Console && pane != "" && res.Err == "" && len(res.Ran) > 0 {
		s.undrive(pane) // the pane's last input is the person's at a console
	}
	return res
}

// yours are the acts only the person at a console sends: the launch
// sheet's and the stoker tab's verbs, and z. The stoker's own plan and
// steps come from its CLI.
func yours(recID string) bool {
	if verb, ok := strings.CutPrefix(recID, "stoker:"); ok {
		// matchblox stoker quit is the builder's in a shell too.
		return verb != "plan" && verb != "step" && verb != "when" && verb != "quit"
	}
	return strings.HasPrefix(recID, "park:")
}

func (s *Service) dispatch(ctx context.Context, a proto.Act) proto.Result {
	switch a.RecID {
	case "rescan:git":
		s.rescanGit()
		return proto.Result{}
	case "service:replace":
		s.replace()
		return proto.Result{}
	}
	if verb, ok := strings.CutPrefix(a.RecID, "stoker:"); ok {
		switch verb {
		case "order":
			return s.orderAct(ctx, a)
		case "plan", "step", "when":
			return s.planAct(verb, a)
		case "grant":
			return s.grantAct(a)
		case "start":
			return s.startAct(ctx)
		case "quit":
			return s.quitAct(ctx, a)
		}
		return s.stokerAct(verb, s.now())
	}
	if pane, ok := strings.CutPrefix(a.RecID, "park:"); ok {
		return s.park(pane)
	}
	if pane, ok := strings.CutPrefix(a.RecID, "kill:"); ok {
		return s.kill(ctx, pane, a)
	}
	if pane, ok := strings.CutPrefix(a.RecID, "rename:"); ok {
		return s.rename(ctx, pane, a)
	}
	if a.RecID == NewSessionID {
		return s.newSession(ctx, a)
	}
	key := a.RecID + "\x00" + a.Which
	id, door := strings.CutPrefix(a.RecID, "door:")
	if door {
		key += "\x00" + a.Text
	}
	askPane, pick := strings.CutPrefix(a.RecID, "pick:")
	if pick {
		key += "\x00" + a.Text
	}
	pane, answer := strings.CutPrefix(a.RecID, "answer:")
	if answer {
		// The same text to a new question is a new answer: the key holds
		// the time the session started to wait.
		it, _ := s.queued(pane)
		key += "\x00" + a.Text + "\x00" + it.Since.String()
	}
	msgPane, message := strings.CutPrefix(a.RecID, "message:")
	within := doneFor
	if message {
		key += "\x00" + a.Text
		within = messageDoneFor
	}
	if !s.acts.begin(key, within) {
		return proto.Result{Skipped: []string{"already done"}}
	}
	ran := false
	defer func() { s.acts.end(key, ran) }()
	if door {
		res := s.door(id, a)
		ran = res.Err == ""
		return res
	}
	if pick {
		res := s.pick(ctx, askPane, a)
		ran = len(res.Ran) > 0
		return res
	}
	if answer {
		if err := panes.CheckAnswer(a.Text); err != nil {
			return proto.Result{Err: err.Error()}
		}
		switch it, ok := s.queued(pane); {
		case !ok:
			return proto.Result{Err: "the session in " + pane + " no longer waits"}
		case it.State == queue.StatePermission || s.permits(pane):
			return proto.Result{Err: "a permission prompt is answered in its pane: Enter goes there"}
		}
		release := s.holdPane(pane)
		res := s.Actions.Do(ctx, answerAction(pane, a.Text), a.Confirm)
		release()
		ran = len(res.Ran) > 0
		return res
	}
	if message {
		res := s.message(ctx, msgPane, a)
		ran = len(res.Ran) > 0
		return res
	}
	if cPane, ok := strings.CutPrefix(a.RecID, "compact:"); ok {
		res := s.compact(ctx, cPane, a)
		ran = len(res.Ran) > 0
		return res
	}
	if holdPane, ok := strings.CutPrefix(a.RecID, "hold:"); ok {
		res := s.hold(ctx, holdPane, a)
		ran = len(res.Ran) > 0
		return res
	}
	if relPane, ok := strings.CutPrefix(a.RecID, "release:"); ok {
		res := s.release(ctx, relPane, a)
		ran = len(res.Ran) > 0
		return res
	}
	action, ok := s.resolve(a.RecID, a.Which)
	if !ok {
		return proto.Result{Err: "unknown action"}
	}
	if action.Nav {
		return proto.Result{Err: "runs in the console"}
	}
	res := s.Actions.Do(ctx, action, a.Confirm)
	ran = len(res.Ran) > 0
	if ran && len(action.Steps) > 0 && action.Steps[0][0] == "git" {
		s.rescanGit() // a removed worktree leaves the list now, not at the next scan
	}
	return res
}

// resolve finds the action in the service's own state: a rec by its id, or
// the x action of a panel row ("orphan:<pid>", "worktree:<path>",
// "ship:<repo>#<n>"). A session's x is kill:<pane>.
func (s *Service) resolve(id, which string) (advice.Action, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range s.cur.Recommendations {
		if r.ID != id {
			continue
		}
		a := r.Primary
		if which == "secondary" {
			a = r.Second
		}
		if a == nil || len(a.Steps) == 0 {
			return advice.Action{}, false
		}
		return *a, true
	}
	if which != "secondary" {
		return advice.Action{}, false
	}
	if v, ok := strings.CutPrefix(id, "orphan:"); ok {
		pid, _ := strconv.Atoi(v)
		for _, o := range s.cur.Orphans {
			if o.PID == pid && pid > 0 && len(o.Kill) > 0 {
				return advice.Action{Label: "kill", Steps: [][]string{o.Kill}, Destructive: true,
					Guards: []advice.Guard{{PID: o.PID, StartTicks: o.Start}}}, true
			}
		}
	}
	if v, ok := strings.CutPrefix(id, "ship:"); ok && s.cur.Ships != nil {
		// A ship that waits for your merge, as this service read it last.
		for _, sh := range s.cur.Ships.Ships {
			if sh.Repo+"#"+strconv.Itoa(sh.Number) == v && sh.Kind == ships.WaitsForYou && len(sh.Merge) > 0 {
				return advice.Action{Label: "merge", Steps: [][]string{sh.Merge}, Destructive: true}, true
			}
		}
	}
	if path, ok := strings.CutPrefix(id, "worktree:"); ok && s.cur.Git != nil {
		for _, r := range s.cur.Git.Repos {
			for _, wt := range r.Worktrees {
				if wt.Path == path && wt.Safe && len(wt.Remove) > 0 {
					return advice.Action{Label: "remove", Steps: [][]string{wt.Remove}, Destructive: true,
						Guards: []advice.Guard{{Worktree: wt.Path}}}, true
				}
			}
		}
	}
	return advice.Action{}, false
}

// answerAction types one line and Enter. Both steps are guarded: a bare
// Enter at a permission prompt would approve it.
func answerAction(pane, text string) advice.Action {
	g := advice.Guard{AnswerPane: pane}
	return advice.Action{Label: "answer", Steps: panes.Send(pane, text), Destructive: true, Guards: []advice.Guard{g, g}}
}

// message types a line and Enter into the pane of a session that does not
// wait: Claude Code queues a message typed while it works. It never goes
// into the question tool's menu as text, nor to a permission prompt, where
// Enter approves; and the pane must still hold the same agent.
func (s *Service) message(ctx context.Context, pane string, a proto.Act) proto.Result {
	if err := panes.CheckAnswer(a.Text); err != nil {
		return proto.Result{Err: strings.Replace(err.Error(), "the answer", "the message", 1)}
	}
	x, ok := s.sessionAt(pane)
	if !ok {
		return proto.Result{Err: "no agent session in " + pane + " any more"}
	}
	if err := s.messageable(pane, x.PID); err != nil {
		return proto.Result{Err: err.Error()}
	}
	release := s.holdPane(pane)
	defer release()
	return s.Actions.Do(ctx, messageAction(pane, x, a.Text), a.Confirm)
}

// messageAction types one line and Enter, each step guarded: the same
// agent is still in the pane, and it asks nothing a line would answer.
func messageAction(pane string, x sample.Session, text string) advice.Action {
	g := advice.Guard{MessagePane: pane, MessagePID: x.PID, PID: x.PID, StartTicks: x.Start}
	return advice.Action{Label: "message", Steps: panes.Send(pane, text), Destructive: true, Guards: []advice.Guard{g, g}}
}

// sessionAt is the agent session in a pane, as last sampled.
func (s *Service) sessionAt(pane string) (sample.Session, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, x := range s.cur.Sessions {
		if x.Pane == pane && pane != "" {
			return x, true
		}
	}
	return sample.Session{}, false
}

// messageable is the guard of a message, checked right before each step:
// nil when the agent pid is still the one in pane and it neither asks
// with the question tool nor waits at a permission prompt.
func (s *Service) messageable(pane string, pid int) error {
	x, ok := s.sessionAt(pane)
	switch {
	case !ok:
		return fmt.Errorf("no agent session in %s any more", pane)
	case x.PID != pid:
		return fmt.Errorf("another agent runs in %s now", pane)
	case x.Ask != nil:
		return fmt.Errorf("%s asks a question now: open it again to pick", x.Name)
	case x.Permits:
		return errors.New("a permission prompt is answered in its pane: Enter goes there")
	}
	if h, held := s.heldOf(x.SessionID); held {
		if h.By == holds.ByStoker {
			return fmt.Errorf("%s is held by the stoker: z lets it go", x.Name)
		}
		return fmt.Errorf("%s is parked: z lets it go", x.Name)
	}
	if it, ok := s.queued(pane); ok && it.State == queue.StatePermission {
		return errors.New("a permission prompt is answered in its pane: Enter goes there")
	}
	return nil
}

// pick answers the question tool's call a session waits on, with the
// choices the person made in the answer peek. Act.Text is a said.Answer.
func (s *Service) pick(ctx context.Context, pane string, a proto.Act) proto.Result {
	var ans said.Answer
	if err := json.Unmarshal([]byte(a.Text), &ans); err != nil {
		return proto.Result{Err: "the answer does not read: " + err.Error()}
	}
	ask, ok := s.askIn(pane)
	if !ok {
		return proto.Result{Err: "the session in " + pane + " no longer asks"}
	}
	if err := ask.Check(ans); err != nil {
		return proto.Result{Err: err.Error()}
	}
	for _, c := range ans.Choices {
		if c.Text == "" {
			continue
		}
		if err := panes.CheckAnswer(c.Text); err != nil {
			return proto.Result{Err: err.Error()}
		}
	}
	// Each page is guarded: the call still waits, and the pane, captured
	// right before the page's keys, shows that page. A person who answered
	// in the pane, or another console's pick, stops it there.
	steps, pages := panes.Pick(pane, ask, ans)
	guards := make([]advice.Guard, len(steps))
	for i, at := range pages {
		guards[at] = advice.Guard{AskPane: pane, AskID: ask.ID, Page: i + 1}
	}
	defer s.holdPane(pane)()
	return s.Actions.Do(ctx, advice.Action{Label: "answer", Steps: steps, Destructive: true, Guards: guards}, a.Confirm)
}

// menuWait is how long a page's guard waits for the pane to draw the page:
// the menu redraws a moment after the keys of the page before it.
var menuWait = 2 * time.Second

// menu is the guard of a pick's page: the pane, captured now, shows page
// (from 1) of the call id, the question or the review page after the last.
func (s *Service) menu(pane, id string, page int) error {
	ask, ok := s.askIn(pane)
	if !ok || ask.ID != id || page < 1 || page > len(ask.Questions)+1 {
		return fmt.Errorf("the session in %s no longer waits on that question", pane)
	}
	for deadline := time.Now().Add(menuWait); ; time.Sleep(100 * time.Millisecond) {
		screen, read := s.Capture([]string{pane})[pane]
		switch {
		case !read:
			return fmt.Errorf("the pane %s cannot be read", pane)
		case page <= len(ask.Questions) && said.ShowsQuestion(screen, ask.Questions[page-1]),
			page > len(ask.Questions) && said.ShowsReview(screen):
			return nil
		case time.Now().After(deadline):
			if page > len(ask.Questions) {
				return fmt.Errorf("the pane %s does not show the answers to submit", pane)
			}
			return fmt.Errorf("the pane %s does not show question %d", pane, page)
		}
	}
}

// holdPane makes the keys of one act to a pane one sequence: two consoles
// that answer the same pane take turns. It returns the release.
func (s *Service) holdPane(pane string) func() {
	v, _ := s.panes.LoadOrStore(pane, &sync.Mutex{})
	mu := v.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// askIn is the question tool's call the session in a pane waits on.
func (s *Service) askIn(pane string) (said.Ask, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, x := range s.cur.Sessions {
		if x.Pane == pane && pane != "" && x.Ask != nil {
			return *x.Ask, true
		}
	}
	return said.Ask{}, false
}

// asking is the guard of a pick, checked right before its first key.
func (s *Service) asking(pane, id string) bool {
	ask, ok := s.askIn(pane)
	return ok && ask.ID == id
}

// queued finds the queue item of a pane. Hold no lock.
func (s *Service) queued(pane string) (queue.Item, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, it := range s.cur.Queue {
		if it.Pane == pane {
			return it, true
		}
	}
	return queue.Item{}, false
}

// answerable is the guard of an answer, checked right before each step.
func (s *Service) answerable(pane string) bool {
	it, ok := s.queued(pane)
	return ok && it.State != queue.StatePermission && !s.permits(pane)
}

// permits tells if the last sample shows the approval menu in pane: a
// typed line would approve the call, whatever the hooks said.
func (s *Service) permits(pane string) bool {
	x, ok := s.sessionAt(pane)
	return ok && x.Permits
}

// doors reads the machine's doors: a few small files, cheap on each sample.
func (s *Service) doors() []doors.Door {
	if s.Setup == nil {
		return nil
	}
	return setup.Doors(*s.Setup)
}

// door opens a door ("primary", Text = the Sum of the preview the person
// saw; "also" opens it with the choice of its preview turned on) or closes
// it ("secondary"). All need a typed y: they change the person's own files.
func (s *Service) door(id string, a proto.Act) proto.Result {
	if s.Setup == nil {
		return proto.Result{Err: "no doors on this machine"}
	}
	if a.Confirm != "y" {
		return proto.Result{Err: "needs a typed y"}
	}
	defer s.refreshDoors()
	if a.Which == "secondary" {
		if err := setup.Close(*s.Setup, id); err != nil {
			return proto.Result{Err: err.Error()}
		}
		return proto.Result{Ran: [][]string{{"close", id}}}
	}
	path := ""
	for _, d := range s.doors() {
		if d.ID == id {
			path = d.Path
		}
	}
	backup, err := setup.Open(*s.Setup, id, a.Text, a.Which == "also")
	ran := [][]string{{"write", path}}
	if backup != "" {
		ran = append(ran, []string{"backup", backup})
	}
	if err != nil {
		if backup == "" {
			ran = nil
		}
		return proto.Result{Ran: ran, Err: err.Error()}
	}
	return proto.Result{Ran: ran}
}

// refreshDoors sends the doors at once, so a door folds when it is done.
func (s *Service) refreshDoors() {
	ds := s.doors()
	s.mu.Lock()
	s.cur.Doors, s.gen = ds, s.gen+1
	s.mu.Unlock()
	s.broadcast()
}

func (s *Service) doorsGen() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.gen
}

// storeDoors keeps doors read at gen, unless a door was written since.
func (s *Service) storeDoors(gen uint64, ds []doors.Door) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.setDoors(gen, ds)
}

// setDoors is storeDoors with s.mu held.
func (s *Service) setDoors(gen uint64, ds []doors.Door) {
	if s.gen == gen {
		s.cur.Doors = ds
	}
}

func (s *Service) names() proto.Result {
	s.mu.Lock()
	sessions, accounts := s.cur.Sessions, s.cur.Limits
	s.mu.Unlock()
	n := &proto.Names{Sessions: map[int]string{}, Accounts: map[int]string{}}
	if s.PushNumbers != nil {
		n.Sessions, n.Accounts = s.PushNumbers.Names(sessions, accounts)
	}
	return proto.Result{Names: n}
}
