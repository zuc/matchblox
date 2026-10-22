package app

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/blox-eng/matchblox/internal/proto"
)

func shipsAt(t *testing.T, w int, st proto.State) (Model, *fakeConn) {
	t.Helper()
	m, f := loadedWith(t, w, st)
	next, _ := key(m, "p")
	return next.(Model), f
}

// TestShipsShowTheirTrack: each ship's number, title and age, its track
// and its note; the tab counts the ships in flight.
func TestShipsShowTheirTrack(t *testing.T) {
	for _, w := range []int{44, 86} {
		m, _ := shipsAt(t, w, screensState(t))
		out := ansi.Strip(m.render())
		for _, want := range []string{"SHIPS 3", "#4551 Rate limits", "●━◉┄○┄○┄○  CI · 6 of 9 jobs", "●━●━◉┄○┄○  green · waits for your merge",
			"●━✕┄○┄○┄○  conflicts with main", "●━●━●━●━●  live", "by hearth-api"} {
			if !strings.Contains(out, want) {
				t.Fatalf("width %d: Ships lacks %q:\n%s", w, want, out)
			}
		}
	}
}

// TestShipKeys: Enter selects the session that drives the ship in
// Sessions; x on one that waits for your merge shows its merge and runs it
// only after a typed y; a on a stuck one opens the peek on its session.
func TestShipKeys(t *testing.T) {
	st := screensState(t)
	m, f := shipsAt(t, 86, st)
	next, _ := key(m, "enter")
	if s, ok := next.(Model).selectedSession(); next.(Model).tab != tabSessions || !ok || s.Name != "hearth-api" {
		t.Fatalf("enter went to tab %d, session %q", next.(Model).tab, s.Name)
	}

	next, _ = key(m, "down") // #4552 waits for your merge
	next, _ = key(next, "x")
	if out := actionLine(next); !strings.Contains(out, "gh pr merge 4552 --squash -R hearth/ember-billing") {
		t.Fatalf("x does not show the merge:\n%s", out)
	}
	if _, cmd := key(next, "enter"); cmd != nil || len(f.acts()) != 0 {
		t.Fatal("enter alone merged")
	}
	_, cmd := key(next, "y")
	drain(cmd)
	if acts := f.acts(); len(acts) != 1 || acts[0] != (proto.Act{RecID: "ship:hearth/ember-billing#4552", Which: "secondary", Confirm: "y"}) {
		t.Fatalf("acts %+v", acts)
	}

	next, _ = key(m, "down")
	next, _ = key(next, "down") // #4545 is stuck; flint-auth asks
	next, _ = key(next, "a")
	if p := next.(Model).peek; p == nil || p.pane != "%2" {
		t.Fatalf("a on a stuck ship: peek %+v flash %q", p, next.(Model).flash)
	}
	next, _ = key(m, "a") // #4551 runs
	if next.(Model).peek != nil || next.(Model).flash == "" {
		t.Fatal("a on a ship that runs opened the peek")
	}
}

// TestShipsSayWhyTheyAreBehind: the tab says plainly when GitHub's rate
// limit pauses the reading, when gh cannot read, and which repository
// failed.
func TestShipsSayWhyTheyAreBehind(t *testing.T) {
	st := screensState(t)
	r := *st.Ships
	r.Paused, r.Unknown, r.Errors = time.Date(2026, 10, 1, 23, 33, 0, 0, time.Local), "gh not signed in", []string{"hearth/hearth-api: HTTP 502"}
	st.Ships = &r
	m, _ := shipsAt(t, 132, st)
	out := ansi.Strip(m.render())
	for _, want := range []string{"ships read again at 23:33", "GitHub cannot be read: gh not signed in", "not read: hearth/hearth-api: HTTP 502"} {
		if !strings.Contains(out, want) {
			t.Fatalf("Ships lacks %q:\n%s", want, out)
		}
	}
	st.Ships = nil
	m, _ = shipsAt(t, 50, st)
	if out := ansi.Strip(m.render()); !strings.Contains(out, "No ships yet.") || strings.Contains(out, "SHIPS 0") {
		t.Fatalf("no ships:\n%s", out)
	}
}

// A session's row and detail show the ship it drives.
func TestASessionShowsItsShip(t *testing.T) {
	m, _ := loadedWith(t, 132, screensState(t))
	out := ansi.Strip(m.render())
	if !strings.Contains(rowOf(out, "flint-auth"), "#4545 ●━✕┄○┄○┄○") {
		t.Fatalf("the row lacks the ship:\n%s", out)
	}
}

// A tap selects a ship; a tap on the selected one goes to its session.
func TestATapOnAShipGoesToItsSession(t *testing.T) {
	m, _ := shipsAt(t, 44, screensState(t))
	y := lineOf(t, m, "#4552")
	next, _ := tap(m, 5, y)
	if sh, _ := next.(Model).selectedShip(); sh.Number != 4552 || next.(Model).tab != tabShips {
		t.Fatalf("the first tap: ship %d tab %d", sh.Number, next.(Model).tab)
	}
	next, _ = tap(next, 5, lineOf(t, next, "#4552"))
	if s, ok := next.(Model).selectedSession(); next.(Model).tab != tabSessions || !ok || s.Name != "ember-billing" {
		t.Fatalf("the second tap: tab %d session %q", next.(Model).tab, s.Name)
	}
}
