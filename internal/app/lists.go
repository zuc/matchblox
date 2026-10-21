package app

import (
	"fmt"
	"maps"
	"slices"
	"strconv"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/blox-eng/matchblox/internal/advice"
	"github.com/blox-eng/matchblox/internal/proto"
	"github.com/blox-eng/matchblox/internal/queue"
	"github.com/blox-eng/matchblox/internal/sample"
)

// lists is what the service sent, before the search and the sort of the
// open tab. The console draws from the copies in the Model.
type lists struct {
	queue    []queue.Item
	recs     []advice.Rec
	sessions []sample.Session
	orphans  []sample.Orphan
}

// sortBy is a column and its direction: rev is the other way from the
// column's own (busy first, longest wait first, A to Z).
type sortBy struct {
	col int
	rev bool
}

const (
	sortState = iota
	sortIdle
	sortContext
	sortCPU
	sortName
	sortPlace
	sortAccount
)

var sessCols = []string{"state", "newest", "context", "cpu", "name", "tmux", "account"}

const (
	paneByPlace = iota
	paneByRuns
	paneByPath
)

var paneCols = []string{"tmux", "runs", "path"}

// headerRow marks the column header line of a body: a tap there sorts.
const headerRow = -2

func (s sortBy) arrow() string {
	if s.rev {
		return "▴"
	}
	return "▾"
}

// refresh draws the lists again from what the service sent: the search of
// the open tab, then the sort.
func (m *Model) refresh() {
	q := strings.ToLower(m.filter)
	keep := func(fields ...string) bool {
		if q == "" {
			return true
		}
		return strings.Contains(strings.ToLower(strings.Join(fields, " ")), q)
	}
	m.queue, m.snap.Orphans = m.all.queue, m.all.orphans
	m.recs = slices.DeleteFunc(slices.Clone(m.all.recs), m.recPrefs.dismissed)
	// The stoker's own agent is not a lane: its waits show in the header.
	sessions := slices.DeleteFunc(slices.Clone(m.all.sessions), m.isStoker)
	switch m.tab {
	case tabSessions:
		m.recs = slices.DeleteFunc(m.recs, func(r advice.Rec) bool { return !keep(r.Title, r.Evidence) })
		sessions = slices.DeleteFunc(sessions, func(s sample.Session) bool {
			return !keep(s.Name, s.Target, s.Pane, s.Tab, s.Cwd, s.Model, m.lastWords(s), m.wordOf(s))
		})
	case tabMachine:
		m.snap.Orphans = slices.DeleteFunc(slices.Clone(m.all.orphans), func(o sample.Orphan) bool {
			return !keep(strconv.Itoa(o.PID), o.Comm, o.Cmdline, o.Cwd, o.Target)
		})
	}
	need, since := map[int]int{}, map[int]time.Time{}
	for _, s := range sessions {
		need[s.PID] = m.groupOf(s)
		if it, ok := m.waitOf(s); ok {
			since[s.PID] = it.Since
		}
	}
	rank := m.groupRanks(sessions, need)
	slices.SortStableFunc(sessions, func(a, b sample.Session) int {
		if c := rank[a.PID] - rank[b.PID]; c != 0 {
			return c // the groups keep their order; the sort works inside them
		}
		return m.sessSort.compare(a, b, need, since)
	})
	m.snap.Sessions = sessions
	if m.git != nil {
		for p := range m.picked {
			if !m.safeWorktree(p) {
				delete(m.picked, p)
			}
		}
	}
}

// groupRanks is the place of each session's group in the list: by need
// the groups' own order, by state the needier first, by repo the names
// A to Z.
func (m Model) groupRanks(sessions []sample.Session, need map[int]int) map[int]int {
	first := map[string]int{}
	for _, s := range sessions {
		l := m.groupLabel(s)
		if n, ok := first[l]; !ok || need[s.PID] < n {
			first[l] = need[s.PID]
		}
	}
	labels := slices.SortedFunc(maps.Keys(first), func(a, b string) int {
		if c := first[a] - first[b]; c != 0 && m.sessGroup != groupRepo {
			return c
		}
		return strings.Compare(a, b)
	})
	rank := map[int]int{}
	for _, s := range sessions {
		rank[s.PID] = slices.Index(labels, m.groupLabel(s))
	}
	return rank
}

// compare orders two sessions inside a group. group holds the need of
// each pid, since when it waits.
func (s sortBy) compare(a, b sample.Session, group map[int]int, since map[int]time.Time) int {
	if s.rev {
		a, b = b, a
	}
	desc := func(x, y float64) int { return -cmpF(x, y) }
	c := 0
	switch s.col {
	case sortState:
		// Who waits for the person, the oldest wait first; who works; then
		// the quiet ones, the newest first: the oldest sink to the bottom.
		c = group[a.PID] - group[b.PID]
		switch {
		case c != 0:
		case group[a.PID] == groupWaits:
			c = since[a.PID].Compare(since[b.PID])
		case group[a.PID] == groupQuiet, group[a.PID] == groupHeld:
			c = cmpF(float64(a.Idle), float64(b.Idle))
		}
	case sortIdle:
		c = cmpF(float64(a.Idle), float64(b.Idle))
	case sortContext:
		c = desc(a.ContextPct, b.ContextPct)
	case sortCPU:
		c = desc(a.CPU, b.CPU)
	case sortName:
		c = strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
	case sortPlace:
		c = strings.Compare(a.Target, b.Target)
	case sortAccount:
		c = strings.Compare(accountCell(a), accountCell(b))
	}
	if c == 0 {
		c = strings.Compare(a.Name, b.Name)
	}
	if c == 0 {
		c = a.PID - b.PID
	}
	return c
}

func cmpF(x, y float64) int {
	switch {
	case x < y:
		return -1
	case x > y:
		return 1
	}
	return 0
}

func (s sortBy) panes(a, b paneRow) int {
	if s.rev {
		a, b = b, a
	}
	c := 0
	switch s.col {
	case paneByRuns:
		c = strings.Compare(a.what, b.what)
	case paneByPath:
		c = strings.Compare(a.path, b.path)
	}
	if c == 0 {
		c = strings.Compare(a.target, b.target)
	}
	return c
}

// sortable is the sort of the open tab, when it has one.
func (m *Model) sortable() (*sortBy, []string) {
	switch m.tab {
	case tabSessions:
		return &m.sessSort, sessCols
	case tabPanes:
		return &m.paneSort, paneCols
	}
	return nil, nil
}

// sortBy sets the column, or turns it around when it is already the one.
func (m *Model) sortOn(col int) {
	s, names := m.sortable()
	if s == nil {
		return
	}
	if s.col == col {
		s.rev = !s.rev
	} else {
		*s = sortBy{col: col}
	}
	m.flash = "sort: " + names[s.col]
	if s.rev {
		m.flash += ", reversed"
	}
	m.refresh()
}

// sortNote names the sort where no header shows it (a phone).
func (m Model) sortNote(w int, s sortBy, names []string) string {
	if m.layoutAt(w) != Narrow {
		return ""
	}
	return "  ·  by " + names[s.col] + " " + s.arrow()
}

func (m Model) paneHeader() (string, []colSpan) {
	return header([]struct {
		label string
		w     int
		col   int
	}{{"TMUX", colPane, paneByPlace}, {"RUNS", colName, paneByRuns}, {"PATH", 30, paneByPath}}, m.paneSort, "")
}

type colSpan struct{ x0, x1, col int }

func header(cols []struct {
	label string
	w     int
	col   int
}, s sortBy, tail string) (string, []colSpan) {
	var b strings.Builder
	var spans []colSpan
	x := 1
	b.WriteString(" ")
	for _, c := range cols {
		label := c.label
		if c.col == s.col && c.col >= 0 {
			label += s.arrow()
		}
		b.WriteString(pad(label, c.w))
		if c.col >= 0 {
			spans = append(spans, colSpan{x, x + c.w, c.col})
		}
		x += c.w
	}
	return strings.TrimRight(b.String(), " ") + tail, spans
}

// tapHeader sorts Panes by the column under x.
func (m Model) tapHeader(x int) (tea.Model, tea.Cmd) {
	_, spans := m.paneHeader()
	for _, s := range spans {
		if x >= s.x0 && x < s.x1 {
			m.sortOn(s.col)
		}
	}
	return m, nil
}

// search edits the search of the open tab. Every printable key is text.
func (m Model) search(k tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch k.String() {
	case "esc":
		m.searching, m.filter = false, ""
	case "enter":
		m.searching = false
	case "ctrl+c":
		return m.quit()
	case "backspace":
		m.filter = trimLast(m.filter)
	default:
		if k.Text != "" && len([]rune(m.filter)) < 100 {
			m.filter += k.Text
		}
	}
	m.refresh()
	return m, nil
}

// setTab opens a tab; a search belongs to the tab it was typed on.
func (m *Model) setTab(t int) {
	m.tab, m.filter, m.searching, m.armed = t, "", false, ""
	if t == tabMachine {
		m.loadDevices()
	}
	if t == tabStoker {
		m.stokerSel = m.firstNeed() // its one action leads the key bar
	}
	m.refresh()
}

// nothing is the empty state of a list: the search when there is one.
func (m Model) nothing(text string) string {
	if m.filter != "" {
		return " nothing matches " + m.filter + "  esc clears"
	}
	return text
}

// safeWorktree reads every worktree, not the found ones: a search hides
// rows, not marks.
func (m Model) safeWorktree(path string) bool {
	for _, r := range m.allWorktreeRows() {
		if r.wt.Path == path {
			return r.wt.Safe && len(r.wt.Remove) > 0
		}
	}
	return false
}

// mark adds the selected worktree to the ones x removes, or takes it out.
func (m *Model) mark() {
	wt, ok := m.selectedWorktree()
	switch {
	case !ok:
	case m.picked[wt.Path]:
		delete(m.picked, wt.Path)
	case !wt.Safe:
		m.flash = "not safe to remove: " + unsafeWhy(wt)
	default:
		if m.picked == nil {
			m.picked = map[string]bool{}
		}
		m.picked[wt.Path] = true
	}
}

func unsafeWhy(wt proto.Worktree) string {
	switch {
	case wt.Sessions > 0:
		return "an agent works in it"
	case wt.InUse:
		return "a process is in it"
	case wt.Dirty != 0:
		return "it has changes"
	case !wt.Merged:
		return "its branch is not merged"
	}
	return "the main checkout"
}

// markAllSafe marks every safe worktree, or clears the marks when all of
// them are marked already.
func (m *Model) markAllSafe() {
	var safe []string
	for _, r := range m.worktreeRows() {
		if r.wt.Safe && len(r.wt.Remove) > 0 {
			safe = append(safe, r.wt.Path)
		}
	}
	all := len(safe) > 0 && len(m.picked) == len(safe)
	m.picked = map[string]bool{}
	if all {
		return
	}
	for _, p := range safe {
		m.picked[p] = true
	}
	if len(safe) == 0 {
		m.flash = "no worktree is safe to remove"
	}
}

// removeMarked is x with marks: every marked worktree, behind one typed y.
func (m Model) removeMarked() *action {
	var steps [][]string
	var paths []string
	for _, r := range m.allWorktreeRows() {
		if m.picked[r.wt.Path] {
			steps, paths = append(steps, r.wt.Remove), append(paths, r.wt.Path)
		}
	}
	if len(paths) == 0 {
		return nil
	}
	return &action{label: fmt.Sprintf("remove %d", len(paths)), steps: steps, destructive: true, batch: paths}
}

// batch counts the results of the acts of one confirm.
type batch struct {
	ids                      map[string]bool
	removed, skipped, failed int
	why                      string
}

// sendBatch sends one guarded act for each worktree: the service checks
// each one again and removes only what is still safe.
func (m Model) sendBatch(a action) (tea.Model, tea.Cmd) {
	b := &batch{ids: map[string]bool{}}
	var cmds []tea.Cmd
	for _, p := range a.batch {
		cmds = append(cmds, m.send(proto.Act{RecID: "worktree:" + p, Which: "secondary", Confirm: "y"}))
		b.ids["a"+strconv.Itoa(m.acts)] = true
	}
	m.batch, m.picked = b, nil
	m.flash = fmt.Sprintf("removing %d worktrees…", len(a.batch))
	if len(a.batch) == 1 {
		m.flash = "removing 1 worktree…"
	}
	return m, tea.Batch(cmds...)
}

// tally counts one result of the batch; the last one writes the summary.
func (m *Model) tally(r proto.Result) bool {
	if m.batch == nil || !m.batch.ids[r.ActID] {
		return false
	}
	b := m.batch
	delete(b.ids, r.ActID)
	switch {
	case r.Err != "":
		b.failed++
		b.why = r.Err
	case len(r.Skipped) > 0:
		b.skipped++
		b.why = r.Skipped[len(r.Skipped)-1]
	case len(r.Ran) > 0:
		b.removed++
	}
	if len(b.ids) > 0 {
		return true
	}
	text := fmt.Sprintf("removed %d", b.removed)
	if b.skipped > 0 {
		text += fmt.Sprintf(", skipped %d", b.skipped)
	}
	if b.failed > 0 {
		text += fmt.Sprintf(", failed %d", b.failed)
	}
	if b.why != "" {
		text += ": " + b.why
	}
	m.flash, m.batch = text, nil
	return true
}
