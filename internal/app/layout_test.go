package app

import (
	"fmt"
	"slices"
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/blox-eng/matchblox/internal/proto"
	"github.com/blox-eng/matchblox/internal/sample"
)

func TestLayoutBreakpoints(t *testing.T) {
	for _, c := range []struct {
		cols, rows int
		want       Layout
	}{
		{107, 60, Medium},  // iPhone Pro Max upright, keyboard up: the drawer
		{107, 101, Medium}, // keyboard down
		{107, 96, Medium},  // mid-animation
		{205, 21, Wide},    // landscape, keyboard down
		{205, 5, Wide},     // landscape, keyboard up
		{192, 60, Wide},    // a desk
		{44, 31, Narrow},
		{86, 30, Medium},
		{132, 32, Wide},
		{100, 30, Wide},
		{120, 60, Medium}, // rows*2 == cols is portrait
		{89, 60, Narrow},  // upright under 90: one column
		{90, 45, Medium},
		{70, 120, Narrow},
		{121, 60, Wide},
		{59, 10, Narrow},
		{60, 10, Medium},
		{99, 10, Medium},
		{0, 0, Narrow},
		{1, 1, Narrow},
		{80, 0, Medium}, // no height yet: by width
		{200, 0, Wide},
	} {
		if got := layout(c.cols, c.rows); got != c.want {
			t.Errorf("layout(%d, %d) = %v, want %v", c.cols, c.rows, got, c.want)
		}
	}
}

// TestAnUprightPhoneGetsTheDrawer: a phone held upright with a small font
// (107x60 with the keyboard up, 107x101 down) splits the list and the
// detail at the middle, the detail a drawer on the right, and no row opens
// in place; its dock shows two panels side by side. Upright under 90
// columns is one column.
func TestAnUprightPhoneGetsTheDrawer(t *testing.T) {
	for _, h := range []int{60, 101} {
		m, _ := loadedWith(t, 107, limitsState())
		next, _ := m.Update(tea.WindowSizeMsg{Width: 107, Height: h})
		m = next.(Model)
		out := screen(m)
		if at := strings.Split(out, "\n")[lineOf(t, m, "USAGE")]; !strings.Contains(at, "MACHINE") {
			t.Fatalf("107x%d: usage and the machine are not side by side:\n%s", h, out)
		}
		fitsWidth(t, m, 107)
		if list, detail := m.panes(107); list != 53 || detail != 53 {
			t.Fatalf("107x%d: panes = %d, %d, want 53, 53", h, list, detail)
		}
		for _, s := range m.all.sessions {
			if rows := m.sessionRow(s, true, 53); len(rows) != 1 {
				t.Fatalf("107x%d: the selected row opens in place: %q", h, rows)
			}
		}
		p, _ := key(m, "a")
		if sc := p.(Model).draw(107); !sc.peekInline || sc.peekX != 54 || sc.peekW != 53 {
			t.Fatalf("107x%d: the peek is not the drawer: inline %v at %d, %d wide", h, sc.peekInline, sc.peekX, sc.peekW)
		}
	}
	m, _ := loadedWith(t, 70, limitsState())
	next, _ := m.Update(tea.WindowSizeMsg{Width: 70, Height: 120})
	m = next.(Model)
	if list, detail := m.panes(70); list != 70 || detail != 0 {
		t.Fatalf("70x120: panes = %d, %d, want one column", list, detail)
	}
	if s := m.all.sessions; len(s) == 0 || len(m.sessionRow(s[0], true, 70)) < 2 {
		t.Fatal("70x120: the selected row does not open in place")
	}
	wide, _ := loadedWith(t, 107, limitsState())
	if wide.layoutAt(107) != Wide {
		t.Fatal("107x30 is no longer a desk")
	}
}

// lineOf is the screen row of the first line that holds s.
func lineOf(t *testing.T, m tea.Model, s string) int {
	t.Helper()
	for i, line := range strings.Split(ansi.Strip(m.(Model).render()), "\n") {
		if strings.Contains(line, s) {
			return i
		}
	}
	t.Fatalf("no line holds %q:\n%s", s, ansi.Strip(m.(Model).render()))
	return -1
}

func fitsWidth(t *testing.T, m Model, w int) {
	t.Helper()
	for i, line := range strings.Split(m.render(), "\n") {
		if lw := lipgloss.Width(line); lw > w {
			t.Fatalf("width %d: line %d is %d wide: %q", w, i, lw, ansi.Strip(line))
		}
	}
}

// sessLine is the screen row of a session's row: its name between
// spaces, never a recommendation that names it ("Compact x (%1)").
func sessLine(t *testing.T, m tea.Model, name string) int {
	t.Helper()
	lw, _ := m.(Model).panes(max(m.(Model).width, minWidth))
	for i, line := range strings.Split(ansi.Strip(m.(Model).render()), "\n") {
		if r := []rune(line); lw > 0 && len(r) > lw {
			line = string(r[:lw]) // the list, not the detail beside it
		}
		if strings.Contains(line+" ", " "+name+" ") && !strings.Contains(line, "(%") {
			return i
		}
	}
	t.Fatalf("no row of %q:\n%s", name, ansi.Strip(m.(Model).render()))
	return -1
}

// TestAPhoneRowOpensInPlace: on a phone each session is one line, and the
// selected one grows by what its agent said, then its plan and its keys.
// Who waits comes before the quiet ones. Every tab fits.
func TestAPhoneRowOpensInPlace(t *testing.T) {
	m, _ := loadedWith(t, 50, queueState())
	fitsWidth(t, m, 50)
	out := strings.Split(ansi.Strip(m.render()), "\n")
	at := sessLine(t, m, "app-feature")
	if !strings.HasPrefix(out[at], "▌") || !strings.Contains(out[at], "asks") || !strings.Contains(out[at], "3m") || !strings.Contains(out[at], "90%") {
		t.Fatalf("the selected row: %q", out[at])
	}
	if !strings.Contains(out[at+1], "“Claude needs your permission to use Bash”") {
		t.Fatalf("no last words under the row:\n%s", strings.Join(out, "\n"))
	}
	// The plan and the keys share no line at 50 columns: each gets its own.
	if !strings.Contains(out[at+2], "plan 75%") || !strings.Contains(out[at+2], "! compact") || !strings.Contains(out[at+3], "a answer  A switch  ⏎ open") {
		t.Fatalf("no plan and keys:\n%s", strings.Join(out[at:at+4], "\n"))
	}
	if !strings.HasPrefix(out[at-1], "────") || !strings.HasPrefix(out[at+4], "────") {
		t.Fatalf("the opened block is not between hairlines:\n%s", strings.Join(out, "\n"))
	}
	if sessLine(t, m, "app-review") != at+5 || strings.Contains(out[at+6], "“") {
		t.Fatalf("the rows that are not selected open too:\n%s", strings.Join(out, "\n"))
	}
	if lineOf(t, m, "WAITS FOR YOU · 2") > lineOf(t, m, "QUIET · 1") {
		t.Fatalf("the quiet ones come first:\n%s", strings.Join(out, "\n"))
	}
	// Selected, app-review (done) offers an answer.
	next, _ := key(m, "down")
	out = strings.Split(ansi.Strip(next.(Model).render()), "\n")
	if at := sessLine(t, next, "app-review"); !strings.Contains(out[at+1], "a answer  A switch  ⏎ open") {
		t.Fatalf("no a for a session that waits for an answer:\n%s", strings.Join(out, "\n"))
	}
	for tab := range tabNames {
		m.tab = tab
		fitsWidth(t, m, 50)
	}
}

func tap(m tea.Model, x, y int) (tea.Model, tea.Cmd) {
	return m.Update(tea.MouseClickMsg{X: x, Y: y, Button: tea.MouseLeft})
}

func TestTapSelectsThenActs(t *testing.T) {
	for _, w := range []int{50, 100} {
		m, f := loadedWith(t, w, queueState())
		var ran []string
		m.opt.Run = func(argv []string) error { ran = argv; return nil }

		// A session that does not wait: the first tap selects it, a tap on
		// it selected opens the peek to message it. Nothing runs.
		next, cmd := tap(m, 5, sessLine(t, m, "tools"))
		if cmd != nil || ran != nil || next.(Model).peek != nil {
			t.Fatalf("width %d: the first tap acted", w)
		}
		if it, _ := next.(Model).selectedSession(); it.Pane != "%3" {
			t.Fatalf("width %d: selected %q, want %%3", w, it.Pane)
		}
		next, cmd = tap(next, 5, sessLine(t, next, "tools"))
		if p := next.(Model).peek; p == nil || p.pane != "%3" || cmd != nil || ran != nil {
			t.Fatalf("width %d: the second tap did not open the peek on tools: %+v", w, p)
		}
		if !strings.Contains(screen(next), "To tools") {
			t.Fatalf("width %d: the peek is not titled To tools:\n%s", w, screen(next))
		}
		if len(f.acts()) != 0 {
			t.Fatalf("width %d: a tap sent %v to the service", w, f.acts())
		}

		// A session that waits: the first tap selects it, a tap on it
		// selected opens the answer peek.
		next, _ = tap(m, 5, sessLine(t, m, "app-review"))
		if p := next.(Model).peek; p != nil {
			t.Fatalf("width %d: the first tap opened the peek", w)
		}
		next, _ = tap(next, 5, sessLine(t, next, "app-review"))
		if p := next.(Model).peek; p == nil || p.pane != "%2" {
			t.Fatalf("width %d: the second tap did not open the peek: %+v", w, p)
		}
	}
}

func TestTapOnATabOpensIt(t *testing.T) {
	for _, w := range []int{58, 100} {
		m, _ := loadedWith(t, w, queueState())
		line := ansi.Strip(strings.Split(m.render(), "\n")[1])
		x := strings.Index(line, "panes")
		if x < 0 {
			t.Fatalf("width %d: no panes tab in %q", w, line)
		}
		next, _ := tap(m, len([]rune(line[:x])), 1)
		if got := next.(Model).tab; got != tabPanes {
			t.Fatalf("width %d: tab %d, want the panes tab", w, got)
		}
	}
}

// TestTapNeverRunsADestructiveStep: a tap is never consent to kill or
// remove; those stay behind x and a typed y.
func TestTapNeverRunsADestructiveStep(t *testing.T) {
	st := fixtureState()
	st.Orphans = []sample.Orphan{{PID: 4242, Start: 7, Comm: "bash", Pane: "%1", PaneAlive: true, Kill: []string{"kill", "4242"}}}
	m, f := loadedWith(t, 100, st)
	var ran [][]string
	m.opt.Run = func(argv []string) error { ran = append(ran, argv); return nil }
	m.tab = tabMachine
	o, ok := m.selectedOrphan()
	if !ok {
		t.Fatal("the fixture has no orphan")
	}
	y := lineOf(t, m, strconv.Itoa(o.PID))
	next, _ := tap(m, 5, y)
	next, cmd := tap(next, 5, y)
	if cmd != nil {
		next.Update(cmd())
	}
	for _, step := range ran {
		if step[0] == "kill" {
			t.Fatalf("a tap ran %v", step)
		}
	}
	if len(f.acts()) != 0 {
		t.Fatalf("a tap sent %v", f.acts())
	}
}

func TestTapWhilePendingCancels(t *testing.T) {
	m, _ := loadedWith(t, 100, queueState())
	next, _ := key(m, "enter")
	if next.(Model).pending == nil {
		t.Fatal("enter did not ask to confirm")
	}
	next, cmd := tap(next, 5, sessLine(t, next, "app-feature"))
	if cmd != nil || next.(Model).pending != nil {
		t.Fatal("a tap did not cancel the pending step")
	}
}

// TestNoCtrlKeysNeeded: every key is on a phone SSH key bar (DESIGN.md §6).
func TestNoCtrlKeysNeeded(t *testing.T) {
	for _, w := range []int{50, 100} {
		m, _ := loadedWith(t, w, queueState())
		for tab := range tabNames {
			m.tab = tab
			line, _ := m.keyLine(w)
			footer := ansi.Strip(line)
			if strings.Contains(strings.ToLower(footer), "ctrl") || strings.Contains(footer, "^") {
				t.Fatalf("width %d tab %d: footer names a Ctrl key: %q", w, tab, footer)
			}
		}
		for tab := range tabNames {
			next, _ := key(m, string(rune('1'+tab)))
			if next.(Model).tab != tab {
				t.Fatalf("digit %d does not open its tab", tab+1)
			}
		}
		m.tab = tabSessions
		next, _ := key(m, "down") // the second row waits for an answer
		next, _ = key(next, "a")
		if next.(Model).peek == nil {
			t.Fatal("a does not start an answer")
		}
		next, _ = key(next, "esc")
		if next.(Model).peek != nil {
			t.Fatal("esc does not leave the answer")
		}
		if next, cmd := key(m, "q"); cmd == nil || !next.(Model).quitting {
			t.Fatal("q does not quit")
		}
	}
}

func TestMouseIsOn(t *testing.T) {
	m, _ := loadedWith(t, 100, queueState())
	if m.View().MouseMode != tea.MouseModeCellMotion {
		t.Fatal("the console does not take taps")
	}
}

func manySessions(n int) proto.State {
	st := fixtureState()
	base := st.Sessions[0]
	st.Sessions = nil
	for i := range n {
		s := base
		s.PID, s.Name, s.Pane, s.Target = 9000+i, fmt.Sprintf("agent-%02d", i), fmt.Sprintf("%%%d", 100+i), fmt.Sprintf("work:%d.1", i)
		st.Sessions = append(st.Sessions, s)
	}
	st.Queue = nil
	return st
}

// TestNarrowScrollsToTheSelection: on a phone the list scrolls, and the
// selected row stays on the screen, opened.
func TestNarrowScrollsToTheSelection(t *testing.T) {
	m, _ := loadedWith(t, 50, manySessions(30))
	var next tea.Model = m
	next, _ = key(next, "1")
	for range 25 {
		next, _ = key(next, "down")
	}
	got := next.(Model)
	if !strings.Contains(ansi.Strip(got.render()), "agent-25") {
		t.Fatalf("the selected agent-25 is off the screen:\n%s", ansi.Strip(got.render()))
	}
	if got := len(strings.Split(got.render(), "\n")); got != 30 {
		t.Fatalf("%d lines on a 30-line screen", got)
	}
	// A tap on the row it shows selects that row.
	y := lineOf(t, next, "agent-24")
	next, _ = tap(next, 5, y)
	if s, _ := next.(Model).selected(); s.Name != "agent-24" {
		t.Fatalf("the tap selected %s", s.Name)
	}
}

// TestFirstTapOnTheSelectedRowOnlyShows: the default selection is not a
// tap; the step shows before a tap runs it, and a key in between disarms.
func TestFirstTapOnTheSelectedRowOnlyShows(t *testing.T) {
	m, _ := loadedWith(t, 50, queueState())
	var ran []string
	m.opt.Run = func(argv []string) error { ran = argv; return nil }
	y := sessLine(t, m, "app-feature")
	next, cmd := tap(m, 5, y)
	if cmd != nil || ran != nil || next.(Model).peek != nil {
		t.Fatal("one tap on the preselected row acted")
	}
	if next, _ := tap(next, 5, y); next.(Model).peek == nil {
		t.Fatal("a tap on the row it selected does not open the peek")
	}
	y = sessLine(t, m, "tools")
	next, _ = tap(m, 5, y)
	if next.(Model).peek != nil || next.(Model).flash != "" {
		t.Fatalf("one tap on another row acted: flash %q", next.(Model).flash)
	}
	if next, _ = tap(next, 5, sessLine(t, next, "tools")); next.(Model).peek == nil {
		t.Fatal("a tap on a selected session that does not wait does not open the peek")
	}
}

func TestWheelMovesTheSelection(t *testing.T) {
	m, _ := loadedWith(t, 50, queueState())
	next, _ := m.Update(tea.MouseWheelMsg{X: 5, Y: 6, Button: tea.MouseWheelDown})
	if it, _ := next.(Model).selectedSession(); it.Pane != "%2" {
		t.Fatalf("wheel down selected %s", it.Pane)
	}
	next, _ = next.Update(tea.MouseWheelMsg{X: 5, Y: 6, Button: tea.MouseWheelUp})
	if it, _ := next.(Model).selectedSession(); it.Pane != "%1" {
		t.Fatalf("wheel up selected %s", it.Pane)
	}
}

// TestTheKeyLineIsAtTheBottom: the keys, a tap's step, a confirm and the
// answer being typed are all on the last line, under a hairline
// (design/0006 §11.1).
func TestTheKeyLineIsAtTheBottom(t *testing.T) {
	for _, w := range []int{50, 100} {
		m, _ := loadedWith(t, w, queueState())
		last := func(m tea.Model) string {
			out := strings.Split(ansi.Strip(m.(Model).render()), "\n")
			if !strings.HasPrefix(out[len(out)-2], "────") {
				t.Fatalf("width %d: no hairline over the key line:\n%s", w, strings.Join(out, "\n"))
			}
			return out[len(out)-1]
		}
		// A phone drops the spare keys first: p ships is a tab tap away.
		if l := last(m); !strings.Contains(l, "⏎ open") || !strings.Contains(l, "a answer") || !strings.Contains(l, "x kill") || !strings.Contains(l, "N new") || w > 60 && !strings.Contains(l, "p ships") {
			t.Fatalf("width %d: the keys are not at the bottom: %q", w, l)
		}
		if l := last(selectPane(t, m, "%3")); !strings.Contains(l, "a message") {
			t.Fatalf("width %d: the keys are not at the bottom: %q", w, l)
		}
		if strings.Contains(ansi.Strip(strings.Split(m.render(), "\n")[2]), "open") {
			t.Fatalf("width %d: keys under the tabs", w)
		}
		next, _ := key(selectPane(t, m, "%3"), "enter")
		// The confirm is a box: on a phone its foot sits on the key line's
		// hairline, under the thumb; wider, it is in the middle.
		out := strings.Split(ansi.Strip(next.(Model).render()), "\n")
		foot := slices.IndexFunc(out, func(l string) bool { return strings.Contains(l, "╰─ ⏎ run") })
		phone := next.(Model).layoutAt(w) == Narrow || next.(Model).upright()
		if !strings.Contains(actionLine(next), "tmux switch-client -t %3") || foot < 0 || phone != (foot == len(out)-3) {
			t.Fatalf("width %d: the confirm box is not where it belongs:\n%s", w, strings.Join(out, "\n"))
		}
		next, _ = key(next, "esc")
		next = selectPane(t, next, "%2")
		next, _ = key(next, "a")
		next = typeText(next, "yes")
		out = strings.Split(ansi.Strip(next.(Model).render()), "\n")
		if w < 60 && (!strings.Contains(out[len(out)-3], "⏎ send") || !strings.Contains(out[len(out)-4], "› yes▏")) {
			t.Fatalf("width %d: the peek does not sit on the key line:\n%s", w, strings.Join(out, "\n"))
		}
		if !strings.Contains(strings.Join(out, "\n"), "› yes▏") {
			t.Fatalf("width %d: the answer is not typed in the peek:\n%s", w, strings.Join(out, "\n"))
		}
	}
}

// TestATapOnAKeyPressesIt: each key of the key line is a tap target.
func TestATapOnAKeyPressesIt(t *testing.T) {
	st := queueState()
	st.Agents = []string{"claude"}
	m, _ := loadedWith(t, 50, st)
	out := strings.Split(ansi.Strip(m.render()), "\n")
	y, line := len(out)-1, out[len(out)-1]
	next, _ := tap(m, len([]rune(line[:strings.Index(line, "N new")])), y)
	if f := next.(Model).form; f == nil || f.rename {
		t.Fatalf("a tap on N new opened %+v", f)
	}
	next, _ = tap(m, len([]rune(line[:strings.Index(line, "x kill")])), y)
	if p := next.(Model).pending; p == nil || p.rec != "kill:%1" {
		t.Fatalf("a tap on x kill: pending %+v", p)
	}
	next, _ = tap(m, len([]rune(line[:strings.Index(line, "⏎ open")])), y)
	if p := next.(Model).pending; p == nil || p.String() != "tmux switch-client -t %1" {
		t.Fatalf("a tap on ⏎ open: pending %+v", p)
	}
	// The header's count of who waits selects the first one.
	moved := selectPane(t, m, "%3")
	head := ansi.Strip(strings.Split(moved.(Model).render(), "\n")[0])
	next, _ = tap(moved, len([]rune(head[:strings.Index(head, "● 2")])), 0)
	if s, ok := next.(Model).selectedSession(); !ok || s.Pane != "%1" {
		t.Fatalf("a tap on who waits selected %+v", s)
	}
}

// head is the header line, without its tap target.
func (m Model) head(w int) string {
	h, _, _ := m.header(w)
	return h
}

// selectPane goes to the top, then presses ↓ until the session of pane
// is selected.
func selectPane(t *testing.T, m tea.Model, pane string) tea.Model {
	t.Helper()
	for range m.(Model).rowCount() {
		m, _ = key(m, "up")
	}
	for range m.(Model).rowCount() {
		if s, ok := m.(Model).selectedSession(); ok && s.Pane == pane {
			return m
		}
		m, _ = key(m, "down")
	}
	t.Fatalf("no session in %s to select", pane)
	return m
}

// listOf is the list column of a screen line: its first w cells, the list
// width of a console w wide.
func listOf(line string, w int) string {
	lw := w
	if byWidth(w) != Narrow {
		lw = splitAt(w, byWidth(w))
	}
	r := []rune(line)
	return string(r[:min(lw, len(r))])
}

func stripped(s string) string { return ansi.Strip(s) }

// TestAToastSaysWhatRan: after an action the header's right says what ran,
// cut before the name is, then goes; a failure stays on the key line.
func TestAToastSaysWhatRan(t *testing.T) {
	m, _ := loadedWith(t, 44, queueState())
	next, cmd := m.Update(resultMsg(proto.Result{Ran: [][]string{{"tmux", "send-keys", "-t", "%2", "-l", "--", "a long answer for a narrow screen"}}}))
	head := strings.Split(screen(next), "\n")[0]
	if !strings.HasPrefix(head, " ▰ matchblox") || !strings.Contains(head, "ran: tmux") || !strings.Contains(head, "…") {
		t.Fatalf("header %q", head)
	}
	if cmd == nil {
		t.Fatal("the toast never goes")
	}
	gone, _ := next.Update(toastMsg{n: next.(Model).toasts})
	if head := strings.Split(screen(gone), "\n")[0]; strings.Contains(head, "ran:") || !strings.Contains(head, "● 2") {
		t.Fatalf("after the toast: %q", head)
	}
	failed, _ := m.Update(resultMsg(proto.Result{Err: "the pane is gone"}))
	if !strings.Contains(actionLine(failed), "failed: the pane is gone") || failed.(Model).toast != "" {
		t.Fatalf("a failure: key line %q, toast %q", actionLine(failed), failed.(Model).toast)
	}
}

// TestTheDockSaysTheDisk: the home's file system as a bar, its percent and
// its size, and / beside it when it is another.
func TestTheDockSaysTheDisk(t *testing.T) {
	st := queueState()
	st.Machine.Disks = []sample.Disk{{Path: "/home/user", Total: 1 << 40, Used: 710 << 30, Avail: 290 << 30}, {Path: "/", Total: 100 << 30, Used: 90 << 30, Avail: 10 << 30}}
	m, _ := loadedWith(t, 100, st)
	out := ansi.Strip(strings.Join(m.machineLines(60, false, ""), "\n"))
	if !strings.Contains(out, "disk ▰▰▰▰▰▰▱▱ 71% of 1 TB · / 90%") {
		t.Fatalf("the machine lacks the disk:\n%s", out)
	}
	if got := bytesWord(931 << 30); got != "931 GB" {
		t.Fatalf("bytesWord %q", got)
	}
}

// TestRowColumnsLineUp: every session row spans its pane, and its word
// starts at the same x on every row, at every width and shape.
func TestRowColumnsLineUp(t *testing.T) {
	for _, size := range [][2]int{{44, 31}, {86, 30}, {107, 60}, {132, 32}, {192, 60}} {
		w, h := size[0], size[1]
		m := loaded(t, w)
		next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
		m = next.(Model)
		lw, _ := m.panes(w)
		if lw == 0 {
			lw = w
		}
		x := -1
		for _, s := range m.all.sessions {
			line := ansi.Strip(m.sessionRow(s, false, lw)[0])
			if got := ansi.StringWidth(line); got != lw {
				t.Fatalf("%dx%d: %s's row is %d wide, want %d: %q", w, h, s.Name, got, lw, line)
			}
			word := m.wordOf(s)
			at := strings.LastIndex(line, " "+word+" ")
			if at < 0 {
				t.Fatalf("%dx%d: no word %q in %q", w, h, word, line)
			}
			if wx := ansi.StringWidth(line[:at]); x < 0 {
				x = wx
			} else if wx != x {
				t.Fatalf("%dx%d: %s's word is at %d, the others at %d: %q", w, h, s.Name, wx, x, line)
			}
		}
	}
}
