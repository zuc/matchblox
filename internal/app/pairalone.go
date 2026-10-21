package app

import (
	"errors"
	"io"
	"time"

	tea "charm.land/bubbletea/v2"
)

// matchblox pair with a terminal runs the pair page as a program of its
// own: the same page, with no console under it (design/0009-the-box-opens.md
// §4.7).

// pairAloneWait is how long the page stays after the opening, so the strike
// is seen, before the alternate screen closes.
const pairAloneWait = 1200 * time.Millisecond

// PairOptions is what RunPairPage needs. The page reaches the machine only
// through Host, which main fills from setup.
type PairOptions struct {
	Host *PairHost
	// NoMotion shows the last frame of each step.
	NoMotion bool
	// Now is the clock. Nil: time.Now.
	Now func() time.Time
	// Clip copies text to the clipboard. Nil: OSC 52 on stdout.
	Clip func(text string)
	// Paired is the line that names the device that paired. Nil: the
	// console's own line.
	Paired func(PairDevice) string
	// In and Out are the terminal. Nil: stdin and stdout.
	In  io.Reader
	Out io.Writer

	cols, rows int // the size of a terminal that is not one, for tests
}

// pairAloneDoneMsg ends the program a while after the opening.
type pairAloneDoneMsg struct{}

// pairAlone is the program: the console's Model with only the pair page.
type pairAlone struct {
	m      Model
	opt    PairOptions
	result string
	err    error
	waited bool // the end after the opening is on its way
}

func (a *pairAlone) Init() tea.Cmd {
	var offer tea.Cmd
	a.m, offer = a.m.openPairPage()
	a.m.pair.standalone = true
	return tea.Batch(tea.RequestBackgroundColor, offer)
}

func (a *pairAlone) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(pairAloneDoneMsg); ok {
		a.finish(a.m.pair)
		return a, tea.Quit
	}
	old := a.m.pair
	next, cmd := a.m.Update(msg)
	a.m = next.(Model)
	p := a.m.pair
	switch {
	case a.m.quitting || p == nil: // esc or ctrl+c
		a.finish(old)
		return a, tea.Quit
	case p.failed: // the offer failed: there is no code to show
		a.err = errors.New(p.line)
		return a, tea.Quit
	case p.beat == 2 && !a.waited && !a.m.pairMoving():
		a.waited = true
		cmd = tea.Batch(cmd, tea.Tick(pairAloneWait, func(time.Time) tea.Msg { return pairAloneDoneMsg{} }))
	}
	return a, cmd
}

// finish sets the line main prints after the alternate screen closes. p is
// the page as it was when it ended.
func (a *pairAlone) finish(p *pairPage) {
	switch {
	case p == nil:
	case p.device.ID != "":
		a.result = a.opt.Paired(p.device)
	case p.phase == boxExpired:
		a.result = p.line
	case p.live() || p.waiting():
		a.result = pairStoppedLine
	}
}

func (a *pairAlone) View() tea.View { return a.m.View() }

// RunPairPage shows the pair page until the code is used, expires or is
// stopped. It gives the line to print after the screen closes. The code
// never outlives the call.
func RunPairPage(o PairOptions) (result string, err error) {
	if o.Host == nil {
		return "", errors.New("the pair page needs a host")
	}
	if o.Paired == nil {
		o.Paired = pairedLine
	}
	m := New(Options{Pair: o.Host, NoMotion: o.NoMotion, Now: o.Now, Clip: o.Clip})
	m.splashDone = true // no start screen: the page is all there is
	a := &pairAlone{m: m, opt: o}
	opts := []tea.ProgramOption{}
	if o.In != nil {
		opts = append(opts, tea.WithInput(o.In))
	}
	if o.Out != nil {
		opts = append(opts, tea.WithOutput(o.Out))
	}
	if o.cols > 0 {
		opts = append(opts, tea.WithWindowSize(o.cols, o.rows))
	}
	final, err := tea.NewProgram(a, opts...).Run()
	if f, ok := final.(*pairAlone); ok {
		f.m.ClosePair() // the key goes however the program ended
	}
	if err != nil {
		return "", err
	}
	return a.result, a.err
}
