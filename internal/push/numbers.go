package push

import (
	"encoding/json"
	"fmt"
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
	err error // the last save's, nil after a good one
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

// OpenNumbers reads numbers.json in dir. A missing file is an empty set. A
// file that cannot be read or does not parse is an error: starting again at 1
// would give old numbers, and their names on the phone, to new sessions.
func OpenNumbers(dir string) (*Numbers, error) {
	n := &Numbers{dir: dir}
	b, err := os.ReadFile(filepath.Join(dir, "numbers.json")) //nolint:gosec // our own state dir
	switch {
	case err == nil:
		if err := json.Unmarshal(b, &n.st); err != nil {
			return nil, fmt.Errorf("numbers.json: %w", err)
		}
	case !os.IsNotExist(err):
		return nil, err
	}
	n.st.fix()
	return n, nil
}

func accountKey(provider, account string) string { return provider + "\x00" + account }

// Session is the number of a session, given on first sight and kept. It is 0
// when a new number cannot be written down: a number kept in memory only
// would go to another session after a restart. Err says why.
func (n *Numbers) Session(sessionID string) int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.session(sessionID)
}

func (n *Numbers) session(sessionID string) int {
	if v, ok := n.st.Sessions[sessionID]; ok {
		return v
	}
	v := n.st.NextSession
	n.st.NextSession++
	n.st.Sessions[sessionID] = v
	if n.keep() != nil {
		delete(n.st.Sessions, sessionID)
		n.st.NextSession--
		return 0
	}
	return v
}

// Account is the number of an account, given on first sight and kept; 0 as
// for Session.
func (n *Numbers) Account(provider, account string) int {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.account(provider, account)
}

func (n *Numbers) account(provider, account string) int {
	k := accountKey(provider, account)
	if v, ok := n.st.Accounts[k]; ok {
		return v
	}
	v := n.st.NextAccount
	n.st.NextAccount++
	n.st.Accounts[k] = v
	if n.keep() != nil {
		delete(n.st.Accounts, k)
		n.st.NextAccount--
		return 0
	}
	return v
}

// Err is why the last new number could not be written down, or nil.
func (n *Numbers) Err() error {
	n.mu.Lock()
	defer n.mu.Unlock()
	return n.err
}

// keep saves and notes the result. The caller holds mu.
func (n *Numbers) keep() error {
	n.err = n.save()
	return n.err
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
// label. Each current session and account gets its number here if it had
// none, so the phone learns every name at one register. A number not in the
// given lists has no name.
func (n *Numbers) Names(sessions []sample.Session, accounts []limits.Account) (map[int]string, map[int]string) {
	n.mu.Lock()
	defer n.mu.Unlock()
	ss := map[int]string{}
	for _, s := range sessions {
		if s.SessionID == "" {
			continue
		}
		if v := n.session(s.SessionID); v != 0 {
			ss[v] = s.Name
		}
	}
	as := map[int]string{}
	for _, a := range accounts {
		if v := n.account(a.Provider, a.Account); v != 0 {
			as[v] = strings.TrimSpace(a.Provider + " " + a.Account)
		}
	}
	return ss, as
}

// save writes numbers.json whole. The caller holds mu.
func (n *Numbers) save() error {
	b, err := json.Marshal(n.st)
	if err != nil {
		return err
	}
	return writeAtomic(n.dir, "numbers.json", b)
}

// writeAtomic writes name in dir whole, through a temporary file and a
// rename, as the stoker does.
func writeAtomic(dir, name string, b []byte) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, "."+strings.TrimSuffix(name, ".json")+"-*.json")
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
	if err := os.Rename(tmp.Name(), filepath.Join(dir, name)); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}
