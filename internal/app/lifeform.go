package app

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"

	"github.com/blox-eng/matchblox/internal/proto"
	"github.com/blox-eng/matchblox/internal/queue"
	"github.com/blox-eng/matchblox/internal/sample"
)

// A session's life from the console (#74): N opens New session, r opens
// Rename, x kills (the secondary action of a session's row).

// killAnyway is the Text of a kill confirmed a second time (the service's
// KillAnyway).
const killAnyway = "anyway"

// The fields of the New session form, in the order Tab walks them.
const (
	fieldWhere = iota
	fieldBranch
	fieldName
	fieldAgent
	fieldFirst
)

var formLabels = [...]string{"where", "branch", "name", "agent", "first"}

// form is the open New session or Rename form. Rename has the name only.
type form struct {
	rename    bool
	pane, who string // Rename: the session's pane and its name now
	at        int    // the field the cursor is on
	where     string
	branch    string
	name      string
	named     bool // the name was typed: it no longer follows where
	agent     string
	first     string
	whereAt   int    // ←→ on where: the Git path it shows (-1: typed)
	act, err  string // the act in flight, and why the last one did not go
}

const maxField = 300

// openNew is N: the New session form, where from the selected row (a Git
// repository or worktree, a session's directory), or the first repository.
func (m Model) openNew() (tea.Model, tea.Cmd) {
	switch {
	case m.conn == nil || m.lost:
		m.flash = "not sent, no connection"
		return m, nil
	case len(m.agents) == 0:
		m.flash = "no agent to start here: install claude, codex or opencode"
		return m, nil
	}
	f := &form{at: fieldWhere, whereAt: -1, agent: m.agents[0]}
	if slices.Contains(m.agents, m.lastAgent) {
		f.agent = m.lastAgent
	}
	switch m.tab {
	case tabGit:
		if wt, ok := m.selectedWorktree(); ok {
			f.where = wt.Path
		}
	case tabSessions:
		if s, ok := m.selectedSession(); ok {
			f.where = s.Cwd
		}
	}
	if paths := m.gitPaths(); f.where == "" && len(paths) > 0 {
		f.where, f.whereAt = paths[0], 0
	}
	if i := slices.Index(m.gitPaths(), f.where); i >= 0 {
		f.whereAt = i
	}
	m.form, m.pending, m.peek = f, nil, nil
	return m, nil
}

// openRename is r on a session: Rename, its name to edit.
func (m Model) openRename() (tea.Model, tea.Cmd) {
	s, ok := m.selectedSession()
	switch {
	case !ok:
		return m, nil
	case s.Pane == "":
		m.flash = s.Name + " is outside tmux: nothing to rename"
		return m, nil
	case m.conn == nil || m.lost:
		m.flash = "not sent, no connection"
		return m, nil
	}
	name := s.Name
	if proto.CheckName(name) != nil {
		name = proto.NameOf(name)
	}
	m.form, m.pending, m.peek = &form{rename: true, pane: s.Pane, who: s.Name, name: name, named: true, at: fieldName}, nil, nil
	return m, nil
}

// gitPaths are the repositories and worktrees of the Git tab, ←→ on where.
func (m Model) gitPaths() []string {
	var out []string
	for _, r := range m.allWorktreeRows() {
		if !slices.Contains(out, r.wt.Path) {
			out = append(out, r.wt.Path)
		}
	}
	return out
}

// fields are the form's fields, in Tab's order.
func (f *form) fields() []int {
	if f.rename {
		return []int{fieldName}
	}
	return []int{fieldWhere, fieldBranch, fieldName, fieldAgent, fieldFirst}
}

func (f *form) text(field int) *string {
	switch field {
	case fieldWhere:
		return &f.where
	case fieldBranch:
		return &f.branch
	case fieldName:
		return &f.name
	case fieldFirst:
		return &f.first
	}
	return nil
}

// autoName is the name a new session takes when none is typed: the
// worktree's for a branch, else the directory's, as the service makes it.
func (f *form) autoName() string {
	base := filepath.Base(filepath.Clean(f.where))
	if f.where == "" {
		return ""
	}
	if f.branch != "" {
		return proto.NameOf(base + "-" + proto.NameOf(f.branch))
	}
	return proto.NameOf(base)
}

func (f *form) move(by int) {
	fs := f.fields()
	i := slices.Index(fs, f.at)
	f.at = fs[(i+by+len(fs))%len(fs)]
}

func (m Model) formKey(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	f := *m.form
	m.form = &f
	key := k.String()
	switch key {
	case "ctrl+c":
		return m.quit()
	case "esc":
		m.form = nil // nothing is sent
		return m, nil
	case "enter":
		return m.formEnter()
	case "tab", "down":
		f.move(1)
		return m, nil
	case "shift+tab", "up":
		f.move(-1)
		return m, nil
	}
	f.err = ""
	if f.at == fieldAgent {
		i := slices.Index(m.agents, f.agent)
		switch key {
		case "left":
			f.agent = m.agents[(i+len(m.agents)-1)%len(m.agents)]
		case "right", "space":
			f.agent = m.agents[(i+1)%len(m.agents)]
		}
		return m, nil
	}
	if f.at == fieldWhere && (key == "left" || key == "right") {
		paths := m.gitPaths()
		if len(paths) == 0 {
			return m, nil
		}
		n := len(paths)
		switch {
		case f.whereAt < 0 && key == "right":
			f.whereAt = 0
		case f.whereAt < 0:
			f.whereAt = n - 1
		case key == "right":
			f.whereAt = (f.whereAt + 1) % n
		default:
			f.whereAt = (f.whereAt + n - 1) % n
		}
		f.where = paths[f.whereAt]
		return m, nil
	}
	t := f.text(f.at)
	if t == nil {
		return m, nil
	}
	switch key {
	case "backspace":
		*t = trimLast(*t)
	default:
		if k.Text == "" || len([]rune(*t+k.Text)) > maxField {
			return m, nil
		}
		*t += k.Text
	}
	switch f.at {
	case fieldWhere:
		f.whereAt = -1
	case fieldName:
		f.named = f.name != ""
	}
	return m, nil
}

// formEnter sends the form: a refusal stays in it, where it can be fixed.
func (m Model) formEnter() (tea.Model, tea.Cmd) {
	f := *m.form
	m.form = &f
	if m.conn == nil || m.lost {
		f.err = "not sent, no connection"
		return m, nil
	}
	name := strings.TrimSpace(f.name)
	if f.rename || f.named {
		if err := proto.CheckName(name); err != nil {
			f.err, f.at = err.Error(), fieldName
			return m, nil
		}
	}
	var cmd tea.Cmd
	if f.rename {
		cmd = m.send(proto.Act{RecID: "rename:" + f.pane, Confirm: "y", Text: name})
		m = m.pairActed(action{rec: "rename:" + f.pane})
	} else {
		if strings.TrimSpace(f.where) == "" {
			f.err, f.at = "where does it start? a path, or ←→ for the Git tab's", fieldWhere
			return m, nil
		}
		n := proto.NewSession{Where: strings.TrimSpace(f.where), Branch: strings.TrimSpace(f.branch), Agent: f.agent, First: strings.TrimSpace(f.first)}
		if f.named {
			n.Name = name
		}
		b, _ := json.Marshal(n)
		cmd = m.send(proto.Act{RecID: "new:session", Confirm: "y", Text: string(b)})
	}
	f.act, f.err = fmt.Sprintf("a%d", m.acts), ""
	return m, cmd
}

// formResult takes the result of the form's act: it closes when the act
// went through, and a refusal stays in it.
func (m Model) formResult(r proto.Result) (Model, bool) {
	if m.form == nil || m.form.act != r.ActID {
		return m, false
	}
	f := *m.form
	m.form = &f
	f.act = ""
	switch {
	case r.Err != "":
		f.err = r.Err
		return m, true
	case len(r.Skipped) > 0 && f.rename && strings.HasSuffix(r.Skipped[0], "renames when it rests"):
		m.flash = r.Skipped[0]
	case len(r.Skipped) > 0:
		f.err = strings.Join(r.Skipped, "; ")
		return m, true
	case f.rename:
		m.flash = "renamed " + f.who + " to " + strings.TrimSpace(f.name)
	default:
		name := strings.TrimSpace(f.name)
		if !f.named {
			name = f.autoName()
		}
		m.flash = "started " + name + ": it shows in Sessions in a few seconds"
		m.lastAgent = f.agent
	}
	m.form = nil
	return m, true
}

// formBox is the form's lines in bw columns, the key a tap on each line
// presses, the footer's key spans, and the agent chips' line with the
// agent a tap on each chip picks.
func (m Model) formBox(bw int) (box, press []string, spans []span, agentsAt int, agents []span) {
	st, f := m.st, m.form
	iw := bw - 4
	var lines []string
	press = []string{""} // the box's top edge
	add := func(p string, l ...string) {
		for _, x := range l {
			lines, press = append(lines, x), append(press, p)
		}
	}
	field := func(id int, value, placeholder string) {
		label := fmt.Sprintf("%-7s", formLabels[id])
		lead := st.faint.Render(label) + "  "
		if f.at == id {
			lead = st.muted.Render(label) + st.accent.Render("› ")
		}
		room := iw - 9
		shown := tilde(value)
		if w := lipgloss.Width(shown); w > room-1 {
			shown = "…" + string([]rune(shown)[len([]rune(shown))-(room-2):])
		}
		body := st.text.Render(shown)
		if value == "" {
			body = st.faint.Render(fit(placeholder, room-1))
		}
		if f.at == id {
			body += st.accent.Render("▏")
		}
		add(fmt.Sprintf("field:%d", id), lead+body)
	}
	title := st.accent.Bold(true).Render("New session")
	if f.rename {
		title = st.accent.Bold(true).Render("Rename " + f.who)
		field(fieldName, f.name, "")
		add("", "", st.faint.Render(fit("tmux and the agent take this name", iw)))
	} else {
		hint := "a path"
		if len(m.gitPaths()) > 0 {
			hint = "a path, or ←→ for the Git tab's"
		}
		field(fieldWhere, f.where, hint)
		field(fieldBranch, f.branch, "blank: no new worktree")
		name := f.name
		if !f.named {
			name = ""
		}
		field(fieldName, name, f.autoName())
		agentsAt = len(press)
		line, chips := m.agentChips(iw)
		add(fmt.Sprintf("field:%d", fieldAgent), line)
		agents = chips
		field(fieldFirst, f.first, "blank: it waits for you")
	}
	if f.act != "" {
		add("", "", st.muted.Render("starting…"))
	}
	if f.err != "" {
		add("", "")
		for _, l := range wrapPlain(f.err, iw-2) {
			add("", st.warn.Render("! ")+st.text.Render(l))
		}
	}
	box, spans = boxFrame(st.hair, title, lines, m.formFoot(), bw)
	press = append(press, "") // the box's bottom edge: its footer keys are spans
	return box, press, spans, agentsAt, agents
}

// agentChips is the agent chips, the one chosen filled, with the span of
// each a tap picks.
func (m Model) agentChips(iw int) (string, []span) {
	st, f := m.st, m.form
	label := fmt.Sprintf("%-7s", formLabels[fieldAgent])
	line := st.faint.Render(label) + "  "
	if f.at == fieldAgent {
		line = st.muted.Render(label) + st.accent.Render("› ")
	}
	x := 9
	var spans []span
	for i, a := range m.agents {
		if i > 0 {
			line += "  "
			x += 2
		}
		chip, style := "○ "+a, st.muted
		if a == f.agent {
			chip, style = "● "+a, st.accent
		}
		if x+lipgloss.Width(chip) > iw {
			break
		}
		line += style.Render(chip)
		spans = append(spans, span{x + 2, x + 2 + lipgloss.Width(chip), a}) // + the box's "│ "
		x += lipgloss.Width(chip)
	}
	return line, spans
}

func (m Model) formFoot() []footPart {
	st := m.st
	key := func(k, label, press string) footPart {
		t := st.accent.Render(k)
		if label != "" {
			t += st.muted.Render(" " + label)
		}
		return footPart{t, press, " ─ "}
	}
	if m.form.rename {
		return []footPart{key("⏎", "rename", "enter"), key("esc", "", "esc")}
	}
	return []footPart{key("⏎", "start", "enter"), key("tab", "next", "tab"), key("esc", "", "esc")}
}

func (m Model) overlayForm(sc *drawn, w int) {
	phone := m.layoutAt(w) == Narrow || m.upright()
	bw := min(w, 72)
	if !phone {
		bw = min(62, w-8)
	}
	box, press, spans, agentsAt, agents := m.formBox(bw)
	m.overlayBox(sc, w, bw, phone, box, press, spans)
	sc.formAgentsAt, sc.formAgents = sc.peekAt+agentsAt, agents
}

// tapForm: a tap on a field puts the cursor there, one on an agent chip
// picks it, one on a key of the footer presses it.
func (m Model) tapForm(ms tea.Mouse, sc drawn) (tea.Model, tea.Cmd) {
	i, dx := ms.Y-sc.peekAt, ms.X-sc.peekX
	if i < 0 || i >= len(sc.peekRows) || dx < 0 || dx >= sc.peekW {
		return m, nil
	}
	f := *m.form
	if ms.Y == sc.formAgentsAt {
		if c, ok := spanAt(sc.formAgents, dx); ok {
			f.agent, f.at = c.press, fieldAgent
			m.form = &f
			return m, nil
		}
	}
	if i == len(sc.peekRows)-1 {
		if s, ok := spanAt(sc.peekKeys, dx); ok {
			return m.formKey(keyPress(s.press))
		}
		return m, nil
	}
	var id int
	if _, err := fmt.Sscanf(sc.peekRows[i], "field:%d", &id); err == nil {
		f.at = id
		m.form = &f
	}
	return m, nil
}

// killAction is x on a session: kill-pane, then a TERM for an agent that
// outlived it. One that works or waits asks for a second y.
func (m Model) killAction(s sample.Session) *action {
	if s.Pane == "" {
		return nil
	}
	if sv, _ := sample.SplitPane(s.Pane); sv != "" && sv == m.stokerServer {
		return nil // the stoker's own session: Q on its tab quits it
	}
	steps := proto.KillSteps(s.Pane, s.PID)
	say := "kill " + s.Name + ": " + shellLine(steps[0])
	if len(steps) > 1 {
		say += ", then " + shellLine(steps[1])
	}
	a := &action{label: "kill", steps: steps, destructive: true, rec: "kill:" + s.Pane, which: "secondary", say: say}
	switch {
	case s.Ask != nil || s.Permits || m.waitsFor(s.Pane):
		a.again = s.Name + " waits for you: y again kills it all the same"
	case s.Works():
		a.again = s.Name + " works: y again kills it mid-turn"
	}
	return a
}

// waitsFor tells if the queue says the session in pane waits for you.
func (m Model) waitsFor(pane string) bool {
	for _, it := range m.queue {
		if it.Pane == pane && (it.State == queue.StatePermission || it.State == queue.StateQuestion && !it.Reminder) {
			return true
		}
	}
	return false
}
