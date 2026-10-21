package app

import (
	"image/color"
	"math"
	"strings"
	"time"

	"charm.land/lipgloss/v2"
	"github.com/blox-eng/matchblox/internal/pair"
	"github.com/charmbracelet/x/ansi"
)

// The pair page draws the site's theme-switch box front-on: the sleeve, the
// striker along its top, the drawer that slides out to the right, and the
// match. The paper label on the sleeve is the pair code.

type boxPhase int

const (
	boxShut    boxPhase = iota // the code waits
	boxKnock                   // a device reached gate pair
	boxOpen                    // a device claimed the code
	boxExpired                 // the code ran out
)

type boxState struct {
	phase boxPhase
	since time.Duration // time since the phase began
	link  string        // the v2 URL; the label's QR
	name  string        // the device's name on the label after the claim; "" before
}

type fitKind int

const (
	fitNone   fitKind = iota // the code does not fit
	fitQROnly                // the code without the box
	fitBox                   // the box, the open drawer and the standing match
)

// The steps of the motion, the site's timings. The drawer shuts in openFor
// while the match stands.
const (
	knockFor     = 240 * time.Millisecond
	openFor      = 260 * time.Millisecond
	boxOutFor    = 280 * time.Millisecond
	boxStrikeFor = 150 * time.Millisecond
	standFor     = 320 * time.Millisecond
	flashFor     = 40 * time.Millisecond
)

// pairTextRows are the rows under the box for the page's two lines and the
// gap above them.
const pairTextRows = 3

const (
	labelPad  = 7  // sleeve columns on each side of the label
	drawerOut = 16 // columns the drawer slides out
	matchRows = 20 // the upright stick; lying down it is twice as many columns
)

// geom is the box for a code of size modules, in the cells of its canvas.
type geom struct {
	w, h             int // the canvas
	sw, sh           int // the sleeve, at column 1 of the canvas
	labelX, labelY   int // with the box at rest
	drawerY, drawerH int
	ux, base         int // the upright match: its column and the row of its tail
}

func boxGeom(size int) geom {
	rows := (size + 1) / 2
	g := geom{sw: size + 2*labelPad + 2, sh: rows + 5, labelX: 1 + 1 + labelPad, labelY: 3}
	g.drawerY, g.drawerH = g.sh/2, g.sh-g.sh/2-2
	g.ux = 1 + g.sw + drawerOut + 3
	g.base = g.sh - 2
	g.w, g.h = g.ux+2, g.sh
	return g
}

type pcell struct {
	ch     string // "" is the second half of a wide rune
	fg, bg color.Color
}

type canvas [][]pcell

func newCanvas(w, h int) canvas {
	g := make(canvas, h)
	for y := range g {
		g[y] = make([]pcell, w)
		for x := range g[y] {
			g[y][x].ch = " "
		}
	}
	return g
}

// put draws ch in fg; a nil bg keeps the cell's ground.
func (g canvas) put(x, y int, ch string, fg, bg color.Color) {
	if y < 0 || y >= len(g) || x < 0 || x >= len(g[y]) {
		return
	}
	c := &g[y][x]
	c.ch, c.fg = ch, fg
	if bg != nil {
		c.bg = bg
	}
}

func (g canvas) fill(x, y, w, h int, bg color.Color) {
	for j := y; j < y+h; j++ {
		for i := x; i < x+w; i++ {
			g.put(i, j, " ", nil, bg)
		}
	}
}

// lines renders the canvas centred in w columns.
func (g canvas) lines(w int) []string {
	left := (w - len(g[0])) / 2
	pad, tail := strings.Repeat(" ", left), strings.Repeat(" ", w-left-len(g[0]))
	out := make([]string, len(g))
	for y, row := range g {
		var b strings.Builder
		b.WriteString(pad)
		for i := 0; i < len(row); {
			j := i
			var run strings.Builder
			for ; j < len(row) && row[j].fg == row[i].fg && row[j].bg == row[i].bg; j++ {
				run.WriteString(row[j].ch)
			}
			st := lipgloss.NewStyle()
			if row[i].fg != nil {
				st = st.Foreground(row[i].fg)
			}
			if row[i].bg != nil {
				st = st.Background(row[i].bg)
			}
			if row[i].fg == nil && row[i].bg == nil {
				b.WriteString(run.String())
			} else {
				b.WriteString(st.Render(run.String()))
			}
			i = j
		}
		b.WriteString(tail)
		out[y] = b.String()
	}
	return out
}

// labelInks are the label's paper and ink: the dark terminal's text and
// ground in both themes, so the code scans.
func labelInks(dark bool) (paper, ink color.Color) {
	if dark {
		return darkTheme.Text, Tokens(true)["bg"]
	}
	return Tokens(false)["bg"], lightTheme.Text
}

// label draws the code two modules to a cell. After the claim it is plain
// paper with the device's name; after expiry, blank paper.
func (g canvas) label(x0, y0, size int, light func(x, y int) bool, s boxState, dark bool) {
	paper, ink := labelInks(dark)
	code := s.phase == boxShut || s.phase == boxKnock
	rows := (size + 1) / 2
	for r := 0; r < rows; r++ {
		for c := 0; c < size; c++ {
			top, bottom := paper, paper
			if code && !light(c, 2*r) {
				top = ink
			}
			if code && !light(c, 2*r+1) {
				bottom = ink
			}
			g.put(x0+c, y0+r, "▀", top, bottom)
		}
	}
	if s.phase != boxOpen || s.name == "" {
		return
	}
	name := ansi.Truncate(s.name, size-4, "…")
	x := x0 + (size-ansi.StringWidth(name))/2
	for _, r := range name {
		ch := string(r)
		g.put(x, y0+rows/2, ch, ink, paper)
		if ansi.StringWidth(ch) == 2 {
			g.put(x+1, y0+rows/2, "", ink, paper)
			x++
		}
		x++
	}
}

func easeIn(x float64) float64 { return math.Pow(clamp01(x), 1.6) }

func frac(t, d time.Duration) float64 { return float64(t) / float64(d) }

// pairBox draws the box at a moment, centred in w columns, or nil when the
// QR does not fit. w × h is the page: the box leaves pairTextRows under it
// for the page's lines, and the caller centres both in h.
func pairBox(s boxState, dark, noMotion bool, w, h int) (lines []string, fits fitKind) {
	size, light, err := pair.Modules(s.link)
	if err != nil {
		return nil, fitNone
	}
	if noMotion {
		s.since = time.Hour
	}
	g := boxGeom(size)
	switch {
	case w >= g.w && h >= g.h+pairTextRows:
		return drawBox(s, dark, g, size, light).lines(w), fitBox
	case w >= size+4 && h >= (size+1)/2+pairTextRows+1:
		c := newCanvas(size, (size+1)/2)
		c.label(0, 0, size, light, s, dark)
		return c.lines(w), fitQROnly
	}
	return nil, fitNone
}

func drawBox(s boxState, dark bool, g geom, size int, light func(x, y int) bool) canvas {
	mk, th := lightMark, lightTheme
	if dark {
		mk, th = darkMark, darkTheme
	}
	c := newCanvas(g.w, g.h)
	t := s.since
	x0 := 1
	if s.phase == boxKnock && t < knockFor && (t/(knockFor/4))%2 == 0 {
		x0 = 0 // one cell left and back, twice
	}

	// The timeline of the opening.
	var (
		drawer float64 // how far the drawer is out, 0 to 1
		struck = t >= openFor+boxOutFor+boxStrikeFor-flashFor
		stand  = t - (openFor + boxOutFor + boxStrikeFor)
	)
	if s.phase == boxOpen {
		switch {
		case t < openFor:
			drawer = easeOut(frac(t, openFor))
		case stand < 0:
			drawer = 1
		default:
			drawer = 1 - easeOut(frac(stand, openFor))
		}
	}

	// The drawer, behind the sleeve's lower half.
	sx := x0 + g.sw // the first column right of the sleeve
	if out := int(math.Round(drawer * drawerOut)); out > 0 {
		c.fill(sx, g.drawerY, out, g.drawerH, mk.Tray)
		for i := 0; i < out; i++ {
			c.put(sx+i, g.drawerY, "▀", mk.Left, mk.Tray)
			c.put(sx+i, g.drawerY+g.drawerH-1, "▄", mk.Left, mk.Tray)
		}
		c.fill(sx+out-1, g.drawerY, 1, g.drawerH, mk.Left)
	}

	// The sleeve: a hairline top and bottom, the sides a cell wide.
	for i := 0; i < g.sw; i++ {
		c.put(x0+i, 0, "▄", mk.Left, nil)
		c.put(x0+i, g.sh-1, "▀", mk.Left, nil)
	}
	c.fill(x0, 1, g.sw, g.sh-2, mk.Right)
	c.fill(x0, 1, 1, g.sh-2, mk.Left)
	c.fill(x0+g.sw-1, 1, 1, g.sh-2, mk.Left)
	for i := 1; i < g.sw-1; i++ {
		ch := "▚"
		if i%2 == 0 {
			ch = "▞"
		}
		c.put(x0+i, 1, ch, mk.Striker, mk.Right)
	}
	c.label(x0+g.labelX-1, g.labelY, size, light, s, dark)

	if s.phase != boxOpen {
		return c
	}

	// The match: in the drawer, out to the start of the striker, along it,
	// then upright right of the drawer.
	lh, lv := float64(2*matchRows), float64(matchRows)
	head := mk.Head
	if struck {
		head = th.Accent
	}
	from := float64(x0 + 2 + 2*matchRows) // the head at the start of the strike
	to := float64(x0 + g.sw - 3)          // the head at the end of the striker
	boxed := float64(sx + drawerOut - 3)  // the head in the open drawer
	inDrawer := float64(g.drawerY + g.drawerH/2)
	var tx, ty, hx, hy float64
	clip := 0
	switch {
	case t < openFor:
		hx, hy = float64(sx)+drawer*drawerOut-3, inDrawer
		tx, ty, clip = hx-lh, hy, sx
	case t < openFor+boxOutFor:
		k := easeOut((frac(t-openFor, boxOutFor) - 0.2) / 0.8)
		hx, hy = boxed+(from-boxed)*k, inDrawer+(1-inDrawer)*k
		tx, ty = hx-lh, hy
		if k == 0 {
			clip = sx
		}
	case stand < 0:
		k := easeIn(frac(t-openFor-boxOutFor, boxStrikeFor))
		hx, hy = from+(to-from)*k, 1
		tx, ty = hx-lh, hy
	default:
		k := easeOut(frac(stand, standFor))
		a := k * math.Pi / 2
		tx, ty = to-lh+(float64(g.ux)-(to-lh))*k, 1+float64(g.base-1)*k
		hx, hy = tx+lh*math.Cos(a), ty-lv*math.Sin(a)
	}
	hxi, hyi := int(math.Round(hx)), int(math.Round(hy))
	dx, dy := hx-tx, hy-ty
	glyph := "━"
	if math.Abs(dx) < 2*math.Abs(dy) {
		glyph = "┃"
	}
	n := int(math.Max(math.Abs(float64(hxi-int(math.Round(tx)))), math.Abs(float64(hyi-int(math.Round(ty))))))
	for i := 0; i < n; i++ {
		x := int(math.Round(tx + dx*float64(i)/float64(n)))
		y := int(math.Round(ty + dy*float64(i)/float64(n)))
		if x >= clip && (x != hxi || y != hyi) {
			c.put(x, y, glyph, mk.Stick, nil)
		}
	}
	if hxi >= clip {
		c.put(hxi, hyi, "●", head, nil)
	}
	if dark && stand < 0 && struck {
		c.put(hxi+1, hyi, "▸", mk.Core, nil)
	}

	// The flame grows once the match stands past 45°, in a dark terminal only.
	if k := easeOut(frac(stand, standFor)); dark && stand >= 0 && k > 0.5 {
		c.flame(hxi, hyi, (k-0.5)*2, mk)
	}
	return c
}

// flameShape is the flame in half-block pixels, three columns wide, from
// the tip down to the head: f flame, a amber, c core.
var flameShape = [8]string{".f.", ".f.", ".a.", "faf", "fcf", "fcf", ".c.", ".c."}

// flame draws the flame over the head at hx, hy, grown to k: the lower
// part shows first.
func (g canvas) flame(hx, hy int, k float64, mk markColours) {
	shown := int(math.Ceil(k * 8))
	ink := func(r, i int) color.Color {
		if r < 8-shown {
			return nil
		}
		return map[byte]color.Color{'f': mk.Flame, 'a': mk.Amber, 'c': mk.Core}[flameShape[r][i]]
	}
	for row := 0; row < 4; row++ {
		for i := 0; i < 3; i++ {
			top, bottom := ink(2*row, i), ink(2*row+1, i)
			x, y := hx-1+i, hy-4+row
			switch {
			case top != nil && bottom != nil:
				g.put(x, y, "▀", top, bottom)
			case top != nil:
				g.put(x, y, "▀", top, nil)
			case bottom != nil:
				g.put(x, y, "▄", bottom, nil)
			}
		}
	}
}
