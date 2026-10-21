package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func write(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "tui.toml")
	os.WriteFile(path, []byte(body), 0o644)
	return path
}

func TestMissingFileIsDefaults(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "none.toml"))
	if err != nil || c.Interval.Duration != 2*time.Second || c.Orphans.CPUOver != 80 {
		t.Fatalf("%+v %v", c, err)
	}
}

func TestOverlay(t *testing.T) {
	c, err := Load(write(t, `
interval = "5s"
[alerts]
temp_over_c = 85
load1_over = 40
load1_for = "3m"
power_limits_w = [100, 150]
[orphans]
cpu_over = 70
[[groups]]
name = "CI"
container = "^ci-"
[sessions.windows]
"model-x" = 1000000
[hooks]
alert = ["notify-send", "console"]
`))
	if err != nil {
		t.Fatal(err)
	}
	if c.Interval.Duration != 5*time.Second || c.Alerts.TempOverC != 85 || c.Alerts.Load1For.Duration != 3*time.Minute ||
		len(c.Alerts.PowerLimitsW) != 2 || c.Orphans.CPUOver != 70 || c.Sessions.Windows["model-x"] != 1_000_000 ||
		len(c.Hooks.Alert) != 2 {
		t.Fatalf("%+v", c)
	}
	// Unset values keep their defaults.
	if c.Orphans.For.Duration != 30*time.Second || c.Sessions.CompactAt != 85 {
		t.Fatalf("defaults lost: %+v", c)
	}
	if !c.Groups[0].Match("ci-runner-2") || c.Groups[0].Match("app-ci-") {
		t.Fatal("group pattern")
	}
}

func TestBadInput(t *testing.T) {
	if _, err := Load(write(t, "[[groups]]\nname = \"x\"\ncontainer = \"(\"\n")); err == nil {
		t.Error("bad regexp accepted")
	}
	if _, err := Load(write(t, "interval = \"soon\"\n")); err == nil {
		t.Error("bad duration accepted")
	}
}

func TestExampleLoads(t *testing.T) {
	c, err := Load("../../config.example.toml")
	if err != nil {
		t.Fatal(err)
	}
	if d := Default(); c.Interval != d.Interval || c.Sessions.CompactAt != d.Sessions.CompactAt || c.Sessions.ProgressPrompt != d.Sessions.ProgressPrompt || c.Orphans.CPUOver != d.Orphans.CPUOver {
		t.Fatalf("the example must state the defaults: %+v", c)
	}
	if d := Default(); c.Stoker != d.Stoker || d.Stoker.Agent != "claude" || d.Stoker.MaxSteps != 60 {
		t.Fatalf("the example must state the stoker defaults: %+v", c.Stoker)
	}
	if d := Default(); c.Push != d.Push || d.Push.Relay != "https://matchblox.sh" || d.Push.Off {
		t.Fatalf("the example must state the push defaults: %+v", c.Push)
	}
}

// TestPushOff: [push] off turns the sender off; relay names another relay.
func TestPushOff(t *testing.T) {
	c, err := Load(write(t, "[push]\noff = true\nrelay = \"http://127.0.0.1:9\"\n"))
	if err != nil || !c.Push.Off || c.Push.Relay != "http://127.0.0.1:9" {
		t.Fatalf("push %+v, %v", c.Push, err)
	}
}

// TestThePushRelayIsHTTPS: the relay gets device tokens and grants, so only
// https, or plain http to this machine.
func TestThePushRelayIsHTTPS(t *testing.T) {
	for relay, ok := range map[string]bool{
		"https://relay.example":   true,
		"http://127.0.0.1:9":      true,
		"http://localhost:8787":   true,
		"http://[::1]:9":          true,
		"http://relay.example":    false,
		"http://127.0.0.1.nip.io": false,
		"ftp://relay.example":     false,
		"relay.example":           false,
	} {
		_, err := Load(write(t, "[push]\nrelay = \""+relay+"\"\n"))
		if (err == nil) != ok {
			t.Errorf("relay %s: err %v, want ok %v", relay, err, ok)
		}
	}
}

// TestTheStokersPaneKeyIsGone: matchblox starts the stoker agent
// itself, so [stoker] pane is no longer read. A config that still has it
// loads, says so (status warns), and starts the default agent.
func TestTheStokersPaneKeyIsGone(t *testing.T) {
	c, err := Load(write(t, "[stoker]\npane = \"stoker\"\n"))
	if err != nil || !c.Stoker.PaneSet || c.Stoker.Agent != "claude" {
		t.Fatalf("pane set %v, agent %q, %v", c.Stoker.PaneSet, c.Stoker.Agent, err)
	}
	if c, err := Load(write(t, "[stoker]\nagent = \"codex\"\n")); err != nil || c.Stoker.PaneSet || c.Stoker.Agent != "codex" {
		t.Fatalf("pane set %v, agent %q, %v", c.Stoker.PaneSet, c.Stoker.Agent, err)
	}
}

// TestAgentsSetTellsAConfigWithoutAgents: a config written before agents
// were found has no agents key; the console then finds them at start.
func TestAgentsSetTellsAConfigWithoutAgents(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "old.toml")
	os.WriteFile(old, []byte("[alerts]\nload1_over = 8\n"), 0o600)
	if c, err := Load(old); err != nil || c.AgentsSet {
		t.Fatalf("old config: set %v, %v", c.AgentsSet, err)
	}
	set := filepath.Join(dir, "set.toml")
	os.WriteFile(set, []byte("agents = [\"codex\"]\n"), 0o600)
	if c, _ := Load(set); !c.AgentsSet || strings.Join(c.Agents, ",") != "codex" {
		t.Fatalf("set config: %+v", c.Agents)
	}
}

// TestQuietHoursShapeTheForecast: the forecast counts the hours the builder
// does not usually work as quiet; the defaults are the night and the
// weekend, and a value that does not parse is an error, not a guess.
func TestQuietHoursShapeTheForecast(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "none.toml"))
	if err != nil {
		t.Fatal(err)
	}
	if c.Limits.QuietHours != "23:00-08:00" || strings.Join(c.Limits.QuietDays, ",") != "Sat,Sun" {
		t.Fatalf("defaults: %+v", c.Limits)
	}
	sat := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	if !c.Limits.Quiet.Quiet(sat) {
		t.Fatal("the default quiet days were not parsed")
	}
	c, err = Load(write(t, "[limits]\nquiet_hours = \"01:00-07:00\"\nquiet_days = []\n"))
	if err != nil || c.Limits.Quiet.Quiet(sat) || !c.Limits.Quiet.Quiet(sat.Add(-9*time.Hour)) {
		t.Fatalf("overlay: %+v %v", c.Limits, err)
	}
	if _, err := Load(write(t, "[limits]\nquiet_hours = \"late\"\n")); err == nil || !strings.Contains(err.Error(), "limits.quiet_hours") {
		t.Fatalf("bad quiet hours: %v", err)
	}
}

// TestCursorUsageIsOffUnlessAsked: reading Cursor's plan usage reads its
// login token: only a config that says so turns it on.
func TestCursorUsageIsOffUnlessAsked(t *testing.T) {
	c, err := Load(filepath.Join(t.TempDir(), "none.toml"))
	if err != nil || c.Limits.CursorUsage {
		t.Fatalf("default: %v %v, want off", c.Limits.CursorUsage, err)
	}
	if c, err = Load(write(t, "[limits]\ncursor_usage = true\n")); err != nil || !c.Limits.CursorUsage {
		t.Fatalf("on: %v %v", c.Limits.CursorUsage, err)
	}
}
