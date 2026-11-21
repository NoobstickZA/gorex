package main

import (
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// A terminal tab's file panel, as Tern's: the tab's file browser, left of
// its panes, rooted in the focused terminal's folder and following it as
// the shell changes directory, until its root is pinned. A click on a
// file shows it in the popup over the terminal (openPeek).

// toggleFiles shows or hides the active terminal tab's file panel. It
// first opens on the focused terminal's folder; hidden, it keeps its
// root, pin and open folders for when it shows again.
func (a *App) toggleFiles() {
	t := a.tab()
	if t == nil || t.Kind != paneTerminal {
		return
	}
	if t.FilesOpen {
		t.FilesOpen = false
		if t.Focus == t.Browser {
			t.setFocus(t.treeFocus())
			a.focusReq = t.Focus
		}
		a.changed()
		return
	}
	if a.bind.noFiles() {
		a.toast(ui.Toast{ID: "plain/files", Title: "No files here", Description: a.noFilesReason() + ".", Timeout: 4 * time.Second})
		return
	}
	if t.Browser == nil {
		root := hostHome
		if p := t.treeFocus(); p != nil {
			if p.info.Dir != "" {
				root = p.info.Dir
			} else if p.startDir != "" {
				root = p.startDir
			}
		}
		t.Browser = a.newBrowserPane(t, &savedBrowser{Root: root, Hidden: is(a.eff.Editor.HiddenFiles)})
	}
	t.FilesOpen = true
	t.setFocus(t.Browser)
	a.focusReq = t.Browser
	a.changed()
}

// followTerminal moves a terminal tab's file panel to the folder of the
// tab's focused terminal, as the shell's cd changes it: while the shell
// is at its prompt, not while a program runs, which may change folders
// of its own, and not while the root is pinned or being changed. A folder
// the server can't tell, as a root shell's, leaves the root as it is.
func (a *App) followTerminal(t *Tab, p *Pane) {
	b := p.files
	if t.Kind != paneTerminal || b.pinned || b.changing {
		return
	}
	f := t.treeFocus()
	if f == nil || f.Kind != paneTerminal || !f.info.Idle || f.info.Dir == "" || samePath(f.info.Dir, b.root) {
		return
	}
	a.followRoot(p, f.info.Dir)
}

// followRoot moves a file panel to root, keeping which folders were open
// under the root it leaves, for the last few roots, to open them again
// on coming back.
func (a *App) followRoot(p *Pane, root string) {
	b := p.files
	if b.seen == nil {
		b.seen = map[string][]string{}
	}
	var open []string
	for d := range b.tree.Open.All() {
		if d != b.root {
			open = append(open, d)
		}
	}
	if len(b.seen) >= 8 {
		for k := range b.seen {
			delete(b.seen, k)
			break
		}
	}
	b.seen[b.root] = open
	a.setRoot(p, root)
	for _, d := range b.seen[b.root] {
		b.isDir[d] = true
		b.tree.Open.Add(d)
	}
}

// pinRoot moves a file panel to root and pins it there, as a root the
// user chose stays chosen.
func (a *App) pinRoot(p *Pane, root string) {
	a.setRoot(p, root)
	if p.Tab != nil && p.Tab.Kind == paneTerminal {
		p.files.pinned = true
	}
}

// panelClick acts on a click on a file panel's row: it chooses the row,
// shows a file in the popup and opens or closes a folder, as Tern's.
// The row takes the click from the list, so it chooses the row itself.
func (a *App) panelClick(t *Tab, p *Pane, item string) {
	b := p.files
	for i := range b.tree.Rows() {
		if b.tree.Item(i) == item {
			b.row = i
			break
		}
	}
	t.setFocus(p)
	if !b.isDir[item] {
		a.peekFromPanel(t, item)
		return
	}
	if b.tree.Open.Has(item) {
		b.tree.Open.Remove(item)
	} else {
		b.tree.Open.Add(item)
	}
	a.changed()
}

// peekFromPanel shows a file of a terminal tab's panel in the popup, over
// the tab's focused terminal.
func (a *App) peekFromPanel(t *Tab, path string) {
	if over := t.treeFocus(); over != nil {
		a.openPeek(over, path)
	}
}

// panelMenu is the menu of a file panel's row: opening a file either way,
// a shell in the row's folder (a new tab, a split, or the terminal's own
// cd), the root, and the paths.
func (a *App) panelMenu(m *ui.Menu, t *Tab, p *Pane, item string) {
	b := p.files
	isDir := b.isDir[item]
	dir := item
	if !isDir {
		dir = dirName(item)
		if m.Item("Open in Popup").Chosen() {
			a.laterService(func() { a.peekFromPanel(t, item) })
		}
		if m.Item("Open in Editor Tab").Chosen() {
			a.laterService(func() {
				if over := t.treeFocus(); over != nil {
					a.openPaneFile(over, item)
				}
			})
		}
		m.Separator()
	}
	if m.Item("New Tab Here").Chosen() {
		a.laterService(func() { a.newTab(dir) })
	}
	if m.Item("Split Here").Chosen() {
		a.laterService(func() { a.splitHere(t, dir) })
	}
	f := t.treeFocus()
	idle := f != nil && f.Kind == paneTerminal && f.info.Idle
	if m.Item("cd Here").Disabled(!idle).Chosen() {
		a.laterService(func() { a.cdHere(t, dir) })
	}
	m.Separator()
	if isDir {
		if m.Item("Refresh").Chosen() {
			a.laterService(func() { a.refreshDir(p, item) })
		}
		if m.Item("Pin as Root").Disabled(samePath(item, b.root) && b.pinned).Chosen() {
			a.laterService(func() { a.pinRoot(p, item) })
		}
		m.Separator()
	}
	if m.Item("Copy Path").Chosen() {
		a.services.WriteClipboard(item)
	}
	if m.Item("Copy Relative Path").Chosen() {
		a.services.WriteClipboard(relPath(b.root, item))
	}
	if !a.bind.away() && m.Item(revealLabel()).Chosen() {
		a.reveal(item)
	}
}

// awayNote is what a file panel says while its terminal runs a program
// whose files are on another machine, as ssh: the tree stays on this
// host's folder, which is not where the program works.
func (a *App) awayNote(t *Tab) string {
	f := t.treeFocus()
	if f == nil || f.Kind != paneTerminal || f.info.Idle {
		return ""
	}
	what, ok := map[string]string{
		"ssh": "the ssh session", "mosh": "the mosh session", "mosh-client": "the mosh session",
		"wsl": "WSL", "docker": "the container", "podman": "the container",
	}[strings.ToLower(f.info.Program)]
	if !ok {
		return ""
	}
	return "Showing the files of " + a.hostName() + ", not those of " + what + "."
}

// cdHere has the tab's focused terminal change to dir, typed at the
// shell's prompt as that shell takes it.
func (a *App) cdHere(t *Tab, dir string) {
	f := t.treeFocus()
	if f == nil || f.Kind != paneTerminal || f.term == nil || !f.info.Idle {
		return
	}
	f.term.Send([]byte(cdCommand(f.info.Program, dir) + "\r"))
	t.setFocus(f)
	a.focusReq = f
}

// cdCommand is the command that changes shell's directory to dir: cmd's
// cd /d, which also changes drive, and a quoted cd elsewhere, quoted as
// PowerShell or as a POSIX shell takes it.
func cdCommand(shell, dir string) string {
	switch strings.ToLower(shell) {
	case "cmd":
		return `cd /d "` + dir + `"`
	case "pwsh", "powershell":
		return "cd '" + strings.ReplaceAll(dir, "'", "''") + "'"
	}
	return "cd '" + strings.ReplaceAll(dir, "'", `'\''`) + "'"
}

// splitHere opens a shell in dir in a split right of the tab's focused
// terminal.
func (a *App) splitHere(t *Tab, dir string) {
	old := t.treeFocus()
	if old == nil || a.client == nil || a.onePane() {
		return
	}
	cols, rows := 80, 24
	if old.term != nil {
		cols, rows = old.term.Size()
		cols = max(cols/2, 10)
	}
	a.splitWith(old, a.newPane(dir, cols, rows), false)
}

// relPath is item's path from root, as Copy Relative Path gives it.
func relPath(root, item string) string {
	if samePath(item, root) {
		return "."
	}
	if underDir(item, root) {
		return strings.TrimLeft(item[len(root):], `/\`)
	}
	return item
}

// revealLabel names the command that shows a file in this system's file
// manager.
func revealLabel() string {
	switch runtime.GOOS {
	case "windows":
		return "Reveal in File Explorer"
	case "darwin":
		return "Reveal in Finder"
	}
	return "Show in File Manager"
}

// reveal shows a path of this machine's in its file manager; the files of
// a host elsewhere have none here.
func (a *App) reveal(path string) {
	if !a.bind.away() {
		mygo.Shell.ShowItemInFolder(path)
	}
}
