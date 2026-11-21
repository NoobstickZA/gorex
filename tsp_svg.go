package main

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/egoist/mygo/ui"
)

// A surface's SVGs (omp's charts, as fenced svg blocks in its Markdown)
// color with the reader's theme through var(--fg), var(--muted), …: the
// colors are the theme's before it is parsed. MyGo draws the rest, the
// labels too.

// tspSVG is an SVG parsed for a theme.
type tspSVG struct {
	svg  *ui.SVG
	w, h float32
}

var svgVar = regexp.MustCompile(`var\(\s*--([a-zA-Z0-9-]+)\s*(?:,\s*([^)]*))?\)`)

// tspSVGFor returns src parsed for the colors k, nil when it does not
// parse; the last ones are kept, by source and theme.
func (a *App) tspSVGFor(k *colors, src string) *tspSVG {
	key := fmt.Sprintf("%v/%s", a.dark, src)
	if s, ok := a.tspSVGs[key]; ok {
		return s
	}
	themed := svgVar.ReplaceAllStringFunc(src, func(m string) string {
		sub := svgVar.FindStringSubmatch(m)
		if c, ok := svgThemeColor(k, sub[1]); ok {
			return hexOf(c)
		}
		if sub[2] != "" {
			return strings.TrimSpace(sub[2])
		}
		return hexOf(k.text)
	})
	s := &tspSVG{}
	svg, err := ui.ParseSVG([]byte(themed))
	if err != nil {
		s = nil
	} else {
		s.svg = svg
		s.w, s.h = svg.Size()
	}
	if a.tspSVGs == nil || len(a.tspSVGs) > 64 {
		a.tspSVGs = map[string]*tspSVG{}
	}
	a.tspSVGs[key] = s
	return s
}

// draw shows the SVG at most its own width.
func (s *tspSVG) draw(c *ui.Context) {
	ui.Image(c, s.svg).Width(s.w).MaxWidthPercent(100).AlignSelf(ui.Start)
}

// svgThemeColor is the color of a theme variable of the SVGs programs
// draw for a reader's theme.
func svgThemeColor(k *colors, name string) (ui.Color, bool) {
	switch name {
	case "fg", "text", "foreground":
		return k.text, true
	case "muted", "dim":
		return k.textMuted, true
	case "border":
		return k.cardBorder, true
	case "surface", "bg", "background":
		return k.card, true
	case "accent", "success", "c1":
		return k.busy, true
	case "warning", "error", "c2":
		return k.attention, true
	case "c3":
		return ui.RGB(0x60, 0xa5, 0xfa), true
	case "c4":
		return ui.RGB(0xc0, 0x84, 0xfc), true
	case "c5":
		return ui.RGB(0xf4, 0x72, 0xb6), true
	case "c6":
		return ui.RGB(0x2d, 0xd4, 0xbf), true
	}
	return ui.Color{}, false
}

// parseHexColor parses #rgb or #rrggbb.
func parseHexColor(s string) (c ui.Color, err error) {
	defer func() {
		if recover() != nil {
			err = fmt.Errorf("bad color %q", s)
		}
	}()
	return ui.Hex(s), nil
}
