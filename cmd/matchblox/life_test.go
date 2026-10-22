package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/blox-eng/matchblox/internal/proto"
	"github.com/blox-eng/matchblox/internal/sample"
	"github.com/blox-eng/matchblox/internal/transport"
)

func TestKillAndRenameNameThePane(t *testing.T) {
	a, err := lifeAct(cliDoc(), "kill", []string{"api"}, false, "%9")
	if err != nil || a.RecID != "kill:%3" || a.Text != "" || a.Confirm != "y" || a.From != "%9" {
		t.Fatalf("%+v %v", a, err)
	}
	if a, err = lifeAct(cliDoc(), "kill", []string{"%4"}, true, ""); err != nil || a.Text != "anyway" {
		t.Fatalf("%+v %v", a, err)
	}
	if a, err = lifeAct(cliDoc(), "rename", []string{"api", "ships-flake"}, false, ""); err != nil || a.RecID != "rename:%3" || a.Text != "ships-flake" {
		t.Fatalf("%+v %v", a, err)
	}
}

func TestKillAndRenameRefusals(t *testing.T) {
	for name, c := range map[string]struct {
		cmd   string
		words []string
		want  string
	}{
		"kill nobody":   {"kill", nil, "kill needs a session: matchblox kill <who>"},
		"kill two":      {"kill", []string{"api", "web"}, "kill takes one session"},
		"rename bare":   {"rename", []string{"api"}, "rename needs a name: matchblox rename <who> <name>"},
		"rename spaces": {"rename", []string{"api", "a", "b"}, "a name is one word"},
		"rename bad":    {"rename", []string{"api", "a.b"}, "a name is letters, digits"},
		"unknown":       {"kill", []string{"nope"}, "no session named nope"},
	} {
		if _, err := lifeAct(cliDoc(), c.cmd, c.words, false, ""); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: %v, want %q", name, err, c.want)
		}
	}
}

func TestNewTakesWhereAgentAndFirstMessage(t *testing.T) {
	a, err := newSessionAct([]string{"work", "on", "#88"}, newOpts{where: "/src/api", agent: "codex", branch: "feat/88", name: "n88"}, "%9", "/home/u")
	if err != nil || a.RecID != "new:session" || a.From != "%9" {
		t.Fatalf("%+v %v", a, err)
	}
	var n proto.NewSession
	if err := json.Unmarshal([]byte(a.Text), &n); err != nil {
		t.Fatal(err)
	}
	if n != (proto.NewSession{Where: "/src/api", Agent: "codex", Branch: "feat/88", Name: "n88", First: "work on #88"}) {
		t.Fatalf("%+v", n)
	}
	// Bare, it starts Claude Code here.
	if a, err = newSessionAct(nil, newOpts{}, "", "/home/u/src"); err != nil {
		t.Fatal(err)
	}
	var bare proto.NewSession
	if err := json.Unmarshal([]byte(a.Text), &bare); err != nil || bare != (proto.NewSession{Where: "/home/u/src", Agent: "claude"}) {
		t.Fatalf("%+v %v", bare, err)
	}
	// A relative --where is from here.
	if a, err = newSessionAct(nil, newOpts{where: "api"}, "", "/home/u/src"); err != nil || !strings.Contains(a.Text, `"where":"/home/u/src/api"`) {
		t.Fatalf("%s %v", a.Text, err)
	}
}

func TestSwitchNamesThePaneAndTheAgent(t *testing.T) {
	decode := func(a proto.Act) proto.SwitchAgent {
		t.Helper()
		var sw proto.SwitchAgent
		if err := json.Unmarshal([]byte(a.Text), &sw); err != nil {
			t.Fatal(err)
		}
		return sw
	}
	a, err := switchAct(cliDoc(), []string{"api"}, switchOpts{agent: "codex"}, "%9")
	if err != nil || a.RecID != "switch:%3" || a.Confirm != "y" || a.From != "%9" {
		t.Fatalf("%+v %v", a, err)
	}
	if sw := decode(a); sw != (proto.SwitchAgent{Agent: "codex", Handoff: proto.DefaultHandoff}) {
		t.Fatalf("%+v", sw)
	}
	none := ""
	if a, err = switchAct(cliDoc(), []string{"api"}, switchOpts{agent: "codex", handoff: &none}, ""); err != nil || decode(a).Handoff != "" {
		t.Fatalf("%+v %v", a, err)
	}
	if a, err = switchAct(cliDoc(), []string{"api"}, switchOpts{agent: "codex", anyway: true}, ""); err != nil || !decode(a).Anyway {
		t.Fatalf("%+v %v", a, err)
	}
}

func TestSwitchRefusals(t *testing.T) {
	doc := cliDoc()
	doc.Sessions = append(doc.Sessions, sample.Session{Name: "ghost", SessionID: "s6"})
	for name, c := range map[string]struct {
		words []string
		o     switchOpts
		want  string
	}{
		"nobody":   {nil, switchOpts{agent: "codex"}, "switch needs a session: matchblox switch <who> --agent <name>"},
		"no agent": {[]string{"api"}, switchOpts{}, "switch needs an agent: --agent claude, codex, opencode or cursor-agent"},
		"two":      {[]string{"api", "web"}, switchOpts{agent: "codex"}, "switch takes one session: matchblox switch <who> --agent <name>"},
		"unknown":  {[]string{"nope"}, switchOpts{agent: "codex"}, "no session named nope"},
		"no pane":  {[]string{"ghost"}, switchOpts{agent: "codex"}, "ghost is outside tmux: nothing to switch"},
	} {
		if _, err := switchAct(doc, c.words, c.o, ""); err == nil || err.Error() != c.want {
			t.Errorf("%s: %v, want %q", name, err, c.want)
		}
	}
}

// switchService serves cliDoc and answers every act with res.
func switchService(t *testing.T, res proto.Result) string {
	t.Helper()
	sock := isolate(t)
	l, err := transport.Listen(sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			nc, err := l.Accept()
			if err != nil {
				return
			}
			go func() {
				c := transport.NewConn(nc)
				defer c.Close()
				_ = c.Send(proto.KindHello, "", proto.Hello{Version: proto.Version})
				_ = c.Send(proto.KindSnapshot, "", proto.State{Doc: cliDoc()})
				for {
					env, err := c.Recv()
					if err != nil {
						return
					}
					if env.Kind == proto.KindAct {
						r := res
						r.ActID = env.ID
						_ = c.Send(proto.KindResult, env.ID, r)
					}
				}
			}()
		}
	}()
	return sock
}

func TestSwitchJumpsOnlyAfterItRan(t *testing.T) {
	var jumped [][]string
	jump := func(argv []string) error { jumped = append(jumped, argv); return nil }
	o := switchOpts{agent: "codex"}

	var buf bytes.Buffer
	sock := switchService(t, proto.Result{Ran: [][]string{{"tmux", "respawn-pane"}}})
	if err := cliSwitch([]string{"api"}, o, &buf, sock, jump); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "switched api to codex\n" || len(jumped) != 1 || !slices.Equal(jumped[0], []string{"tmux", "switch-client", "-t", "%3"}) {
		t.Fatalf("%q %v", buf.String(), jumped)
	}

	buf.Reset()
	if err := cliSwitch([]string{"api"}, o, &buf, sock, nil); err != nil || buf.String() != "switched api to codex\n" {
		t.Fatalf("%q %v", buf.String(), err)
	}

	jumped = nil
	sock = switchService(t, proto.Result{Err: "api works: y again switches it mid-turn"})
	err := cliSwitch([]string{"api"}, o, &buf, sock, jump)
	if err == nil || err.Error() != "switch api: refused: api works: y again switches it mid-turn" || jumped != nil {
		t.Fatalf("%v %v", err, jumped)
	}

	buf.Reset()
	sock = switchService(t, proto.Result{Ran: [][]string{{"tmux"}}})
	failing := func([]string) error { return errors.New("no client") }
	if err := cliSwitch([]string{"api"}, o, &buf, sock, failing); err != nil || !strings.Contains(buf.String(), "switched, but not jumped: no client") {
		t.Fatalf("%q %v", buf.String(), err)
	}
}
