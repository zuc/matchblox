package app

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/blox-eng/matchblox/internal/gitscan"
	"github.com/blox-eng/matchblox/internal/proto"
	"github.com/blox-eng/matchblox/internal/queue"
	"github.com/blox-eng/matchblox/internal/sample"
)

func screen(m tea.Model) string { return ansi.Strip(m.(Model).render()) }

// actionLine is the key line, at the bottom.
func actionLine(m tea.Model) string {
	if mm, ok := m.(Model); ok && mm.consenting() {
		return consentText(mm)
	}
	lines := strings.Split(screen(m), "\n")
	return lines[len(lines)-1]
}

// consentText is the question box's words on one line: "title: lines ·
// footer", its edges dropped.
func consentText(m Model) string {
	box := m.consentBox(72)
	edge := func(l string) string { return strings.Trim(ansi.Strip(l), "╭╮╰╯─ ") }
	var out []string
	for _, l := range box[1 : len(box)-1] {
		if l = strings.TrimSpace(strings.Trim(ansi.Strip(l), "│")); l != "" {
			out = append(out, l)
		}
	}
	return edge(box[0]) + ": " + strings.Join(out, " ") + " · " + edge(box[len(box)-1])
}

// TestSlashSearchesTheTab: / starts a search of the open tab; while it is
// typed every letter is text, q too; Enter keeps it, Esc clears it.
func TestSlashSearchesTheTab(t *testing.T) {
	m, _ := loadedWith(t, 100, fixtureState())
	var next tea.Model = m
	next, _ = key(next, "1")
	next, _ = key(next, "/")
	next = typeText(next, "review q")
	if next.(Model).quitting {
		t.Fatal("q quit while typing a search")
	}
	next, _ = key(next, "backspace")
	next, _ = key(next, "backspace")
	if out := screen(next); !strings.Contains(out, "app-review") || strings.Contains(out, "app-feature") {
		t.Fatalf("the search did not narrow the sessions:\n%s", out)
	}
	if !strings.Contains(actionLine(next), "/ review") {
		t.Fatalf("the search is not on the action line: %q", actionLine(next))
	}
	next, _ = key(next, "enter")
	if next.(Model).searching {
		t.Fatal("enter did not keep the search")
	}
	if !strings.Contains(actionLine(next), "/ review") {
		t.Fatalf("a kept search is not shown: %q", actionLine(next))
	}
	if s, ok := next.(Model).selected(); !ok || s.Name != "app-review" {
		t.Fatalf("the selection is not in the found rows: %+v", s)
	}
	next, _ = key(next, "esc")
	if out := screen(next); !strings.Contains(out, "app-feature") {
		t.Fatalf("esc did not clear the search:\n%s", out)
	}
}

func TestSearchEndsWithTheTab(t *testing.T) {
	m, _ := loadedWith(t, 100, fixtureState())
	var next tea.Model = m
	next, _ = key(next, "1")
	next, _ = key(next, "/")
	next = typeText(next, "review")
	next, _ = key(next, "enter")
	next, _ = key(next, "3")
	next, _ = key(next, "1")
	if out := screen(next); !strings.Contains(out, "app-feature") {
		t.Fatalf("the search stayed after a tab change:\n%s", out)
	}
}

func TestSearchFindsNothingSaysSo(t *testing.T) {
	m, _ := loadedWith(t, 100, queueState())
	next, _ := key(m, "/")
	next = typeText(next, "zzz")
	if out := screen(next); !strings.Contains(out, "nothing matches zzz") {
		t.Fatalf("no empty state for the search:\n%s", out)
	}
}

func sortFixture() proto.State {
	st := fixtureState()
	base := st.Sessions[0]
	mk := func(pid int, name string, busy bool, idle time.Duration, ctx, cpu float64) sample.Session {
		s := base
		s.PID, s.Name, s.Busy, s.Idle, s.Context, s.ContextPct, s.CPU = pid, name, busy, idle, "known", ctx, cpu
		s.Pane, s.Target, s.Do = "%"+name, "w:"+name, ""
		return s
	}
	st.Sessions = []sample.Session{
		mk(1, "alpha", false, 10*time.Minute, 20, 1),
		mk(2, "bravo", true, 0, 50, 30),
		mk(3, "charlie", false, time.Hour, 90, 2),
	}
	st.Queue = nil
	return st
}

func order(m tea.Model) string {
	var names []string
	for _, s := range m.(Model).snap.Sessions {
		names = append(names, s.Name)
	}
	return strings.Join(names, " ")
}

// TestSessionsBusyFirst: with nobody waiting, who works is on top, then
// the newest idle; the oldest are at the bottom (owner, 2026-10-08).
func TestSessionsBusyFirst(t *testing.T) {
	m, _ := loadedWith(t, 100, sortFixture())
	if got := order(m); got != "bravo alpha charlie" {
		t.Fatalf("order %q", got)
	}
}

func TestSortCyclesAndReverses(t *testing.T) {
	m, _ := loadedWith(t, 100, sortFixture())
	var next tea.Model = m
	next, _ = key(next, "1")
	for range 3 {
		next, _ = key(next, "g") // one flat list
	}
	next, _ = key(next, "s") // newest
	next, _ = key(next, "s") // context
	if got := order(next); got != "charlie bravo alpha" {
		t.Fatalf("by context: %q", got)
	}
	if out := screen(next); !strings.Contains(out, "3 SESSIONS · by context ▾") || strings.Contains(out, "WORKING") {
		t.Fatalf("the list does not show the sort, or keeps its groups:\n%s", out)
	}
	next, _ = key(next, "S")
	if got := order(next); got != "alpha bravo charlie" {
		t.Fatalf("by context, reversed: %q", got)
	}
	// The order holds when the next state comes.
	next, _ = next.Update(stateMsg(sortFixture()))
	if got := order(next); got != "alpha bravo charlie" {
		t.Fatalf("a new state undid the sort: %q", got)
	}
}

func TestNarrowShowsTheSort(t *testing.T) {
	m, _ := loadedWith(t, 50, sortFixture())
	var next tea.Model = m
	next, _ = key(next, "1")
	next, _ = key(next, "s")
	if !strings.Contains(screen(next), "by newest ▾") {
		t.Fatalf("the phone does not show the sort:\n%s", screen(next))
	}
}

func TestTapOnAHeaderSorts(t *testing.T) {
	m, _ := loadedWith(t, 100, queueState())
	var next tea.Model = m
	next, _ = key(next, "6")
	y := lineOf(t, next, "PATH")
	x := strings.Index(strings.Split(screen(next), "\n")[y], "RUNS")
	next, _ = tap(next, x+1, y)
	if s := next.(Model).paneSort; s.col != paneByRuns || s.rev {
		t.Fatalf("a tap on RUNS: %+v", s)
	}
	next, _ = tap(next, x+1, y)
	if s := next.(Model).paneSort; s.col != paneByRuns || !s.rev {
		t.Fatalf("a second tap on RUNS did not reverse: %+v", s)
	}
}

func TestPanesSort(t *testing.T) {
	m, _ := loadedWith(t, 100, queueState())
	var next tea.Model = m
	next, _ = key(next, "6")
	next, _ = key(next, "s")
	rows := next.(Model).paneRows()
	for i := 1; i < len(rows); i++ {
		if rows[i-1].what > rows[i].what {
			t.Fatalf("panes not by what runs: %+v", rows)
		}
	}
}

func gitState() proto.State {
	st := fixtureState()
	st.Git = &proto.GitReport{At: st.At, Repos: []gitscan.Repo{{Path: "/w/app", Main: "main", Worktrees: []gitscan.Worktree{
		{Path: "/w/app", Branch: "main"},
		{Path: "/w/wt/done-1", Branch: "done-1", Merged: true, Safe: true, Remove: []string{"git", "-C", "/w/app", "worktree", "remove", "/w/wt/done-1"}},
		{Path: "/w/wt/done-2", Branch: "done-2", Merged: true, Safe: true, Remove: []string{"git", "-C", "/w/app", "worktree", "remove", "/w/wt/done-2"}},
		{Path: "/w/wt/busy", Branch: "busy", Merged: true, Dirty: 3},
	}}}}
	return st
}

// TestRemoveEverySafeWorktree: X marks every safe worktree, x asks once
// for a typed y, and the service gets one guarded act for each.
func TestRemoveEverySafeWorktree(t *testing.T) {
	m, f := loadedWith(t, 100, gitState())
	var next tea.Model = m
	next, _ = key(next, "4")
	next, _ = key(next, "X")
	if n := strings.Count(screen(next), "●"); n != 2 {
		t.Fatalf("%d marked, want the 2 safe ones:\n%s", n, screen(next))
	}
	next, _ = key(next, "x")
	if line := actionLine(next); !strings.Contains(line, "git -C /w/app worktree remove /w/wt/done-1 git -C /w/app worktree remove /w/wt/done-2") || !strings.Contains(line, "y run") {
		t.Fatalf("the confirm does not show the steps: %q", line)
	}
	next, cmd := key(next, "y")
	if cmd == nil {
		t.Fatal("y sent nothing")
	}
	for _, msg := range collect(cmd) {
		next, _ = next.Update(msg)
	}
	acts := f.acts()
	if len(acts) != 2 || acts[0] != (proto.Act{RecID: "worktree:/w/wt/done-1", Which: "secondary", Confirm: "y"}) ||
		acts[1].RecID != "worktree:/w/wt/done-2" {
		t.Fatalf("sent %+v", acts)
	}
	next, _ = next.Update(resultMsg(proto.Result{ActID: "a1", Ran: [][]string{{"git"}}}))
	next, _ = next.Update(resultMsg(proto.Result{ActID: "a2", Skipped: []string{"has changes"}}))
	if line := actionLine(next); !strings.Contains(line, "removed 1, skipped 1: has changes") {
		t.Fatalf("no summary: %q", line)
	}
	if strings.Contains(screen(next), "●") {
		t.Fatal("the marks stayed after the removal")
	}
}

func TestSpaceMarksOnlyASafeWorktree(t *testing.T) {
	m, _ := loadedWith(t, 100, gitState())
	var next tea.Model = m
	next, _ = key(next, "4")
	for range 3 {
		next, _ = key(next, "down")
	}
	next, _ = key(next, " ")
	if strings.Contains(screen(next), "●") || !strings.Contains(actionLine(next), "not safe to remove") {
		t.Fatalf("an unsafe worktree was marked, or no reason: %q", actionLine(next))
	}
	next, _ = key(next, "up")
	next, _ = key(next, " ")
	if strings.Count(screen(next), "●") != 1 {
		t.Fatal("space did not mark the safe worktree")
	}
	next, _ = key(next, " ")
	if strings.Contains(screen(next), "●") {
		t.Fatal("a second space did not unmark")
	}
}

// collect runs a command, and the commands of a batch, for their messages.
func collect(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if b, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Msg
		for _, c := range b {
			out = append(out, collect(c)...)
		}
		return out
	}
	if msg == nil {
		return nil
	}
	return []tea.Msg{msg}
}

// Review 2, #1: without a connection a bulk removal is not sent, the marks
// stay, and a batch cut by a lost connection says what was pending.
func TestBatchWithoutAConnectionIsNotSent(t *testing.T) {
	m, f := loadedWith(t, 100, gitState())
	var next tea.Model = m
	next, _ = key(next, "4")
	next, _ = key(next, "X")
	got := next.(Model)
	got.lost = true
	next, _ = key(got, "x")
	next, cmd := key(next, "y")
	if cmd != nil {
		for _, msg := range collect(cmd) {
			next, _ = next.Update(msg)
		}
	}
	if len(f.acts()) != 0 || !strings.Contains(actionLine(next), "not sent, no connection") {
		t.Fatalf("sent %v, line %q", f.acts(), actionLine(next))
	}
	if n := strings.Count(screen(next), "●"); n != 2 {
		t.Fatalf("the marks went: %d", n)
	}

	got = next.(Model)
	got.lost = false
	got.batch = &batch{ids: map[string]bool{"a1": true, "a2": true}, removed: 1}
	next, _ = got.Update(lostMsg{err: errors.New("broken pipe"), conn: got.conn})
	if next.(Model).batch != nil || !strings.Contains(next.(Model).flash, "2 removals have no answer") {
		t.Fatalf("a lost batch: %+v %q", next.(Model).batch, next.(Model).flash)
	}
}

// Review 2, #2: a search on the Git tab hides rows, not marks.
func TestSearchKeepsTheMarks(t *testing.T) {
	m, _ := loadedWith(t, 100, gitState())
	var next tea.Model = m
	next, _ = key(next, "4")
	next, _ = key(next, "down")
	next, _ = key(next, " ")
	next, _ = key(next, "/")
	next = typeText(next, "done-2")
	next, _ = key(next, "enter")
	next, _ = key(next, " ")
	if p := next.(Model).picked; !p["/w/wt/done-1"] || !p["/w/wt/done-2"] {
		t.Fatalf("marks %v", p)
	}
	next, _ = key(next, "x")
	if line := actionLine(next); !strings.Contains(line, "/w/wt/done-1") || !strings.Contains(line, "/w/wt/done-2") {
		t.Fatalf("the hidden mark does not count: %q", line)
	}
	got := next.(Model)
	got.git = nil
	got.refresh()
	if len(got.picked) != 2 {
		t.Fatal("a state without git dropped the marks")
	}
}

// Review 2, #5: the plan cell holds its column at any percent.
func TestProgressHoldsItsColumn(t *testing.T) {
	st := sortFixture()
	st.Sessions[0].Progress = &sample.Progress{Pct: 5}
	st.Sessions[1].Progress = &sample.Progress{Pct: 100}
	m, _ := loadedWith(t, 132, st)
	next, _ := key(m, "1")
	lines := strings.Split(screen(next), "\n")
	a, b := lines[sessLine(t, next, "alpha")], lines[sessLine(t, next, "bravo")]
	at := func(line, place string) int { return lipgloss.Width(line[:max(strings.Index(line, place), 0)]) }
	if ia, ib := at(a, "w:alpha"), at(b, "w:bravo"); !strings.Contains(a, "5%") || ia == 0 || ia != ib {
		t.Fatalf("the place column moved (%d, %d):\n%s\n%s", ia, ib, a, b)
	}
}

// Review 2, #9: a tap ends the typing of a search, so a confirm it asks
// for is on the screen.
func TestTapEndsTheTypingOfASearch(t *testing.T) {
	m, _ := loadedWith(t, 100, queueState())
	next, _ := key(m, "/")
	next = typeText(next, "app")
	y := lineOf(t, next, "Clear app-review")
	next, _ = tap(next, 5, y)
	if next.(Model).searching || next.(Model).filter != "app" {
		t.Fatalf("searching %v filter %q", next.(Model).searching, next.(Model).filter)
	}
	if !strings.Contains(actionLine(next), "tap again") {
		t.Fatalf("line %q", actionLine(next))
	}
}

// TestAnOpenPullRequestShowsWhereItsAgentShows: the PR of the worktree an
// agent works in is on its desk row, in the detail, and under its row
// when it opens on a phone.
func TestAnOpenPullRequestShowsWhereItsAgentShows(t *testing.T) {
	st := fixtureState()
	st.Sessions = append(st.Sessions, sample.Session{PID: 950, Pane: "%9", Target: "api:1.1", Name: "cache-work",
		Status: "idle", Idle: time.Minute, Cwd: "/w/app-cache/internal", Context: "fresh"})
	st.Queue = []queue.Item{{Pane: "%9", Target: "api:1.1", Name: "cache-work", State: queue.StateQuestion,
		Since: st.At.Add(-time.Minute), LastLine: "Shall I merge it?"}}
	st.Git = &proto.GitReport{At: st.At, Repos: []gitscan.Repo{{Path: "/w/app", Main: "main", Worktrees: []gitscan.Worktree{
		{Path: "/w/app", Branch: "main"},
		{Path: "/w/app-cache", Branch: "cache", Sessions: 1, PR: &gitscan.PR{Number: 4312, Title: "Add the cache", URL: "https://example.com/pr/4312"}},
	}}}}
	m, _ := loadedWith(t, 132, st) // cache-work waits: it is selected
	if l := rowOf(m.render(), "cache-work"); !strings.Contains(listOf(l, 132), "#4312") {
		t.Fatalf("the desk row lacks the PR: %q", l)
	}
	for _, w := range []int{86, 132} {
		m.width = w
		out := ansi.Strip(m.render())
		for _, want := range []string{"ship     #4312 Add the cache", "https://example.com/pr/4312"} {
			if !strings.Contains(out, want) {
				t.Fatalf("width %d: the detail lacks %q:\n%s", w, want, out)
			}
		}
	}
	m.width = 44 // a phone
	out := strings.Split(ansi.Strip(m.render()), "\n")
	if at := sessLine(t, m, "cache-work"); !strings.Contains(out[at+2], "#4312 Add the cache") {
		t.Fatalf("the phone row does not open on its PR:\n%s", strings.Join(out, "\n"))
	}
}

func TestRemovingOneWorktreeSaysWorktree(t *testing.T) {
	st := gitState()
	wts := st.Git.Repos[0].Worktrees
	st.Git.Repos[0].Worktrees = append(wts[:2:2], wts[3])
	m, _ := loadedWith(t, 100, st)
	var next tea.Model = m
	for _, k := range []string{"4", "X", "x", "y"} {
		next, _ = key(next, k)
	}
	if line := actionLine(next); !strings.Contains(line, "removing 1 worktree…") {
		t.Fatalf("action line %q", line)
	}
}
