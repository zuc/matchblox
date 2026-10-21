package app

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/blox-eng/matchblox/internal/pair"
	"github.com/blox-eng/matchblox/internal/replay"
	"github.com/charmbracelet/x/ansi"
)

// boxLink is the v2 link of the fixture with a long tailnet host, the
// largest code the page draws.
func boxLink(t *testing.T) string {
	t.Helper()
	b, err := os.ReadFile("../../testdata/pair/v2.json")
	if err != nil {
		t.Fatal(err)
	}
	var f struct{ Payload pair.Payload }
	if err := json.Unmarshal(b, &f); err != nil {
		t.Fatal(err)
	}
	p := f.Payload
	p.Host, p.User, p.Name = "studio-workstation-mbp.tail1a2b3c.ts.net", "buildersmith", "Studio-Workstation-Mbp"
	return p.URL()
}

var boxPhases = []boxPhase{boxShut, boxKnock, boxOpen, boxExpired}

type tokCell struct{ ch, fg, bg string }

// tokCells reads a frame the way the replay does, one entry for each cell.
func tokCells(t *testing.T, lines []string, dark bool) [][]tokCell {
	t.Helper()
	spans, err := replay.Parse(strings.Join(lines, "\n"), Tokens(dark))
	if err != nil {
		t.Fatal(err)
	}
	out := make([][]tokCell, len(spans))
	for y, line := range spans {
		for _, s := range line {
			for _, r := range s.Text {
				out[y] = append(out[y], tokCell{string(r), s.FG, s.BG})
			}
		}
	}
	return out
}

func TestPairBoxEveryCellIsAToken(t *testing.T) {
	link := boxLink(t)
	for _, dark := range []bool{true, false} {
		for _, ph := range boxPhases {
			for _, at := range []time.Duration{0, 100 * time.Millisecond, 500 * time.Millisecond, time.Second, 2 * time.Second} {
				s := boxState{phase: ph, since: at, link: link, name: "Morgan's iPad"}
				lines, _ := pairBox(s, dark, false, 100, 40)
				if _, err := replay.Parse(strings.Join(lines, "\n"), Tokens(dark)); err != nil {
					t.Fatalf("dark=%v phase %d at %v: %v", dark, ph, at, err)
				}
			}
		}
	}
}

func TestPairBoxLabelScans(t *testing.T) {
	link := boxLink(t)
	size, light, err := pair.Modules(link)
	if err != nil {
		t.Fatal(err)
	}
	for _, dark := range []bool{true, false} {
		paper := "bg"
		if dark {
			paper = "fg"
		}
		for _, c := range []struct{ w, h int }{{100, 40}, {70, 36}} {
			lines, fit := pairBox(boxState{phase: boxShut, link: link}, dark, false, c.w, c.h)
			g := boxGeom(size)
			x0, y0 := (c.w-g.w)/2+g.labelX, g.labelY
			if fit == fitQROnly {
				x0, y0 = (c.w-size)/2, 0
			}
			cells := tokCells(t, lines, dark)
			for y := 0; y < size; y++ {
				for x := 0; x < size; x++ {
					cl := cells[y0+y/2][x0+x]
					if cl.ch != "▀" {
						t.Fatalf("dark=%v %dx%d: label cell %d,%d is %q", dark, c.w, c.h, x, y/2, cl.ch)
					}
					ink := cl.fg
					if y%2 == 1 {
						ink = cl.bg
					}
					if (ink == paper) != light(x, y) {
						t.Fatalf("dark=%v %dx%d: module %d,%d is %q", dark, c.w, c.h, x, y, ink)
					}
				}
			}
		}
	}
}

func TestPairBoxFits(t *testing.T) {
	link := boxLink(t)
	for _, ph := range boxPhases {
		for _, at := range []time.Duration{0, 500 * time.Millisecond, 2 * time.Second} {
			s := boxState{phase: ph, since: at, link: link, name: "iPad"}
			lines, fit := pairBox(s, true, false, 100, 40)
			if fit != fitBox {
				t.Fatalf("phase %d at %v: 100 × 40 gives %d", ph, at, fit)
			}
			if len(lines)+pairTextRows > 40 {
				t.Fatalf("phase %d at %v: %d lines leave no room for the text", ph, at, len(lines))
			}
			for i, l := range lines {
				if n := ansi.StringWidth(l); n > 100 {
					t.Fatalf("phase %d at %v: line %d is %d wide", ph, at, i, n)
				}
			}
		}
	}
	if _, fit := pairBox(boxState{link: link}, true, false, 70, 36); fit != fitQROnly {
		t.Fatalf("70 × 36 gives %d", fit)
	}
	if _, fit := pairBox(boxState{link: link}, true, false, 61, 33); fit != fitQROnly {
		t.Fatalf("61 × 33 gives %d", fit)
	}
	if lines, fit := pairBox(boxState{link: link}, true, false, 60, 32); fit != fitNone || lines != nil {
		t.Fatalf("60 × 32 gives %d", fit)
	}
}

func TestPairBoxOpenEndsStill(t *testing.T) {
	link := boxLink(t)
	for _, dark := range []bool{true, false} {
		s := boxState{phase: boxOpen, since: 1100 * time.Millisecond, link: link, name: "iPad"}
		a, _ := pairBox(s, dark, false, 100, 40)
		s.since = 10 * time.Second
		b, _ := pairBox(s, dark, false, 100, 40)
		if strings.Join(a, "\n") != strings.Join(b, "\n") {
			t.Fatalf("dark=%v: the opening still moves at 1.1 s", dark)
		}
	}
}

func TestPairBoxNoMotionIsTheLastFrame(t *testing.T) {
	link := boxLink(t)
	for _, dark := range []bool{true, false} {
		for _, ph := range boxPhases {
			s := boxState{phase: ph, link: link, name: "iPad"}
			a, _ := pairBox(s, dark, true, 100, 40)
			s.since = 10 * time.Second
			b, _ := pairBox(s, dark, false, 100, 40)
			if strings.Join(a, "\n") != strings.Join(b, "\n") {
				t.Fatalf("dark=%v phase %d: no motion is not the last frame", dark, ph)
			}
		}
	}
}

func TestPairBoxLightIsUnlit(t *testing.T) {
	link := boxLink(t)
	s := boxState{phase: boxOpen, since: 10 * time.Second, link: link, name: "iPad"}
	lines, _ := pairBox(s, false, false, 100, 40)
	for _, row := range tokCells(t, lines, false) {
		for _, c := range row {
			if strings.HasPrefix(c.fg, "flame") || strings.HasPrefix(c.bg, "flame") {
				t.Fatalf("a flame cell %+v in a light terminal", c)
			}
		}
	}
	lines, _ = pairBox(s, true, false, 100, 40)
	burns := false
	for _, row := range tokCells(t, lines, true) {
		for _, c := range row {
			burns = burns || c.fg == "flame"
		}
	}
	if !burns {
		t.Fatal("in a dark terminal the match burns at the end")
	}
}

// flameCells counts the cells drawn in a flame colour, and the flash.
func flameCells(t *testing.T, lines []string, dark bool) (flame, flash int) {
	t.Helper()
	for _, row := range tokCells(t, lines, dark) {
		for _, c := range row {
			if strings.HasPrefix(c.fg, "flame") || strings.HasPrefix(c.bg, "flame") {
				flame++
			}
			if c.ch == "▸" {
				flash++
			}
		}
	}
	return flame, flash
}

func TestPairBoxFlashOnlyInTheDark(t *testing.T) {
	link := boxLink(t)
	s := boxState{phase: boxOpen, since: openFor + boxOutFor + boxStrikeFor - flashFor/2, link: link, name: "iPad"}
	lines, _ := pairBox(s, false, false, 100, 40)
	if flame, flash := flameCells(t, lines, false); flame != 0 || flash != 0 {
		t.Fatalf("at the strike in a light terminal: %d flame cells, %d flashes", flame, flash)
	}
	lines, _ = pairBox(s, true, false, 100, 40)
	if _, flash := flameCells(t, lines, true); flash != 1 {
		t.Fatalf("at the strike in a dark terminal: %d flashes, want 1", flash)
	}
}

func TestPairBoxFlameWaitsForTheMatchToStand(t *testing.T) {
	link := boxLink(t)
	s := boxState{phase: boxOpen, since: openFor + boxOutFor + boxStrikeFor + 20*time.Millisecond, link: link, name: "iPad"}
	lines, _ := pairBox(s, true, false, 100, 40)
	if flame, _ := flameCells(t, lines, true); flame != 0 {
		t.Fatalf("the match still lies on the striker, yet %d flame cells show", flame)
	}
}

func TestPairBoxOpenedLabelNamesTheDevice(t *testing.T) {
	link := boxLink(t)
	for _, dark := range []bool{true, false} {
		s := boxState{phase: boxOpen, since: 10 * time.Second, link: link, name: "Morgan's iPad"}
		lines, _ := pairBox(s, dark, false, 100, 40)
		out := ansi.Strip(strings.Join(lines, "\n"))
		if !strings.Contains(out, "Morgan's iPad") || !strings.Contains(out, "●") || !strings.Contains(out, "┃") {
			t.Fatalf("dark=%v: the opened box has the name and the standing match:\n%s", dark, out)
		}
		s.phase = boxExpired
		lines, _ = pairBox(s, dark, false, 100, 40)
		if out := ansi.Strip(strings.Join(lines, "\n")); strings.Contains(out, "iPad") || strings.Contains(out, "●") {
			t.Fatalf("dark=%v: the expired box is shut with a blank label:\n%s", dark, out)
		}
	}
}

func TestPairBoxKnockMovesOneCell(t *testing.T) {
	link := boxLink(t)
	rest, _ := pairBox(boxState{phase: boxShut, link: link}, true, false, 100, 40)
	left, _ := pairBox(boxState{phase: boxKnock, since: 30 * time.Millisecond, link: link}, true, false, 100, 40)
	back, _ := pairBox(boxState{phase: boxKnock, since: knockFor, link: link}, true, false, 100, 40)
	if strings.Join(back, "\n") != strings.Join(rest, "\n") {
		t.Fatal("after the knock the box is back in place")
	}
	a, b := ansi.Strip(rest[1]), ansi.Strip(left[1])
	if strings.Index(b, "▚") != strings.Index(a, "▚")-1 {
		t.Fatalf("the knock moves the box one cell left:\n%q\n%q", a, b)
	}
}

func TestPairBoxNoAgentGlyph(t *testing.T) {
	link := boxLink(t)
	for _, dark := range []bool{true, false} {
		for _, ph := range boxPhases {
			for at := time.Duration(0); at <= 1200*time.Millisecond; at += 10 * time.Millisecond {
				s := boxState{phase: ph, since: at, link: link, name: "iPad"}
				lines, _ := pairBox(s, dark, false, 100, 40)
				if out := ansi.Strip(strings.Join(lines, "\n")); strings.ContainsAny(out, "✦✧╿│") {
					t.Fatalf("dark=%v phase %d at %v: an agent glyph on the pair page", dark, ph, at)
				}
			}
		}
	}
}
