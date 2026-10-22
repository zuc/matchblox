package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/blox-eng/matchblox/internal/proto"
	"github.com/blox-eng/matchblox/internal/sample"
	"github.com/blox-eng/matchblox/internal/service"
	"github.com/blox-eng/matchblox/internal/state"
)

// lifeAct is the act of matchblox kill <who> (--anyway: it works or waits,
// and you kill it all the same) or matchblox rename <who> <name>. The
// service holds every guard, and the go the stoker needs for a kill.
func lifeAct(doc state.Doc, cmd string, words []string, anyway bool, from string) (proto.Act, error) {
	if len(words) == 0 {
		return proto.Act{}, fmt.Errorf("%s needs a session: matchblox %s <who>", cmd, cmd)
	}
	a := proto.Act{Confirm: "y", From: from}
	switch cmd {
	case "kill":
		if len(words) > 1 {
			return a, errors.New("kill takes one session: matchblox kill <who>")
		}
		a.RecID = "kill:"
		if anyway {
			a.Text = service.KillAnyway
		}
	case "rename":
		switch {
		case len(words) == 1:
			return a, errors.New("rename needs a name: matchblox rename <who> <name>")
		case len(words) > 2:
			return a, errors.New("a name is one word: letters, digits, - and _")
		}
		if err := proto.CheckName(words[1]); err != nil {
			return a, err
		}
		a.RecID, a.Text = "rename:", words[1]
	}
	sess, err := resolve(doc, words[0])
	if err != nil {
		return a, err
	}
	a.RecID += sess.Pane
	return a, nil
}

// switchOpts are matchblox switch's flags: agent is "" and handoff nil
// when the flag was not given.
type switchOpts struct {
	agent   string
	handoff *string
	anyway  bool
}

// switchAct is the act of matchblox switch <who> --agent <name>: the
// session's pane runs another agent, started from the handoff line (the
// default one unless --handoff gives another, or none when it is empty).
// The service holds every guard.
func switchAct(doc state.Doc, words []string, o switchOpts, from string) (proto.Act, error) {
	const usage = "matchblox switch <who> --agent <name>"
	switch {
	case len(words) == 0:
		return proto.Act{}, errors.New("switch needs a session: " + usage)
	case o.agent == "":
		return proto.Act{}, errors.New("switch needs an agent: --agent claude, codex, opencode or cursor-agent")
	case len(words) > 1:
		return proto.Act{}, errors.New("switch takes one session: " + usage)
	}
	sess, err := resolve(doc, words[0])
	if err != nil {
		return proto.Act{}, err
	}
	if sess.Pane == "" {
		return proto.Act{}, fmt.Errorf("%s is outside tmux: nothing to switch", words[0])
	}
	sw := proto.SwitchAgent{Agent: o.agent, Handoff: proto.DefaultHandoff, Anyway: o.anyway}
	if o.handoff != nil {
		sw.Handoff = *o.handoff
	}
	b, err := json.Marshal(sw)
	if err != nil {
		return proto.Act{}, err
	}
	return proto.Act{RecID: "switch:" + sess.Pane, Confirm: "y", Text: string(b), From: from}, nil
}

// newOpts are matchblox new's flags.
type newOpts struct{ where, agent, branch, name string }

// newSessionAct is the act of matchblox new: an agent (claude by default)
// in a tmux session of its own, in --where (here by default), with the
// words as its first message.
func newSessionAct(words []string, o newOpts, from, here string) (proto.Act, error) {
	n := proto.NewSession{Where: o.where, Agent: o.agent, Branch: o.branch, Name: o.name, First: strings.Join(words, " ")}
	switch {
	case n.Where == "":
		n.Where = here
	case !filepath.IsAbs(n.Where):
		n.Where = filepath.Join(here, n.Where)
	}
	if n.Agent == "" {
		n.Agent = "claude"
	}
	b, err := json.Marshal(n)
	if err != nil {
		return proto.Act{}, err
	}
	return proto.Act{RecID: service.NewSessionID, Confirm: "y", Text: string(b), From: from}, nil
}

func cliLife(cmd string, words []string, anyway bool, w io.Writer, path string) error {
	doc, err := readDoc(path, staleAfter)
	if err != nil {
		return err
	}
	a, err := lifeAct(doc, cmd, words, anyway, os.Getenv("TMUX_PANE"))
	if err != nil {
		return err
	}
	res, err := doAct(path, a)
	if err == nil && cmd == "rename" && res.Err == "" && len(res.Skipped) == 1 && strings.HasSuffix(res.Skipped[0], "renames when it rests") {
		_, err = fmt.Fprintln(w, res.Skipped[0])
		return err
	}
	return handOutcome(w, cmd, words[0], res, err)
}

func cliNew(words []string, o newOpts, w io.Writer, path string) error {
	here, err := os.Getwd()
	if err != nil {
		return err
	}
	a, err := newSessionAct(words, o, os.Getenv("TMUX_PANE"), here)
	if err != nil {
		return err
	}
	res, err := doAct(path, a)
	if err != nil {
		if errors.Is(err, errNoResult) {
			return errors.New("new: not known whether it started: matchblox sessions shows it within seconds if it did")
		}
		return fmt.Errorf("new: did not run: %w", err)
	}
	if err := reportResult(w, res); err != nil {
		return fmt.Errorf("new: refused: %w", err)
	}
	return nil
}

// cliSwitch switches the session's agent and, when jump is not nil, takes
// the tmux client to its pane: only after the switch ran.
func cliSwitch(words []string, o switchOpts, w io.Writer, path string, jump func(argv []string) error) error {
	doc, err := readDoc(path, staleAfter)
	if err != nil {
		return err
	}
	a, err := switchAct(doc, words, o, os.Getenv("TMUX_PANE"))
	if err != nil {
		return err
	}
	res, err := doAct(path, a)
	if err != nil || res.Err != "" || len(res.Ran) == 0 {
		return handOutcome(w, "switch", words[0], res, err)
	}
	if _, err := fmt.Fprintf(w, "switched %s to %s\n", words[0], o.agent); err != nil {
		return err
	}
	if jump == nil {
		return nil
	}
	server, bare := sample.SplitPane(strings.TrimPrefix(a.RecID, "switch:"))
	if err := jump(slices.Concat(sample.TmuxOn(server), []string{"switch-client", "-t", bare})); err != nil {
		_, err = fmt.Fprintf(w, "switched, but not jumped: %v\n", err)
		return err
	}
	return nil
}
