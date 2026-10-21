package app

import (
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/blox-eng/matchblox/internal/remote"
)

// The layers of the console, as in k9s: Hosts above the console of one
// host (design/0005-remote-mode.md §1).
type layer int

const (
	layerHosts layer = iota
	layerConsole
)

// ShellOptions are what the layers need from the machine. The shell never
// reads files itself: main gives it these.
type ShellOptions struct {
	// Base holds what each console shares: the version, the clock, motion.
	Base Options
	// Hosts are the hosts the builder added (~/.config/matchblox/hosts).
	Hosts func() []string
	// SSHHosts are the hosts of ~/.ssh/config, to add one.
	SSHHosts func() []string
	// Open dials a host, "" for this machine, and returns its console's
	// options: the connection, the redial, the key.
	Open func(host string) Options
	// Remove takes a host out of the list.
	Remove func(host string) error
	// Exec gives this terminal to argv until it exits. Nil: tea.ExecProcess.
	Exec func(argv []string, done func(error) tea.Msg) tea.Cmd
	// Start is the host the shell opens first; "" decides by the list.
	Start string
	// FirstRun asks where the agents run before anything else; Answered
	// records that the builder answered.
	FirstRun bool
	Answered func()
}

// Shell is the top of the console: the hosts layer, or one host's console.
type Shell struct {
	opt     ShellOptions
	layer   layer
	cur     Model
	curHost string
	hosts   []string
	sel     int
	adding  bool    // the rows are hosts to add
	input   *string // a host being typed
	pending *action // connect or remove, waiting for its y
	flash   string
	width   int
	height  int
	dark    bool
	st      styles
	gen     int // the generation of the last console opened
}

// layerMsg asks the shell to go back to Hosts.
type layerMsg struct{}

// connectedHostMsg: `matchblox connect` gave the terminal back.
type connectedHostMsg struct {
	host string
	err  error
}

func NewShell(opt ShellOptions) Shell {
	s := Shell{opt: opt, width: 100, height: 30, dark: true, st: newStyles(true)}
	s.hosts = s.readHosts()
	switch {
	case opt.Start != "":
		s, _ = s.open(opt.Start, true)
	case len(s.hosts) == 0 && !opt.FirstRun:
		s, _ = s.open("", true)
	}
	return s
}

func (s Shell) readHosts() []string {
	if s.opt.Hosts == nil {
		return nil
	}
	return s.opt.Hosts()
}

// open starts the console of a host. The start screen plays only for the
// first console of a run.
func (s Shell) open(host string, first bool) (Shell, tea.Cmd) {
	o := s.opt.Open(host)
	b := s.opt.Base
	o.Binary, o.CompactAt, o.CompactTokens, o.NoMotion, o.Now, o.Self, o.StateDir = b.Binary, b.CompactAt, b.CompactTokens, b.NoMotion, b.Now, b.Self, b.StateDir
	o.Clip = b.Clip
	if host == "" {
		o.Pair = b.Pair // the key goes in this machine's authorized_keys
	}
	if o.Run == nil {
		o.Run = b.Run
	}
	if o.Exec == nil {
		o.Exec = s.opt.Exec
	}
	s.gen++
	o.Layered, o.Gen = true, s.gen
	m := New(o)
	m.width, m.height, m.dark, m.st = s.width, s.height, s.dark, s.st
	m.splashDone = !first
	s.cur, s.curHost, s.layer = m, host, layerConsole
	return s, m.Init()
}

func (s Shell) Init() tea.Cmd {
	if s.layer == layerConsole {
		return s.cur.Init()
	}
	return tea.RequestBackgroundColor
}

func (s Shell) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		s.width, s.height = msg.Width, msg.Height
	case tea.BackgroundColorMsg:
		s.dark = msg.IsDark()
		s.st = newStyles(s.dark)
	case layerMsg:
		return s.back(), nil
	case connectedHostMsg:
		if msg.err != nil {
			s.flash = "not connected: " + msg.err.Error()
			return s, nil
		}
		s.hosts, s.adding = s.readHosts(), false
		return s.open(msg.host, false)
	}
	if s.layer == layerConsole {
		next, cmd := s.cur.Update(msg)
		s.cur = next.(Model)
		return s, cmd
	}
	switch msg := msg.(type) {
	case connMsg:
		_ = msg.conn.Close() // a closed console's late connection
	case tea.KeyPressMsg:
		return s.key(msg)
	case tea.MouseClickMsg:
		return s.tap(msg.Mouse())
	}
	return s, nil
}

// back closes the console and shows Hosts with its host selected.
func (s Shell) back() Shell {
	if s.cur.conn != nil {
		_ = s.cur.conn.Close()
	}
	s.cur = s.cur.closePairPage("")
	s.cur.quitting = true // its late messages find nothing to do
	s.layer, s.adding, s.input, s.pending, s.flash = layerHosts, false, nil, nil, ""
	s.hosts = s.readHosts()
	s.sel = 0
	if i := slices.Index(s.hosts, s.curHost); i >= 0 {
		s.sel = i + 1
	}
	return s
}

// row is one line of the hosts layer: a host to open, or what to do.
type row struct {
	label, note string
	host        string // "" with open: this machine
	open        bool   // Enter opens host
	add         bool   // Enter lists the hosts to add
	connect     bool   // Enter connects host
	typed       bool   // Enter types a host
	removable   bool
}

func (s Shell) firstRun() bool { return s.opt.FirstRun && len(s.hosts) == 0 }

func (s Shell) rows() []row {
	if s.adding {
		var rs []row
		if s.opt.SSHHosts != nil {
			for _, h := range s.opt.SSHHosts() {
				if !slices.Contains(s.hosts, h) {
					rs = append(rs, row{label: h, note: "from ~/.ssh/config", host: h, connect: true})
				}
			}
		}
		return append(rs, row{label: "type a host…", note: "a name or user@address that ssh reaches", typed: true})
	}
	rs := []row{{label: "this machine", note: "the agents here", open: true}}
	if s.firstRun() {
		return append(rs, row{label: "another machine", note: "a host you reach with ssh", add: true})
	}
	for _, h := range s.hosts {
		rs = append(rs, row{label: h, note: "over ssh", host: h, open: true, removable: true})
	}
	return append(rs, row{label: "+ add a host", note: "from ~/.ssh/config, or type one", add: true})
}

func (s Shell) key(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	key := k.String()
	if key == "ctrl+c" {
		return s, tea.Quit
	}
	if s.input != nil {
		return s.typing(k)
	}
	if s.pending != nil {
		a := *s.pending
		s.pending = nil
		if key == "y" {
			return s.confirm(a)
		}
		s.flash = "cancelled: " + a.String()
		return s, nil
	}
	s.flash = ""
	rs := s.rows()
	switch key {
	case "q":
		return s, tea.Quit
	case "up", "k":
		s.sel = max(s.sel-1, 0)
	case "down", "j":
		s.sel = min(s.sel+1, len(rs)-1)
	case "esc":
		if s.adding {
			s.adding, s.sel = false, len(s.hosts)+1
		}
	case "x":
		if r := rs[min(s.sel, len(rs)-1)]; r.removable {
			s.pending = &action{label: "remove", say: "remove " + r.host + " from the list (matchblox stays on " + r.host + ")", door: r.host, destructive: true}
		}
	case "enter":
		return s.enter(rs[min(s.sel, len(rs)-1)])
	}
	return s, nil
}

func (s Shell) enter(r row) (tea.Model, tea.Cmd) {
	if s.firstRun() && !s.adding && (r.open || r.add) {
		s.opt.FirstRun = false
		if s.opt.Answered != nil {
			s.opt.Answered()
		}
	}
	switch {
	case r.open:
		return s.open(r.host, false)
	case r.add:
		s.adding, s.sel = true, 0
	case r.typed:
		empty := ""
		s.input = &empty
	case r.connect:
		s.pending = s.connect(r.host)
	}
	return s, nil
}

// connect is `matchblox connect <host>` in this terminal: the ssh login is
// the builder's own (design/0005-remote-mode.md §3.2).
func (s Shell) connect(host string) *action {
	self := s.opt.Base.Self
	if self == "" {
		self = "matchblox"
	}
	argv := []string{self, "connect", host}
	return &action{label: "connect", say: shellLine(argv), steps: [][]string{argv}, destructive: true, connect: true, door: host}
}

func (s Shell) confirm(a action) (tea.Model, tea.Cmd) {
	host := a.door
	if a.connect {
		s.flash = "running: " + a.String()
		done := func(err error) tea.Msg { return connectedHostMsg{host, err} }
		if s.opt.Exec != nil {
			return s, s.opt.Exec(a.steps[0], done)
		}
		return s, Model{}.exec(a.steps[0], done)
	}
	if s.opt.Remove == nil {
		return s, nil
	}
	if err := s.opt.Remove(host); err != nil {
		s.flash = "not removed: " + err.Error()
		return s, nil
	}
	s.hosts = s.readHosts()
	s.sel = min(s.sel, len(s.rows())-1)
	s.flash = "removed " + host
	return s, nil
}

// typing edits a host name; Enter shows its connect.
func (s Shell) typing(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	text := *s.input
	switch k.String() {
	case "esc":
		s.input = nil
		return s, nil
	case "enter":
		s.input = nil
		if !remote.ValidHost(text) {
			s.flash = fmt.Sprintf("%q is not a host name: a name or user@address that ssh reaches", text)
			return s, nil
		}
		s.pending = s.connect(text)
		return s, nil
	case "backspace":
		text = trimLast(text)
	default:
		if k.Text != "" && len(text) < 200 {
			text += k.Text
		}
	}
	s.input = &text
	return s, nil
}

// tap selects a row; a tap on the selected row does what Enter does.
func (s Shell) tap(ms tea.Mouse) (tea.Model, tea.Cmd) {
	if ms.Button != tea.MouseLeft || s.pending != nil || s.input != nil {
		return s, nil
	}
	rs := s.rows()
	y := ms.Y - hostsTop
	i := y + s.scrollTop(len(rs))
	if y < 0 || y >= s.rowRoom() || i >= len(rs) {
		return s, nil
	}
	if i != s.sel {
		s.sel = i
		return s, nil
	}
	return s.enter(rs[i])
}

// rowRoom is how many rows fit under the label, with the hint and, at the
// bottom, a hairline and the action line.
func (s Shell) rowRoom() int { return max(3, s.height-hostsTop-4) }

// scrollTop is the first row on the screen: the list follows the
// selection, so every row is reachable.
func (s Shell) scrollTop(n int) int {
	room := s.rowRoom()
	if n <= room || s.sel < room {
		return 0
	}
	return min(s.sel-room+1, n-room)
}

// hostsTop is the first row's line: the header, a hairline and the region
// label.
const hostsTop = 3

func (s Shell) View() tea.View {
	if s.layer == layerConsole {
		return s.cur.View()
	}
	v := tea.NewView(s.render())
	v.AltScreen = true
	v.MouseMode = tea.MouseModeCellMotion
	return v
}

func (s Shell) render() string {
	if s.layer == layerConsole {
		return s.cur.render()
	}
	st, w := s.st, max(s.width, minWidth)
	out := []string{
		fit(" "+st.accent.Render("▰")+" "+st.text.Render("matchblox")+st.faint.Render(" · ")+st.muted.Render("hosts"), w),
		st.hair.Render(strings.Repeat("─", w)),
	}
	label := fmt.Sprintf(" HOSTS · %d", len(s.hosts)+1)
	switch {
	case s.adding:
		label = " ADD A HOST"
	case s.firstRun():
		label = " WHERE DO YOUR AGENTS RUN?"
	}
	out = append(out, st.label.Render(fit(label, w)))
	var b body
	rs := s.rows()
	top := s.scrollTop(len(rs))
	for i, r := range rs[top:min(len(rs), top+s.rowRoom())] {
		i += top
		mark := "  "
		if i == s.sel {
			mark = "▌ "
		}
		line := " " + st.accent.Render(mark) + st.text.Render(pad(r.label, 24)) + st.muted.Render(r.note)
		if layout(w, s.height) == Narrow {
			line = " " + st.accent.Render(mark) + st.text.Render(r.label)
		}
		b.addRow(i, i == s.sel, w, st, line)
	}
	out = append(out, b.lines...)
	if s.firstRun() && !s.adding {
		out = append(out, "", fit(" "+st.faint.Render("You can add a host later: esc in a console comes back here."), w))
	}
	// The action line is at the bottom, as in a console (design/0006 §11.1).
	for len(out) < s.height-2 {
		out = append(out, "")
	}
	out = append(out, st.hair.Render(strings.Repeat("─", w)), s.actionLine(w))
	return strings.Join(out, "\n")
}

func (s Shell) actionLine(w int) string {
	st := s.st
	switch {
	case s.input != nil:
		return fit(" "+st.label.Render("HOST ")+st.text.Render(*s.input)+st.accent.Render("▏")+"  "+st.faint.Render("⏎ connect  esc cancel"), w)
	case s.pending != nil:
		return fit(" "+st.label.Render("RUN ")+st.text.Render(s.pending.String())+"   "+st.neg.Render("y")+st.muted.Render(" run  any other key cancels"), w)
	case s.flash != "":
		return fit(" "+st.text.Render(s.flash), w)
	case s.adding:
		return fit(" "+st.muted.Render("↑↓ select  ⏎ connect  esc back  q quit"), w)
	}
	keys := "↑↓ select  ⏎ open  q quit"
	if rs := s.rows(); s.sel < len(rs) && rs[s.sel].removable {
		keys = "↑↓ select  ⏎ open  x remove  q quit"
	}
	return fit(" "+st.muted.Render(keys), w)
}

// Close ends the open console's connection and its pair code when the
// program ends.
func (s Shell) Close() {
	s.cur.ClosePair()
	if s.layer == layerConsole && s.cur.conn != nil {
		_ = s.cur.conn.Close()
	}
}
