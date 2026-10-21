// Package said reads what a coding agent said last, from its screen or its
// reply: the line to show, and whether it asks the person something.
package said

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"
)

// Kind is what a screen asks of the person.
type Kind int

const (
	None Kind = iota
	// Asks is a question: the person can answer it with a line of text.
	Asks
	// Permits is an approval prompt: a menu where Enter takes the selected
	// choice. It is answered in its pane, never by typing a line into it.
	Permits
)

func (k Kind) String() string { return [...]string{"none", "asks", "permits"}[k] }

const (
	// screenWithin is how many lines with words, from the bottom of a pane,
	// a prompt may sit above: an approval prompt lists its choices and a key
	// hint under the question. A question further up was answered.
	screenWithin = 8
	// replyWithin is the end of a reply: a question further up in a reply
	// is one the agent answered itself.
	replyWithin = 3
)

var (
	// permitsRe is an approval prompt, in the agents' own words: OpenCode's
	// permission box and its choices, aider's (Y)es/(N)o, a [y/n], Codex's
	// confirm hint, a numbered Yes choice (Claude Code, Codex), Cursor's
	// Run (once) (y) and its workspace trust prompt.
	permitsRe = regexp.MustCompile(`(?i)^permission required\b|^allow once\b|\(y\)es/\(n\)o|[\[(]y/n[\])]|press enter to confirm|^\d\.\s+yes\b|\brun \(once\) \(y\)|\[a\] trust this workspace`)
	// midRe is a question with a sentence after it.
	midRe = regexp.MustCompile(`\pL\?\s+\pL`)
	// hintRe is a line of key hints or a footer, never what the agent said.
	hintRe = regexp.MustCompile(`(?i)\b(ctrl|shift|alt|esc|enter)\b|^\? for shortcuts|^[~/]\S*`)
	wordRe = regexp.MustCompile(`\pL\pL`)
)

type line struct {
	text    string
	boxed   bool // inside a box: an input field, a prompt, a quote
	person  bool // the person's own turn or draft: › or > or ❯ or Cursor's → in front
	heading bool // a Markdown heading
}

// Screen reads an agent's pane. When it asks or wants an approval, the line
// is the question; else it is the last line with words outside the input
// box and the key hints.
func Screen(screen string) (string, Kind) {
	ls := lines(screen)
	win := ls[max(len(ls)-screenWithin, 0):]
	for _, l := range win {
		if permitsRe.MatchString(l.text) {
			for i := len(win) - 1; i >= 0; i-- {
				if strings.HasSuffix(win[i].text, "?") && !strings.HasPrefix(win[i].text, "?") {
					return win[i].text, Permits
				}
			}
			return l.text, Permits
		}
	}
	if q, ok := question(win); ok {
		return q, Asks
	}
	for i := len(ls) - 1; i >= 0; i-- {
		if !ls[i].boxed && !ls[i].person && !hintRe.MatchString(ls[i].text) {
			return ls[i].text, None
		}
	}
	return "", None
}

// Draft is what the person typed into an agent's input field and did not
// send: the text after the prompt marker on the lowest prompt line of the
// screen. "" when the field is empty or shows its placeholder. On a screen
// captured with its styles (capture-pane -e), text drawn grey or dim is
// Claude Code's suggestion, not typed: the draft ends where it starts.
func Draft(screen string) string {
	raw := strings.Split(strings.TrimRight(screen, "\n"), "\n")
	for i := len(raw) - 1; i >= max(len(raw)-2*screenWithin, 0); i-- {
		text, faint := styled(raw[i])
		in := strings.TrimLeft(text, " │┃")
		for _, mark := range []string{">", "›", "❯"} {
			rest, ok := strings.CutPrefix(in, mark)
			if !ok {
				continue
			}
			at := len(text) - len(rest)
			for j := range rest {
				if faint[at+j] {
					rest = rest[:j]
					break
				}
			}
			t := strings.TrimFunc(rest, frame)
			if strings.HasPrefix(t, `Try "`) {
				return "" // Claude Code's placeholder in an empty field
			}
			return t
		}
	}
	return ""
}

// styled is a captured line without its escape sequences, as Clean reads
// it, and for each byte of it whether it is drawn grey or dim.
func styled(line string) (string, []bool) {
	var b strings.Builder
	var faint []bool
	dim, grey := false, false
	rs := []rune(line)
	for i := 0; i < len(rs); i++ {
		r := rs[i]
		switch {
		case r == 0x1b && i+1 < len(rs) && rs[i+1] == '[':
			j := i + 2
			for j < len(rs) && (rs[j] < 0x40 || rs[j] > 0x7e) {
				j++
			}
			if j < len(rs) && rs[j] == 'm' {
				dim, grey = sgr(string(rs[i+2:j]), dim, grey)
			}
			i = j
			continue
		case r == 0x1b && i+1 < len(rs) && rs[i+1] == ']':
			j := i + 2
			for j < len(rs) && rs[j] != 0x07 && (rs[j] != 0x1b || j+1 >= len(rs) || rs[j+1] != '\\') {
				j++
			}
			if j < len(rs) && rs[j] == 0x1b {
				j++
			}
			i = j
			continue
		case r == '\t':
			r = ' '
		case unicode.IsControl(r):
			continue
		}
		n := b.Len()
		b.WriteRune(r)
		for range b.Len() - n {
			faint = append(faint, dim || grey)
		}
	}
	return b.String(), faint
}

// sgr applies one Select Graphic Rendition sequence to the dim and grey
// state of the text after it.
func sgr(params string, dim, grey bool) (bool, bool) {
	ps := strings.Split(params, ";")
	for i := 0; i < len(ps); i++ {
		switch p := ps[i]; p {
		case "", "0":
			dim, grey = false, false
		case "2":
			dim = true
		case "22":
			dim = false
		case "90":
			grey = true
		case "38":
			switch {
			case i+2 < len(ps) && ps[i+1] == "5":
				n, _ := strconv.Atoi(ps[i+2])
				grey = n == 8 || n >= 239 && n <= 250
				i += 2
			case i+4 < len(ps) && ps[i+1] == "2":
				r, _ := strconv.Atoi(ps[i+2])
				g, _ := strconv.Atoi(ps[i+3])
				bl, _ := strconv.Atoi(ps[i+4])
				grey = max(r, g, bl)-min(r, g, bl) <= 16 && r >= 90 && r <= 180
				i += 4
			}
		default:
			if n, err := strconv.Atoi(p); err == nil && (n >= 30 && n <= 37 || n == 39 || n >= 91 && n <= 97) {
				grey = false
			}
		}
	}
	return dim, grey
}

// Reply reads an agent's last reply (Markdown). The progress line that
// ends each reply (hooks.ProgressPrompt) and headings are never what it
// said.
func Reply(text string) (string, bool) {
	var ls []line
	for _, l := range lines(text) {
		if !l.heading && !strings.HasPrefix(l.text, "Progress [") {
			ls = append(ls, l)
		}
	}
	if q, ok := question(ls[max(len(ls)-replyWithin, 0):]); ok {
		return q, true
	}
	if len(ls) == 0 {
		return "", false
	}
	return ls[len(ls)-1].text, false
}

// Clean makes untrusted text safe to show: a tab is a space, every escape
// sequence (a colour, a title, a clipboard write) is dropped whole, and so
// is every other control key, which would drive the terminal. New lines
// stay.
func Clean(s string) string {
	ls := strings.Split(s, "\n")
	for i, l := range ls {
		ls[i], _ = styled(l)
	}
	return strings.Join(ls, "\n")
}

// lines keeps the lines with words, without the frame around them.
func lines(s string) []line {
	s = Clean(s)
	var out []line
	footer := false
	for _, raw := range strings.Split(s, "\n") {
		// Cursor draws its input box between rows of half blocks; what is
		// under the box is its status: the model, context use, the path.
		if b := strings.TrimSpace(raw); len([]rune(b)) >= 20 && strings.Trim(b, "▀") == "" {
			footer = true
			continue
		}
		t := strings.TrimFunc(raw, frame)
		if len(wordRe.FindAllString(t, 2)) < 2 && !strings.HasSuffix(t, "?") && !permitsRe.MatchString(t) {
			continue
		}
		in := strings.TrimLeft(raw, " ")
		out = append(out, line{
			text:    t,
			boxed:   footer || strings.HasPrefix(in, "│") || strings.HasPrefix(in, "┃"),
			person:  strings.HasPrefix(in, "›") || strings.HasPrefix(in, ">") || strings.HasPrefix(in, "❯") || strings.HasPrefix(in, "→"),
			heading: strings.HasPrefix(in, "#"),
		})
	}
	return out
}

// question is the last line, of the person's own lines and the input box
// left out, that asks.
func question(ls []line) (string, bool) {
	for i := len(ls) - 1; i >= 0; i-- {
		l := ls[i]
		if l.boxed || l.person {
			continue
		}
		if (strings.HasSuffix(l.text, "?") && !strings.HasPrefix(l.text, "?")) || midRe.MatchString(l.text) {
			return l.text, true
		}
	}
	return "", false
}

// frame is what a TUI or Markdown draws around words: space, box and block
// drawing, emphasis, and the markers in front of a turn or a choice.
func frame(r rune) bool {
	switch {
	case unicode.IsSpace(r), r >= 0x2500 && r <= 0x259f: // box drawing, blocks
		return true
	}
	return strings.ContainsRune("❯›>•⏺·*|⎿_`#", r)
}

var (
	headingRe = regexp.MustCompile(`^\s{0,3}#{1,6}\s+`)
	bulletRe  = regexp.MustCompile(`^(\s*)[-*+]\s+`)
	ruleRe    = regexp.MustCompile(`^\s*\|?\s*:?-+:?\s*(\|\s*:?-+:?\s*)*\|?\s*$`)
	boldRe    = regexp.MustCompile(`\*\*(\S(?:.*?\S)?)\*\*|__(\S(?:.*?\S)?)__`)
)

// Plain shows a reply's light Markdown the way a person reads it: no
// emphasis marks, backticks or heading marks, a table row's cells spaced
// apart, a bullet as •. Lines stay lines.
func Plain(s string) string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		t := strings.TrimSpace(l)
		switch {
		case strings.HasPrefix(t, "```"), strings.HasPrefix(t, "|") && ruleRe.MatchString(t):
			continue // a fence, or the rule under a table's head
		case strings.HasPrefix(t, "|"):
			cells := strings.Split(strings.Trim(t, "|"), "|")
			for i, c := range cells {
				cells[i] = strings.TrimSpace(c)
			}
			l = strings.Join(cells, "   ")
		default:
			l = headingRe.ReplaceAllString(l, "")
			l = bulletRe.ReplaceAllString(l, "$1• ")
		}
		l = boldRe.ReplaceAllString(l, "$1$2")
		out = append(out, strings.ReplaceAll(l, "`", ""))
	}
	return strings.Join(out, "\n")
}
