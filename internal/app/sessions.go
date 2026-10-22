package app

import (
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/blox-eng/matchblox/internal/advice"
	"github.com/blox-eng/matchblox/internal/doors"
	"github.com/blox-eng/matchblox/internal/proto"
	"github.com/blox-eng/matchblox/internal/queue"
	"github.com/blox-eng/matchblox/internal/sample"
)

// Sessions is one list in three groups, by what each session needs from
// the person (design/0006 §5.1). The doors, the stoker's card and the
// recommendations sit above them.
const (
	groupWaits = iota
	groupWorking
	groupQuiet
	groupHeld
)

var groupNames = [...]string{"WAITS FOR YOU", "WORKING", "QUIET", "HELD"}

// What the list is grouped by: g cycles them.
const (
	groupNeed = iota
	groupStatus
	groupRepo
	groupNone
)

// holdByYou is the hold of the builder; the stoker's are the others.
const holdByYou = "you"

var groupModes = [...]string{"need", "status", "repo", "none"}

// waitOf is the queue row of a session that waits: by its pane, or by its
// id outside tmux, where no pane tells sessions apart.
func (m Model) waitOf(s sample.Session) (queue.Item, bool) {
	for _, it := range m.queue {
		if s.Pane != "" && it.Pane == s.Pane || s.Pane == "" && s.SessionID != "" && it.SessionID == s.SessionID {
			return it, true
		}
	}
	return queue.Item{}, false
}

// groupOf is the group of a session. One that waits since yesterday is
// cold, not waiting; one the plan's limit stops can do nothing now.
func (m Model) groupOf(s sample.Session) int {
	if _, held := m.holdOf(s); held {
		return groupHeld
	}
	switch _, waits := m.waitOf(s); {
	case s.Busy:
		return groupWorking
	case s.Age() != "idle" || s.Stopped():
		return groupQuiet
	case waits:
		return groupWaits
	case len(s.Shells) > 0:
		return groupWorking
	}
	return groupQuiet
}

// parkWord is what z does to a session: park it, or let it go.
func (m Model) parkWord(s sample.Session) string {
	if _, held := m.holdOf(s); held {
		return "free"
	}
	return "park"
}

// holdOf is the hold on a session: by its id, or by its pane outside
// tmux's reach of ids.
func (m Model) holdOf(s sample.Session) (proto.Hold, bool) {
	for _, h := range m.holds {
		if s.SessionID != "" && h.SessionID == s.SessionID || s.SessionID == "" && s.Pane != "" && h.Pane == s.Pane {
			return h, true
		}
	}
	return proto.Hold{}, false
}

// groupLabel is the group a session is listed under: its need, its state
// word, its repo; a held one is HELD whatever else is true of it.
func (m Model) groupLabel(s sample.Session) string {
	switch m.sessGroup {
	case groupStatus:
		if _, held := m.holdOf(s); held {
			return groupNames[groupHeld]
		}
		return strings.ToUpper(m.wordOf(s))
	case groupRepo:
		return m.repoOf(s)
	case groupNone:
		return ""
	}
	return groupNames[m.groupOf(s)]
}

// repoOf is the repository a session works in: the checkout its directory
// is in, else the directory's own name.
func (m Model) repoOf(s sample.Session) string {
	if m.git != nil {
		best, name := 0, ""
		within := func(dir, repo string) {
			if (s.Cwd == dir || strings.HasPrefix(s.Cwd, dir+"/")) && len(dir) > best {
				best, name = len(dir), filepath.Base(repo)
			}
		}
		for _, r := range m.git.Repos {
			within(r.Path, r.Path)
			for _, wt := range r.Worktrees {
				within(wt.Path, r.Path)
			}
		}
		if name != "" {
			return name
		}
	}
	if s.Cwd == "" {
		return "no directory"
	}
	return filepath.Base(s.Cwd)
}

// rowWord is the state cell of a session row. merged: the cell is a hold
// or a reason, long enough to take the wait column's cells too.
func (m Model) rowWord(s sample.Session) (word string, merged bool) {
	st := m.st
	if h, ok := m.holdOf(s); ok {
		if h.By == holdByYou {
			return st.muted.Render("⏸ parked by you"), true
		}
		why := "held by stoker"
		if h.Why != "" {
			why += " · " + h.Why
		}
		return st.muted.Render("⏸ " + why), true
	}
	plain := m.wordOf(s)
	if r, ok := m.reasons[s.Pane]; ok && r.Word != "" && s.Pane != "" && plain != "asks" && plain != "waits" {
		return m.tone(r.Tone).Render(r.Word), true
	}
	if m.groupOf(s) == groupWaits {
		return st.accent.Render(plain), false
	}
	return st.muted.Render(plain), false
}

// tone is the style a reason is drawn in; ok has no colour of its own.
func (m Model) tone(t string) lipgloss.Style {
	switch t {
	case "warn":
		return m.st.warn
	case "neg":
		return m.st.neg
	case "faint":
		return m.st.faint
	}
	return m.st.accent
}

var testRunner = regexp.MustCompile(`\b(go test|pytest|jest|vitest|playwright|mocha|rspec|cargo test|(npm|pnpm|yarn|bun)( run)? (test|e2e)|make (test|e2e|check))\b`)

// wordOf is the state of a session in a row: what it does, the word of
// its queue row, or its age.
func (m Model) wordOf(s sample.Session) string {
	switch {
	case s.Busy:
		return busyWord(s)
	case m.groupOf(s) == groupWorking:
		for _, sh := range s.Shells {
			if testRunner.MatchString(sh.Command) {
				return "tests"
			}
		}
		return "shell"
	}
	return m.stateWord(s)
}

// waitCell is how long a session waits, or sat idle, or when the plan's
// limit lets it work again; a session at work has none.
func (m Model) waitCell(s sample.Session) string {
	if it, ok := m.waitOf(s); ok && m.groupOf(s) == groupWaits {
		return brief(m.now().Sub(it.Since))
	}
	if m.groupOf(s) == groupWorking {
		return ""
	}
	if s.Stopped() {
		return m.idleCell(s)
	}
	return brief(s.Idle)
}

// brief is the first unit of a duration: 4m, 2h, 20d.
func brief(d time.Duration) string {
	h := sample.Human(d)
	for i, r := range h {
		if r < '0' || r > '9' {
			return h[:i+1]
		}
	}
	return h
}

// firstWaiting is the first session of the list that waits, or -1.
func (m Model) firstWaiting() int {
	for i, s := range m.snap.Sessions {
		if m.groupOf(s) == groupWaits {
			return i
		}
	}
	return -1
}

// sessIndex is the selected row of Sessions: the doors, the sessions,
// then the recommendations. A door that folds passes the
// selection to the next door. Nothing picked: the first session that
// waits, else the first door.
func (m Model) sessIndex() int {
	nd, ns, nr := len(m.doors), len(m.snap.Sessions), m.recRows()
	for i, d := range m.doors {
		if d.ID == m.doorPick {
			return i
		}
	}
	if nd > 0 && m.doorPick != "" {
		return min(m.doorAt, nd-1)
	}
	for i, r := range m.recs[:nr] {
		if r.ID == m.recPick && m.recPick != "" {
			return nd + ns + i
		}
	}
	if nd > 0 && !m.chose && m.firstWaiting() < 0 {
		return 0
	}
	if ns == 0 {
		if nr > 0 {
			return nd
		}
		return 0
	}
	return nd + m.selIndex()
}

// selectedSession is the session under the selection in Sessions; a door
// or a recommendation has none.
func (m Model) selectedSession() (sample.Session, bool) {
	if i := m.sessIndex(); m.tab != tabSessions || i < len(m.doors) || i >= len(m.doors)+len(m.snap.Sessions) {
		return sample.Session{}, false
	}
	return m.selected()
}

func (m Model) selectedDoor() (doors.Door, bool) {
	if m.tab != tabSessions {
		return doors.Door{}, false
	}
	if i := m.sessIndex(); i < len(m.doors) {
		return m.doors[i], true
	}
	return doors.Door{}, false
}

// selectedRec is the recommendation selected under the sessions.
func (m Model) selectedRec() (advice.Rec, bool) {
	if m.tab != tabSessions {
		return advice.Rec{}, false
	}
	i := m.sessIndex() - len(m.doors) - len(m.snap.Sessions)
	if i < 0 || i >= m.recRows() {
		return advice.Rec{}, false
	}
	return m.recs[i], true
}

// sessionKeys are the keys of the selected row of Sessions.
func (m Model) sessionKeys() []hint {
	if _, ok := m.selectedDoor(); ok {
		return []hint{{"⏎", "open", "enter"}, {"x", "close", "x"}}
	}
	if _, ok := m.selectedRec(); ok {
		return []hint{{"⏎", "do", "enter"}, {"x", "other", "x"}, {"f", "stoker", "f"}}
	}
	keys := []hint{{"⏎", "open", "enter"}}
	s, ok := m.selectedSession()
	if ok && s.Pane != "" {
		keys = append(keys, hint{"a", m.peekWord(s), "a"}, hint{"x", "kill", "x"}, hint{"r", "rename", "r"}, hint{"A", "switch", "A"}, hint{"z", m.parkWord(s), "z"})
	}
	return append(keys, hint{"N", "new", "N"}, hint{"p", "ships", "p"}, hint{"f", "stoker", "f"})
}

func (m Model) sessionsPanel(w int) body {
	var b body
	m.doorsSection(&b, w)
	m.sessionRows(&b, w)
	st := m.st
	if n := len(m.snap.IdlePanes); n > 0 {
		var ids []string
		for _, p := range m.snap.IdlePanes {
			ids = append(ids, place(p.Target, p.Pane)+" "+p.Command)
		}
		b.add(-1, "", st.faint.Render(fit(fmt.Sprintf(" %d without an agent: %s", n, strings.Join(ids, ", ")), w)))
	}
	b.add(-1, m.exitedLines(w)...)
	m.recsSection(&b, w)
	return b
}

// sessionRows are the sessions under a faint label for each group, or
// under the sort when another one is on.
func (m Model) sessionRows(b *body, w int) {
	st := m.st
	ss := m.snap.Sessions
	if len(b.lines) > 0 {
		b.add(-1, "")
	}
	if len(ss) == 0 {
		b.add(-1, st.faint.Render(m.nothing(" no agent sessions found in tmux")))
		return
	}
	base := len(m.doors)
	sel := m.sessIndex() - base
	count := map[string]int{}
	for _, s := range ss {
		count[m.groupLabel(s)]++
	}
	by := ""
	if m.sessSort != (sortBy{}) {
		by = "  ·  by " + sessCols[m.sessSort.col] + " " + m.sessSort.arrow()
	}
	if m.sessGroup == groupNone {
		b.add(-1, st.label.Render(fit(fmt.Sprintf(" %d SESSIONS · by %s %s", len(ss), sessCols[m.sessSort.col], m.sessSort.arrow()), w)))
	}
	group := ""
	for i, s := range ss {
		if g := m.groupLabel(s); m.sessGroup != groupNone && (g != group || i == 0) {
			group = g
			b.add(-1, st.label.Render(fit(fmt.Sprintf(" %s · %d", g, count[g])+by, w)))
			by = ""
		}
		rows := m.sessionRow(s, i == sel, w)
		if len(rows) > 1 {
			// The opened block of a phone stands apart between hairlines.
			b.add(-1, st.hair.Render(strings.Repeat("─", w)))
		}
		b.addRow(base+i, i == sel, w, st, rows...)
		if len(rows) > 1 {
			b.add(-1, st.hair.Render(strings.Repeat("─", w)))
		}
	}
}

// sessionRow is one line for a session. A row of 70 columns or more adds
// the plan, the pull request and the place; on a phone the selected one
// opens in place.
func (m Model) sessionRow(s sample.Session, sel bool, w int) []string {
	st := m.st
	mark := " "
	if sel {
		mark = st.accent.Render("▌")
	}
	word, merged := m.rowWord(s)
	wait := st.faint.Render(m.waitCell(s))
	ctx := m.contextCell(s)
	t := m.sessTracks(w)
	opens := sel && m.layoutAt(m.width) == Narrow
	state := padTo(word, t.word) + cutLeft(wait, t.wait)
	if merged {
		state = padTo(word, t.word+t.wait) // the wait column's cells are the word's too
	}
	if !t.wide {
		line := spread(mark+m.matchCell(s)+st.ink(sel).Render(s.Name), state+cutLeft(ctx, t.ctx)+" ", w)
		if !opens {
			return []string{line}
		}
		return append([]string{line}, m.opened(s, w)...)
	}
	line := mark + m.matchCell(s) + st.ink(sel).Render(pad(s.Name, t.name)) + state + cutLeft(ctx, t.ctx)
	if t.plan > 0 {
		plan := ""
		switch {
		case s.Do == "compact":
			plan = st.neg.Render("! compact")
		case s.Do == "clear":
			plan = st.warn.Render("▲ clear")
		case s.Progress != nil:
			plan = m.progressCell(s.Progress, 5)
		}
		line += "  " + padTo(plan, t.plan-2)
	}
	if t.where > 0 {
		// The ship's track takes the room of the place when ships are read:
		// the detail names the place.
		where := padTo(m.shipRowCell(s), 7) + st.muted.Render(place(s.Target, s.Pane))
		if len(m.ships()) > 0 {
			where = m.shipRowCell(s)
		}
		line += padTo(where, t.where)
	}
	if !opens {
		return []string{fit(line, w)}
	}
	return append([]string{fit(line, w)}, m.opened(s, w)...)
}

// tracks are the widths of a session row's columns, the same on every row
// of a list w wide so each column starts at the same x: the name takes
// what the others leave. A list under 70 columns has the name and, on the
// right, the word, the wait and the context; wider adds the plan and the
// ship (or the pull request and the place), dropped in that order (the
// place first) when the name would get under 12.
type tracks struct {
	wide                               bool
	name, word, wait, ctx, plan, where int
}

func (m Model) sessTracks(w int) tracks {
	t := tracks{word: 8, wait: 5, ctx: 6}
	if m.layoutAt(m.width) == Narrow && w < 70 {
		t.word = 7
	}
	// A hold or a reason spans the word and the wait; the wait column
	// grows to fit the longest, so every row's columns stay in line.
	longest := 0
	for _, s := range m.snap.Sessions {
		if word, merged := m.rowWord(s); merged {
			longest = max(longest, lipgloss.Width(word))
		}
	}
	t.wait += max(min(longest, min(w*55/100, 32))-t.word-t.wait, 0)
	if w < 70 {
		return t
	}
	t.wide, t.plan = true, 15
	t.where = 7 + 16 // the pull request and the place
	if len(m.ships()) > 0 {
		t.where = m.numW() + 1 + trackW
	}
	fixed := 3 + t.word + t.wait + t.ctx + 1 // the mark, the match, the columns and a margin
	if w-fixed-t.plan-t.where < 12 {
		t.where = 0
	}
	if w-fixed-t.plan-t.where < 12 {
		t.plan = 0
	}
	t.name = w - fixed - t.plan - t.where
	return t
}

// cutLeft right-aligns styled text in w cells, cutting it when it is wider.
func cutLeft(s string, w int) string {
	return padLeft(ansi.Truncate(s, w, "…"), w)
}

// opened is what a selected row on a phone grows by: what the agent said
// last, its pull request, then its plan and its keys.
func (m Model) opened(s sample.Session, w int) []string {
	st := m.st
	var out []string
	if said := m.lastWords(s); said != "" {
		lines := wrapPlain("“"+said+"”", w-4)
		if len(lines) > 2 {
			lines = []string{lines[0], ansi.Truncate(lines[1], w-5, "") + "…"}
		}
		for _, l := range lines {
			out = append(out, "   "+st.muted.Render(l))
		}
	}
	if l := m.shipOpened(s, w); l != "" {
		out = append(out, l)
	}
	left := "  "
	if p := s.Progress; p != nil {
		full := min(max((p.Pct*5+50)/100, 0), 5)
		left += " " + st.text.Render(strings.Repeat("▰", full)) + st.faint.Render(strings.Repeat("▱", 5-full)) +
			st.muted.Render(fmt.Sprintf(" plan %d%%", p.Pct))
	}
	switch s.Do {
	case "compact":
		left += "  " + st.neg.Render("! compact")
	case "clear":
		left += "  " + st.warn.Render("▲ clear")
	}
	keys, _ := m.openedKeys(s, w)
	if strings.TrimSpace(left) != "" && lipgloss.Width(left)+lipgloss.Width(keys)+1 > w {
		out, left = append(out, left), "" // the plan and the keys get a line each
	}
	return append(out, spread(left, keys, w))
}

// openedKeys are the keys on the right of an opened row, and where a tap
// presses each, in columns of the row.
func (m Model) openedKeys(s sample.Session, w int) (string, []span) {
	hs := []hint{{"⏎", "open", "enter"}}
	if s.Pane != "" {
		hs = []hint{{"a", m.peekWord(s), "a"}, {"A", "switch", "A"}, hs[0]}
	}
	keys, spans := m.hintRow(hs, 2)
	keys += " "
	return keys, shift(spans, w-lipgloss.Width(keys))
}

// hintRow is hints on one line, gap columns apart, and where a tap on each
// presses its key, in columns of the line.
func (m Model) hintRow(hs []hint, gap int) (string, []span) {
	line, x := "", 0
	var spans []span
	for i, h := range hs {
		if i > 0 {
			line += strings.Repeat(" ", gap)
			x += gap
		}
		part := m.st.accent.Render(h.key) + m.st.muted.Render(" "+h.label)
		spans = append(spans, span{x, x + lipgloss.Width(part), h.press})
		line += part
		x += lipgloss.Width(part)
	}
	return line, spans
}

func shift(spans []span, dx int) []span {
	out := make([]span, len(spans))
	for i, s := range spans {
		out[i] = span{s.x0 + dx, s.x1 + dx, s.press}
	}
	return out
}

// lastWords is what the agent said last: its queue row's message while it
// waits, else the last line read from its reply or its pane.
func (m Model) lastWords(s sample.Session) string {
	if it, ok := m.waitOf(s); ok && it.LastLine != "" {
		return it.LastLine
	}
	return s.LastLine
}

// contextCell is a session's context use: warn from 70%, neg past the
// line; fresh before the first turn, a dash when it is not measured.
func (m Model) contextCell(s sample.Session) string {
	switch s.Context {
	case "known":
	case "fresh":
		return m.st.faint.Render("fresh")
	case "unmeasured":
		return m.st.faint.Render("—")
	default:
		return ""
	}
	style := m.st.muted
	switch {
	case m.full().Reached(s):
		style = m.st.neg
	case s.ContextPct >= 70:
		style = m.st.warn
	}
	return style.Render(fmt.Sprintf("%.0f%%", s.ContextPct))
}

// longWord is a session's state in words, for the detail.
func (m Model) longWord(s sample.Session) string {
	switch w := m.wordOf(s); w {
	case "asks":
		return "asks you"
	case "waits":
		return "waits for you"
	case "done", "paused":
		return w + ", waits for you"
	case "busy":
		return "working"
	case "agents":
		return "working through subagents"
	case "tests":
		return "running tests"
	case "shell":
		return "running a shell"
	case "out":
		return "out until " + m.idleCell(s)
	case "stale":
		return "stale, x ends it"
	default:
		return w
	}
}

// detailPane is the right pane of Sessions on an opened phone and a desk:
// the selected session, recommendation or door.
// detailPane is the right pane in w columns, grown into rows when it has
// more to say (rows 0: its fewest lines), and whether all of it shows.
func (m Model) detailPane(w, rows int) ([]string, bool) {
	lines, cut := m.detailLines(w, rows)
	return lines, !cut
}

func (m Model) detailLines(w, rows int) ([]string, bool) {
	st := m.st
	if m.tab == tabShips {
		return m.shipDetail(w), false
	}
	if d, ok := m.selectedDoor(); ok {
		out := []string{"", fit(" "+st.accent.Render(d.Title), w), ""}
		text, style := d.Why, st.muted
		if d.Problem != "" {
			text, style = d.Problem, st.warn
		}
		for _, l := range wrapPlain(text, w-2) {
			out = append(out, " "+style.Render(l))
		}
		return append(out, "", " "+st.accent.Render("⏎")+st.muted.Render(" open   ")+st.accent.Render("x")+st.muted.Render(" close")), false
	}
	if r, ok := m.selectedRec(); ok {
		out := []string{""}
		for _, l := range wrapPlain(tilde(r.Title), w-2) {
			out = append(out, " "+st.accent.Render(l))
		}
		out = append(out, "")
		for _, l := range wrapPlain(tilde(r.Evidence), w-2) {
			out = append(out, " "+st.muted.Render(l))
		}
		return append(append(out, ""), m.recActions(r, w)...), false
	}
	s, ok := m.selectedSession()
	if !ok {
		return []string{"", st.faint.Render(m.nothing(" no agent sessions found in tmux"))}, false
	}
	out, cut := m.sessionDetail(s, w, 6)
	if extra := rows - len(out); extra > 0 && cut {
		// Free rows: more of what it said.
		out, cut = m.sessionDetail(s, w, 6+extra)
	}
	return out, cut
}

// sessionDetail is a session's detail with at most said lines of what it
// said last, and whether that was cut.
func (m Model) sessionDetail(s sample.Session, w, said int) ([]string, bool) {
	st := m.st
	out := []string{"", fit(" "+st.accent.Bold(true).Render(s.Name), w)}
	where := []string{m.longWord(s)}
	if c := m.waitCell(s); c != "" && m.wordOf(s) != "out" {
		where = append(where, c)
	}
	where = append(where, place(s.Target, s.Pane), tilde(s.Cwd))
	out = append(out, fit(" "+m.matchCell(s)+st.muted.Render(strings.Join(where, " · ")), w))
	cut := false
	if words := m.lastWords(s); words != "" {
		agent := s.Agent
		if agent == "" {
			agent = "it"
		}
		out = append(out, "", " "+st.label.Render(strings.ToUpper(agent)+" SAID"))
		lines := wrapPlain(words, w-3)
		if len(lines) > said {
			lines = append(lines[:said-1], ansi.Truncate(lines[said-1], w-4, "")+"…")
			cut = true
		}
		for _, l := range lines {
			out = append(out, " "+st.text.Render(l))
		}
	}
	out = append(out, "")
	field := func(name, value string) {
		out = append(out, fit(" "+st.label.Render(pad(name, 9))+value, w))
	}
	if s.Context == "known" {
		field("context", m.meter(s.ContextPct, 10, m.contextStyle(s))+" "+m.contextCell(s))
	} else {
		field("context", st.faint.Render(contextWord(s.Context)))
	}
	if p := s.Progress; p != nil {
		field("plan", m.progressCell(p, 10)+"  "+st.text.Render(p.Step))
	}
	out = append(out, m.shipField(s, w)...)
	if s.Agents > 0 {
		field("agents", st.text.Render(plural(s.Agents, "subagent")+" running"))
	}
	if len(s.Shells) > 0 {
		shells := plural(len(s.Shells), "shell")
		if old := s.Shells[0].Started; !old.IsZero() {
			shells += " · oldest " + sample.Human(m.now().Sub(old))
		}
		field("shells", st.text.Render(shells))
		for _, sh := range s.Shells {
			out = append(out, fit("   "+st.muted.Render(strconv.Itoa(sh.PID)+"  "+sh.Command), w))
		}
	}
	switch {
	case s.Do != "" && s.Why != "":
		field("why", st.text.Render(s.Do+": "+s.Why))
	case s.Why != "":
		field("why", st.text.Render(s.Why))
	}
	facts := m.facts(s)
	for _, l := range wrapPlain(strings.Join(facts, " · "), w-2) {
		out = append(out, " "+st.faint.Render(l))
	}
	if s.FromPane {
		for _, l := range wrapPlain("no hooks for "+s.Name+": busy and idle are read from the pane", w-2) {
			out = append(out, " "+st.faint.Render(l))
		}
	}
	keys, _ := m.detailKeys(s)
	return append(out, "", fit(keys, w)), cut
}

// detailKeys are the keys under a session's detail, and where a tap
// presses each, in columns of the pane.
func (m Model) detailKeys(s sample.Session) (string, []span) {
	var hs []hint
	if s.Pane != "" {
		hs = append(hs, hint{"a", m.peekWord(s), "a"}, hint{"⏎", "open it", "enter"}, hint{"A", "switch agent", "A"})
	}
	if s.Age() == "stale" {
		hs = append(hs, hint{"x", "end it", "x"})
	}
	keys, spans := m.hintRow(hs, 3)
	return " " + keys, shift(spans, 1)
}

// facts are the figures of a session the rows have no room for.
func (m Model) facts(s sample.Session) []string {
	var facts []string
	if s.Model != "" {
		facts = append(facts, s.Model)
	}
	switch {
	case s.Tokens > 0 && s.Window > 0:
		facts = append(facts, fmt.Sprintf("%s / %s tokens", sample.Kilo(s.Tokens), sample.Kilo(s.Window)), "+"+sample.Kilo(s.Burn30m)+" in 30 min")
	case s.Tokens > 0 && s.Agent == "claude":
		facts = append(facts, sample.Kilo(s.Tokens)+" tokens · no window known: run matchblox setup, or add the model to [sessions.windows]")
	case s.Tokens > 0:
		facts = append(facts, sample.Kilo(s.Tokens)+" tokens · no window known: add the model to [sessions.windows]")
	}
	if a := accountCell(s); a != "" {
		facts = append(facts, a)
	}
	facts = append(facts, fmt.Sprintf("pid %d", s.PID), plural(s.Procs, "process"))
	if !s.Started.IsZero() {
		facts = append(facts, "started "+sample.Human(m.now().Sub(s.Started))+" ago")
	}
	return facts
}

func (m Model) contextStyle(s sample.Session) lipgloss.Style {
	switch {
	case m.full().Reached(s):
		return m.st.neg
	case s.ContextPct >= 70:
		return m.st.warn
	}
	return m.st.muted
}

// meter is a bar of n cells for a percent.
func (m Model) meter(pct float64, n int, style lipgloss.Style) string {
	full := min(max(int(pct/100*float64(n)+0.5), 0), n)
	return style.Render(strings.Repeat("▰", full)) + m.st.faint.Render(strings.Repeat("▱", n-full))
}

// wrapPlain breaks text into lines of at most w cells, a word too long
// for a line cut into pieces.
func wrapPlain(s string, w int) []string {
	w = max(w, 4)
	var out []string
	line := ""
	for _, word := range strings.Fields(s) {
		for lipgloss.Width(word) > w {
			if line != "" {
				out, line = append(out, line), ""
			}
			head := ansi.Truncate(word, w, "")
			out, word = append(out, head), word[len(head):]
		}
		switch {
		case line == "":
			line = word
		case lipgloss.Width(line)+1+lipgloss.Width(word) <= w:
			line += " " + word
		default:
			out, line = append(out, line), word
		}
	}
	if line != "" {
		out = append(out, line)
	}
	return out
}

// spread puts left and right on one line of w cells; left is cut first.
func spread(left, right string, w int) string {
	lw, rw := lipgloss.Width(left), lipgloss.Width(right)
	if lw+rw+1 > w {
		return fit(ansi.Truncate(left, max(w-rw-1, 0), "…")+" "+right, w)
	}
	return left + strings.Repeat(" ", w-lw-rw) + right
}

// padTo pads styled text to w cells, cutting it when it is wider.
func padTo(s string, w int) string {
	s = ansi.Truncate(s, w, "…")
	return s + strings.Repeat(" ", max(w-lipgloss.Width(s), 0))
}

// padLeft right-aligns styled text in w cells.
func padLeft(s string, w int) string {
	return strings.Repeat(" ", max(w-lipgloss.Width(s), 0)) + s
}
