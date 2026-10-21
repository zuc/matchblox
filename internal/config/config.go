// Package config loads the per-machine goals. The file is local and never
// part of the repository: thresholds describe one machine, not the tool.
package config

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/blox-eng/matchblox/internal/limits"
)

// Duration reads "30s", "2m", "24h" from TOML.
type Duration struct{ time.Duration }

func (d *Duration) UnmarshalText(b []byte) error {
	v, err := time.ParseDuration(string(b))
	d.Duration = v
	return err
}

type Config struct {
	Interval Duration `toml:"interval"`
	Agents   []string `toml:"agents"`
	// AgentsSet: the file has an agents key. A config written before the
	// first run found the agents has none; the console finds them then.
	AgentsSet     bool     `toml:"-"`
	LatencyTarget string   `toml:"latency_target"`
	Sessions      Sessions `toml:"sessions"`
	Alerts        Alerts   `toml:"alerts"`
	Orphans       Orphans  `toml:"orphans"`
	Groups        []Group  `toml:"groups"`
	Git           Git      `toml:"git"`
	History       History  `toml:"history"`
	Hooks         Hooks    `toml:"hooks"`
	Stoker        Stoker   `toml:"stoker"`
	Limits        Limits   `toml:"limits"`
	Ships         Ships    `toml:"ships"`
	Push          Push     `toml:"push"`
}

// Push sends the buzz and the island to the phones through the relay.
type Push struct {
	Relay string `toml:"relay"` // the relay's base URL
	// Off: no sender, and the host never opens a connection to the relay.
	Off bool `toml:"off"`
}

// Ships follows the pull requests sessions drive through CI to live.
type Ships struct {
	// Deploy names the workflows that deploy a repository, by "owner/name".
	// A repository not named is live when every run on its merge commit is.
	Deploy map[string][]string `toml:"deploy"`
}

// Limits shape the forecast of each account's week.
type Limits struct {
	QuietHours string   `toml:"quiet_hours"` // "23:00-08:00": hours you do not usually work
	QuietDays  []string `toml:"quiet_days"`  // ["Sat", "Sun"]
	// Quiet is QuietHours and QuietDays, read by Load.
	Quiet limits.Quiet `toml:"-"`
}

// Stoker compacts full sessions while the builder is away.
type Stoker struct {
	NightEnds string `toml:"night_ends"` // "07:00": n runs the stoker until then
	// Agent is the command matchblox starts the stoker agent with, on its
	// own tmux server: claude, codex or opencode.
	Agent string `toml:"agent"`
	// PaneSet: the file still has the pane key of before matchblox started
	// the agent itself. It is ignored, and status says so.
	PaneSet bool `toml:"-"`
	// MaxSteps is how many messages the stoker agent may send in one run.
	MaxSteps int `toml:"max_steps"`
}

// Hooks run a command when something happens. The command is an argv list,
// never a shell string; the event's title and evidence are appended as two
// more arguments.
type Hooks struct {
	Alert []string `toml:"alert"` // on each new alert, e.g. ["notify-send", "matchblox"]
}

type Sessions struct {
	CompactAt float64 `toml:"compact_at"`
	// CompactTokens fills a session at this many context tokens, whichever
	// comes first with CompactAt, when the stoker has no pace for it (no
	// progress bar): a 1M window rarely reaches 85%. 0: off.
	CompactTokens int            `toml:"compact_tokens"`
	ClearIdle     Duration       `toml:"clear_idle"`
	ClearMinPct   float64        `toml:"clear_min_pct"`
	ClearStale    Duration       `toml:"clear_stale"`
	Windows       map[string]int `toml:"windows"` // model prefix -> context tokens
	// ProgressPrompt asks each new agent session for a progress bar at the
	// end of each reply, which the console shows.
	ProgressPrompt bool `toml:"progress_prompt"`
}

// Alerts: a zero threshold is off. "For" durations make an alert wait until
// the condition has held that long, so a spike does not page anyone.
type Alerts struct {
	TempOverC               float64  `toml:"temp_over_c"`
	Load1Over               float64  `toml:"load1_over"`
	Load1For                Duration `toml:"load1_for"`
	PSICPUOver              float64  `toml:"psi_cpu_over"`
	PSICPUFor               Duration `toml:"psi_cpu_for"`
	MemAvailableUnderGB     float64  `toml:"mem_available_under_gb"`
	SwapOverGB              float64  `toml:"swap_over_gb"`
	SwapWhenAvailableOverGB float64  `toml:"swap_when_available_over_gb"`
	// PowerLimitsW are the expected package limits in watts, long term then
	// short term. Empty: alert when a limit changes from its value at startup.
	PowerLimitsW []float64 `toml:"power_limits_w"`
}

// Orphans are processes re-parented to init or a user service manager that
// keep burning CPU: leaked test fixtures, forgotten loops.
type Orphans struct {
	CPUOver  float64  `toml:"cpu_over"` // percent of one core
	For      Duration `toml:"for"`
	MinAge   Duration `toml:"min_age"`
	Commands []string `toml:"commands"` // empty: any command
}

// Group sums the CPU of containers whose name matches a pattern.
type Group struct {
	Name      string `toml:"name"`
	Container string `toml:"container"` // regular expression on the container name
	re        *regexp.Regexp
}

func (g Group) Match(name string) bool { return g.re != nil && g.re.MatchString(name) }

type Git struct {
	Repos    []string `toml:"repos"` // checkouts to watch besides the ones sessions use
	Interval Duration `toml:"interval"`
	Main     string   `toml:"main"`   // default branch name; empty: what the remote's HEAD names
	Remote   string   `toml:"remote"` // remote to compare against
}

type History struct {
	Log    string `toml:"log"`    // append-only CSV the console writes; "-" disables
	Source string `toml:"source"` // extra CSV with a ts column to chart (another logger)
}

func Default() Config {
	return Config{
		Interval: Duration{2 * time.Second},
		Agents:   []string{"claude"},
		Sessions: Sessions{
			CompactAt:      85,
			CompactTokens:  350_000,
			ClearIdle:      Duration{30 * time.Minute},
			ClearMinPct:    20,
			ClearStale:     Duration{24 * time.Hour},
			ProgressPrompt: true,
		},
		Alerts: Alerts{
			TempOverC: 95,
			Load1For:  Duration{2 * time.Minute},
			PSICPUFor: Duration{2 * time.Minute},
		},
		Orphans: Orphans{
			CPUOver:  80,
			For:      Duration{30 * time.Second},
			MinAge:   Duration{2 * time.Minute},
			Commands: []string{"sh", "bash", "zsh", "dash", "fish"},
		},
		Git:    Git{Interval: Duration{5 * time.Minute}, Remote: "origin"},
		Stoker: Stoker{NightEnds: "07:00", MaxSteps: 60, Agent: "claude"},
		Limits: Limits{QuietHours: "23:00-08:00", QuietDays: []string{"Sat", "Sun"}, Quiet: defaultQuiet},
		Push:   Push{Relay: "https://matchblox.sh"},
	}
}

var defaultQuiet, _ = limits.ParseQuiet("23:00-08:00", []string{"Sat", "Sun"})

// Path is $XDG_CONFIG_HOME/matchblox/config.toml, defaulting to ~/.config.
func Path() string {
	dir := os.Getenv("XDG_CONFIG_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "matchblox", "config.toml")
}

// WaysPath is the builder's ways file, next to config.toml.
func WaysPath() string { return filepath.Join(filepath.Dir(Path()), "ways.md") }

// Load overlays the file on the defaults. A missing file is not an error.
func Load(path string) (Config, error) {
	c := Default()
	md, err := toml.DecodeFile(path, &c)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return c, err
	}
	c.AgentsSet = md.IsDefined("agents")
	c.Stoker.PaneSet = md.IsDefined("stoker", "pane")
	if c.Limits.Quiet, err = limits.ParseQuiet(c.Limits.QuietHours, c.Limits.QuietDays); err != nil {
		return c, errors.New("limits.quiet_hours or quiet_days: " + err.Error())
	}
	for i := range c.Groups {
		re, err := regexp.Compile(c.Groups[i].Container)
		if err != nil {
			return c, errors.New("groups." + c.Groups[i].Name + ": " + err.Error())
		}
		c.Groups[i].re = re
	}
	return c, nil
}
