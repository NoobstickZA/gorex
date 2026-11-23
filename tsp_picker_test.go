package main

import (
	"fmt"
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestPickerScrollsPastSelected: the wheel scrolls a picker's rows away
// from the one selected, which stays where it is scrolled to; a row that
// becomes the selected one comes into view.
func TestPickerScrollsPastSelected(t *testing.T) {
	a := &App{eff: builtin}
	selected := "i0"
	tt := ui.NewTester(func(c *ui.Context) {
		k := colorsOf(c)
		ui.Scroll(c.Key("list")).Height(200).FillWidth().Children(func() {
			ui.Column(c).FillWidth().Children(func() {
				for i := range 60 {
					id := fmt.Sprintf("i%d", i)
					a.tspPickerRow(c.Key(id), k, map[string]any{"id": id, "label": "Row " + id}, "", nil, nil, id == selected, false)
				}
			})
		})
	}, 600, 400)
	tt.Frame()
	at := func(label string) float32 {
		t.Helper()
		r, ok := tt.Find(label)
		if !ok {
			t.Fatalf("no %q", label)
		}
		return r.Y
	}
	before := at("Row i0")
	tt.Scroll(300, 100, 0, 400)
	tt.Frame()
	tt.Frame()
	if after := at("Row i0"); after == before {
		t.Fatalf("the wheel did not move the rows away from the selected one (at %v)", after)
	}
	moved := at("Row i0")
	tt.Frame()
	if again := at("Row i0"); again != moved {
		t.Errorf("the rows went back from %v to %v", moved, again)
	}

	selected = "i50"
	tt.Frame()
	tt.Frame()
	if y := at("Row i50"); y < 0 || y > 200 {
		t.Errorf("the selected row is at %v, out of the list's view", y)
	}
}
