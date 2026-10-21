package app

import (
	"fmt"
	"os/exec"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/blox-eng/matchblox/internal/doors"
	"github.com/blox-eng/matchblox/internal/proto"
	"github.com/blox-eng/matchblox/internal/remote"
)

// openDoors are the doors the console shows: done and closed ones fold.
func openDoors(ds []doors.Door) []doors.Door {
	var out []doors.Door
	for _, d := range ds {
		if d.Open() {
			out = append(out, d)
		}
	}
	return out
}

// shownDoors are the open doors this console can use. The pair door needs
// the host's own console: a remote console has no PairHost.
func (m Model) shownDoors(ds []doors.Door) []doors.Door {
	out := openDoors(ds)
	if m.opt.Host != "" || m.opt.Pair == nil {
		out = slices.DeleteFunc(out, func(d doors.Door) bool { return d.ID == doors.Pair })
	}
	return out
}

// openPairDoor is Enter or a tap on the pair door: the page owns the
// screen and its keys, so nothing else stays open under it.
func (m Model) openPairDoor() (Model, tea.Cmd) {
	if m.fromDevice() {
		m.flash = pairFromDeviceLine
		return m, nil
	}
	m.pending, m.peek, m.sheet, m.form, m.held, m.searching = nil, nil, nil, nil, nil, false
	m.armed = ""
	return m.openPairPage()
}

// doorAction is what Enter does on a door: write its file, or run its
// command in this terminal. Both wait for a typed y.
func (m Model) doorAction(d doors.Door) *action {
	if d.Problem != "" {
		return nil
	}
	if d.ID == doors.Night {
		return nightDoorAction(d)
	}
	if d.ID == doors.Pair {
		return &action{label: d.Title, say: d.Title + ": show a one-time code", door: d.ID, pair: true}
	}
	if d.RunsTerm() {
		argv, where := d.Term, "in this terminal"
		if d.ID == doors.Guide && !m.opt.OutsideTmux {
			argv, where = doors.GuideArgv(true), "in a new tmux window"
		}
		return &action{label: d.Title, say: d.Title + ": " + where, steps: [][]string{argv}, destructive: true, term: true, door: d.ID}
	}
	if d.Sum == "" {
		return nil
	}
	return &action{label: d.Title, say: d.Title + ": write " + tilde(d.Path), destructive: true,
		rec: "door:" + d.ID, which: "primary", text: d.Sum, door: d.ID}
}

func closeDoor(d doors.Door) *action {
	return &action{label: "close", say: "close " + d.Title + " (matchblox setup opens it again)", destructive: true,
		rec: "door:" + d.ID, which: "secondary", door: d.ID}
}

// doorRanMsg: a door's command gave the terminal back.
type doorRanMsg struct {
	door, cmd string
	err       error
}

// runDoor gives the terminal to a door's command: the password prompt of
// a package manager, or an agent, belongs to the person.
func (m Model) runDoor(a action) (tea.Model, tea.Cmd) {
	argv := a.steps[0]
	if !doors.TermAllowed(argv) {
		m.flash = "refused: a door runs only the tmux install or the guide"
		return m, nil
	}
	cmd := strings.Join(argv, " ")
	m.flash = "running: " + cmd
	return m, tea.ExecProcess(exec.Command(argv[0], argv[1:]...), func(err error) tea.Msg { //nolint:gosec // argv checked by TermAllowed
		return doorRanMsg{door: a.door, cmd: cmd, err: err}
	})
}

func (m Model) doorRan(msg doorRanMsg) (tea.Model, tea.Cmd) {
	switch {
	case msg.err != nil:
		m.flash = fmt.Sprintf("failed: %s: %v", msg.cmd, msg.err)
		return m, nil
	case msg.door == doors.Tmux:
		m.flash = "tmux is installed: q, then matchblox, opens the console in tmux"
	case msg.door == doors.Guide:
		m.flash = "the guide runs in its window"
		if m.conn != nil && !m.lost {
			// The guide is opened once; matchblox setup offers it again.
			return m, m.send(proto.Act{RecID: "door:" + doors.Guide, Which: "secondary", Confirm: "y"})
		}
	}
	return m, nil
}

// doorsSection is the doors above the sessions: one row each, and under
// the door that waits for its y, the exact diff or command.
func (m Model) doorsSection(b *body, w int) {
	if len(m.doors) == 0 {
		return
	}
	st := m.st
	sel := m.sessIndex()
	b.add(-1, st.label.Render(fit(fmt.Sprintf(" SET UP · %d", len(m.doors)), w)))
	narrow := m.layoutAt(w) == Narrow
	for i, d := range m.doors {
		mark := "  "
		if i == sel {
			mark = "▌ "
		}
		if narrow {
			// A phone: one line for each door, the why of the selected one,
			// so the sessions stay on the first screen.
			b.addRow(i, i == sel, w, st, " "+st.accent.Render(mark)+st.ink(i == sel).Render(d.Title))
			if i != sel {
				continue
			}
			for _, l := range wrap(d.Why, w-2, st.muted.Render) {
				b.addRow(i, i == sel, w, st, "  "+l)
			}
			if d.Problem != "" {
				for _, l := range wrap(d.Problem, w-2, st.warn.Render) {
					b.addRow(i, i == sel, w, st, "  "+l)
				}
			}
			continue
		}
		// The why wraps beside the title: "uses your tokens" must be read
		// before the y.
		text, style := d.Why, st.muted
		if d.Problem != "" {
			text, style = d.Problem, st.warn
		}
		for j, l := range wrap(text, w-colDoor+1, plainText) {
			lead := " " + st.accent.Render(mark) + st.ink(i == sel).Render(pad(d.Title, colDoor-3))
			if j > 0 {
				lead = strings.Repeat(" ", colDoor)
			}
			b.addRow(i, i == sel, w, st, lead+style.Render(strings.TrimPrefix(l, " ")))
		}
	}
	if d, ok := m.previewDoor(); ok {
		m.preview(b, d, w)
	}
}

// previewDoor is the door whose diff waits for a y.
func (m Model) previewDoor() (doors.Door, bool) {
	if m.pending == nil || m.pending.door == "" || m.pending.which == "secondary" {
		return doors.Door{}, false
	}
	if d, ok := m.selectedDoor(); ok && d.ID == m.pending.door {
		return d, true
	}
	return doors.Door{}, false
}

func (m Model) previewLines(d doors.Door) []string {
	if d.RunsTerm() && m.pending != nil {
		return []string{shellLine(m.pending.steps[0])} // the exact argv this console runs
	}
	if m.alsoOn(d) {
		return strings.Split(d.Also.Preview, "\n")
	}
	return strings.Split(d.Preview, "\n")
}

// alsoOn: the person turned the door's choice on with m.
func (m Model) alsoOn(d doors.Door) bool {
	return d.Also != nil && m.pending != nil && m.pending.which == "also"
}

// choiceLines is the door's choice above its diff, with its key.
func (m Model) choiceLines(d doors.Door, w int) []string {
	if d.Also == nil {
		return nil
	}
	box := "[ ]"
	if m.alsoOn(d) {
		box = "[x]"
	}
	return wrap(box+" m "+d.Also.Label, w-2, plainText)
}

// previewRoom is how many lines of the diff fit under the doors.
func (m Model) previewRoom() int {
	var rows body
	mm := m
	mm.pending = nil
	w, _ := m.panes(max(m.width, minWidth))
	mm.doorsSection(&rows, w)
	choice := 0
	if d, ok := m.previewDoor(); ok {
		choice = len(m.choiceLines(d, w))
	}
	// The header, the tabs and a hairline above; a hairline and the key
	// line under.
	return max(3, m.height-5-len(rows.lines)-3-choice)
}

// previewSeen: the last line of the diff was on the screen. A door opens
// only on what the person saw, so y waits until then.
func (m Model) previewSeen() bool {
	d, ok := m.previewDoor()
	return !ok || m.previewTop+m.previewRoom() >= len(m.previewLines(d))
}

// scrollPreview moves a long diff with ↑↓; other keys go on to the confirm.
func (m Model) scrollPreview(k string) (Model, bool) {
	d, ok := m.previewDoor()
	if !ok {
		return m, false
	}
	last := max(0, len(m.previewLines(d))-m.previewRoom())
	switch k {
	case "down", "j":
		m.previewTop = min(m.previewTop+1, last)
	case "up", "k":
		m.previewTop = max(m.previewTop-1, 0)
	case "y":
		return m, !m.previewSeen() // y waits for the end of the diff
	case "m":
		if d.Also == nil {
			return m, false
		}
		a := *m.pending
		a.which, a.say = "also", d.Title+": write "+tilde(d.Path)+", mouse on"
		if m.pending.which == "also" {
			a.which, a.say = "primary", d.Title+": write "+tilde(d.Path)
		}
		m.pending, m.previewTop = &a, 0
	default:
		return m, false
	}
	return m, true
}

// colDoor is the title column of a door row.
const colDoor = 26

// preview draws a door's diff: new lines in the accent, removed ones in
// neg. What does not fit says where to see it all.
func (m Model) preview(b *body, d doors.Door, w int) {
	st := m.st
	if d.Path != "" {
		b.add(-1, fit(" "+st.muted.Render(tilde(d.Path)), w))
	}
	for _, l := range m.choiceLines(d, w) {
		b.add(-1, fit(" "+st.text.Render(l), w))
	}
	lines := m.previewLines(d)
	top := min(m.previewTop, max(0, len(lines)-1))
	end := min(top+m.previewRoom(), len(lines))
	cut := len(lines) - end
	lines = lines[top:end]
	for _, l := range lines {
		style := st.faint
		switch {
		case strings.HasPrefix(l, "+"):
			style = st.accent
		case strings.HasPrefix(l, "-"):
			style = st.neg
		case d.Path == "":
			style = st.text // a command
		}
		b.add(-1, fit(" "+style.Render(tilde(l)), w))
	}
	if cut > 0 {
		b.add(-1, fit(" "+st.faint.Render(fmt.Sprintf("… %d more lines: ↓ the rest of the diff, then y", cut)), w))
	}
}

func plainText(s ...string) string { return strings.Join(s, "") }

// shellLine is argv as the person would type it.
func shellLine(argv []string) string { return remote.Line(argv) }
