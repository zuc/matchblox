package app

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// swiftTokens maps each name in ios/Matchblox/Tokens.swift to the token of
// Tokens(true), the dark set of the console.
var swiftTokens = map[string]string{
	"bg": "bg", "fg": "fg", "muted": "muted", "faint": "faint", "hair": "hair",
	"accent": "accent", "wash": "wash", "warn": "warn", "neg": "neg",
	"flame": "flame", "flameAmber": "flame-amber", "flameCore": "flame-core",
	"matchHead": "match-head", "matchBreath1": "match-breath-1",
	"matchBreath2": "match-breath-2", "matchBreath3": "match-breath-3",
	"matchEmber": "match-ember",
	"markTop":    "mark-top", "markLeft": "mark-left", "markRight": "mark-right",
	"markTray": "mark-tray", "markStriker": "mark-striker", "markStick": "mark-stick",
	"night": "night",
}

func TestTokensSwiftMatchesTheConsole(t *testing.T) {
	src, err := os.ReadFile("../../ios/Matchblox/Tokens.swift")
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`static let (\w+) *(?::\s*Color)? *= *Color\(hex: 0x([0-9A-Fa-f]{6})\)`)
	got := map[string]string{}
	for _, m := range re.FindAllStringSubmatch(string(src), -1) {
		got[m[1]] = "#" + strings.ToUpper(m[2])
	}
	console := Tokens(true)
	for name, token := range swiftTokens {
		c, ok := console[token]
		if !ok {
			t.Fatalf("Tokens(true) has no %q for Swift %s", token, name)
		}
		have, ok := got[name]
		if !ok {
			t.Errorf("Tokens.swift has no %s", name)
			continue
		}
		if want := hex(c); have != want {
			t.Errorf("Tokens.%s = %s, the console's %s is %s", name, have, token, want)
		}
	}
	for name := range got {
		if _, ok := swiftTokens[name]; !ok {
			t.Errorf("Tokens.swift has %s, which the test does not map to a console token", name)
		}
	}
}
