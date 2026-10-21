package push

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/blox-eng/matchblox/internal/limits"
	"github.com/blox-eng/matchblox/internal/sample"
)

// Numbers gives each session and each account one small number for life, so a
// payload names them by number and never by name. A number is never used
// again: it is not freed when its session ends. numbers.json keeps them.
type Numbers struct {
	dir string
	mu  sync.Mutex
	st  numState
}

type numState struct {
	NextSession int            `json:"next_session"`
	NextAccount int            `json:"next_account"`
	Sessions    map[string]int `json:"sessions"` // session id -> number
	Accounts    map[string]int `json:"accounts"` // provider NUL account -> number
}

func (s *numState) fix() {
	if s.Sessions == nil {
		s.Sessions = map[string]int{}
	}
	if s.Accounts == nil {
		s.Accounts = map[string]int{}
	}
	s.NextSession = max(s.NextSession, 1)
	s.NextAccount = max(s.NextAccount, 1)
	for _, n := range s.Sessions {
		s.NextSession = max(s.NextSession, n+1)
	}
	for _, n := range s.Accounts {
		s.NextAccount = max(s.NextAccount, n+1)
	}
}

// OpenNumbers reads numbers.json in dir. A missing or unreadable file is an
// empty set: numbers then start again at 1.
func OpenNumbers(dir string) (*Numbers, error) {
	n := &Numbers{dir: dir}
	b, err := os.ReadFile(filepath.Join(dir, "numbers.json")) //nolint:gosec // our own state dir
	if err == nil {
		if json.Unmarshal(b, &n.st) != nil {
			n.st = numState{}
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	n.st.fix()
	return n, nil
}

func accountKey(provider, account string) string { return provider + "\x00" + account }

// Session is the number of a session, given on first sight and kept.
func (n *Numbers) Session(sessionID string) int {
	n.mu.Lock()
	defer n.mu.Unlock()
	if v, ok := n.st.Sessions[sessionID]; ok {
		return v
	}
	v := n.st.NextSession
	n.st.NextSession++
	n.st.Sessions[sessionID] = v
	_ = n.save() // the number holds in memory even when the file cannot be written
	return v
}

// Account is the number of an account, given on first sight and kept.
func (n *Numbers) Account(provider, account string) int {
	n.mu.Lock()
	defer n.mu.Unlock()
	k := accountKey(provider, account)
	if v, ok := n.st.Accounts[k]; ok {
		return v
	}
	v := n.st.NextAccount
	n.st.NextAccount++
	n.st.Accounts[k] = v
	_ = n.save()
	return v
}

// SessionOf is the session id a number stands for.
func (n *Numbers) SessionOf(num int) (string, bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	for id, v := range n.st.Sessions {
		if v == num {
			return id, true
		}
	}
	return "", false
}

// Names say what each given session and account is called now, by number:
// the session's name as the console shows it, and the account's provider and
// label. A number not in the given lists has no name.
func (n *Numbers) Names(sessions []sample.Session, accounts []limits.Account) (map[int]string, map[int]string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	ss := map[int]string{}
	for _, s := range sessions {
		if v, ok := n.st.Sessions[s.SessionID]; ok && s.SessionID != "" {
			ss[v] = s.Name
		}
	}
	as := map[int]string{}
	for _, a := range accounts {
		if v, ok := n.st.Accounts[accountKey(a.Provider, a.Account)]; ok {
			as[v] = strings.TrimSpace(a.Provider + " " + a.Account)
		}
	}
	return ss, as
}

// save writes numbers.json whole, through a temporary file and a rename, as
// the stoker does. The caller holds mu.
func (n *Numbers) save() error {
	b, err := json.Marshal(n.st)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(n.dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(n.dir, ".numbers-*.json")
	if err != nil {
		return err
	}
	if _, err := tmp.Write(b); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), filepath.Join(n.dir, "numbers.json")); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}
