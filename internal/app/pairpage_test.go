package app

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/blox-eng/matchblox/internal/replay"
	"github.com/blox-eng/matchblox/internal/setup"
)

const (
	pairHostKey  = "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIM4bfwfUcmyCX2tnMZ4mHkeEjSRJnkf0ehLEuB8iWLHU root@ws-1\n"
	pairPhoneKey = "ecdsa-sha2-nistp256 AAAAE2VjZHNhLXNoYTItbmlzdHAyNTYAAAAIbmlzdHAyNTYAAABBBFNcqPRX8M69Sf5wSAjjnCSQ7eAlW+J0FHl38LZZ/uI1dOc+R0B75EtirPaf7OBcEUynTj95ks6kamKOuSCb+mA="
	pairExe      = "/usr/local/bin/matchblox"
)

// pairRig is a console with a home of its own, a host key and a clock the
// test moves.
type pairRig struct {
	home   string
	now    time.Time
	copied []string
}

func newPairRig(t *testing.T) (*pairRig, Model) {
	t.Helper()
	r := &pairRig{home: t.TempDir(), now: time.Date(2026, 10, 21, 14, 32, 0, 0, time.UTC)}
	if err := os.MkdirAll(filepath.Join(r.home, ".ssh"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.home, ".ssh", "authorized_keys"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	hostKey := filepath.Join(t.TempDir(), "host.pub")
	if err := os.WriteFile(hostKey, []byte(pairHostKey), 0o644); err != nil {
		t.Fatal(err)
	}
	m, _ := loadedWith(t, 100, fixtureState())
	m.height = 40
	m.opt.Pair = testPairHost(r.home, pairExe, hostKey)
	m.opt.Now = func() time.Time { return r.now }
	m.opt.Clip = func(text string) { r.copied = append(r.copied, text) }
	return r, m
}

// testPairHost is main's consolePair: the console itself never imports setup.
func testPairHost(home, exe, hostKey string) *PairHost {
	return &PairHost{
		Offer: func(until time.Time) (string, string, error) {
			if exe == "" {
				return "", "", errors.New("cannot find this matchblox on the PATH")
			}
			return setup.NewPairOffer(home, exe, "ws-1.tail1234.ts.net", 22, "ws-1", hostKey, until)
		},
		Seen:    func(id string) (bool, string) { return setup.PairSeen(home, id) },
		Present: func(id string) (bool, error) { return setup.PairKeyPresent(home, id) },
		Remove: func(id string) (bool, error) {
			setup.RemovePairSeen(home, id)
			return setup.RemovePairKey(home, id)
		},
		Claim: func(id string) (PairDevice, bool) {
			d, ok := setup.TakeClaim(home, id)
			return PairDevice(d), ok
		},
		Devices: func() ([]PairDevice, error) {
			ds, err := setup.Devices(home)
			out := make([]PairDevice, len(ds))
			for i, d := range ds {
				out[i] = PairDevice(d)
			}
			return out, err
		},
		RemoveDevice: func(name string) (string, error) { return setup.RemoveDevice(home, name) },
		DeviceTTYs:   func() map[string]string { return setup.DeviceTTYs(home) },
		DeviceIn:     func(id string) (time.Time, string, bool) { return setup.DeviceIn(home, id) },
	}
}

func (r *pairRig) pairLines(t *testing.T) int {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(r.home, ".ssh", "authorized_keys"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.Count(string(b), " gate pair ")
}

func (r *pairRig) seenNotes(t *testing.T) []string {
	t.Helper()
	n, _ := filepath.Glob(filepath.Join(r.home, ".ssh", ".matchblox-pair-*.seen"))
	return n
}

func openedPage(t *testing.T) (*pairRig, Model) {
	t.Helper()
	r, m := newPairRig(t)
	m, cmd := m.openPairPage()
	m = offered(t, m, cmd)
	if m.pair == nil || m.pair.id == "" || m.pair.link == "" {
		t.Fatalf("no code: %+v", m.pair)
	}
	return r, m
}

func pairKeyMsg(k string) tea.KeyPressMsg {
	if k == "ctrl+c" {
		return tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl}
	}
	return keyPress(k)
}

// findOffer runs cmd and finds the answer of the offer in it.
func findOffer(cmd tea.Cmd) (pairOfferMsg, bool) {
	if cmd == nil {
		return pairOfferMsg{}, false
	}
	switch msg := cmd().(type) {
	case pairOfferMsg:
		return msg, true
	case tea.BatchMsg:
		for _, c := range msg {
			if o, ok := findOffer(c); ok {
				return o, true
			}
		}
	}
	return pairOfferMsg{}, false
}

// offered runs the offer that cmd started and gives its answer to m.
func offered(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	msg, ok := findOffer(cmd)
	if !ok {
		t.Fatal("no offer ran")
	}
	next, _ := m.Update(msg)
	return next.(Model)
}

func TestPairPageWaitsForTheOfferOffUpdate(t *testing.T) {
	r, m := newPairRig(t)
	release := make(chan struct{})
	slow := *m.opt.Pair
	offer := slow.Offer
	slow.Offer = func(until time.Time) (string, string, error) {
		<-release
		return offer(until)
	}
	m.opt.Pair = &slow
	m, cmd := m.openPairPage() // returns at once: the offer has not run
	if !m.pair.waiting() || r.pairLines(t) != 0 {
		t.Fatal("the page did not wait for the offer")
	}
	for _, dark := range []bool{true, false} {
		v := m
		v.dark, v.st = dark, newStyles(dark)
		if _, err := replay.Parse(v.render(), Tokens(dark)); err != nil {
			t.Fatal(err)
		}
	}
	text := pageText(m)
	if !strings.Contains(text, "▚▞") || strings.Contains(text, "5:00") || strings.Contains(text, pairKeyLine) {
		t.Fatalf("not the shut box with no countdown:\n%s", text)
	}
	close(release)
	m = offered(t, m, cmd)
	if m.pair.link == "" || r.pairLines(t) != 1 || !strings.Contains(pageText(m), "5:00") {
		t.Fatalf("the offer did not fill the label: %+v", m.pair)
	}
}

// A page that closes while its offer runs leaves no key, whichever side
// sees the key last.
func TestPairPageCancelledOfferLeavesNoKey(t *testing.T) {
	for _, k := range []string{"esc", "ctrl+c"} {
		// The offer returns after the close.
		r, m := newPairRig(t)
		m, cmd := m.openPairPage()
		next, _ := m.Update(pairKeyMsg(k))
		m = next.(Model)
		msg, _ := findOffer(cmd)
		next, _ = m.Update(msg)
		if next.(Model).pair != nil || r.pairLines(t) != 0 {
			t.Fatalf("%s while the offer ran: %d pair lines", k, r.pairLines(t))
		}

		// The offer returned before the close; its answer comes after.
		r, m = newPairRig(t)
		m, cmd = m.openPairPage()
		msg, _ = findOffer(cmd)
		if r.pairLines(t) != 1 {
			t.Fatal("the offer wrote no key")
		}
		next, _ = m.Update(pairKeyMsg(k))
		next, _ = next.Update(msg)
		if next.(Model).pair != nil || r.pairLines(t) != 0 {
			t.Fatalf("%s before the answer: %d pair lines", k, r.pairLines(t))
		}
	}
}

func TestPairPageClosePairWaitsForTheOffer(t *testing.T) {
	r, m := newPairRig(t)
	release := make(chan struct{})
	slow := *m.opt.Pair
	offer := slow.Offer
	slow.Offer = func(until time.Time) (string, string, error) {
		<-release
		return offer(until)
	}
	m.opt.Pair = &slow
	m, cmd := m.openPairPage()
	go func() { _, _ = findOffer(cmd) }() // the program's goroutine
	next, _ := m.Update(pairKeyMsg("ctrl+c"))
	closed := make(chan struct{})
	go func() { next.(Model).ClosePair(); close(closed) }() // main, after Run
	time.Sleep(20 * time.Millisecond)
	close(release)
	<-closed
	if r.pairLines(t) != 0 {
		t.Fatal("the program ended before the offer removed its key")
	}
}

// The end of the program waits for the offer of an earlier console too:
// the page opened, the builder went back to Hosts and opened a console again.
func TestShellCloseWaitsForTheOfferOfAnEarlierConsole(t *testing.T) {
	r, m := newPairRig(t)
	release := make(chan struct{})
	slow := *m.opt.Pair
	offer := slow.Offer
	slow.Offer = func(until time.Time) (string, string, error) {
		<-release
		return offer(until)
	}
	s := NewShell(ShellOptions{
		Base: Options{NoMotion: true, Pair: &slow},
		Open: func(string) Options { return Options{Conn: newFake()} },
	})
	if s.layer != layerConsole || s.cur.opt.Pair == nil {
		t.Fatal("the shell did not open this machine's console")
	}
	var cmd tea.Cmd
	s.cur, cmd = s.cur.openPairPage()
	go func() { _, _ = findOffer(cmd) }() // the program's goroutine
	s = s.back()
	s, _ = s.open("", false)
	closed := make(chan struct{})
	go func() { s.Close(); close(closed) }() // main, after Run
	select {
	case <-closed:
		t.Fatal("Close did not wait for the offer of the earlier console")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	<-closed
	if r.pairLines(t) != 0 {
		t.Fatal("the program ended before the offer removed its key")
	}
}

func poll(t *testing.T, m Model) Model {
	t.Helper()
	next, _ := m.Update(pairPollMsg{})
	return next.(Model)
}

func pageText(m Model) string { return ansi.Strip(m.render()) }

func TestPairPageWritesAndRemovesTheKey(t *testing.T) {
	r, m := openedPage(t)
	if n := r.pairLines(t); n != 1 {
		t.Fatalf("%d pair lines after the open, want 1", n)
	}
	if err := setup.MarkPairSeen(r.home, m.pair.id, ""); err != nil {
		t.Fatal(err)
	}
	next, _ := key(m, "esc")
	m = next.(Model)
	if m.pair != nil {
		t.Fatal("esc left the page open")
	}
	if n := r.pairLines(t); n != 0 {
		t.Fatalf("%d pair lines after esc", n)
	}
	if n := r.seenNotes(t); len(n) != 0 {
		t.Fatalf("the seen note stayed: %v", n)
	}
	if m.flash != "Stopped. The code no longer works." {
		t.Fatalf("flash = %q", m.flash)
	}
}

func TestPairPageRemovesTheKeyOnQuit(t *testing.T) {
	r, m := openedPage(t)
	next, cmd := m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if cmd == nil || cmd() != tea.Quit() {
		t.Fatal("ctrl+c did not quit")
	}
	if next.(Model).pair != nil || r.pairLines(t) != 0 {
		t.Fatal("ctrl+c left the pair line")
	}
}

func TestPairPageRemovesTheKeyWhenTheProgramEnds(t *testing.T) {
	r, m := openedPage(t)
	// The service drops: the page stays, as it needs only this machine.
	next, _ := m.Update(lostMsg{err: errors.New("EOF"), conn: m.conn})
	m = next.(Model)
	if m.pair == nil || r.pairLines(t) != 1 {
		t.Fatal("a dropped service closed the page")
	}
	// The program ends some other way: main closes the page of the final model.
	m.ClosePair()
	if r.pairLines(t) != 0 {
		t.Fatal("the end of the program left the pair line")
	}

	r, m = openedPage(t)
	next, _ = m.Update(lostMsg{err: errors.New("EOF"), conn: m.conn})
	next, _ = next.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if !next.(Model).quitting || r.pairLines(t) != 0 {
		t.Fatal("ctrl+c after a lost connection left the pair line")
	}
}

func TestPairPageShellCloseRemovesTheKey(t *testing.T) {
	r, m := openedPage(t)
	s := Shell{layer: layerConsole, cur: m}
	s.Close()
	if r.pairLines(t) != 0 {
		t.Fatal("Shell.Close left the pair line")
	}
}

func TestPairPageKnocksOnSeen(t *testing.T) {
	r, m := openedPage(t)
	m = poll(t, m)
	if m.pair.phase != boxShut {
		t.Fatalf("phase = %v before the device came", m.pair.phase)
	}
	if err := setup.MarkPairSeen(r.home, m.pair.id, ""); err != nil {
		t.Fatal(err)
	}
	m = poll(t, m)
	if m.pair.phase != boxKnock || m.pair.line != "A device is here…" {
		t.Fatalf("phase %v, line %q", m.pair.phase, m.pair.line)
	}
	if !strings.Contains(pageText(m), "A device is here…") {
		t.Fatal("the page does not show the line")
	}
	if err := setup.MarkPairSeen(r.home, m.pair.id, "iPad"); err != nil {
		t.Fatal(err)
	}
	m = poll(t, m)
	if m.pair.line != "iPad is here…" {
		t.Fatalf("line = %q", m.pair.line)
	}
}

func TestPairPageOpensOnClaim(t *testing.T) {
	r, m := openedPage(t)
	d, err := setup.ClaimPairKey(r.home, pairExe, m.pair.id, pairPhoneKey, "iPad")
	if err != nil {
		t.Fatal(err)
	}
	m = poll(t, m)
	if m.pair.phase != boxOpen || m.pair.device.Name != "iPad" {
		t.Fatalf("phase %v, device %+v", m.pair.phase, m.pair.device)
	}
	want := "Paired iPad (" + d.Fingerprint + "). Not yours? x on it in DEVICES."
	if !strings.HasPrefix(d.Fingerprint, "SHA256:") || m.pair.line != want {
		t.Fatalf("line = %q, want %q", m.pair.line, want)
	}
	if !strings.Contains(pageText(m), "iPad") {
		t.Fatal("the label does not name the device")
	}
}

func TestPairPageExpires(t *testing.T) {
	r, m := openedPage(t)
	r.now = r.now.Add(5*time.Minute - time.Second)
	m = poll(t, m)
	if !strings.Contains(pageText(m), "0:01") {
		t.Fatalf("the countdown is not 0:01:\n%s", pageText(m))
	}
	r.now = r.now.Add(time.Second)
	m = poll(t, m)
	if m.pair.phase != boxExpired || m.pair.line != "The code expired. Enter pairs again." {
		t.Fatalf("phase %v, line %q", m.pair.phase, m.pair.line)
	}
	if r.pairLines(t) != 0 {
		t.Fatal("the expired code is still in authorized_keys")
	}
	next, _ := key(m, "x")
	if next.(Model).pair == nil {
		t.Fatal("a key other than esc and Enter closed the page")
	}
	old := m.pair.id
	next, cmd := key(m, "enter")
	m = offered(t, next.(Model), cmd)
	if m.pair == nil || m.pair.phase != boxShut || m.pair.id == old || r.pairLines(t) != 1 {
		t.Fatalf("Enter made no new code: %+v", m.pair)
	}
}

func TestPairPageCopiesTheLink(t *testing.T) {
	r, m := openedPage(t)
	next, _ := key(m, "c")
	m = next.(Model)
	if len(r.copied) != 1 || r.copied[0] != m.pair.link {
		t.Fatalf("copied %q, want the link", r.copied)
	}
	if m.flash != "link copied" || !strings.Contains(pageText(m), "link copied") {
		t.Fatalf("flash = %q", m.flash)
	}

	// Without a Clip, the program writes OSC 52 with its own output, between
	// frames: Update writes nothing.
	m.opt.Clip = nil
	next, cmd := key(m, "c")
	if cmd == nil || cmd() != tea.SetClipboard(m.pair.link)() || next.(Model).flash != "link copied" {
		t.Fatal("c does not ask the program to set the clipboard")
	}
}

func TestPairPageSurvivesResize(t *testing.T) {
	r, m := openedPage(t)
	if err := setup.MarkPairSeen(r.home, m.pair.id, ""); err != nil {
		t.Fatal(err)
	}
	m = poll(t, m)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 60, Height: 30})
	m = next.(Model)
	// The line wraps in 60 columns.
	if !strings.Contains(strings.Join(strings.Fields(pageText(m)), " "), "Make this terminal 61 × 33 or larger to show the code, or press c to copy the link.") {
		t.Fatalf("no make-larger line:\n%s", pageText(m))
	}
	next, _ = key(m, "c")
	m = next.(Model)
	if len(r.copied) != 1 || !strings.Contains(pageText(m), "link copied") {
		t.Fatal("c does not copy in a small terminal")
	}
	next, _ = m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = next.(Model)
	if m.pair.phase != boxKnock || !strings.Contains(pageText(m), "▚▞") {
		t.Fatalf("the box did not come back in the same phase:\n%s", pageText(m))
	}
}

func TestPairPageViewIsTokens(t *testing.T) {
	r, m := openedPage(t)
	views := func(m Model) []Model {
		var out []Model
		for _, dark := range []bool{true, false} {
			for _, size := range [][2]int{{100, 40}, {80, 36}, {60, 30}} {
				v := m
				v.dark, v.st = dark, newStyles(dark)
				v.width, v.height = size[0], size[1]
				out = append(out, v)
			}
		}
		return out
	}
	check := func(m Model) {
		t.Helper()
		for _, v := range views(m) {
			if _, err := replay.Parse(v.render(), Tokens(v.dark)); err != nil {
				t.Fatalf("phase %v, dark %v, %dx%d: %v", v.pair.phase, v.dark, v.width, v.height, err)
			}
		}
	}
	next, _ := key(m, "c")
	m = next.(Model)
	check(m)
	_ = setup.MarkPairSeen(r.home, m.pair.id, "iPad")
	m = poll(t, m)
	check(m)
	if _, err := setup.ClaimPairKey(r.home, pairExe, m.pair.id, pairPhoneKey, "iPad"); err != nil {
		t.Fatal(err)
	}
	m.opt.NoMotion = false
	for _, at := range []time.Duration{0, 300 * time.Millisecond, 700 * time.Millisecond, 2 * time.Second} {
		r.now = r.now.Add(at)
		m = poll(t, m)
		check(m)
	}

	_, m = openedPage(t)
	r2 := m.opt.Now().Add(6 * time.Minute)
	m.opt.Now = func() time.Time { return r2 }
	m = poll(t, m)
	if m.pair.phase != boxExpired {
		t.Fatal("not expired")
	}
	check(m)
}

func TestPairPageOnlyForThisMachine(t *testing.T) {
	for host, want := range map[string]bool{"": true, "ws-2": false} {
		sh := NewShell(ShellOptions{
			Base:  Options{NoMotion: true, Pair: &PairHost{}},
			Open:  func(string) Options { return Options{Conn: newFake()} },
			Start: host,
		})
		if got := sh.cur.opt.Pair != nil; got != want {
			t.Errorf("host %q: the console can pair = %v, want %v", host, got, want)
		}
	}
}

func TestPairPageWithoutTheBinaryWritesNoKey(t *testing.T) {
	r, m := newPairRig(t)
	m.opt.Pair = testPairHost(r.home, "", filepath.Join(t.TempDir(), "host.pub"))
	m, cmd := m.openPairPage()
	m = offered(t, m, cmd)
	if m.pair == nil || m.pair.line != "cannot find this matchblox on the PATH" || r.pairLines(t) != 0 {
		t.Fatalf("page %+v, %d lines", m.pair, r.pairLines(t))
	}
	if !strings.Contains(pageText(m), "cannot find this matchblox on the PATH") {
		t.Fatal("the page does not say why")
	}
}

// The console pairs on port 22; matchblox pair --port is the way for
// another. The line shows only when the page has a row for it.
func TestTheConsolePageNamesItsPort(t *testing.T) {
	_, m := openedPage(t)
	if !strings.Contains(pageText(m), pairPortLine) {
		t.Fatalf("no port line:\n%s", pageText(m))
	}
	p := *m.pair
	p.standalone = true
	alone := m
	alone.pair = &p
	if strings.Contains(pageText(alone), pairPortLine) {
		t.Fatal("matchblox pair, which has --port, shows the port line")
	}
	small := m
	small.height = len(strings.Split(strings.TrimLeft(pageText(m), "\n"), "\n")) - 1 // all rows but the port line
	if text := pageText(small); strings.Contains(text, pairPortLine) || !strings.Contains(text, pairKeyLine) {
		t.Fatalf("the port line took a row the page needs:\n%s", text)
	}
}

// Every failed offer says the two ways on: Enter and esc.
func TestAFailedOfferSaysTheKeys(t *testing.T) {
	r, m := newPairRig(t)
	m.opt.Pair = testPairHost(r.home, "", filepath.Join(t.TempDir(), "host.pub"))
	m, cmd := m.openPairPage()
	m = offered(t, m, cmd)
	if !strings.Contains(pageText(m), "Enter tries again · esc goes back") {
		t.Fatalf("no key line:\n%s", pageText(m))
	}
	m.opt.Pair = testPairHost(r.home, pairExe, filepath.Join(t.TempDir(), "host.pub"))
	next, cmd := key(m, "enter")
	if m = offered(t, next.(Model), cmd); m.pair == nil || !m.pair.failed {
		t.Fatalf("Enter did not try again: %+v", m.pair)
	}
	if !strings.Contains(pageText(m), "Enter tries again · esc goes back") {
		t.Fatalf("no key line after the retry:\n%s", pageText(m))
	}
	if next, _ = key(m, "esc"); next.(Model).pair != nil {
		t.Fatal("esc did not go back")
	}
}

// claimedPage is a page whose code a device took: beat 2, with the device's
// own console not in yet. The last client is the builder's terminal.
func claimedPage(t *testing.T) (*pairRig, Model, *string) {
	t.Helper()
	r, m := openedPage(t)
	if _, err := setup.ClaimPairKey(r.home, pairExe, m.pair.id, pairPhoneKey, "iPad"); err != nil {
		t.Fatal(err)
	}
	m = poll(t, m)
	if m.pair.beat != 2 || m.pair.device.ID == "" {
		t.Fatalf("beat %d, device %+v", m.pair.beat, m.pair.device)
	}
	for i := range m.all.sessions {
		if m.all.sessions[i].Pane == "%2" {
			m.all.sessions[i].Name = "api"
		}
	}
	tty := "/dev/ttys001"
	m.opt.LastClient = func() (string, time.Time, error) { return tty, time.Time{}, nil }
	return r, m, &tty
}

const phoneTTY = "/dev/ttys009"

func deviceIn(t *testing.T, r *pairRig, id string, at time.Time) {
	t.Helper()
	if err := setup.MarkDeviceIn(r.home, id, phoneTTY, at); err != nil {
		t.Fatal(err)
	}
}

func TestBeatThreeOnTheDevicesNote(t *testing.T) {
	r, m, _ := claimedPage(t)
	deviceIn(t, r, "ba9876543210", r.now.Add(time.Second)) // another device
	r.now = r.now.Add(2 * time.Second)
	m = poll(t, m)
	if m.pair.beat != 2 {
		t.Fatalf("a note for another id moved the page to beat %d", m.pair.beat)
	}
	deviceIn(t, r, m.pair.device.ID, r.now.Add(-time.Hour)) // from before the claim
	m = poll(t, m)
	if m.pair.beat != 2 {
		t.Fatalf("an old note moved the page to beat %d", m.pair.beat)
	}
	deviceIn(t, r, m.pair.device.ID, r.now.Add(time.Second))
	m = poll(t, m)
	if want := "iPad is in. Answer an agent from it, or esc to go back."; m.pair.beat != 3 || m.pair.line != want {
		t.Fatalf("beat %d, line %q", m.pair.beat, m.pair.line)
	}
}

func inPage(t *testing.T) (*pairRig, Model, *string) {
	t.Helper()
	r, m, tty := claimedPage(t)
	deviceIn(t, r, m.pair.device.ID, r.now.Add(time.Second))
	m = poll(t, m)
	if m.pair.beat != 3 {
		t.Fatalf("beat %d", m.pair.beat)
	}
	return r, m, tty
}

func TestBeatFourNamesTheAct(t *testing.T) {
	r, m, tty := inPage(t)
	*tty = phoneTTY
	m = m.pairActed(action{rec: "answer:%2"})
	if m.pair.beat != 4 || m.pair.line != "iPad answered api" {
		t.Fatalf("beat %d, line %q", m.pair.beat, m.pair.line)
	}
	m = m.pairActed(action{rec: "pick:%2"})
	if m.pair.line != "iPad answered api" {
		t.Fatalf("a second act changed the line: %q", m.pair.line)
	}
	r.now = r.now.Add(1400 * time.Millisecond)
	if m = poll(t, m); m.pair == nil {
		t.Fatal("the page folded before 1.5 s")
	}
	r.now = r.now.Add(200 * time.Millisecond)
	if m = poll(t, m); m.pair != nil {
		t.Fatal("the page did not fold after 1.5 s")
	}
}

func TestBeatFourVerbs(t *testing.T) {
	for rec, want := range map[string]string{
		"pick:%2": "iPad picked in api", "message:%2": "iPad wrote to api",
		"compact:%2": "iPad compacted api", "kill:%2": "iPad acted in api",
	} {
		_, m, tty := inPage(t)
		*tty = phoneTTY
		if m = m.pairActed(action{rec: rec}); m.pair.line != want {
			t.Errorf("%s: line %q, want %q", rec, m.pair.line, want)
		}
	}
}

func TestBeatFourIgnoresTheBuilder(t *testing.T) {
	_, m, _ := inPage(t)
	m = m.pairActed(action{rec: "answer:%2"})
	if m.pair.beat != 3 || strings.Contains(m.pair.line, "answered") {
		t.Fatalf("the builder's act moved the page: beat %d, line %q", m.pair.beat, m.pair.line)
	}
}

func TestBeatFourNeedsBeatThree(t *testing.T) {
	_, m, tty := claimedPage(t)
	*tty = phoneTTY
	if m = m.pairActed(action{rec: "answer:%2"}); m.pair.beat != 2 {
		t.Fatalf("beat %d", m.pair.beat)
	}
}

func TestFoldsWithoutBeatThree(t *testing.T) {
	r, m, _ := claimedPage(t)
	r.now = r.now.Add(59 * time.Second)
	if m = poll(t, m); m.pair == nil {
		t.Fatal("the page folded before 60 s")
	}
	r.now = r.now.Add(time.Second)
	m = poll(t, m)
	if m.pair != nil || !strings.HasPrefix(m.flash, "Paired iPad (SHA256:") {
		t.Fatalf("page %v, flash %q", m.pair, m.flash)
	}
}

func TestStandaloneNeverWaitsForBeats(t *testing.T) {
	r, m, _ := claimedPage(t)
	p := *m.pair
	p.standalone = true
	m.pair = &p
	r.now = r.now.Add(10 * time.Minute)
	if m = poll(t, m); m.pair == nil || m.pair.beat != 2 {
		t.Fatalf("standalone page: %+v", m.pair)
	}
}

func TestEscFoldsAtBeatThree(t *testing.T) {
	_, m, _ := inPage(t)
	next, _ := key(m, "esc")
	if next.(Model).pair != nil {
		t.Fatal("esc did not fold the page")
	}
}

// TestBeatFourOnAPeekAnswer: the peek sends its answer without confirm. The
// phone answers while the page waits at beat 3.
func TestBeatFourOnAPeekAnswer(t *testing.T) {
	st := queueState()
	for i := range st.Sessions {
		if st.Sessions[i].Pane == "%2" {
			st.Sessions[i].Name = "api"
		}
	}
	m, f := loadedWith(t, 100, st)
	m.opt.LastClient = func() (string, time.Time, error) { return phoneTTY, time.Time{}, nil }
	m.opt.Pair = &PairHost{DeviceIn: func(id string) (time.Time, string, bool) { return time.Now(), phoneTTY, id == "iPad-id" }}
	next, _ := key(m, "down") // app-review finished its turn
	next, _ = key(next, "a")
	next = typeText(next, "ok")
	m = next.(Model)
	m.pair = &pairPage{beat: 3, device: PairDevice{Name: "iPad", ID: "iPad-id"}}
	// The page takes keys, so the peek's Enter is its own call.
	v, p, _ := m.peekNow()
	if p.pane != "%2" {
		t.Fatalf("peek on %q", p.pane)
	}
	next, cmd := m.sendPeek(v.s, "answer:%2", "ok")
	drain(cmd)
	got := next.(Model)
	if got.pair == nil || got.pair.beat != 4 || got.pair.line != "iPad answered api" {
		t.Fatalf("page %+v", got.pair)
	}
	if acts := f.acts(); len(acts) != 1 || acts[0].RecID != "answer:%2" {
		t.Fatalf("acts %+v", acts)
	}

	// An act the builder sent, and an act that names no session, do not count.
	m.opt.LastClient = func() (string, time.Time, error) { return "/dev/ttys001", time.Time{}, nil }
	if got := m.pairActed(action{rec: "answer:%2"}); got.pair.beat != 3 {
		t.Fatal("the builder's act counted")
	}
	m.opt.LastClient = func() (string, time.Time, error) { return phoneTTY, time.Time{}, nil }
	if got := m.pairActed(action{rec: "door:guide"}); got.pair.beat != 3 {
		t.Fatal("a door act counted")
	}
}

// TestTheStripHandsTheConsoleBack: from beat 3 the page is one line on the
// key line. The device's keys work the console, its answer is beat 4, and
// esc dismisses the strip when the console has nothing to cancel.
func TestTheStripHandsTheConsoleBack(t *testing.T) {
	st := queueState()
	for i := range st.Sessions {
		if st.Sessions[i].Pane == "%2" {
			st.Sessions[i].Name = "api"
		}
	}
	m, f := loadedWith(t, 100, st)
	now := time.Date(2026, 10, 21, 14, 32, 0, 0, time.UTC)
	m.opt.Now = func() time.Time { return now }
	m.opt.LastClient = func() (string, time.Time, error) { return phoneTTY, time.Time{}, nil }
	m.opt.Pair = &PairHost{DeviceIn: func(id string) (time.Time, string, bool) { return now, phoneTTY, id == "iPad-id" }}
	m.pair = &pairPage{beat: 3, line: "iPad is in. Answer an agent from it, or esc to go back.", device: PairDevice{Name: "iPad", ID: "iPad-id"}}
	if out := ansi.Strip(m.render()); !strings.Contains(out, "iPad is in. Answer an agent from it, or esc to go back.") || !strings.Contains(out, "SESSIONS") && !strings.Contains(out, "api") {
		t.Fatalf("the console does not show with the strip:\n%s", out)
	}

	next, _ := key(m, "down")
	next, _ = key(next, "a")
	if next.(Model).peek == nil {
		t.Fatal("a key did not reach the console at beat 3")
	}
	next, _ = key(next, "esc") // closes the peek, not the strip
	if got := next.(Model); got.peek != nil || got.pair == nil {
		t.Fatalf("esc: peek %v, page %v", got.peek, got.pair)
	}
	next, _ = key(next, "a")
	next = typeText(next, "ok")
	next, cmd := key(next, "enter")
	drain(cmd)
	got := next.(Model)
	if got.pair == nil || got.pair.beat != 4 || got.pair.line != "iPad answered api" {
		t.Fatalf("page %+v", got.pair)
	}
	if acts := f.acts(); len(acts) != 1 || acts[0].RecID != "answer:%2" {
		t.Fatalf("acts %+v", acts)
	}
	if out := ansi.Strip(got.render()); !strings.Contains(out, "iPad answered api") {
		t.Fatalf("the strip does not say the act:\n%s", out)
	}
	now = now.Add(1600 * time.Millisecond)
	after, _ := got.Update(pairPollMsg{})
	if after.(Model).pair != nil {
		t.Fatal("the strip stayed after 1.5 s")
	}

	// esc with nothing to cancel dismisses the strip at beat 3.
	next, _ = key(m, "esc")
	if next.(Model).pair != nil {
		t.Fatal("esc did not dismiss the strip")
	}
}

// pump runs cmd as the program would and gives its pair messages to m: the
// real ticks, not hand-fed polls. It gives the Cmds that m returned.
func pump(t *testing.T, m Model, cmd tea.Cmd) (Model, tea.Cmd) {
	t.Helper()
	var msgs []tea.Msg
	var run func(tea.Cmd)
	run = func(c tea.Cmd) {
		if c == nil {
			return
		}
		switch msg := c().(type) {
		case tea.BatchMsg:
			for _, c := range msg {
				run(c)
			}
		case pairOfferMsg, pairPollMsg:
			msgs = append(msgs, msg)
		}
	}
	run(cmd)
	var next []tea.Cmd
	for _, msg := range msgs {
		got, c := m.Update(msg)
		m = got.(Model)
		next = append(next, c)
	}
	return m, tea.Batch(next...)
}

// TestPairPageOpensAgainAfterAFold: offer, polls, the fold, the next page.
// The next page polls too: it knocks and expires.
func TestPairPageOpensAgainAfterAFold(t *testing.T) {
	folds := map[string]func(t *testing.T, r *pairRig, m Model, cmd tea.Cmd) (Model, tea.Cmd){
		"beat 4": func(t *testing.T, r *pairRig, m Model, cmd tea.Cmd) (Model, tea.Cmd) {
			deviceIn(t, r, m.pair.device.ID, r.now.Add(time.Second))
			r.now = r.now.Add(2 * time.Second)
			if m, cmd = pump(t, m, cmd); m.pair == nil || m.pair.beat != 3 {
				t.Fatalf("no beat 3: %+v", m.pair)
			}
			m.opt.LastClient = func() (string, time.Time, error) { return phoneTTY, time.Time{}, nil }
			m = m.pairActed(action{rec: "answer:%2"})
			r.now = r.now.Add(pairActFold)
			return pump(t, m, cmd)
		},
		"60 s": func(t *testing.T, r *pairRig, m Model, cmd tea.Cmd) (Model, tea.Cmd) {
			r.now = r.now.Add(pairInWait)
			return pump(t, m, cmd)
		},
	}
	for name, fold := range folds {
		t.Run(name, func(t *testing.T) {
			r, m := newPairRig(t)
			for i := range m.all.sessions {
				if m.all.sessions[i].Pane == "%2" {
					m.all.sessions[i].Name = "api"
				}
			}
			m.opt.LastClient = func() (string, time.Time, error) { return "/dev/ttys001", time.Time{}, nil }
			m, cmd := m.openPairPage()
			m, cmd = pump(t, m, cmd) // the offer
			if m.pair == nil || m.pair.link == "" || cmd == nil {
				t.Fatalf("the first page does not poll: %+v", m.pair)
			}
			if _, err := setup.ClaimPairKey(r.home, pairExe, m.pair.id, pairPhoneKey, "iPad"); err != nil {
				t.Fatal(err)
			}
			if m, cmd = pump(t, m, cmd); m.pair == nil || m.pair.beat != 2 {
				t.Fatalf("no claim: %+v", m.pair)
			}
			if m, cmd = fold(t, r, m, cmd); m.pair != nil {
				t.Fatalf("the page did not fold: %+v", m.pair)
			}
			m, _ = pump(t, m, cmd) // what the fold left runs out

			m.opt.LastClient = func() (string, time.Time, error) { return "/dev/ttys001", time.Time{}, nil }
			m, cmd = m.openPairPage()
			m, cmd = pump(t, m, cmd)
			if m.pair == nil || m.pair.link == "" || cmd == nil {
				t.Fatalf("the second page does not poll: %+v", m.pair)
			}
			if err := setup.MarkPairSeen(r.home, m.pair.id, "iPhone"); err != nil {
				t.Fatal(err)
			}
			if m, cmd = pump(t, m, cmd); m.pair.phase != boxKnock {
				t.Fatalf("the second page did not knock: %v", m.pair.phase)
			}
			r.now = r.now.Add(pairWindow)
			if m, _ = pump(t, m, cmd); m.pair.phase != boxExpired || r.pairLines(t) != 0 {
				t.Fatalf("the second page did not expire: %v, %d lines", m.pair.phase, r.pairLines(t))
			}
		})
	}
}
