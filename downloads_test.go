package main

import (
	"io"
	"os"
	"path/filepath"
	"testing"
	"time"

	"gorex/internal/rex"
)

// TestDragOut: a folder of the host's dragged out lists itself and all it
// holds, folders before what they hold, with sizes; a file of it reads
// through a download; and the copy offered where there are no virtual
// files is the folder, whole, with its files' times, once the file
// manager asks for it.
func TestDragOut(t *testing.T) {
	a, _ := newTestApp(t)
	a.eff = builtin
	root := testDir(t)
	defer os.RemoveAll(root)
	docs := filepath.Join(root, "docs")
	os.MkdirAll(filepath.Join(docs, "sub"), 0o755)
	os.WriteFile(filepath.Join(docs, "a.md"), []byte("alpha"), 0o644)
	os.WriteFile(filepath.Join(docs, "sub", "b.md"), []byte("beta!"), 0o644)
	mod := time.Date(2022, 1, 2, 3, 4, 5, 0, time.UTC)
	os.Chtimes(filepath.Join(docs, "sub", "b.md"), mod, mod)

	b := &browserState{dirs: map[string][]rex.FileInfo{}}
	entries, err := a.dragEntries(a.client, b, docs)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int64{"docs": -1, "docs/a.md": 5, "docs/sub": -1, "docs/sub/b.md": 5}
	if len(entries) != len(want) || entries[0].rel != "docs" || !entries[0].dir {
		t.Fatalf("entries %+v", entries)
	}
	seen := map[string]int{}
	for i, e := range entries {
		seen[e.rel] = i
		if size, ok := want[e.rel]; !ok || (size >= 0 && e.size != size) || e.dir != (size < 0) {
			t.Errorf("entry %+v", e)
		}
	}
	if seen["docs/sub"] > seen["docs/sub/b.md"] {
		t.Error("a folder came after what it holds")
	}

	r := downloadReader(a.client, filepath.Join(docs, "a.md"))
	got, err := io.ReadAll(r)
	r.Close()
	if err != nil || string(got) != "alpha" {
		t.Errorf("download read %q, %v", got, err)
	}

	data := a.dragCopy(a.client, docs, entries)
	paths, err := data.Files()
	if err != nil || len(paths) != 1 || filepath.Base(paths[0]) != "docs" {
		t.Fatalf("the copy's files %q, %v", paths, err)
	}
	defer os.RemoveAll(filepath.Dir(paths[0]))
	copied := filepath.Join(paths[0], "sub", "b.md")
	if b, _ := os.ReadFile(copied); string(b) != "beta!" {
		t.Errorf("the copy of b.md: %q", b)
	}
	if fi, err := os.Stat(copied); err != nil || !fi.ModTime().Equal(mod) {
		t.Errorf("the copy's time: %v %v, want %v", fi, err, mod)
	}
}
