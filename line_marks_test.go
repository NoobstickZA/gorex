package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/egoist/mygo/ui/editor"
	"github.com/egoist/mygo/ui/markdown/parser"

	"gorex/internal/rex"
)

// TestToolLineMarks: the lines a tool card names for its file. A read's
// range follows the file's name in the head; an edit's lines are the
// added lines of its diff's hunks, counted from each hunk's newStart; a
// diff of another file, or another tool's line, only goes to the line.
func TestToolLineMarks(t *testing.T) {
	file := "/src/goppp/internal/conf/api.go"
	read := func(target string) *rex.TSPNode {
		return &rex.TSPNode{Kind: "tool", Props: map[string]any{"name": "read", "target": []any{map[string]any{"t": target}}}}
	}
	edit := func(path string, hunks ...any) *rex.TSPNode {
		return &rex.TSPNode{Kind: "tool", Props: map[string]any{"name": "edit", "target": "conf/api.go:11"},
			Children: []*rex.TSPNode{{Kind: "col", Children: []*rex.TSPNode{{Kind: "diff", Props: map[string]any{"path": path, "hunks": hunks}}}}}}
	}
	hunk := func(newStart int, lines ...any) any {
		return map[string]any{"oldStart": float64(newStart), "newStart": float64(newStart), "lines": lines}
	}
	for _, c := range []struct {
		name  string
		n     *rex.TSPNode
		want  []lineRange
		line  int
		wrote bool
	}{
		{"a range", read("goppp/internal/conf/api.go:225-280"), []lineRange{{224, 279}}, 224, false},
		{"one line", read("conf/api.go:50"), []lineRange{{49, 49}}, 49, false},
		{"a count", read("api.go:50+3"), []lineRange{{49, 51}}, 49, false},
		{"several", read("api.go:960-973,5-16"), []lineRange{{4, 15}, {959, 972}}, 4, false},
		{"two hunks", edit("conf/api.go",
			hunk(10, " a", "-b", "+c", "+d", " e"),
			hunk(30, " f", "-x", " g"),
			hunk(40, "+y")), []lineRange{{10, 11}, {39, 39}}, 10, true},
		{"removed first", edit("", hunk(5, "-gone", " kept", "+new")), []lineRange{{5, 5}}, 4, true},
		{"another file's diff", edit("other.go", hunk(10, "+c")), nil, 10, false},
	} {
		m := toolLineMarks(c.n, file)
		if m == nil {
			t.Errorf("%s: no marks", c.name)
			continue
		}
		if !reflect.DeepEqual(m.ranges, c.want) || m.line != c.line || m.wrote != c.wrote {
			t.Errorf("%s: ranges %v line %d wrote %v, want %v %d %v", c.name, m.ranges, m.line, m.wrote, c.want, c.line, c.wrote)
		}
	}
	for _, target := range []string{"api.go", "api.go:-60", "api.go:raw", "other.go:12"} {
		if m := toolLineMarks(read(target), file); m != nil {
			t.Errorf("%q: %+v, want none", target, m)
		}
	}
}

// TestFileRowLineMarks: a row of a read's files (folder, name, range, as
// omp draws them) opens its file at its range, coloured as read; in
// another tool's card, the row only goes to the first line.
func TestFileRowLineMarks(t *testing.T) {
	for _, c := range []struct {
		row, file, tool string
		want            []lineRange
		line            int
	}{
		{"nicman/docs/design.md:626-632", "/src/nicman/docs/design.md", "read", []lineRange{{625, 631}}, 625},
		{"nicman/README.md:115-118", "/src/nicman/README.md", "read", []lineRange{{114, 117}}, 114},
		{"main.go:12", "/src/main.go", "search", nil, 11},
	} {
		m := textLineMarks(c.row, c.file, c.tool)
		if m == nil || !reflect.DeepEqual(m.ranges, c.want) || m.line != c.line || m.wrote {
			t.Errorf("%q in %s: %+v, want %v from line %d", c.row, c.tool, m, c.want, c.line)
		}
	}
	if m := textLineMarks("nicman/docs/json.md", "/src/nicman/docs/json.md", "read"); m != nil {
		t.Errorf("a row without a range: %+v", m)
	}
}

// TestLineMarksInEditor: a file opened at the lines a read named shows
// them coloured with the caret on the first; an edit takes the colour
// away, as the lines may have moved.
func TestLineMarksInEditor(t *testing.T) {
	a, tt := newTestApp(t)
	a.eff = builtin
	root := testDir(t)
	defer os.RemoveAll(root)
	file := filepath.Join(root, "a.go")
	os.WriteFile(file, []byte(strings.Repeat("x\n", 39)+"x"), 0o644)
	e := a.openPeek(a.tab().Focus, file)
	a.markLines(e, &lineMarks{ranges: []lineRange{{4, 9}, {38, 60}}, line: 4})
	waitFor(t, tt, "the popup's file", func() bool { return e.buf != nil })
	tt.Frame()
	got := e.state.LineBackgrounds
	if len(got) != 2 || got[0].First != 4 || got[0].Last != 9 || got[1].First != 38 || got[1].Last != 39 {
		t.Fatalf("backgrounds %+v, want 4-9 and 38-39 (the file's end)", got)
	}
	if line := e.state.CursorPos().Line; line != 4 {
		t.Errorf("caret on line %d, want 4", line)
	}
	e.buf.Edit(editor.Edit{Start: 0, End: 0, Text: "y\n"})
	tt.Frame()
	if e.marks != nil || len(e.state.LineBackgrounds) != 0 {
		t.Errorf("after an edit: marks %v, backgrounds %+v", e.marks, e.state.LineBackgrounds)
	}
}

// TestNativeLineMarks: MyGo's preview colours the blocks that hold a
// marked line, and no others.
func TestNativeLineMarks(t *testing.T) {
	src := "# Title\n\nFirst paragraph,\ntwo lines.\n\n- one\n- two\n\n```\ncode\n```\n"
	d := parser.New(parser.Default)
	d.Set(src)
	m := &lineMarks{ranges: []lineRange{{3, 3}, {9, 9}}, line: 3}
	np := &nativePreview{}
	var marked []string
	for i := 1; i < d.Len(); i++ {
		if first, last := np.nativeLines(d, i); nativeLeaf(d.Block(i).Kind) && m.hits(first, last) {
			marked = append(marked, strings.TrimSpace(src[d.Block(i).Start:d.Block(i).End]))
		}
	}
	if want := []string{"First paragraph,\ntwo lines.", "```\ncode\n```"}; !reflect.DeepEqual(marked, want) {
		t.Errorf("marked %q, want %q", marked, want)
	}
}
