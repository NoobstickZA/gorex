package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"
	"github.com/egoist/mygo/ui/editor"
	"github.com/egoist/mygo/ui/markdown/parser"

	"gorex/internal/rex"
)

// Lines of a file that one of omp's tool calls read or wrote. A click on
// the file's name in the call's card opens the file at them, coloured:
// green for a read, amber for an edit. The editor colours the lines;
// MyGo's preview and the web view's colour the blocks of Markdown the
// lines are in. Each scrolls to them the first time it shows them. An
// edit of the file, or the file read again, takes the colour away, as the
// lines it names may have moved.

// lineMarks are the lines a tool call named, from 0: ranges, inclusive
// and in order, to colour, and line, the one to go to. With no ranges
// the view only goes to line.
type lineMarks struct {
	ranges []lineRange
	line   int
	wrote  bool
	// buf and version are the buffer the marks were first shown on and
	// its version then.
	buf     *editor.Buffer
	version uint64
	// Whether the editor, the web view's page and MyGo's preview have
	// shown the marks; editorScroll and nativeScroll ask the editor and
	// MyGo's preview to scroll to them once their lines are laid out.
	inEditor, editorScroll, inWeb, inNative, nativeScroll bool
}

// markTop is how far down the view the line to go to comes to rest, as a
// fraction of its height: with some of what comes before above it.
const markTop = 0.3

type lineRange struct{ first, last int }

// hits reports whether lines first to last hold a marked line.
func (m *lineMarks) hits(first, last int) bool {
	for _, r := range m.ranges {
		if r.first <= last && first <= r.last {
			return true
		}
	}
	return false
}

// colors are the wash the marked lines take and the bar beside them.
func (m *lineMarks) colors(k *colors) (wash, bar ui.Color) {
	bar = k.busy
	if m.wrote {
		bar = k.attention
	}
	return bar.Alpha(0.14), bar
}

// toolLineMarks are the lines tool call n's card names in file: those an
// edit wrote, from the hunks of its diff; those a read read, from the
// range after the file's name in the card's head (api.go:225-280); for
// any other call with a line there, that line to go to; nil for none.
func toolLineMarks(n *rex.TSPNode, file string) *lineMarks {
	if hunks, ok := toolHunks(n, baseName(file)); ok {
		return hunkMarks(hunks)
	}
	target, _ := n.Props["target"].(string)
	name, _ := n.Props["name"].(string)
	return textLineMarks(tspPlain(n.Props["target"], target), file, name)
}

// textLineMarks are the lines text names after file's name, as a card's
// head or one of its file rows shows them (design.md:626-632), in a card
// of the tool named tool: coloured as read in a read's card, else only
// the first to go to; nil for none.
func textLineMarks(text, file, tool string) *lineMarks {
	ranges := selRanges(targetSel(text, baseName(file)))
	if len(ranges) == 0 {
		return nil
	}
	m := &lineMarks{line: ranges[0].first}
	if tool == "read" {
		m.ranges = ranges
	}
	return m
}

// toolHunks finds the hunks of the diff in tool call n's body that is of
// the file named base, or of no file named.
func toolHunks(n *rex.TSPNode, base string) ([]any, bool) {
	for _, ch := range n.Children {
		if ch.Kind == "diff" {
			hunks, _ := ch.Props["hunks"].([]any)
			path, _ := ch.Props["path"].(string)
			if len(hunks) > 0 && (path == "" || baseName(path) == base) {
				return hunks, true
			}
			continue
		}
		if h, ok := toolHunks(ch, base); ok {
			return h, true
		}
	}
	return nil, false
}

// hunkMarks are the lines a diff's hunks added, in the file as it was
// written: each hunk counts from its newStart, from 1, through its
// context and added lines. The line to go to is where the first hunk
// changed something, its first added or removed line.
func hunkMarks(hunks []any) *lineMarks {
	m := &lineMarks{wrote: true, line: -1}
	for _, h := range hunks {
		h, _ := h.(map[string]any)
		start, _ := h["newStart"].(float64)
		lines, _ := h["lines"].([]any)
		n := max(int(start), 1) - 1
		for _, l := range lines {
			s, _ := l.(string)
			if s == "" {
				n++
				continue
			}
			switch s[0] {
			case '+':
				if m.line < 0 {
					m.line = n
				}
				if k := len(m.ranges) - 1; k >= 0 && m.ranges[k].last == n-1 {
					m.ranges[k].last = n
				} else {
					m.ranges = append(m.ranges, lineRange{n, n})
				}
				n++
			case '-':
				if m.line < 0 {
					m.line = n
				}
			default:
				n++
			}
		}
	}
	if m.line < 0 {
		return nil
	}
	return m
}

// targetSel is what follows the file's name in a card's target, after
// its colon: "225-280" of "conf/api.go:225-280"; "" for nothing.
func targetSel(target, base string) string {
	i := strings.LastIndex(target, base)
	if base == "" || i < 0 {
		return ""
	}
	rest := target[i+len(base):]
	if !strings.HasPrefix(rest, ":") {
		return ""
	}
	return rest[1:]
}

// selRanges are the lines a read's selection names, from 0: N, N-M and
// N+C (C lines from N), separated by commas or colons. What it can't
// place, as the last N lines (-N) or raw, it leaves out.
func selRanges(sel string) []lineRange {
	var out []lineRange
	for _, part := range strings.FieldsFunc(sel, func(r rune) bool { return r == ',' || r == ':' || r == ' ' }) {
		first, last := part, ""
		count := false
		if i := strings.IndexAny(part, "-+"); i >= 0 {
			first, last, count = part[:i], part[i+1:], part[i] == '+'
		}
		a, err := strconv.Atoi(first)
		if err != nil || a < 1 {
			continue
		}
		b := a
		if last != "" {
			n, err := strconv.Atoi(last)
			if err != nil || n < 1 {
				continue
			}
			b = n
			if count {
				b = a + n - 1
			}
		}
		if b < a {
			continue
		}
		out = append(out, lineRange{a - 1, b - 1})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].first < out[j].first })
	return out
}

// markLines shows a view's file at the lines m names, in place of any it
// showed.
func (a *App) markLines(e *fileView, m *lineMarks) {
	if e == nil || m == nil {
		return
	}
	a.unmarkLines(e)
	e.marks = m
	a.changed()
}

// unmarkLines takes a view's marks away.
func (a *App) unmarkLines(e *fileView) {
	m := e.marks
	if m == nil {
		return
	}
	e.marks = nil
	if m.inEditor && e.state != nil && e.state.Buffer == m.buf {
		e.state.LineBackgrounds = nil
	}
	if m.inWeb && e.mode == modePreview && previewSettled(e) && e.preview.showing == "" {
		a.previewEval(e, "return gorexMarkLines([])")
	}
}

// showLineMarks shows a view's marks in the mode it is in, as it is
// drawn, and takes them away once the buffer is edited or read again.
func (a *App) showLineMarks(e *fileView, k *colors) {
	m := e.marks
	if m == nil || e.buf == nil {
		return
	}
	if m.buf == nil {
		m.buf, m.version = e.buf, e.buf.Version()
		// Lines past the end of the file, as a read past it names.
		n := e.buf.LineCount()
		kept := m.ranges[:0]
		for _, r := range m.ranges {
			if r.first < n {
				kept = append(kept, lineRange{r.first, min(r.last, n-1)})
			}
		}
		m.ranges = kept
		m.line = min(m.line, max(n-1, 0))
	} else if m.buf != e.buf || m.version != e.buf.Version() {
		a.unmarkLines(e)
		return
	}
	switch e.mode {
	case modeEditor:
		st := e.state
		switch {
		case !m.inEditor:
			m.inEditor, m.editorScroll = true, true
			wash, _ := m.colors(k)
			st.LineBackgrounds = st.LineBackgrounds[:0]
			for _, r := range m.ranges {
				st.LineBackgrounds = append(st.LineBackgrounds, editor.LineBackground{First: r.first, Last: r.last, Color: wash})
			}
			st.GoTo(editor.Pos{Line: m.line})
			a.changed()
		case m.editorScroll && st.ViewportHeight() > 0:
			m.editorScroll = false
			st.SetScrollY(max(st.LineTop(m.line)-markTop*st.ViewportHeight(), 0))
		}
	case modePreview:
		if !m.inWeb && previewSettled(e) && e.preview.showing == "" {
			m.inWeb = true
			a.previewEval(e, a.markLinesJS(m, k, true))
		}
	case modeNative:
		if !m.inNative {
			m.inNative, m.nativeScroll = true, true
		}
	}
}

// markLinesJS is the page's call that colours the blocks of m's lines,
// scrolling to the first when asked.
func (a *App) markLinesJS(m *lineMarks, k *colors, scroll bool) string {
	ranges := make([][2]int, 0, len(m.ranges))
	for _, r := range m.ranges {
		ranges = append(ranges, [2]int{r.first, r.last})
	}
	js, _ := json.Marshal(ranges)
	wash, bar := m.colors(k)
	line := -1
	if scroll {
		line = m.line
	}
	return fmt.Sprintf("return gorexMarkLines(%s, %q, %q, %d, %g)", js, rgbaCSS(wash), rgbaCSS(bar), line, markTop)
}

// rgbaCSS is a color as CSS's rgba().
func rgbaCSS(c ui.Color) string {
	return fmt.Sprintf("rgba(%d, %d, %d, %.3f)", c.R, c.G, c.B, float64(c.A)/255)
}

// nativeLeaf reports whether MyGo's preview builds a block of this kind
// with no blocks in it, which the marks colour as a whole.
func nativeLeaf(k parser.Kind) bool {
	switch k {
	case parser.Paragraph, parser.Heading, parser.ThematicBreak, parser.CodeBlock, parser.HTMLBlock, parser.MathBlock, parser.Table:
		return true
	}
	return false
}

// lineOf is the line, from 0, of an offset in a text whose line starts
// are starts.
func lineOf(starts []int, off int) int {
	return sort.Search(len(starts), func(i int) bool { return starts[i] > off }) - 1
}

// lineStarts are the offsets the lines of a text start at.
func lineStarts(text string) []int {
	starts := []int{0}
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			starts = append(starts, i+1)
		}
	}
	return starts
}

// nativeLines are the lines block i of the preview's document spans.
func (np *nativePreview) nativeLines(d *parser.Document, i int) (first, last int) {
	if np.starts == nil {
		np.starts = lineStarts(d.Source())
	}
	b := d.Block(i)
	return lineOf(np.starts, b.Start), lineOf(np.starts, max(b.End-1, b.Start))
}

// nativeMarkBlock colours block i of MyGo's preview when the view's marks
// hold one of its lines: a wash behind it and a bar at its start, a
// little outside its text. The first block that holds or follows the
// line to go to, as the blocks come in the document's order, is scrolled
// to markTop of the view, once. It builds nothing in the block's place.
func (a *App) nativeMarkBlock(c *ui.Context, e *fileView, np *nativePreview, d *parser.Document, i int) bool {
	m := e.marks
	if m == nil || !nativeLeaf(d.Block(i).Kind) {
		return false
	}
	first, last := np.nativeLines(d, i)
	if m.hits(first, last) {
		wash, bar := m.colors(np.k)
		ui.Box(c).Absolute().Left(-12).Right(-12).Top(-3).Bottom(-3).Radius(4).
			Background(wash).BorderWidth(0, 0, 0, 3).BorderColor(bar)
	}
	if m.nativeScroll && last >= m.line {
		if np.viewH <= 0 {
			a.changed() // the view's height comes with its first layout
			return false
		}
		m.nativeScroll = false
		// A point above the block, by markTop of the view, comes to its
		// top.
		ui.Box(c).Absolute().Left(0).Top(-markTop*np.viewH).Size(1, 1).
			ScrollIntoView(ui.ScrollIntoViewOptions{Block: ui.ScrollStart})
	}
	return false
}
