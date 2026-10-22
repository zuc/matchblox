package app

import (
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/blox-eng/matchblox/internal/proto"
	"github.com/blox-eng/matchblox/internal/queue"
	"github.com/blox-eng/matchblox/internal/said"
	"github.com/blox-eng/matchblox/internal/sample"
)

// The peek is a panel over the dimmed screen: what a session said last and
// the answer or message to it (design/0006 §6). It is the review: what it
// shows goes to the pane named in its title on ⏎, with no second confirm.

// peek is the open peek.
type peek struct {
	pane string
	// The question tool's call: the question on screen, the row under the
	// cursor (the typing row after the options; -1 none), the options
	// ticked (from 1), and the answers to the questions before it.
	askID   string
	q, at   int
	ticks   map[int]bool
	choices []said.Choice
	text    string // the typing line
}

// peekView is the session of the open peek and what it asks: the question
// tool's call, or a permission prompt, which is answered in its pane.
type peekView struct {
	s      sample.Session
	ask    *said.Ask
	permit bool
}

// peekNow is the open peek's session, with the peek moved to a new call
// when the session asks another.
func (m Model) peekNow() (peekView, peek, bool) {
	if m.peek == nil {
		return peekView{}, peek{}, false
	}
	s, ok := m.sessionIn(m.peek.pane)
	if !ok {
		return peekView{}, peek{}, false
	}
	p := *m.peek
	v := peekView{s: s, ask: s.Ask}
	if s.Ask == nil {
		it, _ := m.waitOf(s)
		v.permit = it.State == queue.StatePermission
	} else if p.askID != s.Ask.ID {
		// A new call: nothing is picked until the person picks it.
		p = peek{pane: p.pane, askID: s.Ask.ID, at: -1}
	}
	return v, p, true
}

// peekTarget is the pane `a` opens the peek on: the selected session,
// whatever it does, when it has a pane to type into. Why says why there is
// none.
func (m Model) peekTarget() (pane, why string) {
	if m.tab == tabStoker {
		return m.stokerPeekTarget()
	}
	s, ok := m.selectedSession()
	switch {
	case !ok:
		return "", ""
	case s.Pane == "":
		return "", s.Name + " runs outside tmux: there is no pane to type into"
	}
	return s.Pane, ""
}

// peekWord is the key line's word for `a` on a session: answer one that
// waits, message any other.
func (m Model) peekWord(s sample.Session) string {
	if _, waits := m.waitOf(s); waits {
		return "answer"
	}
	return "message"
}

func (m *Model) openPeek(pane string) {
	m.peek, m.flash = &peek{pane: pane}, ""
	if s, ok := m.sessionIn(pane); ok {
		m.chose, m.selPID = true, s.PID
		if s.Ask != nil {
			m.peek.askID = s.Ask.ID
		}
	}
}

// peekQueue is every other session that waits, in the list's order after
// the one in pane: the ones a send goes to.
func (m Model) peekQueue(pane string) []sample.Session {
	var out []sample.Session
	for _, s := range m.peekRing(pane) {
		if s.Pane != pane && m.groupOf(s) == groupWaits {
			out = append(out, s)
		}
	}
	return out
}

// peekRing is every session with a pane, in the list's order (who waits
// first), starting after the one in pane and ending on it: the ring ← and
// → move along.
func (m Model) peekRing(pane string) []sample.Session {
	ss := m.snap.Sessions
	at := slices.IndexFunc(ss, func(s sample.Session) bool { return s.Pane == pane })
	var out []sample.Session
	for k := 1; k <= len(ss); k++ {
		if s := ss[(at+k+len(ss))%len(ss)]; s.Pane != "" {
			out = append(out, s)
		}
	}
	return out
}

// peekQuiet is how long the peek takes no key after it moved to another
// session: a second Enter, a key repeat or a buffered tap meant for the
// question before must not answer one the person has not read.
const peekQuiet = 400 * time.Millisecond

// peekNext moves the peek to the next session that waits, or closes it.
func (m Model) peekNext(pane string) Model {
	m.peek = nil
	if next := m.peekQueue(pane); len(next) > 0 {
		m = m.peekTo(next[0].Pane)
	}
	return m
}

// peekMove moves the peek along the ring, by one either way, wrapping.
func (m Model) peekMove(pane string, back bool) Model {
	ring := m.peekRing(pane)
	if len(ring) < 2 {
		return m
	}
	to := ring[0]
	if back {
		to = ring[len(ring)-2]
	}
	return m.peekTo(to.Pane)
}

// peekCount is where the peek is among the n sessions it moves along:
// "3/9", in the list's order.
func (m Model) peekCount(pane string, n int) string {
	at := 0
	for _, s := range m.snap.Sessions {
		if s.Pane == "" {
			continue
		}
		at++
		if s.Pane == pane {
			break
		}
	}
	return fmt.Sprintf("%d/%d", at, n)
}

// peekTo opens the peek on another session: nothing is picked, and keys
// wait a moment.
func (m Model) peekTo(pane string) Model {
	m.openPeek(pane)
	m.peek.at, m.peekQuiet = -1, time.Now().Add(peekQuiet)
	return m
}

// peekKey is a key while the peek is open. Every printable key is text on
// the typing line, q too.
func (m Model) peekKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	v, p, ok := m.peekNow()
	if !ok {
		m.peek, m.flash = nil, "the session is gone"
		return m, nil
	}
	n := 0
	var q said.Question
	if v.ask != nil {
		q = v.ask.Questions[p.q]
		n = len(q.Options)
	}
	typing := v.ask == nil || p.at == n
	key := k.String()
	if time.Now().Before(m.peekQuiet) && key != "esc" && key != "ctrl+c" {
		return m, nil
	}
	switch {
	case key == "esc":
		m.peek = nil
		return m, nil
	case key == "ctrl+c":
		return m.quit()
	case key == "ctrl+o":
		m.peek = nil
		if a := m.forHost(m.jump(v.s.Pane)); a != nil {
			return m.confirm(*a)
		}
		return m, nil
	case key == "right" || key == "tab":
		return m.peekMove(p.pane, false), nil
	case key == "left" || key == "shift+tab":
		return m.peekMove(p.pane, true), nil
	case v.permit:
		if key == "enter" {
			m.peek = nil
			if a := m.forHost(m.jump(v.s.Pane)); a != nil {
				return m.confirm(*a)
			}
		}
		return m, nil
	case key == "enter" && v.ask == nil:
		if strings.TrimSpace(p.text) == "" {
			return m, nil
		}
		rec := "message:"
		if _, waits := m.waitOf(v.s); waits {
			rec = "answer:"
		}
		return m.sendPeek(v.s, rec+p.pane, p.text)
	case key == "enter":
		c := said.Choice{}
		if q.Multi {
			for i := 1; i <= n; i++ {
				if p.ticks[i] {
					c.Picks = append(c.Picks, i)
				}
			}
		}
		switch {
		case typing && strings.TrimSpace(p.text) == "":
			return m, nil
		case typing:
			c.Text = p.text
		case q.Multi && len(c.Picks) > 0:
		case p.at < 0:
			m.flash = "pick an option"
			return m, nil
		default:
			c.Picks = []int{p.at + 1}
		}
		return m.peekChoose(v, p, c)
	case key == "up" && v.ask != nil:
		p.at = max(p.at-1, 0)
	case key == "down" && v.ask != nil:
		p.at = min(p.at+1, n)
	case key == "backspace":
		p.text = trimLast(p.text)
	case !typing && len(key) == 1 && key[0] >= '1' && key[0] <= '9':
		d := int(key[0] - '0')
		switch {
		case d == n+1:
			p.at = n
		case d > n:
		case q.Multi:
			p.ticks = toggled(p.ticks, d)
		default:
			return m.peekChoose(v, p, said.Choice{Picks: []int{d}})
		}
	case !typing && (key == "space" || key == " ") && q.Multi && p.at >= 0:
		p.ticks = toggled(p.ticks, p.at+1)
	case k.Text != "" && len([]rune(p.text)) < 500:
		p.at, p.text = n, p.text+k.Text
	}
	m.peek = &p
	return m, nil
}

func toggled(ticks map[int]bool, i int) map[int]bool {
	out := map[int]bool{}
	for k, v := range ticks {
		out[k] = v
	}
	out[i] = !out[i]
	return out
}

// peekChoose answers the question on screen: the next one shows, or the
// call's answers go to the pane.
func (m Model) peekChoose(v peekView, p peek, c said.Choice) (tea.Model, tea.Cmd) {
	p.choices = append(slices.Clone(p.choices), c)
	if p.q+1 < len(v.ask.Questions) {
		p.q, p.at, p.ticks, p.text = p.q+1, 0, nil, ""
		m.peek = &p
		return m, nil
	}
	b, err := json.Marshal(said.Answer{ID: v.ask.ID, Choices: p.choices})
	if err != nil {
		m.flash = err.Error()
		return m, nil
	}
	return m.sendPeek(v.s, "pick:"+p.pane, string(b))
}

// sendPeek sends the answer the peek shows and moves the peek on. The
// toast says "sent to" only when the result says every key went; a guard
// that stopped it says why on the key line.
func (m Model) sendPeek(s sample.Session, rec, text string) (tea.Model, tea.Cmd) {
	if m.conn == nil || m.lost {
		m.flash = "not sent, no connection: the answer to " + s.Name
		return m, nil
	}
	cmd := m.send(proto.Act{RecID: rec, Which: "secondary", Confirm: "y", Text: text})
	m.peekSent = maps.Clone(m.peekSent)
	if m.peekSent == nil {
		m.peekSent = map[string]string{}
	}
	m.peekSent["a"+strconv.Itoa(m.acts)] = s.Name
	toast := m.say("sending to " + s.Name + "…")
	m = m.pairActed(action{rec: rec})
	return m.peekNext(s.Pane), tea.Batch(cmd, toast)
}

// peekBox is the peek's lines in bw columns, and the key each line presses
// on a tap (an option's digit; "" none). The last line is the footer, its
// keys in spans from the box's left edge. fill: the peek is the detail
// pane, maxH lines tall: its title says the state, what was said takes the
// room, and the typing line and the footer sit at the bottom.
func (m Model) peekBox(bw, maxH int, phone, fill bool) ([]string, []string, []span) {
	st := m.st
	v, p, _ := m.peekNow()
	iw := bw - 4
	it, waits := m.waitOf(v.s)
	title := st.muted.Render("To ") + st.accent.Bold(true).Render(v.s.Name)
	switch {
	case v.ask != nil || v.permit || waits && it.State == queue.StateQuestion:
		title = st.accent.Bold(true).Render(v.s.Name) + st.muted.Render(" asks")
	case waits:
		title = st.accent.Bold(true).Render(v.s.Name) + st.muted.Render(" said")
	}
	if fill && !waits && v.ask == nil {
		title += st.faint.Render(" · " + m.longWord(v.s)) // asks and said say it already
	}
	if v.ask != nil && len(v.ask.Questions) > 1 {
		title += st.faint.Render(fmt.Sprintf(" · %d of %d", p.q+1, len(v.ask.Questions)))
	}
	// Said is this turn's: a question tool call with no words before it
	// shows only its question.
	text := v.s.Said
	if text == "" && v.ask == nil || v.permit {
		text = m.lastWords(v.s)
	}
	var rest []string // what follows the message, each with its key
	var keys []string
	add := func(press string, lines ...string) {
		rest = append(rest, lines...)
		for range lines {
			keys = append(keys, press)
		}
	}
	typing := v.ask == nil && !v.permit
	if v.ask != nil {
		q := v.ask.Questions[p.q]
		add("", "")
		for _, l := range wrapPlain(q.Question, iw) {
			add("", st.text.Render(l))
		}
		for i, o := range append(slices.Clone(q.Options), said.Option{Label: "Type an answer…"}) {
			on := i == p.at
			mark := "  "
			if on {
				mark = st.accent.Render("› ")
			}
			label := st.muted.Render(o.Label)
			if on {
				label = st.text.Render(o.Label)
			}
			box := ""
			if q.Multi && i < len(q.Options) {
				box = st.faint.Render("[ ] ")
				if p.ticks[i+1] {
					box = st.accent.Render("[✔] ")
				}
			}
			line := mark + st.accent.Render(strconv.Itoa(i+1)) + " " + box + label
			if o.Description != "" && o.Description != o.Label {
				line += st.faint.Render("  " + o.Description)
			}
			line = fit(line, iw)
			if on {
				line = st.selected.Render(line)
			}
			add(strconv.Itoa(i+1), line)
		}
		typing = p.at == len(q.Options)
	}
	switch {
	case typing && fill:
		// The pane is narrow and tall: the line wraps, its last four rows
		// show, so what was typed can be read before it goes.
		lines := wrapPlain(p.text, iw-3)
		if len(lines) == 0 {
			lines = []string{""}
		}
		if strings.HasSuffix(p.text, " ") {
			lines[len(lines)-1] += " "
		}
		lines = lines[max(len(lines)-4, 0):]
		rows := []string{"sep"}
		for i, l := range lines {
			lead := "  "
			if i == 0 {
				lead = st.accent.Render("› ")
			}
			if i == len(lines)-1 {
				l += st.accent.Render("▏")
			}
			rows = append(rows, lead+st.text.Render(l))
		}
		add("", rows...)
	case typing:
		add("", "sep", st.accent.Render("› ")+st.text.Render(ansi.TruncateLeft(p.text, max(lipgloss.Width(p.text)-iw+3, 0), "…"))+st.accent.Render("▏"))
	}
	limit := 9
	if phone {
		limit = 8
	}
	if fill {
		limit = maxH
	}
	limit = max(min(limit, maxH-2-len(rest)), 1)
	var said []string
	for _, l := range strings.Split(text, "\n") {
		said = append(said, wrapPlain(l, iw)...) // a bullet or a table row keeps its own line
	}
	if len(said) > limit {
		// The end is kept: a question closes a reply.
		said = said[len(said)-limit:]
		said[0] = "…" + ansi.TruncateLeft(said[0], max(lipgloss.Width(said[0])-iw+1, 0), "")
	}
	if len(said) == 0 && len(rest) > 0 && rest[0] == "" {
		rest, keys = rest[1:], keys[1:] // no words: the question opens the box
	}
	// The footer: each part a tap target (press "" none), " ─ " between
	// them, two spaces inside the move group (← prev  3/7  next →).
	var foot []footPart
	key := func(k, label, press string) footPart {
		t := st.accent.Render(k)
		if label != "" {
			t += st.muted.Render(" " + label)
		}
		return footPart{t, press, " ─ "}
	}
	switch {
	case v.permit:
		foot = append(foot, key("⏎", "open", "enter"))
	case v.ask != nil && p.q+1 < len(v.ask.Questions):
		foot = append(foot, key("⏎", "question "+strconv.Itoa(p.q+2), "enter"))
	default:
		foot = append(foot, key("⏎", "send", "enter"))
	}
	if v.ask != nil && v.ask.Questions[p.q].Multi && !typing {
		foot = append(foot, key("␣", "tick", "space"))
	}
	if ring := m.peekRing(p.pane); len(ring) > 1 {
		foot = append(foot, footPart{st.accent.Render("←") + st.muted.Render(" prev"), "left", " ─ "})
		count := footPart{st.faint.Render(m.peekCount(p.pane, len(ring))), "", "  "}
		next := footPart{st.muted.Render("next ") + st.accent.Render("→"), "right", "  "}
		width := 3 + 3 + 3 // the corners, the space and esc
		for _, f := range append(foot, count, next) {
			width += lipgloss.Width(f.sep + f.text)
		}
		if width <= bw {
			foot = append(foot, count)
		}
		foot = append(foot, next)
	}
	foot = append(foot, key("esc", "", "esc"))
	// ^o goes to the pane itself, where it fits: the typing line takes
	// every letter.
	jump := key("^o", "jump", "ctrl+o")
	width := 3 + 3 + lipgloss.Width(jump.sep+jump.text) // the corners and the space
	for _, f := range foot {
		width += lipgloss.Width(f.sep + f.text)
	}
	if width <= bw {
		foot = append(foot, jump)
	}

	var lines, press []string
	body := func(l, k string) {
		lines, press = append(lines, l), append(press, k)
	}
	for _, l := range said {
		body(st.text.Render(l), "")
	}
	// The pane's room goes between what was said (or asked) and the typing
	// line, so the typing line sits at the bottom, over the footer.
	padAt := slices.Index(rest, "sep")
	if padAt < 0 {
		padAt = len(rest)
	}
	for i := range len(rest) + 1 {
		if i == padAt && fill {
			for range maxH - 2 - len(said) - len(rest) {
				body("", "")
			}
		}
		if i < len(rest) {
			body(rest[i], keys[i])
		}
	}
	box, spans := boxFrame(st.hair, title, lines, foot, bw)
	return box, append([]string{""}, append(press, "")...), spans
}

// footPart is one part of a box's footer: its text, the key a tap on it
// presses ("" none) and the separator before it.
type footPart struct{ text, press, sep string }

// boxFrame draws the rounded box the peek and the launch sheet share: the
// title in the top edge, each body line between the sides ("sep" is a
// divider) and the footer in the bottom edge, with the footer's keys as
// spans from the box's left edge.
func boxFrame(hair lipgloss.Style, title string, lines []string, foot []footPart, bw int) ([]string, []span) {
	iw := bw - 4
	top := hair.Render("╭─ ") + title + " "
	out := []string{top + hair.Render(strings.Repeat("─", max(bw-lipgloss.Width(top)-1, 0))+"╮")}
	for _, l := range lines {
		if l == "sep" {
			out = append(out, hair.Render("├"+strings.Repeat("─", bw-2)+"┤"))
		} else {
			out = append(out, hair.Render("│ ")+fit(l, iw)+hair.Render(" │"))
		}
	}
	line, x := hair.Render("╰─ "), 3
	var spans []span
	for i, f := range foot {
		if i > 0 {
			line += hair.Render(f.sep)
			x += lipgloss.Width(f.sep)
		}
		if f.press != "" {
			spans = append(spans, span{x, x + lipgloss.Width(f.text), f.press})
		}
		line += f.text
		x += lipgloss.Width(f.text)
	}
	line += " "
	out = append(out, ansi.Truncate(line, bw-1, "")+hair.Render(strings.Repeat("─", max(bw-1-lipgloss.Width(line), 0))+"╯"))
	return out, spans
}

// overlayPeek dims the screen and lays the peek over it: on a phone its
// foot on the line above the key line, full width up to 72 columns and
// centred past that (a phone held upright with a small font); wider, a
// centred box of 62 columns at most.
func (m Model) overlayPeek(sc *drawn, w int) {
	phone := m.layoutAt(w) == Narrow || m.upright()
	bw := min(w, 72)
	if !phone {
		bw = min(62, w-8)
	}
	box, press, spans := m.peekBox(bw, len(sc.lines)-3, phone, false)
	m.overlayBox(sc, w, bw, phone, box, press, spans)
}

// overlayBox dims the screen and lays a box over it, bw columns wide: on a
// phone with its foot on the key line's hairline, else centred.
func (m Model) overlayBox(sc *drawn, w, bw int, phone bool, box, press []string, spans []span) {
	x, y := layBox(m.st, sc.lines, w, bw, phone, box)
	sc.peekAt, sc.peekX, sc.peekW, sc.peekRows, sc.peekKeys = y, x, bw, press, spans
}

// layBox dims lines, a screen with its key line and the hairline over it
// last, and lays box over them in place, bw columns wide: on a phone with
// its foot on that hairline, else centred. It says where the box went.
func layBox(st styles, lines []string, w, bw int, phone bool, box []string) (x, y int) {
	bottom := len(lines) - 2 // the key line's hairline
	x, y = (w-bw)/2, bottom-len(box)
	if !phone {
		y = max((len(lines)-len(box))/2-1, 1)
	}
	y = max(y, 1)
	for i, l := range lines {
		lines[i] = st.faint.Render(ansi.Strip(l))
	}
	for j, b := range box {
		r := y + j
		if r >= len(lines) {
			break
		}
		plain := ansi.Strip(lines[r])
		left := ansi.Truncate(plain, x, "")
		left += strings.Repeat(" ", x-lipgloss.Width(left))
		right := ansi.TruncateLeft(plain, x+bw, "")
		lines[r] = st.faint.Render(left) + b + st.faint.Render(right)
	}
	return x, y
}

// tapPeek is a tap while the peek is open: on an option it picks it, on
// a key of the footer it presses it; outside the box it closes the peek.
func (m Model) tapPeek(ms tea.Mouse, sc drawn) (tea.Model, tea.Cmd) {
	i, dx := ms.Y-sc.peekAt, ms.X-sc.peekX
	if i < 0 || i >= len(sc.peekRows) || dx < 0 || dx >= sc.peekW {
		m.peek = nil
		return m, nil
	}
	press := sc.peekRows[i]
	if i == len(sc.peekRows)-1 {
		s, ok := spanAt(sc.peekKeys, dx)
		if !ok {
			return m, nil
		}
		press = s.press
	}
	if press == "" {
		return m, nil
	}
	if p := *m.peek; press[0] >= '1' && press[0] <= '9' {
		p.at = 0 // a tap on an option picks it, also while the typing line has the cursor
		m.peek = &p
	}
	return m.peekKey(keyPress(press))
}

// keyPress is the key a tap presses.
func keyPress(k string) tea.KeyPressMsg {
	switch k {
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "tab":
		return tea.KeyPressMsg{Code: tea.KeyTab}
	case "left":
		return tea.KeyPressMsg{Code: tea.KeyLeft}
	case "right":
		return tea.KeyPressMsg{Code: tea.KeyRight}
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEsc}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	case "ctrl+o":
		return tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl}
	}
	r := []rune(k)
	return tea.KeyPressMsg{Code: r[0], Text: k}
}
