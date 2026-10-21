package app

import (
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
)

// The pair page is a layer over the console, as the peek is: the box with
// the one-time code as its label (design/0009-the-box-opens.md §4.2).

const (
	pairWindow    = 5 * time.Minute
	pairPollEvery = 100 * time.Millisecond
	pairInWait    = 60 * time.Second        // beat 3 comes within this, or the page folds
	pairActFold   = 1500 * time.Millisecond // the page folds this long after beat 4
)

const (
	pairScanLine    = "Scan with the matchblox app, or paste the link."
	pairKeyLine     = "c copies the link · esc stops"
	pairStoppedLine = "Stopped. The code no longer works."
	pairSmallLine   = "Make this terminal 61 × 33 or larger to show the code, or press c to copy the link."
)

// PairDevice is a device that claimed a pair code, as setup.Device.
type PairDevice struct{ Kind, Name, Fingerprint, ID string }

// PairHost is what the pair page reads and writes on this machine. The
// console does not touch the machine itself: main fills these from setup.
type PairHost struct {
	// Offer writes a one-time key that works until until, and makes its link.
	Offer func(until time.Time) (id, link string, err error)
	// Seen is whether a device reached gate pair, and the name it gave.
	Seen func(id string) (seen bool, name string)
	// Present is whether the key line is still there.
	Present func(id string) (bool, error)
	// Remove takes out the key line and the seen note; removed is false
	// when the line was gone already.
	Remove func(id string) (removed bool, err error)
	// Claim is the device that used the key. It gives a device once.
	Claim func(id string) (PairDevice, bool)
	// Devices are the keys that open this host.
	Devices func() ([]PairDevice, error)
	// RemoveDevice takes out one key, by its name or fingerprint, and gives
	// the backup it made of the key file.
	RemoveDevice func(nameOrFingerprint string) (backup string, err error)
	// DeviceTTYs maps each terminal a device is in on to that device's id.
	DeviceTTYs func() map[string]string
	// DeviceIn is when and where a device's own console started.
	DeviceIn func(id string) (at time.Time, tty string, ok bool)
}

type pairPage struct {
	id, link   string
	until      time.Time
	phase      boxPhase
	phaseAt    time.Time
	seenName   string
	device     PairDevice // after the claim
	standalone bool       // matchblox pair: stops after the opening
	beat       int        // 1 shut, 2 opened, 3 in, 4 first act
	claimedAt  time.Time  // when the device took the code
	foldAt     time.Time  // beat 4: when the page folds
	line       string     // the line under the box
	failed     bool       // line is an offer error: it shows wrapped, and Enter retries
	offer      *pairOffer // the offer that writes the key
}

type pairPollMsg struct{}

// pairFrameMsg draws the next frame of the knock or the opening.
type pairFrameMsg struct{}

func pairPoll() tea.Cmd {
	return tea.Tick(pairPollEvery, func(time.Time) tea.Msg { return pairPollMsg{} })
}

func pairFrame() tea.Cmd {
	return tea.Tick(splashTick, func(time.Time) tea.Msg { return pairFrameMsg{} })
}

// osc52 asks the terminal to put text on the clipboard. It works over SSH,
// and it does not move the cursor.
func osc52(text string) string {
	return "\x1b]52;c;" + base64.StdEncoding.EncodeToString([]byte(text)) + "\x07"
}

func (m Model) clip(text string) {
	if m.opt.Clip != nil {
		m.opt.Clip(text)
		return
	}
	_, _ = os.Stdout.WriteString(osc52(text))
}

// pairOffer is an Offer that runs off the Update goroutine: it can take
// seconds (tailscale status). A page that closes while it runs cancels it,
// and whichever side sees the key last removes it.
type pairOffer struct {
	mu        sync.Mutex
	cancelled bool
	id        string // the key it wrote, once written
}

// pairWaits counts the offers that still run, those of closed pages too:
// the end of the program waits for all of them.
type pairWaits struct{ wg sync.WaitGroup }

// cancel stops the offer; the key it wrote already goes now.
func (o *pairOffer) cancel(h *PairHost) {
	o.mu.Lock()
	o.cancelled = true
	id := o.id
	o.mu.Unlock()
	if id != "" {
		_, _ = h.Remove(id)
	}
}

type pairOfferMsg struct {
	offer    *pairOffer
	id, link string
	err      error
}

// pairBlank stands in for the link while the offer runs: the label is a
// blank code of about the size of the real one.
var pairBlank = "matchblox://pair?p=" + strings.Repeat("A", 110)

// openPairPage starts the offer of a one-time key. The box shows shut, with
// a blank label, until the key is written.
func (m Model) openPairPage() (Model, tea.Cmd) {
	now := m.now()
	p := &pairPage{phase: boxShut, phaseAt: now, beat: 1}
	m.pair, m.flash = p, ""
	h := m.opt.Pair
	if h == nil {
		p.line = "Pair from the host's own terminal."
		return m, nil
	}
	p.until = now.Add(pairWindow)
	o := &pairOffer{}
	p.offer = o
	if m.pairOffers == nil {
		m.pairOffers = &pairWaits{}
	}
	waits := m.pairOffers
	waits.wg.Add(1)
	until := p.until
	return m, func() tea.Msg {
		defer waits.wg.Done()
		id, link, err := h.Offer(until)
		if err == nil {
			o.mu.Lock()
			o.id = id
			gone := o.cancelled
			o.mu.Unlock()
			if gone {
				_, _ = h.Remove(id)
			}
		}
		return pairOfferMsg{o, id, link, err}
	}
}

// offered fills in the label once the key is written, and starts the poll.
func (m Model) offered(msg pairOfferMsg) (Model, tea.Cmd) {
	if m.pair == nil || m.pair.offer != msg.offer {
		return m, nil // the page closed: the offer removed its key
	}
	p := *m.pair
	m.pair = &p
	if msg.err != nil {
		p.line, p.failed = msg.err.Error(), true
		return m, nil
	}
	p.id, p.link, p.line = msg.id, msg.link, pairScanLine
	if m.pairPolling {
		return m, nil // the poll on its way goes on
	}
	m.pairPolling = true
	return m, pairPoll()
}

// waiting is a page whose offer still runs.
func (p *pairPage) waiting() bool { return p.offer != nil && p.link == "" && p.line == "" }

// closePairPage removes the key line and the .seen note; why goes to the
// flash. A device that claimed the code since the last poll is named.
func (m Model) closePairPage(why string) Model {
	p := m.pair
	m.pair = nil
	if p == nil {
		return m
	}
	m.flash = why
	if p.offer != nil && m.opt.Pair != nil {
		p.offer.cancel(m.opt.Pair)
	}
	if p.id == "" || m.opt.Pair == nil {
		return m
	}
	removed, err := m.opt.Pair.Remove(p.id)
	if err == nil && !removed && p.live() {
		if d, ok := m.opt.Pair.Claim(p.id); ok {
			p.device = d // the page that ended names it, for matchblox pair
			m.flash = pairedLine(d)
		}
	}
	return m
}

// ClosePair removes the one-time key of an open pair page. main calls it
// when the program ends, however it ends. An offer that still runs is
// waited for, so that it removes its key before the process exits.
func (m Model) ClosePair() {
	_ = m.closePairPage("")
	if w := m.pairOffers; w != nil {
		done := make(chan struct{})
		go func() { w.wg.Wait(); close(done) }()
		select {
		case <-done:
		case <-time.After(10 * time.Second):
		}
	}
}

// quit ends the program; an open pair page takes its code with it.
func (m Model) quit() (tea.Model, tea.Cmd) {
	m = m.closePairPage("")
	m.quitting = true
	return m, tea.Quit
}

// live is a code that still waits for a device.
func (p *pairPage) live() bool {
	return p.link != "" && (p.phase == boxShut || p.phase == boxKnock)
}

func (p *pairPage) to(ph boxPhase, at time.Time) {
	p.phase, p.phaseAt = ph, at
}

func pairedLine(d PairDevice) string {
	return fmt.Sprintf("Paired %s (%s). Not yours? x on it in DEVICES.", d.Name, d.Fingerprint)
}

func (p *pairPage) expiredLine() string {
	if p.standalone {
		return "The code expired. Run matchblox pair again."
	}
	return "The code expired. Enter pairs again."
}

// pairMoving tells if the knock or the opening still plays.
func (m Model) pairMoving() bool {
	if m.pair == nil || m.opt.NoMotion {
		return false
	}
	t := m.now().Sub(m.pair.phaseAt)
	switch m.pair.phase {
	case boxKnock:
		return t < knockFor
	case boxOpen:
		return t < openFor+boxOutFor+boxStrikeFor+standFor
	}
	return false
}

// pairPolled reads the notes: the expiry first, then a device at the door,
// then the claim.
func (m Model) pairPolled() (Model, tea.Cmd) {
	if m.pair == nil {
		m.pairPolling = false
		return m, nil
	}
	if m.pair.beat >= 2 {
		return m.pairWatched()
	}
	if !m.pair.live() {
		return m, pairPoll()
	}
	p := *m.pair
	h, now, was := m.opt.Pair, m.now(), p.phase
	claim := func() bool {
		d, ok := h.Claim(p.id)
		if ok {
			p.device, p.beat, p.line, p.claimedAt = d, 2, pairedLine(d), now
			p.to(boxOpen, now)
		}
		return ok
	}
	if !now.Before(p.until) {
		removed, err := h.Remove(p.id)
		if err != nil || removed || !claim() { // else the device took it at the last moment
			p.line = p.expiredLine()
			p.to(boxExpired, now)
		}
	} else {
		if seen, name := h.Seen(p.id); seen {
			if p.phase == boxShut {
				p.to(boxKnock, now)
			}
			p.seenName, p.line = name, "A device is here…"
			if name != "" {
				p.line = name + " is here…"
			}
		}
		if present, err := h.Present(p.id); err == nil && !present {
			if !claim() { // the line went, but no device used it
				p.line = p.expiredLine()
				p.to(boxExpired, now)
			}
			_, _ = h.Remove(p.id) // the seen note
		}
	}
	m.pair = &p
	if p.phase != was && m.pairMoving() {
		return m, tea.Batch(pairPoll(), pairFrame())
	}
	return m, pairPoll()
}

// owns is a page that takes the screen and the keys. From beat 3 the device
// works in the console, so the page is a strip on the key line instead.
func (p *pairPage) owns() bool { return p != nil && p.beat < 3 }

// strips is a page at beat 3 or 4: one line on the key line.
func (p *pairPage) strips() bool { return p != nil && p.beat >= 3 }

// pairStripEsc is esc that dismisses the strip: the console has nothing
// else of its own for it to cancel.
func (m Model) pairStripEsc(k string) bool {
	return k == "esc" && m.pair.strips() && m.peek == nil && m.form == nil && m.sheet == nil &&
		m.pending == nil && m.held == nil && !m.searching && m.filter == ""
}

// pairWatched is the poll after the claim: beat 3 is the device's own
// console, and the page folds without it after a minute, and after beat 4.
func (m Model) pairWatched() (Model, tea.Cmd) {
	p := *m.pair
	h, now := m.opt.Pair, m.now()
	switch {
	case p.standalone || h == nil || h.DeviceIn == nil:
	case p.beat == 4:
		if !now.Before(p.foldAt) {
			return m.closePairPage(""), nil
		}
	case p.beat == 2:
		if at, _, ok := h.DeviceIn(p.device.ID); ok && at.After(p.claimedAt) {
			p.beat, p.line = 3, p.device.Name+" is in. Answer an agent from it, or esc to go back."
		} else if now.Sub(p.claimedAt) >= pairInWait {
			return m.closePairPage(pairedLine(p.device)), nil
		}
	}
	m.pair = &p
	return m, pairPoll()
}

// pairActed tells the page of an act the console sent. At beat 3, an act
// from the new device's own terminal is its first one: the page names it
// and folds soon.
func (m Model) pairActed(a action) Model {
	p, h := m.pair, m.opt.Pair
	if p == nil || p.standalone || p.beat != 3 || h == nil || h.DeviceIn == nil {
		return m
	}
	_, tty, found := h.DeviceIn(p.device.ID)
	if !found || tty == "" || m.lastClient() != tty {
		return m
	}
	verb, pane := "acted in", a.rec
	if i := strings.IndexByte(a.rec, ':'); i >= 0 {
		pane = a.rec[i+1:]
		switch a.rec[:i] {
		case "answer":
			verb = "answered"
		case "pick":
			verb = "picked in"
		case "message":
			verb = "wrote to"
		case "compact":
			verb = "compacted"
		}
	}
	s, ok := m.sessionIn(pane)
	if !ok {
		return m // the rec names no session (door:, stoker:): not an act in one
	}
	who := s.Name
	next := *p
	next.beat, next.line = 4, fmt.Sprintf("%s %s %s", p.device.Name, verb, who)
	next.foldAt = m.now().Add(pairActFold)
	m.pair = &next
	return m
}

// pairKey is a key while the page shows: c copies, esc closes, Enter
// makes a new code after the old one expired, ctrl+c quits. Every other key
// does nothing.
func (m Model) pairKey(k string) (tea.Model, tea.Cmd) {
	p := m.pair
	m.flash = ""
	switch k {
	case "c":
		if p.live() {
			m.clip(p.link)
			m.flash = "link copied"
		}
	case "esc":
		why := ""
		if p.live() {
			why = pairStoppedLine
		}
		return m.closePairPage(why), nil
	case "enter":
		if p.standalone { // matchblox pair makes a new code on the next run
			return m, nil
		}
		if p.phase == boxExpired || p.failed {
			if m.fromDevice() {
				m.flash = pairFromDeviceLine
				return m, nil
			}
			return m.closePairPage("").openPairPage()
		}
	case "ctrl+c":
		return m.quit()
	}
	return m, nil
}

// pairView takes the whole screen: the box, or the code alone, and the two
// lines under it, in the middle.
func (m Model) pairView(w int) string {
	p := m.pair
	now := m.now()
	centre := func(s string) string {
		s = ansi.Truncate(s, w, "…")
		return strings.Repeat(" ", max((w-lipgloss.Width(s))/2, 0)) + s
	}
	line := m.st.muted.Render(p.line)
	if p.live() {
		left := max(p.until.Sub(now), 0).Round(time.Second)
		line += "   " + m.st.muted.Render(fmt.Sprintf("%d:%02d", int(left.Minutes()), int(left.Seconds())%60))
	}
	var keys []string
	if p.live() {
		keys = append(keys, m.st.muted.Render(pairKeyLine))
	}
	if m.flash != "" {
		keys = append(keys, m.st.text.Render(m.flash))
	}
	text := []string{"", centre(line), centre(strings.Join(keys, "   "))}
	if p.failed { // an error says how to fix it: wrapped, never cut
		text = []string{""}
		for _, l := range strings.Split(ansi.Wordwrap(p.line, max(w-2, 1), ""), "\n") {
			text = append(text, centre(m.st.muted.Render(l)))
		}
		text = append(text, centre(strings.Join(keys, "   ")))
	}

	var lines []string
	if p.link != "" || p.waiting() {
		s := boxState{phase: p.phase, since: now.Sub(p.phaseAt), link: p.link, name: p.device.Name}
		if p.waiting() {
			s.phase, s.link = boxExpired, pairBlank // shut, blank paper
		}
		box, fits := pairBox(s, m.dark, m.opt.NoMotion, w, m.height)
		if fits == fitNone {
			for _, l := range strings.Split(ansi.Wordwrap(pairSmallLine, max(w-2, 1), ""), "\n") {
				box = append(box, centre(m.st.muted.Render(l)))
			}
		}
		lines = box
	}
	lines = append(lines, text...)
	top := max((m.height-len(lines))/2, 0)
	return strings.Repeat("\n", top) + strings.Join(lines, "\n")
}
