package sample

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/blox-eng/matchblox/internal/limits"
)

const cursorJS = "/home/user/.local/share/cursor-agent/versions/2026.10.19-6bbf2c4/index.js"

// cursorFixture is the fixture machine with Cursor's CLI in pane %20, signed
// in, and its status line's reading as tapped at tapAt (zero: none).
func cursorFixture(t *testing.T, tapAt time.Time) (*Sampler, string) {
	t.Helper()
	root := copyFixture(t)
	addProc(t, root, 900, 1, "node", []string{"/home/user/.local/bin/agent", "--use-system-ca", cursorJS}, "%20")
	env := "TMUX=/tmp/tmux-1000/default,1,1\x00TMUX_PANE=%20\x00"
	if err := os.WriteFile(filepath.Join(root, "proc", "900", "environ"), []byte(env), 0o644); err != nil {
		t.Fatal(err)
	}
	addPane(t, root, "%20\tcur:1.1\tw\t"+strconv.Itoa(900)+"\tnode\t/work")
	cfg := filepath.Join(root, "home", ".cursor", "cli-config.json")
	if err := os.MkdirAll(filepath.Dir(cfg), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, []byte(`{"version":1,"authInfo":{"email":"you@example.com","displayName":"You","teamId":1,"teamName":"Acme","authId":"x"}}`), 0o644); err != nil {
		t.Fatal(err)
	}
	smp := newFixtureSampler(root)
	smp.Agents = []string{"claude", CursorAgent}
	if !tapAt.IsZero() {
		in, err := os.ReadFile("../../testdata/hooks/cursor-statusline.json")
		if err != nil {
			t.Fatal(err)
		}
		if err := limits.TapCursor(in, smp.LimitsDir, "/tmp/tmux-1000/default", "%20", tapAt); err != nil {
			t.Fatal(err)
		}
	}
	return smp, root
}

// TestCursorContextComesFromItsStatusLine: a Cursor session shows its
// context, model and account as the other agents do.
func TestCursorContextComesFromItsStatusLine(t *testing.T) {
	smp, _ := cursorFixture(t, fixtureNow.Add(-time.Minute))
	snap := smp.Sample()
	s, ok := byPane(snap)["%20"]
	if !ok {
		t.Fatal("no Cursor session")
	}
	if s.Context != "known" || s.Tokens != 14592 || s.Window != 256000 || s.Model != "Auto" || int(s.ContextPct*10) != 57 {
		t.Errorf("context %q %d/%d %.2f%% %q, want known 14592/256000 5.7%% Auto", s.Context, s.Tokens, s.Window, s.ContextPct, s.Model)
	}
	if s.Account != "you@example.com · Acme" || s.Provider != "cursor" {
		t.Errorf("account %q provider %q", s.Account, s.Provider)
	}
	var cursor *limits.Account
	for i, a := range snap.Limits {
		if a.Provider == "cursor" {
			cursor = &snap.Limits[i]
		}
	}
	if cursor == nil || cursor.Account != "you@example.com · Acme" || cursor.State != limits.Unmeasured || cursor.Fix == "" {
		t.Errorf("limits %+v, want Cursor's account, not measured, with why", snap.Limits)
	}
}

// TestCursorWithoutItsStatusLineIsNotMeasured: no reading, or one of an
// earlier session in the same pane, measures nothing.
func TestCursorWithoutItsStatusLineIsNotMeasured(t *testing.T) {
	for name, at := range map[string]time.Time{"no reading": {}, "an older session's": time.Unix(1, 0)} {
		smp, _ := cursorFixture(t, at)
		s := byPane(smp.Sample())["%20"]
		if s.Context != "unmeasured" || s.Tokens != 0 {
			t.Errorf("%s: context %q tokens %d, want unmeasured", name, s.Context, s.Tokens)
		}
	}
}
