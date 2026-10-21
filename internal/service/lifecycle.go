package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/blox-eng/matchblox/internal/advice"
	"github.com/blox-eng/matchblox/internal/holds"
	"github.com/blox-eng/matchblox/internal/panes"
	"github.com/blox-eng/matchblox/internal/proto"
	"github.com/blox-eng/matchblox/internal/sample"
	"github.com/blox-eng/matchblox/internal/stoker"
)

// A session's life from matchblox (#74): new starts an agent in a tmux
// session of its own, rename names its tmux session, its window and the
// agent alike, and kill ends the agent and closes its pane. Its worktree
// and its transcript stay.

// NewSessionID is the act that starts a session; its Text is a
// proto.NewSession.
const NewSessionID = "new:session"

// KillAnyway is the Text of a kill the builder confirmed a second time:
// the session works or waits for them.
const KillAnyway = "anyway"

// Startable are the agents matchblox starts, each with its way to take a
// name and a first message.
var Startable = []string{"claude", "codex", "opencode", sample.CursorAgent}

// cursorBins are the commands Cursor's CLI is installed as, the first one
// found the one a new session runs; `agent` only when it is Cursor's.
var cursorBins = []string{sample.CursorAgent, "agent"}

// agentBin is the command that starts agent here ("" when none is
// installed), agent itself but for Cursor's CLI.
func agentBin(agent string, look func(string) (string, error)) string {
	bins := []string{agent}
	if agent == sample.CursorAgent {
		bins = cursorBins
	}
	for _, b := range bins {
		path, err := look(b)
		if err != nil {
			continue
		}
		if b == "agent" && !cursorsAgent(path) {
			continue
		}
		return b
	}
	return ""
}

// cursorsAgent: the `agent` at path is Cursor's, a link into its installed
// versions. Other tools are called agent too.
func cursorsAgent(path string) bool {
	real, err := filepath.EvalSymlinks(path)
	return err == nil && strings.Contains(filepath.ToSlash(real), "/cursor-agent/versions/")
}

// launchArgv is the agent's command, run as bin: Claude Code takes the
// session's name (--name, which its /rename changes later) and the first
// message as its prompt; Codex and Cursor take the prompt; OpenCode takes
// it with --prompt.
func launchArgv(bin, agent, name, first string) []string {
	argv := []string{bin}
	switch agent {
	case "claude":
		argv = append(argv, "--name", name)
	case "opencode":
		if first != "" {
			argv = append(argv, "--prompt")
		}
	}
	if first != "" {
		argv = append(argv, first)
	}
	return argv
}

func (s *Service) tmuxOut(argv []string) (string, error) {
	if s.TmuxOut != nil {
		return s.TmuxOut(argv)
	}
	out, err := exec.Command(argv[0], argv[1:]...).Output() //nolint:gosec // argv built here, never a shell string
	return string(out), err
}

func (s *Service) isDir(path string) bool {
	if s.IsDir != nil {
		return s.IsDir(path)
	}
	fi, err := os.Stat(path)
	return err == nil && fi.IsDir()
}

// tmuxSessions are the names of the tmux sessions on server, and how many
// windows each holds, as the last sample listed every pane.
func (s *Service) tmuxSessions(server string) map[string]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	windows := map[string]map[string]bool{}
	for _, p := range s.cur.Panes {
		if sv, _ := sample.SplitPane(p.ID); sv != server {
			continue
		}
		sess, rest, _ := strings.Cut(p.Target, ":")
		win, _, _ := strings.Cut(rest, ".")
		if windows[sess] == nil {
			windows[sess] = map[string]bool{}
		}
		windows[sess][win] = true
	}
	out := make(map[string]int, len(windows))
	for name, w := range windows {
		out[name] = len(w)
	}
	return out
}

func taken(name string) error {
	return fmt.Errorf("a tmux session is named %s already: choose another name", name)
}

// waitsOn tells if the session waits for the builder: a question or a
// permission prompt, by its hooks or its pane.
func (s *Service) waitsOn(pane string, x sample.Session) bool {
	if x.Ask != nil || x.Permits {
		return true
	}
	it, ok := s.queued(pane)
	return ok && stoker.Waits(it)
}

// kill ends the agent in pane and closes the pane: kill-pane hangs it up,
// and a TERM follows for an agent that outlived its pane. Both steps are
// guarded by the agent's pid and start time, so a pid reused since is
// never signalled. The builder confirms a second time (KillAnyway) for a
// session that works or waits. The stoker kills only a session at rest
// that you did not park, and only with the order's kills go or your y.
func (s *Service) kill(ctx context.Context, pane string, a proto.Act) proto.Result {
	if a.Confirm != "y" {
		return proto.Result{Err: "needs confirm"}
	}
	if s.fromStoker(proto.Act{From: pane}) {
		return proto.Result{Err: "the stoker is not killed here: Q on the STOKER tab quits it"}
	}
	x, ok := s.sessionAt(pane)
	if !ok {
		return proto.Result{Err: "no agent session in " + pane + " any more"}
	}
	word := ""
	switch {
	case s.waitsOn(pane, x):
		word = "waits for you"
	case x.Works():
		word = "works"
	}
	if s.agentsPane(a) {
		if word != "" {
			return proto.Result{Err: x.Name + " " + word + ": the stoker kills only a session at rest"}
		}
		if h, held := s.heldOf(x.SessionID); held && h.By == holds.ByYou {
			return proto.Result{Err: "parked by you: the stoker leaves it alone"}
		}
		if !s.killGo(a.From, pane) {
			s.askKill(a.From, pane, x)
			return proto.Result{Err: "the kill waits for the builder's go: it is on the card, y gives it"}
		}
	} else if word != "" && a.Text != KillAnyway {
		return proto.Result{Err: x.Name + " " + word + ": y again kills it mid-turn"}
	}
	release := s.holdPane(pane)
	res := s.Actions.Do(ctx, KillAction(pane, x), "y")
	release()
	if len(res.Ran) > 0 {
		res.Skipped = nil // the agent went with its pane: no TERM was needed
		s.renames.drop(pane)
	}
	return res
}

// KillAction is what kill runs: both steps guarded by the agent's pid
// and start time.
func KillAction(pane string, x sample.Session) advice.Action {
	steps := proto.KillSteps(pane, x.PID)
	g := advice.Guard{PID: x.PID, StartTicks: x.Start}
	guards := make([]advice.Guard, len(steps))
	for i := range guards {
		guards[i] = g
	}
	return advice.Action{Label: "kill", Steps: steps, Guards: guards, Destructive: true, Each: true}
}

// killGo tells if the stoker's kill of pane may go: the order gave kills,
// or the builder's y gave the go for this one, which it uses up.
func (s *Service) killGo(from, pane string) bool {
	ok := false
	_ = s.updateDriven(func(d *stoker.Driven, grants []string) bool {
		if slices.Contains(grants, "kill") {
			ok = true
			return false
		}
		need := "kill:" + pane
		i := slices.Index(d.Steps[from], need)
		if i < 0 {
			return false
		}
		d.Steps[from], ok = slices.Delete(d.Steps[from], i, i+1), true
		return true
	})
	return ok
}

// askKill puts the stoker's kill on the card as a step that waits for the
// builder's y, once while it waits.
func (s *Service) askKill(from, pane string, x sample.Session) {
	if s.Stoker == nil {
		return
	}
	who := "stoker"
	if !s.fromStoker(proto.Act{From: from}) {
		if y, ok := s.sessionAt(from); ok {
			who = y.Name
		}
	}
	now := s.now()
	need := "kill:" + pane
	want := "matchblox kill " + x.Name
	e := stoker.Entry{
		ID: "go@" + now.UTC().Format(time.RFC3339Nano) + "/" + need, At: now, Session: x.SessionID, Name: who, Pane: from,
		Rule: stoker.RuleGo, State: stoker.StepWaits, Needs: need, Want: want, Argv: [][]string{{want}},
		Text: who + " wants to kill " + x.Name,
	}
	if stoker.LastGo(s.Stoker.View().Run, e).State == stoker.StepWaits {
		return
	}
	if s.Stoker.Append(e) == nil {
		s.refreshStoker()
	}
}

// renameState keeps the renames that wait for their session's rest.
type renameState struct {
	mu   sync.Mutex
	wait map[string]pendingRename
}

type pendingRename struct {
	name string
	pid  int
}

func (r *renameState) set(pane string, p pendingRename) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.wait == nil {
		r.wait = map[string]pendingRename{}
	}
	r.wait[pane] = p
}

func (r *renameState) drop(pane string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.wait, pane)
}

func (r *renameState) all() map[string]pendingRename {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make(map[string]pendingRename, len(r.wait))
	for k, v := range r.wait {
		out[k] = v
	}
	return out
}

// names are the names that wait, by pane. Nil: none.
func (r *renameState) names() map[string]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(r.wait) == 0 {
		return nil
	}
	out := make(map[string]string, len(r.wait))
	for pane, p := range r.wait {
		out[pane] = p.name
	}
	return out
}

// agentBin is the command that starts agent on this machine.
func (s *Service) agentBin(agent string) string {
	look := s.LookPath
	if look == nil {
		look = exec.LookPath
	}
	return agentBin(agent, look)
}

// startable are the agents of Startable installed here.
func (s *Service) startable() []string {
	look := s.LookPath
	if look == nil {
		look = exec.LookPath
	}
	var out []string
	for _, a := range Startable {
		if agentBin(a, look) != "" {
			out = append(out, a)
		}
	}
	return out
}

// rename names the session in pane: its window, its tmux session when the
// agent is that session's one window, and a Claude Code agent with
// /rename, so all of them read the same. Claude Code takes /rename only
// at rest, so a busy one is renamed, both sides at once, when it rests.
func (s *Service) rename(ctx context.Context, pane string, a proto.Act) proto.Result {
	name := strings.TrimSpace(a.Text)
	if err := proto.CheckName(name); err != nil {
		return proto.Result{Err: err.Error()}
	}
	if s.fromStoker(proto.Act{From: pane}) {
		return proto.Result{Err: "the stoker's session keeps its name"}
	}
	x, ok := s.sessionAt(pane)
	if !ok {
		return proto.Result{Err: "no agent session in " + pane + " any more"}
	}
	server, _ := sample.SplitPane(pane)
	if _, in := s.tmuxSessions(server)[name]; in && name != x.TmuxSession() {
		return proto.Result{Err: taken(name).Error()}
	}
	if x.Agent == "claude" && (!s.resting(pane) || s.waitsOn(pane, x)) {
		s.renames.set(pane, pendingRename{name: name, pid: x.PID})
		s.mu.Lock()
		s.cur.Renames = s.renames.names()
		s.mu.Unlock()
		s.broadcast()
		return proto.Result{Skipped: []string{x.Name + " renames when it rests"}}
	}
	s.renames.drop(pane)
	return s.doRename(ctx, pane, x, name)
}

func (s *Service) doRename(ctx context.Context, pane string, x sample.Session, name string) proto.Result {
	server, _ := sample.SplitPane(pane)
	one := s.tmuxSessions(server)[x.TmuxSession()] == 1
	release := s.holdPane(pane)
	defer release()
	return s.Actions.Do(ctx, RenameAction(pane, x, name, one), "y")
}

// RenameAction is what rename runs: the agent's /rename first, guarded
// as a message at rest, so a guard that stops it leaves tmux as it was.
func RenameAction(pane string, x sample.Session, name string, one bool) advice.Action {
	a := advice.Action{Label: "rename", Destructive: true, Steps: panes.RenameSteps(pane, x.Agent, name, one)}
	if x.Agent == "claude" {
		g := advice.Guard{MessagePane: pane, MessagePID: x.PID, RestPane: pane}
		a.Guards = []advice.Guard{g, g}
	}
	return a
}

// renameAtRest runs the renames whose session rests now. One whose agent
// is gone or replaced is dropped.
func (s *Service) renameAtRest(ctx context.Context) {
	for pane, p := range s.renames.all() {
		x, ok := s.sessionAt(pane)
		switch {
		case !ok || x.PID != p.pid:
			s.renames.drop(pane)
		case s.resting(pane) && !s.waitsOn(pane, x):
			s.renames.drop(pane)
			s.doRename(ctx, pane, x, p.name)
		}
	}
}

// newSession starts an agent in a tmux session of its own on the
// builder's server, never the stoker's: the session, its window and the
// agent share one name. With a branch it first adds a worktree for it
// next to the repository, branched from origin's default branch (or HEAD)
// and tracking nothing, so a bare push never goes to main. The stoker
// starts sessions only in a run, each a message toward max_steps, and the
// session it starts is one it drives from its first second.
func (s *Service) newSession(ctx context.Context, a proto.Act) proto.Result {
	var n proto.NewSession
	if err := json.Unmarshal([]byte(a.Text), &n); err != nil {
		return proto.Result{Err: "the new session does not read: " + err.Error()}
	}
	if n.Agent == "cursor" {
		n.Agent = sample.CursorAgent
	}
	steps, name, dir, err := s.newSteps(ctx, n)
	if err != nil {
		return proto.Result{Err: err.Error()}
	}
	steps = append(steps, slices.Concat(
		[]string{"env", "-u", "GIT_TERMINAL_PROMPT", "-u", "GIT_SSH_COMMAND", "-u", panes.HomeEnv},
		sample.TmuxOn(""), []string{"new-session", "-d", "-s", name, "-n", name, "-c", dir},
		launchArgv(s.agentBin(n.Agent), n.Agent, name, n.First)))
	res := s.Actions.Do(ctx, advice.Action{Label: "new session", Steps: steps}, a.Confirm)
	if res.Err != "" || len(res.Ran) < len(steps) {
		return res
	}
	if s.agentsPane(a) {
		out, _ := s.tmuxOut(slices.Concat(sample.TmuxOn(""), []string{"display-message", "-p", "-t", "=" + name + ":", "#{pane_id}"}))
		s.recordNew(strings.TrimSpace(out), name, res.Ran)
	}
	select {
	case s.sampleKick <- struct{}{}:
	default: // a sample is asked for already
	}
	return res
}

// newSteps checks a new session and builds the worktree's step, if any.
// It returns the session's name and the directory it starts in.
func (s *Service) newSteps(ctx context.Context, n proto.NewSession) (steps [][]string, name, dir string, err error) {
	look := s.LookPath
	if look == nil {
		look = exec.LookPath
	}
	switch {
	case !slices.Contains(Startable, n.Agent):
		return nil, "", "", fmt.Errorf("%s is not an agent matchblox starts: claude, codex, opencode or cursor-agent", n.Agent)
	}
	if rest, ok := strings.CutPrefix(n.Where, "~/"); ok || n.Where == "~" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, "", "", err
		}
		n.Where = filepath.Join(home, rest)
	}
	if !filepath.IsAbs(n.Where) {
		return nil, "", "", errors.New("a new session starts in a full path")
	}
	if agentBin(n.Agent, look) == "" {
		return nil, "", "", fmt.Errorf("%s is not installed here", n.Agent)
	}
	if n.First != "" {
		if err := panes.CheckAnswer(n.First); err != nil {
			return nil, "", "", errors.New(strings.Replace(err.Error(), "the answer", "the first message", 1))
		}
		// It goes to the agent as an argument: one that starts with a dash
		// would be read as a flag (--dangerously-skip-permissions).
		if strings.HasPrefix(strings.TrimSpace(n.First), "-") {
			return nil, "", "", errors.New("a first message does not start with -")
		}
	}
	dir = filepath.Clean(n.Where)
	if !s.isDir(dir) {
		return nil, "", "", fmt.Errorf("no directory %s", dir)
	}
	if n.Branch != "" {
		step, wt, err := worktreeStep(ctx, dir, n.Branch)
		if err != nil {
			return nil, "", "", err
		}
		steps, dir = append(steps, step), wt
	}
	name = n.Name
	if name == "" {
		name = proto.NameOf(filepath.Base(dir))
	}
	if err := proto.CheckName(name); err != nil {
		return nil, "", "", err
	}
	if _, in := s.tmuxSessions("")[name]; in {
		return nil, "", "", taken(name)
	}
	return steps, name, dir, nil
}

// worktreeStep adds a worktree for branch beside the repository dir is
// in: <repo>-<branch>, from origin's default branch, or HEAD with no
// origin.
func worktreeStep(ctx context.Context, dir, branch string) ([]string, string, error) {
	git := func(args ...string) (string, error) {
		out, err := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...).Output() //nolint:gosec // argv, never a shell string
		return strings.TrimSpace(string(out)), err
	}
	root, err := git("rev-parse", "--show-toplevel")
	if err != nil || root == "" {
		return nil, "", fmt.Errorf("a new worktree needs a git repository: %s is none", dir)
	}
	if _, err := git("check-ref-format", "--branch", branch); err != nil {
		return nil, "", fmt.Errorf("%s is no branch name", branch)
	}
	if _, err := git("rev-parse", "--verify", "-q", "refs/heads/"+branch); err == nil {
		return nil, "", fmt.Errorf("the branch %s is there already: start the session in its worktree", branch)
	}
	wt := filepath.Join(filepath.Dir(root), filepath.Base(root)+"-"+proto.NameOf(branch))
	if _, err := os.Stat(wt); err == nil {
		return nil, "", fmt.Errorf("%s is there already", wt)
	}
	base := "HEAD"
	if _, err := git("rev-parse", "--verify", "-q", "refs/remotes/origin/HEAD"); err == nil {
		base = "origin/HEAD"
	}
	return []string{"git", "-C", root, "worktree", "add", "--no-track", "-b", branch, wt, base}, wt, nil
}

// recordNew keeps a session the stoker started in the run: it counts as a
// message, and the stoker drives its pane, so the guard watches it.
func (s *Service) recordNew(pane, name string, ran [][]string) {
	if s.Stoker == nil || pane == "" {
		return
	}
	now := s.now()
	_ = s.Stoker.Append(stoker.Entry{
		ID: name + "@" + now.UTC().Format(time.RFC3339Nano), At: now,
		Name: name, Pane: pane, Rule: stoker.RuleOrder, Argv: ran, Result: stoker.Sent,
	})
	s.drive(pane)
	s.refreshStoker()
}
