package app

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/blox-eng/matchblox/internal/proto"
	"github.com/blox-eng/matchblox/internal/sample"
)

// series keeps the last n machine samples for the dock's sparkline and the
// Machine tab's chart.
type series struct {
	n                            int
	cpu, mem, net, io, lat, load []float64
}

func newSeries(n int) *series { return &series{n: n} }

func (s *series) push(m sample.Machine) {
	add := func(xs *[]float64, v float64) {
		*xs = append(*xs, v)
		if len(*xs) > s.n {
			*xs = (*xs)[len(*xs)-s.n:]
		}
	}
	add(&s.cpu, m.CPU)
	if m.MemTotal > 0 {
		add(&s.mem, float64(m.MemTotal-m.MemAvail))
	}
	add(&s.net, m.NetRx+m.NetTx)
	add(&s.io, m.DiskRead+m.DiskWrite)
	add(&s.lat, float64(m.Latency))
	add(&s.load, m.Load[0])
}

var blocks = []rune("▁▂▃▄▅▆▇█")

// spark draws the last w values scaled to their own max (or ceil if larger).
func spark(xs []float64, w int, ceil float64) string {
	if len(xs) > w {
		xs = xs[len(xs)-w:]
	}
	top := ceil
	for _, x := range xs {
		top = max(top, x)
	}
	var b strings.Builder
	for _, x := range xs {
		i := 0
		if top > 0 {
			i = min(int(x/top*float64(len(blocks)-1)+0.5), len(blocks)-1)
		}
		b.WriteRune(blocks[i])
	}
	return b.String()
}

func (m Model) render() string {
	if m.quitting {
		return ""
	}
	w := max(m.width, minWidth)
	if m.splashing() {
		return m.splashView(w)
	}
	if m.pair.owns() {
		return m.pairView(w)
	}
	if m.night() {
		m.st = nightStyles()
		f := m.frame(w)
		if n := strings.Count(f, "\n") + 1; n < m.height {
			f += strings.Repeat("\n", m.height-n) // the night covers every row
		}
		return paint(f, m.st.ground, w)
	}
	return m.frame(w)
}

func (m Model) frame(w int) string { return strings.Join(m.draw(w).lines, "\n") }

// span is a tap target on a line: columns x0 to x1, and the key it presses.
type span struct {
	x0, x1 int
	press  string
}

func spanAt(spans []span, x int) (span, bool) {
	for _, s := range spans {
		if x >= s.x0 && x < s.x1 {
			return s, true
		}
	}
	return span{}, false
}

// drawn is a frame and where its parts are, so a tap finds what it
// hit: the header, the tabs, the body rows, the dock and the key line.
type drawn struct {
	lines   []string
	waits   span // the header's "● N wait for you"
	tabs    []span
	bodyTop int   // the line of the first body row
	rows    []int // the row each body line shows; -1 none, headerRow
	listW   int   // the list's columns; the detail pane is right of them
	dockAt  int   // the dock's first line; -1 when it is folded
	keysAt  int   // the key line; -1 when there is none
	keys    []span
	// The answer peek: its first line and column, its width, the key a tap
	// on each of its lines presses, and the keys of its footer.
	peekAt, peekX, peekW int
	peekRows             []string
	peekKeys             []span
	peekInline           bool // the peek is the detail pane, the list beside it live
	// The dock: its height and the columns of the stoker's panel. full:
	// the dock shows all it has; paneFull: so does the right pane.
	dockH, stx0, stx1 int
	full, paneFull    bool
	// The launch sheet's go chips: their line, and the key a tap on each
	// one presses.
	sheetGoesAt int
	sheetGoes   []span
	// The New session form's agent chips: their line, and the agent a tap
	// on each picks.
	formAgentsAt int
	formAgents   []span
	stokerAt     span // the header's stoker word
	// The selected session's own keys, on its opened row or under its
	// detail: their line, and the key a tap on each presses.
	actKeysAt int
	actKeys   []span
}

// draw lays out the screen, top to bottom: the header, the tabs, a
// hairline, the body (on an opened phone and a desk the list and the
// detail side by side), the dock when it fits, then a hairline and the
// key line (design/0006 §4.1).
func (m Model) draw(w int) drawn {
	head, waits, stokerAt := m.header(w)
	tabs, tabSpans := m.tabsLine(w)
	sc := drawn{lines: []string{head, tabs, m.st.rule.Render(strings.Repeat("─", w))}, waits: waits, stokerAt: stokerAt, tabs: tabSpans,
		bodyTop: 3, listW: w, dockAt: -1, keysAt: -1, actKeysAt: -1}
	switch {
	case m.mismatch:
		sc.lines = append(sc.lines, "", m.mismatchText(w))
		return sc
	case m.blocked != nil:
		sc.lines = append(sc.lines, "", m.blockedText(w))
		return sc
	case !m.have:
		why := m.waitingWhy()
		if len(sc.lines)+markH/2+3 <= m.height {
			sc.lines = append(append(sc.lines, ""), m.markLines(w)...)
		}
		sc.lines = append(sc.lines, "", strings.Repeat(" ", max((w-lipgloss.Width(why))/2, 0))+m.st.faint.Render(why))
		if m.opt.Layered {
			keys := "esc hosts  q quit"
			sc.lines = append(sc.lines, "", strings.Repeat(" ", max((w-len(keys))/2, 0))+m.st.muted.Render(keys))
		}
		return sc
	}
	full := max(m.height-5, 1)
	listW, detailW := m.panes(w)
	sc.listW = listW
	b := m.body(listW)
	_, _, peeking := m.peekNow()
	inline := peeking && detailW > 0 // the peek takes the detail pane
	paneFull := true
	paneLines := func(rows int) []string {
		switch {
		case detailW == 0 || inline:
			return nil
		}
		lines, all := m.detailPane(detailW, rows)
		paneFull = all
		return lines
	}
	detail := paneLines(0)
	var d docked
	if m.tab != tabMachine && m.tab != tabStoker {
		d = m.dock(w, -1)
	}
	room := full
	if len(d.lines) > 0 && max(len(b.lines), len(detail))+len(d.lines)+1 <= full {
		// The dock fits; it grows into the rows the body leaves free.
		d = m.dock(w, full-max(len(b.lines), len(detail))-1)
		room -= len(d.lines) + 1
	} else {
		d = docked{}
	}
	sc.full = d.full || m.tab == tabMachine || m.tab == tabStoker
	detail = paneLines(room)
	sc.paneFull = paneFull && len(detail) <= room
	if inline {
		var press []string
		var spans []span
		detail, press, spans = m.peekBox(detailW, room, false, true)
		sc.peekAt, sc.peekX, sc.peekW, sc.peekRows, sc.peekKeys, sc.peekInline = sc.bodyTop, listW+1, detailW, press, spans, true
	}
	vis := windowed(b, room, m.rowIndex())
	for i := range room {
		line, row := "", -1
		if i < len(vis.lines) {
			line, row = vis.lines[i], vis.rows[i]
		}
		if detailW > 0 {
			right := ""
			if i < len(detail) {
				right = detail[i]
			}
			line = fit(line, listW) + m.st.hair.Render("│") + fit(right, detailW)
		}
		sc.lines, sc.rows = append(sc.lines, line), append(sc.rows, row)
	}
	m.markActKeys(&sc, vis, detail, room, detailW, inline)
	if len(d.lines) > 0 {
		sc.dockAt, sc.dockH, sc.stx0, sc.stx1 = len(sc.lines)+1, len(d.lines), d.stx0, d.stx1
		sc.lines = append(append(sc.lines, m.st.hair.Render(strings.Repeat("─", w))), d.lines...)
	}
	keys, keySpans := m.keyLine(w)
	sc.keysAt, sc.keys = len(sc.lines)+1, keySpans
	sc.lines = append(sc.lines, m.st.hair.Render(strings.Repeat("─", w)), keys)
	switch {
	case m.consenting():
		m.overlayConsent(&sc, w)
	case m.sheet != nil:
		m.overlaySheet(&sc, w)
	case m.form != nil:
		m.overlayForm(&sc, w)
	case peeking && !inline:
		m.overlayPeek(&sc, w)
	}
	return sc
}

// markActKeys finds the selected session's own keys: the last line of its
// opened row on a phone, the last line of its detail beside the list.
func (m Model) markActKeys(sc *drawn, vis body, detail []string, room, detailW int, inline bool) {
	s, ok := m.selectedSession()
	if !ok || m.tab != tabSessions {
		return
	}
	switch {
	case detailW > 0 && !inline && len(detail) <= room:
		_, spans := m.detailKeys(s)
		sc.actKeysAt, sc.actKeys = sc.bodyTop+len(detail)-1, shift(spans, sc.listW+1)
	case detailW == 0 && m.layoutAt(m.width) == Narrow:
		last := -1
		for i, r := range vis.rows {
			if r == m.rowIndex() {
				last = i
			}
		}
		if last > 0 && vis.rows[last-1] == m.rowIndex() { // opened: more than its one line
			_, spans := m.openedKeys(s, sc.listW)
			sc.actKeysAt, sc.actKeys = sc.bodyTop+last, spans
		}
	}
}

// panes splits Sessions into the list and the detail: at the middle on an
// opened phone, the list's ≈78 columns on a desk. A phone and the other
// tabs have one column, and so has a door's diff while it waits for its
// y: it shows whole.
func (m Model) panes(w int) (list, detail int) {
	if _, preview := m.previewDoor(); m.tab != tabSessions && m.tab != tabShips || m.layoutAt(w) == Narrow || preview {
		return w, 0
	}
	at := splitAt(w, m.layoutAt(w))
	return at, w - at - 1
}

// splitAt is the column the list and the detail split at, and the dock's
// usage and machine under them.
func splitAt(w int, l Layout) int {
	if l == Medium {
		return (w - 1) / 2
	}
	return min(78, w-41)
}

// windowed is the part of a body that fits in room lines: it scrolls so
// every line of the selected row is on the screen, with a line to spare.
func windowed(b body, room, sel int) body {
	if len(b.lines) <= room {
		return b
	}
	first, last := -1, -1
	for i, r := range b.rows {
		if r == sel && r >= 0 {
			if first < 0 {
				first = i
			}
			last = i
		}
	}
	start := 0
	if last >= room {
		start = min(last-room+2, len(b.lines)-room)
	}
	if first >= 0 && first < start {
		start = max(first-1, 0)
	}
	return body{lines: b.lines[start : start+room], rows: b.rows[start : start+room]}
}

// header is one line: the mark, the name and the host on the left; who
// waits and the stoker on the right, or a toast after an action. The
// spans are what a tap on who waits, and on the stoker's word, presses.
func (m Model) header(w int) (string, span, span) {
	st := m.st
	left := " " + st.accent.Render("▰") + " " + st.text.Render("matchblox")
	host := m.opt.Host
	if host == "" {
		host = m.host.Host
	}
	lost := ""
	if m.lost && m.have {
		// What shows is the last state the host sent.
		lost = st.faint.Render(" · ") + st.warn.Render("stale")
	}
	if !m.have {
		if host != "" {
			left += st.faint.Render(" · ") + st.muted.Render(host)
		}
		return fit(left, w), span{}, span{}
	}
	if m.toast != "" {
		// A long toast is cut, never the name; the host stays when the
		// toast still has room.
		if host != "" && w-lipgloss.Width(left+lost)-len(host)-6 >= 20 {
			left += st.faint.Render(" · ") + st.muted.Render(host)
		}
		room := max(w-lipgloss.Width(left+lost)-3, 1)
		return spread(left+lost, st.accent.Render(ansi.Truncate(m.toast, room, "…"))+" ", w), span{}, span{}
	}
	n := 0
	for _, s := range m.all.sessions {
		if m.groupOf(s) == groupWaits {
			n++
		}
	}
	stoker := m.stokerBadge(m.layoutAt(w) == Narrow)
	// Short of room the host goes first (a phone), then the words of who
	// waits; the count stays.
	waits := []string{""}
	if n > 0 {
		waits = []string{fmt.Sprintf("● %d wait for you", n), fmt.Sprintf("● %d", n)}
	}
	lefts := []string{left}
	if host != "" {
		lefts = []string{left + st.faint.Render(" · ") + st.muted.Render(host), left}
	}
	var right string
	for _, word := range waits {
		right = stoker + " "
		switch {
		case word != "" && stoker != "":
			right = st.accent.Render(word) + "  " + right
		case word != "":
			right = st.accent.Render(word) + " "
		}
		hit := span{}
		if word != "" {
			hit = span{w - lipgloss.Width(right), w - lipgloss.Width(right) + lipgloss.Width(word), "waits"}
		}
		for _, l := range lefts {
			if lipgloss.Width(l+lost)+lipgloss.Width(right)+1 <= w {
				return spread(l+lost, right, w), hit, m.stokerHit(stoker, w)
			}
		}
	}
	return spread(left+lost, right, w), span{}, m.stokerHit(stoker, w)
}

// stokerHit is where the header's stoker word is, the line's last word:
// a tap opens the stoker agent.
func (m Model) stokerHit(word string, w int) span {
	if !m.hasAgent() || word == "" {
		return span{}
	}
	return span{w - 1 - lipgloss.Width(word), w - 1, "stoker"}
}

// stokerBadge is the stoker's word: off in faint, on and the night in the
// stoker's token, resting in muted. A phone drops the night's end.
func (m Model) stokerBadge(short bool) string {
	st := m.st
	switch {
	case m.stoker == nil:
		return ""
	case m.stokerAsks() && short:
		return st.accent.Render("stoker waits")
	case m.stokerAsks():
		return st.accent.Render("stoker waits for you")
	case !m.stoker.On:
		return st.faint.Render("stoker off")
	case m.resting():
		return st.muted.Render("stoker rests")
	case !m.stoker.Until.IsZero() && short:
		return st.stoker.Render("☾ night")
	case !m.stoker.Until.IsZero():
		return st.stoker.Render("☾ " + stokerWordOf(m.stoker.Mode))
	}
	return st.stoker.Render("stoker")
}

// tabsLine names every tab: the open one uppercase in the accent with its
// rule, the others lowercase in faint, a count after a name when it means
// something. When the names do not fit, the tabs far from the open one
// shorten to their digit, the nearest last.
func (m Model) tabsLine(w int) (string, []span) {
	alert := m.alert()
	aw := lipgloss.Width(alert)
	try := func(near int, gap string) (string, []span) {
		line, x := " ", 1
		var spans []span
		for i := range views {
			if i > 0 {
				line += gap
				x += len(gap)
			}
			label := m.tabLabel(i, i-m.tab <= near && m.tab-i <= near)
			spans = append(spans, span{x, x + lipgloss.Width(label), strconv.Itoa(i + 1)})
			line += label
			x += lipgloss.Width(label)
		}
		return line, spans
	}
	var line string
	var spans []span
	for _, t := range []struct {
		near int
		gap  string
	}{{len(views), "    "}, {len(views), "   "}, {len(views), "  "}, {2, "  "}, {1, "  "}, {0, "  "}, {-1, "  "}} {
		if line, spans = try(t.near, t.gap); lipgloss.Width(line)+aw+2 <= w {
			break
		}
	}
	if alert != "" {
		line += strings.Repeat(" ", max(w-lipgloss.Width(line)-aw-1, 2)) + alert
	}
	return fit(line, w), spans
}

// tabLabel is a tab's name and count, or its digit when it is shortened.
func (m Model) tabLabel(i int, named bool) string {
	st := m.st
	name, count := views[i].name, ""
	switch i {
	case tabSessions:
		count = strconv.Itoa(len(m.all.sessions))
	case tabShips:
		if m.shipReport != nil && m.shipReport.InFlight > 0 {
			count = strconv.Itoa(m.shipReport.InFlight)
		}
	case tabMachine:
		if len(m.all.orphans) > 0 {
			count = "!" // a detached busy loop burns
		}
	case tabStoker:
		if n := m.stokerNeeds(); n > 0 {
			count = strconv.Itoa(n)
		}
	}
	if !named {
		name = strconv.Itoa(i + 1)
	}
	switch {
	case i == m.tab && count == "":
		return st.tabActive.Render(strings.ToUpper(name))
	case i == m.tab:
		return st.tabActive.Render(strings.ToUpper(name)) + st.accent.Render(" "+count)
	case count == "!":
		return st.faint.Render(name) + st.neg.Render(" !")
	case count != "" && named:
		return st.faint.Render(name + " " + count)
	}
	return st.faint.Render(name)
}

func (m Model) alert() string {
	n := len(m.snap.Alerts)
	if n == 0 {
		return ""
	}
	if m.layoutAt(m.width) == Narrow {
		return m.st.warn.Render(fmt.Sprintf("▲ %d", n))
	}
	alert := m.st.warn.Render(fmt.Sprintf("▲ %d alert", n))
	if n > 1 {
		alert += m.st.warn.Render("s")
	}
	return alert
}

// keyLine is the bottom line: the keys of the open tab, each a tap target,
// or what the person acts on now: an answer, a search, the take-over
// question, a pending step and its confirm, or what the last key said.
func (m Model) keyLine(w int) (string, []span) {
	st := m.st
	if m.searching {
		return fit(" "+st.label.Render("/ ")+st.text.Render(m.filter)+st.accent.Render("▏")+"  "+st.faint.Render("⏎ keep  esc clear"), w), nil
	}
	if m.consenting() {
		return "", nil // the box says it all
	}
	if m.held != nil {
		switch {
		case m.flash != "":
			return fit(" "+st.text.Render("Take over? ")+st.warn.Render(m.flash), w), nil
		case m.layoutAt(w) == Narrow:
			return fit(" "+st.text.Render("Stoker on. Take over? ")+st.accent.Render("y")+st.muted.Render(" rest · ")+
				st.accent.Render("n")+st.muted.Render(" keep"), w), nil
		}
		return fit(" "+st.text.Render("The stoker is on. Take over?  ")+st.accent.Render("y")+st.muted.Render(" rests it while you work · ")+
			st.accent.Render("n")+st.muted.Render(" keeps it on"), w), nil
	}
	if m.pending != nil {
		confirm := "⏎ run  esc cancel"
		if m.pending.destructive {
			confirm = st.neg.Render("y") + st.muted.Render(" run  any other key cancels")
		}
		if !m.previewSeen() {
			confirm = st.muted.Render("↓ the rest, then ") + st.neg.Render("y")
		}
		return fit(" "+st.label.Render("RUN ")+st.text.Render(m.pending.String())+"   "+st.muted.Render(confirm), w), nil
	}
	if m.pair.strips() && (m.pair.beat == 4 || m.flash == "") {
		return fit(" "+st.text.Render(m.pair.line), w), nil
	}
	if m.flash != "" {
		return fit(" "+st.text.Render(m.flash), w), nil
	}
	hints := m.view().keys(m)
	if m.layoutAt(w) == Wide {
		hints = append([]hint{{"↑↓", "move", ""}}, hints...)
		hints = append(hints, hint{"/", "find", "/"}, hint{"q", "quit", ""})
	}
	line, x := " ", 1
	if m.filter != "" {
		lead := "/ " + m.filter + " · esc clears  "
		line += st.muted.Render(lead)
		x += lipgloss.Width(lead)
	}
	// The age goes before a spare key does; a spare key goes before a key
	// of the row.
	if fit := fitHints(hints, w-x-lipgloss.Width("sampled 59m ago ")); hintsWidth(fit) <= w-x-lipgloss.Width("sampled 59m ago ") {
		hints = fit
	} else {
		hints = fitHints(hints, w-x)
	}
	var spans []span
	for i, h := range hints {
		if i > 0 {
			line += "  "
			x += 2
		}
		part := st.accent.Render(h.key) + st.muted.Render(" "+h.label)
		if h.press != "" {
			spans = append(spans, span{x, x + lipgloss.Width(part), h.press})
		}
		line += part
		x += lipgloss.Width(part)
	}
	right := st.faint.Render("sampled " + sample.Human(m.now().Sub(m.snap.At)) + " ago ")
	if lipgloss.Width(line)+lipgloss.Width(right)+1 > w {
		return fit(line, w), spans // the keys matter more than the age
	}
	return line + strings.Repeat(" ", w-lipgloss.Width(line)-lipgloss.Width(right)) + right, spans
}

// spareKeys are the keys a short key line drops first, in that order:
// each one is reached another way (a tab tap, the dock, the docs).
var spareKeys = []string{"A", "↑↓", "q", "/", "p", "z", "f"}

// fitHints drops spare keys until the hints fit in room columns. The
// row's own keys always stay: a line that is still too long is cut.
func fitHints(hints []hint, room int) []hint {
	for _, k := range spareKeys {
		if hintsWidth(hints) <= room {
			break
		}
		hints = slices.DeleteFunc(slices.Clone(hints), func(h hint) bool { return h.key == k })
	}
	return hints
}

func hintsWidth(hs []hint) int {
	n := 0
	for i, h := range hs {
		if i > 0 {
			n += 2
		}
		n += lipgloss.Width(h.key) + 1 + lipgloss.Width(h.label)
	}
	return n
}

// accountCell is the provider's mark and the account label: "✻ a@b.c · Max".
// A provider without a mark keeps its name.
func accountCell(s sample.Session) string {
	p := providerMarks[s.Provider]
	if p == "" {
		p = s.Provider
	}
	switch {
	case p == "":
		return s.Account
	case s.Account == "":
		return p
	case providerMarks[s.Provider] != "":
		return p + " " + s.Account
	}
	return p + " · " + s.Account
}

// idleCell is how long a session sat idle, or, while the plan's limit stops
// it, when it can work again: 19:50 within a day, else the weekday.
func (m Model) idleCell(s sample.Session) string {
	switch {
	case !s.Stopped():
		return sample.Human(s.Idle)
	case s.Out.Sub(m.now()) < 24*time.Hour:
		return s.Out.Local().Format("15:04")
	}
	return s.Out.Local().Format("Mon")
}

func (m Model) full() sample.Full {
	return sample.Full{Pct: m.opt.CompactAt, Tokens: m.opt.CompactTokens}
}

// plural is "1 shell", "2 shells".
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	if strings.HasSuffix(noun, "s") {
		return strconv.Itoa(n) + " " + noun + "es"
	}
	return strconv.Itoa(n) + " " + noun + "s"
}

// contextWord names a context use that has no figure: "fresh" before the
// first turn, "not measured" for an agent without an adapter.
func contextWord(c string) string {
	if c == "unmeasured" {
		return "not measured"
	}
	return c
}

// prOf is the open pull request of the worktree a directory is in: the
// worktree with the longest path that holds it.
func (m Model) prOf(cwd string) *proto.PR {
	if m.git == nil || cwd == "" {
		return nil
	}
	var pr *proto.PR
	best := 0
	for _, r := range m.git.Repos {
		for _, wt := range r.Worktrees {
			if (cwd == wt.Path || strings.HasPrefix(cwd, wt.Path+"/")) && len(wt.Path) > best {
				pr, best = wt.PR, len(wt.Path)
			}
		}
	}
	return pr
}

// place names a pane the way the tmux status bar does (session:window.pane),
// falling back to its id when tmux did not list it.
func place(target, id string) string {
	if target != "" {
		return target
	}
	if id == "" {
		return "outside tmux"
	}
	return id
}

func pad(s string, w int) string {
	s = ansi.Truncate(s, w-1, "…")
	return s + strings.Repeat(" ", max(w-lipgloss.Width(s), 0))
}

func fit(s string, w int) string {
	s = ansi.Truncate(s, w, "…")
	return s + strings.Repeat(" ", max(w-lipgloss.Width(s), 0))
}

func gib(b uint64) string { return fmt.Sprintf("%.0fG", float64(b)/(1<<30)) }

// size renders bytes or bytes per second compactly: 512B, 12K, 3.4M, 1.2G.
func size(b float64) string {
	switch {
	case b >= 1<<30:
		return fmt.Sprintf("%.1fG", b/(1<<30))
	case b >= 1<<20:
		return fmt.Sprintf("%.1fM", b/(1<<20))
	case b >= 1<<10:
		return fmt.Sprintf("%.0fK", b/(1<<10))
	}
	return fmt.Sprintf("%.0fB", b)
}

// now is the console's clock.
func (m Model) now() time.Time {
	if m.opt.Now != nil {
		return m.opt.Now()
	}
	return time.Now()
}

// mismatchText names both versions and the command for the older side
// (design §6).
func (m Model) mismatchText(w int) string {
	host := m.host.Host
	if host == "" {
		host = "the host"
	}
	side := "update the console"
	if m.host.Version < proto.Version {
		side = "update " + host
	}
	cmd := installCmd
	if a := m.connectAction(); a != nil && m.host.Version < proto.Version {
		cmd = shellLine(a.steps[0])
	}
	lines := []string{
		fit(" "+m.st.text.Render(fmt.Sprintf("the console is version %d, the host is version %d", proto.Version, m.host.Version)), w),
		"",
		fit(" "+m.st.label.Render(side+": ")+m.st.text.Render(cmd), w),
		"",
		m.connectKeys(w),
	}
	return strings.Join(lines, "\n")
}
