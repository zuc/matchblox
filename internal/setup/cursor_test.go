package setup

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/blox-eng/matchblox/internal/doors"
	"github.com/blox-eng/matchblox/internal/hooks"
)

// cursorEnv is env with Cursor's CLI installed.
func cursorEnv(t *testing.T) Env {
	t.Helper()
	e := env(t)
	look := e.LookPath
	e.LookPath = func(name string) (string, error) {
		if name == "cursor-agent" {
			return "/home/u/.local/bin/cursor-agent", nil
		}
		return look(name)
	}
	return e
}

func cursorHooks(t *testing.T, e Env, body string) string {
	t.Helper()
	p := filepath.Join(e.Home, ".cursor", "hooks.json")
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

// cursorFile is a hooks.json as Cursor reads it.
type cursorFile struct {
	Version *int `json:"version"`
	Hooks   map[string][]struct {
		Command string `json:"command"`
	} `json:"hooks"`
}

func readCursorHooks(t *testing.T, p string) cursorFile {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var f cursorFile
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatalf("%s: %v", b, err)
	}
	return f
}

func TestCursorHooksDoorWritesTheSixEvents(t *testing.T) {
	e := cursorEnv(t)
	open(t, e, doors.CursorHooks)
	f := readCursorHooks(t, filepath.Join(e.Home, ".cursor", "hooks.json"))
	if f.Version == nil || *f.Version != 1 {
		t.Errorf("version %v, want 1", f.Version)
	}
	var events []string
	for ev, hs := range f.Hooks {
		events = append(events, ev)
		if len(hs) != 1 || hs[0].Command != exe+" hook --cursor" {
			t.Errorf("%s: %+v, want one %q", ev, hs, exe+" hook --cursor")
		}
	}
	slices.Sort(events)
	want := slices.Sorted(slices.Values(hooks.CursorEvents))
	if !slices.Equal(events, want) {
		t.Errorf("events %q, want exactly %q", events, want)
	}
}

func TestCursorHooksKeepTheirOwnHooks(t *testing.T) {
	e := cursorEnv(t)
	p := cursorHooks(t, e, `{
  "version": 1,
  "hooks": {
    "afterFileEdit": [{"command": ".cursor/hooks/format.sh"}],
    "stop": [{"command": "say done"}]
  }
}
`)
	open(t, e, doors.CursorHooks)
	b, _ := os.ReadFile(p)
	for _, line := range []string{`"afterFileEdit": [{"command": ".cursor/hooks/format.sh"}]`, `{"command": "say done"}`} {
		if !strings.Contains(string(b), line) {
			t.Errorf("lost %s:\n%s", line, b)
		}
	}
	stop := readCursorHooks(t, p).Hooks["stop"]
	if len(stop) != 2 || stop[0].Command != "say done" || stop[1].Command != exe+" hook --cursor" {
		t.Errorf("stop = %+v, want say done, then matchblox", stop)
	}
}

func TestCursorHooksAreAddedOnce(t *testing.T) {
	e := cursorEnv(t)
	p := filepath.Join(e.Home, ".cursor", "hooks.json")
	open(t, e, doors.CursorHooks)
	before, _ := os.ReadFile(p)
	if d := door(t, e, doors.CursorHooks); !d.Done {
		t.Errorf("door after open: %+v, want done", d)
	}
	after, _ := os.ReadFile(p)
	if string(after) != string(before) {
		t.Errorf("the file changed:\n%s", after)
	}
	// matchblox from another path counts.
	other := strings.ReplaceAll(string(before), exe, "/usr/local/bin/matchblox")
	cursorHooks(t, e, other)
	if d := door(t, e, doors.CursorHooks); !d.Done {
		t.Errorf("matchblox at another path: %+v, want done", d)
	}
}

func TestCursorHooksSetTheVersion(t *testing.T) {
	e := cursorEnv(t)
	p := cursorHooks(t, e, `{"hooks": {}}`)
	open(t, e, doors.CursorHooks)
	if f := readCursorHooks(t, p); f.Version == nil || *f.Version != 1 {
		t.Errorf("version %v, want 1", f.Version)
	}
}

func TestCursorHooksDoorOnlyWhereCursorIs(t *testing.T) {
	if slices.Contains(ids(Doors(env(t))), doors.CursorHooks) {
		t.Error("no Cursor, yet its door")
	}
	if !slices.Contains(ids(Doors(cursorEnv(t))), doors.CursorHooks) {
		t.Error("cursor-agent on PATH, no door")
	}
	e := env(t)
	if err := os.MkdirAll(filepath.Join(e.Home, ".cursor"), 0o700); err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(ids(Doors(e)), doors.CursorHooks) {
		t.Error("~/.cursor exists, no door")
	}
}

func TestCursorHooksOnBrokenJSONSayHowToFix(t *testing.T) {
	e := cursorEnv(t)
	cursorHooks(t, e, `{"hooks": `)
	d := door(t, e, doors.CursorHooks)
	if !strings.Contains(d.Problem, "~/.cursor/hooks.json") {
		t.Errorf("problem %q, want it to name ~/.cursor/hooks.json", d.Problem)
	}
}

func cursorConfig(t *testing.T, e Env, body string) string {
	t.Helper()
	p := filepath.Join(e.Home, ".cursor", "cli-config.json")
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func statusCommand(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	var f struct {
		StatusLine struct {
			Type    string `json:"type"`
			Command string `json:"command"`
			Padding int    `json:"padding"`
		} `json:"statusLine"`
	}
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatalf("%s: %v", b, err)
	}
	if f.StatusLine.Type != "command" {
		t.Errorf("type %q", f.StatusLine.Type)
	}
	return f.StatusLine.Command
}

// TestCursorContextDoorTapsCursorsStatusLine: the tap goes into Cursor's
// status line, and every other key of its config keeps its bytes.
func TestCursorContextDoorTapsCursorsStatusLine(t *testing.T) {
	e := cursorEnv(t)
	p := cursorConfig(t, e, "{\n  \"version\": 1,\n  \"editor\": {\n    \"vimMode\": false\n  }\n}\n")
	open(t, e, doors.CursorLimits)
	if got := statusCommand(t, p); got != exe+" hook statusline --cursor" {
		t.Errorf("command %q", got)
	}
	b, _ := os.ReadFile(p)
	if !strings.Contains(string(b), "\"editor\": {\n    \"vimMode\": false\n  }") {
		t.Errorf("the other keys moved:\n%s", b)
	}
	if d := door(t, e, doors.CursorLimits); !d.Done {
		t.Errorf("door after open: %+v, want done", d)
	}
}

// TestCursorContextDoorKeepsYourStatusLine: a status line there keeps
// running inside the tap, with its other settings.
func TestCursorContextDoorKeepsYourStatusLine(t *testing.T) {
	e := cursorEnv(t)
	p := cursorConfig(t, e, `{"statusLine": {"type": "command", "command": "~/.cursor/statusline.sh", "padding": 2}}`)
	open(t, e, doors.CursorLimits)
	if got := statusCommand(t, p); got != exe+" hook statusline --cursor -- sh -c '~/.cursor/statusline.sh'" {
		t.Errorf("command %q", got)
	}
	b, _ := os.ReadFile(p)
	if !strings.Contains(string(b), `"padding": 2`) {
		t.Errorf("padding lost:\n%s", b)
	}
}

func TestCursorContextDoorReadsCursorsConfigDir(t *testing.T) {
	e := cursorEnv(t)
	dir := filepath.Join(e.Home, "elsewhere")
	e.Getenv = func(k string) string {
		if k == "CURSOR_CONFIG_DIR" {
			return dir
		}
		return ""
	}
	if d := door(t, e, doors.CursorLimits); d.Path != filepath.Join(dir, "cli-config.json") {
		t.Errorf("path %q, want the one in CURSOR_CONFIG_DIR", d.Path)
	}
}
