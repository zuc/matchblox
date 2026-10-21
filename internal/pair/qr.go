package pair

import (
	"strings"

	"rsc.io/qr"
)

const quiet = 2

// Modules gives the module grid of text at level L, with the quiet zone in
// it. light reports whether a module is light.
func Modules(text string) (size int, light func(x, y int) bool, err error) {
	code, err := qr.Encode(text, qr.L)
	if err != nil {
		return 0, nil, err
	}
	light = func(x, y int) bool {
		x, y = x-quiet, y-quiet
		if x < 0 || y < 0 || x >= code.Size || y >= code.Size {
			return true
		}
		return !code.Black(x, y)
	}
	return code.Size + 2*quiet, light, nil
}

// QR draws text as terminal lines, two modules to a cell. Light modules are
// the filled glyphs, so the code scans on a dark terminal.
func QR(text string) ([]string, error) {
	total, light, err := Modules(text)
	if err != nil {
		return nil, err
	}
	glyphs := [4]string{" ", "▄", "▀", "█"}
	var lines []string
	for row := 0; row < total; row += 2 {
		var sb strings.Builder
		for col := 0; col < total; col++ {
			i := 0
			if light(col, row) {
				i += 2
			}
			if light(col, row+1) {
				i++
			}
			sb.WriteString(glyphs[i])
		}
		lines = append(lines, sb.String())
	}
	return lines, nil
}
