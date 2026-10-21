// Package sample turns /proc, tmux and agent session files into Snapshots.
// A Sampler keeps the previous counters so rates (CPU, network, disk) are
// deltas between two calls; it is not safe for concurrent use.
package sample

import (
	"net"
	"path/filepath"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/blox-eng/matchblox/internal/config"
	"github.com/blox-eng/matchblox/internal/limits"
	"github.com/blox-eng/matchblox/internal/procfs"
	"github.com/blox-eng/matchblox/internal/said"
)

// Exited is an agent process that exited and waits for its parent to
// reap it.
type Exited struct {
	PID        int    `json:"pid"`
	Parent     int    `json:"parent"`
	ParentComm string `json:"parent_comm"`
}

// Session is one agent process and the pane that owns it.
type Session struct {
	PID    int    `json:"pid"`
	Start  uint64 `json:"start_ticks"` // /proc/<pid>/stat field 22: tells a reused pid apart
	Pane   string `json:"pane"`
	Target string `json:"target"`
	Tab    string `json:"tab"` // tmux window name
	Name   string `json:"name"`
	Agent  string `json:"agent"` // the command it was found by: claude, codex, opencode, ...
	// Account is a short label of the login it runs under ("a@b.c · Max",
	// "API key"), never a credential.
	Account string `json:"account,omitempty"`
	// Provider serves the model: anthropic, openai, bedrock, vertex, or an
	// OpenCode provider id. The console shows it as a mark.
	Provider   string        `json:"provider,omitempty"`
	SessionID  string        `json:"session_id"`
	Cwd        string        `json:"cwd"`
	Busy       bool          `json:"busy"`
	Status     string        `json:"status"` // as the agent reports it: busy, idle, ...
	Idle       time.Duration `json:"idle_ns"`
	Started    time.Time     `json:"started"`
	Model      string        `json:"model,omitempty"`
	Context    string        `json:"context"` // known | fresh (no turn since start or clear) | unknown (no session file)
	Tokens     int           `json:"tokens"`
	Window     int           `json:"window"`
	ContextPct float64       `json:"context_pct"`
	Burn30m    int           `json:"burn_30m_tokens"`
	CPU        float64       `json:"cpu_pct"` // whole process tree, 100 = one core
	Procs      int           `json:"procs"`
	Do         string        `json:"do,omitempty"` // compact | clear
	Why        string        `json:"why,omitempty"`
	// Progress is the bar the agent drew in its last reply with text.
	Progress *Progress `json:"progress,omitempty"`
	// LastLine and Asks are read from the pane of an agent without an
	// adapter: what it said last, and whether that asks the person.
	LastLine string `json:"last_line,omitempty"`
	Asks     bool   `json:"asks,omitempty"`
	// Permits: an approval menu, where Enter takes the selected choice.
	Permits bool `json:"permits,omitempty"`
	// Said is the current turn's newest reply with text, plain, its end
	// kept up to a bound; Ask is the question tool's call it waits on
	// (Claude Code).
	Said     string    `json:"said,omitempty"`
	Ask      *said.Ask `json:"ask,omitempty"`
	FromPane bool      `json:"from_pane,omitempty"` // busy and idle were read from the pane
	// Out: the plan's limit stops the session until here (its turn was
	// refused, or its account's window is spent); zero when it can work.
	Out time.Time `json:"out,omitzero"`
	// Failed: the last turn ended on an API error or a failed tool call,
	// and the model said nothing after it; the error's first line.
	Failed string `json:"failed,omitempty"`
	// Agents and Shells are the background work a session left running
	// when its turn ended: the subagents Claude Code counted then, and the
	// shells its Bash tool still runs.
	Agents int       `json:"agents,omitempty"`
	Shells []Shell   `json:"shells,omitempty"`
	turnAt time.Time // the newest real turn of the transcript
	// TurnEnded is when the transcript's newest turn ended; zero while it
	// runs. The service reads it, it never leaves the host.
	TurnEnded time.Time `json:"-"`
	// Transcript is the Claude Code session file, read back by the service
	// after a step; it never leaves the host.
	Transcript string `json:"-"`
}

// Shell is a shell an agent's Bash tool runs in the background.
type Shell struct {
	PID   int    `json:"pid"`
	Start uint64 `json:"start_ticks"` // tells a reused pid apart
	// Group: the shell leads its own process group, so ending the group ends
	// the command it runs too.
	Group   bool      `json:"group,omitempty"`
	Started time.Time `json:"started"`
	Command string    `json:"command"` // as the agent wrote it
}

// IdlePane is a pane that hosts no agent.
type IdlePane struct {
	Pane    string `json:"pane"`
	Target  string `json:"target"`
	Command string `json:"command"`
	Path    string `json:"path"`
}

type Machine struct {
	CPU        float64             `json:"cpu_pct"` // 0-100 over all cores
	Cores      []float64           `json:"cores_pct"`
	Load       [3]float64          `json:"load"`
	MemTotal   uint64              `json:"mem_total"`
	MemAvail   uint64              `json:"mem_available"`
	SwapUsed   uint64              `json:"swap_used"`
	PSI        procfs.PSI          `json:"psi"`
	NetRx      float64             `json:"net_rx_bps"`
	NetTx      float64             `json:"net_tx_bps"`
	DiskRead   float64             `json:"disk_read_bps"`
	DiskWrite  float64             `json:"disk_write_bps"`
	Latency    time.Duration       `json:"latency_ns"`
	LatencyErr string              `json:"latency_error,omitempty"`
	TempC      float64             `json:"temp_c"`
	Power      []procfs.PowerLimit `json:"power_limits"`
	GPUs       []GPU               `json:"gpus"`
	Disks      []Disk              `json:"disks,omitempty"`
}

type Snapshot struct {
	At        time.Time  `json:"at"`
	Machine   Machine    `json:"machine"`
	Sessions  []Session  `json:"sessions"`
	IdlePanes []IdlePane `json:"idle_panes"`
	Orphans   []Orphan   `json:"orphans"`
	Top       []ProcRow  `json:"top"`
	// Exited are agents that exited and that their parent has not reaped
	// (zombies): not sessions, listed so the person sees why they stay.
	Exited     []Exited     `json:"exited,omitempty"`
	Containers []Container  `json:"containers"`
	Groups     []GroupShare `json:"groups"`
	GitPolling []GitPolling `json:"git_polling"`
	Alerts     []Alert      `json:"alerts"`
	Errors     []string     `json:"errors,omitempty"`
	// Limits are the plan limits of each account a session runs under.
	Limits []limits.Account `json:"limits"`
	// Panes are every tmux pane, with the pid tmux started in it: the
	// service tells by them which pane a process runs in. Not sent.
	Panes []Pane `json:"-"`
}

// StatusAgents is a session whose turn ended with subagents still working:
// busy to the person, since their results start its next turn by itself.
const StatusAgents = "agents"

// TmuxSession is the name of the tmux session the pane belongs to.
func (s Session) TmuxSession() string {
	name, _, _ := strings.Cut(s.Target, ":")
	return name
}

// Resting tells if no turn runs: the session is idle, or works only through
// its subagents. The stoker acts at a rest.
func (s Session) Resting() bool {
	return s.Status == "idle" && !s.Busy || s.Status == StatusAgents
}

// Works tells if a turn runs, or its subagents do: what a kill confirms
// twice. Unlike Resting, it holds for an agent with no status of its own
// (Codex, OpenCode), whose Busy comes from its pane.
func (s Session) Works() bool { return s.Busy || s.Status == StatusAgents }

// Stopped tells if the plan's limit stops a session the builder would pick
// up now: idle (not cold or stale) and out.
func (s Session) Stopped() bool { return !s.Out.IsZero() && s.Age() == "idle" }

// Full is when a session's context should compact: at Pct of its window,
// or at Tokens, whichever comes first. A 1M window rarely reaches a
// percentage; zero Tokens is the percentage alone.
type Full struct {
	Pct    float64
	Tokens int
}

func (f Full) Reached(s Session) bool {
	return s.Tokens > 0 && (f.Pct > 0 && s.ContextPct >= f.Pct || f.Tokens > 0 && s.Tokens >= f.Tokens)
}

// String is the threshold as the console writes it: "85% or 350k".
func (f Full) String() string {
	if f.Tokens <= 0 {
		return pct(f.Pct)
	}
	return pct(f.Pct) + " or " + Kilo(f.Tokens)
}

// Kilo writes a token count in thousands: 350k, 1.2M.
func Kilo(n int) string {
	if n >= 1_000_000 {
		return strconv.FormatFloat(float64(n)/1e6, 'f', 1, 64) + "M"
	}
	return strconv.Itoa((n+500)/1000) + "k"
}

// Rules decide the per-session suggestion.
type Rules struct {
	CompactAt     float64       // context percent at which a busy session should compact
	CompactTokens int           // or this many context tokens, whichever comes first; 0: the percentage alone
	ClearIdle     time.Duration // idle this long with ClearMinPct context: clear
	ClearMinPct   float64
	ClearStale    time.Duration // idle this long: clear regardless of context
	Windows       map[string]int
}

// Full is the rules' compact threshold.
func (r Rules) Full() Full { return Full{r.CompactAt, r.CompactTokens} }

var DefaultRules = Rules{
	CompactAt:     85,
	CompactTokens: 350_000,
	ClearIdle:     30 * time.Minute,
	ClearMinPct:   20,
	ClearStale:    24 * time.Hour,
}

type Sampler struct {
	FS   procfs.Host
	Sys  procfs.Sys
	Home string
	// HomeAs is Home as the processes name it in their HOME; empty: Home.
	// A fixture tree sets it: its processes name a made-up path.
	HomeAs string
	Tmux   func() ([]byte, error)
	// Stoker is the tmux server matchblox keeps the stoker agent on: its
	// panes are listed too, each id carrying the server (OnServer), and
	// StokerTmux lists them (nil: TmuxPanesOn(Stoker)). Empty: none.
	Stoker     string
	StokerTmux func() ([]byte, error)
	// Capture returns the visible text of each pane, by pane id. It reads
	// only the panes of agents without an adapter.
	Capture       func(panes []string) map[string]string
	GPU           func() ([]byte, error) // nil: no GPUs
	Disks         func() []Disk          // nil: no disks
	Docker        func() ([]byte, error) // nil: no containers
	Cfg           config.Config
	Agents        []string // process names that are agent sessions
	Rules         Rules
	LatencyTarget string // host:port, empty disables
	// OwnEntries compares each ~/.claude/sessions/<pid>.json with its
	// process's start time (see ownEntry). On the live machine only: a
	// fixture tree has fixed uptimes and a real clock.
	OwnEntries bool
	// ProcEvery is how often the process table, tmux and sessions are
	// re-read. Reading every /proc/<pid>/stat is most of a sample's cost, so
	// it runs slower than the cheap machine counters. Zero: every sample.
	ProcEvery time.Duration
	Now       func() time.Time
	// LimitsDir is where the status-line tap keeps each Claude Code config
	// directory's limits. Empty: none are read.
	LimitsDir string

	agents   *agentReader
	panes    map[procKey]string // environ is read once per process
	names    map[procKey]agentRef
	screens  map[procKey]screen // last pane text of agents without an adapter
	prevAt   time.Time
	procAt   time.Time
	prevProc map[int]procfs.Proc
	last     Snapshot // sessions and panes from the last process scan
	prevCPU  procfs.CPUTimes
	prevCore []procfs.CPUTimes
	prevNet  [2]uint64
	prevDisk [2]uint64
	latAt    time.Time
	lat      time.Duration
	latErr   string
	burn     map[string][]tokenPoint
	burnLive map[string]bool  // burn keys measured in this scan
	longCtx  map[procKey]bool // processes started with a 1M model setting
	envs     map[procKey]agentEnv
	accounts *accounts
	opencode *opencodeReader

	owners     map[procKey]string
	hotSince   map[procKey]time.Time
	condSince  map[string]time.Time
	powerBase  map[string]float64
	gpuAt      time.Time
	gpus       []GPU
	dockerAt   time.Time
	containers []containerRef
	prevCgroup map[string]uint64
	toplevel   map[string]string
	sensors    []string            // temperature files, found once
	power      []procfs.PowerLimit // read on the scan cadence
	polls      []pollPoint
}

// procKey tells a reused PID apart from the process that had it before.
type procKey struct {
	pid   int
	start uint64
}

type tokenPoint struct {
	at     time.Time
	tokens int
}

func (s *Sampler) init() {
	if s.agents == nil {
		s.agents = newAgentReader()
		s.envs = map[procKey]agentEnv{}
		s.accounts = newAccounts()
		s.opencode = newOpencodeReader()
		s.panes = map[procKey]string{}
		s.names = map[procKey]agentRef{}
		s.burn = map[string][]tokenPoint{}
		s.burnLive = map[string]bool{}
		s.longCtx = map[procKey]bool{}
		s.owners = map[procKey]string{}
		s.toplevel = map[string]string{}
		if s.Cfg.Interval.Duration == 0 && s.Cfg.Orphans.CPUOver == 0 {
			s.Cfg = config.Default() // an unset config, not a partly set one
		}
		if s.Rules.CompactAt == 0 {
			s.Rules = DefaultRules
		}
		if s.Now == nil {
			s.Now = time.Now
		}
		if s.Tmux == nil {
			s.Tmux = TmuxPanes
		}
		if s.StokerTmux == nil && s.Stoker != "" {
			s.StokerTmux = func() ([]byte, error) { return TmuxPanesOn(s.Stoker) }
		}
		if s.Capture == nil {
			s.Capture = CapturePanes
		}
		s.screens = map[procKey]screen{}
		if len(s.Agents) == 0 {
			s.Agents = []string{"claude"}
		}
	}
}

func (s *Sampler) Sample() Snapshot {
	s.init()
	now := s.Now()
	snap := Snapshot{At: now}
	s.sampleMachine(&snap, seconds(s.prevAt, now))
	snap.Machine.GPUs = s.sampleGPUs(now)
	s.prevAt = now

	if s.procAt.IsZero() || now.Sub(s.procAt) >= s.ProcEvery || s.panesChanged() {
		s.scan(now)
	}
	l := s.last
	snap.Sessions, snap.IdlePanes, snap.Orphans, snap.Top, snap.Exited = l.Sessions, l.IdlePanes, l.Orphans, l.Top, l.Exited
	snap.Containers, snap.Groups, snap.GitPolling, snap.Limits, snap.Panes = l.Containers, l.Groups, l.GitPolling, l.Limits, l.Panes
	snap.Errors = append(snap.Errors, l.Errors...)
	s.alerts(&snap, now)
	return snap
}

// panesChanged lists tmux, which is cheap, between two scans: a pane the
// last scan did not see, or a pane that now runs an agent, needs a scan at
// once. A new session then shows within a sample, not within ProcEvery.
func (s *Sampler) panesChanged() bool {
	out, err := s.Tmux()
	if err != nil {
		return false
	}
	seen := make(map[string]string, len(s.last.Panes))
	for _, p := range s.last.Panes {
		seen[p.ID] = p.Command
	}
	for _, p := range ParsePanes(out) {
		if cmd, ok := seen[p.ID]; !ok || cmd != p.Command && s.isAgent(p.Command) {
			return true
		}
	}
	return false
}

// scan reads the process table, tmux and containers: the expensive half of a
// sample, run every ProcEvery and when the panes change.
func (s *Sampler) scan(now time.Time) {
	dt := seconds(s.procAt, now)
	var l Snapshot
	s.power = s.Sys.PowerLimits()
	procs, err := s.FS.Procs()
	if err != nil {
		l.Errors = append(l.Errors, "proc: "+err.Error())
	}
	out, err := s.Tmux()
	if err != nil {
		l.Errors = append(l.Errors, "tmux: "+err.Error())
	}
	panes := ParsePanes(out)
	all := panes
	if s.Stoker != "" {
		// No server is no error: matchblox starts it when the builder
		// launches the stoker.
		b, _ := s.StokerTmux()
		own := ParsePanes(b)
		for i := range own {
			own[i].ID = OnServer(own[i].ID, s.Stoker)
		}
		all = append(slices.Clip(panes), own...)
	}
	l.Panes = all
	s.sampleSessions(&l, all, procs, dt, now)
	l.Containers, l.Groups = s.sampleContainers(now, dt, len(s.prevCore))
	s.sampleProcs(&l, procs, panes, dt, now)
	l.GitPolling = s.samplePolling(procs, now)
	for key := range s.owners {
		if p, ok := procs[key.pid]; !ok || p.StartTime != key.start {
			delete(s.owners, key)
		}
	}
	for key := range s.names {
		if p, ok := procs[key.pid]; !ok || p.StartTime != key.start {
			delete(s.names, key)
		}
	}
	s.procAt, s.prevProc, s.last = now, procs, l
}

func seconds(prev, now time.Time) float64 {
	if prev.IsZero() || !now.After(prev) {
		return 0
	}
	return now.Sub(prev).Seconds()
}

func (s *Sampler) sampleMachine(snap *Snapshot, dt float64) {
	m := &snap.Machine
	if all, cores, err := s.FS.Stat(); err == nil {
		m.CPU = busyPct(s.prevCPU, all)
		m.Cores = make([]float64, len(cores))
		for i, c := range cores {
			if i < len(s.prevCore) {
				m.Cores[i] = busyPct(s.prevCore[i], c)
			}
		}
		s.prevCPU, s.prevCore = all, cores
	}
	m.Load, _ = s.FS.Loadavg()
	if mem, err := s.FS.Meminfo(); err == nil {
		m.MemTotal, m.MemAvail = mem.Total, mem.Available
		m.SwapUsed = mem.SwapTotal - mem.SwapFree
	}
	m.PSI = s.FS.Pressure()
	if rx, tx, err := s.FS.NetBytes(); err == nil {
		if dt > 0 {
			m.NetRx, m.NetTx = rate(s.prevNet[0], rx, dt), rate(s.prevNet[1], tx, dt)
		}
		s.prevNet = [2]uint64{rx, tx}
	}
	if r, w, err := s.FS.DiskBytes(); err == nil {
		if dt > 0 {
			m.DiskRead, m.DiskWrite = rate(s.prevDisk[0], r, dt), rate(s.prevDisk[1], w, dt)
		}
		s.prevDisk = [2]uint64{r, w}
	}
	s.sampleLatency()
	m.Latency, m.LatencyErr = s.lat, s.latErr
	if s.sensors == nil {
		s.sensors = s.Sys.TempSensors()
	}
	m.TempC = procfs.ReadTemp(s.sensors)
	m.Power = s.power
	if s.Disks != nil {
		m.Disks = s.Disks()
	}
}

// sampleLatency measures a TCP connect at most every 10 s: no root and no
// ICMP needed, and it reflects what a git push or an API call will feel.
func (s *Sampler) sampleLatency() {
	if s.LatencyTarget == "" || s.Now().Sub(s.latAt) < 10*time.Second {
		return
	}
	s.latAt = s.Now()
	start := time.Now()
	c, err := net.DialTimeout("tcp", s.LatencyTarget, 2*time.Second)
	if err != nil {
		s.lat, s.latErr = 0, err.Error()
		return
	}
	_ = c.Close()
	s.lat, s.latErr = time.Since(start), ""
}

func busyPct(prev, cur procfs.CPUTimes) float64 {
	if prev.Total == 0 || cur.Total <= prev.Total {
		return 0
	}
	return 100 * float64(cur.Busy-prev.Busy) / float64(cur.Total-prev.Total)
}

func rate(prev, cur uint64, dt float64) float64 {
	if prev == 0 || cur < prev {
		return 0
	}
	return float64(cur-prev) / dt
}

func (s *Sampler) sampleSessions(snap *Snapshot, panes []Pane, procs map[int]procfs.Proc, dt float64, now time.Time) {
	children := map[int][]int{}
	for pid, p := range procs {
		children[p.PPID] = append(children[p.PPID], pid)
	}
	byID := map[string]Pane{}
	byPID := map[int]Pane{}
	for _, p := range panes {
		byID[p.ID] = p
		byPID[p.PID] = p
	}

	hosting := map[string]bool{}
	alive := map[procKey]bool{}
	var fromPane []int // sessions without an adapter: read from their pane
	// The socket and pid of the builder's tmux server and of the
	// stoker's, as listed: an agent's TMUX names one of them.
	var server, stokers Pane
	for _, p := range panes {
		switch sv, _ := SplitPane(p.ID); {
		case sv == "" && server.ID == "":
			server = p
		case sv != "" && stokers.ID == "":
			stokers = p
		}
	}
	if stokers.Socket == "" && s.Stoker != "" {
		stokers.Socket = SocketPath(s.Stoker)
	}
	uptime := s.FS.Uptime()
	lims := limitSources{}
	// OpenCode does not say which session a process shows: two in one
	// directory cannot be told apart.
	opencodeIn := map[string]int{}
	for pid, p := range procs {
		// An `opencode run` that another agent started counts too: it
		// writes a session in the same directory.
		if s.agentName(pid, p) == "opencode" && p.State != 'Z' && p.State != 'X' {
			if pp, ok := procs[p.PPID]; !ok || s.agentName(p.PPID, pp) != "opencode" {
				opencodeIn[s.FS.Cwd(pid)]++
			}
		}
	}
	for pid, p := range procs {
		agent := s.agentName(pid, p)
		if agent == "" {
			continue
		}
		if p.State == 'Z' || p.State == 'X' {
			snap.Exited = append(snap.Exited, Exited{PID: pid, Parent: p.PPID, ParentComm: procs[p.PPID].Comm})
			continue
		}
		// An agent started anywhere below another agent (helpers, a nested
		// `claude -p` in its shell) inherits its pane: count only the top one.
		if s.underAgent(p, procs) {
			continue
		}
		key := procKey{pid, p.StartTime}
		alive[key] = true
		paneID, ok := s.panes[key]
		if !ok {
			paneID = s.ownerPane(pid, key, procs, byPID, server, stokers)
			if paneID != "" { // a server listed late still finds it
				s.panes[key] = paneID
			}
		}
		pane := byID[paneID]
		hosting[paneID] = true

		cpu, n := s.treeCPU(pid, procs, children, dt)
		sess := Session{
			PID: pid, Start: p.StartTime, Pane: paneID, Target: pane.Target, Tab: pane.Window, Cwd: s.FS.Cwd(pid),
			Agent: agent, CPU: cpu, Procs: n, Context: "unknown",
		}
		env := s.envOf(pid, key)
		switch agent {
		case "claude":
			sess.Account, sess.Provider = s.accounts.claudeAccount(env), claudeProvider(env)
			s.claudeSession(&sess, pid, p, key, env, uptime, now)
			if sess.Resting() {
				sess.Shells = s.backgroundShells(pid, procs, children, uptime, now)
			}
			if env.cloud == "" && env.claudeDir != "" {
				var r *limits.Reading
				if s.LimitsDir != "" {
					r, _ = limits.ReadTap(s.LimitsDir, s.asProcess(env.claudeDir))
				}
				lims.add(sess, limits.SourceClaude, r)
				// A turn after the reading says this session still works:
				// extra usage, or a reading the reset has not reached.
				if out := r.Out(now); out.After(sess.Out) && !sess.turnAt.After(r.At) {
					sess.Out = out
				}
				if r == nil && env.claudeDir != filepath.Join(s.Home, ".claude") {
					// The door edits the default settings: name the one this
					// session reads.
					lims.fix(sess, "run CLAUDE_CONFIG_DIR="+s.asProcess(env.claudeDir)+" matchblox setup")
				}
			}
		case "codex":
			sess.Account, sess.Provider = s.accounts.codexAccount(env), "openai"
			path, read := s.codexRollout(pid, children)
			sess.Context = "unmeasured"
			if read {
				sess.Context = "fresh"
			}
			var lim *limits.Reading
			if path != "" {
				var u Usage
				var found codexFound
				switch u, lim, found = s.agents.codexUsage(path); found {
				case codexMeasured:
					s.measured(&sess, u, "codex:"+path, now)
				case codexNotInTail:
					sess.Context = "unmeasured"
				}
			}
			lims.add(sess, limits.SourceCodex, lim)
		case "opencode":
			sess.Context = "unmeasured"
			if opencodeIn[sess.Cwd] != 1 {
				break
			}
			started := now.Add(-time.Duration((uptime - float64(p.StartTime)/procfs.ClockTicks) * float64(time.Second)))
			data := filepath.Join(env.dataHome, "opencode")
			if env.dataHome == "" {
				break
			}
			switch oc, n := s.opencode.session(data, sess.Cwd, started.UnixMilli()); n {
			case 0:
				sess.Context = "fresh" // its first session is written at the first prompt
			case 1:
				sess.Context = "fresh"
				if u, provider, ok := s.opencode.usage(data, env.cacheHome, oc.ID); ok {
					sess.Account, sess.Provider = s.accounts.opencodeAccount(env, provider), label(provider)
					s.measured(&sess, u, "opencode:"+oc.ID, now)
					lims.add(sess, limits.SourceOpenCode, nil)
				}
			}
		default:
			sess.Context = "unmeasured"
		}
		if sess.Name == "" {
			sess.Name = agent
		}
		if sess.SessionID == "" && paneID != "" {
			fromPane = append(fromPane, len(snap.Sessions))
		}
		sess.Do, sess.Why = s.Rules.suggest(sess)
		snap.Sessions = append(snap.Sessions, sess)
	}
	s.readPanes(snap.Sessions, fromPane, now)
	snap.Limits = lims.build(now, s.Cfg.Limits.Quiet)
	for key := range s.envs {
		if !alive[key] {
			delete(s.envs, key)
		}
	}
	for key := range s.panes {
		if !alive[key] {
			delete(s.panes, key)
		}
	}
	for key := range s.longCtx {
		if !alive[key] {
			delete(s.longCtx, key)
		}
	}
	for key := range s.screens {
		if !alive[key] {
			delete(s.screens, key)
		}
	}
	seen := map[string]bool{}
	for _, x := range snap.Sessions {
		seen[x.SessionID] = true
	}
	for id := range s.burn {
		if !s.burnLive[id] {
			delete(s.burn, id)
		}
	}
	clear(s.burnLive)
	for _, p := range panes {
		// The stoker's server is matchblox's own: none of its panes is
		// the builder's to use.
		if sv, _ := SplitPane(p.ID); !hosting[p.ID] && sv == "" {
			snap.IdlePanes = append(snap.IdlePanes, IdlePane{p.ID, p.Target, p.Command, p.Path})
		}
	}
	sort.Slice(snap.Exited, func(i, j int) bool { return snap.Exited[i].PID < snap.Exited[j].PID })
	sort.Slice(snap.Sessions, func(i, j int) bool {
		a, b := snap.Sessions[i], snap.Sessions[j]
		if a.ContextPct != b.ContextPct {
			return a.ContextPct > b.ContextPct
		}
		return a.Pane < b.Pane
	})
}

// agentName is the configured agent a process runs, or "". An agent looks
// different in /proc depending on how it was installed: a native binary or
// a Python entry point has the agent's process name; a node agent started
// by a wrapper keeps the name in argv[0] while its process name is node's
// "MainThread"; an npm script runs as `node <path>/<agent>`. Cursor's
// `agent` launcher is node with the launcher's path in argv[0] and Cursor's
// index.js after it. The argv half is read once per process and kept while
// its process name stays the same.
func (s *Sampler) agentName(pid int, p procfs.Proc) string {
	if s.isAgent(p.Comm) {
		return p.Comm
	}
	key := procKey{pid, p.StartTime}
	if c, ok := s.names[key]; ok && c.comm == p.Comm {
		return c.name
	}
	name := ""
	if argv := s.FS.Argv(pid); len(argv) > 0 {
		cmd := filepath.Base(argv[0])
		if interpreter(cmd) {
			cmd = ""
			for _, a := range argv[1:] {
				if !strings.HasPrefix(a, "-") {
					cmd = filepath.Base(a)
					break
				}
			}
		}
		switch {
		case s.isAgent(cmd):
			name = cmd
		case s.isAgent(CursorAgent) && cursorLauncher(argv):
			name = CursorAgent
		}
		if name == CursorAgent && cursorPrint(argv) {
			name = ""
		}
	}
	s.names[key] = agentRef{p.Comm, name}
	return name
}

type agentRef struct{ comm, name string }

// CursorAgent is Cursor's CLI, by the command its installer adds beside
// `agent`.
const CursorAgent = "cursor-agent"

// cursorPrint: argv is a print run of Cursor's CLI (-p), a script's, with
// no one at it: no session.
func cursorPrint(argv []string) bool {
	for _, a := range argv[1:] {
		if a == "-p" || a == "--print" || strings.HasPrefix(a, "--print=") {
			return true
		}
	}
	return false
}

// cursorLauncher: argv runs Cursor's CLI, whatever its argv[0]. Its
// worker-server runs the same script and is no session: it outlives the
// `agent -p` that started it.
func cursorLauncher(argv []string) bool {
	cursor := false
	for _, a := range argv[1:] {
		if a == "worker-server" {
			return false
		}
		if strings.HasSuffix(a, "/index.js") && strings.Contains(a, "/cursor-agent/versions/") {
			cursor = true
		}
	}
	return cursor
}

// interpreter tells a program that runs an agent's script: its argv[0] is
// the interpreter and the agent is the first argument that is not a flag.
func interpreter(cmd string) bool {
	switch cmd {
	case "node", "nodejs", "bun", "deno", "ruby", "perl":
		return true
	}
	return strings.HasPrefix(cmd, "python")
}

// screen is what an agent's pane showed, and since when.
type screen struct {
	text    string
	changed time.Time
}

// readPanes fills the sessions that have no adapter from their panes: busy
// while the pane changes (every agent animates while it works), idle since
// the last change, and what the agent said last. A pane seen for the first
// time has no state yet.
func (s *Sampler) readPanes(sessions []Session, idx []int, now time.Time) {
	if len(idx) == 0 {
		return
	}
	ids := make([]string, len(idx))
	for i, j := range idx {
		ids[i] = sessions[j].Pane
	}
	text := s.Capture(ids)
	for _, j := range idx {
		sess := &sessions[j]
		t, ok := text[sess.Pane]
		if !ok {
			continue
		}
		key := procKey{sess.PID, sess.Start}
		prev, seen := s.screens[key]
		switch {
		case !seen:
			s.screens[key] = screen{t, now}
		case t != prev.text:
			s.screens[key] = screen{t, now}
			sess.Busy, sess.Status = true, "busy"
		default:
			sess.Status, sess.Idle = "idle", now.Sub(prev.changed).Truncate(time.Second)
		}
		sess.FromPane = true
		var kind said.Kind
		sess.LastLine, kind = said.Screen(t)
		sess.Asks, sess.Permits = kind == said.Asks, kind == said.Permits
	}
}

func (s *Sampler) isAgent(name string) bool {
	for _, a := range s.Agents {
		if name == a {
			return true
		}
	}
	return false
}

// ownerPane prefers the pane the agent recorded in its session file, then
// TMUX_PANE from its environment (both survive re-parenting), and falls back
// to walking up to a pane's shell. Pane ids repeat across tmux servers: an
// agent whose TMUX names another server takes only a pane it descends from,
// and one of the stoker's server (stokers) takes its pane there, with the
// server in its id.
func (s *Sampler) ownerPane(pid int, key procKey, procs map[int]procfs.Proc, byPID map[int]Pane, server, stokers Pane) string {
	ours, on := true, ""
	// TMUX is "<socket>,<server pid>,<session>": an agent can outlive its
	// server, and a new server can take the same socket path.
	if v, ok := s.FS.Environ(pid, "TMUX"); ok {
		sock, rest, _ := strings.Cut(v, ",")
		spid, _, _ := strings.Cut(rest, ",")
		switch {
		case stokers.Socket != "" && sock == stokers.Socket:
			ours = stokers.Server == "" || spid == stokers.Server
			on = s.Stoker
		case server.Socket != "":
			ours = sock == server.Socket && (server.Server == "" || spid == server.Server)
		}
	}
	if dir := s.envOf(pid, key).claudeDir; dir != "" && ours {
		if e, ok := s.agents.entry(dir, pid); ok && e.Tmux != "" {
			if i := strings.LastIndexByte(e.Tmux, '.'); i >= 0 && strings.HasPrefix(e.Tmux[i+1:], "%") {
				return OnServer(e.Tmux[i+1:], on)
			}
		}
	}
	if v, ok := s.FS.Environ(pid, "TMUX_PANE"); ok && ours {
		return OnServer(v, on)
	}
	for cur, hops := pid, 0; cur > 1 && hops < 64; hops++ {
		if p, ok := byPID[cur]; ok {
			return p.ID
		}
		cur = procs[cur].PPID
	}
	return ""
}

func (s *Sampler) treeCPU(root int, procs map[int]procfs.Proc, children map[int][]int, dt float64) (float64, int) {
	var ticks uint64
	n := 0
	stack := []int{root}
	for len(stack) > 0 {
		pid := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		p := procs[pid]
		n++
		if prev, ok := s.prevProc[pid]; ok && prev.StartTime == p.StartTime && p.Ticks >= prev.Ticks {
			ticks += p.Ticks - prev.Ticks
		}
		stack = append(stack, children[pid]...)
	}
	if dt == 0 {
		return 0, n
	}
	return 100 * float64(ticks) / procfs.ClockTicks / dt, n
}

func (s *Sampler) homeAs() string {
	if s.HomeAs != "" {
		return s.HomeAs
	}
	return s.Home
}

// backgroundShells are the shells Claude Code's Bash tool started under the
// agent and still runs: its children that source a shell snapshot.
func (s *Sampler) backgroundShells(pid int, procs map[int]procfs.Proc, children map[int][]int, uptime float64, now time.Time) []Shell {
	var out []Shell
	for _, c := range children[pid] {
		cmd := s.FS.Cmdline(c)
		if !strings.Contains(cmd, "/shell-snapshots/snapshot-") {
			continue
		}
		sh := Shell{PID: c, Start: procs[c].StartTime, Group: procs[c].PGRP == c, Command: shellCommand(cmd)}
		if uptime > 0 {
			sh.Started = now.Add(-time.Duration((uptime - float64(procs[c].StartTime)/procfs.ClockTicks) * float64(time.Second)))
		}
		out = append(out, sh)
	}
	slices.SortFunc(out, func(a, b Shell) int { return a.Started.Compare(b.Started) })
	return out
}

// shellCommand is the command inside the Bash tool's wrapper:
// `source <snapshot> ... && eval '<command>' < /dev/null && pwd -P >| <file>`.
func shellCommand(cmdline string) string {
	_, body, ok := strings.Cut(cmdline, "eval '")
	if !ok {
		return cmdline
	}
	for _, tail := range []string{"' < /dev/null", "' && pwd -P"} {
		if i := strings.LastIndex(body, tail); i >= 0 {
			body = body[:i]
			break
		}
	}
	return strings.ReplaceAll(body, `'"'"'`, "'")
}

// claudeSession reads the session file Claude Code keeps for the process
// and the end of its transcript, in the process's config directory.
func (s *Sampler) claudeSession(sess *Session, pid int, p procfs.Proc, key procKey, env agentEnv, uptime float64, now time.Time) {
	if env.claudeDir == "" {
		return
	}
	e, ok := s.agents.entry(env.claudeDir, pid)
	if !ok || !s.ownEntry(e, p, uptime, now) {
		return
	}
	sess.Name, sess.SessionID = e.Name, e.SessionID
	sess.Busy, sess.Status = e.Status == "busy", e.Status
	if e.StartedAt > 0 {
		sess.Started = time.UnixMilli(e.StartedAt)
	}
	if !sess.Busy && e.StatusUpdatedAt > 0 {
		sess.Idle = now.Sub(time.UnixMilli(e.StatusUpdatedAt)).Truncate(time.Second)
	}
	sess.Context = "fresh"
	u, ok := s.agents.usageOf(env.claudeDir, e.SessionID, e.Cwd)
	if !ok {
		return
	}
	sess.Progress, sess.LastLine, sess.Asks, sess.turnAt = u.Progress, u.LastLine, u.Asks, u.At
	sess.Said, sess.Ask, sess.TurnEnded, sess.Failed = u.Said, u.Ask, u.Ended, u.Failed
	if !u.Ended.IsZero() && sess.Busy {
		// Claude Code's registry says busy while background work runs; the
		// transcript says whether a turn does. A subagent still working
		// starts the next turn by itself: the session works through it. A
		// shell does not. An idle registry says no work runs, whatever the
		// turn's end counted then.
		sess.Agents = u.Background
		sess.Status = StatusAgents
		if u.Background == 0 {
			sess.Busy, sess.Status = false, "idle"
		}
		sess.Idle = now.Sub(u.Ended).Truncate(time.Second)
	}
	if u.Out.After(now) {
		sess.Out = u.Out
	}
	sess.Transcript = s.agents.transcript(env.claudeDir, e.SessionID, e.Cwd)
	if s.LimitsDir != "" {
		u.Window = limits.SessionWindow(s.LimitsDir, e.SessionID)
	}
	if u.Window == 0 {
		long, ok := s.longCtx[key]
		if !ok {
			get := func(k string) (string, bool) { return s.FS.Environ(pid, k) }
			long = longContext(modelSetting(s.FS.Cmdline(pid), get, e.Cwd, env.claudeDir))
			s.longCtx[key] = long
		}
		if long {
			u.Window = 1_000_000
		}
	}
	s.measured(sess, u, e.SessionID, now)
}

// measured sets the context use against the window of the session (see
// window). Without a window it is not measured, never a guess.
func (s *Sampler) measured(sess *Session, u Usage, burnKey string, now time.Time) {
	sess.Model, sess.Tokens, sess.Window = label(u.Model), u.Tokens, s.window(u)
	if sess.Window <= 0 {
		sess.Context = "unmeasured"
		return
	}
	sess.Context = "known"
	sess.ContextPct = 100 * float64(u.Tokens) / float64(sess.Window)
	sess.Burn30m = s.trackBurn(burnKey, now, u.Tokens)
	s.burnLive[burnKey] = true
}

func (s *Sampler) underAgent(p procfs.Proc, procs map[int]procfs.Proc) bool {
	for cur, hops := p.PPID, 0; cur > 1 && hops < 64; hops++ {
		pp, ok := procs[cur]
		if !ok {
			return false
		}
		if s.agentName(cur, pp) != "" {
			return true
		}
		cur = pp.PPID
	}
	return false
}

// trackBurn returns how many tokens the session's context grew in the last
// 30 minutes, keeping at most one point a minute.
func (s *Sampler) trackBurn(id string, now time.Time, tokens int) int {
	pts := s.burn[id]
	if n := len(pts); n > 0 && now.Sub(pts[n-1].at) < time.Minute {
		pts[n-1] = tokenPoint{now, tokens}
	} else {
		pts = append(pts, tokenPoint{now, tokens})
	}
	for len(pts) > 1 && now.Sub(pts[1].at) >= 30*time.Minute {
		pts = pts[1:]
	}
	s.burn[id] = pts
	return max(tokens-pts[0].tokens, 0)
}

// suggest: a session left idle while holding context should be cleared;
// one near its window should compact before its next turn hits the limit,
// unless the plan's limit stops it (a compact needs the model).
func (r Rules) suggest(s Session) (do, why string) {
	switch {
	case s.Tokens == 0:
		return "", ""
	case !s.Busy && s.Idle >= r.ClearStale:
		return "clear", "idle " + Human(s.Idle) + " (clear after " + Human(r.ClearStale) + ")"
	case !s.Busy && s.Idle >= r.ClearIdle && s.ContextPct >= r.ClearMinPct:
		return "clear", "idle " + Human(s.Idle) + " holding " + pct(s.ContextPct) + " context"
	case r.Full().Reached(s) && s.Out.IsZero():
		return "compact", "context " + Kilo(s.Tokens) + " · " + pct(s.ContextPct) + " (compact at " + r.Full().String() + ")"
	}
	return "", ""
}

func pct(f float64) string { return strconv.Itoa(int(f+0.5)) + "%" }

// Human renders a duration as its two largest units, dropping a zero
// second unit: 2h14m, 6d2h, 1d, 41m, 3s.
func Human(d time.Duration) string {
	d = d.Truncate(time.Second)
	two := func(a time.Duration, ua string, b time.Duration, ub string) string {
		out := strconv.Itoa(int(a)) + ua
		if b > 0 {
			out += strconv.Itoa(int(b)) + ub
		}
		return out
	}
	day := 24 * time.Hour
	switch {
	case d >= day:
		return two(d/day, "d", d%day/time.Hour, "h")
	case d >= time.Hour:
		return two(d/time.Hour, "h", d%time.Hour/time.Minute, "m")
	case d >= time.Minute:
		return strconv.Itoa(int(d/time.Minute)) + "m"
	}
	return strconv.Itoa(int(d/time.Second)) + "s"
}

// registrySlack is how much earlier than its process an agent may say it
// started: the agent writes its file after the process starts, and clocks
// round.
const registrySlack = 30 * time.Second

// ownEntry tells if a registry file belongs to this process. An agent that
// died without removing ~/.claude/sessions/<pid>.json leaves its status
// there, and a new process with the same pid must not take it: the console
// would show a fresh agent as stale and offer to end it.
func (s *Sampler) ownEntry(e registryEntry, p procfs.Proc, uptime float64, now time.Time) bool {
	if !s.OwnEntries || e.StartedAt == 0 || uptime == 0 {
		return true // nothing to compare: the file is the only evidence
	}
	started := now.Add(-time.Duration((uptime - float64(p.StartTime)/procfs.ClockTicks) * float64(time.Second)))
	return !time.UnixMilli(e.StartedAt).Before(started.Add(-registrySlack))
}

// How long a session sits idle before its state word ages.
const (
	ColdAfter  = 24 * time.Hour
	StaleAfter = 7 * 24 * time.Hour
)

// Age is the state word of a session: busy, or idle, cold and stale as
// its idle time grows.
func (s Session) Age() string {
	switch {
	case s.Busy:
		return "busy"
	case s.Idle >= StaleAfter:
		return "stale"
	case s.Idle >= ColdAfter:
		return "cold"
	}
	return "idle"
}

// limitSources gathers the newest reading of each account the sessions run
// under.
type limitSources map[[2]string]*limitSource

type limitSource struct {
	provider, account, source string
	r                         *limits.Reading
	fix                       string // replaces the source's fix while there is no reading
}

func (l limitSources) fix(s Session, fix string) {
	if x, ok := l[[2]string{s.Provider, s.Account}]; ok && x.r == nil {
		x.fix = fix
	}
}

// add counts a session's account. An API key has no plan limits: no line.
func (l limitSources) add(s Session, source string, r *limits.Reading) {
	if s.Account == "API key" {
		return
	}
	k := [2]string{s.Provider, s.Account}
	cur, ok := l[k]
	if !ok {
		l[k] = &limitSource{provider: s.Provider, account: s.Account, source: source, r: r}
		return
	}
	if r != nil && (cur.r == nil || r.At.After(cur.r.At)) {
		cur.r, cur.source = r, source
	}
}

func (l limitSources) build(now time.Time, q limits.Quiet) []limits.Account {
	out := make([]limits.Account, 0, len(l))
	for _, x := range l {
		a := limits.Build(x.provider, x.account, x.source, x.r, now, q)
		if a.State == limits.Unmeasured && x.fix != "" && x.account != "API key" {
			a.Fix = x.fix
		}
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Provider != out[j].Provider {
			return out[i].Provider < out[j].Provider
		}
		return out[i].Account < out[j].Account
	})
	return out
}

// asProcess names a path under Home as the agent processes name it: the
// status-line tap runs inside them.
func (s *Sampler) asProcess(path string) string {
	if s.HomeAs == "" {
		return path
	}
	if rest, ok := strings.CutPrefix(path, s.Home); ok {
		return s.HomeAs + rest
	}
	return path
}
