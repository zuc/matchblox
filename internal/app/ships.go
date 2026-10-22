package app

import (
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/blox-eng/matchblox/internal/proto"
	"github.com/blox-eng/matchblox/internal/sample"
)

// Ships are the pull requests the sessions drive, each on its track from
// open to live (design/0006 §8). The service reads them; the console
// draws the report it sends.

// Ship kinds and stages, as the tracker names them (proto aliases its
// types; the console does not import it).
const (
	shipRunning = "running"
	shipWaits   = "waits-for-you"
	shipStuck   = "stuck"
	shipDone    = "done"
	stageLive   = 4
)

var stageNames = [...]string{"open", "ci", "green", "merged", "live"}

func (m Model) ships() []proto.Ship {
	if m.shipReport == nil {
		return nil
	}
	return m.shipReport.Ships
}

func (m Model) selectedShip() (proto.Ship, bool) {
	ss := m.ships()
	if len(ss) == 0 {
		return proto.Ship{}, false
	}
	return ss[min(m.shipSel, len(ss)-1)], true
}

// shipOf is the ship a session drives: one in flight before one that went
// live.
func (m Model) shipOf(s sample.Session) (proto.Ship, bool) {
	for _, sh := range m.ships() {
		if sh.Session.Pane != "" && sh.Session.Pane == s.Pane || sh.Session.ID != "" && sh.Session.ID == s.SessionID {
			return sh, true
		}
	}
	return proto.Ship{}, false
}

// driver is the session that drives a ship.
func (m Model) driver(sh proto.Ship) (sample.Session, bool) {
	for _, s := range m.all.sessions {
		if sh.Session.Pane != "" && s.Pane == sh.Session.Pane || sh.Session.ID != "" && s.SessionID == sh.Session.ID {
			return s, true
		}
	}
	return sample.Session{}, false
}

func (m Model) driverName(sh proto.Ship) string {
	if s, ok := m.driver(sh); ok && s.Name != "" {
		return s.Name
	}
	if sh.Session.Name != "" {
		return sh.Session.Name
	}
	return sh.Session.Pane
}

// driverGone is the flash for a ship whose session is no longer there.
func (m Model) driverGone(sh proto.Ship) string {
	if name := m.driverName(sh); name != "" {
		return name + " no longer runs"
	}
	return "its driver no longer runs"
}

// shipStyle is the colour of a ship's current stage and note: warn while
// it runs, the accent when it waits for you, neg when it is stuck.
func (m Model) shipStyle(sh proto.Ship) lipgloss.Style {
	switch sh.Kind {
	case shipRunning:
		return m.st.warn
	case shipWaits, shipDone:
		return m.st.accent
	case shipStuck:
		return m.st.neg
	}
	return m.st.faint
}

// track is a ship's five stages, ●━●━◉┄○┄○: done muted, the current one in
// its colour (✕ when stuck), the rest faint.
// trackW is the width of a ship's track: a mark for each stage and a line
// between two.
var trackW = 2*len(stageNames) - 1

// numW is the width of the widest ship number read, "#4551", so the tracks
// after the numbers line up.
func (m Model) numW() int {
	n := 2
	for _, sh := range m.ships() {
		n = max(n, len("#"+strconv.Itoa(sh.Number)))
	}
	return n
}

func (m Model) track(sh proto.Ship) string {
	st := m.st
	var b strings.Builder
	for i := range stageNames {
		switch {
		case i < sh.Stage:
			b.WriteString(st.muted.Render("●"))
		case i == sh.Stage && sh.Kind == shipStuck:
			b.WriteString(st.neg.Render("✕"))
		case i == sh.Stage && sh.Kind == shipDone:
			b.WriteString(m.shipStyle(sh).Render("●"))
		case i == sh.Stage:
			b.WriteString(m.shipStyle(sh).Render("◉"))
		default:
			b.WriteString(st.faint.Render("○"))
		}
		switch {
		case i == len(stageNames)-1:
		case i < sh.Stage:
			b.WriteString(st.muted.Render("━"))
		default:
			b.WriteString(st.faint.Render("┄"))
		}
	}
	return b.String()
}

// shipCell is "#4551 ●━◉┄○┄○", with the note when note is set.
func (m Model) shipCell(sh proto.Ship, note bool) string {
	cell := padTo(m.st.accent.Render("#"+strconv.Itoa(sh.Number)), m.numW()) + " " + m.track(sh)
	if note {
		cell += "  " + m.shipStyle(sh).Render(sh.Note)
	}
	return cell
}

// shipAge is how long the ship has been in its stage.
func (m Model) shipAge(sh proto.Ship) string {
	if !sh.Since.IsZero() {
		return brief(m.now().Sub(sh.Since))
	}
	return brief(sh.Age)
}

// shipsStrip says plainly why the ships may be behind: GitHub's rate limit,
// gh that cannot read, a repository whose poll failed.
func (m Model) shipsStrip(w int) []string {
	r := m.shipReport
	if r == nil {
		return nil
	}
	var says []string
	if !r.Paused.IsZero() {
		says = append(says, "paused · GitHub's rate limit is low, ships read again at "+r.Paused.Local().Format("15:04"))
	}
	if r.Unknown != "" {
		says = append(says, "GitHub cannot be read: "+r.Unknown)
	}
	for _, e := range r.Errors {
		says = append(says, "not read: "+e)
	}
	var out []string
	for _, say := range says {
		for _, l := range wrapPlain(say, w-2) {
			out = append(out, " "+m.st.warn.Render(l))
		}
	}
	return out
}

// shipsPanel is the Ships tab: each ship's number, title and age, then its
// track and note. On a phone the selected one adds who drives it and its
// keys.
func (m Model) shipsPanel(w int) body {
	st := m.st
	var b body
	b.add(-1, m.shipsStrip(w)...)
	ss := m.ships()
	if len(ss) == 0 {
		b.add(-1, "", fit(" "+st.faint.Render("No ships yet."), w))
		for _, l := range wrapPlain("A pull request a session creates, pushes or acts on shows here, on its way from open to live.", w-2) {
			b.add(-1, " "+st.faint.Render(l))
		}
		return b
	}
	b.add(-1, st.label.Render(fit(" open → ci → green → merged → live", w)))
	sel := min(m.shipSel, len(ss)-1)
	for i, sh := range ss {
		on := i == sel
		mark := " "
		if on {
			mark = st.accent.Render("▌")
		}
		lines := []string{
			spread(mark+padTo(st.accent.Render("#"+strconv.Itoa(sh.Number)), m.numW())+" "+st.ink(on).Render(sh.Title), cutLeft(st.faint.Render(m.shipAge(sh)), 4)+" ", w),
			"   " + m.track(sh) + "  " + m.shipStyle(sh).Render(sh.Note),
		}
		if on && m.layoutAt(m.width) == Narrow {
			lines = append(lines, spread("   "+st.faint.Render("by ")+st.text.Render(m.driverName(sh)), m.shipKeysLine(sh)+" ", w))
		}
		b.addRow(i, on, w, st, lines...)
		b.add(-1, "")
	}
	return b
}

// shipKeys are the keys of a ship: its session, a merge when it waits for
// yours, an answer when it is stuck.
func (m Model) shipKeys(sh proto.Ship) []hint {
	keys := []hint{{"⏎", "session", "enter"}}
	if len(sh.Merge) > 0 {
		keys = append(keys, hint{"x", "merge", "x"})
	}
	if sh.Kind == shipStuck {
		keys = append(keys, hint{"a", "answer", "a"})
	}
	return keys
}

func (m Model) shipKeysLine(sh proto.Ship) string {
	var parts []string
	for _, h := range m.shipKeys(sh) {
		parts = append(parts, m.st.accent.Render(h.key)+m.st.muted.Render(" "+h.label))
	}
	return strings.Join(parts, "  ")
}

// shipDetail is the right pane of Ships: the title, who drives it, the
// five stages with their times and the note of the current one, the URL
// and the keys.
func (m Model) shipDetail(w int) []string {
	st := m.st
	sh, ok := m.selectedShip()
	if !ok {
		return nil
	}
	out := []string{"", fit(" "+st.accent.Bold(true).Render("#"+strconv.Itoa(sh.Number))+st.faint.Render("  by ")+st.text.Render(m.driverName(sh))+
		st.faint.Render(" · "+sh.Repo), w)}
	for _, l := range wrapPlain(sh.Title, w-2) {
		out = append(out, " "+st.text.Render(l))
	}
	out = append(out, "", " "+st.label.Render("TRACK"))
	for i, name := range stageNames {
		glyph, style := "○", st.faint
		switch {
		case i < sh.Stage:
			glyph, style = "●", st.muted
		case i == sh.Stage && sh.Kind == shipStuck:
			glyph, style = "✕", st.neg
		case i == sh.Stage && sh.Kind == shipDone:
			glyph, style = "●", m.shipStyle(sh)
		case i == sh.Stage:
			glyph, style = "◉", m.shipStyle(sh)
		}
		nameStyle := st.faint
		if i <= sh.Stage {
			nameStyle = st.text
		}
		what := ""
		if at := sh.Stages[i].At; !at.IsZero() && i <= sh.Stage {
			what = st.muted.Render(m.when(at))
		}
		if i == sh.Stage && sh.Note != "" {
			what = strings.TrimSpace(what + " " + style.Render(sh.Note))
		}
		out = append(out, fit("  "+style.Render(glyph)+" "+nameStyle.Render(pad(name, 8))+what, w))
		if i < len(stageNames)-1 {
			if i < sh.Stage {
				out = append(out, "  "+st.muted.Render("┃"))
			} else {
				out = append(out, "  "+st.faint.Render("┆"))
			}
		}
	}
	if sh.URL != "" {
		out = append(out, "", fit(" "+st.faint.Render(sh.URL), w))
	}
	return append(out, "", fit(" "+m.shipKeysLine(sh), w))
}

// when is a time on the screen: 14:05 today, else Thu 14:05.
func (m Model) when(t time.Time) string {
	now := m.now().Local()
	t = t.Local()
	if t.YearDay() == now.YearDay() && t.Year() == now.Year() {
		return t.Format("15:04")
	}
	return t.Format("Mon 15:04")
}

// toDriver selects the session that drives the selected ship in Sessions.
func (m Model) toDriver() Model {
	sh, ok := m.selectedShip()
	if !ok {
		return m
	}
	s, ok := m.driver(sh)
	if !ok {
		m.flash = m.driverGone(sh)
		return m
	}
	m.setTab(tabSessions)
	m.doorPick, m.recPick, m.chose, m.selPID = "", "", true, s.PID
	return m
}

// shipAnswer is a on a stuck ship: the peek on its session, to answer it
// when it waits and to message it when it does not.
func (m Model) shipAnswer() Model {
	sh, ok := m.selectedShip()
	if !ok || sh.Kind != shipStuck {
		m.flash = "a answers the session of a stuck ship"
		return m
	}
	s, ok := m.driver(sh)
	if !ok || s.Pane == "" {
		m.flash = m.driverGone(sh)
		return m
	}
	m.openPeek(s.Pane)
	return m
}

// mergeShip is x on a ship that waits for your merge: the repository's
// merge, behind a typed y.
func (m Model) mergeShip() *action {
	sh, ok := m.selectedShip()
	if !ok || len(sh.Merge) == 0 {
		return nil
	}
	return &action{label: "merge", steps: [][]string{sh.Merge}, destructive: true,
		rec: "ship:" + sh.Repo + "#" + strconv.Itoa(sh.Number), which: "secondary"}
}

// shipDetailLine is a session's ship in its detail: the track and note,
// then the title and the URL.
func (m Model) shipField(s sample.Session, w int) []string {
	st := m.st
	if sh, ok := m.shipOf(s); ok {
		out := []string{fit(" "+st.label.Render(pad("ship", 9))+m.shipCell(sh, true), w),
			fit(" "+pad("", 9)+st.text.Render(sh.Title), w)}
		if sh.URL != "" {
			out = append(out, fit(" "+pad("", 9)+st.faint.Render(sh.URL), w))
		}
		return out
	}
	p := m.prOf(s.Cwd)
	if p == nil {
		return nil
	}
	title := p.Title
	if p.Draft {
		title = "draft " + title
	}
	out := []string{fit(" "+st.label.Render(pad("ship", 9))+st.accent.Render("#"+strconv.Itoa(p.Number))+" "+st.text.Render(title), w)}
	if p.URL != "" {
		out = append(out, fit(" "+pad("", 9)+st.faint.Render(p.URL), w))
	}
	return out
}

// shipRowCell is the desk row's ship: "#4551 ●━◉┄○┄○", or the number of
// the worktree's open PR when no ship is read.
func (m Model) shipRowCell(s sample.Session) string {
	if sh, ok := m.shipOf(s); ok {
		return m.shipCell(sh, false)
	}
	if p := m.prOf(s.Cwd); p != nil {
		return m.st.accent.Render("#" + strconv.Itoa(p.Number))
	}
	return ""
}

// shipOpened is a phone's opened row's ship: "#4545 ●━✕┄○┄○ conflicts with
// main", or the worktree's PR and its title.
func (m Model) shipOpened(s sample.Session, w int) string {
	st := m.st
	if sh, ok := m.shipOf(s); ok {
		return ansi.Truncate("   "+m.shipCell(sh, true), w, "…")
	}
	p := m.prOf(s.Cwd)
	if p == nil {
		return ""
	}
	title := p.Title
	if p.Draft {
		title = "draft " + title
	}
	return "   " + st.accent.Render("#"+strconv.Itoa(p.Number)) + " " + st.muted.Render(title)
}
