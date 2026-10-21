package sample

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/blox-eng/matchblox/internal/said"
)

// agentEnv is where one agent process keeps its files, and how it signs in,
// read once from its environment. Each process can use another config
// directory, so two sessions can run under two accounts. A process with
// another HOME keeps its files there: unless its environment names a
// directory, that directory is "" and nothing is read for it.
type agentEnv struct {
	claudeDir  string // CLAUDE_CONFIG_DIR, else ~/.claude
	claudeJSON string // .claude.json next to it: in CLAUDE_CONFIG_DIR, else in the home
	codexHome  string // CODEX_HOME, else ~/.codex
	cursorDir  string // CURSOR_CONFIG_DIR, else ~/.cursor
	dataHome   string // XDG_DATA_HOME, else ~/.local/share
	cacheHome  string // XDG_CACHE_HOME, else ~/.cache
	cloud      string // bedrock or vertex: Claude Code signs in through the cloud
	apiKey     bool   // an API key is set in the environment (its value is never kept)
}

func (s *Sampler) envOf(pid int, key procKey) agentEnv {
	if e, ok := s.envs[key]; ok {
		return e
	}
	get := func(k string) string {
		v, _ := s.FS.Environ(pid, k)
		return v
	}
	home := s.Home
	if h := get("HOME"); h != "" && filepath.Clean(h) != filepath.Clean(s.homeAs()) {
		home = ""
	}
	under := func(parts ...string) string {
		if home == "" {
			return ""
		}
		return filepath.Join(append([]string{home}, parts...)...)
	}
	abs := func(v, def string) string {
		if filepath.IsAbs(v) {
			return filepath.Clean(v)
		}
		return def
	}
	e := agentEnv{
		claudeDir:  under(".claude"),
		claudeJSON: under(".claude.json"),
		codexHome:  abs(get("CODEX_HOME"), under(".codex")),
		cursorDir:  abs(get("CURSOR_CONFIG_DIR"), under(".cursor")),
		dataHome:   abs(get("XDG_DATA_HOME"), under(".local", "share")),
		cacheHome:  abs(get("XDG_CACHE_HOME"), under(".cache")),
	}
	if d := abs(get("CLAUDE_CONFIG_DIR"), ""); d != "" {
		e.claudeDir, e.claudeJSON = d, filepath.Join(d, ".claude.json")
	}
	switch {
	case truthy(get("CLAUDE_CODE_USE_BEDROCK")):
		e.cloud = "bedrock"
	case truthy(get("CLAUDE_CODE_USE_VERTEX")):
		e.cloud = "vertex"
	}
	for _, k := range []string{"ANTHROPIC_API_KEY", "ANTHROPIC_AUTH_TOKEN", "OPENAI_API_KEY", "CODEX_API_KEY"} {
		if get(k) != "" {
			e.apiKey = true
		}
	}
	s.envs[key] = e
	return e
}

// joinIn joins under dir, and stays "" when dir is not known.
func joinIn(dir string, parts ...string) string {
	if dir == "" {
		return ""
	}
	return filepath.Join(append([]string{dir}, parts...)...)
}

func truthy(v string) bool { return v == "1" || strings.EqualFold(v, "true") }

// label makes text from an agent's file safe and short for one line: no
// control keys, no new lines.
func label(parts ...string) string {
	var keep []string
	for _, p := range parts {
		if p = strings.Join(strings.Fields(said.Clean(p)), " "); p != "" {
			keep = append(keep, p)
		}
	}
	return strings.Join(keep, " · ")
}

// accounts caches what it read from each file on the file's size and mtime.
type accounts struct {
	labels map[string]cached[string]
	types  map[string]cached[map[string]string] // OpenCode: provider -> auth type
}

func newAccounts() *accounts {
	return &accounts{labels: map[string]cached[string]{}, types: map[string]cached[map[string]string]{}}
}

func readCached[T any](m map[string]cached[T], path string, parse func([]byte) T) T {
	var zero T
	if path == "" { // a directory that is not known
		return zero
	}
	key, ok := statKey(path)
	if !ok {
		return zero
	}
	if c, ok := m[path]; ok && c.key == key {
		return c.val
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return zero
	}
	v := parse(b)
	m[path] = cached[T]{key, v}
	return v
}

var claudePlans = map[string]string{"claude_max": "Max", "claude_pro": "Pro", "claude_team": "Team", "claude_enterprise": "Enterprise"}

// claudeProvider serves a Claude Code session: a cloud, else Anthropic.
func claudeProvider(e agentEnv) string {
	if e.cloud != "" {
		return e.cloud
	}
	return "anthropic"
}

// claudeAccount reads only the login's email and plan from .claude.json.
// The file holds other secrets; they are not decoded. A cloud has no login.
func (a *accounts) claudeAccount(e agentEnv) string {
	if e.cloud != "" || e.claudeJSON == "" {
		return ""
	}
	v := readCached(a.labels, e.claudeJSON, func(b []byte) string {
		var f struct {
			OAuth *struct {
				Email   string `json:"emailAddress"`
				OrgType string `json:"organizationType"`
			} `json:"oauthAccount"`
		}
		if json.Unmarshal(b, &f) != nil || f.OAuth == nil || f.OAuth.Email == "" {
			return ""
		}
		return label(f.OAuth.Email, claudePlans[f.OAuth.OrgType])
	})
	if v == "" && e.apiKey {
		return "API key"
	}
	return v
}

var codexPlans = map[string]string{"free": "Free", "go": "Go", "plus": "Plus", "pro": "Pro", "team": "Team", "business": "Business", "enterprise": "Enterprise", "edu": "Edu"}

// codexAccount reads auth.json: a ChatGPT login shows the email and plan
// from the claims of its id token (decoded, never verified or kept); an API
// key shows "API key". No token or key leaves this function.
func (a *accounts) codexAccount(e agentEnv) string {
	if e.codexHome == "" { // another home: its auth.json decides, not its environment
		return ""
	}
	v := readCached(a.labels, joinIn(e.codexHome, "auth.json"), func(b []byte) string {
		var f struct {
			Mode   string          `json:"auth_mode"`
			Key    json.RawMessage `json:"OPENAI_API_KEY"`
			Tokens *struct {
				ID string `json:"id_token"`
			} `json:"tokens"`
		}
		if json.Unmarshal(b, &f) != nil {
			return ""
		}
		if f.Mode != "apikey" && f.Tokens != nil {
			if email, plan := idClaims(f.Tokens.ID); email != "" {
				return label(email, codexPlans[plan])
			}
		}
		if f.Mode == "apikey" || (len(f.Key) > 0 && string(f.Key) != "null" && string(f.Key) != `""`) {
			return "API key"
		}
		return ""
	})
	if v == "" && e.apiKey {
		return "API key"
	}
	return v
}

// idClaims returns the email and the ChatGPT plan from a JWT's payload.
func idClaims(jwt string) (email, plan string) {
	parts := strings.Split(jwt, ".")
	if len(parts) != 3 {
		return "", ""
	}
	b, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return "", ""
	}
	var c struct {
		Email string `json:"email"`
		Auth  struct {
			Plan string `json:"chatgpt_plan_type"`
		} `json:"https://api.openai.com/auth"`
	}
	if json.Unmarshal(b, &c) != nil {
		return "", ""
	}
	return c.Email, c.Auth.Plan
}

// cursorAccount reads the login Cursor's CLI keeps in its cli-config.json:
// the email and the team, nothing else. Its tokens are in the Keychain.
func (a *accounts) cursorAccount(e agentEnv) string {
	return readCached(a.labels, joinIn(e.cursorDir, "cli-config.json"), func(b []byte) string {
		var f struct {
			Auth *struct {
				Email string `json:"email"`
				Team  string `json:"teamName"`
			} `json:"authInfo"`
		}
		if json.Unmarshal(b, &f) != nil || f.Auth == nil || f.Auth.Email == "" {
			return ""
		}
		return label(f.Auth.Email, f.Auth.Team)
	})
}

var opencodeAuth = map[string]string{"api": "API key", "oauth": "login", "wellknown": "login"}

// opencodeAccount tells how OpenCode signs in to the provider of the
// session's model: only the type of its auth.json entry is read.
func (a *accounts) opencodeAccount(e agentEnv, provider string) string {
	if provider == "" {
		return ""
	}
	types := readCached(a.types, joinIn(e.dataHome, "opencode", "auth.json"), func(b []byte) map[string]string {
		var f map[string]struct {
			Type string `json:"type"`
		}
		if json.Unmarshal(b, &f) != nil {
			return nil
		}
		m := map[string]string{}
		for p, v := range f {
			m[p] = v.Type
		}
		return m
	})
	return opencodeAuth[types[provider]]
}
