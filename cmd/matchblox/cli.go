package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/blox-eng/matchblox/internal/limits"
	"github.com/blox-eng/matchblox/internal/proto"
	"github.com/blox-eng/matchblox/internal/said"
	"github.com/blox-eng/matchblox/internal/sample"
	"github.com/blox-eng/matchblox/internal/ships"
	"github.com/blox-eng/matchblox/internal/state"
	"github.com/blox-eng/matchblox/internal/stoker"
	"github.com/blox-eng/matchblox/internal/transport"
)

var errNotRunning = errors.New("matchblox is not running: matchblox serve")

// errNotSent: the service dropped the line before the act went, so it did
// not run. errNoResult: the act went and no result came, so it may have
// run.
var (
	errNotSent  = errors.New("the service did not answer")
	errNoResult = errors.New("the service took the act and did not answer")
)

// readDoc is the running service's state, or a state file fresher than
// maxAge. Neither: matchblox is not running.
func readDoc(path string, maxAge time.Duration) (state.Doc, error) {
	if doc, ok := fromService(path, maxAge); ok {
		return doc, nil
	}
	if d, err := state.Read(state.Path()); err == nil && time.Since(d.At) <= maxAge {
		return d, nil
	}
	return state.Doc{}, errNotRunning
}

// cliRead runs one read command against the service's state.
func cliRead(cmd string, w io.Writer, path string, asJSON bool) error {
	doc, err := readDoc(path, staleAfter)
	if err != nil {
		return err
	}
	switch cmd {
	case "sessions":
		return writeSessions(w, doc, asJSON)
	case "ships":
		return writeShips(w, doc, asJSON)
	}
	return writeUsage(w, doc, asJSON)
}

// resolve finds the session a person names by pane id (%3), target
// (app:1.1) or name. A name two sessions share is refused, never guessed.
func resolve(doc state.Doc, who string) (sample.Session, error) {
	var hit []sample.Session
	for _, s := range doc.Sessions {
		if s.Pane == who || s.Target == who {
			return s, nil
		}
		if s.Name == who {
			hit = append(hit, s)
		}
	}
	switch len(hit) {
	case 0:
		return sample.Session{}, fmt.Errorf("no session named %s", who)
	case 1:
		return hit[0], nil
	}
	panes := make([]string, len(hit))
	for i, s := range hit {
		panes[i] = s.Pane
	}
	return sample.Session{}, fmt.Errorf("%s is %d sessions: %s; name the pane", who, len(hit), strings.Join(panes, ", "))
}

type shipJSON struct {
	Repo   string `json:"repo"`
	Number int    `json:"number"`
	Stage  string `json:"stage"`
	Kind   string `json:"kind"`
	Note   string `json:"note"`
}

type sessionJSON struct {
	Name       string    `json:"name"`
	Pane       string    `json:"pane"`
	Target     string    `json:"target"`
	Status     string    `json:"status"`
	Word       string    `json:"word"`
	Reason     string    `json:"reason"`
	Held       bool      `json:"held"`
	Stoker     bool      `json:"stoker"`
	ContextPct float64   `json:"context_pct"`
	PlanPct    *int      `json:"plan_pct"`
	Account    string    `json:"account"`
	Ship       *shipJSON `json:"ship"`
}

var stageNames = [...]string{ships.StageOpen: "open", ships.StageCI: "ci", ships.StageGreen: "green", ships.StageMerged: "merged", ships.StageLive: "live"}

func stageName(n int) string {
	if n < 0 || n >= len(stageNames) {
		return "unknown"
	}
	return stageNames[n]
}

// shipOf is the ship a session drives, found by pane and then session id.
func shipOf(doc state.Doc, s sample.Session) *ships.Ship {
	if doc.Ships == nil {
		return nil
	}
	for i, sh := range doc.Ships.Ships {
		if sh.Session.Pane != "" && sh.Session.Pane == s.Pane || sh.Session.ID != "" && sh.Session.ID == s.SessionID {
			return &doc.Ships.Ships[i]
		}
	}
	return nil
}

func heldBy(doc state.Doc, s sample.Session) bool {
	for _, h := range doc.Holds {
		if h.SessionID != "" && h.SessionID == s.SessionID {
			return true
		}
	}
	return false
}

func sessionRows(doc state.Doc) []sessionJSON {
	rows := []sessionJSON{}
	for _, s := range doc.Sessions {
		r := sessionJSON{Name: s.Name, Pane: s.Pane, Target: s.Target, Status: s.Status, Word: stoker.Word(s),
			Reason: doc.Reasons[s.Pane].Word, Held: heldBy(doc, s), Stoker: doc.StokerServer != "" && s.Server() == doc.StokerServer, ContextPct: s.ContextPct, Account: s.Account}
		if s.Progress != nil {
			r.PlanPct = &s.Progress.Pct
		}
		if sh := shipOf(doc, s); sh != nil {
			r.Ship = &shipJSON{sh.Repo, sh.Number, stageName(sh.Stage), sh.Kind, sh.Note}
		}
		rows = append(rows, r)
	}
	return rows
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

func writeSessions(w io.Writer, doc state.Doc, asJSON bool) error {
	rows := sessionRows(doc)
	if asJSON {
		return writeJSON(w, rows)
	}
	for _, r := range rows {
		target := r.Target
		if r.Pane == "" {
			target = "outside tmux"
		}
		line := fmt.Sprintf("%-4s %-16s %-8s %-6s %3.0f%%", r.Pane, r.Name, target, r.Word, r.ContextPct)
		if r.Held {
			line += "  held"
		}
		if r.Reason != "" {
			line += "  " + r.Reason
		}
		if r.Ship != nil {
			line += fmt.Sprintf("  %s#%d %s", r.Ship.Repo, r.Ship.Number, r.Ship.Stage)
		}
		if _, err := fmt.Fprintln(w, line); err != nil {
			return err
		}
	}
	return nil
}

func writeShips(w io.Writer, doc state.Doc, asJSON bool) error {
	if asJSON {
		return writeJSON(w, doc.Ships)
	}
	if doc.Ships == nil {
		return nil
	}
	for _, sh := range doc.Ships.Ships {
		if _, err := fmt.Fprintf(w, "%-28s %-6s %-13s %s\n", fmt.Sprintf("%s#%d", sh.Repo, sh.Number), stageName(sh.Stage), sh.Kind, sh.Note); err != nil {
			return err
		}
	}
	return nil
}

func writeUsage(w io.Writer, doc state.Doc, asJSON bool) error {
	accts := doc.Limits
	if accts == nil {
		accts = []limits.Account{}
	}
	if asJSON {
		return writeJSON(w, accts)
	}
	for _, a := range accts {
		line := fmt.Sprintf("%-10s %-24s", a.Provider, a.Account)
		for _, u := range a.Uses(doc.At) {
			line += fmt.Sprintf("  %s %.0f%%", u.Name, u.Pct)
		}
		if a.State != "" && len(a.Windows) == 0 {
			line += "  " + a.State
		}
		if _, err := fmt.Fprintln(w, strings.TrimRight(line, " ")); err != nil {
			return err
		}
	}
	return nil
}

// cliPeek runs `matchblox peek <session>`: what the session said last and
// what it asks, as the console's peek shows it. It acts on nothing.
func cliPeek(words []string, lines int, w io.Writer, path string, asJSON bool) error {
	if len(words) != 1 {
		return errors.New("peek needs one session: matchblox peek <who>")
	}
	doc, err := readDoc(path, staleAfter)
	if err != nil {
		return err
	}
	return writePeek(w, doc, words[0], lines, asJSON)
}

// writePeek prints the peek of the session who names, with the last lines
// of what it said (0: all).
func writePeek(w io.Writer, doc state.Doc, who string, lines int, asJSON bool) error {
	s, err := resolve(doc, who)
	if err != nil {
		return err
	}
	p := stoker.PeekOf(s, doc.Queue)
	if all := strings.Split(p.Said, "\n"); lines > 0 && len(all) > lines {
		p.Said = strings.Join(all[len(all)-lines:], "\n")
	}
	if asJSON {
		return writeJSON(w, p)
	}
	var b strings.Builder
	switch {
	case p.Approval:
		fmt.Fprintf(&b, "%s %s asks for an approval: %s\n", p.Name, p.Pane, p.Asks)
	case p.Asks != "":
		fmt.Fprintf(&b, "%s %s asks: %s\n", p.Name, p.Pane, p.Asks)
	default:
		fmt.Fprintf(&b, "%s %s · %s\n", p.Name, p.Pane, stoker.Word(s))
	}
	for i, q := range p.Questions {
		if i > 0 {
			fmt.Fprintf(&b, "then: %s\n", firstOf(q.Question))
		}
		for j, o := range q.Options {
			fmt.Fprintf(&b, "  %d. %s\n", j+1, o)
		}
	}
	if p.Said == "" {
		b.WriteString("said: nothing yet\n")
	} else {
		b.WriteString("said:\n")
		for l := range strings.SplitSeq(p.Said, "\n") {
			b.WriteString(strings.TrimRight("  "+l, " ") + "\n")
		}
	}
	_, err = io.WriteString(w, b.String())
	return err
}

func firstOf(text string) string {
	first, _, _ := strings.Cut(strings.TrimSpace(text), "\n")
	return first
}

var readCommands = []string{"sessions", "ships", "usage", "peek"}

// handCommands act on a session: `send <who> "<text>"`, `send <who> --pick
// <n>`, `compact <who>`, `hold <who> --why "<text>"`, `release <who>`, and
// a session's life (#74): `new`, `rename <who> <name>`, `kill <who>`.
var handCommands = []string{"send", "compact", "hold", "release", "new", "rename", "kill"}

// stokerCommands are the stoker agent's own: `stoker plan` reads its plan
// on stdin, `stoker step <n> --state <s> "<text>"` logs a step to the card,
// and `stoker when` sets a trigger matchblox fires itself.
var stokerCommands = []string{"stoker plan", "stoker step", "stoker when"}

// readPlan reads a plan, one step a line (`<n>. <session>: <text>`).
// Blank lines are skipped, and so are the `asks:` and `grant:` lines an
// older brief asked for: the goes are the builder's chips.
func readPlan(r io.Reader) (stoker.PlanText, error) {
	var p stoker.PlanText
	sc := bufio.NewScanner(r)
	for i := 1; sc.Scan(); i++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "asks:") || strings.HasPrefix(line, "grant:") {
			continue
		}
		num, rest, ok1 := strings.Cut(line, ". ")
		who, text, ok2 := strings.Cut(rest, ": ")
		n, err := strconv.Atoi(num)
		who, text = strings.TrimSpace(who), strings.TrimSpace(text)
		if !ok1 || !ok2 || err != nil || who == "" || text == "" {
			return p, fmt.Errorf("line %d: %q is not `<n>. <session>: <text>`", i, line)
		}
		p.Steps = append(p.Steps, stoker.PlanStep{N: n, Session: who, Text: text})
	}
	if err := sc.Err(); err != nil {
		return p, err
	}
	if len(p.Steps) == 0 {
		return p, errors.New("the plan has no step: one a line, `<n>. <session>: <text>`")
	}
	return p, nil
}

// stokerCmdAct is the act `matchblox stoker plan|step …` asks the service
// for. The service checks it comes from the stoker's pane.
func stokerCmdAct(words []string, state, needs, rule string, stdin io.Reader, from string) (proto.Act, error) {
	a := proto.Act{Confirm: "y", From: from}
	if len(words) == 0 {
		return a, errors.New("stoker needs plan, step, when or quit: matchblox stoker plan, matchblox stoker step <n>, matchblox stoker when, matchblox stoker quit")
	}
	var body any
	switch words[0] {
	case "quit":
		if len(words) > 1 {
			return a, errors.New("quit takes no words: matchblox stoker quit")
		}
		a.RecID = "stoker:quit"
		return a, nil
	case "plan":
		p, err := readPlan(stdin)
		if err != nil {
			return a, err
		}
		a.RecID, body = "stoker:plan", p
	case "step":
		if len(words) < 3 {
			return a, errors.New(`step needs its number and text: matchblox stoker step <n> --state <s> "<text>"`)
		}
		n, err := strconv.Atoi(words[1])
		if err != nil {
			return a, fmt.Errorf("%q is not a step number", words[1])
		}
		if state == "" {
			return a, errors.New("step needs --state doing|done|failed|waits")
		}
		a.RecID, body = "stoker:step", stoker.Entry{Step: n, State: state, Needs: needs, Rule: rule, Text: strings.Join(words[2:], " ")}
	default:
		return a, fmt.Errorf("stoker has plan, step, when and quit, not %s", words[0])
	}
	b, err := json.Marshal(body)
	if err != nil {
		return a, err
	}
	a.Text = string(b)
	return a, nil
}

// cliStoker runs `matchblox stoker plan|step|quit` and says what the
// service did with it.
func cliStoker(words []string, state, needs, rule string, stdin io.Reader, w io.Writer, path string) error {
	a, err := stokerCmdAct(words, state, needs, rule, stdin, os.Getenv("TMUX_PANE"))
	if err != nil {
		return err
	}
	res, err := doAct(path, a)
	if err != nil {
		return err
	}
	if err := reportResult(w, res); err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, stokerDone(words, state, rule))
	return err
}

// whenOpts are the flags of `matchblox stoker when`.
type whenOpts struct {
	ship             int
	stage, rest, why string
	drop             string
	list             bool
}

// whenCmdAct is the act `matchblox stoker when` asks the service for: a
// trigger (--ship <n> --stage <s> or --rest <who>, then the hand: send
// <who> "<text>", compact <who>, release <who>, hold <who> --why "<text>"),
// or --drop <id>. The service checks it comes from the stoker's pane.
func whenCmdAct(words []string, o whenOpts, from string) (proto.Act, error) {
	a := proto.Act{RecID: "stoker:when", Confirm: "y", From: from}
	var body any
	switch {
	case o.drop != "" && (len(words) > 1 || o.ship != 0 || o.rest != ""):
		return a, errors.New("--drop <id> stands alone")
	case o.drop != "":
		body = struct {
			Drop string `json:"drop"`
		}{o.drop}
	case len(words) < 3:
		return a, errors.New(`when needs its hand: send <who> "<text>", compact <who>, release <who> or hold <who> --why "<text>"`)
	default:
		t := stoker.Trigger{Ship: o.ship, Stage: o.stage, Rest: o.rest, Do: words[1], Who: words[2], Text: strings.Join(words[3:], " ")}
		switch t.Do {
		case "hold":
			if len(words) > 3 {
				return a, errors.New(`hold takes its why in --why "<text>"`)
			}
			t.Text = o.why
		case "compact", "release":
			if len(words) > 3 {
				return a, fmt.Errorf("%s takes the session only", t.Do)
			}
		}
		if err := t.Check(); err != nil {
			return a, err
		}
		body = t
	}
	b, err := json.Marshal(body)
	if err != nil {
		return a, err
	}
	a.Text = string(b)
	return a, nil
}

// writeTriggers prints the run's triggers that have not fired, one a line.
func writeTriggers(w io.Writer, doc state.Doc) error {
	var ts []stoker.Trigger
	if doc.Stoker != nil {
		ts = doc.Stoker.Triggers
	}
	if len(ts) == 0 {
		_, err := fmt.Fprintln(w, "no trigger waits")
		return err
	}
	for _, t := range ts {
		if _, err := fmt.Fprintf(w, "%s  when %s → %s\n", t.ID, t.Event(), t.Action()); err != nil {
			return err
		}
	}
	return nil
}

// cliWhen runs `matchblox stoker when`: --list reads the triggers, every
// other form asks the service.
func cliWhen(words []string, o whenOpts, w io.Writer, path string) error {
	if o.list {
		doc, err := readDoc(path, staleAfter)
		if err != nil {
			return err
		}
		return writeTriggers(w, doc)
	}
	a, err := whenCmdAct(words, o, os.Getenv("TMUX_PANE"))
	if err != nil {
		return err
	}
	res, err := doAct(path, a)
	if err != nil {
		return err
	}
	if err := reportResult(w, res); err != nil {
		return err
	}
	line := "the trigger is set: matchblox runs it at its event"
	if o.drop != "" {
		line = o.drop + " is dropped"
	}
	_, err = fmt.Fprintln(w, line)
	return err
}

// stokerDone is the line a stoker command prints when the service took it.
func stokerDone(words []string, state, rule string) string {
	switch words[0] {
	case "plan":
		return "the plan is on the card"
	case "quit":
		return "the stoker agent and its tmux session are ended"
	}
	if len(words) < 2 {
		return state
	}
	line := fmt.Sprintf("step %s logged: %s", words[1], state)
	if rule != "" {
		line += fmt.Sprintf(" (rule %q)", rule)
	}
	return line
}

// positionals takes the words a hand command was given, with its flags
// before or after them (`stoker step 3 --state done`). Only a word that
// names one of its flags is a flag: "-1 is wrong" is a word, and every
// word after -- is one.
func positionals(fl *flag.FlagSet, args []string) ([]string, error) {
	var words []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			return append(words, args[i+1:]...), nil
		}
		f := flagOf(fl, a)
		if f == nil {
			words = append(words, a)
			continue
		}
		n := 2 // the flag and its value
		if b, ok := f.Value.(interface{ IsBoolFlag() bool }); strings.Contains(a, "=") || ok && b.IsBoolFlag() {
			n = 1
		}
		if i+n > len(args) {
			return nil, fmt.Errorf("flag needs an argument: %s", a)
		}
		if err := fl.Parse(args[i : i+n]); err != nil {
			return nil, err
		}
		i += n - 1
	}
	return words, nil
}

// flagOf is the flag a word names (-why, --why, --why=x); nil: a word.
func flagOf(fl *flag.FlagSet, a string) *flag.Flag {
	name, ok := strings.CutPrefix(a, "-")
	if !ok {
		return nil
	}
	name = strings.TrimPrefix(name, "-")
	name, _, _ = strings.Cut(name, "=")
	if name == "" {
		return nil
	}
	return fl.Lookup(name)
}

// handAct is the act a hand command asks the service for. The service
// holds every guard; the command only names the session and the words.
func handAct(doc state.Doc, cmd string, words []string, pick int, why, from string) (proto.Act, error) {
	if len(words) == 0 {
		return proto.Act{}, fmt.Errorf("%s needs a session: matchblox %s <who>", cmd, cmd)
	}
	sess, err := resolve(doc, words[0])
	if err != nil {
		return proto.Act{}, err
	}
	text := strings.Join(words[1:], " ")
	a := proto.Act{Confirm: "y", From: from}
	switch cmd {
	case "send":
		switch {
		case pick > 0 && text != "":
			return a, errors.New("send takes words or --pick, not both")
		case pick > 0:
			return pickAct(a, sess, pick)
		case text == "":
			return a, errors.New(`send needs words: matchblox send <who> "<text>"`)
		}
		a.RecID, a.Text = "message:"+sess.Pane, text
	case "compact":
		a.RecID = "compact:" + sess.Pane
	case "hold":
		if why == "" {
			return a, errors.New(`hold needs its reason: matchblox hold <who> --why "<text>"`)
		}
		a.RecID, a.Text = "hold:"+sess.Pane, why
	case "release":
		a.RecID = "release:" + sess.Pane
	}
	return a, nil
}

// pickAct answers the one question a session asks with its n-th option.
func pickAct(a proto.Act, sess sample.Session, n int) (proto.Act, error) {
	ask := sess.Ask
	switch {
	case ask == nil:
		return a, fmt.Errorf("%s asks no question", sess.Name)
	case len(ask.Questions) != 1:
		return a, fmt.Errorf("%s asks %d questions: answer them in its pane", sess.Name, len(ask.Questions))
	case n > len(ask.Questions[0].Options):
		return a, fmt.Errorf("%s offers %d options, not %d", sess.Name, len(ask.Questions[0].Options), n)
	}
	body, err := json.Marshal(said.Answer{ID: ask.ID, Choices: []said.Choice{{Picks: []int{n}}}})
	if err != nil {
		return a, err
	}
	a.RecID, a.Text = "pick:"+sess.Pane, string(body)
	return a, nil
}

// cliHand runs one hand command: it reads the sessions, asks the service
// for the act and reports what the service says.
func cliHand(cmd string, words []string, pick int, why string, w io.Writer, path string) error {
	doc, err := readDoc(path, staleAfter)
	if err != nil {
		return err
	}
	a, err := handAct(doc, cmd, words, pick, why, os.Getenv("TMUX_PANE"))
	if err != nil {
		return err
	}
	res, err := doAct(path, a)
	return handOutcome(w, cmd, words[0], res, err)
}

// handOutcome says what became of a hand's act, naming the command:
// typed (and the keys), typed without its Enter, refused with why, not
// typed, or not known, when the service took the act and never answered.
func handOutcome(w io.Writer, cmd, who string, res proto.Result, err error) error {
	name := cmd + " " + who
	done := "it ran"
	if cmd == "send" {
		done = "it was typed"
	}
	switch {
	case errors.Is(err, errNoResult) && cmd == "send":
		return fmt.Errorf("%s: not known whether %s: peek %s before you send again", name, done, who)
	case errors.Is(err, errNoResult):
		return fmt.Errorf("%s: not known whether %s: peek %s before you try again", name, done, who)
	case err != nil && cmd == "send":
		return fmt.Errorf("%s: not typed: %w", name, err)
	case err != nil:
		return fmt.Errorf("%s: did not run: %w", name, err)
	}
	var out bytes.Buffer
	if err := reportResult(&out, res); err != nil {
		if cmd == "send" && len(res.Ran) > 0 {
			// The text went, and a guard or tmux stopped the Enter: a send
			// again would type it twice.
			return fmt.Errorf("%s: typed, not sent (Enter not pressed): %w", name, err)
		}
		return fmt.Errorf("%s: refused: %w", name, err)
	}
	if cmd == "send" && len(res.Ran) > 0 {
		if _, err := fmt.Fprintf(w, "%s: typed\n", name); err != nil {
			return err
		}
	}
	_, err = w.Write(out.Bytes())
	return err
}

// reportResult is a nil error when the act ran or was already done, and the
// service's own words when it was refused or a guard stopped it.
func reportResult(w io.Writer, res proto.Result) error {
	switch {
	case res.Err != "":
		return errors.New(res.Err)
	case len(res.Skipped) == 1 && res.Skipped[0] == "already done":
		_, err := fmt.Fprintln(w, "already done")
		return err
	case len(res.Skipped) > 0:
		return errors.New(strings.Join(res.Skipped, "; "))
	}
	for _, argv := range res.Ran {
		if _, err := fmt.Fprintln(w, strings.Join(argv, " ")); err != nil {
			return err
		}
	}
	return nil
}

const actWait = 30 * time.Second

// doAct sends one act to the running service and waits for its result.
func doAct(path string, a proto.Act) (proto.Result, error) {
	c, err := transport.Dial(path, time.Second)
	if err != nil {
		return proto.Result{}, errNotRunning
	}
	defer c.Close()
	timer := time.AfterFunc(actWait, func() { c.Close() })
	defer timer.Stop()
	if err := c.Send(proto.KindHello, "", proto.Hello{Version: proto.Version, Binary: version, Client: proto.ClientCLI}); err != nil {
		return proto.Result{}, errNotRunning
	}
	const id = "cli"
	sent := false
	for {
		env, err := c.Recv()
		if err != nil {
			if sent {
				return proto.Result{}, errNoResult
			}
			return proto.Result{}, errNotSent
		}
		switch env.Kind {
		case proto.KindHello:
			var h proto.Hello
			if json.Unmarshal(env.Body, &h) != nil || h.Version != proto.Version {
				return proto.Result{}, errors.New("the service speaks another version: restart it")
			}
			if !sent {
				sent = true
				if err := c.Send(proto.KindAct, id, a); err != nil {
					return proto.Result{}, errNoResult // part of it may have gone
				}
			}
		case proto.KindResult:
			var res proto.Result
			if env.ID == id && json.Unmarshal(env.Body, &res) == nil {
				return res, nil
			}
		}
	}
}
