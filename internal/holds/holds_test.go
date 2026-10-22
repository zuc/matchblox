package holds

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestHoldsRoundTrip(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	at := time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)
	if err := s.Set(Hold{SessionID: "s1", Pane: "%1", Name: "a", By: ByYou, At: at}); err != nil {
		t.Fatal(err)
	}
	if err := s.Set(Hold{SessionID: "s2", Pane: "%2", Name: "b", By: ByStoker, Why: "not priority", At: at.Add(time.Minute)}); err != nil {
		t.Fatal(err)
	}
	h, ok, err := s.Release("s1")
	if err != nil || !ok || h.Name != "a" {
		t.Fatalf("release: %+v %v %v", h, ok, err)
	}
	re, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := re.List()
	if len(got) != 1 || got[0].SessionID != "s2" || got[0].Why != "not priority" || !got[0].At.Equal(at.Add(time.Minute)) {
		t.Fatalf("list %+v", got)
	}
	if _, ok := re.Of("s1"); ok {
		t.Fatal("s1 came back")
	}
}

func TestInMemoryWithoutADir(t *testing.T) {
	s, err := Open("")
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Set(Hold{SessionID: "s1", By: ByYou}); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Of("s1"); !ok {
		t.Fatal("lost")
	}
}

func TestPruneDropsAHoldGoneForADay(t *testing.T) {
	s, _ := Open(t.TempDir())
	t0 := time.Date(2026, 10, 13, 9, 0, 0, 0, time.UTC)
	_ = s.Set(Hold{SessionID: "s1", By: ByYou})
	_ = s.Prune(map[string]bool{"s1": true}, t0)
	_ = s.Prune(nil, t0.Add(23*time.Hour))
	if _, ok := s.Of("s1"); !ok {
		t.Fatal("pruned after 23 h")
	}
	_ = s.Prune(nil, t0.Add(24*time.Hour))
	if _, ok := s.Of("s1"); ok {
		t.Fatal("kept after 24 h")
	}
}

func TestAHoldMarkedRebindMovesToTheNextSessionInItsPane(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Set(Hold{SessionID: "old", Pane: "%1", By: ByYou, Rebind: true}); err != nil {
		t.Fatal(err)
	}
	if h, ok := s.Pending("%1"); !ok || h.SessionID != "old" {
		t.Fatalf("pending %%1: %+v %v", h, ok)
	}
	if _, ok := s.Pending("%2"); ok {
		t.Fatal("%2 has no pending hold")
	}
	for _, id := range []string{"old", ""} {
		if moved, err := s.Rebind("%1", id); moved || err != nil {
			t.Fatalf("rebind to %q: %v %v", id, moved, err)
		}
	}
	if h, ok := s.Of("old"); !ok || !h.Rebind {
		t.Fatalf("old changed: %+v %v", h, ok)
	}
	if moved, err := s.Rebind("%1", "new"); !moved || err != nil {
		t.Fatalf("rebind: %v %v", moved, err)
	}
	check := func(s *Store) {
		t.Helper()
		if h, ok := s.Of("new"); !ok || h.By != ByYou || h.Pane != "%1" || h.Rebind {
			t.Fatalf("new: %+v %v", h, ok)
		}
		if _, ok := s.Of("old"); ok {
			t.Fatal("old is still held")
		}
		if _, ok := s.Pending("%1"); ok {
			t.Fatal("%1 is still pending")
		}
	}
	check(s)
	re, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	check(re)
}

func TestRebindRoundTripsThroughJSONAndAPlainHoldHasNoKey(t *testing.T) {
	b, err := json.Marshal(Hold{SessionID: "a", Rebind: true})
	if err != nil || !strings.Contains(string(b), `"rebind":true`) {
		t.Fatalf("%s %v", b, err)
	}
	var h Hold
	if err := json.Unmarshal(b, &h); err != nil || !h.Rebind {
		t.Fatalf("%+v %v", h, err)
	}
	b, _ = json.Marshal(Hold{SessionID: "a"})
	if strings.Contains(string(b), "rebind") {
		t.Fatalf("plain hold: %s", b)
	}
}
