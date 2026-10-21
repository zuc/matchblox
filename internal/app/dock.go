package app

import (
	"fmt"
	"slices"
	"strings"

	"charm.land/lipgloss/v2"

	"github.com/blox-eng/matchblox/internal/limits"
)

// The dock is usage, the machine and the stoker, between the body and the
// key line, when the body leaves room for it and one blank row
// (design/0006 §7). It is never scrolled: when it does not fit it folds
// away, and the Machine tab has it all. Rows the body leaves free beyond
// its minimum, it grows into: usage its forecasts and resets, the machine
// its top processes, the stoker its recent steps.

// The panels of the dock, in the order u cycles them on a phone.
const (
	panelUsage = iota
	panelMachine
	panelStoker
	panels
)

var panelWords = [panels]string{"usage ›", "machine ›", "stoker ›"}

// stokerClaims: a run is in charge, or has steps the builder has not
// seen; the stoker's panel takes the dock until the builder switches.
func (m Model) stokerClaims() bool {
	return m.stoker != nil && (m.stoker.On || m.stokedShows())
}

// dockShows is the panel a phone's dock shows.
func (m Model) dockShows() int {
	if m.stokerClaims() && !m.dockSwitched {
		return panelStoker
	}
	return m.dockPanel
}

// cycleDock is u: the next panel of a phone's dock.
func (m *Model) cycleDock() {
	m.dockPanel = (m.dockShows() + 1) % panels
	m.dockSwitched = m.stokerClaims()
}

// docked is the dock drawn: its lines, whether every panel shows all it
// has, and the columns of the stoker's panel (a tap there opens its card;
// x1 0: no stoker panel).
type docked struct {
	lines      []string
	full       bool
	stx0, stx1 int
}

// panel is one panel of the dock in w columns: its fewest lines, and all
// it has to say. A cut keeps the fewest lines and never cuts usage.
func (m Model) panel(p, w int, toggle string) (least, most []string) {
	switch p {
	case panelUsage:
		return m.usageLines(w, false, toggle), m.usageLines(w, true, toggle)
	case panelMachine:
		least = m.machineLines(w, false, toggle)
		return least, append(slices.Clone(least), m.topLines(w)...)
	}
	return m.stokerLines(w, false, toggle), m.stokerLines(w, true, toggle)
}

// dock is the dock in w columns and at most rows lines (rows < 0: its
// fewest). A phone shows one panel, the one u picks; an opened phone, an
// upright drawer and a desk short of room two, usage and the machine
// (the stoker in the machine's place while it claims the dock); a wide
// desk all three.
func (m Model) dock(w, rows int) docked {
	type col struct {
		p, w   int
		toggle string
	}
	var cols []col
	switch {
	case m.phoneDock(w):
		p := m.dockShows()
		cols = []col{{p, w, panelWords[(p+1)%panels]}}
	case w >= 152 && m.layoutAt(w) == Wide:
		third := (w - 2) / 3
		cols = []col{{panelUsage, third, ""}, {panelMachine, third, ""}, {panelStoker, w - 2 - 2*third, ""}}
	default:
		split := splitAt(w, m.layoutAt(w))
		right := panelMachine
		if m.stokerClaims() {
			right = panelStoker
		}
		cols = []col{{panelUsage, split, ""}, {right, w - split - 1, ""}}
	}
	d := docked{full: true}
	var parts [][]string
	x := 0
	for i, c := range cols {
		least, most := m.panel(c.p, c.w, c.toggle)
		lines := least
		switch {
		case rows < 0:
			d.full = d.full && len(most) <= len(least)
		case len(most) <= rows:
			lines = most
		case c.p != panelUsage:
			lines = most[:max(rows, len(least))]
			d.full = false
		default:
			d.full = false
		}
		if c.p == panelStoker {
			d.stx0, d.stx1 = x, x+c.w
		}
		x += c.w
		if i < len(cols)-1 {
			x++
		}
		parts = append(parts, lines)
	}
	n := 0
	for _, p := range parts {
		n = max(n, len(p))
	}
	for i := range n {
		line := ""
		for j, c := range cols {
			cell := ""
			if i < len(parts[j]) {
				cell = parts[j][i]
			}
			if j > 0 {
				line += m.st.hair.Render("│")
			}
			line += fit(cell, c.w)
		}
		d.lines = append(d.lines, line)
	}
	return d
}

// dockToggle is where the phone dock's switch sits on its first line.
func (m Model) dockToggle(w int) (x0, x1 int) {
	word := panelWords[(m.dockShows()+1)%panels]
	return w - lipgloss.Width(word) - 1, w - 1
}

// usageLines are the limits of each account a session runs under, as
// /usage says them: each window's name, a bar (warn from 60%, neg from
// 90%), the percent and its reset; the forecast and an old reading on the
// account's last line. Short of room (a phone's dock) an account takes
// two lines, and one of another provider than Anthropic one.
func (m Model) usageLines(w int, full bool, toggle string) []string {
	st := m.st
	right := ""
	if toggle != "" {
		right = st.accent.Render(toggle) + " "
	}
	out := []string{spread(st.label.Render(" USAGE"), right, w)}
	if len(m.snap.Limits) == 0 {
		return append(out, fit(" "+st.faint.Render("no session runs under a plan"), w))
	}
	names, nameW := m.accountNames()
	now := m.now()
	tight := !full && w < 50
	for i, a := range m.snap.Limits {
		mark := providerMarks[a.Provider]
		if mark == "" {
			mark = "·"
		}
		head := " " + st.muted.Render(mark) + " " + st.text.Render(pad(names[i], nameW+1)) + " "
		indent := strings.Repeat(" ", lipgloss.Width(head))
		if a.State == limits.Unmeasured {
			out = append(out, fit(head+st.warn.Render("not measured · "+a.Fix), w))
			continue
		}
		uses := a.Uses(now)
		if len(uses) == 0 {
			out = append(out, fit(head+st.faint.Render("no window read"), w))
			continue
		}
		if tight && a.Provider != "anthropic" && len(uses) > 1 {
			line := head + st.faint.Render(uses[0].Name+" ") + m.meter(uses[0].Pct, 3, m.useStyle(uses[0].Pct)) +
				st.text.Render(fmt.Sprintf(" %.0f%%", uses[0].Pct))
			for _, u := range uses[1:] {
				line += st.faint.Render(" · "+u.Name+" ") + st.text.Render(fmt.Sprintf("%.0f%%", u.Pct))
			}
			out = append(out, fit(line, w))
			continue
		}
		if tight {
			uses = uses[:min(2, len(uses))]
		}
		var lines []string
		for j, u := range uses {
			lead := head
			if j > 0 {
				lead = indent
			}
			line := lead + st.faint.Render(pad(u.Name, 5)) + m.meter(u.Pct, 8, m.useStyle(u.Pct)) + st.text.Render(pad(fmt.Sprintf(" %.0f%%", u.Pct), 5))
			if !u.Resets.IsZero() {
				line += st.faint.Render(" · " + limits.When(u.Resets, now, ""))
			}
			lines = append(lines, line)
		}
		for _, p := range m.outlook(a) {
			last := lines[len(lines)-1]
			style := st.muted
			if p.Tone == limits.Warn {
				style = st.warn
			}
			switch more := last + st.faint.Render(" · ") + style.Render(p.Text); {
			case lipgloss.Width(more) <= w:
				lines[len(lines)-1] = more
			case !tight:
				lines = append(lines, indent+style.Render(p.Text))
			case p.Tone == limits.Warn:
				// A phone keeps the account on its lines: a warning takes the
				// place of the reset.
				cut, _, _ := strings.Cut(last, st.faint.Render(" · "))
				lines[len(lines)-1] = cut + st.faint.Render(" · ") + style.Render(p.Text)
			}
		}
		for _, l := range lines {
			out = append(out, fit(l, w))
		}
	}
	return out
}

// outlook is what an account's block ends with: the forecast of its week,
// and the time of a reading over an hour old.
func (m Model) outlook(a limits.Account) []limits.Part {
	var out []limits.Part
	for _, p := range limits.Parts(a, m.now()) {
		if p.Tone != "" {
			out = append(out, p)
		}
	}
	return out
}

func (m Model) useStyle(pct float64) lipgloss.Style {
	switch {
	case pct >= 90:
		return m.st.neg
	case pct >= 60:
		return m.st.warn
	}
	return m.st.muted
}

// providerMarks stand for the logos a terminal cannot draw: one cell each,
// none an emoji.
var providerMarks = map[string]string{"anthropic": "✻", "openai": "❋", "opencode": "▣", "cursor": "⬢"}

// accountNames are the short names of the accounts: one account of a
// provider by its plan, two by the name of their emails.
func (m Model) accountNames() ([]string, int) {
	per := map[string]int{}
	for _, a := range m.snap.Limits {
		per[a.Provider]++
	}
	names := make([]string, len(m.snap.Limits))
	nameW := 0
	for i, a := range m.snap.Limits {
		email, plan, _ := strings.Cut(a.Account, " · ")
		names[i] = plan
		if per[a.Provider] > 1 || plan == "" {
			names[i], _, _ = strings.Cut(email, "@")
		}
		if names[i] == "" {
			names[i] = a.Provider
		}
		nameW = max(nameW, lipgloss.Width(names[i]))
	}
	return names, nameW
}

// machineLines are the machine in a few lines: cpu with its last minute
// and the load, memory, disk, and the agents that run.
func (m Model) machineLines(w int, full bool, toggle string) []string {
	st, mc := m.st, m.snap.Machine
	host := m.opt.Host
	if host == "" {
		host = m.host.Host
	}
	about := ""
	if host != "" {
		about = host + " · "
	}
	about += plural(len(mc.Cores), "core")
	right := st.faint.Render(about + " ")
	if toggle != "" {
		right = st.accent.Render(toggle) + " "
	}
	sp := max(min(w-30, 24), 8)
	if full {
		sp = 24
	}
	used := mc.MemTotal - mc.MemAvail
	memPct := 0.0
	if mc.MemTotal > 0 {
		memPct = float64(used) / float64(mc.MemTotal) * 100
	}
	out := []string{
		spread(st.label.Render(" MACHINE"), right, w),
		" " + st.label.Render("cpu  ") + st.accent.Render(spark(m.history.cpu, sp, 100)) +
			st.text.Render(pad(fmt.Sprintf(" %.0f%%", mc.CPU), 6)) + st.label.Render("load ") + st.text.Render(fmt.Sprintf("%.1f", mc.Load[0])),
		" " + st.label.Render("mem  ") + m.meter(memPct, 8, st.muted) + st.text.Render(fmt.Sprintf(" %.0f of %.0f GB", gb(used), gb(mc.MemTotal))),
		" " + st.label.Render("disk ") + m.diskCell(),
		" " + st.label.Render("runs ") + st.text.Render(m.runs()),
	}
	for i := range out {
		out[i] = fit(out[i], w)
	}
	return out
}

func gb(b uint64) float64 { return float64(b) / (1 << 30) }

// diskCell is the use of the home's file system, "▰▰▰▰▰▱▱▱ 71% of 1 TB",
// and of / when it is another; the disk's traffic when none was read.
func (m Model) diskCell() string {
	st, mc := m.st, m.snap.Machine
	if len(mc.Disks) == 0 {
		return st.text.Render("r " + size(mc.DiskRead) + "/s · w " + size(mc.DiskWrite) + "/s")
	}
	d := mc.Disks[0]
	cell := m.meter(d.Pct(), 8, m.useStyle(d.Pct())) + st.text.Render(fmt.Sprintf(" %.0f%% of %s", d.Pct(), bytesWord(d.Total)))
	for _, o := range mc.Disks[1:] {
		cell += st.faint.Render(" · "+o.Path+" ") + m.useStyle(o.Pct()).Render(fmt.Sprintf("%.0f%%", o.Pct()))
	}
	return cell
}

// bytesWord is a size as a disk is named: 931 GB, 2 TB.
func bytesWord(b uint64) string {
	if tb := float64(b) / (1 << 40); tb >= 1 {
		return strings.TrimSuffix(fmt.Sprintf("%.1f", tb), ".0") + " TB"
	}
	return fmt.Sprintf("%.0f GB", gb(b))
}

// runs counts the agents by their command: "13 claude · 1 codex".
func (m Model) runs() string {
	n := map[string]int{}
	for _, s := range m.all.sessions {
		agent := s.Agent
		if agent == "" {
			agent = "agent"
		}
		n[agent]++
	}
	if len(n) == 0 {
		return "no agents"
	}
	var names []string
	for a := range n {
		names = append(names, a)
	}
	slices.SortFunc(names, func(a, b string) int {
		if n[a] != n[b] {
			return n[b] - n[a]
		}
		return strings.Compare(a, b)
	})
	parts := make([]string, len(names))
	for i, a := range names {
		parts[i] = fmt.Sprintf("%d %s", n[a], a)
	}
	return strings.Join(parts, " · ")
}

// machinePanel is the Machine tab: usage first, then the machine, then
// the detached busy loops and the top processes.
func (m Model) machinePanel(w int) body {
	var b body
	b.add(-1, m.usageLines(w, true, "")...)
	b.add(-1, "")
	b.add(-1, m.machineLines(w, true, "")...)
	b.add(-1, m.machine(w)...)
	p := m.procs(w)
	b.lines, b.rows = append(b.lines, p.lines...), append(b.rows, p.rows...)
	return b
}
