package main

import (
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// The day states a guest reads on the calendar, and the text colour each renders with.
// An empty textColor means the cell inherits the page default.
var dayStateColors = []struct {
	rule      string // CSS selector whose block holds the background
	textColor string // explicit colour in that block, or "" to inherit
}{
	{".cal-day.blocked", "#5f6368"},
	{".cal-day.jesse-away", ""},
	{".cal-day.allison-away", ""},
	{".cal-day.cat-sitting", ""},
}

// inheritedText is the page's default text colour, from `body`.
const inheritedText = "#333"

// TestDayStateContrast reads the palette out of style.css rather than restating it, so a
// colour change that drops below WCAG AA fails here. JES-41 recoloured the day states
// after measuring only how distinct the swatches were from each other, and shipped
// blocked text at 3.06:1 — worse than the 4.63:1 it replaced, on the state a guest most
// needs to read.
func TestDayStateContrast(t *testing.T) {
	css, err := os.ReadFile("static/style.css")
	if err != nil {
		t.Fatalf("reading style.css: %v", err)
	}
	sheet := string(css)

	const minAA = 4.5
	for _, st := range dayStateColors {
		t.Run(st.rule, func(t *testing.T) {
			bg, ok := declaration(sheet, st.rule, "background")
			if !ok {
				t.Fatalf("no background declared for %s — has the rule been renamed?", st.rule)
			}
			fg := st.textColor
			if fg == "" {
				fg = inheritedText
			} else if got, ok := declaration(sheet, st.rule, "color"); !ok || got != fg {
				t.Fatalf("%s declares color %q, but this test expects %q; update dayStateColors deliberately", st.rule, got, fg)
			}

			if r := contrastRatio(fg, bg); r < minAA {
				t.Errorf("%s: %s on %s is %.2f:1, below WCAG AA %.1f:1 for normal text", st.rule, fg, bg, r, minAA)
			}
		})
	}
}

// TestStateHoversAreReachable guards the specificity trap that made the recoloured
// hovers dead CSS: each :not() counts toward specificity, so the generic available-day
// hover outranked every `.cal-day.<state>:hover` rule and away days hovered blue.
//
// This asserts the exclusions are present, not that a browser renders them — it cannot
// catch a specificity regression introduced some other way, only the removal of this fix.
func TestStateHoversAreReachable(t *testing.T) {
	css, err := os.ReadFile("static/style.css")
	if err != nil {
		t.Fatalf("reading style.css: %v", err)
	}

	re := regexp.MustCompile(`\.cal-day:hover(:not\([^)]*\))*`)
	generic := re.FindString(string(css))
	if generic == "" {
		t.Fatal("no generic .cal-day:hover rule found — has it been renamed?")
	}

	for _, state := range []string{".jesse-away", ".allison-away", ".cat-sitting"} {
		if !strings.Contains(generic, ":not("+state+")") {
			t.Errorf("generic hover %q does not exclude %s, so it outranks that state's own hover rule and the day hovers blue", generic, state)
		}
	}
}

// declaration pulls one property out of a rule's block. It deliberately matches the
// selector exactly so a renamed rule fails loudly rather than silently passing.
func declaration(sheet, selector, prop string) (string, bool) {
	re := regexp.MustCompile(`(?m)^\` + regexp.QuoteMeta(selector)[1:] + `\s*\{([^}]*)\}`)
	m := re.FindStringSubmatch(sheet)
	if m == nil {
		return "", false
	}
	for _, line := range strings.Split(m[1], ";") {
		name, value, found := strings.Cut(line, ":")
		if !found || strings.TrimSpace(name) != prop {
			continue
		}
		return strings.TrimSpace(value), true
	}
	return "", false
}

// contrastRatio implements the WCAG 2.x relative-luminance formula.
func contrastRatio(a, b string) float64 {
	la, lb := relativeLuminance(a), relativeLuminance(b)
	hi, lo := math.Max(la, lb), math.Min(la, lb)
	return (hi + 0.05) / (lo + 0.05)
}

func relativeLuminance(hex string) float64 {
	hex = strings.TrimPrefix(hex, "#")
	if len(hex) == 3 { // #333 -> #333333
		hex = string([]byte{hex[0], hex[0], hex[1], hex[1], hex[2], hex[2]})
	}
	channel := func(i int) float64 {
		v, err := strconv.ParseInt(hex[i:i+2], 16, 32)
		if err != nil {
			return 0
		}
		c := float64(v) / 255
		if c <= 0.03928 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	return 0.2126*channel(0) + 0.7152*channel(2) + 0.0722*channel(4)
}
