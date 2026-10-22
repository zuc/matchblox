package app

import (
	"strings"
)

// consenting is a question the builder answers with a key: a pending step,
// or the stoker's take-over. A door's long diff keeps its confirm on the
// key line: the diff fills the body, and a box would hide what it asks
// about.
func (m Model) consenting() bool {
	if m.held != nil {
		return true
	}
	if m.pending == nil {
		return false
	}
	_, preview := m.previewDoor()
	return !preview
}

// consentPad is the room a consent box keeps inside its frame: a blank row
// over and under its words, and two columns more on each side than the
// frame's own one.
const consentPad = 2

// consentBox is the question in a box: what runs, in words and commands,
// and the key that runs it. A destructive step says so in its title.
func (m Model) consentBox(bw int) []string {
	st := m.st
	if m.held == nil {
		return stepBox(st, *m.pending, bw)
	}
	var words [][]string
	if m.flash != "" {
		var said []string
		for i, l := range wrapPlain(m.flash, bw-6-2*consentPad) {
			lead := "  "
			if i == 0 {
				lead = st.warn.Render("! ")
			}
			said = append(said, lead+st.text.Render(l))
		}
		words = append(words, said)
	}
	words = append(words, []string{st.accent.Render("y") + st.muted.Render("  rests it while you work"), st.accent.Render("n") + st.muted.Render("  keeps it on")})
	box, _ := boxFrame(st.hair, st.warn.Bold(true).Render("The stoker is on. Take over?"), padded(words, bw),
		[]footPart{{st.accent.Render("y") + st.muted.Render(" take over"), "", ""}, {st.accent.Render("n") + st.muted.Render(" keep it on"), "", " ─ "}}, bw)
	return box
}

// stepBox is a pending step in a box: what it does in its title, how in
// its lines, one step each, and the key that runs it in its foot.
func stepBox(st styles, a action, bw int) []string {
	head, body := a.consentWords()
	var how []string
	for _, b := range body {
		for _, l := range wrapPlain(b, bw-4-2*consentPad) {
			how = append(how, st.text.Render(l))
		}
	}
	title, foot := st.accent.Bold(true).Render(head), []footPart{{st.accent.Render("⏎") + st.muted.Render(" run"), "", ""}, {st.accent.Render("esc") + st.muted.Render(" cancel"), "", " ─ "}}
	if a.destructive {
		title, foot = st.neg.Bold(true).Render(head), []footPart{{st.neg.Render("y") + st.muted.Render(" run"), "", ""}, {st.muted.Render("any other key cancels"), "", " ─ "}}
	}
	box, _ := boxFrame(st.hair, title, padded([][]string{how}, bw), foot, bw)
	return box
}

// padded lays groups of lines inside a consent box: indented, a blank row
// over, under and between the groups. A line too long for the room wraps.
func padded(groups [][]string, bw int) []string {
	indent := strings.Repeat(" ", consentPad)
	out := []string{""}
	for i, g := range groups {
		if i > 0 {
			out = append(out, "")
		}
		for _, l := range g {
			out = append(out, indent+l)
		}
	}
	return append(out, "")
}

// consentWords are the box's title and lines: what the step does, then how.
// A say of "what: how" splits there; else the title is the step's label
// and the lines are its commands, one each.
func (a action) consentWords() (string, []string) {
	if what, how, ok := strings.Cut(a.say, ": "); ok {
		if strings.HasPrefix(what, a.label+" ") {
			what = capital(what) // a verb: kill spark-docs; never a name
		}
		steps := strings.Split(how, ", then ")
		for i := 1; i < len(steps); i++ {
			steps[i] = "then " + steps[i]
		}
		return what, steps
	}
	if a.say != "" {
		return capital(a.label), []string{a.say}
	}
	var lines []string
	for _, s := range a.steps {
		lines = append(lines, strings.Join(s, " "))
	}
	return capital(a.label), lines
}

func capital(s string) string {
	if s == "" {
		return "Run"
	}
	return strings.ToUpper(s[:1]) + s[1:]
}

// overlayConsent lays the question over the dimmed screen: on a phone it
// rises from the key line, under the thumb and above the keyboard; wider,
// it sits in the middle. A tap anywhere cancels it: a tap is never consent.
func (m Model) overlayConsent(sc *drawn, w int) {
	phone := m.layoutAt(w) == Narrow || m.upright()
	bw := min(w, 72)
	if !phone {
		bw = min(m.consentWidth()+4, 72, w-8)
	}
	box := m.consentBox(bw)
	press := make([]string, len(box))
	m.overlayBox(sc, w, bw, phone, box, press, nil)
}

// consentWidth is the columns the question's words take on one line each,
// 40 at least: a desk's box is no wider than what it says.
func (m Model) consentWidth() int {
	if m.held != nil {
		return 48
	}
	return m.pending.width()
}

func (a action) width() int {
	head, body := a.consentWords()
	w := max(40, len([]rune(head))+16)
	for _, b := range body {
		w = max(w, len([]rune(b))+2*consentPad)
	}
	return w
}
