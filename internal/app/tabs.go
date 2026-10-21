package app

import "strconv"

// tabView is one tab: its rows and selection, what Enter and x do on the
// selected row, its keys and its body. The selection itself stays in the
// Model, so it lives across tab changes and new states.
type tabView struct {
	name      string
	keys      func(m Model) []hint // the key line, for the selected row
	count     func(m Model) int
	index     func(m Model) int
	pick      func(m *Model, i int)
	primary   func(m Model) *action // Enter: never destructive
	secondary func(m Model) *action // x: destructive, behind a typed y
	body      func(m Model, w int) body
}

// hint is a key on the key line: the key, its word, and the key a tap on
// it presses ("" for none).
type hint struct{ key, label, press string }

const (
	tabSessions = iota
	tabShips
	tabMachine
	tabGit
	tabHistory
	tabPanes
	tabStoker
)

var views = [...]tabView{
	tabSessions: {
		name:  "sessions",
		keys:  Model.sessionKeys,
		count: func(m Model) int { return len(m.doors) + len(m.snap.Sessions) + m.recRows() },
		index: Model.sessIndex,
		pick: func(m *Model, i int) {
			nd, ns := len(m.doors), len(m.snap.Sessions)
			switch {
			case i < nd:
				m.doorPick, m.doorAt, m.recPick = m.doors[i].ID, i, ""
			case i < nd+ns:
				m.doorPick, m.recPick, m.chose, m.selPID = "", "", true, m.snap.Sessions[i-nd].PID
			default:
				m.doorPick, m.recPick = "", m.recs[i-nd-ns].ID
			}
		},
		primary: func(m Model) *action {
			if d, ok := m.selectedDoor(); ok {
				return m.doorAction(d)
			}
			if r, ok := m.selectedRec(); ok {
				return fromAdvice(r, "primary")
			}
			if s, ok := m.selectedSession(); ok && s.Pane != "" {
				return m.jump(s.Pane)
			}
			return nil
		},
		secondary: func(m Model) *action {
			if d, ok := m.selectedDoor(); ok {
				return closeDoor(d)
			}
			if r, ok := m.selectedRec(); ok {
				return fromAdvice(r, "secondary")
			}
			if s, ok := m.selectedSession(); ok {
				return m.killAction(s)
			}
			return nil
		},
		body: Model.sessionsPanel,
	},
	tabShips: {
		name: "ships",
		keys: func(m Model) []hint {
			if sh, ok := m.selectedShip(); ok {
				return append(m.shipKeys(sh), hint{"f", "stoker", "f"})
			}
			return []hint{{"1", "sessions", "1"}, {"f", "stoker", "f"}}
		},
		count:     func(m Model) int { return len(m.ships()) },
		index:     func(m Model) int { return min(m.shipSel, max(len(m.ships())-1, 0)) },
		pick:      func(m *Model, i int) { m.shipSel = i },
		secondary: Model.mergeShip,
		body:      Model.shipsPanel,
	},
	tabMachine: {
		name: "machine",
		keys: func(m Model) []hint {
			if r, ok := m.selectedDevice(); ok {
				if r.plus {
					return []hint{{"⏎", "pair", "enter"}, {"f", "stoker", "f"}}
				}
				return []hint{{"x", "remove", "x"}, {"f", "stoker", "f"}}
			}
			if len(m.snap.Orphans) == 0 {
				return []hint{{"p", "ships", "p"}, {"f", "stoker", "f"}}
			}
			return []hint{{"⏎", "jump", "enter"}, {"x", "kill", "x"}, {"f", "stoker", "f"}}
		},
		count: Model.machineRows,
		index: Model.machineIndex,
		pick:  (*Model).machinePick,
		primary: func(m Model) *action {
			if r, ok := m.selectedDevice(); ok {
				if r.plus {
					return &action{label: "pair a device", say: "pair a device: show a one-time code", pair: true}
				}
				return nil
			}
			if o, ok := m.selectedOrphan(); ok && o.PaneAlive {
				return m.jump(o.Pane)
			}
			return nil
		},
		secondary: func(m Model) *action {
			if r, ok := m.selectedDevice(); ok {
				if r.plus {
					return nil
				}
				return m.removeDevice(r)
			}
			if o, ok := m.selectedOrphan(); ok {
				return &action{label: "kill", steps: [][]string{o.Kill}, destructive: true,
					rec: "orphan:" + strconv.Itoa(o.PID), which: "secondary"}
			}
			return nil
		},
		body: Model.machinePanel,
	},
	tabGit: {
		name: "git",
		keys: func(Model) []hint {
			return []hint{{"⏎", "shell", "enter"}, {"␣", "mark", "space"}, {"X", "all safe", "X"}, {"x", "remove", "x"}, {"r", "rescan", "r"}}
		},
		count: func(m Model) int { return len(m.worktreeRows()) },
		index: func(m Model) int { return min(m.gitSel, max(len(m.worktreeRows())-1, 0)) },
		pick:  func(m *Model, i int) { m.gitSel = i },
		primary: func(m Model) *action {
			if wt, ok := m.selectedWorktree(); ok {
				return &action{label: "shell", nav: true, steps: [][]string{{"tmux", "new-window", "-c", wt.Path}}}
			}
			return nil
		},
		secondary: func(m Model) *action {
			if a := m.removeMarked(); a != nil {
				return a
			}
			if wt, ok := m.selectedWorktree(); ok && wt.Safe {
				return &action{label: "remove", steps: [][]string{wt.Remove}, destructive: true,
					rec: "worktree:" + wt.Path, which: "secondary"}
			}
			return nil
		},
		body: Model.gitPanel,
	},
	tabHistory: {
		name: "history",
		keys: func(Model) []hint { return []hint{{"p", "ships", "p"}, {"f", "stoker", "f"}} },
		body: func(m Model, w int) body { return plain(m.historyPanel(w)) },
	},
	tabPanes: {
		name:  "panes",
		keys:  func(Model) []hint { return []hint{{"⏎", "go", "enter"}, {"s", "sort", "s"}} },
		count: func(m Model) int { return len(m.paneRows()) },
		index: func(m Model) int { return min(m.paneSel, max(len(m.paneRows())-1, 0)) },
		pick:  func(m *Model, i int) { m.paneSel = i },
		primary: func(m Model) *action {
			if p, ok := m.selectedPane(); ok && p.id != "" {
				return m.jump(p.id)
			}
			return nil
		},
		body: Model.panesPanel,
	},
	tabStoker: {
		name:  "stoker",
		keys:  Model.stokerKeys,
		count: func(m Model) int { return len(m.stokerRows()) },
		index: func(m Model) int { return min(m.stokerSel, max(len(m.stokerRows())-1, 0)) },
		pick:  func(m *Model, i int) { m.stokerSel = i },
		body:  Model.stokerBody,
	},
}

// tabNames names the tabs, in the order of their digits.
var tabNames = func() []string {
	var names []string
	for _, v := range views {
		names = append(names, v.name)
	}
	return names
}()

func (m Model) view() tabView { return views[m.tab] }

// rowIndex is the selected row of the open tab.
func (m Model) rowIndex() int {
	if v := m.view(); v.index != nil {
		return v.index(m)
	}
	return 0
}

func (m Model) rowCount() int {
	if v := m.view(); v.count != nil {
		return v.count(m)
	}
	return 0
}

func (m *Model) selectRow(i int) {
	if v := m.view(); v.pick != nil && i >= 0 && i < m.rowCount() {
		v.pick(m, i)
	}
}

func (m Model) primary() *action {
	if v := m.view(); v.primary != nil {
		return m.forHost(v.primary(m))
	}
	return nil
}

func (m Model) secondary() *action {
	if v := m.view(); v.secondary != nil {
		return m.forHost(v.secondary(m))
	}
	return nil
}

func (m Model) body(w int) body { return m.view().body(m, w) }
