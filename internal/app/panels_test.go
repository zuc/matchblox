package app

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"

	"github.com/blox-eng/matchblox/internal/sample"
)

// A failed push shows once on the Machine tab, under the alerts; without
// one the tab has no push line.
func TestTheMachineTabShowsAPushFailureOnce(t *testing.T) {
	st := fixtureState()
	st.Alerts = []sample.Alert{{Key: "load", Level: "warn", Title: "load high"}}
	st.Push = "push: relay 503"
	m, _ := loadedWith(t, 100, st)
	m.setTab(tabMachine)
	out := screen(m)
	if n := strings.Count(out, "push: relay 503"); n != 1 {
		t.Fatalf("the push line shows %d times:\n%s", n, out)
	}
	lines := m.machine(100)
	at := func(want string) int {
		for i, l := range lines {
			if strings.Contains(ansi.Strip(l), want) {
				return i
			}
		}
		return -1
	}
	if a, p, load := at("load high"), at("push: relay 503"), at("pressure cpu"); a < 0 || p != a+1 || load < p {
		t.Fatalf("alert %d, push %d, cpu %d: want the push line under the alerts", a, p, load)
	}
	if l := lines[at("push: relay 503")]; !strings.Contains(l, m.st.warn.Render("push: relay 503")) {
		t.Fatalf("the push line is not in warn: %q", l)
	}

	st.Push = ""
	m, _ = loadedWith(t, 100, st)
	m.setTab(tabMachine)
	if out := screen(m); strings.Contains(out, "push:") {
		t.Fatalf("a push line without a failure:\n%s", out)
	}
}
