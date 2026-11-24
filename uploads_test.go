package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/egoist/mygo"
)

// TestUploadDrop: files and a folder dropped on a folder of the host's.
// A file whose name exists, kept both, comes in with the next free
// number; a folder of a name that exists goes into it, where a file of a
// name that exists, replaced, is replaced; the rest is new, and keeps its
// modification time. The paths given back are the dropped items' as
// named on the host.
func TestUploadDrop(t *testing.T) {
	a, tt := newTestApp(t)
	a.eff = builtin
	root := testDir(t)
	defer os.RemoveAll(root)
	local, dest := filepath.Join(root, "local"), filepath.Join(root, "dest")
	for _, d := range []string{local, filepath.Join(local, "sub", "deep"), filepath.Join(dest, "sub")} {
		os.MkdirAll(d, 0o755)
	}
	write := func(path, text string) { os.WriteFile(path, []byte(text), 0o644) }
	write(filepath.Join(local, "a.txt"), "new a")
	write(filepath.Join(local, "sub", "b.txt"), "new b")
	write(filepath.Join(local, "sub", "deep", "c.txt"), "new c")
	write(filepath.Join(dest, "a.txt"), "old a")
	write(filepath.Join(dest, "sub", "b.txt"), "old b")
	mod := time.Date(2023, 5, 6, 7, 8, 9, 0, time.UTC)
	os.Chtimes(filepath.Join(local, "sub", "deep", "c.txt"), mod, mod)

	var asked []string
	defer func(old func(mygo.MessageOptions) (mygo.MessageResult, error)) { messageBox = old }(messageBox)
	messageBox = func(o mygo.MessageOptions) (mygo.MessageResult, error) {
		asked = append(asked, o.Title)
		if len(asked) == 1 {
			return mygo.MessageResult{Button: 1}, nil // Keep Both
		}
		return mygo.MessageResult{Button: 0}, nil // Replace
	}
	var top []string
	got := false
	a.uploadDrop([]string{filepath.Join(local, "a.txt"), filepath.Join(local, "sub")}, dest, func(t []string) { top, got = t, true })
	waitFor(t, tt, "the upload", func() bool { return got })

	read := func(names ...string) string {
		b, _ := os.ReadFile(filepath.Join(append([]string{dest}, names...)...))
		return string(b)
	}
	for _, c := range [][2]string{
		{read("a.txt"), "old a"}, {read("a (1).txt"), "new a"},
		{read("sub", "b.txt"), "new b"}, {read("sub", "deep", "c.txt"), "new c"},
	} {
		if c[0] != c[1] {
			t.Errorf("%q, want %q", c[0], c[1])
		}
	}
	if len(asked) != 2 || asked[0] != "a.txt already exists in dest" || asked[1] != "b.txt already exists in sub" {
		t.Errorf("asked %q", asked)
	}
	if fi, err := os.Stat(filepath.Join(dest, "sub", "deep", "c.txt")); err != nil || !fi.ModTime().Equal(mod) {
		t.Errorf("c.txt's time: %v %v, want %v", fi, err, mod)
	}
	if want := []string{filepath.Join(dest, "a (1).txt"), filepath.Join(dest, "sub")}; len(top) != 2 || top[0] != want[0] || top[1] != want[1] {
		t.Errorf("top %q, want %q", top, want)
	}
}

// TestFreeName: the next free name of a file keeps its extensions; a
// folder's is its whole name.
func TestFreeName(t *testing.T) {
	existing := map[string]bool{"notes.txt": false, "notes (1).txt": false, "my.folder": true}
	for _, c := range []struct {
		name string
		dir  bool
		want string
	}{
		{"notes.txt", false, "notes (2).txt"},
		{"archive.tar.gz", false, "archive (1).tar.gz"},
		{".bashrc", false, ".bashrc (1)"},
		{"my.folder", true, "my.folder (1)"},
	} {
		if got := freeName(c.name, c.dir, existing); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}
