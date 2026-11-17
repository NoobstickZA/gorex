package main

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"gorex/internal/rex"
)

// Code in a message is asked about as a file only when it looks like a
// file's name: a name with an extension of letters, or a path; not a
// version, a number, a command, an option or an address.
func TestLooksLikeFile(t *testing.T) {
	for _, c := range []struct {
		code string
		want bool
	}{
		{"FOO.md", true},
		{"main.go", true},
		{"internal/rex/tsp.go", true},
		{"docs/", false}, // a folder, which opens in no popup
		{"src/cmd", true},
		{`C:\experiment\TSP.md`, true},
		{"v1.2", false},
		{"0.95", false},
		{"e.g.", false},
		{"Makefile", false},
		{"go build ./...", false},
		{"--force", false},
		{"https://example.com/a.md", false},
		{"*.go", false},
		{"", false},
	} {
		if got := looksLikeFile(c.code); got != c.want {
			t.Errorf("looksLikeFile(%q) = %v, want %v", c.code, got, c.want)
		}
	}
}

// A file a message names before it is written, as an agent does, links
// once it is there: the name not found is asked about again a moment
// later.
func TestCodeLinkFileWrittenLater(t *testing.T) {
	a, tt := newTestApp(t)
	a.eff = builtin
	hostGOOS = runtime.GOOS
	root := testDir(t)
	defer os.RemoveAll(root)
	p := &Pane{info: rex.SessionInfo{Dir: root}}
	link := a.tspCodeLinker(p)
	asked := func() bool {
		_, ok := link("NOTES.md")
		return ok
	}
	// Asked while it is not there: no link, the answer in.
	asked()
	waitFor(t, tt, "the first answer", func() bool { ans := a.fileKnown[filepath.Join(root, "NOTES.md")]; return ans != nil && !ans.asking })
	if asked() {
		t.Fatal("a missing file links")
	}
	os.WriteFile(filepath.Join(root, "NOTES.md"), []byte("tea\n"), 0o644)
	deadline := time.Now().Add(fileAskFirst + 3*time.Second)
	for !asked() {
		if time.Now().After(deadline) {
			t.Fatal("the file written after its name was asked about never links")
		}
		time.Sleep(50 * time.Millisecond)
		tt.Frame()
	}
}
