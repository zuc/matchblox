package push

import (
	"math"
	"slices"
	"time"

	"github.com/blox-eng/matchblox/internal/queue"
	"github.com/blox-eng/matchblox/internal/sample"
	"github.com/blox-eng/matchblox/internal/state"
)

// MaxAccounts is how many accounts a content state carries, which keeps it
// under 2 KB.
const MaxAccounts = 4

// Build is the content state of one state.Doc: the counts, the session the
// island leads with, the usage of each account, and the stoker. It holds
// numbers, states, times and percents only.
func Build(doc state.Doc, hostFP string, n *Numbers, now time.Time) ContentState {
	cs := ContentState{Host: Host8(hostFP), Accounts: []AccountUse{}}
	if doc.Stoker != nil {
		cs.Stoker = doc.Stoker.On
	}

	// What each session does, by id. A queue item is the truth for a wait or
	// a finish; a session in neither is out (a limit stops it) or burns.
	type cand struct {
		s     sample.Session
		state string
		since time.Time
	}
	items := map[string]queue.Item{}
	for _, it := range doc.Queue {
		if it.State != queue.StatePermission && it.State != queue.StateQuestion && it.State != queue.StateFinished {
			continue
		}
		if old, ok := items[it.SessionID]; !ok || waits(it) && !waits(old) || waits(it) == waits(old) && it.Since.Before(old.Since) {
			items[it.SessionID] = it
		}
		if waits(it) {
			cs.Wait++
			if it.Since.After(cs.WaitAt) {
				cs.WaitAt = it.Since.UTC()
			}
		}
	}
	var cands []cand
	for _, s := range doc.Sessions {
		if s.SessionID == "" {
			continue
		}
		c := cand{s: s}
		switch it, ok := items[s.SessionID]; {
		case ok && waits(it):
			c.state, c.since = "waits", it.Since
		case ok:
			c.state, c.since = "done", it.Since
		case !s.Out.IsZero():
			c.state = "out"
		case s.Busy:
			c.state = "burns"
		default:
			continue
		}
		if c.state == "burns" {
			cs.Burn++
		}
		cands = append(cands, c)
	}
	// A wait before a done before an out before a burn; of the waits and
	// dones the oldest, of the others the one prompted last.
	rank := map[string]int{"waits": 0, "done": 1, "out": 2, "burns": 3}
	slices.SortStableFunc(cands, func(a, b cand) int {
		if a.state != b.state {
			return rank[a.state] - rank[b.state]
		}
		if a.state == "waits" || a.state == "done" {
			return a.since.Compare(b.since)
		}
		return b.s.Started.Compare(a.s.Started)
	})
	if len(cands) > 0 {
		c := cands[0]
		top := &Top{N: n.Session(c.s.SessionID), State: c.state, Out: c.s.Out.UTC()}
		if c.s.Progress != nil {
			top.Pct = step5(c.s.Progress.Pct)
		}
		cs.Top = top
	}

	for _, a := range doc.Limits {
		au := AccountUse{N: n.Account(a.Provider, a.Account), Lasts: true}
		for _, u := range a.Uses(now) {
			if u.Name != "5h" && u.Name != "wk" && u.Name != "mo" {
				continue // a window the island has no name for
			}
			au.Windows = append(au.Windows, WindowUse{Name: u.Name, Pct: min(100, max(0, int(math.Round(u.Pct)))), Resets: u.Resets.UTC()})
		}
		if len(au.Windows) == 0 {
			continue
		}
		if f := a.Forecast; f != nil && !f.Lasts {
			au.Lasts, au.RunsOut = false, f.Out.UTC()
		}
		cs.Accounts = append(cs.Accounts, au)
	}
	slices.SortFunc(cs.Accounts, func(a, b AccountUse) int { return a.N - b.N })
	if len(cs.Accounts) > MaxAccounts {
		cs.Accounts = cs.Accounts[:MaxAccounts]
	}
	return cs
}

func waits(it queue.Item) bool {
	return it.State == queue.StatePermission || it.State == queue.StateQuestion
}

// step5 rounds a percent down to a multiple of 5, within 0..100.
func step5(p int) int { return min(100, max(0, p)) / 5 * 5 }
