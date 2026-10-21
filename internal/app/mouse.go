package app

import (
	tea "charm.land/bubbletea/v2"
)

// tap is a click or a finger on a phone. A tap on a tab opens it, on a key
// of the key line presses it, on `● N wait for you` selects the first
// session that waits, on the dock's `machine ›` switches the panel. A tap
// on a row selects it and shows the step a second tap runs: an answer for
// a session that waits, else the row's Enter. The second tap runs it when
// it only moves this terminal, or asks to confirm it. A tap is never
// consent to a destructive step, and it cancels a pending one.
func (m Model) tap(ms tea.Mouse) (tea.Model, tea.Cmd) {
	if ms.Button != tea.MouseLeft || m.pair.owns() { // the pair page takes keys only
		return m, nil
	}
	if m.held != nil { // a tap is another key: it drops the question
		m.held, m.flash = nil, ""
		return m, nil
	}
	if m.splashing() {
		m.splashDone = true
		return m, nil
	}
	if m.pending != nil {
		m.flash, m.pending = "cancelled: "+m.pending.String(), nil
		return m, nil
	}
	if !m.have || m.mismatch {
		return m, nil
	}
	m.searching = false // keep the search; what the tap asks for shows on the key line
	w := max(m.width, minWidth)
	sc := m.draw(w)
	if m.sheet != nil {
		return m.tapSheet(ms, sc)
	}
	if m.form != nil {
		return m.tapForm(ms, sc)
	}
	if m.peek != nil {
		inside := ms.Y >= sc.peekAt && ms.Y < sc.peekAt+len(sc.peekRows) && ms.X >= sc.peekX && ms.X < sc.peekX+sc.peekW
		if !sc.peekInline || inside {
			return m.tapPeek(ms, sc)
		}
		// The list beside the pane is live: the tap closes the peek and
		// does what it does there.
		m.peek = nil
		sc = m.draw(w)
	}
	switch {
	case ms.Y == 0:
		if s, ok := spanAt([]span{sc.stokerAt}, ms.X); ok && s.press != "" {
			return m.openAgent()
		}
		if s, ok := spanAt([]span{sc.waits}, ms.X); ok && s.press != "" {
			m.setTab(tabSessions)
			if i := m.firstWaiting(); i >= 0 {
				m.selectRow(len(m.doors) + i)
			}
			m.flash = ""
		}
		return m, nil
	case ms.Y == 1:
		if s, ok := spanAt(sc.tabs, ms.X); ok {
			m.setTab(int(s.press[0] - '1'))
			m.flash = ""
			return m.seeStoker()
		}
		return m, nil
	case ms.Y == sc.keysAt:
		if s, ok := spanAt(sc.keys, ms.X); ok {
			m.armed = ""
			return m.keyed(s.press)
		}
		return m, nil
	case sc.dockAt >= 0 && ms.Y >= sc.dockAt && ms.Y < sc.dockAt+sc.dockH:
		if x0, x1 := m.dockToggle(w); ms.Y == sc.dockAt && m.phoneDock(w) && ms.X >= x0 && ms.X < x1 {
			m.cycleDock()
			return m, nil
		}
		if ms.X >= sc.stx0 && ms.X < sc.stx1 {
			return m.openStoker().seeStoker()
		}
		return m, nil
	}
	i := ms.Y - sc.bodyTop
	if i < 0 || i >= len(sc.rows) || ms.X >= sc.listW {
		return m, nil
	}
	if sc.rows[i] == headerRow {
		return m.tapHeader(ms.X)
	}
	if sc.rows[i] == recsLabelRow {
		m.foldRecs()
		return m, nil
	}
	if _, ok := m.selectedRec(); ok && sc.rows[i] == m.rowIndex() && ms.X >= sc.listW-dismissWidth {
		m.dismissRec()
		return m, nil
	}
	if sc.rows[i] < 0 {
		return m, nil
	}
	picked := m.view().index != nil && m.rowIndex() == sc.rows[i] && (m.tab != tabSessions || m.chose)
	m.selectRow(sc.rows[i])
	if m.tab == tabShips {
		// A tap on the selected ship goes to its session, as Enter does.
		m.armed, m.flash = "", ""
		if picked {
			return m.toDriver(), nil
		}
		return m, nil
	}
	if pane, _ := m.peekTarget(); pane != "" {
		// A session opens in place with its keys; a tap on it once it is
		// selected opens the peek, to answer it or to message it.
		m.armed, m.flash = "", ""
		if !picked {
			return m, nil // the default selection is not a tap
		}
		open := func(m Model) (tea.Model, tea.Cmd) { m.openPeek(pane); return m, nil }
		if m.night() && !m.stoker.Asked {
			m.held = open
			return m, nil
		}
		return open(m)
	}
	say, run := m.tapAct()
	if run == nil {
		m.armed, m.flash = "", "nothing to do here"
		return m, nil
	}
	// The second tap runs what the first one showed, and only that: a row
	// that moved under the finger shows its own step first.
	if m.armed != say {
		m.armed, m.flash = say, "tap again: "+say
		return m, nil
	}
	m.armed = ""
	if _, door := m.selectedDoor(); !door && m.night() && !m.stoker.Asked {
		m.held, m.flash = run, ""
		return m, nil
	}
	return run(m)
}

// tapAct is what a second tap on the selected row does, and what the
// first one shows: the row's Enter.
func (m Model) tapAct() (string, func(Model) (tea.Model, tea.Cmd)) {
	a := m.primary()
	if a == nil {
		return "", nil
	}
	return a.String(), func(m Model) (tea.Model, tea.Cmd) {
		if a.pair {
			return m.openPairDoor()
		}
		if a.nav && !a.destructive {
			return m.confirm(*a)
		}
		m.pending = a
		return m, nil
	}
}

// wheel is a swipe on a phone: it moves the selection, as ↑ and ↓ do.
func (m Model) wheel(ms tea.Mouse) (tea.Model, tea.Cmd) {
	m.held = nil // a swipe is another key: it drops the question
	switch ms.Button {
	case tea.MouseWheelUp:
		m.move(-1)
	case tea.MouseWheelDown:
		m.move(1)
	}
	m.armed = ""
	return m, nil
}
