package main

import (
	"os"
	"path/filepath"
	"testing"
)

// TestFilePanel: a terminal tab's file panel opens on the terminal's
// folder, follows the shell's cd while the shell is at its prompt but not
// while a program runs, stays put while pinned, shows a file in the popup
// over the terminal on a click, and is kept in the layout.
func TestFilePanel(t *testing.T) {
	a, tt := newTestApp(t)
	a.eff = builtin
	root := testDir(t)
	defer os.RemoveAll(root)
	sub := filepath.Join(root, "sub")
	os.MkdirAll(sub, 0o755)
	os.WriteFile(filepath.Join(root, "top.txt"), []byte("top\n"), 0o644)
	os.WriteFile(filepath.Join(sub, "notes.txt"), []byte("notes\n"), 0o644)

	tab := a.tab()
	p := tab.Root.Pane
	waitFor(t, tt, "the shell", func() bool { return p.term != nil && p.term.Text() != "" })
	// cdHere types the shell's own cd; the server tells the new folder
	// once the shell is back at its prompt.
	at := func(dir string) func() bool {
		return func() bool {
			refresh(a)
			return p.info.Idle && samePath(p.info.Dir, dir)
		}
	}
	waitFor(t, tt, "the shell at its prompt", func() bool { refresh(a); return p.info.Idle })
	a.cdHere(tab, root)
	waitFor(t, tt, "the shell in the test folder", at(root))

	a.toggleFiles()
	if !tab.showsFiles() || tab.Focus != tab.Browser {
		t.Fatalf("the panel does not show, or has no focus: open %v, focus %+v", tab.FilesOpen, tab.Focus)
	}
	b := tab.Browser.files
	if !samePath(b.root, root) {
		t.Fatalf("the panel opened on %q, not the terminal's %q", b.root, root)
	}
	waitFor(t, tt, "the root's entries", func() bool { return tt.HasText("sub") && tt.HasText("top.txt") })

	// The shell's cd moves the panel.
	a.cdHere(tab, sub)
	waitFor(t, tt, "the shell in sub", at(sub))
	waitFor(t, tt, "the panel to follow", func() bool { tt.Frame(); return samePath(b.root, sub) })

	// A click on a file shows it in the popup over the terminal.
	waitFor(t, tt, "sub's entries", func() bool { return tt.HasText("notes.txt") })
	if err := tt.Click("notes.txt"); err != nil {
		t.Fatal(err)
	}
	if a.peek == nil || a.peek.over != p || !samePath(a.peek.view.path, filepath.Join(sub, "notes.txt")) {
		t.Fatalf("the click showed %+v", a.peek)
	}
	a.dropPeek()

	// While a program runs, the folder it works in leaves the root alone;
	// back at the prompt, the panel follows.
	p.term.Send([]byte(testShell.run + "\r"))
	waitFor(t, tt, "the program", func() bool {
		refresh(a)
		return !p.info.Idle && samePath(p.info.Dir, testShell.dir)
	})
	tt.Frame()
	if !samePath(b.root, sub) {
		t.Errorf("the panel moved to %q while a program ran", b.root)
	}
	waitFor(t, tt, "the program to end", at(testShell.dir))
	waitFor(t, tt, "the panel to follow after the program", func() bool { tt.Frame(); return samePath(b.root, testShell.dir) })

	// Pinned, it stays; unpinned, it follows at once.
	a.togglePin(tab.Browser)
	a.cdHere(tab, root)
	waitFor(t, tt, "the shell back in the test folder", at(root))
	tt.Frame()
	if !samePath(b.root, testShell.dir) {
		t.Errorf("the pinned panel moved to %q", b.root)
	}
	a.togglePin(tab.Browser)
	tt.Frame()
	if !samePath(b.root, root) {
		t.Errorf("the unpinned panel is at %q, not %q", b.root, root)
	}

	// The layout keeps the panel, and hiding it.
	st := a.snapshot().Tabs[a.active]
	if st.Browser == nil || !st.Files || !samePath(st.Browser.Root, root) {
		t.Errorf("the layout keeps %+v, files %v", st.Browser, st.Files)
	}
	a.toggleFiles()
	if tab.showsFiles() || tab.Focus == tab.Browser {
		t.Error("the hidden panel shows, or keeps the focus")
	}
	if st := a.snapshot().Tabs[a.active]; st.Files || st.Browser == nil {
		t.Errorf("the layout of the hidden panel: files %v, browser %+v", st.Files, st.Browser)
	}
}

// TestCdCommand: the cd the file panel types quotes the folder as each
// shell takes it.
func TestCdCommand(t *testing.T) {
	for _, c := range []struct{ shell, dir, want string }{
		{"cmd", `C:\Program Files`, `cd /d "C:\Program Files"`},
		{"pwsh", `C:\it's`, `cd 'C:\it''s'`},
		{"powershell", `C:\a b`, `cd 'C:\a b'`},
		{"bash", "/home/me/it's here", `cd '/home/me/it'\''s here'`},
		{"zsh", "/tmp/a b", `cd '/tmp/a b'`},
	} {
		if got := cdCommand(c.shell, c.dir); got != c.want {
			t.Errorf("%s, %q: %q, want %q", c.shell, c.dir, got, c.want)
		}
	}
}
