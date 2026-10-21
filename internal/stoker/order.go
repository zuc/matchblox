package stoker

import (
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// The states of an order: it runs from its first second, and the run's
// end is done.
const (
	OrderRunning = "running"
	OrderDone    = "done"
)

// The states of a logged step.
const (
	StepDoing  = "doing"
	StepDone   = "done"
	StepFailed = "failed"
	StepWaits  = "waits" // for a person: Needs names the go it waits for
)

// StepStates are the states a step may log, in the words of the CLI.
var StepStates = []string{StepDoing, StepDone, StepFailed, StepWaits}

// Order is what the builder told the stoker at launch, and the plan the
// stoker agent keeps for it.
type Order struct {
	ID    string `json:"id"` // the run id the brief names
	Words string `json:"words,omitempty"`
	// Earlier are the orders the builder gave before Words in this run,
	// oldest first: by day a new order is one more line to the stoker.
	Earlier []Said    `json:"earlier,omitempty"`
	Night   bool      `json:"night,omitempty"`
	At      time.Time `json:"at,omitzero"`
	// Went is when the order's run went: its steps count from here.
	Went  time.Time `json:"went,omitzero"`
	State string    `json:"state"`
	// Plan is the plan the stoker agent wrote last: each one replaces the
	// one before it.
	Plan []PlanStep `json:"plan,omitempty"`
	// Grants are the goes the order gives: "merge", "release", "deploy"
	// or one merge, "merge:#<n>".
	Grants []string `json:"grants,omitempty"`
	// Unguarded are the plan's sessions no merge guard watches (Codex,
	// OpenCode), each with the goes the order does not give.
	Unguarded []Unguard `json:"unguarded,omitempty"`
}

// Said is an order the builder gave, and when.
type Said struct {
	At    time.Time `json:"at"`
	Words string    `json:"words,omitempty"`
}

// NewOrder is the line a new order by day types into the stoker's pane.
func NewOrder(words string) string {
	if words = strings.Join(strings.Fields(words), " "); words == "" {
		return "New order: none. Follow the standing rules in your ways file."
	}
	return "New order: " + words
}

// PlanStep is one line of the plan: the session the agent will message
// and what it will say.
type PlanStep struct {
	N       int    `json:"n"`
	Session string `json:"session"`
	Text    string `json:"text"`
}

// Unguard is a session of the plan that runs an agent with no merge guard,
// and the goes the order does not give it: nothing but the agent stops
// its merge, release or deploy there.
type Unguard struct {
	Session string   `json:"session"`
	Goes    []string `json:"goes"`
}

// Goes are the kinds of go the guard asks for, and the chips of the
// launch sheet.
var Goes = []string{"merge", "release", "deploy", "kill"}

// PlanText is what `matchblox stoker plan` sends. Goes it names (an older
// CLI's "grants") are not read: the goes are the builder's.
type PlanText struct {
	Steps []PlanStep `json:"steps"`
}

var oneMerge = regexp.MustCompile(`^merge:(#[0-9]+|@\S+)$`)

// CheckGrant refuses anything but the five kinds of go: merge, release,
// deploy, one merge (merge:#<n>) and a session's own merges
// (merge:@<session>).
func CheckGrant(g string) error {
	switch {
	case g == "merge", g == "release", g == "deploy", oneMerge.MatchString(g):
		return nil
	}
	return fmt.Errorf("%q is no grant: merge, release, deploy, merge:#<n> or merge:@<session>", g)
}

// Unguarded are the plan's sessions that run an agent with no merge guard
// (agentOf names it: codex, opencode, cursor-agent), each with the goes the grants do not
// give in full: one merge (merge:#n) still leaves every other merge open.
func Unguarded(plan []PlanStep, grants []string, agentOf func(session string) string) []Unguard {
	var missing []string
	for _, g := range Goes {
		// matchblox itself asks for a kill, whatever the agent: no hook is needed.
		if g != "kill" && !slices.Contains(grants, g) {
			missing = append(missing, g)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	var out []Unguard
	for _, st := range plan {
		a := agentOf(st.Session)
		if (a == "codex" || a == "opencode" || a == "cursor-agent") && !slices.ContainsFunc(out, func(u Unguard) bool { return u.Session == st.Session }) {
			out = append(out, Unguard{Session: st.Session, Goes: missing})
		}
	}
	return out
}

// Logged tells if the stoker agent logged this step with `matchblox
// stoker step`: no compact and no message.
func (e Entry) Logged() bool { return e.State != "" }

// LastGo is the last go step (RuleGo) of run for the same pane and go as
// e: whether it still waits or was given. None: the zero Entry.
func LastGo(run []Entry, e Entry) Entry {
	var last Entry
	for _, x := range run {
		if x.Rule == RuleGo && x.Pane == e.Pane && x.Needs == e.Needs {
			last = x
		}
	}
	return last
}

// NeedOf is the go a y gives for exactly the guarded command g in the
// pane of the session who: one merge by its number, the session's own
// merge when it names none, or the kind of go.
func NeedOf(g Guard, who string) string {
	switch {
	case g.What == "merge" && g.N > 0:
		return "merge:#" + strconv.Itoa(g.N)
	case g.What == "merge" && g.Bare && who != "":
		return "merge:@" + who
	}
	return g.What
}
