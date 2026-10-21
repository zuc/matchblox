// Package limits reads the plan limits of each account an agent runs
// under, says them as /usage does (the percent of each window used), and
// forecasts whether the week lasts.
package limits

import (
	"fmt"
	"math"
	"slices"
	"strings"
	"time"
)

const (
	BoxMinutes  = 5 * 60      // the 5-hour window
	WeekMinutes = 7 * 24 * 60 // the week

	// StaleAfter: a reading older than this, with no reset since, is shown
	// with its time.
	StaleAfter = time.Hour
)

// The states of an account's limits.
const (
	Measured   = "measured"
	Stale      = "stale"
	Unmeasured = "not measured"
)

// The sources an account's limits come from.
const (
	SourceClaude   = "claude-statusline"
	SourceCodex    = "codex"
	SourceOpenCode = "opencode"
	SourceCursor   = "cursor"
)

// Window is one limit as the agent reported it.
type Window struct {
	UsedPct  float64   `json:"used_pct"`
	Minutes  int       `json:"window_minutes"`
	ResetsAt time.Time `json:"resets_at"`
}

// Reading is what one source reported at one time.
type Reading struct {
	At      time.Time `json:"at"`
	Windows []Window  `json:"windows,omitempty"`
}

// Outlook is the forecast of the week.
type Outlook struct {
	Lasts bool      `json:"lasts"`
	Out   time.Time `json:"out,omitempty"` // when the week runs out, to the hour
}

// Account is the limits of one account, as the console shows them.
type Account struct {
	Provider string    `json:"provider"`
	Account  string    `json:"account"`
	Source   string    `json:"source"`
	State    string    `json:"state"`
	Fix      string    `json:"fix,omitempty"`
	At       time.Time `json:"at,omitzero"`
	Windows  []Window  `json:"windows,omitempty"`
	Forecast *Outlook  `json:"forecast,omitempty"`
}

// Use is one window as /usage says it: a short name (5h, wk, mo), the
// percent used and when it resets.
type Use struct {
	Name   string
	Pct    float64
	Resets time.Time
}

// Uses are the windows of an account, the 5 hours first, then the week,
// then any other. A window whose reset passed since the reading is empty
// again, with no reset known.
func (a Account) Uses(now time.Time) []Use {
	var out []Use
	ws := slices.Clone(a.Windows)
	slices.SortStableFunc(ws, func(x, y Window) int { return x.Minutes - y.Minutes })
	for _, w := range ws {
		u := Use{Name: span(w.Minutes), Pct: math.Max(0, w.UsedPct), Resets: w.ResetsAt}
		if !w.ResetsAt.IsZero() && !w.ResetsAt.After(now) {
			u.Pct, u.Resets = 0, time.Time{}
		}
		out = append(out, u)
	}
	return out
}

// Build turns a reading into what the console shows. A nil reading, or one
// without windows, is not measured and says how to make it measured.
func Build(provider, account, source string, r *Reading, now time.Time, q Quiet) Account {
	a := Account{Provider: provider, Account: account, Source: source}
	if r == nil || len(r.Windows) == 0 {
		a.State, a.Fix = Unmeasured, fixFor(source, account, r != nil)
		return a
	}
	a.State, a.At, a.Windows = Measured, r.At, r.Windows
	if now.Sub(r.At) > StaleAfter && !allReset(r.Windows, now) {
		a.State = Stale
	}
	for _, w := range r.Windows {
		if w.Minutes == WeekMinutes {
			a.Forecast = Forecast(w, now, q)
		}
	}
	return a
}

// allReset: every window reset after the reading, so its figures are known.
func allReset(ws []Window, now time.Time) bool {
	for _, w := range ws {
		if w.ResetsAt.IsZero() || w.ResetsAt.After(now) {
			return false
		}
	}
	return true
}

// unix is a resets_at; 0 (none given) is no time, not 1970.
func unix(sec int64) time.Time {
	if sec <= 0 {
		return time.Time{}
	}
	return time.Unix(sec, 0)
}

// Unix is unix for the readers of other agents.
func Unix(sec int64) time.Time { return unix(sec) }

// span is the short name of a window: 5h, wk, mo, or its days or hours.
func span(minutes int) string {
	switch d := time.Duration(minutes) * time.Minute; {
	case minutes == WeekMinutes:
		return "wk"
	case d >= 28*24*time.Hour:
		return "mo"
	case d >= 24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	default:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
}

func fixFor(source, account string, read bool) string {
	switch {
	case account == "API key":
		return "an API key has no plan limits"
	case source == SourceClaude:
		return "open the door “Show your limits”"
	case source == SourceCodex && read:
		return "this Codex login reports no limits"
	case source == SourceCodex:
		return "Codex reports them after its next turn"
	case source == SourceCursor:
		return "set cursor_usage = true under [limits] in config.toml"
	default:
		return "OpenCode keeps no limits"
	}
}

// quietWeight is how much an hour the builder does not usually work counts
// against an hour they do.
const quietWeight = 0.1

// Quiet is when the builder does not usually work: hours of each day and
// whole days.
type Quiet struct {
	From, To int // minutes of the day; From == To: no quiet hours
	Days     [7]bool
}

var weekdays = map[string]time.Weekday{"sun": time.Sunday, "mon": time.Monday, "tue": time.Tuesday,
	"wed": time.Wednesday, "thu": time.Thursday, "fri": time.Friday, "sat": time.Saturday}

// ParseQuiet reads "23:00-08:00" and day names ("Sat", "sun"). Empty hours:
// none.
func ParseQuiet(hours string, days []string) (Quiet, error) {
	var q Quiet
	if hours != "" {
		from, to, ok := strings.Cut(hours, "-")
		var err error
		if !ok {
			return q, fmt.Errorf("quiet hours %q: want HH:MM-HH:MM", hours)
		}
		if q.From, err = clock(from); err != nil {
			return q, err
		}
		if q.To, err = clock(to); err != nil {
			return q, err
		}
	}
	for _, d := range days {
		wd, ok := weekdays[strings.ToLower(strings.TrimSpace(d))]
		if !ok {
			return q, fmt.Errorf("quiet day %q: want Mon, Tue, … Sun", d)
		}
		q.Days[wd] = true
	}
	return q, nil
}

func clock(s string) (int, error) {
	t, err := time.Parse("15:04", strings.TrimSpace(s))
	if err != nil {
		return 0, fmt.Errorf("quiet hours: %q is not HH:MM", s)
	}
	return t.Hour()*60 + t.Minute(), nil
}

// Quiet says whether t falls in a quiet hour or on a quiet day, in t's zone.
func (q Quiet) Quiet(t time.Time) bool {
	if q.Days[t.Weekday()] {
		return true
	}
	m := t.Hour()*60 + t.Minute()
	switch {
	case q.From == q.To:
		return false
	case q.From < q.To:
		return m >= q.From && m < q.To
	default: // over midnight
		return m >= q.From || m < q.To
	}
}

const step = 15 * time.Minute

// weighted counts the hours from a to b, a quiet hour as quietWeight.
func (q Quiet) weighted(a, b time.Time) float64 {
	var h float64
	for t := a; t.Before(b); {
		next := t.Truncate(step).Add(step)
		if next.After(b) {
			next = b
		}
		w := 1.0
		if q.Quiet(t) {
			w = quietWeight
		}
		h += w * next.Sub(t).Hours()
		t = next
	}
	return h
}

// Forecast says whether the week lasts at the rate of its hours so far,
// with quiet hours discounted. Nil in the first hour of a week, or for a
// window without a reset.
func Forecast(w Window, now time.Time, q Quiet) *Outlook {
	if w.ResetsAt.IsZero() || !w.ResetsAt.After(now) {
		return nil
	}
	if w.UsedPct >= 100 {
		return &Outlook{Out: now}
	}
	start := w.ResetsAt.Add(-time.Duration(w.Minutes) * time.Minute)
	if now.Sub(start) < time.Hour {
		return nil
	}
	rate := w.UsedPct / q.weighted(start, now)
	used := w.UsedPct
	for t := now; t.Before(w.ResetsAt); t = t.Add(step) {
		end := t.Add(step)
		if end.After(w.ResetsAt) {
			end = w.ResetsAt
		}
		used += rate * q.weighted(t, end)
		if used >= 100 {
			return &Outlook{Out: end.Round(time.Hour)}
		}
	}
	return &Outlook{Lasts: true}
}

// The tones of a part of the line.
const (
	Muted = "muted"
	Warn  = "warn"
)

// Part is a piece of an account's line and how loud it is.
type Part struct {
	Text string
	Tone string // "", Muted or Warn
}

// NearPct is the use from which a window says when it resets.
const NearPct = 80

// Parts is an account's line: the percent used of each window, the
// forecast, and the age of an old reading, or what to do when there is
// none.
func Parts(a Account, now time.Time) []Part {
	if a.State == Unmeasured {
		return []Part{{Text: "not measured · " + a.Fix, Tone: Warn}}
	}
	var ps []Part
	for _, u := range a.Uses(now) {
		text := fmt.Sprintf("%s %.0f%%", u.Name, u.Pct)
		if u.Pct >= NearPct && !u.Resets.IsZero() {
			text += " until " + When(u.Resets, now, "")
		}
		ps = append(ps, Part{Text: text})
	}
	switch f := a.Forecast; {
	case f == nil:
	case f.Lasts:
		ps = append(ps, Part{Text: "lasts the week", Tone: Muted})
	default:
		ps = append(ps, Part{Text: "out " + When(f.Out, now, "~"), Tone: Warn})
	}
	if a.State == Stale {
		ps = append(ps, Part{Text: "as of " + When(a.At, now, ""), Tone: Warn})
	}
	return ps
}

// When is "16:40" today, else "Mon 09:00", in now's zone.
func When(t, now time.Time, approx string) string {
	t = t.In(now.Location())
	if y, m, d := t.Date(); y == now.Year() && m == now.Month() && d == now.Day() {
		return approx + t.Format("15:04")
	}
	return t.Format("Mon") + " " + approx + t.Format("15:04")
}

// Until is "00:47" within a day of now, else "Mon 09:00": when a reset a
// few hours on comes back, even past midnight.
func Until(t, now time.Time) string {
	t = t.In(now.Location())
	if t.Sub(now) < 24*time.Hour {
		return t.Format("15:04")
	}
	return t.Format("Mon 15:04")
}

// Plain joins the parts as text.
func Plain(ps []Part) string {
	s := make([]string, len(ps))
	for i, p := range ps {
		s[i] = p.Text
	}
	return strings.Join(s, " · ")
}

// Out is when an account whose window is spent can work again: the latest
// reset of a window at 100%, or zero when none is spent.
func (r *Reading) Out(now time.Time) time.Time {
	var out time.Time
	if r == nil {
		return out
	}
	for _, w := range r.Windows {
		if w.UsedPct >= 100 && w.ResetsAt.After(now) && w.ResetsAt.After(out) {
			out = w.ResetsAt
		}
	}
	return out
}
