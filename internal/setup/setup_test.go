package setup

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/blox-eng/matchblox/internal/config"
	"github.com/blox-eng/matchblox/internal/doors"
	"github.com/blox-eng/matchblox/internal/panes"
	"github.com/blox-eng/matchblox/internal/stoker"
)

const exe = "/opt/mb/matchblox"

// env is a machine with a fresh home: no tmux, no hooks, Claude Code found.
func env(t *testing.T) Env {
	t.Helper()
	home := t.TempDir()
	return Env{
		GOOS:      "linux",
		OSRelease: "ID=debian\n",
		Home:      home,
		Exe:       exe,
		StateDir:  filepath.Join(home, ".local", "state", "matchblox"),
		Getenv:    func(string) string { return "" },
		LookPath: func(name string) (string, error) {
			switch name {
			case "claude", "apt-get", "sudo":
				return "/usr/bin/" + name, nil
			}
			return "", errors.New("not found")
		},
		Source: func(string) error { return nil },
	}
}

func ids(ds []doors.Door) []string {
	var out []string
	for _, d := range ds {
		if d.Open() {
			out = append(out, d.ID)
		}
	}
	return out
}

func door(t *testing.T, e Env, id string) doors.Door {
	t.Helper()
	for _, d := range Doors(e) {
		if d.ID == id {
			return d
		}
	}
	t.Fatalf("no door %s", id)
	return doors.Door{}
}

func TestDoorsFirstRun(t *testing.T) {
	e := env(t)
	got := ids(Doors(e))
	if want := []string{doors.Tmux, doors.Hooks, doors.Limits, doors.SafetyNet, doors.WayBack, doors.Pair, doors.KeepRunning, doors.Guide, doors.Night, doors.Ways}; !slices.Equal(got, want) {
		t.Fatalf("doors = %v, want %v", got, want)
	}
	if d := door(t, e, doors.Tmux); !slices.Equal(d.Term, []string{"sudo", "sh", "-c", "apt-get update && apt-get install -y tmux"}) {
		t.Fatalf("tmux door Term = %q", d.Term)
	}
}

func TestNoClaudeNoHooksOrGuide(t *testing.T) {
	e := env(t)
	e.LookPath = func(string) (string, error) { return "", errors.New("not found") }
	if got := ids(Doors(e)); !slices.Equal(got, []string{doors.Tmux, doors.WayBack, doors.Pair, doors.KeepRunning, doors.Ways}) {
		t.Fatalf("doors = %v", got)
	}
}

func TestUnknownOSSaysHowToInstallTmux(t *testing.T) {
	e := env(t)
	e.OSRelease = "ID=plan9\n"
	d := door(t, e, doors.Tmux)
	if d.Term != nil || !strings.Contains(d.Problem, "https://github.com/tmux/tmux/wiki/Installing") {
		t.Fatalf("door = %+v", d)
	}
}

const others = `{
  "model": "opus",
  "hooks": {
    "Stop": [
      {
        "hooks": [
          {
            "type": "command",
            "command": "notify-send done"
          }
        ]
      }
    ]
  },
  "theme": "dark"
}
`

func settings(t *testing.T, e Env, body string) string {
	t.Helper()
	p := filepath.Join(e.Home, ".claude", "settings.json")
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func open(t *testing.T, e Env, id string) string {
	t.Helper()
	backup, err := Open(e, id, door(t, e, id).Sum, false)
	if err != nil {
		t.Fatalf("Open(%s): %v", id, err)
	}
	return backup
}

// commands is every hook command of each event.
func commands(t *testing.T, path string) map[string][]string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var s struct {
		Hooks map[string][]struct {
			Hooks []struct{ Command string } `json:"hooks"`
		} `json:"hooks"`
	}
	if err := json.Unmarshal(b, &s); err != nil {
		t.Fatalf("settings.json is not JSON: %v\n%s", err, b)
	}
	out := map[string][]string{}
	for ev, groups := range s.Hooks {
		for _, g := range groups {
			for _, h := range g.Hooks {
				out[ev] = append(out[ev], h.Command)
			}
		}
	}
	return out
}

func TestAddHooksKeepsOtherHooks(t *testing.T) {
	e := env(t)
	p := settings(t, e, others)
	open(t, e, doors.Hooks)
	got := commands(t, p)
	for _, ev := range Events {
		if !slices.Contains(got[ev], exe+" hook "+ev) {
			t.Errorf("%s: %q has no matchblox hook", ev, got[ev])
		}
	}
	if got["Stop"][0] != "notify-send done" {
		t.Errorf("Stop = %q, the other hook is gone", got["Stop"])
	}
	b, _ := os.ReadFile(p)
	if !strings.HasPrefix(string(b), "{\n  \"model\": \"opus\",\n  \"hooks\": {") || !strings.HasSuffix(string(b), "  \"theme\": \"dark\"\n}\n") {
		t.Errorf("the other keys moved:\n%s", b)
	}
}

func TestAddHooksKeepsTheFormat(t *testing.T) {
	e := env(t)
	settings(t, e, others)
	d := door(t, e, doors.Hooks)
	for line := range strings.SplitSeq(d.Preview, "\n") {
		if strings.HasPrefix(line, "-") && strings.TrimSpace(line) != "-      }" {
			t.Errorf("the diff removes %q; only the end of the last Stop entry gains a comma", line)
		}
	}
	if !strings.Contains(d.Preview, "-      }\n+      },\n") {
		t.Errorf("the changed line does not read old, then new:\n%s", d.Preview)
	}
	if !strings.Contains(d.Preview, `+      {"hooks": [{"type": "command", "command": "`+exe+` hook Stop"}]}`) {
		t.Errorf("preview has no one-line Stop entry:\n%s", d.Preview)
	}
}

func TestAddHooksIsIdempotent(t *testing.T) {
	e := env(t)
	p := settings(t, e, others)
	open(t, e, doors.Hooks)
	first, _ := os.ReadFile(p)
	if d := door(t, e, doors.Hooks); !d.Done {
		t.Fatalf("hooks door not done after Open: %+v", d)
	}
	if _, err := AddHooks(p, exe, Sum(first)); err != nil {
		t.Fatal(err)
	}
	second, _ := os.ReadFile(p)
	if string(first) != string(second) {
		t.Fatalf("a second run changed the file:\n%s", second)
	}
}

func TestAddHooksSeesAnotherPath(t *testing.T) {
	e := env(t)
	settings(t, e, others)
	open(t, e, doors.Hooks)
	e.Exe = "/usr/local/bin/matchblox"
	if d := door(t, e, doors.Hooks); !d.Done {
		t.Fatalf("hooks of another matchblox path count as done: %+v", d)
	}
}

func TestAddHooksWritesBackup(t *testing.T) {
	e := env(t)
	p := settings(t, e, others)
	backup := open(t, e, doors.Hooks)
	b, err := os.ReadFile(backup)
	if err != nil || string(b) != others {
		t.Fatalf("backup %s = %q, %v", backup, b, err)
	}
	dir, _ := filepath.EvalSymlinks(filepath.Dir(p)) // macOS: /var is a link to /private/var
	if filepath.Dir(backup) != dir {
		t.Fatalf("backup %s is not next to %s", backup, p)
	}
	if fi, _ := os.Stat(p); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode = %v, want 0600", fi.Mode().Perm())
	}
}

func TestAddHooksCreatesTheFile(t *testing.T) {
	e := env(t)
	backup := open(t, e, doors.Hooks)
	if backup != "" {
		t.Fatalf("backup of a missing file: %s", backup)
	}
	got := commands(t, filepath.Join(e.Home, ".claude", "settings.json"))
	if len(got) != len(Events) {
		t.Fatalf("events = %v", got)
	}
}

func TestAddHooksFollowsALink(t *testing.T) {
	e := env(t)
	real := filepath.Join(t.TempDir(), "dotfiles-settings.json")
	if err := os.WriteFile(real, []byte(others), 0o600); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(e.Home, ".claude", "settings.json")
	_ = os.MkdirAll(filepath.Dir(p), 0o700)
	if err := os.Symlink(real, p); err != nil {
		t.Fatal(err)
	}
	open(t, e, doors.Hooks)
	if fi, err := os.Lstat(p); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatalf("the link was replaced by a file")
	}
	if got := commands(t, real); len(got["Stop"]) != 2 {
		t.Fatalf("the link target did not get the hooks: %v", got)
	}
}

func TestHooksDoorRefusesBrokenJSON(t *testing.T) {
	e := env(t)
	settings(t, e, "{ not json")
	d := door(t, e, doors.Hooks)
	if d.Sum != "" || !strings.Contains(d.Problem, "settings.json is not valid JSON") {
		t.Fatalf("door = %+v", d)
	}
}

func TestDoorRefusesChangedFile(t *testing.T) {
	e := env(t)
	p := settings(t, e, others)
	seen := door(t, e, doors.Hooks).Sum
	changed := strings.Replace(others, "opus", "sonnet", 1)
	if err := os.WriteFile(p, []byte(changed), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(e, doors.Hooks, seen, false); !errors.Is(err, ErrChanged) {
		t.Fatalf("Open after a change: %v, want ErrChanged", err)
	}
	if b, _ := os.ReadFile(p); string(b) != changed {
		t.Fatalf("the file was written:\n%s", b)
	}
}

func TestAddWayBackOnce(t *testing.T) {
	e := env(t)
	var sourced []string
	e.Source = func(conf string) error { sourced = append(sourced, conf); return nil }
	conf := filepath.Join(e.Home, ".tmux.conf")
	if err := os.WriteFile(conf, []byte("set -g prefix C-a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := door(t, e, doors.WayBack)
	if d.Path != conf || !strings.Contains(d.Preview, "+bind m switch-client -t =matchblox") {
		t.Fatalf("door = %+v", d)
	}
	backup := open(t, e, doors.WayBack)
	b, _ := os.ReadFile(conf)
	if want := "set -g prefix C-a\n\n" + panes.WayBack; string(b) != want {
		t.Fatalf("tmux.conf =\n%s\nwant\n%s", b, want)
	}
	if bk, _ := os.ReadFile(backup); string(bk) != "set -g prefix C-a\n" {
		t.Fatalf("backup = %q", bk)
	}
	if !slices.Equal(sourced, []string{conf}) {
		t.Fatalf("sourced %v", sourced)
	}
	if d := door(t, e, doors.WayBack); !d.Done {
		t.Fatalf("way back not done: %+v", d)
	}
	if _, err := AddWayBack(conf, Sum(b), false, e.Source); err != nil {
		t.Fatal(err)
	}
	if again, _ := os.ReadFile(conf); string(again) != string(b) {
		t.Fatalf("a second run appended again:\n%s", again)
	}
}

func TestWayBackUsesTheXDGConf(t *testing.T) {
	e := env(t)
	xdg := filepath.Join(e.Home, ".config", "tmux", "tmux.conf")
	_ = os.MkdirAll(filepath.Dir(xdg), 0o700)
	if err := os.WriteFile(xdg, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if d := door(t, e, doors.WayBack); d.Path != xdg {
		t.Fatalf("path = %s, want %s", d.Path, xdg)
	}
}

func TestWayBackWithoutAServerStillWrites(t *testing.T) {
	e := env(t)
	e.Source = func(string) error { return ErrNoServer }
	open(t, e, doors.WayBack)
	if d := door(t, e, doors.WayBack); !d.Done {
		t.Fatalf("door = %+v", d)
	}
}

func TestDoneDoorFolds(t *testing.T) {
	e := env(t)
	e.LookPath = func(name string) (string, error) { return "/usr/bin/" + name, nil }
	open(t, e, doors.Hooks)
	open(t, e, doors.Limits)
	open(t, e, doors.SafetyNet)
	open(t, e, doors.WayBack)
	if got := ids(Doors(e)); !slices.Equal(got, []string{doors.Pair, doors.KeepRunning, doors.Guide, doors.Night, doors.Ways}) {
		t.Fatalf("open doors = %v, want [pair keeprunning guide night ways]", got)
	}
}

func TestClosedDoorStaysClosed(t *testing.T) {
	e := env(t)
	if err := Close(e, doors.Guide); err != nil {
		t.Fatal(err)
	}
	d := door(t, e, doors.Guide)
	if !d.Closed || d.Open() {
		t.Fatalf("guide = %+v", d)
	}
	if err := Close(e, "nothing"); err == nil {
		t.Fatal("Close of an unknown door: no error")
	}
}

func TestGuideIsATermDoor(t *testing.T) {
	d := door(t, env(t), doors.Guide)
	if !slices.Equal(d.Term, doors.GuideArgv(false)) || !strings.Contains(d.Why, "uses your tokens") {
		t.Fatalf("guide = %+v", d)
	}
	if _, err := Open(env(t), doors.Guide, "", false); err == nil {
		t.Fatal("the service opened a terminal door")
	}
}

func TestThresholdsFromMachine(t *testing.T) {
	a := Thresholds(16, 64<<30)
	if a.Load1Over != 16 || a.MemAvailableUnderGB != 6 {
		t.Fatalf("16 cores, 64 GB: %+v", a)
	}
	if a := Thresholds(2, 2<<30); a.MemAvailableUnderGB != 1 {
		t.Fatalf("2 GB: %+v", a)
	}
}

func TestWriteConfigOnce(t *testing.T) {
	p := filepath.Join(t.TempDir(), "matchblox", "config.toml")
	wrote, err := WriteConfig(p, 16, 64<<30, []string{"claude", "codex", "opencode"})
	if err != nil || !wrote {
		t.Fatalf("WriteConfig = %v, %v", wrote, err)
	}
	b, _ := os.ReadFile(p)
	for _, want := range []string{"load1_over = 16", "mem_available_under_gb = 6"} {
		if !strings.Contains(string(b), want) {
			t.Errorf("config.toml has no %q:\n%s", want, b)
		}
	}
	if c, err := config.Load(p); err != nil || strings.Join(c.Agents, ",") != "claude,codex,opencode" || c.Alerts.Load1Over != 16 {
		t.Errorf("config.toml reads back as agents %q load %v (%v):\n%s", c.Agents, c.Alerts.Load1Over, err, b)
	}
	if err := os.WriteFile(p, []byte("# mine\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if wrote, _ := WriteConfig(p, 8, 8<<30, nil); wrote {
		t.Fatal("WriteConfig overwrote a config")
	}
}

func TestAddHooksWithASpacedPathFolds(t *testing.T) {
	e := env(t)
	e.Exe = "/home/u/My Tools/matchblox"
	p := settings(t, e, others)
	open(t, e, doors.Hooks)
	if d := door(t, e, doors.Hooks); !d.Done {
		t.Fatalf("door still open after one open:\n%s", d.Preview)
	}
	if got := commands(t, p)["Stop"]; len(got) != 2 || got[1] != "'/home/u/My Tools/matchblox' hook Stop" {
		t.Fatalf("Stop = %q", got)
	}
}

func TestADuplicateHooksKeyEditsTheOneClaudeReads(t *testing.T) {
	e := env(t)
	p := settings(t, e, `{"hooks": {}, "hooks": {"Stop": []}}`)
	open(t, e, doors.Hooks)
	b, _ := os.ReadFile(p)
	var s struct {
		Hooks map[string]json.RawMessage `json:"hooks"` // the last key wins, as in JSON.parse
	}
	if err := json.Unmarshal(b, &s); err != nil || len(s.Hooks) != len(Events) {
		t.Fatalf("the last hooks has %d events, %v:\n%s", len(s.Hooks), err, b)
	}
}

func TestNullHooksIsAnEmptyObject(t *testing.T) {
	e := env(t)
	settings(t, e, `{"hooks": null}`)
	if d := door(t, e, doors.Hooks); d.Problem != "" || d.Sum == "" {
		t.Fatalf("door = %+v", d)
	}
}

func TestADanglingLinkStaysALink(t *testing.T) {
	e := env(t)
	target := filepath.Join(t.TempDir(), "dotfiles", "settings.json")
	p := filepath.Join(e.Home, ".claude", "settings.json")
	_ = os.MkdirAll(filepath.Dir(p), 0o700)
	if err := os.Symlink(target, p); err != nil {
		t.Fatal(err)
	}
	open(t, e, doors.Hooks)
	if fi, err := os.Lstat(p); err != nil || fi.Mode()&os.ModeSymlink == 0 {
		t.Fatal("the link was replaced by a file")
	}
	if got := commands(t, target); len(got) != len(Events) {
		t.Fatalf("target = %v", got)
	}
}

func TestBackupsNeverOverwriteEachOther(t *testing.T) {
	e := env(t)
	p := settings(t, e, others)
	b1, err := AddHooks(p, exe, Sum([]byte(others)))
	if err != nil {
		t.Fatal(err)
	}
	cur, _ := os.ReadFile(p)
	b2, err := AddHooks(p, "/other/matchblox", Sum(cur)) // no change: no backup
	if err != nil || b2 != "" {
		t.Fatalf("unchanged write made backup %q, %v", b2, err)
	}
	_ = os.WriteFile(p, []byte(others), 0o600)
	b3, err := AddHooks(p, exe, Sum([]byte(others)))
	if err != nil || b3 == b1 {
		t.Fatalf("second backup %q = first %q, %v", b3, b1, err)
	}
}

func TestTmuxOnAFreshDebianUpdatesFirst(t *testing.T) {
	d := door(t, env(t), doors.Tmux)
	want := []string{"sudo", "sh", "-c", "apt-get update && apt-get install -y tmux"}
	if !slices.Equal(d.Term, want) || !doors.TermAllowed(d.Term) {
		t.Fatalf("Term = %q", d.Term)
	}
}

func TestTmuxWithoutSudoOrRootSaysHow(t *testing.T) {
	e := env(t)
	e.LookPath = func(name string) (string, error) {
		if name == "apt-get" {
			return "/usr/bin/apt-get", nil
		}
		return "", errors.New("not found")
	}
	d := door(t, e, doors.Tmux)
	if d.Term != nil || !strings.Contains(d.Problem, "as root") {
		t.Fatalf("door = %+v", d)
	}
	e.Root = true
	if d := door(t, e, doors.Tmux); d.Term == nil {
		t.Fatalf("root: %+v", d)
	}
}

// TestFindAgentsFindsWhatIsInstalled: the first run lists the agents on
// PATH, in the order of KnownAgents; with none, Claude Code.
func TestFindAgentsFindsWhatIsInstalled(t *testing.T) {
	on := map[string]bool{"codex": true, "claude": true, "aider": true}
	look := func(name string) (string, error) {
		if on[name] {
			return "/usr/bin/" + name, nil
		}
		return "", errors.New("not found")
	}
	if got := strings.Join(FindAgents(look), ","); got != "claude,codex,aider" {
		t.Fatalf("found %q", got)
	}
	none := func(string) (string, error) { return "", errors.New("not found") }
	if got := strings.Join(FindAgents(none), ","); got != "claude" {
		t.Fatalf("with none installed: %q, want claude", got)
	}
}

// TestFindAgentsAlwaysKeepsClaudeCode: Claude Code can be reached by an
// alias only (~/.claude/local); it is the agent with hooks and stays.
func TestFindAgentsAlwaysKeepsClaudeCode(t *testing.T) {
	look := func(name string) (string, error) {
		if name == "codex" {
			return "/usr/bin/codex", nil
		}
		return "", errors.New("not found")
	}
	if got := strings.Join(FindAgents(look), ","); got != "claude,codex" {
		t.Fatalf("found %q", got)
	}
}

// The night door leads to the stoker, which compacts Claude Code only. It
// folds after the first run and can be closed like any door.
func TestNightDoorFoldsAfterTheFirstRun(t *testing.T) {
	e := env(t)
	d := door(t, e, doors.Night)
	if !d.Open() || !strings.Contains(d.Why, "n keeps your sessions lit") {
		t.Fatalf("night = %+v", d)
	}
	st, err := stoker.Open(e.StateDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Set(stoker.Mode{}.TurnOn(time.Now())); err != nil {
		t.Fatal(err)
	}
	if d := door(t, e, doors.Night); !d.Done {
		t.Fatalf("night after a run = %+v", d)
	}
	e = env(t)
	if err := Close(e, doors.Night); err != nil || !door(t, e, doors.Night).Closed {
		t.Fatalf("close night: %v", err)
	}
}

func TestWayBackOffersTheMouseOffByDefault(t *testing.T) {
	e := env(t)
	e.Source = func(string) error { return nil }
	conf := filepath.Join(e.Home, ".tmux.conf")
	if err := os.WriteFile(conf, []byte("set -g prefix C-a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d := door(t, e, doors.WayBack)
	if d.Also == nil || !strings.Contains(d.Also.Label, "mouse") {
		t.Fatalf("no mouse choice: %+v", d)
	}
	if strings.Contains(d.Preview, "mouse on") || !strings.Contains(d.Also.Preview, "+set -g mouse on") {
		t.Fatalf("preview %q, with the mouse %q", d.Preview, d.Also.Preview)
	}
	open(t, e, doors.WayBack)
	if b, _ := os.ReadFile(conf); strings.Contains(string(b), "mouse on") {
		t.Fatalf("the mouse is on without the choice:\n%s", b)
	}
}

func TestWayBackWithTheMouseWritesIt(t *testing.T) {
	e := env(t)
	e.Source = func(string) error { return nil }
	conf := filepath.Join(e.Home, ".tmux.conf")
	if err := os.WriteFile(conf, []byte("set -g prefix C-a\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(e, doors.WayBack, door(t, e, doors.WayBack).Sum, true); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(conf)
	if want := "set -g prefix C-a\n\n" + panes.WayBack + panes.Mouse; string(b) != want {
		t.Fatalf("tmux.conf =\n%s\nwant\n%s", b, want)
	}
	if d := door(t, e, doors.WayBack); !d.Done {
		t.Fatalf("way back not done: %+v", d)
	}
}

func TestWayBackOffersNoMouseWhenItIsOn(t *testing.T) {
	for _, line := range []string{"set -g mouse on", "set-option -g mouse on", "  set -gq mouse on # taps"} {
		e := env(t)
		conf := filepath.Join(e.Home, ".tmux.conf")
		if err := os.WriteFile(conf, []byte(line+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		if d := door(t, e, doors.WayBack); d.Also != nil {
			t.Errorf("%q: mouse offered again: %+v", line, d.Also)
		}
	}
	e := env(t)
	conf := filepath.Join(e.Home, ".tmux.conf")
	if err := os.WriteFile(conf, []byte("# set -g mouse on\nset -g mouse off\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if d := door(t, e, doors.WayBack); d.Also == nil {
		t.Errorf("a commented or off mouse is not on: %+v", d)
	}
}

// TestTheLimitsDoorTapsTheStatusLine: with no status line the door sets
// `matchblox hook statusline`; a status line the builder has keeps running,
// wrapped, and prints what it printed. A second open changes nothing.
func TestTheLimitsDoorTapsTheStatusLine(t *testing.T) {
	e := env(t)
	d := door(t, e, doors.Limits)
	if d.Title != "Show your limits" || d.Done || !strings.Contains(d.Preview, `+  "statusLine": {"type": "command", "command": "`+exe+` hook statusline"}`) {
		t.Fatalf("a fresh door: %+v", d)
	}
	open(t, e, doors.Limits)
	if d := door(t, e, doors.Limits); !d.Done {
		t.Fatalf("not done after it opened: %+v", d)
	}

	e = env(t)
	path := settingsPath(e)
	writeFile(t, path, `{
  "model": "opus",
  "statusLine": {
    "type": "command",
    "command": "bash \"$HOME/.claude/statusline.sh\" | head -1"
  }
}
`)
	d = door(t, e, doors.Limits)
	want := `+  "statusLine": {"type": "command", "command": "` + exe + ` hook statusline -- sh -c 'bash \"$HOME/.claude/statusline.sh\" | head -1'"}`
	if !strings.Contains(d.Preview, want) || !strings.Contains(d.Preview, `-    "command": "bash \"$HOME/.claude/statusline.sh\" | head -1"`) {
		t.Fatalf("the wrap:\n%s\nwant a line\n%s", d.Preview, want)
	}
	open(t, e, doors.Limits)
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), `"model": "opus"`) {
		t.Fatalf("other keys went: %s", b)
	}
	if d := door(t, e, doors.Limits); !d.Done {
		t.Fatalf("a wrapped status line is not done: %+v", d)
	}
	// Another build of matchblox taps it already.
	writeFile(t, path, `{"statusLine": {"type": "command", "command": "/opt/matchblox-dev hook statusline"}}`+"\n")
	if d := door(t, e, doors.Limits); !d.Done {
		t.Fatalf("matchblox-dev is wrapped again: %+v", d)
	}
}

// The safety net turns Claude Code's own compaction on at the stoker's line:
// it is the only thing that compacts inside a turn, and the stoker picks a
// boundary before it. Its diff keeps every other key and env variable.
func TestTheSafetyNetDoorTurnsAutoCompactOnAtTheLine(t *testing.T) {
	e := env(t)
	e.CompactAt = 80
	path := settingsPath(e)
	writeFile(t, path, `{
  "autoCompactEnabled": false,
  "model": "opus",
  "env": {"FOO": "1"}
}
`)
	d := door(t, e, doors.SafetyNet)
	if d.Done || !strings.Contains(d.Preview, `+  "autoCompactEnabled": true,`) || !strings.Contains(d.Preview, `"CLAUDE_AUTOCOMPACT_PCT_OVERRIDE": "80"`) || !strings.Contains(d.Why, "80%") {
		t.Fatalf("door %+v\n%s", d, d.Preview)
	}
	open(t, e, doors.SafetyNet)
	b, _ := os.ReadFile(path)
	var s struct {
		Auto  *bool             `json:"autoCompactEnabled"`
		Model string            `json:"model"`
		Env   map[string]string `json:"env"`
	}
	if err := json.Unmarshal(b, &s); err != nil || s.Auto == nil || !*s.Auto || s.Model != "opus" || s.Env["FOO"] != "1" || s.Env["CLAUDE_AUTOCOMPACT_PCT_OVERRIDE"] != "80" {
		t.Fatalf("settings %s (%v)", b, err)
	}
	if d := door(t, e, doors.SafetyNet); !d.Done {
		t.Fatalf("not done after it opened: %+v", d)
	}
	// A builder's own line stays: the net is there, at another percentage.
	writeFile(t, path, `{"env": {"CLAUDE_AUTOCOMPACT_PCT_OVERRIDE": "90"}}`+"\n")
	if d := door(t, e, doors.SafetyNet); !d.Done {
		t.Fatalf("a net at 90%% is not done: %+v", d)
	}
}

// installed is the settings an earlier matchblox wrote: every hook but the
// question tool's, next to the person's own PreToolUse hook.
const installed = `{
  "hooks": {
    "Notification": [{"hooks": [{"type": "command", "command": "/opt/mb/matchblox hook Notification"}]}],
    "Stop": [{"hooks": [{"type": "command", "command": "/opt/mb/matchblox hook Stop"}]}],
    "UserPromptSubmit": [{"hooks": [{"type": "command", "command": "/opt/mb/matchblox hook UserPromptSubmit"}]}],
    "SessionStart": [{"hooks": [{"type": "command", "command": "/opt/mb/matchblox hook SessionStart"}]}],
    "SessionEnd": [{"hooks": [{"type": "command", "command": "/opt/mb/matchblox hook SessionEnd"}]}],
    "PostToolUse": [{"hooks": [{"type": "command", "command": "/opt/mb/matchblox hook PostToolUse"}]}],
    "PreToolUse": [{"matcher": "Bash", "hooks": [{"type": "command", "command": "guard-bash"}]}]
  }
}
`

// TestAnInstallGainsTheQuestionHook: the hooks door of an earlier install
// asks to be opened again, and its diff adds only the PreToolUse hooks of
// the question tool and of Bash, beside the person's own.
func TestAnInstallGainsTheQuestionHook(t *testing.T) {
	e := env(t)
	p := settings(t, e, installed)
	d := door(t, e, doors.Hooks)
	if d.Done || d.Problem != "" {
		t.Fatalf("door = %+v, want one to open", d)
	}
	want := `+      {"matcher": "AskUserQuestion", "hooks": [{"type": "command", "command": "` + exe + ` hook PreToolUse"}]}`
	if !strings.Contains(d.Preview, want) || !strings.Contains(d.Preview, strings.Replace(want, "AskUserQuestion", "Bash", 1)) {
		t.Fatalf("the diff has no question or Bash hook:\n%s", d.Preview)
	}
	for line := range strings.SplitSeq(d.Preview, "\n") {
		if strings.HasPrefix(line, "+") && !strings.HasPrefix(line, "++") && strings.Contains(line, " hook ") && !strings.Contains(line, "hook PreToolUse") {
			t.Errorf("the diff adds another hook: %q", line)
		}
	}
	open(t, e, doors.Hooks)
	got := commands(t, p)
	if fmt.Sprint(got["PreToolUse"]) != "[guard-bash "+exe+" hook PreToolUse "+exe+" hook PreToolUse]" {
		t.Fatalf("PreToolUse = %q", got["PreToolUse"])
	}
	if d := door(t, e, doors.Hooks); !d.Done {
		t.Fatalf("door not done after open: %+v", d)
	}
}

// TestTheQuestionHookNeedsItsTool: a matchblox PreToolUse hook counts only
// for the tools its matcher takes: the question tool and Bash.
func TestTheQuestionHookNeedsItsTool(t *testing.T) {
	for matcher, done := range map[string]bool{
		"":                     true,
		"*":                    true,
		"AskUserQuestion":      false,
		"Bash|AskUserQuestion": true,
		"Ask.*|Bash":           true,
		"Bash":                 false,
		"AskUserQuestionX":     false,
		"(":                    false,
	} {
		e := env(t)
		body := `{"hooks": {"PreToolUse": [{"matcher": "` + matcher + `", "hooks": [{"type": "command", "command": "matchblox hook PreToolUse"}]}]}}`
		for _, ev := range Events {
			if ev != "PreToolUse" {
				body = strings.Replace(body, `{"hooks": {`, `{"hooks": {"`+ev+`": [{"hooks": [{"type": "command", "command": "matchblox hook `+ev+`"}]}], `, 1)
			}
		}
		settings(t, e, allowingHands(body))
		if d := door(t, e, doors.Hooks); d.Done != done {
			t.Errorf("matcher %q: done = %v, want %v", matcher, d.Done, done)
		}
	}
}

func TestPairDoorIsDoneWithAPhone(t *testing.T) {
	e := env(t)
	d := door(t, e, doors.Pair)
	if d.Done || d.Title != "Pair a device" || d.Why != "answer your agents from your phone" || d.Sum != "" || d.Term != nil {
		t.Fatalf("door = %+v", d)
	}
	writeFile(t, filepath.Join(e.Home, ".ssh", "authorized_keys"), "ssh-rsa AAAAother other@box\n")
	if door(t, e, doors.Pair).Done {
		t.Fatal("another key marked the door done")
	}
	if _, err := AuthorizeDevice(e.Home, exe, phoneKey, "Jordan’s iPhone"); err != nil {
		t.Fatal(err)
	}
	if !door(t, e, doors.Pair).Done {
		t.Fatal("a phone did not mark the door done")
	}
}

func TestPairDoorCloses(t *testing.T) {
	e := env(t)
	if err := Close(e, doors.Pair); err != nil {
		t.Fatal(err)
	}
	if !door(t, e, doors.Pair).Closed {
		t.Fatal("the door stayed open")
	}
}
