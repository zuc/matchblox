package app

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/blox-eng/matchblox/internal/proto"
)

const (
	sealedText   = "The night is sealed: n stops it, then launch a new one."
	startingText = "starting the stoker…"
	busyText     = "the stoker is busy: your order waits for it to rest"
)

// sheet is the open launch sheet: the order's words, the go chips and the
// recipe chips. ⏎ sends the order, and the run starts at once. Tab takes
// the cursor from the order line to the chips (onChips), where at is the
// chip it is on: the go chips first, then the recipes.
type sheet struct {
	night, restart bool
	text           string
	onChips        bool
	at             int
	goes           []string // the go chips that are on: merge, release, deploy, kill

	act string // the act in flight ("" none): the sheet closes when it goes through
	err string // why the last act did not go
	// queued: ⏎ was pressed while the stoker agent was not ready; the
	// order goes once it rests, never into a half-started agent.
	queued bool
}

// goChips are the go chips of the launch sheet: the go and its word.
var goChips = []struct{ go_, word string }{
	{"merge", "merges"},
	{"release", "releases"},
	{"deploy", "deploys"},
	{"kill", "kills"},
}

const maxOrder = 500

// openSheet opens the launch sheet for a night, a day run, or by day a new
// order to the run that is on: its chips then start as the run's goes.
// With no stoker agent running, matchblox starts it now: the sheet says so
// until it rests.
func (m Model) openSheet(night, restart bool) (Model, tea.Cmd) {
	m.sheet = &sheet{night: night, restart: restart}
	if o := m.stokerOrder(); restart && o != nil {
		m.sheet.goes = slices.Clone(o.Grants)
	}
	m.peek, m.pending = nil, nil
	return m, m.startStoker()
}

// startStoker asks the service to start the stoker agent, when there is
// one to start and it neither runs nor starts.
func (m Model) startStoker() tea.Cmd {
	v := m.stoker
	if m.noAgent() || v.PaneUp || v.Starting || m.conn == nil || m.lost {
		return nil
	}
	return m.send(proto.Act{RecID: "stoker:start", Which: "primary"})
}

// orderKey is o: a new order for a day run. A night is sealed.
func (m Model) orderKey() (tea.Model, tea.Cmd) {
	switch {
	case m.conn == nil || m.lost:
		m.flash = "not sent, no connection"
	case m.stoker == nil:
		m.flash = "this service keeps no stoker"
	case m.stoker.On && !m.stoker.Until.IsZero():
		m.flash = sealedText
	case m.stoker.On:
		return m.openSheet(false, true)
	default:
		m.flash = "o gives a new order to a running stoker: f starts it"
	}
	return m, nil
}

// noAgent: there is no stoker agent to start (fixtures), or its command
// is not on PATH. ⏎ then turns today's stoker on.
func (m Model) noAgent() bool { return m.stoker.Agent == "" || m.stoker.Missing != "" }

func (m Model) sheetKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	s := *m.sheet
	key := k.String()
	m.sheet = &s
	switch key {
	case "ctrl+c":
		return m.quit()
	case "enter":
		return m.sheetEnter()
	}
	if s.onChips {
		n := len(goChips) + len(m.stoker.Recipes)
		switch key {
		case "esc", "tab":
			s.onChips = false
		case "left":
			s.at = (s.at + n - 1) % n
		case "right":
			s.at = (s.at + 1) % n
		case "space":
			s.pick(m.stoker.Recipes)
		default:
			// Any other key is the order's: the cursor goes back to it.
			s.onChips = false
			s.typeText(k.Text)
		}
		return m, nil
	}
	switch key {
	case "esc":
		m.sheet = nil // nothing is sent
		return m, nil
	case "tab":
		s.onChips, s.at = true, 0
		return m, nil
	case "backspace":
		s.text = trimLast(s.text)
		return m, nil
	}
	s.typeText(k.Text)
	return m, nil
}

// typeText adds text to the order, as long as the order may grow.
func (s *sheet) typeText(text string) {
	if text != "" && len([]rune(s.text+text)) <= maxOrder {
		s.text += text
	}
}

// pick is space on the chip the cursor is on: a go chip turns on or off,
// and a recipe goes into the order.
func (s *sheet) pick(recipes []string) {
	if s.at < len(goChips) {
		s.toggle(goChips[s.at].go_)
		return
	}
	i := s.at - len(goChips)
	if i >= len(recipes) { // the ways file lost recipes since the cursor got here
		s.at = len(goChips) + max(len(recipes)-1, 0)
		return
	}
	if r := recipes[i]; len([]rune(joinWords(s.text, r))) <= maxOrder {
		s.text = joinWords(s.text, r)
	}
}

// toggle turns the go chip of go_ on or off.
func (s *sheet) toggle(go_ string) {
	if i := slices.Index(s.goes, go_); i >= 0 {
		s.goes = slices.Delete(slices.Clone(s.goes), i, i+1)
	} else {
		s.goes = append(slices.Clip(s.goes), go_)
	}
}

func joinWords(text, word string) string {
	if text == "" || strings.HasSuffix(text, " ") {
		return text + word
	}
	return text + " " + word
}

func (m Model) sheetEnter() (tea.Model, tea.Cmd) {
	s := *m.sheet
	m.sheet = &s
	if m.conn == nil || m.lost {
		s.err = "not sent, no connection"
		return m, nil
	}
	if !m.noAgent() && !m.stoker.Ready {
		s.queued, s.err = true, ""
		return m, m.startStoker()
	}
	return m.sendOrder()
}

// sendOrder sends the sheet's order: ⏎, or the stoker agent coming to
// rest after a ⏎ that waited for it.
func (m Model) sendOrder() (Model, tea.Cmd) {
	s := *m.sheet
	m.sheet = &s
	s.queued = false
	var goes []string // in the chips' order
	for _, c := range goChips {
		if slices.Contains(s.goes, c.go_) {
			goes = append(goes, c.go_)
		}
	}
	body, _ := json.Marshal(struct {
		Words  string   `json:"words"`
		Night  bool     `json:"night"`
		Grants []string `json:"grants,omitempty"`
	}{strings.TrimSpace(s.text), s.night, goes})
	cmd := m.send(proto.Act{RecID: "stoker:order", Which: "primary", Confirm: "y", Text: string(body)})
	if m.noAgent() { // today's run starts at once: a refusal is a flash
		m.sheet = nil
		return m, cmd
	}
	s.act, s.err = fmt.Sprintf("a%d", m.acts), ""
	return m, cmd
}

// sheetResult takes the result of the sheet's act: the sheet closes when
// it went through, and a refusal stays in it, where the builder can read
// it and edit.
func (m Model) sheetResult(r proto.Result) (Model, bool) {
	if m.sheet == nil || m.sheet.act != r.ActID {
		return m, false
	}
	s := *m.sheet
	m.sheet = &s
	s.act = ""
	if r.Err != "" {
		s.err = r.Err
		return m, true
	}
	m.sheet = nil
	return m, true
}

// sheetBox is the sheet's lines in bw columns, the key each line presses
// on a tap, the footer's key spans, and the go chips' line in the box with
// the key a tap on each chip presses.
func (m Model) sheetBox(bw int) (box, press []string, spans []span, goesAt int, goes []span) {
	st, s := m.st, m.sheet
	iw := bw - 4
	var lines []string
	add := func(l ...string) { lines = append(lines, l...) }
	title := st.accent.Bold(true).Render("New order")
	prompt := "How should I run?"
	if s.night {
		title = st.stoker.Bold(true).Render("☾ Night")
		prompt = "How should I run tonight?"
	}
	add(st.muted.Render(prompt), "")
	words := wrapPlain(s.text, iw-2)
	if len(words) == 0 {
		words = []string{""}
	}
	for i, l := range words {
		lead := "  "
		if i == 0 {
			lead = st.accent.Render("› ")
		}
		l = st.text.Render(l)
		if i == len(words)-1 && !s.onChips {
			l += st.accent.Render("▏")
		}
		add(lead + l)
	}
	line, chipSpans := m.goLine(iw)
	add("")
	goesAt = len(lines) + 1 // the box's top line comes first
	add(line)
	if r := m.stoker.Recipes; len(r) > 0 {
		focus := -1
		if s.onChips {
			focus = s.at - len(goChips)
		}
		add(m.chips(r, iw, focus))
	}
	for i := range chipSpans {
		chipSpans[i].x0 += 2 // the box's "│ "
		chipSpans[i].x1 += 2
	}
	switch v := m.stoker; {
	case v.Agent == "":
	case v.Missing != "":
		// The agent cannot start: the door comes first, not after ⏎.
		add("")
		for i, l := range wrapPlain(v.Missing, iw-2) {
			lead := "  "
			if i == 0 {
				lead = st.warn.Render("! ")
			}
			add(lead + st.text.Render(l))
		}
	case m.stokerAsks():
		add("", st.warn.Render("! ")+st.text.Render("the stoker waits for you: tap its word at the top to answer it"))
	case !v.Ready && v.PaneUp && !v.Starting:
		// Up and mid-turn, started long ago: the order waits for its rest.
		add("")
		for _, l := range wrapPlain(busyText, iw-2) {
			add("  " + st.muted.Render(l))
		}
	case !v.Ready:
		line := startingText
		if s.queued {
			line += " the order goes once it rests"
		}
		add("")
		for _, l := range wrapPlain(line, iw-2) {
			add("  " + st.muted.Render(l))
		}
		if v.Start != "" {
			for _, l := range wrapPlain(v.Start, iw-2) {
				add("  " + st.faint.Render(l))
			}
		}
	}
	if s.err != "" {
		add("")
		for _, l := range wrapPlain(s.err, iw-2) {
			add(st.warn.Render("! ") + st.text.Render(l))
		}
	}
	box, spans = boxFrame(st.hair, title, lines, m.sheetFoot(), bw)
	return box, make([]string, len(box)), spans, goesAt, chipSpans
}

// goLine is the go chips: each on (✓, the run gives it) or off (asks you),
// the one the cursor is on reversed, and the span of each a tap toggles.
func (m Model) goLine(iw int) (string, []span) {
	st, s := m.st, m.sheet
	line, x := st.faint.Render("gos "), 4
	need := x
	for i, c := range goChips {
		need += 2*min(i, 1) + lipgloss.Width("○ "+c.word)
	}
	if need > iw {
		line, x = "", 0 // a phone: the chips alone, ✓ and ○ say what they are
	}
	var spans []span
	for i, c := range goChips {
		if i > 0 {
			line += "  "
			x += 2
		}
		chip := "○ " + c.word
		style := st.muted
		if slices.Contains(s.goes, c.go_) {
			chip, style = "✓ "+c.word, st.accent
		}
		if s.onChips && s.at == i {
			style = style.Reverse(true)
		}
		line += style.Render(chip)
		spans = append(spans, span{x, x + lipgloss.Width(chip), c.go_})
		x += lipgloss.Width(chip)
	}
	if rest := "  off asks you"; x+lipgloss.Width(rest) <= iw {
		line += st.faint.Render(rest)
	}
	return line, spans
}

func (m Model) sheetFoot() []footPart {
	st := m.st
	key := func(k, label, press string) footPart {
		t := st.accent.Render(k)
		if label != "" {
			t += st.muted.Render(" " + label)
		}
		return footPart{t, press, " ─ "}
	}
	verb := "send"
	if m.noAgent() {
		verb = "start"
	}
	foot := []footPart{key("⏎", verb, "enter")}
	if m.sheet.onChips {
		return append(foot, key("space", "toggle", "space"), key("tab", "back", "tab"))
	}
	return append(foot, key("tab", "chips", "tab"), key("esc", "", "esc"))
}

// chips are the recipes of the ways file, as many as the line holds, the
// one at focus (-1: none) reversed. A recipe the line does not reach
// starts the line, so the cursor is always seen.
func (m Model) chips(recipes []string, iw, focus int) string {
	st := m.st
	from := 0
	if focus >= fitting(recipes, iw) {
		from = focus
	}
	line, w := st.faint.Render("ways "), 5
	if from > 0 {
		lead := fmt.Sprintf("+%d  ", from)
		line, w = line+st.faint.Render(lead), w+lipgloss.Width(lead)
	}
	for i := from; i < len(recipes); i++ {
		r := recipes[i]
		rest := len(recipes) - i
		more := ""
		if rest > 1 {
			more = fmt.Sprintf("  +%d", rest-1)
		}
		if w+2+lipgloss.Width(r)+lipgloss.Width(more) > iw && i > from || w+lipgloss.Width(r) > iw {
			return line + st.faint.Render(fmt.Sprintf("  +%d", rest))
		}
		if i > from {
			line += "  "
			w += 2
		}
		style := st.accent.Underline(true)
		if i == focus {
			style = style.Reverse(true)
		}
		line += style.Render(r)
		w += lipgloss.Width(r)
	}
	return line
}

// fitting is how many recipes the ways line shows from the first.
func fitting(recipes []string, iw int) int {
	w := 5
	for i, r := range recipes {
		more := 0
		if rest := len(recipes) - i; rest > 1 {
			more = lipgloss.Width(fmt.Sprintf("  +%d", rest-1))
		}
		if w+2+lipgloss.Width(r)+more > iw && i > 0 || w+lipgloss.Width(r) > iw {
			return i
		}
		if i > 0 {
			w += 2
		}
		w += lipgloss.Width(r)
	}
	return len(recipes)
}

// asksFor are the goes the run does not give: merges, releases and
// deploys ask the builder unless a chip gives them.
func asksFor(grants []string) []string {
	var out []string
	for _, c := range goChips {
		if !slices.Contains(grants, c.go_) {
			out = append(out, c.word)
		}
	}
	return out
}

// grantNames name the goes the run gives, in the words of the line.
func grantNames(grants []string) []string {
	var out []string
	for _, c := range goChips {
		if slices.Contains(grants, c.go_) {
			out = append(out, c.word)
		}
	}
	return out
}

func joinList(s []string) string {
	if len(s) < 2 {
		return strings.Join(s, "")
	}
	return strings.Join(s[:len(s)-1], ", ") + " and " + s[len(s)-1]
}

// overlaySheet lays the sheet over the dimmed screen like the peek: full
// width up to 72 columns on a phone, else a centred box.
func (m Model) overlaySheet(sc *drawn, w int) {
	phone := m.layoutAt(w) == Narrow || m.upright()
	bw := min(w, 72)
	if !phone {
		bw = min(62, w-8)
	}
	box, press, spans, goesAt, goes := m.sheetBox(bw)
	m.overlayBox(sc, w, bw, phone, box, press, spans)
	sc.sheetGoesAt, sc.sheetGoes = sc.peekAt+goesAt, goes
}

// tapSheet: a tap on a go chip toggles it, and one on a key of the footer
// presses it; any other tap does nothing, so the screen behind stays.
func (m Model) tapSheet(ms tea.Mouse, sc drawn) (tea.Model, tea.Cmd) {
	dx := ms.X - sc.peekX
	if dx < 0 || dx >= sc.peekW {
		return m, nil
	}
	if ms.Y == sc.sheetGoesAt {
		if c, ok := spanAt(sc.sheetGoes, dx); ok {
			s := *m.sheet
			s.toggle(c.press)
			m.sheet = &s
		}
		return m, nil
	}
	if ms.Y != sc.peekAt+len(sc.peekRows)-1 {
		return m, nil
	}
	if s, ok := spanAt(sc.peekKeys, dx); ok {
		return m.sheetKey(keyPress(s.press))
	}
	return m, nil
}
