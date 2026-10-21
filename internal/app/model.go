// Package app is the console: a Bubble Tea program that keeps a copy of the
// state the service sends and draws it. It does not read the machine; it
// asks the service to act, and runs only the steps that move its own
// terminal.
package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/blox-eng/matchblox/internal/advice"
	"github.com/blox-eng/matchblox/internal/doors"
	"github.com/blox-eng/matchblox/internal/history"
	"github.com/blox-eng/matchblox/internal/proto"
	"github.com/blox-eng/matchblox/internal/queue"
	"github.com/blox-eng/matchblox/internal/remote"
	"github.com/blox-eng/matchblox/internal/sample"
	"github.com/blox-eng/matchblox/internal/transport"
)

// installCmd updates matchblox on the side that is older.
const installCmd = "curl -fsSL https://matchblox.sh | sh"

type Options struct {
	Conn transport.Conn
	// Redial connects again after the connection drops. Nil: no retry.
	Redial func() (transport.Conn, error)
	// Binary is the version in the console's hello.
	Binary string
	// CompactAt and CompactTokens colour context at the compact threshold.
	CompactAt     float64
	CompactTokens int
	// Run executes one step that moves this terminal (Nav). Nil means exec.
	Run func(argv []string) error
	// Local: the service runs on this machine from the same install, so an
	// older one is replaced instead of asking the person to update it.
	Local bool
	// Owner is the pid of the local service, to name it when it does not
	// answer. Nil or 0: unknown.
	Owner func() int
	// Now is the clock for ages on the screen. Nil: time.Now. The replay
	// sets a fixed clock.
	Now func() time.Time
	// NoMotion makes every motion an instant change.
	NoMotion bool
	// OutsideTmux: the console does not run in tmux, so going to a pane
	// attaches to it, and the console comes back when tmux detaches.
	OutsideTmux bool
	// Host is the host this console reaches over SSH; "" is this machine.
	// Its tmux moves and door commands run there, with the builder's own
	// ssh login.
	Host string
	// DialErr is why the first dial failed; the console starts with it.
	DialErr error
	// Self is this binary, which connects a host (`matchblox connect`).
	Self string
	// Gen tells this console's redial messages from those of a console the
	// shell closed.
	Gen int
	// Layered: the console sits under the hosts layer, and esc with
	// nothing to cancel or clear goes back to it.
	Layered bool
	// StateDir keeps what the person did to the recommendations (dismissed,
	// folded). "": kept for this run only.
	StateDir string
	// Exec gives this terminal to argv until it exits. Nil: tea.ExecProcess.
	Exec func(argv []string, done func(error) tea.Msg) tea.Cmd
	// Pair writes and reads the pair page's one-time key on this machine.
	// Nil: this console cannot pair.
	Pair *PairHost
	// LastClient is the tty and the creation time of the client that was
	// active last in the home session. Nil: ask tmux. "" or an error: the
	// builder's.
	LastClient func() (tty string, created time.Time, err error)
	// Clients are the ttys of the clients of this console's tmux server, for
	// a focus that moves the phone's client. Nil: ask tmux.
	Clients func() ([]string, error)
	// Clip copies text to the builder's clipboard. Nil: OSC 52 with the output.
	Clip func(text string)
}

type helloMsg proto.Hello
type stateMsg proto.State
type resultMsg proto.Result
type lostMsg struct {
	err  error
	conn transport.Conn
}

// fromConn tags a message with the connection it came from: after a
// reconnect, messages from the old one are dropped.
type fromConn struct {
	conn transport.Conn
	msg  tea.Msg
}

// The redial messages carry the generation of the console that asked:
// after the shell opens another console, a late one is dropped.
type redialMsg struct{ gen int }

// silentMsg fires when a connection gave no state in silentAfter.
type silentMsg struct{ conn transport.Conn }
type redialFailed struct {
	err error
	gen int
}
type connMsg struct {
	conn transport.Conn
	gen  int
}
type ranMsg struct {
	cmd    string
	err    error
	jumped string // the pane a jump went to
}

// action waits for the person's confirm. A Nav action runs here; any other
// is sent to the service by name.
type action struct {
	label       string
	steps       [][]string
	destructive bool
	nav         bool
	attach      bool                        // the last step takes the terminal until it exits
	rec         string                      // the service's name for it
	which       string                      // primary | secondary
	text        string                      // what an answer types; a door's preview Sum
	batch       []string                    // worktrees: one guarded act each
	say         string                      // what the confirm shows, when not the first step
	term        bool                        // the step takes this terminal (a door's command)
	pair        bool                        // the pair door: it opens the pair page, never a pending step
	door        string                      // the door it opens or closes
	remote      bool                        // steps[0] is the ssh argv that runs it on the host, checked
	connect     bool                        // it connects the host: install, update or authorize
	pane        string                      // a jump: the pane it goes to
	local       func(Model) (Model, string) // it runs in this console; its string is the flash
	again       string                      // a kill of a session that works or waits: what the second y's confirm says
}

// recRun is an act on a recommendation: the row shows its outcome.
type recRun struct {
	rec, act string // the rec, and the act id ("nav": run in the console)
	key      string // the key that runs it again
	out      string // what came of it; "" while it runs
	failed   bool
}

func (m Model) isRec(id string) bool {
	for _, r := range m.recs {
		if r.ID == id {
			return true
		}
	}
	return false
}

func (m *Model) startRec(a action, act string) {
	if a.rec == "" || !m.isRec(a.rec) {
		return
	}
	key := "⏎"
	if a.which == "secondary" {
		key = "x"
	}
	m.recRun = recRun{rec: a.rec, act: act, key: key}
}

// endRec turns the row of the act into its outcome.
func (m *Model) endRec(act string, r proto.Result) {
	if m.recRun.act != act || act == "" {
		return
	}
	again := " · " + m.recRun.key + " tries again"
	switch {
	case r.Err != "":
		m.recRun.out, m.recRun.failed = "failed: "+r.Err+again, true
	case len(r.Skipped) > 0:
		m.recRun.out, m.recRun.failed = "skipped: "+r.Skipped[len(r.Skipped)-1]+again, true
	default:
		m.recRun.out, m.recRun.failed = describe(r), false
	}
	m.recRun.act = ""
}

func (a action) String() string {
	if a.say != "" {
		return a.say
	}
	s := strings.Join(a.steps[0], " ")
	if n := len(a.steps); n > 1 {
		s += fmt.Sprintf("  (+%d more)", n-1)
	}
	return s
}

func fromAdvice(r advice.Rec, which string) *action {
	a := r.Primary
	if which == "secondary" {
		a = r.Second
	}
	if a == nil || len(a.Steps) == 0 {
		return nil
	}
	return &action{label: a.Label, steps: a.Steps, destructive: a.Destructive, nav: a.Nav, rec: r.ID, which: which}
}

type Model struct {
	recPrefs   recPrefs // dismissed recommendations and the fold
	opt        Options
	conn       transport.Conn
	st         styles
	width      int
	height     int
	snap       sample.Snapshot
	push       string // the push sender's line for the Machine tab, or ""
	have       bool
	git        *proto.GitReport
	recs       []advice.Rec
	queue      []queue.Item
	doors      []doors.Door // the open doors, above the sessions
	stoker     *proto.StokerView
	doorPick   string // the selected door
	doorAt     int    // its row, for the door after it when it folds
	previewTop int    // the first line of a door's diff on screen
	paneSel    int
	peek       *peek             // the answer peek, over the screen
	peekSent   map[string]string // the peek's sends in flight: act id -> the session's name
	peekQuiet  time.Time         // the peek takes no key until then: it just moved
	changes    map[string]change // pane -> a match change that plays once
	animating  bool
	hist       history.Series
	selPID     int
	orphanPID  int
	recPick    string // the selected recommendation, above the sessions
	chose      bool   // a session was picked; before, the first that waits is
	gitSel     int
	shipReport *proto.Ships
	shipSel    int
	tab        int
	pending    *action
	flash      string
	history    *series
	quitting   bool
	host       proto.Hello
	mismatch   bool
	blocked    error // why the host cannot be reached, until the builder acts
	lost       bool
	backoff    time.Duration
	redialing  bool
	answered   bool // a state came on the current connection
	replaced   bool
	acts       int
	recRun     recRun // the last act on a recommendation, and what came of it
	dark       bool
	splashAt   time.Time
	splashDone bool
	armed      string // the step a second tap runs: what the first tap showed
	// held is what the builder pressed or tapped while the stoker was in
	// charge: it runs once they answer the take-over question.
	held       func(Model) (tea.Model, tea.Cmd)
	touched    time.Time // the last key that kept the stoker's rest
	all        lists     // what the service sent; queue, recs and snap hold the drawn copies
	filter     string    // the search of the open tab
	searching  bool      // the search line takes the keys
	sessSort   sortBy
	sessGroup  int // what Sessions is grouped by: groupNeed .. groupNone
	holds      []proto.Hold
	reasons    map[string]proto.IdleReason
	agents     []string          // the agents a new session can start on the host
	renames    map[string]string // the names sessions take at their next rest, by pane
	form       *form             // the open New session or Rename form; nil none
	lastAgent  string            // the agent this console last started
	paneSort   sortBy
	picked     map[string]bool // worktrees x removes
	batch      *batch
	showExited bool // e: the exited agents, one by one
	// dockPanel is the panel a phone's dock shows (usage, the machine, the
	// stoker), u cycling it; dockSwitched: u was pressed while the stoker
	// claimed the dock, so it no longer takes it by itself.
	dockPanel    int
	dockSwitched bool
	// stokerSel is the selected row of the STOKER tab, stokerShown the row
	// whose command it shows, ackSent when the steps were last marked seen.
	stokerSel    int
	stokerShown  string
	ackSent      time.Time
	sheet        *sheet       // the launch sheet, while it is open
	stokerServer string       // the tmux server (-L) the stoker agent runs on; "" none
	toast        string       // what an action did, on the header's right
	toasts       int          // counts the toasts: a later one outlives the timer of an earlier one
	devices      []PairDevice // the keys that open this host, read when the Machine tab opens
	devErr       error        // why the keys could not be read
	devPick      string       // the selected device row: its fingerprint, or devPlus
	pair         *pairPage    // the pair page, while it shows
	pairPolling  bool         // a poll of the pair page is on its way
	pairOffers   *pairWaits   // the offers of a key that still run: the end of the program waits for them
	focusAt      time.Time    // the At of the last focus this console acted on
}

func New(opt Options) Model {
	if opt.CompactAt <= 0 {
		opt.CompactAt, opt.CompactTokens = sample.DefaultRules.CompactAt, sample.DefaultRules.CompactTokens
	}
	if opt.Run == nil {
		opt.Run = run
	}
	if opt.Binary == "" {
		opt.Binary = "dev"
	}
	m := Model{opt: opt, conn: opt.Conn, st: newStyles(true), dark: true, width: 100, height: 30, history: newSeries(60),
		recPrefs: loadRecPrefs(opt.StateDir)}
	m.splashAt = m.now()
	return m
}

// run executes a Nav step: it only moves this terminal, so a short bound
// is enough.
func run(argv []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, argv[0], argv[1:]...).Run() //nolint:gosec // argv from the service's advice, never a shell string
}

// silentAfter is how long a connected service may stay silent before the
// console says so and how to fix it.
const silentAfter = 3 * time.Second

func (m Model) Init() tea.Cmd {
	if err := m.opt.DialErr; err != nil && m.conn == nil {
		return tea.Batch(tea.RequestBackgroundColor, func() tea.Msg { return lostMsg{err: err} }, m.splashCmd())
	}
	return tea.Batch(tea.RequestBackgroundColor, m.hello(), m.recv(), m.watchSilence(), m.splashCmd())
}

func (m Model) watchSilence() tea.Cmd {
	c := m.conn
	return tea.Tick(silentAfter, func(time.Time) tea.Msg { return silentMsg{c} })
}

func (m Model) hello() tea.Cmd {
	c, bin := m.conn, m.opt.Binary
	if c == nil {
		return nil
	}
	return func() tea.Msg {
		if err := c.Send(proto.KindHello, "", proto.Hello{Version: proto.Version, Binary: bin, Client: proto.ClientConsole}); err != nil {
			return lostMsg{err, c}
		}
		return nil
	}
}

// recv reads one message. Each handled message asks for the next, so one
// read is in flight at a time.
func (m Model) recv() tea.Cmd {
	c := m.conn
	if c == nil {
		return nil
	}
	return func() tea.Msg {
		for {
			env, err := c.Recv()
			if err != nil {
				return lostMsg{err, c}
			}
			switch env.Kind {
			case proto.KindHello:
				var h proto.Hello
				if json.Unmarshal(env.Body, &h) == nil {
					return fromConn{c, helloMsg(h)}
				}
			case proto.KindSnapshot:
				var st proto.State
				if json.Unmarshal(env.Body, &st) == nil {
					return fromConn{c, stateMsg(st)}
				}
			case proto.KindResult:
				var r proto.Result
				if json.Unmarshal(env.Body, &r) == nil {
					return fromConn{c, resultMsg(r)}
				}
			}
		}
	}
}

func (m *Model) send(a proto.Act) tea.Cmd {
	c := m.conn
	m.acts++
	id := "a" + strconv.Itoa(m.acts)
	return func() tea.Msg {
		if err := c.Send(proto.KindAct, id, a); err != nil {
			return lostMsg{err, c}
		}
		return nil
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.BackgroundColorMsg:
		m.dark = msg.IsDark()
		m.st = newStyles(m.dark)
	case splashMsg:
		if m.splashing() {
			return m, m.splashCmd()
		}
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case fromConn:
		if msg.conn != m.conn {
			return m, nil
		}
		return m.Update(msg.msg)
	case helloMsg:
		m.host = proto.Hello(msg)
		if m.olderLocal() {
			// The old service exits; the redial starts this binary.
			m.replaced = true
			m.flash = fmt.Sprintf("replacing the service %s with %s…", m.host.Binary, m.opt.Binary)
			return m, tea.Batch(m.send(proto.Act{RecID: "service:replace"}), m.recv())
		}
		m.mismatch = m.host.Version != proto.Version
		if m.mismatch {
			return m, nil
		}
		return m, m.recv()
	case stateMsg:
		first := !m.have
		st := proto.State(msg)
		m.queue = st.Queue
		toast := m.stokerNews(st.Stoker)
		m.stoker, m.stokerServer = st.Stoker, st.StokerServer
		if !m.stokerClaims() {
			m.dockSwitched = false
		}
		m.doors = m.shownDoors(st.Doors)
		m.noteChanges(st.Sessions)
		m.snap, m.git, m.recs, m.have, m.answered = st.Snapshot, st.Git, st.Recommendations, true, true
		m.shipReport = st.Ships
		m.holds, m.reasons = st.Holds, st.Reasons
		m.agents, m.renames = st.Agents, st.Renames
		m.push = st.Push
		if m.recRun.rec != "" && m.recRun.act == "" && !m.isRec(m.recRun.rec) {
			m.recRun = recRun{} // its rec is gone: a later one with the same id starts fresh
		}
		m.all = lists{queue: st.Queue, recs: st.Recommendations, sessions: st.Sessions, orphans: st.Orphans}
		m.refresh()
		if m.lost {
			m.lost, m.backoff, m.flash = false, 0, ""
		}
		m.history.push(m.snap.Machine)
		switch {
		case st.History != nil:
			m.hist = *st.History
		case !first:
			m.hist.Append(history.FromSnapshot(m.snap))
		}
		m.keepSelection()
		m, focus := m.takeFocus(st.Focus)
		var news, order tea.Cmd
		if toast != "" && !first {
			news = m.say(toast)
		}
		if m.sheet != nil && m.sheet.queued && m.sheet.act == "" && m.stoker != nil && m.stoker.Ready {
			m, order = m.sendOrder() // the stoker rests now: the order that waited for it goes
		}
		return m, tea.Batch(m.recv(), m.animate(), news, order, focus)
	case doorRanMsg:
		return m.doorRan(msg)
	case pairOfferMsg:
		return m.offered(msg)
	case pairPollMsg:
		return m.pairPolled()
	case pairFrameMsg:
		if m.pairMoving() {
			return m, pairFrame()
		}
	case animMsg:
		m.animating = false
		return m, m.animate()
	case toastMsg:
		if msg.n == m.toasts {
			m.toast = ""
		}
	case resultMsg:
		m.endRec(msg.ActID, proto.Result(msg))
		if mm, ok := m.formResult(proto.Result(msg)); ok {
			return mm, mm.recv()
		}
		if mm, ok := m.sheetResult(proto.Result(msg)); ok {
			if r := proto.Result(msg); r.Err == "" && mm.sheet == nil {
				toast := mm.say("the order is on")
				return mm, tea.Batch(mm.recv(), toast)
			}
			return mm, mm.recv()
		}
		if r := proto.Result(msg); m.peekSent[r.ActID] != "" {
			name := m.peekSent[r.ActID]
			m.peekSent = maps.Clone(m.peekSent)
			delete(m.peekSent, r.ActID)
			why := r.Err
			if n := len(r.Skipped); n > 0 && why == "" {
				why = r.Skipped[n-1]
			}
			if why != "" {
				m.toast, m.flash = "", tilde("not sent to "+name+": "+why)
				return m, m.recv()
			}
			return m, tea.Batch(m.recv(), m.say("sent to "+name))
		}
		if m.tally(proto.Result(msg)) {
			return m, m.recv()
		}
		r := proto.Result(msg)
		if text := describe(r); text != "" && r.Err == "" && len(r.Skipped) == 0 {
			toast := m.say(tilde(text))
			return m, tea.Batch(m.recv(), toast)
		} else if text != "" {
			m.flash = tilde(text)
		}
		return m, m.recv()
	case lostMsg:
		if m.mismatch || m.quitting || m.redialing || (msg.conn != nil && msg.conn != m.conn) {
			return m, nil
		}
		if m.conn != nil {
			_ = m.conn.Close()
		}
		m.lost = true
		if blocking(msg.err) {
			// Trying again cannot help: connecting the host can.
			m.blocked, m.flash = msg.err, ""
			return m, nil
		}
		m.flash = "connection lost: " + msg.err.Error()
		if m.batch != nil {
			// Their results went with the connection.
			m.flash += fmt.Sprintf("; %d removals have no answer, r on Git rescans", len(m.batch.ids))
			m.batch = nil
		}
		if m.opt.Redial == nil {
			return m, nil
		}
		m.redialing = true
		m.backoff = min(max(m.backoff*2, 250*time.Millisecond), 10*time.Second)
		m.flash += "; trying again in " + m.backoff.String()
		gen := m.opt.Gen
		return m, tea.Tick(m.backoff, func(time.Time) tea.Msg { return redialMsg{gen} })
	case redialMsg:
		if msg.gen != m.opt.Gen || !m.redialing || m.quitting {
			return m, nil // a tick from a console that was closed
		}
		redial, gen := m.opt.Redial, m.opt.Gen
		return m, func() tea.Msg {
			c, err := redial()
			if err != nil {
				return redialFailed{err, gen}
			}
			return connMsg{c, gen}
		}
	case silentMsg:
		if msg.conn != m.conn || m.answered || m.mismatch {
			return m, nil
		}
		if m.opt.Host != "" {
			m.flash = m.opt.Host + " does not answer: run ssh " + m.opt.Host + " matchblox status to see why"
			return m, nil
		}
		pid := 0
		if m.opt.Owner != nil {
			pid = m.opt.Owner()
		}
		if pid > 0 {
			m.flash = fmt.Sprintf("the service (pid %d) does not answer: kill %d, then start matchblox again", pid, pid)
		} else {
			m.flash = "the service does not answer: stop `matchblox serve`, then start matchblox again"
		}
		return m, nil
	case redialFailed:
		if msg.gen != m.opt.Gen {
			return m, nil
		}
		m.redialing = false
		return m.Update(lostMsg{err: msg.err})
	case connectedMsg:
		return m.connected(msg)
	case connMsg:
		if msg.gen != m.opt.Gen || m.quitting {
			_ = msg.conn.Close() // the console that dialed it is gone
			return m, nil
		}
		m.conn, m.redialing, m.answered = msg.conn, false, false
		m.flash = "connected again"
		return m, tea.Batch(m.hello(), m.recv(), m.watchSilence())
	case ranMsg:
		if msg.err != nil {
			m.endRec("nav", proto.Result{Err: msg.cmd + ": " + msg.err.Error()})
		} else {
			m.endRec("nav", proto.Result{Ran: [][]string{strings.Fields(msg.cmd)}})
		}
		if msg.err != nil {
			m.flash = "failed: " + msg.cmd + ": " + msg.err.Error()
			return m, nil
		}
		if msg.jumped != "" {
			m = m.nextAfter(msg.jumped)
		}
		toast := m.say("ran: " + msg.cmd)
		return m, toast
	case tea.MouseClickMsg:
		m, touch := m.touchRest()
		next, cmd := m.tap(msg.Mouse())
		return next, tea.Batch(touch, cmd)
	case tea.MouseWheelMsg:
		m, touch := m.touchRest()
		next, cmd := m.wheel(msg.Mouse())
		return next, tea.Batch(touch, cmd)
	case tea.KeyPressMsg:
		if m.splashing() {
			m.splashDone = true // any key stops the start screen, and does nothing else
			return m, nil
		}
		m.armed = "" // a key between two taps: the next tap shows again
		var touch tea.Cmd
		// f and n are stoker acts themselves: a touch sent with them would
		// race them on the service.
		if k := msg.String(); m.pair != nil || m.peek != nil || m.sheet != nil || m.form != nil || m.searching || k != "f" && k != "n" {
			m, touch = m.touchRest()
		}
		var next tea.Model
		var cmd tea.Cmd
		switch {
		case m.pair.owns():
			next, cmd = m.pairKey(msg.String())
		case m.pairStripEsc(msg.String()):
			next, cmd = m.closePairPage(""), nil
		case m.sheet != nil:
			next, cmd = m.sheetKey(msg)
		case m.form != nil:
			next, cmd = m.formKey(msg)
		case m.peek != nil:
			next, cmd = m.peekKey(msg)
		case m.searching:
			next, cmd = m.search(msg)
		default:
			next, cmd = m.keyed(msg.String())
		}
		return next, tea.Batch(touch, cmd)
	}
	return m, nil
}

// focusWait is how long a focus waits for the phone's tmux client to
// attach: the gate sends the act before it runs the console.
var focusWait = 10 * time.Second

// focusGone is what a focus on a session that ended says.
const focusGone = "That session ended."

// takeFocus opens the session a tap on the island or a buzz asked for, once
// for each focus the service sends, on Sessions: its answer peek when it
// waits, else its pane on the phone's own tmux client. A form, sheet, pair
// page, take-over or a peek with text typed on another session keeps the
// keys: what the person types there is not thrown away.
func (m Model) takeFocus(f *proto.Focus) (Model, tea.Cmd) {
	if f == nil || f.At.Equal(m.focusAt) {
		return m, nil
	}
	m.focusAt = f.At
	var s sample.Session
	found := false
	for _, x := range m.snap.Sessions {
		if f.SessionID != "" && x.SessionID == f.SessionID || f.SessionID == "" && f.Pane != "" && x.Pane == f.Pane {
			s, found = x, true
			break
		}
	}
	typing := m.peek != nil && m.peek.text != "" && (!found || m.peek.pane != s.Pane)
	busy := m.pair.owns() || m.sheet != nil || m.form != nil || m.held != nil || typing
	if f.Gone || !found {
		if !busy {
			m.setTab(tabSessions)
			m.pending = nil
		}
		m.flash = focusGone
		return m, nil
	}
	if busy {
		return m, nil
	}
	m.setTab(tabSessions)
	m.pending, m.peek = nil, nil
	m.doorPick, m.recPick, m.chose, m.selPID = "", "", true, s.PID
	if s.Pane == "" {
		return m, nil // outside tmux: no pane to open
	}
	if _, waits := m.waitOf(s); waits {
		// The home console is shared: the peek shows on the phone too, and an
		// Enter on its way at the desk must not answer it.
		return m.peekTo(s.Pane), nil
	}
	// Only the phone's client moves, and only on this console's own tmux: a
	// console of another host (over ssh) or outside tmux never moves a
	// terminal for a tap on the phone.
	if m.opt.Host != "" || m.opt.OutsideTmux || !remote.ClientTTY(f.Client) {
		return m, nil
	}
	return m, focusJump(m.clients(), m.opt.Run, f.Client, s.Pane)
}

// clients is how this console reads the clients of its tmux server.
func (m Model) clients() func() ([]string, error) {
	if m.opt.Clients != nil {
		return m.opt.Clients
	}
	return tmuxClients
}

// tmuxClients are the ttys of the clients of this console's tmux server.
func tmuxClients() ([]string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	out, err := exec.CommandContext(ctx, "tmux", "list-clients", "-F", "#{client_tty}").Output()
	if err != nil {
		return nil, err
	}
	return strings.Fields(string(out)), nil
}

// focusJump waits up to focusWait for the client on tty to attach to this
// console's tmux, then moves it to pane. A client that never comes is not
// this console's: nothing moves.
func focusJump(clients func() ([]string, error), run func([]string) error, tty, pane string) tea.Cmd {
	return func() tea.Msg {
		argv := remote.FocusArgv(tty, pane)
		if !remote.FocusAllowed(argv) {
			return nil
		}
		for deadline := time.Now().Add(focusWait); ; time.Sleep(100 * time.Millisecond) {
			if ttys, err := clients(); err == nil && slices.Contains(ttys, tty) {
				break
			}
			if time.Now().After(deadline) {
				return nil
			}
		}
		return ranMsg{cmd: strings.Join(argv, " "), err: run(argv), jumped: pane}
	}
}

// olderLocal tells if the local service should give way to this console:
// an older wire, or another build. Once only, so two consoles of two
// versions do not replace each other forever.
func (m Model) olderLocal() bool {
	if !m.opt.Local || m.replaced || m.opt.Redial == nil {
		return false
	}
	return m.host.Version < proto.Version || (m.opt.Binary != "dev" && m.host.Binary != m.opt.Binary)
}

func describe(r proto.Result) string {
	var parts []string
	if len(r.Ran) > 0 {
		cmd := strings.Join(r.Ran[0], " ")
		if n := len(r.Ran); n > 1 {
			cmd += fmt.Sprintf("  (+%d more)", n-1)
		}
		parts = append(parts, "ran: "+cmd)
	}
	if len(r.Skipped) > 0 {
		parts = append(parts, fmt.Sprintf("skipped %d: %s", len(r.Skipped), r.Skipped[len(r.Skipped)-1]))
	}
	if r.Err != "" {
		parts = append(parts, "failed: "+r.Err)
	}
	return strings.Join(parts, "; ")
}

// keyed is a key pressed, and the stoker's steps seen when the tab shows them.
func (m Model) keyed(k string) (tea.Model, tea.Cmd) {
	next, cmd := m.key(k)
	if nm, ok := next.(Model); ok {
		nm, ack := nm.seeStoker()
		return nm, tea.Batch(cmd, ack)
	}
	return next, cmd
}

func (m Model) key(k string) (tea.Model, tea.Cmd) {
	if m.held != nil {
		return m.answerTakeOver(k)
	}
	if m.asksTakeOver(k) {
		m.held, m.flash = m.hold(k), ""
		return m, nil
	}
	if m.pending != nil && m.pending.door != "" && m.pending.which != "secondary" {
		if mm, ok := m.scrollPreview(k); ok {
			return mm, nil
		}
	}
	if m.pending == nil && k == "enter" && errors.Is(m.blocked, remote.ErrHostKeyChanged) {
		return m.tryAgain()
	}
	if m.pending == nil && k == "enter" && (m.mismatch || m.blocked != nil) {
		m.pending = m.connectAction()
		return m, nil
	}
	if m.pending != nil {
		a := *m.pending
		m.pending = nil
		if k == "y" && a.again != "" {
			// It works or waits for you: one more y, said plainly.
			a.say, a.again, a.text = a.again, "", killAnyway
			m.pending = &a
			return m, nil
		}
		// A destructive action needs a typed y; Enter alone is not consent.
		if k == "y" || (k == "enter" && !a.destructive) {
			return m.confirm(a)
		}
		m.flash = "cancelled: " + a.String()
		return m, nil
	}
	m.flash = ""
	switch k {
	case "c":
		return m.openStoker(), nil
	case "q", "ctrl+c":
		return m.quit()
	case "tab":
		m.setTab((m.tab + 1) % len(tabNames))
	case "shift+tab":
		m.setTab((m.tab + len(tabNames) - 1) % len(tabNames))
	case "/":
		m.searching, m.filter = true, ""
		m.refresh()
	case "esc":
		if m.filter != "" {
			m.filter = ""
			m.refresh()
		} else if m.opt.Layered {
			return m, func() tea.Msg { return layerMsg{} }
		}
	case "s":
		if s, _ := m.sortable(); s != nil {
			_, names := m.sortable()
			m.sortOn((s.col + 1) % len(names))
		}
	case "S", "shift+s":
		if s, _ := m.sortable(); s != nil {
			m.sortOn(s.col)
		}
	case "g":
		if m.tab == tabSessions {
			m.sessGroup = (m.sessGroup + 1) % len(groupModes)
			m.flash = "group: " + groupModes[m.sessGroup]
			m.refresh()
		}
	case "z":
		if s, ok := m.selectedSession(); ok {
			if s.Pane == "" {
				m.flash = s.Name + " is outside tmux: nothing to park"
				break
			}
			cmd := m.send(proto.Act{RecID: "park:" + s.Pane})
			return m.pairActed(action{rec: "park:" + s.Pane}), cmd
		}
	case "space", " ":
		if m.tab == tabGit {
			m.mark()
		}
	case "e":
		if m.tab == tabSessions {
			m.showExited = !m.showExited
		}
	case "X", "shift+x":
		if m.tab == tabGit {
			m.markAllSafe()
		}
	case "Q", "shift+q":
		if m.tab == tabStoker {
			return m.quitAgent()
		}
	case "up", "k":
		m.move(-1)
	case "down", "j":
		m.move(1)
	case "enter":
		switch m.tab {
		case tabShips:
			return m.toDriver(), nil
		case tabStoker:
			if r, ok := m.stokerSelected(); ok && r.kind == rowAgent {
				return m.openAgent()
			}
			m.showCommand()
			return m, nil
		}
		if a := m.primary(); a != nil && a.pair {
			return m.openPairDoor()
		}
		m.pending, m.previewTop = m.primary(), 0
	case "x":
		m.pending = m.secondary()
	case "N", "shift+n":
		return m.openNew()
	case "y":
		if m.tab == tabStoker {
			return m.grantStep()
		}
	case "a":
		if m.tab == tabShips {
			return m.shipAnswer(), nil
		}
		if pane, why := m.peekTarget(); pane != "" {
			m.openPeek(pane)
		} else {
			m.flash = why
		}
	case "d":
		if m.tab == tabSessions {
			m.dismissRec()
		}
	case "h":
		if m.tab == tabSessions {
			m.foldRecs()
		}
	case "p":
		m.setTab(tabShips)
	case "u":
		m.cycleDock()
	case "f":
		return m.stokerKey(false)
	case "n":
		return m.stokerKey(true)
	case "o":
		return m.orderKey()
	case "r":
		if m.tab == tabSessions {
			return m.openRename()
		}
		if m.tab == tabGit && m.conn != nil && !m.lost {
			m.flash = "scanning git…"
			return m, m.send(proto.Act{RecID: "rescan:git", Which: "primary"})
		}
	default:
		if len(k) == 1 && k[0] >= '1' && int(k[0]-'1') < len(tabNames) {
			m.setTab(int(k[0] - '1'))
		}
	}
	return m, nil
}

// confirm runs an action the person consented to: a Nav step here, any
// other on the service.
func (m Model) confirm(a action) (tea.Model, tea.Cmd) {
	if a.local != nil {
		next, msg := a.local(m)
		next.flash = msg
		return next, nil
	}
	if a.remote {
		return m.onHost(a)
	}
	if len(a.batch) > 0 {
		if m.conn == nil || m.lost {
			m.flash = "not sent, no connection: " + a.String()
			return m, nil
		}
		return m.sendBatch(a)
	}
	if a.attach {
		return m, m.attach(a)
	}
	if a.term {
		return m.runDoor(a)
	}
	if a.nav {
		run := m.opt.Run
		m.startRec(a, "nav")
		return m, func() tea.Msg { return runNav(a, run) }
	}
	if m.conn == nil || m.lost {
		m.flash = "not sent, no connection: " + a.String()
		return m, nil
	}
	confirm := ""
	if a.destructive {
		confirm = "y"
	}
	m.flash = "sent: " + a.String()
	cmd := m.send(proto.Act{RecID: a.rec, Which: a.which, Confirm: confirm, Text: a.text})
	m.startRec(a, "a"+strconv.Itoa(m.acts))
	return m.pairActed(a), cmd
}

func runNav(a action, run func([]string) error) ranMsg {
	for _, step := range a.steps {
		if !remote.NavAllowed(step) && !remote.PopupAllowed(step) {
			return ranMsg{cmd: strings.Join(step, " "), err: fmt.Errorf("refused: only tmux switch-client, new-window and the stoker's popup run in the console")}
		}
	}
	for _, step := range a.steps {
		if err := run(step); err != nil {
			return ranMsg{cmd: strings.Join(step, " "), err: err}
		}
	}
	return ranMsg{cmd: a.String(), jumped: a.pane}
}

// jump goes to a pane: inside tmux the client switches to it; outside, the
// console gives the terminal to `tmux attach` and comes back after it.
func (m Model) jump(pane string) *action {
	if m.opt.OutsideTmux {
		return &action{label: "attach", nav: true, attach: true, pane: pane, steps: [][]string{
			{"tmux", "select-window", "-t", pane},
			{"tmux", "select-pane", "-t", pane},
			{"tmux", "attach-session", "-t", pane},
		}}
	}
	return &action{label: "jump", nav: true, pane: pane, steps: [][]string{{"tmux", "switch-client", "-t", pane}}}
}

// attach runs the moves, then gives the terminal to the last step.
func (m Model) attach(a action) tea.Cmd {
	for _, step := range a.steps {
		if !remote.NavAllowed(step) {
			return func() tea.Msg {
				return ranMsg{cmd: strings.Join(step, " "), err: fmt.Errorf("refused: only tmux moves run in the console")}
			}
		}
	}
	moves, last := a.steps[:len(a.steps)-1], a.steps[len(a.steps)-1]
	if r := runNav(action{steps: moves}, m.opt.Run); r.err != nil {
		return func() tea.Msg { return r }
	}
	return tea.ExecProcess(exec.Command(last[0], last[1:]...), func(err error) tea.Msg { //nolint:gosec // argv checked by remote.NavAllowed
		return ranMsg{cmd: strings.Join(last, " "), err: err, jumped: a.pane}
	})
}

func clampMove(i, d, n int) int { return min(max(i+d, 0), max(n-1, 0)) }

func (m *Model) move(d int) {
	if n := m.rowCount(); n > 0 {
		m.selectRow(clampMove(m.rowIndex(), d, n))
	}
}

// selIndex is the selected session: the one picked, followed by its PID
// across re-sorts; before one is picked, the first that waits.
func (m Model) selIndex() int {
	if !m.chose {
		if i := m.firstWaiting(); i >= 0 {
			return i
		}
	}
	for i, s := range m.snap.Sessions {
		if s.PID == m.selPID {
			return i
		}
	}
	return 0
}

func (m Model) selected() (sample.Session, bool) {
	if len(m.snap.Sessions) == 0 {
		return sample.Session{}, false
	}
	return m.snap.Sessions[m.selIndex()], true
}

// keepSelection follows the selected session by PID across re-sorts.
func (m *Model) keepSelection() {
	if _, ok := m.selected(); ok {
		m.selPID = m.snap.Sessions[m.selIndex()].PID
	}
}

// toastMsg ends the toast it counts.
type toastMsg struct{ n int }

// toastFor is how long a toast stays on the header.
const toastFor = 2500 * time.Millisecond

// say shows what an action did on the header's right, for toastFor.
func (m *Model) say(text string) tea.Cmd {
	m.toasts++
	m.toast = text
	n := m.toasts
	return tea.Tick(toastFor, func(time.Time) tea.Msg { return toastMsg{n} })
}

func (m Model) View() tea.View {
	v := tea.NewView(m.render())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion // a tap on a phone selects and acts
	return v
}

// nextAfter selects, after a jump to the selected session's pane, the
// next session that waits, else the next idle one. The way back to the
// console lands on it.
func (m Model) nextAfter(pane string) Model {
	if s, ok := m.selectedSession(); !ok || s.Pane != pane {
		return m
	}
	ss := m.snap.Sessions
	at := m.selIndex()
	for _, want := range []func(sample.Session) bool{
		func(s sample.Session) bool { return m.groupOf(s) == groupWaits },
		func(s sample.Session) bool { return s.Age() == "idle" },
	} {
		for k := 1; k < len(ss); k++ {
			if s := ss[(at+k)%len(ss)]; want(s) {
				m.chose, m.selPID = true, s.PID
				return m
			}
		}
	}
	return m
}
