package app

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/blox-eng/matchblox/internal/replay"
)

var stills = flag.Bool("stills", false, "TestPairStills: write the pair page's stills in testdata/pair/stills")

// TestPairStills draws three moments of the pair box for the replay: the
// shut box, the knock and the open box. With -stills it writes them.
func TestPairStills(t *testing.T) {
	link := boxLink(t)
	moments := []struct {
		name string
		s    boxState
	}{
		{"shut", boxState{phase: boxShut, link: link}},
		{"knock", boxState{phase: boxKnock, since: 30 * time.Millisecond, link: link}},
		{"open", boxState{phase: boxOpen, since: 2 * time.Second, link: link, name: "Morgan's iPad"}},
	}
	dir := filepath.Join("..", "..", "testdata", "pair", "stills")
	for _, m := range moments {
		lines, fits := pairBox(m.s, true, false, 100, 40)
		if fits != fitBox {
			t.Fatalf("%s: the box does not fit 100 × 40", m.name)
		}
		got := strings.Join(lines, "\n") + "\n"
		if _, err := replay.Parse(got, Tokens(true)); err != nil {
			t.Fatalf("%s: %v", m.name, err)
		}
		path := filepath.Join(dir, m.name+".ansi")
		if *stills {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: %v (run go test ./internal/app/ -run PairStills -stills)", m.name, err)
		}
		if string(want) != got {
			t.Errorf("%s: the still is out of date (run go test ./internal/app/ -run PairStills -stills)", m.name)
		}
	}
}
