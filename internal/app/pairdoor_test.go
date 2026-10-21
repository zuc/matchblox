package app

import (
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/blox-eng/matchblox/internal/doors"
)

var pairDoorRow = doors.Door{ID: doors.Pair, Title: "Pair a device", Why: "answer your agents from your phone"}

// pairDoorConsole is the host's own console with the pair door open and
// selected.
func pairDoorConsole(t *testing.T) (*pairRig, Model) {
	t.Helper()
	r, m := newPairRig(t)
	next, _ := m.Update(stateMsg(doorState(pairDoorRow)))
	m = next.(Model)
	if d, ok := m.selectedDoor(); !ok || d.ID != doors.Pair {
		t.Fatalf("the pair door is not selected:\n%s", screen(m))
	}
	return r, m
}

func TestEnterOnThePairDoorOpensThePage(t *testing.T) {
	_, m := pairDoorConsole(t)
	next, cmd := key(m, "enter")
	got := next.(Model)
	if got.pending != nil || got.pair == nil || got.held != nil || cmd == nil {
		t.Fatalf("pending %v, page %v, held %v", got.pending, got.pair, got.held != nil)
	}
	got = offered(t, got, cmd)
	if got.pair.link == "" {
		t.Fatal("the page made no code")
	}
}

func TestTapOnThePairDoorOpensThePage(t *testing.T) {
	_, m := pairDoorConsole(t)
	y := lineOf(t, m, "Pair a device")
	next, _ := tap(m, 5, y)
	if got := next.(Model); got.pair != nil || got.armed == "" {
		t.Fatalf("the first tap opened the page (armed %q)", got.armed)
	}
	next, cmd := tap(next, 5, y)
	got := next.(Model)
	if got.pending != nil || got.pair == nil || cmd == nil {
		t.Fatalf("pending %v, page %v", got.pending, got.pair)
	}
}

func TestPairDoorClosesLayersUnderThePage(t *testing.T) {
	_, m := pairDoorConsole(t)
	m.peek, m.sheet, m.form = &peek{}, &sheet{}, &form{}
	m.held = func(m Model) (tea.Model, tea.Cmd) { return m, nil }
	m.pending = &action{label: "x", steps: [][]string{{"x"}}}
	got, _ := m.openPairDoor()
	if got.peek != nil || got.sheet != nil || got.form != nil || got.held != nil || got.pending != nil || got.pair == nil {
		t.Fatalf("a layer stayed: %+v", got)
	}
}

func TestPairDoorHiddenInRemoteMode(t *testing.T) {
	_, m := pairDoorConsole(t)
	for name, mod := range map[string]func(*Model){
		"remote host": func(m *Model) { m.opt.Host = "ws-1" },
		"no pair":     func(m *Model) { m.opt.Pair = nil },
	} {
		c := m
		mod(&c)
		next, _ := c.Update(stateMsg(doorState(pairDoorRow, hooksDoor)))
		got := next.(Model)
		if len(got.doors) != 1 || got.doors[0].ID != doors.Hooks {
			t.Errorf("%s: doors %+v", name, got.doors)
		}
	}
	next, _ := m.Update(stateMsg(doorState(pairDoorRow, hooksDoor)))
	if got := next.(Model); len(got.doors) != 2 {
		t.Errorf("host console: doors %+v", got.doors)
	}
}

func TestPairClosePairWaitsForEveryOffer(t *testing.T) {
	r, m := newPairRig(t)
	var mu sync.Mutex
	var gates []chan struct{}
	slow := *m.opt.Pair
	offer := slow.Offer
	slow.Offer = func(until time.Time) (string, string, error) {
		mu.Lock()
		g := make(chan struct{})
		gates = append(gates, g)
		mu.Unlock()
		<-g
		return offer(until)
	}
	m.opt.Pair = &slow
	run := func(cmd tea.Cmd) { go func() { _, _ = findOffer(cmd) }() }
	m, c1 := m.openPairPage()
	run(c1)
	m = m.closePairPage("") // esc, then the door again
	m, c2 := m.openPairPage()
	run(c2)
	m = m.closePairPage("")
	for { // both offers wait on their gates
		mu.Lock()
		n := len(gates)
		mu.Unlock()
		if n == 2 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	closed := make(chan struct{})
	go func() { m.ClosePair(); close(closed) }()
	close(gates[1]) // the latest offer is done: the first still runs
	select {
	case <-closed:
		t.Fatal("ClosePair did not wait for the first offer")
	case <-time.After(30 * time.Millisecond):
	}
	close(gates[0])
	select {
	case <-closed:
	case <-time.After(5 * time.Second):
		t.Fatal("ClosePair never returned")
	}
	if r.pairLines(t) != 0 {
		t.Fatal("an offer left its key")
	}
}

func TestPairOfferErrorWrapsAndEnterRetries(t *testing.T) {
	r, m := newPairRig(t)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 40, Height: 40})
	m = next.(Model)
	fails := true
	good := m.opt.Pair.Offer
	host := *m.opt.Pair
	msgErr := "sshd has no ed25519 host key (/etc/ssh/ssh_host_ed25519_key.pub is missing); run sudo ssh-keygen -A, then press Enter"
	host.Offer = func(until time.Time) (string, string, error) {
		if fails {
			return "", "", errors.New(msgErr)
		}
		return good(until)
	}
	m.opt.Pair = &host
	m, cmd := m.openPairPage()
	m = offered(t, m, cmd)
	if !m.pair.failed {
		t.Fatalf("no failure on the page: %+v", m.pair)
	}
	out := ansi.Strip(m.render())
	for _, l := range strings.Split(out, "\n") {
		if w := ansi.StringWidth(l); w > 40 {
			t.Fatalf("a line is %d wide:\n%s", w, out)
		}
	}
	if flat := strings.Join(strings.Fields(out), " "); !strings.Contains(flat, msgErr) {
		t.Fatalf("the error is cut:\n%s", out)
	}
	fails = false
	nx, cmd := m.Update(pairKeyMsg("enter"))
	m = offered(t, nx.(Model), cmd)
	if m.pair == nil || m.pair.failed || m.pair.link == "" || r.pairLines(t) != 1 {
		t.Fatalf("Enter did not retry: %+v", m.pair)
	}
}
