//go:build !gorexserver

package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	mermaid "github.com/bvolpato/mermaid-go-renderer"
	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/markdown"
)

// A surface's Mermaid diagrams (omp's mermaid fences in its Markdown) are
// drawn as Tern draws them: a figure with the diagram's kind and Source,
// Copy and Open over the drawing. mmdg, a port of mermaid to Go, lays each
// out as an SVG off the UI thread, which MyGo draws, sharp at any size;
// labels are SVG text rather than HTML (htmlLabels off), which MyGo draws.
// An SVG with an element MyGo leaves out is drawn by mmdg as a picture
// instead. A block shows as code until its fence closes and while it is
// laid out, and stays code when the diagram does not render.

// tspMermaidScale is the pixels per point of a diagram drawn as a picture:
// sharp on a high-density screen, scaled down on others.
const tspMermaidScale = 2

// tspMermaid is a diagram drawn for a theme: what draws it once laid out
// (an SVG, or a picture), its size in DIPs, the kind of diagram, and
// whether the figure shows its source instead; or why it could not be
// drawn.
type tspMermaid struct {
	pic    ui.ImageSource
	w, h   float32
	kind   string
	source bool
	err    string
}

// tspMermaidSlots bounds the diagrams being drawn at once, as when a
// transcript with many of them loads.
var tspMermaidSlots = make(chan struct{}, 2)

// tspMermaidFor returns the diagram src drawn for the window's theme,
// starting to draw it when it is new; the last ones are kept, by source
// and theme.
func (a *App) tspMermaidFor(src string) *tspMermaid {
	key := fmt.Sprintf("%v/%s", a.dark, src)
	if m, ok := a.tspMermaids[key]; ok {
		return m
	}
	if a.tspMermaids == nil || len(a.tspMermaids) > 32 {
		a.tspMermaids = map[string]*tspMermaid{}
	}
	m := &tspMermaid{}
	a.tspMermaids[key] = m
	theme := "default"
	if a.dark {
		theme = "dark"
	}
	go func() {
		tspMermaidSlots <- struct{}{}
		defer func() { <-tspMermaidSlots }()
		kind, pic, w, h, err := renderMermaid(src, theme)
		if err != nil {
			log.Printf("mermaid: %v", err)
			a.laterService(func() { m.err = mermaidErr(err) })
			return
		}
		a.laterService(func() { m.pic, m.w, m.h, m.kind = pic, w, h, kind })
	}()
	return m
}

// renderMermaid lays a diagram out in mermaid's theme, with no background
// of its own, so that the figure's shows through it, and returns its kind
// and what draws it at w by h DIPs: its SVG, or, when the SVG has an
// element MyGo does not draw, mmdg's picture of it. Its labels are text
// (htmlLabels off), and an edge's label hides the edge under it: mermaid
// draws their background at half its opacity, which lets the line run
// through the text. A diagram that takes more than ten seconds is given
// up.
func renderMermaid(src, theme string) (kind string, pic ui.ImageSource, w, h float32, err error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	opts := []mermaid.Option{mermaid.WithTheme(theme), mermaid.WithBackgroundColor("transparent"),
		mermaid.WithConfig(map[string]any{
			"htmlLabels": false, "flowchart": map[string]any{"htmlLabels": false},
			"themeCSS": ".edgeLabel rect { opacity: 1; }",
		})}
	res, err := mermaid.RenderContext(ctx, src, opts...)
	if err != nil {
		return "", nil, 0, 0, err
	}
	if s, err := mermaidSVG(res.SVG, opts); err != nil {
		log.Printf("mermaid: a %s diagram: %v: drawn as a picture", res.Type, err)
	} else {
		w, h = s.Size()
		return res.Type, s, w, h, nil
	}
	png, err := mermaid.SVGToPNG(res.SVG, append(opts, mermaid.WithScale(tspMermaidScale))...)
	if err != nil {
		return "", nil, 0, 0, err
	}
	bm, err := ui.DecodeBitmap(png)
	if err != nil {
		return "", nil, 0, 0, err
	}
	pw, ph := bm.Size()
	return res.Type, bm, float32(pw) / tspMermaidScale, float32(ph) / tspMermaidScale, nil
}

// mermaidSVG parses a diagram's SVG for MyGo to draw; an error says why
// MyGo can't draw it, as with an element it leaves out.
func mermaidSVG(svg string, opts []mermaid.Option) (*ui.SVG, error) {
	doc, err := mermaid.StandaloneSVG(svg, opts...)
	if err != nil {
		return nil, err
	}
	s, err := ui.ParseSVG([]byte(doc))
	if err != nil {
		return nil, err
	}
	if left := mermaidLeftOut(s.Unsupported()); left != "" {
		return nil, fmt.Errorf("it has %s, which MyGo does not draw", left)
	}
	return s, nil
}

// mermaidLeftOut names the elements of a diagram's SVG that MyGo leaves
// out, of what it reports it does not draw, "" for none: the rest (the
// drop shadows' filters, animations, the CSS of looks the diagram does
// not use) changes little of a diagram.
func mermaidLeftOut(unsupported []string) string {
	var elems []string
	for _, u := range unsupported {
		if strings.HasPrefix(u, "<") {
			elems = append(elems, u)
		}
	}
	return strings.Join(elems, ", ")
}

// mermaidErr says in a line why a diagram could not be drawn: the first
// of the renderer's (the parse error and its line).
func mermaidErr(err error) string {
	if errors.Is(err, context.DeadlineExceeded) {
		return "it took too long"
	}
	line, _, _ := strings.Cut(err.Error(), "\n")
	return strings.TrimSuffix(strings.TrimSpace(line), ":")
}

// tspMermaidBlock builds a mermaid code block of the surface's Markdown in
// pane p: the diagram's figure once its picture is made, else the code;
// a diagram that can't be drawn shows its code under why.
func (a *App) tspMermaidBlock(c *ui.Context, k *colors, p *Pane, b *markdown.CodeBlockView) {
	if b.Open || !is(a.eff.Editor.Mermaid) {
		markdown.DefaultCodeBlock(c, b)
		return
	}
	m := a.tspMermaidFor(b.Code())
	if m.pic == nil && m.err == "" {
		markdown.DefaultCodeBlock(c, b)
		return
	}
	size := a.tspMsg(14)
	name := "Mermaid diagram"
	if m.kind != "" {
		name = "Mermaid " + m.kind
	}
	open := func() {
		a.zoomPicture(p, m.pic, m.w, m.h, name, "")
	}
	fig := ui.Column(c).AlignSelf(ui.Stretch).Background(k.card).Border(1, k.cardBorder).Radius(6).Clip()
	fig.Role(ui.RoleGroup).Label(name)
	fig.Children(func() {
		head := ui.Row(c).AlignItems(ui.Center).Gap(2).Padding(0.35*size, 0.4*size, 0, 0.8*size)
		head.Children(func() {
			label := "mermaid"
			if m.kind != "" {
				label += " · " + m.kind
			}
			ui.Text(c, label).FontSize(0.8*size).TextColor(k.textMuted).Unselectable().Shrink(0)
			if m.err != "" {
				ui.Text(c, " · Can't draw the diagram: "+m.err).FontSize(0.8*size).TextColor(k.attention).
					SingleLine().Ellipsis("…").Shrink(1).MinWidth(0).Tooltip(m.err)
			}
			ui.Spacer(c)
			if m.err == "" {
				toggle := "Source"
				if m.source {
					toggle = "Diagram"
				}
				if tspFigureButton(c, k, size, toggle).Clicked() {
					m.source = !m.source
				}
			}
			b.CopyButton(c)
			if m.err == "" && tspFigureButton(c, k, size, "Open").Clicked() {
				open()
			}
		})
		if m.source || m.err != "" {
			b.Body(c)
			return
		}
		body := ui.ScrollHorizontal(c).FillWidth().Padding(0.25*size, 0.8*size, 0.8*size, 0.8*size)
		body.Children(func() {
			// Centered while narrower than the figure; wider, it scrolls
			// from its left edge.
			img := ui.Image(c, m.pic).Size(m.w, m.h).Shrink(0).Margin(0, ui.Auto).Cursor(ui.CursorPointer)
			if img.Clicked() {
				open()
			}
		})
	})
}

// tspFigureButton is a button of a word in a figure's head, the size of
// a code block's Copy, whose em is size.
func tspFigureButton(c *ui.Context, k *colors, size float32, label string) ui.Element {
	btn := ui.ButtonBase(c.Key(label)).Padding(0.2*size, 0.45*size).Radius(5).Label(label)
	if btn.Hovered() {
		btn.Background(k.text.Alpha(0.08))
	}
	btn.Children(func() {
		ui.Text(c, label).FontSize(0.8*size).TextColor(k.textMuted)
	})
	return btn
}
