// Package holds keeps the sessions nothing may type into: parked by the
// builder, or held by the stoker.
package holds

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const (
	ByYou    = "you"
	ByStoker = "stoker"
)

// GoneFor is how long a hold outlives its session's absence from the snapshot.
const GoneFor = 24 * time.Hour

// Hold follows the session, not the pane: a new agent in the pane is free.
// An explicit switch keeps it on the pane instead (Rebind).
type Hold struct {
	SessionID string    `json:"session_id"`
	Pane      string    `json:"pane"`
	Name      string    `json:"name"`
	By        string    `json:"by"` // you | stoker
	Why       string    `json:"why,omitempty"`
	Rebind    bool      `json:"rebind,omitempty"` // switched: the pane's until a new session id shows there
	At        time.Time `json:"at"`
}

// Store keeps holds.json in a directory. An empty dir keeps them in memory.
type Store struct {
	dir  string
	mu   sync.Mutex
	by   map[string]Hold
	seen map[string]time.Time // when the session was last in a snapshot; not persisted
}

func (s *Store) path() string { return filepath.Join(s.dir, "holds.json") }

// Open reads holds.json. A missing file is no holds; one that does not
// parse is moved aside, so a broken file never keeps a session locked.
func Open(dir string) (*Store, error) {
	s := &Store{dir: dir, by: map[string]Hold{}, seen: map[string]time.Time{}}
	if dir == "" {
		return s, nil
	}
	b, err := os.ReadFile(s.path()) //nolint:gosec // our own state dir
	if errors.Is(err, fs.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	var list []Hold
	if err := json.Unmarshal(b, &list); err != nil {
		bad := s.path() + ".bad-" + time.Now().UTC().Format("20060102T150405")
		if err := os.Rename(s.path(), bad); err != nil {
			return nil, err
		}
		return s, nil
	}
	now := time.Now()
	for _, h := range list {
		if h.SessionID != "" {
			s.by[h.SessionID] = h
			s.seen[h.SessionID] = now
		}
	}
	return s, nil
}

// Set holds the session, replacing a hold it had.
func (s *Store) Set(h Hold) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if h.SessionID == "" {
		return errors.New("holds: a hold needs a session id")
	}
	was, had := s.by[h.SessionID]
	s.by[h.SessionID] = h
	if _, ok := s.seen[h.SessionID]; !ok {
		s.seen[h.SessionID] = time.Now()
	}
	if err := s.save(); err != nil {
		if had {
			s.by[h.SessionID] = was
		} else {
			delete(s.by, h.SessionID)
		}
		return err
	}
	return nil
}

// Release removes the hold and returns it.
func (s *Store) Release(sessionID string) (Hold, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	h, ok := s.by[sessionID]
	if !ok {
		return Hold{}, false, nil
	}
	delete(s.by, sessionID)
	if err := s.save(); err != nil {
		s.by[sessionID] = h
		return Hold{}, false, err
	}
	return h, true, nil
}

func (s *Store) Of(sessionID string) (Hold, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	h, ok := s.by[sessionID]
	return h, ok
}

// Pending is the Rebind hold on the pane.
func (s *Store) Pending(pane string) (Hold, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.pending(pane)
}

func (s *Store) pending(pane string) (Hold, bool) {
	for _, h := range s.by {
		if h.Rebind && h.Pane == pane {
			return h, true
		}
	}
	return Hold{}, false
}

// Rebind moves the pane's Rebind hold to the session now in it. It reports
// whether it moved.
func (s *Store) Rebind(pane, sessionID string) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	h, ok := s.pending(pane)
	if !ok || sessionID == "" || sessionID == h.SessionID {
		return false, nil
	}
	old := h.SessionID
	wasSeen, hadSeen := s.seen[old]
	delete(s.by, old)
	delete(s.seen, old)
	h.SessionID, h.Rebind = sessionID, false
	s.by[sessionID] = h
	s.seen[sessionID] = time.Now()
	if err := s.save(); err != nil {
		delete(s.by, sessionID)
		delete(s.seen, sessionID)
		h.SessionID, h.Rebind = old, true
		s.by[old] = h
		if hadSeen {
			s.seen[old] = wasSeen
		}
		return false, err
	}
	return true, nil
}

// List is every hold, oldest first.
func (s *Store) List() []Hold {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.list()
}

func (s *Store) list() []Hold {
	out := make([]Hold, 0, len(s.by))
	for _, h := range s.by {
		out = append(out, h)
	}
	sort.Slice(out, func(i, j int) bool {
		if !out[i].At.Equal(out[j].At) {
			return out[i].At.Before(out[j].At)
		}
		return out[i].SessionID < out[j].SessionID
	})
	return out
}

// Held is the session ids held, for the stoker's plan.
func (s *Store) Held() map[string]bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	m := make(map[string]bool, len(s.by))
	for id := range s.by {
		m[id] = true
	}
	return m
}

// Prune notes which sessions the snapshot holds and drops a hold whose
// session has been gone for GoneFor.
func (s *Store) Prune(present map[string]bool, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	dropped := false
	for id := range s.by {
		switch {
		case present[id]:
			s.seen[id] = now
		case now.Sub(s.seen[id]) >= GoneFor:
			delete(s.by, id)
			delete(s.seen, id)
			dropped = true
		}
	}
	if !dropped {
		return nil
	}
	return s.save()
}

// save writes holds.json atomically. Callers hold mu.
func (s *Store) save() error {
	if s.dir == "" {
		return nil
	}
	b, err := json.MarshalIndent(s.list(), "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(s.dir, ".holds-*.json")
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
	return os.Rename(tmp.Name(), s.path())
}
